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

// metalT0122Src is R-0005's gap-metal fixture (scratchpad/gap-metal/
// shader.metal), copied verbatim as the regression input for T-0122's
// metal.go hardening: a multi-line kernel signature whose first physical
// line has no closing paren at all, an Allman-style helper function, a
// generic/multi-token return type, and a chain of KFunc-to-KFunc calls.
var metalT0122Src = []byte(`kernel void scale_kernel(uint2 gid [[thread_position_in_grid]],
                          device float* data [[buffer(0)]],
                          constant float& factor [[buffer(1)]])
{
    data[gid.x] = data[gid.x] * factor;
}

static inline float3 computeNormal(float3 a, float3 b, float3 c) {
    return normalize(cross(b - a, c - a));
}

float3 computeTangent(float3 a, float3 b, float2 uva, float2 uvb)
{
    return normalize(a * uvb.y - b * uva.y);
}

array<float, 4> packColor(float4 c) {
    array<float, 4> out = {c.x, c.y, c.z, c.w};
    return out;
}

kernel void shade_pixels(device float3* colors [[buffer(0)]],
                          uint id [[thread_position_in_grid]]) {
    float3 n = computeNormal(colors[id], colors[id], colors[id]);
    colors[id] = normalize_vector(n);
}
`)

// TestMetalSpecT0122MultiLineKernelSignatureNoParenOnFirstLine covers
// R-0005 metal.md's [high] "multi-line kernel/vertex/fragment signature
// whose first physical line has no closing paren anywhere before EOL"
// finding (scale_kernel: the total-miss case) plus its Allman brace.
func TestMetalSpecT0122MultiLineKernelSignatureNoParenOnFirstLine(t *testing.T) {
	p := SpecParser{S: metalSpec}
	pr, err := p.Parse("shader.metal", metalT0122Src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)
	n, ok := byID["msl:scale_kernel"]
	if !ok {
		t.Fatalf("node msl:scale_kernel missing, got %+v", pr.Nodes)
	}
	if n.Kind != graph.KKernel {
		t.Errorf("msl:scale_kernel Kind = %v, want KKernel", n.Kind)
	}
	if n.Line != 1 {
		t.Errorf("msl:scale_kernel Line = %d, want 1", n.Line)
	}
}

// TestMetalSpecT0122AllmanHelperAndGenericReturnType covers R-0005
// metal.md's [high] "Allman-style brace ... for a plain helper function"
// finding (computeTangent) and [high] "return type is a multi-token
// generic/container type" finding (packColor: `array<float, 4>`).
func TestMetalSpecT0122AllmanHelperAndGenericReturnType(t *testing.T) {
	p := SpecParser{S: metalSpec}
	pr, err := p.Parse("shader.metal", metalT0122Src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)

	n, ok := byID["msl:computeTangent"]
	if !ok {
		t.Fatalf("node msl:computeTangent missing, got %+v", pr.Nodes)
	}
	if n.Kind != graph.KFunc {
		t.Errorf("msl:computeTangent Kind = %v, want KFunc", n.Kind)
	}
	if n.Line != 12 || n.EndLine != 15 {
		t.Errorf("msl:computeTangent Line/EndLine = %d/%d, want 12/15 (Allman body scanned)", n.Line, n.EndLine)
	}

	if n, ok := byID["msl:packColor"]; !ok {
		t.Fatalf("node msl:packColor missing, got %+v", pr.Nodes)
	} else if n.Kind != graph.KFunc {
		t.Errorf("msl:packColor Kind = %v, want KFunc", n.Kind)
	}
}

// TestMetalSpecT0122SingleLineTemplate covers R-0005 metal.md's [high]
// "C++ function template declared on a single physical line" finding,
// using the gap-metal/probe.metal minimized confirmation fixture.
func TestMetalSpecT0122SingleLineTemplate(t *testing.T) {
	p := SpecParser{S: metalSpec}
	src := []byte("template <typename T> T maxval(T a, T b) { return a > b ? a : b; }\n")
	pr, err := p.Parse("probe.metal", src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)
	n, ok := byID["msl:maxval"]
	if !ok {
		t.Fatalf("node msl:maxval missing, got %+v", pr.Nodes)
	}
	if n.Kind != graph.KFunc {
		t.Errorf("msl:maxval Kind = %v, want KFunc", n.Kind)
	}
	if sig := n.Sig; sig != "T a, T b" {
		t.Errorf("msl:maxval Sig = %q, want \"T a, T b\"", sig)
	}
}

// TestMetalSpecT0122CallEdges covers R-0005 metal.md's [high] "intra-file
// calls between Metal functions/kernels never become call edges at all"
// finding for KFunc-to-KFunc calls (metalSpec.CallRe was nil before
// T-0122, so SpecParser never brace-counted a body or scanned it for calls
// for any Metal def, of any Kind).
//
// NOT FULLY FIXED for calls whose *source* is a kernel/vertex/fragment
// entry point (graph.KKernel), e.g. shade_pixels calling computeNormal and
// normalize_vector in this same fixture: internal/langspec/langspec.go's
// Parse only invokes cspan.Span + callEdges when `def.Kind == graph.KFunc
// || def.Kind == graph.KMethod` — KKernel is structurally excluded from
// that gate. Widening that gate is a langspec.go framework change outside
// metal.go's lease (and outside this task's "brace-style Def fixes only"
// scope), so a kernel's own body is still never scanned for calls; only
// calls between plain KFunc helpers are fixed here. See T-0122's final
// report.
func TestMetalSpecT0122CallEdges(t *testing.T) {
	p := SpecParser{S: metalSpec}
	pr, err := p.Parse("shader.metal", metalT0122Src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := map[[2]string]bool{
		{"msl:computeNormal", "msl:normalize"}:  false,
		{"msl:computeNormal", "msl:cross"}:      false,
		{"msl:computeTangent", "msl:normalize"}: false,
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
	// shade_pixels is a KKernel: per the doc comment above, its body is not
	// scanned, so it must have zero outgoing edges (documenting the known
	// gap rather than silently letting a future accidental fix go
	// unnoticed either way).
	for _, e := range pr.Edges {
		if e.Src == "msl:shade_pixels" {
			t.Errorf("unexpected edge from msl:shade_pixels (KKernel bodies are not scanned by the current framework): %+v", e)
		}
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
