package langspec

import (
	"reflect"
	"testing"

	"github.com/jxsl13/spectackle/internal/graph"
)

// luaSrc exercises every luaSpec Def: plain `function`, `local function`,
// table-qualified `function M.name`, method `function Obj:method`, and the
// `name = function(...)` assignment form (plain and `local`).
var luaSrc = []byte(`function run(x)
    return x
end

local function helper(y)
    return y
end

function M.tool(z)
    return z
end

function Obj:method(a)
    return a
end

local add = function(a, b)
    return a + b
end

mul = function(a, b)
    return a * b
end
`)

func TestLuaSpecLangExtensions(t *testing.T) {
	p := SpecParser{S: luaSpec}
	if p.Lang() != graph.LangLua {
		t.Errorf("Lang() = %v, want %v", p.Lang(), graph.LangLua)
	}
	if got, want := p.Extensions(), []string{".lua"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Extensions() = %v, want %v", got, want)
	}
}

// TestLuaSpecNodes' EndLine expectations were updated by T-0119: luaSpec now
// sets CallRe + EndSpan, so every 3-line body here (def line, one statement,
// "end") keyword-counts to a real 3-line span instead of collapsing to
// Line==EndLine — the pre-T-0119 assertion (EndLine always equals Line) was
// pinning down exactly the "span defect" finding this task fixes ([medium]:
// "Multi-line function/method body span ... the near-universal case in
// idiomatic Lua"), so updating it here is the fix, not a regression.
func TestLuaSpecNodes(t *testing.T) {
	p := SpecParser{S: luaSpec}
	pr, err := p.Parse("pkg/app.lua", luaSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)

	want := map[graph.NodeID]struct {
		Kind          graph.NodeKind
		Line, EndLine int
	}{
		"lua:app.run":        {graph.KFunc, 1, 3},
		"lua:app.helper":     {graph.KFunc, 5, 7},
		"lua:app.M.tool":     {graph.KFunc, 9, 11},
		"lua:app.Obj:method": {graph.KFunc, 13, 15},
		"lua:app.add":        {graph.KFunc, 17, 19},
		"lua:app.mul":        {graph.KFunc, 21, 23},
	}
	if len(pr.Nodes) != len(want) {
		t.Fatalf("got %d nodes, want %d: %+v", len(pr.Nodes), len(want), pr.Nodes)
	}
	for id, w := range want {
		n, ok := byID[id]
		if !ok {
			t.Fatalf("node %s missing, got %+v", id, pr.Nodes)
		}
		if n.Kind != w.Kind {
			t.Errorf("%s Kind = %v, want %v", id, n.Kind, w.Kind)
		}
		if n.Line != w.Line || n.EndLine != w.EndLine {
			t.Errorf("%s Line/EndLine = %d/%d, want %d/%d", id, n.Line, n.EndLine, w.Line, w.EndLine)
		}
		if n.Lang != graph.LangLua {
			t.Errorf("%s Lang = %v, want lua", id, n.Lang)
		}
		if n.File != "pkg/app.lua" {
			t.Errorf("%s File = %q, want pkg/app.lua", id, n.File)
		}
	}
}

// luaGapFixtureSrc reuses the sample.lua fixture's constructs 1, 2, and 15
// (gap-lua/sample.lua from R-0005's empirical findings): a multi-line
// `for ... do ... end` loop nested inside a function body, an `if ... then
// ... end` nested inside another, and a named local function nested inside
// its enclosing function's body — all previously collapsed to a single-line
// span with zero call edges ([medium] "Call edges from any Lua
// function/method body to any callee — CallRe is unset for lua").
var luaGapFixtureSrc = []byte(`function run(x)
  local total = 0
  for i = 1, x do
    total = total + helper(i, total)
  end
  return total
end

local function helper(a, b)
  if a > b then
    return run(a - b)
  end
  return a + b
end

function outer(z)
  local function inner(w)
    return w * w
  end
  return inner(z) + deepFunc(z)
end
`)

// TestLuaSpecGapFixtureSpansAndEdges proves the fixed span+edge behavior
// against real sample.lua constructs: run's body correctly spans past its
// nested `for ... do ... end` loop (not truncated at the loop's own "end"),
// helper's body correctly spans past its nested `if ... then ... end`, and
// both direct calls (run<->helper) produce ECall edges. outer's body spans
// past a nested named local function def without over- or under-counting
// depth from the double "function" opener.
func TestLuaSpecGapFixtureSpansAndEdges(t *testing.T) {
	p := SpecParser{S: luaSpec}
	pr, err := p.Parse("sample.lua", luaGapFixtureSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)

	run, ok := byID["lua:sample.run"]
	if !ok {
		t.Fatalf("lua:sample.run missing, got %+v", pr.Nodes)
	}
	if run.Line != 1 || run.EndLine != 7 {
		t.Errorf("run Line/EndLine = %d/%d, want 1/7 (spans past the nested for-loop's own \"end\")", run.Line, run.EndLine)
	}

	helper, ok := byID["lua:sample.helper"]
	if !ok {
		t.Fatalf("lua:sample.helper missing, got %+v", pr.Nodes)
	}
	if helper.Line != 9 || helper.EndLine != 14 {
		t.Errorf("helper Line/EndLine = %d/%d, want 9/14 (spans past the nested if-then's own \"end\")", helper.Line, helper.EndLine)
	}

	outer, ok := byID["lua:sample.outer"]
	if !ok {
		t.Fatalf("lua:sample.outer missing, got %+v", pr.Nodes)
	}
	if outer.Line != 16 || outer.EndLine != 21 {
		t.Errorf("outer Line/EndLine = %d/%d, want 16/21 (spans past the nested local function def)", outer.Line, outer.EndLine)
	}

	wantEdges := map[graph.NodeID]graph.NodeID{
		"lua:sample.run":    "lua:sample.helper",
		"lua:sample.helper": "lua:sample.run",
	}
	found := map[graph.NodeID]bool{}
	for _, e := range pr.Edges {
		if dst, ok := wantEdges[e.Src]; ok && e.Dst == dst {
			found[e.Src] = true
		}
	}
	for src := range wantEdges {
		if !found[src] {
			t.Errorf("missing ECall edge %s -> %s, got edges %+v", src, wantEdges[src], pr.Edges)
		}
	}
}

// TestLuaSpecOneLinerDoesNotDoubleCount pins the same-line open+close case
// (collide.lua's table-field one-liner form,
// `onClick = function(self, e) return "button" end,`): open and close both
// on the def line net to depth 0, so KeywordSpan returns the def line
// itself as EndLine (single-line body), not an over-extended span.
func TestLuaSpecOneLinerDoesNotDoubleCount(t *testing.T) {
	p := SpecParser{S: luaSpec}
	pr, err := p.Parse("collide.lua", []byte(`local ButtonHandlers = {
  onClick = function(self, e) return "button" end,
}
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)
	n, ok := byID["lua:collide.onClick"]
	if !ok {
		t.Fatalf("lua:collide.onClick missing, got %+v", pr.Nodes)
	}
	if n.Line != 2 || n.EndLine != 2 {
		t.Errorf("onClick Line/EndLine = %d/%d, want 2/2 (single-line body)", n.Line, n.EndLine)
	}
}

// TestLuaSpecAnonymousFunctionCallStop pins that the `function(` keyword in
// an anonymous-function assignment (`run2 = function(x)`) never itself
// becomes a phantom ECall edge to a symbol literally named "function" — it
// is CallRe-shaped (`function(`) but Stop-listed.
func TestLuaSpecAnonymousFunctionCallStop(t *testing.T) {
	p := SpecParser{S: luaSpec}
	pr, err := p.Parse("cb.lua", []byte(`function outer(x)
  local run2 = function(y)
    return y
  end
  return run2(x)
end
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	for _, e := range pr.Edges {
		if e.Dst == "lua:cb.function" {
			t.Errorf("got a phantom edge to \"function\": %+v", e)
		}
	}
}

// TestLuaSpecNegativeLines pins down that `function` inside a comment, a
// plain call, and a bare `end` never mint nodes.
func TestLuaSpecNegativeLines(t *testing.T) {
	p := SpecParser{S: luaSpec}
	pr, err := p.Parse("neg.lua", []byte(`-- function commented(x)
foo(1)
end
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(pr.Nodes) != 0 {
		t.Fatalf("got %d nodes, want 0: %+v", len(pr.Nodes), pr.Nodes)
	}
}

func TestLuaSpecRegisteredInAll(t *testing.T) {
	found := false
	for _, p := range All() {
		if p.Lang() == graph.LangLua {
			found = true
		}
	}
	if !found {
		t.Error("All() does not contain a parser for graph.LangLua — luaSpec not registered via init()")
	}
}

func TestLuaSpecDeterministic(t *testing.T) {
	p := SpecParser{S: luaSpec}
	pr1, err := p.Parse("pkg/app.lua", luaSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	pr2, err := p.Parse("pkg/app.lua", luaSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !reflect.DeepEqual(pr1, pr2) {
		t.Errorf("Parse is not deterministic across identical runs:\n%+v\nvs\n%+v", pr1, pr2)
	}
}
