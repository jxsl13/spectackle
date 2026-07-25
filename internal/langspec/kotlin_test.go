package langspec

import (
	"reflect"
	"testing"

	"github.com/jxsl13/spectackle/internal/graph"
)

// kotlinSrc exercises every kotlinSpec Def: a plain top-level fun, a
// modifier-prefixed (public suspend) fun, a generic fun (`fun <T>`), a
// plain class, a data class, an enum class, a sealed class, an object, an
// interface (plus a fun inside its body), and an annotation class.
var kotlinSrc = []byte(`fun topLevel(x: Int): Int {
    return x
}

public suspend fun fetchData(): Int {
    return 0
}

private inline fun <T> identity(x: T): T = x

class Foo {
}

data class Point(val x: Int, val y: Int)

enum class Color {
    RED, GREEN
}

sealed class Shape

object Singleton {
}

interface Drawable {
    fun draw()
}

annotation class Marker
`)

func TestKotlinSpecLangExtensions(t *testing.T) {
	p := SpecParser{S: kotlinSpec}
	if p.Lang() != graph.Lang("kt") {
		t.Errorf("Lang() = %v, want kt", p.Lang())
	}
	if got, want := p.Extensions(), []string{".kt", ".kts"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Extensions() = %v, want %v", got, want)
	}
}

func TestKotlinSpecNodes(t *testing.T) {
	p := SpecParser{S: kotlinSpec}
	pr, err := p.Parse("pkg/app.kt", kotlinSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)

	want := map[graph.NodeID]struct {
		Kind    graph.NodeKind
		Line    int
		EndLine int // 0 means "same as Line"
	}{
		"kt:app.topLevel":  {graph.KFunc, 1, 3}, // brace-counted body now that CallRe is wired (R-0005)
		"kt:app.fetchData": {graph.KFunc, 5, 7},
		"kt:app.identity":  {graph.KFunc, 9, 0}, // expression body `= x`, no brace: span unchanged
		"kt:app.Foo":       {graph.KType, 11, 0},
		"kt:app.Point":     {graph.KType, 14, 0},
		"kt:app.Color":     {graph.KType, 16, 0},
		"kt:app.Shape":     {graph.KType, 20, 0},
		"kt:app.Singleton": {graph.KType, 22, 0},
		"kt:app.Drawable":  {graph.KType, 25, 0},
		"kt:app.draw":      {graph.KFunc, 26, 0}, // abstract decl, no body: span unchanged
		"kt:app.Marker":    {graph.KType, 29, 0},
	}
	if len(pr.Nodes) != len(want) {
		t.Fatalf("got %d nodes, want %d: %+v", len(pr.Nodes), len(want), pr.Nodes)
	}
	for id, w := range want {
		n, ok := byID[id]
		if !ok {
			t.Fatalf("node %s missing, got %+v", id, pr.Nodes)
		}
		wantEnd := w.EndLine
		if wantEnd == 0 {
			wantEnd = w.Line
		}
		if n.Kind != w.Kind {
			t.Errorf("%s Kind = %v, want %v", id, n.Kind, w.Kind)
		}
		if n.Line != w.Line || n.EndLine != wantEnd {
			t.Errorf("%s Line/EndLine = %d/%d, want %d/%d", id, n.Line, n.EndLine, w.Line, wantEnd)
		}
		if n.Lang != graph.Lang("kt") {
			t.Errorf("%s Lang = %v, want kt", id, n.Lang)
		}
		if n.File != "pkg/app.kt" {
			t.Errorf("%s File = %q, want pkg/app.kt", id, n.File)
		}
	}
}

// TestKotlinSpecNegativeLines pins down that a commented-out fun, a call
// site (`myFun(5)`), and an if-statement never mint nodes.
func TestKotlinSpecNegativeLines(t *testing.T) {
	p := SpecParser{S: kotlinSpec}
	pr, err := p.Parse("neg.kt", []byte(`// fun commentedOut() {}
myFun(5)
if (x > 0) {
    println(x)
}
class Neg {
}
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)
	if _, ok := byID["kt:neg.commentedOut"]; ok {
		t.Error("commented-out fun `// fun commentedOut() {}` must not mint a node")
	}
	if _, ok := byID["kt:neg.myFun"]; ok {
		t.Error("call site `myFun(5)` must not mint a node")
	}
	if len(pr.Nodes) != 1 {
		t.Fatalf("got %d nodes, want 1 (only class Neg): %+v", len(pr.Nodes), pr.Nodes)
	}
	if _, ok := byID["kt:neg.Neg"]; !ok {
		t.Errorf("class Neg missing, got %+v", pr.Nodes)
	}
}

// kotlinGapSrc reproduces the R-0005 gap-fixture constructs previously
// missed/mis-captured by kotlinSpec (findings/kotlin.md, scratch fixture
// gap-kotlin/Sample.kt): extension functions whose receiver type must NOT
// be captured as the name, a named companion object, and the intra-file
// calls that exercise the CallRe wiring fixing both call edges and
// EndLine spans.
var kotlinGapSrc = []byte(`fun String.shout(): String {
    return this.uppercase()
}

fun <T> List<T>.second(): T {
    return this[1]
}

fun caller(): String {
    return "hi".shout()
}

class Factory {
    companion object Creator {
        fun create(): Factory {
            return Factory()
        }
    }
}
`)

// TestKotlinSpecGapFixes pins down the R-0005 [high]/[medium] fixes: the
// extension-function receiver-vs-name disambiguation, the named
// companion-object Def, and CallRe-driven edges/EndLine spans.
func TestKotlinSpecGapFixes(t *testing.T) {
	p := SpecParser{S: kotlinSpec}
	pr, err := p.Parse("pkg/gap.kt", kotlinGapSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)

	want := map[graph.NodeID]struct {
		Kind    graph.NodeKind
		Line    int
		EndLine int
	}{
		"kt:gap.shout":   {graph.KFunc, 1, 3},   // extension fun on String: name must be "shout", not "String"
		"kt:gap.second":  {graph.KFunc, 5, 7},   // generic extension fun on List<T>: name must be "second", not "List"
		"kt:gap.caller":  {graph.KFunc, 9, 11},  // calls "hi".shout()
		"kt:gap.Factory": {graph.KType, 13, 13}, // class
		"kt:gap.Creator": {graph.KType, 14, 14}, // named companion object
		"kt:gap.create":  {graph.KFunc, 15, 17}, // calls Factory()
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
	}
	// The extension-function receiver types must never mint their own
	// spurious nodes.
	if _, ok := byID["kt:gap.String"]; ok {
		t.Error("extension-function receiver `String` was wrongly captured as a function name")
	}
	if _, ok := byID["kt:gap.List"]; ok {
		t.Error("extension-function receiver `List` was wrongly captured as a function name")
	}

	wantEdges := map[[2]graph.NodeID]bool{
		{"kt:gap.caller", "kt:gap.shout"}:   false,
		{"kt:gap.create", "kt:gap.Factory"}: false,
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

func TestKotlinSpecRegisteredInAll(t *testing.T) {
	found := false
	for _, p := range All() {
		if p.Lang() == graph.Lang("kt") {
			found = true
		}
	}
	if !found {
		t.Error("All() does not contain a parser for graph.Lang(\"kt\") — kotlinSpec not registered via init()")
	}
}

func TestKotlinSpecDeterministic(t *testing.T) {
	p := SpecParser{S: kotlinSpec}
	pr1, err := p.Parse("pkg/app.kt", kotlinSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	pr2, err := p.Parse("pkg/app.kt", kotlinSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !reflect.DeepEqual(pr1, pr2) {
		t.Errorf("Parse is not deterministic across identical runs:\n%+v\nvs\n%+v", pr1, pr2)
	}
}
