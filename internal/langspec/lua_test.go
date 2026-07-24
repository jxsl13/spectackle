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

func TestLuaSpecNodes(t *testing.T) {
	p := SpecParser{S: luaSpec}
	pr, err := p.Parse("pkg/app.lua", luaSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)

	want := map[graph.NodeID]struct {
		Kind graph.NodeKind
		Line int
	}{
		"lua:app.run":        {graph.KFunc, 1},
		"lua:app.helper":     {graph.KFunc, 5},
		"lua:app.M.tool":     {graph.KFunc, 9},
		"lua:app.Obj:method": {graph.KFunc, 13},
		"lua:app.add":        {graph.KFunc, 17},
		"lua:app.mul":        {graph.KFunc, 21},
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
		if n.Line != w.Line || n.EndLine != w.Line {
			t.Errorf("%s Line/EndLine = %d/%d, want %d", id, n.Line, n.EndLine, w.Line)
		}
		if n.Lang != graph.LangLua {
			t.Errorf("%s Lang = %v, want lua", id, n.Lang)
		}
		if n.File != "pkg/app.lua" {
			t.Errorf("%s File = %q, want pkg/app.lua", id, n.File)
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
