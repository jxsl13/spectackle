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

// glslSrc exercises all glslSpec Defs (functions) plus negative lines that
// must NOT mint nodes: function prototypes (ending with `;`), commented
// definitions, control flow, and layout qualifiers.
var glslSrc = []byte(`#version 450

layout(local_size_x = 8) in;

void main() {
  vec4 color = compute(vec3(1.0));
}

float helper(float x) {
  return x * 2.0;
}

vec3 compute(vec3 a, vec3 b) {
  return a + b;
}

float proto(float x);
// void commented() {
/* vec3 multiline_comment() { */
if (x > 0) {
  return a + b;
}
`)

func TestGlslSpecLangExtensions(t *testing.T) {
	p := SpecParser{S: glslSpec}
	if p.Lang() != graph.LangGLSL {
		t.Errorf("Lang() = %v, want %v", p.Lang(), graph.LangGLSL)
	}
	want := []string{".comp", ".vert", ".frag", ".geom", ".tesc", ".tese", ".glsl"}
	if got := p.Extensions(); !reflect.DeepEqual(got, want) {
		t.Errorf("Extensions() = %v, want %v", got, want)
	}
}

func TestGlslSpecNodes(t *testing.T) {
	p := SpecParser{S: glslSpec}
	pr, err := p.Parse("shader.glsl", glslSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)

	want := map[graph.NodeID]struct {
		Kind graph.NodeKind
		Line int
	}{
		"glsl:main":    {graph.KFunc, 5},
		"glsl:helper":  {graph.KFunc, 9},
		"glsl:compute": {graph.KFunc, 13},
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
		if n.Lang != graph.LangGLSL {
			t.Errorf("%s Lang = %v, want glsl", id, n.Lang)
		}
	}
	// Verify signature capture for a simpler function
	if sig := byID["glsl:helper"].Sig; sig != "float x" {
		t.Errorf("helper Sig = %q, want \"float x\"", sig)
	}
}

// TestGlslSpecNegativeLines pins that prototypes (ending with `;`),
// commented definitions, control flow, and layout qualifiers mint nothing.
func TestGlslSpecNegativeLines(t *testing.T) {
	p := SpecParser{S: glslSpec}
	pr, err := p.Parse("neg.glsl", []byte(`layout(local_size_x=64) in;
float proto(float x);
// void commented() {
/* vec3 multiline_comment() { */
if (x > 0) {
  return a + b;
}
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(pr.Nodes) != 0 {
		t.Fatalf("got %d nodes, want 0: %+v", len(pr.Nodes), pr.Nodes)
	}
}

func TestGlslSpecRegisteredInAll(t *testing.T) {
	found := false
	for _, p := range All() {
		if p.Lang() == graph.LangGLSL {
			found = true
		}
	}
	if !found {
		t.Error("All() does not contain a parser for graph.LangGLSL — glslSpec not registered via init()")
	}
}

func TestGlslSpecDeterministic(t *testing.T) {
	p := SpecParser{S: glslSpec}
	pr1, err := p.Parse("shader.glsl", glslSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	pr2, err := p.Parse("shader.glsl", glslSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !reflect.DeepEqual(pr1, pr2) {
		t.Errorf("Parse is not deterministic:\n%+v\nvs\n%+v", pr1, pr2)
	}
}

// TestGlslSpecIndexAllE2E proves the full pipeline: a real .comp file on
// disk through index.New + IndexAll mints the glsl:<name> nodes.
func TestGlslSpecIndexAllE2E(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "shader.comp"), glslSrc, 0o644); err != nil {
		t.Fatal(err)
	}

	g := graph.NewMem()
	parsers := append([]index.LanguageParser{index.GoParser{}}, All()...)
	idx := index.New(g, store.NewMem(), parsers, resolve.Default().All())
	if _, err := idx.IndexAll(context.Background(), root); err != nil {
		t.Fatalf("IndexAll(%s): %v", root, err)
	}

	for id, kind := range map[graph.NodeID]graph.NodeKind{
		"glsl:main":    graph.KFunc,
		"glsl:helper":  graph.KFunc,
		"glsl:compute": graph.KFunc,
	} {
		n, ok := g.Node(id)
		if !ok {
			t.Fatalf("node %s missing after IndexAll", id)
		}
		if n.Kind != kind || n.Lang != graph.LangGLSL {
			t.Errorf("%s = %+v, want Kind=%v Lang=glsl", id, n, kind)
		}
	}
}
