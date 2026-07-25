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

// glslT0122Src is R-0005's gap-glsl fixture (scratchpad/gap-glsl/
// shader.comp), copied verbatim as the regression input for T-0122's
// glsl.go hardening: struct declarations, a multi-line function signature,
// and a caller chain to probe call-edge extraction.
var glslT0122Src = []byte(`struct Material {
    vec3 albedo;
    float roughness;
};

struct Light {
    vec3 position;
    vec3 color;
};

vec3 computeLighting(
    vec3 normal,
    vec3 lightDir,
    vec3 lightColor
) {
    float ndotl = max(dot(normal, lightDir), 0.0);
    return lightColor * ndotl;
}

float luminance(vec3 c) {
    return dot(c, vec3(0.2126, 0.7152, 0.0722));
}

vec3 shade(Material m, Light l) {
    vec3 lighting = computeLighting(vec3(0.0, 1.0, 0.0), l.position, l.color);
    float lum = luminance(m.albedo);
    return lighting * lum;
}
`)

// TestGlslSpecT0122Structs covers R-0005 glsl.md's [high] "struct
// declarations" finding: GLSL's only named-type construct was entirely
// absent from glslSpec's Defs before T-0122.
func TestGlslSpecT0122Structs(t *testing.T) {
	p := SpecParser{S: glslSpec}
	pr, err := p.Parse("shader.comp", glslT0122Src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)
	for _, id := range []graph.NodeID{"glsl:Material", "glsl:Light"} {
		n, ok := byID[id]
		if !ok {
			t.Fatalf("node %s missing, got %+v", id, pr.Nodes)
		}
		if n.Kind != graph.KType {
			t.Errorf("%s Kind = %v, want KType", id, n.Kind)
		}
	}
}

// TestGlslSpecT0122MultiLineSignature covers R-0005 glsl.md's [high]
// "multi-line function signature" finding (computeLighting: params wrapped
// across lines before the opening brace) and its [medium] "function body
// end line" consequence (EndLine used to always equal Line).
func TestGlslSpecT0122MultiLineSignature(t *testing.T) {
	p := SpecParser{S: glslSpec}
	pr, err := p.Parse("shader.comp", glslT0122Src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)
	n, ok := byID["glsl:computeLighting"]
	if !ok {
		t.Fatalf("node glsl:computeLighting missing, got %+v", pr.Nodes)
	}
	if n.Kind != graph.KFunc {
		t.Errorf("glsl:computeLighting Kind = %v, want KFunc", n.Kind)
	}
	if n.Line != 11 || n.EndLine != 18 {
		t.Errorf("glsl:computeLighting Line/EndLine = %d/%d, want 11/18", n.Line, n.EndLine)
	}
}

// TestGlslSpecT0122CallEdges covers R-0005 glsl.md's [high] "call edges
// between functions" finding: glslSpec.CallRe was nil before T-0122, so
// SpecParser.callEdges was never invoked for GLSL at all, for any
// construct.
func TestGlslSpecT0122CallEdges(t *testing.T) {
	p := SpecParser{S: glslSpec}
	pr, err := p.Parse("shader.comp", glslT0122Src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := map[[2]string]bool{
		{"glsl:shade", "glsl:computeLighting"}: false,
		{"glsl:shade", "glsl:luminance"}:       false,
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
