package resolve

import (
	"context"

	"github.com/jxsl13/spectackle/internal/graph"
)

// GpuPipeResolver links pipeline-based GPU dispatch (Metal, Vulkan) where the
// binding is by name at runtime, not by symbol (edge kind ELaunch, best
// effort).
//
// Detection contract (implemented in M6):
//
//   - Metal: a string literal "kern_name" appearing as the argument of
//     newFunctionWithName: (ObjC) or MTLLibrary.makeFunction(name:) is
//     matched against [[kernel]] entry points of the same name in .metal
//     files -> ELaunch edge from the enclosing host function to "msl:<name>".
//   - Vulkan: string literals / VkPipelineShaderStageCreateInfo pName values
//     near vkCreate*Pipelines calls are matched against SPIR-V / GLSL entry
//     points.
//   - These edges are heuristic: confidence is recorded in Edge via a Sig
//     annotation on the target node and a Rank penalty, so ranking prefers
//     symbol-resolved edges over string-matched ones.
type GpuPipeResolver struct{}

func (GpuPipeResolver) Name() string { return "gpupipe" }

func (GpuPipeResolver) Langs() []graph.Lang {
	return []graph.Lang{graph.LangGo, graph.LangObjC, graph.LangMSL}
}

func (GpuPipeResolver) Resolve(ctx context.Context, g graph.Graph, fs FileSet) ([]graph.Edge, error) {
	return nil, nil // M6
}
