package resolve

import (
	"context"

	"github.com/jxsl13/spectacle/internal/graph"
)

// CudaResolver links host code to CUDA kernels (edge kind ELaunch).
//
// Detection contract (implemented in M2):
//
//   - `__global__` function definitions in .cu/.cuh files become KKernel
//     nodes with IDs "cu:<name>".
//   - A triple-chevron launch `name<<<grid, block>>>(args)` (or an explicit
//     cudaLaunchKernel call) inside a host function yields an ELaunch edge
//     from the host function node to "cu:<name>".
//   - `extern "C"` host wrapper functions in .cu files are minted with "c:"
//     IDs (not "cu:"), so the cgo resolver chains Go -> c:wrapper and this
//     resolver chains c:wrapper -launch-> cu:kernel — completing the
//     Go -> C -> CUDA impact path.
type CudaResolver struct{}

func (CudaResolver) Name() string { return "cuda" }

func (CudaResolver) Langs() []graph.Lang {
	return []graph.Lang{graph.LangC, graph.LangCpp, graph.LangCuda}
}

func (CudaResolver) Resolve(ctx context.Context, g graph.Graph, fs FileSet) ([]graph.Edge, error) {
	return nil, nil // M2
}
