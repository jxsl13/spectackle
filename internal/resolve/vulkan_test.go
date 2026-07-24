package resolve

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/jxsl13/spectackle/internal/graph"
)

// vulkanFS is a tiny in-memory FileSet exposing only .c files (host code),
// mirroring gpupipeFS's shape in gpupipe_test.go.
type vulkanFS struct {
	files map[string]string
}

func (f vulkanFS) ByLang(l graph.Lang) []string {
	if l != graph.LangC {
		return nil
	}
	var paths []string
	for p := range f.files {
		paths = append(paths, p)
	}
	return paths
}

func (f vulkanFS) Read(path string) ([]byte, error) {
	src, ok := f.files[path]
	if !ok {
		return nil, errors.New("vulkanFS: no such file")
	}
	return []byte(src), nil
}

// resolveVulkan runs VulkanResolver over g and fs and fails the test on
// error.
func resolveVulkan(t *testing.T, g graph.Graph, fs FileSet) []graph.Edge {
	t.Helper()
	edges, err := (VulkanResolver{}).Resolve(context.Background(), g, fs)
	if err != nil {
		t.Fatalf("VulkanResolver.Resolve returned error: %v", err)
	}
	return edges
}

// vkGlslNode builds a glsl: node the way the real GLSL parser (QualFlat,
// see internal/langspec/glsl.go) would: id must already carry any "~N"
// collision suffix the indexer would have assigned.
func vkGlslNode(id graph.NodeID, file string, line int) graph.Node {
	return graph.Node{ID: id, Kind: graph.KFunc, Lang: graph.LangGLSL, File: file, Line: line}
}

// lineOfVulkan returns the 1-indexed line number of a substring in src, for
// test assertions.
func lineOfVulkan(t *testing.T, src, substr string) int {
	t.Helper()
	i := strings.Index(src, substr)
	if i < 0 {
		t.Fatalf("substring %q not found in source", substr)
	}
	return 1 + strings.Count(src[:i], "\n")
}

// blurComp / sharpenComp are fixture .comp sources. VulkanResolver never
// reads GLSL files directly (Resolve only scans LangC/LangCpp/LangGo,
// see vulkan.go); the graph node a real indexing run would mint from these
// is supplied manually via vkGlslNode, matching how the task instructs the
// tests to be built ("upserted into a graph.NewMem()").
const blurComp = `#version 450
layout(local_size_x = 8) in;
void main() {
  vec4 c = imageLoad(img, ivec2(0, 0));
}
`

const sharpenComp = `#version 450
layout(local_size_x = 8) in;
void main() {
  vec4 c = imageLoad(img, ivec2(0, 0));
}
`

// hostSingle is a C host file that carries the full BLOB -> MODULE -> STAGE
// provenance chain for one shader, binding "shaders/blur.comp.spv" through
// vkCreateShaderModule to pName "main".
const hostSingle = `#include <vulkan/vulkan.h>

void loadBlur(VkDevice device) {
    const char *path = "shaders/blur.comp.spv";
    VkShaderModuleCreateInfo createInfo = {};
    createInfo.pCode = (uint32_t*)path;
    VkShaderModule shaderModule;
    vkCreateShaderModule(device, &createInfo, NULL, &shaderModule);

    VkPipelineShaderStageCreateInfo stageInfo = {
        .module = shaderModule,
        .pName = "main",
    };
}
`

// TestVulkanResolverHappyPath is test 1: one shader, one host function
// carrying the full BLOB -> MODULE -> STAGE chain, one ELaunch edge.
func TestVulkanResolverHappyPath(t *testing.T) {
	g := graph.NewMem()
	g.Upsert([]graph.Node{vkGlslNode("glsl:main", "blur.comp", 3)}, nil)

	fs := vulkanFS{files: map[string]string{"host.c": hostSingle}}
	edges := resolveVulkan(t, g, fs)

	if len(edges) != 1 {
		t.Fatalf("want exactly 1 ELaunch edge, got %d: %+v", len(edges), edges)
	}
	e := edges[0]
	if e.Src != "c:loadBlur" {
		t.Errorf("edge src = %q, want c:loadBlur", e.Src)
	}
	if e.Dst != "glsl:main" {
		t.Errorf("edge dst = %q, want glsl:main", e.Dst)
	}
	if e.Kind != graph.ELaunch {
		t.Errorf("edge kind = %v, want ELaunch", e.Kind)
	}
	if e.File != "host.c" {
		t.Errorf("edge file = %q, want host.c", e.File)
	}
	if want := lineOfVulkan(t, hostSingle, "VkPipelineShaderStageCreateInfo stageInfo"); e.Line != want {
		t.Errorf("edge line = %d, want %d", e.Line, want)
	}
}

// TestVulkanResolverCollisionDisambiguation is test 2, THE regression proof.
// Two shaders both define `void main()`. GLSL is QualFlat (see
// internal/langspec/glsl.go), so the real indexer mints "glsl:main" for the
// first-indexed shader in file-path order and "glsl:main~2" for the second
// ("blur.comp" < "sharpen.comp" lexically). Two host functions each bind one
// shader by its own .spv path. An implementation that targeted edges with
// ids.Mint("glsl", "main") instead of a graph.Find lookup would wire BOTH
// launches to the same "glsl:main" node — indistinguishable from correct
// behavior on every other test in this file, but wrong here.
func TestVulkanResolverCollisionDisambiguation(t *testing.T) {
	g := graph.NewMem()
	g.Upsert([]graph.Node{
		vkGlslNode("glsl:main", "blur.comp", 3),
		vkGlslNode("glsl:main~2", "sharpen.comp", 3),
	}, nil)

	src := `#include <vulkan/vulkan.h>

void loadBlur(VkDevice device) {
    const char *path = "shaders/blur.comp.spv";
    VkShaderModuleCreateInfo createInfo = {};
    createInfo.pCode = (uint32_t*)path;
    VkShaderModule shaderModule;
    vkCreateShaderModule(device, &createInfo, NULL, &shaderModule);

    VkPipelineShaderStageCreateInfo stageInfo = {
        .module = shaderModule,
        .pName = "main",
    };
}

void loadSharpen(VkDevice device) {
    const char *path = "shaders/sharpen.comp.spv";
    VkShaderModuleCreateInfo createInfo = {};
    createInfo.pCode = (uint32_t*)path;
    VkShaderModule shaderModule;
    vkCreateShaderModule(device, &createInfo, NULL, &shaderModule);

    VkPipelineShaderStageCreateInfo stageInfo = {
        .module = shaderModule,
        .pName = "main",
    };
}
`
	fs := vulkanFS{files: map[string]string{"host.c": src}}
	edges := resolveVulkan(t, g, fs)

	if len(edges) != 2 {
		t.Fatalf("want exactly 2 ELaunch edges, got %d: %+v", len(edges), edges)
	}

	got := map[string]graph.NodeID{}
	for _, e := range edges {
		got[string(e.Src)] = e.Dst
	}

	blurDst, ok := got["c:loadBlur"]
	if !ok {
		t.Fatalf("missing edge from c:loadBlur, got %+v", edges)
	}
	sharpenDst, ok := got["c:loadSharpen"]
	if !ok {
		t.Fatalf("missing edge from c:loadSharpen, got %+v", edges)
	}

	if blurDst == sharpenDst {
		t.Fatalf("loadBlur and loadSharpen both target %q — resolver failed to disambiguate glsl:main from glsl:main~2 (likely minted the target instead of looking it up)", blurDst)
	}

	blurNode, ok := g.Node(blurDst)
	if !ok {
		t.Fatalf("dst node %q not found in graph", blurDst)
	}
	if blurNode.File != "blur.comp" {
		t.Errorf("loadBlur's target node File = %q, want blur.comp (dst=%q)", blurNode.File, blurDst)
	}

	sharpenNode, ok := g.Node(sharpenDst)
	if !ok {
		t.Fatalf("dst node %q not found in graph", sharpenDst)
	}
	if sharpenNode.File != "sharpen.comp" {
		t.Errorf("loadSharpen's target node File = %q, want sharpen.comp (dst=%q)", sharpenNode.File, sharpenDst)
	}
}

// TestVulkanResolverIncompleteProvenanceNoBlob is test 3: vkCreateShaderModule
// is called but no .spv string literal is ever traceable to its pCode
// argument, so the MODULE step of the chain never binds a stem. RSV-004:
// zero edges, not a guess.
func TestVulkanResolverIncompleteProvenanceNoBlob(t *testing.T) {
	g := graph.NewMem()
	g.Upsert([]graph.Node{vkGlslNode("glsl:main", "blur.comp", 3)}, nil)

	src := `#include <vulkan/vulkan.h>

void loadDynamic(VkDevice device, uint32_t *code) {
    VkShaderModuleCreateInfo createInfo = {};
    createInfo.pCode = code;
    VkShaderModule shaderModule;
    vkCreateShaderModule(device, &createInfo, NULL, &shaderModule);

    VkPipelineShaderStageCreateInfo stageInfo = {
        .module = shaderModule,
        .pName = "main",
    };
}
`
	fs := vulkanFS{files: map[string]string{"host.c": src}}
	edges := resolveVulkan(t, g, fs)
	if len(edges) != 0 {
		t.Fatalf("want 0 edges (no traceable .spv blob), got %d: %+v", len(edges), edges)
	}
}

// TestVulkanResolverMissingPName is test 4: the stage struct never sets
// pName. The resolver must not default to "main" (RSV-004's "no default
// pName" rule, spelled out in the vulkan.go doc comment) — zero edges.
func TestVulkanResolverMissingPName(t *testing.T) {
	g := graph.NewMem()
	g.Upsert([]graph.Node{vkGlslNode("glsl:main", "blur.comp", 3)}, nil)

	src := `#include <vulkan/vulkan.h>

void loadBlur(VkDevice device) {
    const char *path = "shaders/blur.comp.spv";
    VkShaderModuleCreateInfo createInfo = {};
    createInfo.pCode = (uint32_t*)path;
    VkShaderModule shaderModule;
    vkCreateShaderModule(device, &createInfo, NULL, &shaderModule);

    VkPipelineShaderStageCreateInfo stageInfo = {
        .module = shaderModule,
    };
}
`
	fs := vulkanFS{files: map[string]string{"host.c": src}}
	edges := resolveVulkan(t, g, fs)
	if len(edges) != 0 {
		t.Fatalf("want 0 edges (no pName), got %d: %+v", len(edges), edges)
	}
}

// TestVulkanResolverAmbiguousStem is test 5: two glsl nodes share the same
// stem ("blur") from different directories, so graph lookup finds more than
// one candidate for (stem, entry). RSV-004: zero edges rather than guessing
// which one.
func TestVulkanResolverAmbiguousStem(t *testing.T) {
	g := graph.NewMem()
	g.Upsert([]graph.Node{
		vkGlslNode("glsl:main", "effects/blur.comp", 3),
		vkGlslNode("glsl:main~2", "postfx/blur.comp", 3),
	}, nil)

	fs := vulkanFS{files: map[string]string{"host.c": hostSingle}}
	edges := resolveVulkan(t, g, fs)
	if len(edges) != 0 {
		t.Fatalf("want 0 edges (ambiguous stem \"blur\" across two directories), got %d: %+v", len(edges), edges)
	}
}

// TestVulkanResolverDeterministic is test 6: two Resolve runs over the same
// FileSet must produce byte-identical edge slices in the same order — the
// doc comment on VulkanResolver.Resolve explains this is needed because
// FileSet.ByLang iteration order is not guaranteed (map-backed
// implementations, including these tests).
func TestVulkanResolverDeterministic(t *testing.T) {
	g := graph.NewMem()
	g.Upsert([]graph.Node{
		vkGlslNode("glsl:main", "blur.comp", 3),
		vkGlslNode("glsl:main~2", "sharpen.comp", 3),
	}, nil)

	src1 := hostSingle
	src2 := `#include <vulkan/vulkan.h>

void loadSharpen(VkDevice device) {
    const char *path = "shaders/sharpen.comp.spv";
    VkShaderModuleCreateInfo createInfo = {};
    createInfo.pCode = (uint32_t*)path;
    VkShaderModule shaderModule;
    vkCreateShaderModule(device, &createInfo, NULL, &shaderModule);

    VkPipelineShaderStageCreateInfo stageInfo = {
        .module = shaderModule,
        .pName = "main",
    };
}
`
	fs := vulkanFS{files: map[string]string{
		"a_host.c": src1,
		"b_host.c": src2,
	}}

	edges1 := resolveVulkan(t, g, fs)
	edges2 := resolveVulkan(t, g, fs)

	if len(edges1) != 2 {
		t.Fatalf("want 2 edges, got %d: %+v", len(edges1), edges1)
	}
	if !reflect.DeepEqual(edges1, edges2) {
		t.Fatalf("Resolve is not deterministic:\nrun1: %+v\nrun2: %+v", edges1, edges2)
	}
}

func TestVulkanResolverNilFileSet(t *testing.T) {
	edges, err := (VulkanResolver{}).Resolve(context.Background(), graph.NewMem(), nil)
	if err != nil {
		t.Fatalf("fs=nil must not error, got %v", err)
	}
	if edges != nil {
		t.Fatalf("fs=nil must yield nil edges, got %+v", edges)
	}
}
