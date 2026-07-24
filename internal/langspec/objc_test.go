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
