package langspec

import (
	"reflect"
	"testing"

	"github.com/jxsl13/spectackle/internal/graph"
)

// NOTE: TestJavascriptSpecLangExtensions and js registration coverage
// already live in langspec_test.go (javascriptSpec has long been one of
// that file's two cross-cutting reference Specs, alongside pythonSpec —
// see TestJavascriptSpecNodes, TestAllReturnsOneParserPerRegisteredSpec,
// TestIndexAllPyAndJS), so this file — the dedicated javascript_test.go
// R-0005 called for — adds the [high]/[medium] gap-fix coverage only,
// without redeclaring those.

// javascriptGapSrc reproduces (verbatim, R-0005's ground-truth scratch
// fixture gap-javascript/sample.js) every construct findings/javascript.md
// scored as a [high]/[medium] miss: a plain top-level function calling a
// sibling function (call-edge/EndLine-span proof), an exported function, an
// exported async function, a generator, `export default function` (broke
// the old function Def outright), a class whose constructor/instance/
// async/static/getter methods previously had no Def at all, `export
// default class` (broke the old class Def), a CommonJS `exports.foo =
// function foo(){}` assignment (no prior Def), single- and multi-line-
// signature arrow functions, an `async` arrow (proof javascriptCallStop's
// "async"/"await" entries are needed — the arrow's own def line sits
// inside its own body span), and a nested/indented function declaration
// (the old bare-`^`-anchored Defs never matched anything but column 0).
var javascriptGapSrc = []byte(`// 1. add - plain top-level function declaration
function add(a, b) {
  return sum(a, b);
}

// 2. sum - helper called by add, tests call-edge capture
function sum(a, b) {
  return a + b;
}

// 3. multiply - exported top-level function declaration
export function multiply(a, b) {
  return a * b;
}

// 4. fetchData - exported async top-level function
export async function fetchData(url) {
  return await rawFetch(url);
}

// 5. range - top-level generator function
function* range(n) {
  for (let i = 0; i < n; i++) {
    yield i;
  }
}

// 6. mainEntry - export default function (idiomatic ES module entry point)
export default function mainEntry() {
  return add(1, 2);
}

// 7. Calculator - top-level class declaration
class Calculator {
  // 8. constructor - class constructor method
  constructor(value) {
    this.value = value;
  }

  // 9. addValue - regular instance method, calls sum()
  addValue(x) {
    return sum(this.value, x);
  }

  // 10. fetchRemote - async instance method
  async fetchRemote() {
    return await fetchData("https://example.com");
  }

  // 11. create - static factory method
  static create() {
    return new Calculator(0);
  }

  // 12. value - getter accessor
  get value() {
    return this._value;
  }
}

// 13. Widget - export default class declaration
export default class Widget {
  render() {
    return "<div></div>";
  }
}

// 14. helper - CommonJS-style export assignment (function expression)
exports.helper = function helper(x) {
  return x * 2;
};

// 15. square - single-line const arrow function
const square = (x) => x * x;

// 16. computeSum - exported const arrow function with multi-line parameter list
export const computeSum = (
  a,
  b
) => {
  return a + b;
};

// 17. delayedLog - let-bound async arrow function
let delayedLog = async (msg) => {
  console.log(msg);
};

// 18. outer / inner - nested function declaration (inner is indented)
function outer() {
  function inner(n) {
    return n * 2;
  }
  return inner(5);
}
`)

// TestJavascriptSpecGapFixes pins down every R-0005 [high]/[medium] fix in
// one pass: the class-method Def (constructor/instance/async/static/getter
// — the headline "no method Def exists at all" miss), `export default
// function`/`export default class`, the CommonJS export-assignment Def,
// nested/indented function declarations, and CallRe-driven call edges +
// real brace-counted EndLine spans (previously every node's EndLine
// collapsed to its Line because CallRe was never set).
func TestJavascriptSpecGapFixes(t *testing.T) {
	p := SpecParser{S: javascriptSpec}
	pr, err := p.Parse("pkg/sample.js", javascriptGapSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)

	want := map[graph.NodeID]struct {
		Kind    graph.NodeKind
		Line    int
		EndLine int
	}{
		"js:sample.add":         {graph.KFunc, 2, 4},
		"js:sample.sum":         {graph.KFunc, 7, 9},
		"js:sample.multiply":    {graph.KFunc, 12, 14},
		"js:sample.fetchData":   {graph.KFunc, 17, 19},
		"js:sample.range":       {graph.KFunc, 22, 26},
		"js:sample.mainEntry":   {graph.KFunc, 29, 31},
		"js:sample.Calculator":  {graph.KType, 34, 34}, // KType spans never brace-count (langspec.go gates that on KFunc/KMethod)
		"js:sample.constructor": {graph.KFunc, 36, 38},
		"js:sample.addValue":    {graph.KFunc, 41, 43},
		"js:sample.fetchRemote": {graph.KFunc, 46, 48},
		"js:sample.create":      {graph.KFunc, 51, 53},
		"js:sample.value":       {graph.KFunc, 56, 58},
		"js:sample.Widget":      {graph.KType, 62, 62},
		"js:sample.render":      {graph.KFunc, 63, 65},
		"js:sample.helper":      {graph.KFunc, 69, 71},
		"js:sample.square":      {graph.KFunc, 74, 74},
		"js:sample.computeSum":  {graph.KFunc, 77, 82},
		"js:sample.delayedLog":  {graph.KFunc, 85, 87},
		"js:sample.outer":       {graph.KFunc, 90, 95},
		"js:sample.inner":       {graph.KFunc, 91, 93},
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
		if n.Lang != graph.LangJS {
			t.Errorf("%s Lang = %v, want js", id, n.Lang)
		}
	}

	// CallRe-driven call edges (R-0005 EDGES: previously zero, always, for
	// every JS file — javascriptSpec never set CallRe at all).
	wantEdges := map[[2]graph.NodeID]bool{
		{"js:sample.add", "js:sample.sum"}:               false,
		{"js:sample.fetchData", "js:sample.rawFetch"}:    false,
		{"js:sample.mainEntry", "js:sample.add"}:         false,
		{"js:sample.addValue", "js:sample.sum"}:          false,
		{"js:sample.fetchRemote", "js:sample.fetchData"}: false,
		{"js:sample.create", "js:sample.Calculator"}:     false,
		{"js:sample.delayedLog", "js:sample.log"}:        false, // console.log(msg) -> dangling "log"
		{"js:sample.outer", "js:sample.inner"}:           false,
	}
	for _, e := range pr.Edges {
		if e.Kind != graph.ECall {
			t.Errorf("edge Kind = %v, want ECall", e.Kind)
		}
		key := [2]graph.NodeID{e.Src, e.Dst}
		if _, ok := wantEdges[key]; ok {
			wantEdges[key] = true
		}
	}
	for k, seen := range wantEdges {
		if !seen {
			t.Errorf("missing call edge %s -> %s, got edges %+v", k[0], k[1], pr.Edges)
		}
	}
}

// TestJavascriptSpecNoFalsePositivesOnControlFlow pins down that the new
// class-method Def's "name butts directly against `(`, no space" adjacency
// requirement (RE2 has no lookahead, so this — not a keyword blocklist — is
// what disambiguates a method from control flow) correctly excludes
// control-flow statements written in the idiomatic (space-before-paren)
// style, even at method-body indentation, and that the async/await
// CallRe Stop entries suppress the false edges an arrow function's own def
// line would otherwise create.
func TestJavascriptSpecNoFalsePositivesOnControlFlow(t *testing.T) {
	src := []byte(`function outer() {
  if (x) {
    return 1;
  }
  for (let i = 0; i < 3; i++) {
    console.log(i);
  }
  while (x) {
    break;
  }
  switch (x) {
    case 1:
      break;
  }
  return 0;
}
`)
	p := SpecParser{S: javascriptSpec}
	pr, err := p.Parse("neg.js", src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)
	if len(pr.Nodes) != 1 {
		t.Fatalf("got %d nodes, want 1 (only outer): %+v", len(pr.Nodes), pr.Nodes)
	}
	if _, ok := byID["js:neg.outer"]; !ok {
		t.Errorf("js:neg.outer missing, got %+v", pr.Nodes)
	}
	banned := []graph.NodeID{"js:neg.if", "js:neg.for", "js:neg.while", "js:neg.switch"}
	for _, id := range banned {
		if n, ok := byID[id]; ok {
			t.Errorf("false positive node %s = %+v, control-flow lines must never mint a node", id, n)
		}
	}
	for _, e := range pr.Edges {
		if e.Dst == "js:neg.if" || e.Dst == "js:neg.for" || e.Dst == "js:neg.while" || e.Dst == "js:neg.switch" {
			t.Errorf("false positive call edge to control-flow keyword: %+v", e)
		}
	}
}

func TestJavascriptSpecDeterministic(t *testing.T) {
	p := SpecParser{S: javascriptSpec}
	pr1, err := p.Parse("pkg/sample.js", javascriptGapSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	pr2, err := p.Parse("pkg/sample.js", javascriptGapSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !reflect.DeepEqual(pr1, pr2) {
		t.Errorf("Parse is not deterministic across identical runs:\n%+v\nvs\n%+v", pr1, pr2)
	}
}
