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

// metalSrc exercises all three metalSpec Defs (kernel, vertex, fragment,
// and plain helper function) plus negative lines that must NOT mint nodes:
// function prototypes (ending with `;`), commented definitions, call sites.
var metalSrc = []byte(`#include <metal_stdlib>
using namespace metal;

kernel void add_arrays(device const float* a [[buffer(0)]],
                       device float* result [[buffer(1)]],
                       uint id [[thread_position_in_grid]]) {
  result[id] = a[id] * 2.0;
}

vertex float4 vertexShader(uint vertexID [[vertex_id]]) {
  return float4(vertexID, 0.0, 0.0, 1.0);
}

fragment half4 fragmentShader(float4 pos [[position]]) {
  return half4(pos.x, pos.y, 0.0, 1.0);
}

static inline float3 normalize_vector(float3 v) {
  return v / length(v);
}

void proto(float x);
// kernel void commented(uint id [[thread_id]]) {
/* fragment half4 multiline_comment(float2 uv) { */
`)

func TestMetalSpecLangExtensions(t *testing.T) {
	p := SpecParser{S: metalSpec}
	if p.Lang() != graph.LangMSL {
		t.Errorf("Lang() = %v, want %v", p.Lang(), graph.LangMSL)
	}
	if got, want := p.Extensions(), []string{".metal"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Extensions() = %v, want %v", got, want)
	}
}

func TestMetalSpecNodes(t *testing.T) {
	p := SpecParser{S: metalSpec}
	pr, err := p.Parse("shader.metal", metalSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)

	want := map[graph.NodeID]struct {
		Kind graph.NodeKind
		Line int
	}{
		"msl:add_arrays":      {graph.KKernel, 4},
		"msl:vertexShader":    {graph.KKernel, 10},
		"msl:fragmentShader":  {graph.KKernel, 14},
		"msl:normalize_vector": {graph.KFunc, 18},
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
		if n.Lang != graph.LangMSL {
			t.Errorf("%s Lang = %v, want msl", id, n.Lang)
		}
	}
	// Verify signature capture for a simpler function
	if sig := byID["msl:normalize_vector"].Sig; sig != "float3 v" {
		t.Errorf("normalize_vector Sig = %q, want \"float3 v\"", sig)
	}
}

// TestMetalSpecNegativeLines pins that prototypes (ending with `;`),
// commented definitions, and return statements mint nothing.
func TestMetalSpecNegativeLines(t *testing.T) {
	p := SpecParser{S: metalSpec}
	pr, err := p.Parse("neg.metal", []byte(`void proto(float x);
float4 another_proto(float2 uv);
// kernel void commented(uint id [[thread_id]]) {
/* fragment half4 multiline_comment(float2 uv) { */
  return pos;
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(pr.Nodes) != 0 {
		t.Fatalf("got %d nodes, want 0: %+v", len(pr.Nodes), pr.Nodes)
	}
}

func TestMetalSpecRegisteredInAll(t *testing.T) {
	found := false
	for _, p := range All() {
		if p.Lang() == graph.LangMSL {
			found = true
		}
	}
	if !found {
		t.Error("All() does not contain a parser for graph.LangMSL — metalSpec not registered via init()")
	}
}

func TestMetalSpecDeterministic(t *testing.T) {
	p := SpecParser{S: metalSpec}
	pr1, err := p.Parse("shader.metal", metalSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	pr2, err := p.Parse("shader.metal", metalSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !reflect.DeepEqual(pr1, pr2) {
		t.Errorf("Parse is not deterministic:\n%+v\nvs\n%+v", pr1, pr2)
	}
}

// TestMetalSpecIndexAllE2E proves the full pipeline: a real .metal file on
// disk through index.New + IndexAll mints the msl:<name> nodes.
func TestMetalSpecIndexAllE2E(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "shader.metal"), metalSrc, 0o644); err != nil {
		t.Fatal(err)
	}

	g := graph.NewMem()
	parsers := append([]index.LanguageParser{index.GoParser{}}, All()...)
	idx := index.New(g, store.NewMem(), parsers, resolve.Default().All())
	if _, err := idx.IndexAll(context.Background(), root); err != nil {
		t.Fatalf("IndexAll(%s): %v", root, err)
	}

	for id, kind := range map[graph.NodeID]graph.NodeKind{
		"msl:add_arrays":      graph.KKernel,
		"msl:vertexShader":    graph.KKernel,
		"msl:fragmentShader":  graph.KKernel,
		"msl:normalize_vector": graph.KFunc,
	} {
		n, ok := g.Node(id)
		if !ok {
			t.Fatalf("node %s missing after IndexAll", id)
		}
		if n.Kind != kind || n.Lang != graph.LangMSL {
			t.Errorf("%s = %+v, want Kind=%v Lang=msl", id, n, kind)
		}
	}
}
