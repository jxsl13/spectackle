package langspec

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/jxsl13/spectackle/internal/graph"
	"github.com/jxsl13/spectackle/internal/index"
	"github.com/jxsl13/spectackle/internal/resolve"
	"github.com/jxsl13/spectackle/internal/store"
)

// objcSrc exercises all three objcSpec Defs (instance method, class method,
// @interface, @implementation with different name, and plain C function)
// plus negative lines that must NOT mint nodes: @end, message send call sites,
// commented-out methods, and C function prototypes.
var objcSrc = []byte(`@interface ViewController : UIViewController
- (void)viewDidLoad {
  [super viewDidLoad];
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
}

@end

// - (void)commented {
// }

int prototype(int x);

[obj message];
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
		Kind graph.NodeKind
		Line int
	}{
		"objc:ViewController.ViewController": {graph.KType, 1},
		"objc:ViewController.viewDidLoad":    {graph.KMethod, 2},
		"objc:ViewController.sharedInstance": {graph.KMethod, 6},
		"objc:ViewController.AppDelegate":    {graph.KType, 11},
		"objc:ViewController.helper":         {graph.KFunc, 13},
		"objc:ViewController.initialize":     {graph.KFunc, 17},
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

// TestObjcSpecNegativeLines pins that @end, message send call sites
// `[obj message];`, commented-out methods, and C function prototypes
// mint nothing.
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
