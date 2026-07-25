package langspec

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jxsl13/spectackle/internal/graph"
	"github.com/jxsl13/spectackle/internal/index"
	"github.com/jxsl13/spectackle/internal/resolve"
	"github.com/jxsl13/spectackle/internal/store"
)

// objcSrc exercises all three objcSpec Defs (instance method, class method,
// @interface, @implementation with different name, and plain C function)
// plus message send call sites and Stop-listed keywords.
// Negative lines that must NOT mint nodes: @end, commented-out methods,
// and C function prototypes.
var objcSrc = []byte(`@interface ViewController : UIViewController
- (void)viewDidLoad {
  [self helperMethod];
}

- (void)helperMethod {
  return;
}

+ (instancetype)sharedInstance {
  return nil;
}
@end

@implementation AppDelegate

int helper(int x) {
  return x + 1;
}

static void initialize(void) {
  helper(42);
  [obj retain];
}

@end

// - (void)commented {
// }

int prototype(int x);
`)

func TestObjcSpecLangExtensions(t *testing.T) {
	p := SpecParser{S: objcSpec}
	if p.Lang() != graph.LangObjC {
		t.Errorf("Lang() = %v, want %v", p.Lang(), graph.LangObjC)
	}
	if got, want := p.Extensions(), []string{".m", ".mm"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Extensions() = %v, want %v", got, want)
	}
}

func TestObjcSpecNodes(t *testing.T) {
	p := SpecParser{S: objcSpec}
	pr, err := p.Parse("app/ViewController.m", objcSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)

	want := map[graph.NodeID]struct {
		Kind    graph.NodeKind
		Line    int
		EndLine int
	}{
		"objc:ViewController.ViewController": {graph.KType, 1, 1},
		"objc:ViewController.viewDidLoad":    {graph.KMethod, 2, 4},
		"objc:ViewController.helperMethod":   {graph.KMethod, 6, 8},
		"objc:ViewController.sharedInstance": {graph.KMethod, 10, 12},
		"objc:ViewController.AppDelegate":    {graph.KType, 15, 15},
		"objc:ViewController.helper":         {graph.KFunc, 17, 19},
		"objc:ViewController.initialize":     {graph.KFunc, 21, 24},
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
		if n.Line != w.Line {
			t.Errorf("%s Line = %d, want %d", id, n.Line, w.Line)
		}
		if n.EndLine != w.EndLine {
			t.Errorf("%s EndLine = %d, want %d", id, n.EndLine, w.EndLine)
		}
		if n.Lang != graph.LangObjC {
			t.Errorf("%s Lang = %v, want objc", id, n.Lang)
		}
	}
	if sig := byID["objc:ViewController.helper"].Sig; sig != "int x" {
		t.Errorf("helper Sig = %q, want \"int x\"", sig)
	}
	if sig := byID["objc:ViewController.initialize"].Sig; sig != "void" {
		t.Errorf("initialize Sig = %q, want \"void\"", sig)
	}
}

// TestObjcSpecMessageSendEdges verifies that message send call sites generate
// ECall edges. It asserts the expected edge from viewDidLoad to helperMethod
// via [self helperMethod], that Stop-listed retain is not in edges, and that
// no self-recursion edges are created.
func TestObjcSpecMessageSendEdges(t *testing.T) {
	p := SpecParser{S: objcSpec}
	pr, err := p.Parse("app/ViewController.m", objcSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	// Expected edge: viewDidLoad calls helperMethod via [self helperMethod]
	wantSrc := graph.NodeID("objc:ViewController.viewDidLoad")
	wantDst := graph.NodeID("objc:ViewController.helperMethod")
	wantKind := graph.ECall

	found := false
	for _, e := range pr.Edges {
		if e.Src == wantSrc && e.Dst == wantDst && e.Kind == wantKind {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected edge %s -> %s (ECall), got edges: %+v", wantSrc, wantDst, pr.Edges)
	}

	// Assert no edge for Stop-listed 'retain'
	for _, e := range pr.Edges {
		if strings.Contains(string(e.Dst), ".retain") {
			t.Errorf("unexpected edge for Stop-listed 'retain': %+v", e)
		}
	}

	// Assert no self-recursion edges (no caller calling itself)
	for _, e := range pr.Edges {
		if e.Src == e.Dst {
			t.Errorf("unexpected self-recursion edge: %+v", e)
		}
	}
}

// TestObjcSpecNegativeLines pins that @end, commented-out methods,
// and C function prototypes mint nothing.
func TestObjcSpecNegativeLines(t *testing.T) {
	p := SpecParser{S: objcSpec}
	pr, err := p.Parse("neg.m", []byte(`@end
[super viewDidLoad];
[obj message];
// - (void)commented {
// }
int prototype(int x);
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(pr.Nodes) != 0 {
		t.Fatalf("got %d nodes, want 0: %+v", len(pr.Nodes), pr.Nodes)
	}
}

// objcT0122Src is R-0005's gap-objc fixture (scratchpad/gap-objc/Sample.m),
// copied verbatim as the regression input for T-0122's objc.go hardening:
// a @protocol declaration, a category, a single-line method
// declaration/definition pair (viewDidLoad, in both @interface and
// @implementation), dot-syntax and nested/chained message sends, and a
// plain C function calling another plain C function.
var objcT0122Src = []byte(`@protocol SomeProtocol
- (void)protocolMethod;
@end

@interface Foo : NSObject <SomeProtocol>
- (void)viewDidLoad;
@end

@interface Foo (Helpers)
- (void)addHelperSubview;
@end

@implementation Foo

- (void)viewDidLoad {
    [self.tableView reloadData];
    [[self view] addSubview:self.tableView];
}

@end

@implementation Foo (Helpers)

- (void)addHelperSubview {
    [self viewDidLoad];
}

@end

int add(int a, int b) {
    return a + b;
}

int helperMultiply(int a, int b) {
    int sum = add(a, b);
    return sum * 2;
}
`)

// TestObjcSpecT0122Protocol covers R-0005 objc.md's [high] "@protocol
// declarations are not matched by any Def pattern" finding.
func TestObjcSpecT0122Protocol(t *testing.T) {
	p := SpecParser{S: objcSpec}
	pr, err := p.Parse("Sample.m", objcT0122Src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)
	n, ok := byID["objc:Sample.SomeProtocol"]
	if !ok {
		t.Fatalf("node objc:Sample.SomeProtocol missing, got %+v", pr.Nodes)
	}
	if n.Kind != graph.KType {
		t.Errorf("objc:Sample.SomeProtocol Kind = %v, want KType", n.Kind)
	}
	if n.Line != 1 {
		t.Errorf("objc:Sample.SomeProtocol Line = %d, want 1", n.Line)
	}
}

// TestObjcSpecT0122AllmanCFunction covers R-0005 objc.md's [high] "C
// function definition with Allman-style brace" finding, using the
// gap-objc2/Min.m minimized confirmation fixture.
func TestObjcSpecT0122AllmanCFunction(t *testing.T) {
	p := SpecParser{S: objcSpec}
	src := []byte("void plainAllman(int x)\n{\n  NSLog(@\"%d\", x);\n}\n")
	pr, err := p.Parse("Min.m", src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)
	n, ok := byID["objc:Min.plainAllman"]
	if !ok {
		t.Fatalf("node objc:Min.plainAllman missing, got %+v", pr.Nodes)
	}
	if n.Kind != graph.KFunc {
		t.Errorf("objc:Min.plainAllman Kind = %v, want KFunc", n.Kind)
	}
	if n.Line != 1 || n.EndLine != 4 {
		t.Errorf("objc:Min.plainAllman Line/EndLine = %d/%d, want 1/4 (Allman body scanned)", n.Line, n.EndLine)
	}
}

// TestObjcSpecT0122CategoryDistinctFromBaseClass covers R-0005 objc.md's
// [medium] "Category syntax ... captured by the same KType regex as the
// base class" finding: `@interface Foo (Helpers)` must mint a name distinct
// from the base class's own "Foo" node, not a fourth indistinguishable
// "Foo".
func TestObjcSpecT0122CategoryDistinctFromBaseClass(t *testing.T) {
	p := SpecParser{S: objcSpec}
	pr, err := p.Parse("Sample.m", objcT0122Src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)
	if _, ok := byID["objc:Sample.Foo"]; !ok {
		t.Fatalf("node objc:Sample.Foo (base class) missing, got %+v", pr.Nodes)
	}
	foundCategory := false
	for id, n := range byID {
		if id != "objc:Sample.Foo" && n.Kind == graph.KType && strings.HasPrefix(string(id), "objc:Sample.Foo") {
			foundCategory = true
		}
	}
	if !foundCategory {
		t.Errorf("no distinct category KType node found (want something like objc:Sample.Foo (Helpers)), got %+v", pr.Nodes)
	}
}

// TestObjcSpecT0122SingleLineDeclarationVsDefinition covers R-0005
// objc.md's [high] "@interface/@protocol method prototypes ... matched by
// the same KMethod Def regex as real definitions" finding for the
// single-line case (the common shape): viewDidLoad is declared with `;` in
// the @interface and defined with a body in the @implementation — only the
// real, located definition should exist as a node, and only it should be a
// valid ECall edge target.
//
// NOT FULLY FIXED (documented, see objc.go's KMethod Def comment and
// T-0122's final report): a *multi-line* selector's declaration and
// definition share an identical first physical line (the terminating `;`
// or `{` only appears on a later line), which a per-line regex cannot
// distinguish without cross-line state; that specific shape (R-0005
// objc.md's exact `configureWithName:...age:...` example) still mints a
// phantom node from the declaration, same as before this task.
func TestObjcSpecT0122SingleLineDeclarationVsDefinition(t *testing.T) {
	p := SpecParser{S: objcSpec}
	pr, err := p.Parse("Sample.m", objcT0122Src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)
	n, ok := byID["objc:Sample.viewDidLoad"]
	if !ok {
		t.Fatalf("node objc:Sample.viewDidLoad missing, got %+v", pr.Nodes)
	}
	// Only ONE viewDidLoad-named node total: the @interface's `- (void)
	// viewDidLoad;` (line 6) must not also mint a phantom.
	count := 0
	for _, nd := range pr.Nodes {
		if nd.ID == "objc:Sample.viewDidLoad" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("got %d objc:Sample.viewDidLoad nodes, want exactly 1 (the @implementation definition, not the @interface declaration)", count)
	}
	if n.Line != 15 {
		t.Errorf("objc:Sample.viewDidLoad Line = %d, want 15 (the real @implementation body, not line 6's declaration)", n.Line)
	}
}

// TestObjcSpecT0122DotSyntaxAndNestedSendEdges covers R-0005 objc.md's
// [high] "Message send whose receiver uses property dot-syntax" and
// [medium] "Nested/chained message sends" findings.
func TestObjcSpecT0122DotSyntaxAndNestedSendEdges(t *testing.T) {
	p := SpecParser{S: objcSpec}
	pr, err := p.Parse("Sample.m", objcT0122Src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := map[[2]string]bool{
		{"objc:Sample.viewDidLoad", "objc:Sample.reloadData"}: false, // dot-syntax receiver
		{"objc:Sample.viewDidLoad", "objc:Sample.addSubview"}: false, // outer of a nested send
	}
	for _, e := range pr.Edges {
		key := [2]string{string(e.Src), string(e.Dst)}
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

// TestObjcSpecT0122PlainCFunctionCallEdge covers R-0005 objc.md's [high]
// "Plain C function calling another plain C function (no bracket syntax)
// never produces a call edge" finding.
func TestObjcSpecT0122PlainCFunctionCallEdge(t *testing.T) {
	p := SpecParser{S: objcSpec}
	pr, err := p.Parse("Sample.m", objcT0122Src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	found := false
	for _, e := range pr.Edges {
		if e.Src == "objc:Sample.helperMultiply" && e.Dst == "objc:Sample.add" && e.Kind == graph.ECall {
			found = true
		}
	}
	if !found {
		t.Errorf("missing edge objc:Sample.helperMultiply -> objc:Sample.add, got edges %+v", pr.Edges)
	}
}

func TestObjcSpecRegisteredInAll(t *testing.T) {
	found := false
	for _, p := range All() {
		if p.Lang() == graph.LangObjC {
			found = true
		}
	}
	if !found {
		t.Error("All() does not contain a parser for graph.LangObjC — objcSpec not registered via init()")
	}
}

func TestObjcSpecDeterministic(t *testing.T) {
	p := SpecParser{S: objcSpec}
	pr1, err := p.Parse("app/ViewController.m", objcSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	pr2, err := p.Parse("app/ViewController.m", objcSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !reflect.DeepEqual(pr1, pr2) {
		t.Errorf("Parse is not deterministic:\n%+v\nvs\n%+v", pr1, pr2)
	}
}

// TestObjcSpecIndexAllE2E proves the full pipeline: a real .m file on
// disk through index.New + IndexAll mints the objc:<stem>.<name> nodes.
func TestObjcSpecIndexAllE2E(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "ViewController.m"), objcSrc, 0o644); err != nil {
		t.Fatal(err)
	}

	g := graph.NewMem()
	parsers := append([]index.LanguageParser{index.GoParser{}}, All()...)
	idx := index.New(g, store.NewMem(), parsers, resolve.Default().All())
	if _, err := idx.IndexAll(context.Background(), root); err != nil {
		t.Fatalf("IndexAll(%s): %v", root, err)
	}

	for id, kind := range map[graph.NodeID]graph.NodeKind{
		"objc:ViewController.ViewController": graph.KType,
		"objc:ViewController.viewDidLoad":    graph.KMethod,
		"objc:ViewController.helper":         graph.KFunc,
	} {
		n, ok := g.Node(id)
		if !ok {
			t.Fatalf("node %s missing after IndexAll", id)
		}
		if n.Kind != kind || n.Lang != graph.LangObjC {
			t.Errorf("%s = %+v, want Kind=%v Lang=objc", id, n, kind)
		}
	}
}
