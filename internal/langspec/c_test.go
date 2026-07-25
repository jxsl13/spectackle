package langspec

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/jxsl13/spectackle/internal/cspan"
	"github.com/jxsl13/spectackle/internal/graph"
	"github.com/jxsl13/spectackle/internal/index"
	"github.com/jxsl13/spectackle/internal/resolve"
	"github.com/jxsl13/spectackle/internal/store"
)

// cSrc exercises every cSpec Def shape plus every false-positive trap called
// out in the task brief: a prototype, a static definition, a function-like
// macro, a struct/typedef/enum, a #include (must not match, no leading
// [A-Za-z_]), and indented control-flow/return/call statements (must not
// match, no leading whitespace allowed by the anchor).
var cSrc = []byte(`#include <stddef.h>

/* prototype */
int launch_saxpy(int n, float a, const float *x, float *y);

struct Point {
    int x, y;
};

typedef struct Named {
    int id;
} Named;

enum Color { RED, GREEN, BLUE };

#define MAX(a, b) ((a) > (b) ? (a) : (b))

static void helper(void) {
    if (helper_called) {
        return;
    }
    for (int i = 0; i < 10; i++) {
        while (i > 0) {
            switch (i) {
            case 1:
                return;
            }
        }
    }
    do_something(1, 2);
}
`)

func cNodesByID(pr index.ParseResult) map[graph.NodeID]graph.Node {
	m := map[graph.NodeID]graph.Node{}
	for _, n := range pr.Nodes {
		m[n.ID] = n
	}
	return m
}

func TestCSpecLangExtensions(t *testing.T) {
	p := SpecParser{S: cSpec}
	if p.Lang() != graph.LangC {
		t.Errorf("Lang() = %v, want %v", p.Lang(), graph.LangC)
	}
	if got, want := p.Extensions(), []string{".c", ".h"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Extensions() = %v, want %v", got, want)
	}
}

func TestCSpecNodes(t *testing.T) {
	p := SpecParser{S: cSpec}
	pr, err := p.Parse("kernels/saxpy.h", cSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := cNodesByID(pr)

	want := map[graph.NodeID]struct {
		Kind    graph.NodeKind
		Line    int
		EndLine int // brace-counted body end (LSP-001); bodyless defs: == Line
	}{
		"c:launch_saxpy": {graph.KFunc, 4, 4},
		"c:Point":        {graph.KType, 6, 6},
		"c:Named":        {graph.KType, 10, 10},
		"c:Color":        {graph.KType, 14, 14},
		"c:MAX":          {graph.KFunc, 16, 16},
		// helper's body spans lines 18-31 (if/for/while/switch nested inside):
		// CallRe is enabled for cSpec, so EndLine now reflects the real
		// brace-counted span instead of collapsing to Line.
		"c:helper": {graph.KFunc, 18, 31},
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
		if n.Lang != graph.LangC {
			t.Errorf("%s Lang = %v, want c", id, n.Lang)
		}
	}
}

// TestCSpecNoFalsePositivesOnControlFlow is the acceptance-brief guard: a
// fixture full of indented if/for/while/switch/return/call statements must
// never mint a "c:if", "c:for", "c:while", "c:switch", "c:return" or
// "c:do_something" node. It also checks #include never mints a node (its
// line starts with '#', which the function Def's anchor structurally
// excludes since it requires [A-Za-z_] as the first character).
func TestCSpecNoFalsePositivesOnControlFlow(t *testing.T) {
	p := SpecParser{S: cSpec}
	pr, err := p.Parse("kernels/saxpy.h", cSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	banned := []graph.NodeID{"c:if", "c:for", "c:while", "c:switch", "c:return", "c:sizeof", "c:do_something", "c:helper_called", "c:stddef"}
	byID := cNodesByID(pr)
	for _, id := range banned {
		if n, ok := byID[id]; ok {
			t.Errorf("false positive node %s = %+v, must not be minted from control-flow/call/include lines", id, n)
		}
	}
}

// TestCSpecCallEdgesFromControlFlowFixture reuses cSrc (helper()'s body) to
// prove LSP-001 and the Stop list together: the only call inside helper's
// nested if/for/while/switch/return block is do_something(1, 2) — every
// control-flow keyword that looks like `word (` must NOT become an edge
// (Stop-listed), and do_something must become exactly one c:helper ->
// c:do_something ECall edge at its call site.
func TestCSpecCallEdgesFromControlFlowFixture(t *testing.T) {
	p := SpecParser{S: cSpec}
	pr, err := p.Parse("x.c", cSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(pr.Edges) != 1 {
		t.Fatalf("want exactly 1 ECall edge, got %d: %+v", len(pr.Edges), pr.Edges)
	}
	e := pr.Edges[0]
	if e.Src != "c:helper" || e.Dst != "c:do_something" || e.Kind != graph.ECall {
		t.Errorf("edge = %+v, want c:helper -ECall-> c:do_something", e)
	}
	if e.File != "x.c" || e.Line != 30 {
		t.Errorf("edge File/Line = %q/%d, want x.c/30 (the do_something(1, 2) call site)", e.File, e.Line)
	}
}

// cCallSrc is the PART 2 acceptance fixture: kernel_launch() calls helper()
// and printf() (the two expected ECall edges) plus every negative case named
// in the task brief — `if (`, `sizeof(`, and a recursive self-call — none of
// which may produce an edge.
var cCallSrc = []byte(`static void helper(void) {
}

static int kernel_launch(int n) {
    if (n > 0) {
        return n;
    }
    int sz = sizeof(int);
    helper();
    printf("launched %d\n", sz);
    return kernel_launch(n - 1);
}
`)

func TestCSpecCallEdgesKernelLaunch(t *testing.T) {
	p := SpecParser{S: cSpec}
	pr, err := p.Parse("launch.c", cCallSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(pr.Edges) != 2 {
		t.Fatalf("want exactly 2 ECall edges (helper, printf), got %d: %+v", len(pr.Edges), pr.Edges)
	}
	want := map[graph.NodeID]int{"c:helper": 9, "c:printf": 10}
	got := map[graph.NodeID]int{}
	for _, e := range pr.Edges {
		if e.Src != "c:kernel_launch" {
			t.Errorf("edge Src = %q, want c:kernel_launch: %+v", e.Src, e)
		}
		if e.Kind != graph.ECall {
			t.Errorf("edge Kind = %v, want ECall: %+v", e.Kind, e)
		}
		if e.File != "launch.c" {
			t.Errorf("edge File = %q, want launch.c: %+v", e.File, e)
		}
		got[e.Dst] = e.Line
	}
	for dst, wantLine := range want {
		gotLine, ok := got[dst]
		if !ok {
			t.Errorf("missing edge to %s, got edges %+v", dst, pr.Edges)
			continue
		}
		if gotLine != wantLine {
			t.Errorf("edge to %s at line %d, want %d", dst, gotLine, wantLine)
		}
	}
	// negatives: `if (`, `sizeof(`, and the recursive self-call to
	// kernel_launch must never appear as edge destinations.
	for _, banned := range []graph.NodeID{"c:if", "c:sizeof", "c:kernel_launch"} {
		if line, ok := got[banned]; ok {
			t.Errorf("false positive edge to %s at line %d: if/sizeof/own-name must never become call edges", banned, line)
		}
	}
}

// TestCSpecAllmanBraceSpanRegression is T-0053's C-side regression for
// cspan.Span (the scanner T-0053 extended, and T-0054 extracted out of this
// package into internal/cspan): a bare Allman-style body (opening '{' on
// the line after the def line, ddnet's universal C/C++ style) is now found
// and depth-counted correctly.
//
// This drives cspan.Span directly rather than through cSpec.Parse: cSpec's
// plain-function Def (see c.go) is anchored `[;{]\s*$`, requiring the def
// line itself to end in ';' or '{' — a bare Allman signature line like
// `static void helper(void)` ends in ')' and so never matches that Def at
// all, independent of cspan.Span. That anchor is a separate, pre-existing
// limitation of c.go's regex (not touched here per T-0053's scope: only
// braceSpan + its call site, now cspan.Span); it means ddnet's own
// top-level Allman C functions still won't mint nodes even after this fix
// — see docs/validation-ddnet.md's re-validation appendix. cppSpec's
// out-of-line method Def (`Foo::Bar(`, see cpp.go) has no such end anchor,
// so the C++ side benefits from this fix end-to-end (see
// TestCppSpecAllmanMethodCallEdges in cpp_test.go).
func TestCSpecAllmanBraceSpanRegression(t *testing.T) {
	src := "static void helper(void)\n{\n    do_something(1, 2);\n}\n"
	lines, err := scanLines([]byte(src))
	if err != nil {
		t.Fatalf("scanLines: %v", err)
	}
	end, ok := cspan.Span(lines, 0)
	if !ok {
		t.Fatal("cspan.Span ok = false, want true (Allman brace on line 2)")
	}
	if end != 3 {
		t.Errorf("cspan.Span end = %d (0-indexed), want 3 (line 4, the closing '}')", end)
	}
}

func TestCSpecDeterministic(t *testing.T) {
	p := SpecParser{S: cSpec}
	pr1, err := p.Parse("kernels/saxpy.h", cSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	pr2, err := p.Parse("kernels/saxpy.h", cSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !reflect.DeepEqual(pr1, pr2) {
		t.Errorf("Parse is not deterministic across identical runs:\n%+v\nvs\n%+v", pr1, pr2)
	}
}

// cT0122Src is R-0005's gap-c fixture (scratchpad/gap-c/sample.c), copied
// verbatim as the regression input for T-0122's c.go hardening: same-line
// one-liner bodies, Allman signatures (brace on its own line, and return
// type on its own line too), multi-line parameter lists, and a
// function-pointer typedef. The anonymous `typedef struct { ... } Complex;`
// finding from R-0005's c.md is deliberately NOT fixed here (see T-0122's
// final report): the alias name only appears on the struct's *closing*
// line, which is structurally identical to a plain `struct Named { ... }
// Named;` closing line (see cSrc's Named fixture above) — matching it
// unconditionally would mint a second, duplicate "c:Named"-shaped node for
// every already-tagged typedef struct, which is worse than the gap it
// would close, and there is no per-line-regex way to tell the two shapes
// apart without carrying state across lines.
var cT0122Src = []byte(`typedef int (*BinOp)(int, int);

int add(int a, int b) { return a + b; }

static int square(int x) { return x * x; }

int
subtract(int a, int b)
{
    return a - b;
}

double
compute_average(int values[], int count,
                 double weight)
{
    return 0.0;
}

char *
format_point(int x)
{
    return add(x, x);
}

int compute_total(int a, int b) {
    int sum = add(a, b);
    int sq = square(sum);
    return sq;
}
`)

// TestCSpecT0122SameLineBody covers R-0005 c.md's [high] "single-line
// (same-line) function body" finding: a K&R one-liner whose body text
// follows the opening `{` on the same physical line used to be excluded by
// the old `[;{]\s*$` end anchor.
func TestCSpecT0122SameLineBody(t *testing.T) {
	p := SpecParser{S: cSpec}
	pr, err := p.Parse("x.c", cT0122Src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := cNodesByID(pr)
	for _, id := range []graph.NodeID{"c:add", "c:square"} {
		n, ok := byID[id]
		if !ok {
			t.Fatalf("node %s missing, got %+v", id, pr.Nodes)
		}
		if n.Kind != graph.KFunc {
			t.Errorf("%s Kind = %v, want KFunc", id, n.Kind)
		}
	}
	if n := byID["c:add"]; n.Line != 3 {
		t.Errorf("c:add Line = %d, want 3", n.Line)
	}
	if n := byID["c:square"]; n.Line != 5 {
		t.Errorf("c:square Line = %d, want 5", n.Line)
	}
	if n := byID["c:add"]; n.EndLine != 3 {
		t.Errorf("c:add EndLine = %d, want 3 (single-line body)", n.EndLine)
	}
}

// TestCSpecT0122AllmanAndMultiLineSignature covers R-0005 c.md's [high]
// "Allman/BSD-style signature" (opening brace AND return type each on their
// own line) and [high] "multi-line parameter list" findings.
func TestCSpecT0122AllmanAndMultiLineSignature(t *testing.T) {
	p := SpecParser{S: cSpec}
	pr, err := p.Parse("x.c", cT0122Src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := cNodesByID(pr)

	want := map[graph.NodeID]struct{ Line, EndLine int }{
		"c:subtract":        {8, 11},
		"c:compute_average": {14, 18},
		"c:format_point":    {21, 24},
	}
	for id, w := range want {
		n, ok := byID[id]
		if !ok {
			t.Fatalf("node %s missing, got %+v", id, pr.Nodes)
		}
		if n.Kind != graph.KFunc {
			t.Errorf("%s Kind = %v, want KFunc", id, n.Kind)
		}
		if n.Line != w.Line || n.EndLine != w.EndLine {
			t.Errorf("%s Line/EndLine = %d/%d, want %d/%d", id, n.Line, n.EndLine, w.Line, w.EndLine)
		}
	}
}

// TestCSpecT0122FunctionPointerTypedef covers R-0005 c.md's [medium]
// "function-pointer typedef" finding (`typedef int (*BinOp)(int, int);`)
// and pins the regression this fix could have introduced: the return-type
// token ("int") sitting right before the `(*Alias)` declarator must never
// itself be mistaken for a KFunc def (see the Def's own comment in c.go).
func TestCSpecT0122FunctionPointerTypedef(t *testing.T) {
	p := SpecParser{S: cSpec}
	pr, err := p.Parse("x.c", cT0122Src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := cNodesByID(pr)
	n, ok := byID["c:BinOp"]
	if !ok {
		t.Fatalf("node c:BinOp missing, got %+v", pr.Nodes)
	}
	if n.Kind != graph.KType {
		t.Errorf("c:BinOp Kind = %v, want KType", n.Kind)
	}
	if n.Line != 1 {
		t.Errorf("c:BinOp Line = %d, want 1", n.Line)
	}
	if _, ok := byID["c:int"]; ok {
		t.Error("false positive node c:int: the function-pointer typedef's return type must never be captured as a def name")
	}
}

// TestCSpecT0122CallEdgesFromFixedDefs proves the cascade R-0005 c.md's
// EDGES section documents: format_point -> add and compute_total ->
// add/square only exist once add/square/format_point are themselves real
// nodes (previously missed defs silently erased their own call edges too).
func TestCSpecT0122CallEdgesFromFixedDefs(t *testing.T) {
	p := SpecParser{S: cSpec}
	pr, err := p.Parse("x.c", cT0122Src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := map[[2]graph.NodeID]bool{
		{"c:format_point", "c:add"}:    false,
		{"c:compute_total", "c:add"}:    false,
		{"c:compute_total", "c:square"}: false,
	}
	for _, e := range pr.Edges {
		key := [2]graph.NodeID{e.Src, e.Dst}
		if _, ok := want[key]; ok {
			want[key] = true
		}
	}
	for k, found := range want {
		if !found {
			t.Errorf("missing expected edge %s -> %s, got edges %+v", k[0], k[1], pr.Edges)
		}
	}
}

// TestIndexAllSaxpyExampleCChainWithHeader is the e2e acceptance test named
// in the task brief: index the real examples/saxpy fixture with GoParser +
// CudaParser + langspec.All() (which now includes cSpec) and confirm the
// full go -> c -> cu chain holds, with c:launch_saxpy a REAL, located node
// (File != "" and Line > 0) rather than a cgo-resolver stub. Both
// saxpy/kernels/saxpy.h (our new C parser) and saxpy/kernels/saxpy.cu
// (index.CudaParser's extern "C" wrapper) define launch_saxpy; per
// internal/index.disambiguate, whichever file sorts first claims the base
// "c:launch_saxpy" ID deterministically ("saxpy.cu" < "saxpy.h"
// lexicographically), so this test intentionally does not pin down which of
// the two files wins — only that the node is real.
func TestIndexAllSaxpyExampleCChainWithHeader(t *testing.T) {
	root, err := filepath.Abs(filepath.FromSlash("../../examples/saxpy"))
	if err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(root); statErr != nil {
		t.Skipf("examples/saxpy not available: %v", statErr)
	}

	g := graph.NewMem()
	parsers := append([]index.LanguageParser{index.GoParser{}, index.CudaParser{}}, All()...)
	idx := index.New(g, store.NewMem(), parsers, resolve.Default().All())
	if _, err := idx.IndexAll(context.Background(), root); err != nil {
		t.Fatalf("IndexAll(%s): %v", root, err)
	}

	// (a) c:launch_saxpy is a real, located node — not a stub — regardless
	// of which of saxpy.h/saxpy.cu won the disambiguation race.
	wrapper, ok := g.Node("c:launch_saxpy")
	if !ok {
		t.Fatal("node c:launch_saxpy missing")
	}
	if wrapper.File == "" {
		t.Error("c:launch_saxpy File is empty, want a real source location")
	}
	if wrapper.Line <= 0 {
		t.Errorf("c:launch_saxpy Line = %d, want > 0 (real node, not stub)", wrapper.Line)
	}
	if wrapper.Kind != graph.KFunc {
		t.Errorf("c:launch_saxpy Kind = %v, want KFunc", wrapper.Kind)
	}

	// (b) go:saxpy.Saxpy -ECgo-> c:launch_saxpy
	cgo := g.Neighbors("go:saxpy.Saxpy", graph.Out, []graph.EdgeKind{graph.ECgo})
	foundCgo := false
	for _, e := range cgo {
		if e.Dst == "c:launch_saxpy" {
			foundCgo = true
		}
	}
	if !foundCgo {
		t.Errorf("Neighbors(go:saxpy.Saxpy, Out, ECgo) = %+v, want edge to c:launch_saxpy", cgo)
	}

	// (c) c:launch_saxpy -ELaunch-> cu:saxpy_kernel
	launch := g.Neighbors("c:launch_saxpy", graph.Out, []graph.EdgeKind{graph.ELaunch})
	foundLaunch := false
	for _, e := range launch {
		if e.Dst == "cu:saxpy_kernel" {
			foundLaunch = true
		}
	}
	if !foundLaunch {
		t.Errorf("Neighbors(c:launch_saxpy, Out, ELaunch) = %+v, want edge to cu:saxpy_kernel", launch)
	}

	kernel, ok := g.Node("cu:saxpy_kernel")
	if !ok {
		t.Fatal("node cu:saxpy_kernel missing")
	}
	if kernel.Kind != graph.KKernel || kernel.Lang != graph.LangCuda {
		t.Errorf("cu:saxpy_kernel = %+v, want Kind=KKernel Lang=cu", kernel)
	}
}
