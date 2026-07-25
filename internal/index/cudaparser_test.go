package index

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/jxsl13/spectackle/internal/graph"
	"github.com/jxsl13/spectackle/internal/resolve"
	"github.com/jxsl13/spectackle/internal/store"
)

// cudaparserSrc mirrors examples/saxpy/saxpy/kernels/saxpy.cu closely enough
// that the Line/EndLine assertions below track the real reference file (see
// the e2e test further down, which parses the real file directly).
var cudaparserSrc = []byte(`#include <cuda_runtime.h>

#include "saxpy.h"

/* CUDA-KRN-002: every kernel guards element access with an explicit
 * index bound check. */
__global__ void saxpy_kernel(int n, float a, const float *x, float *y) {
    int i = blockIdx.x * blockDim.x + threadIdx.x;
    if (i < n) {
        y[i] = a * x[i] + y[i];
    }
}

extern "C" int launch_saxpy(int n, float a, const float *x, float *y) {
    float *dx = 0, *dy = 0;
    size_t bytes = (size_t)n * sizeof(float);
    cudaError_t err;

    if ((err = cudaMalloc(&dx, bytes)) != cudaSuccess) goto out;

    {
        int block = 256;
        int grid = (n + block - 1) / block;
        saxpy_kernel<<<grid, block>>>(n, a, dx, dy);
    }

out:
    if (dx) cudaFree(dx);
    return (int)err;
}
`)

func parseCudaDemo(t *testing.T) ParseResult {
	t.Helper()
	pr, err := (CudaParser{}).Parse("kernels/saxpy.cu", cudaparserSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return pr
}

func cudaNodesByID(pr ParseResult) map[graph.NodeID]graph.Node {
	m := map[graph.NodeID]graph.Node{}
	for _, n := range pr.Nodes {
		m[n.ID] = n
	}
	return m
}

func TestCudaParserLangExtensions(t *testing.T) {
	p := CudaParser{}
	if p.Lang() != graph.LangCuda {
		t.Errorf("Lang() = %v, want %v", p.Lang(), graph.LangCuda)
	}
	if got, want := p.Extensions(), []string{".cu", ".cuh"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Extensions() = %v, want %v", got, want)
	}
}

func TestCudaParserKernelNode(t *testing.T) {
	pr := parseCudaDemo(t)
	byID := cudaNodesByID(pr)
	n, ok := byID["cu:saxpy_kernel"]
	if !ok {
		t.Fatalf("node cu:saxpy_kernel missing, got nodes %+v", pr.Nodes)
	}
	if n.Kind != graph.KKernel {
		t.Errorf("cu:saxpy_kernel Kind = %v, want KKernel", n.Kind)
	}
	if n.Lang != graph.LangCuda {
		t.Errorf("cu:saxpy_kernel Lang = %v, want LangCuda", n.Lang)
	}
	if n.File != "kernels/saxpy.cu" {
		t.Errorf("cu:saxpy_kernel File = %q, want kernels/saxpy.cu", n.File)
	}
	if n.Line != 7 {
		t.Errorf("cu:saxpy_kernel Line = %d, want 7", n.Line)
	}
	if n.EndLine <= n.Line {
		t.Errorf("cu:saxpy_kernel EndLine = %d, want > Line (%d)", n.EndLine, n.Line)
	}
}

func TestCudaParserExternCWrapperNode(t *testing.T) {
	pr := parseCudaDemo(t)
	byID := cudaNodesByID(pr)
	n, ok := byID["c:launch_saxpy"]
	if !ok {
		t.Fatalf("node c:launch_saxpy missing, got nodes %+v", pr.Nodes)
	}
	if n.Kind != graph.KFunc {
		t.Errorf("c:launch_saxpy Kind = %v, want KFunc", n.Kind)
	}
	if n.Lang != graph.LangC {
		t.Errorf("c:launch_saxpy Lang = %v, want LangC (the extern \"C\" wrapper IS the C symbol)", n.Lang)
	}
	if n.File != "kernels/saxpy.cu" {
		t.Errorf("c:launch_saxpy File = %q, want kernels/saxpy.cu", n.File)
	}
	if n.Line != 14 {
		t.Errorf("c:launch_saxpy Line = %d, want 14", n.Line)
	}
	if n.EndLine <= n.Line {
		t.Errorf("c:launch_saxpy EndLine = %d, want > Line (%d)", n.EndLine, n.Line)
	}
}

// TestCudaParserStaticKernel covers a translation-unit-local kernel using
// the `static` qualifier before `__global__` (common in .cu files):
// static __global__ void reduce_kernel(const float *in, float *out, int n) { ... }
func TestCudaParserStaticKernel(t *testing.T) {
	src := []byte(`static __global__ void reduce_kernel(const float *in, float *out, int n) {
    extern __shared__ float sdata[];
    int tid = threadIdx.x;
    sdata[tid] = (tid < n) ? in[tid] : 0.0f;
    __syncthreads();
    if (tid == 0) out[0] = sdata[0];
}
`)
	pr, err := (CudaParser{}).Parse("sample.cu", src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := cudaNodesByID(pr)
	n, ok := byID["cu:reduce_kernel"]
	if !ok {
		t.Fatalf("node cu:reduce_kernel missing, got nodes %+v", pr.Nodes)
	}
	if n.Kind != graph.KKernel || n.Lang != graph.LangCuda || n.Line != 1 {
		t.Errorf("cu:reduce_kernel node wrong: %+v", n)
	}
}

// TestCudaParserLaunchBoundsKernel covers a __launch_bounds__(N)-decorated
// kernel. Before the fix this attribute (sitting between the return type
// and the kernel name) was mis-captured as the kernel's own name, minting
// a wrong node cu:__launch_bounds__ instead of cu:bounded_kernel.
func TestCudaParserLaunchBoundsKernel(t *testing.T) {
	src := []byte(`__global__ void __launch_bounds__(256) bounded_kernel(float *data, int n) {
    int i = blockIdx.x * blockDim.x + threadIdx.x;
    if (i < n) data[i] += 1.0f;
}
`)
	pr, err := (CudaParser{}).Parse("sample.cu", src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := cudaNodesByID(pr)
	if _, wrong := byID["cu:__launch_bounds__"]; wrong {
		t.Errorf("minted wrong node cu:__launch_bounds__, got nodes %+v", pr.Nodes)
	}
	n, ok := byID["cu:bounded_kernel"]
	if !ok {
		t.Fatalf("node cu:bounded_kernel missing, got nodes %+v", pr.Nodes)
	}
	if n.Kind != graph.KKernel || n.Lang != graph.LangCuda || n.Line != 1 {
		t.Errorf("cu:bounded_kernel node wrong: %+v", n)
	}
}

// TestCudaParserDeviceFunction covers a __device__ helper function, a
// symbol kind the parser previously minted no node for at all.
func TestCudaParserDeviceFunction(t *testing.T) {
	src := []byte(`__device__ float square_device(float v) {
    return v * v;
}
`)
	pr, err := (CudaParser{}).Parse("sample.cu", src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := cudaNodesByID(pr)
	n, ok := byID["cu:square_device"]
	if !ok {
		t.Fatalf("node cu:square_device missing, got nodes %+v", pr.Nodes)
	}
	if n.Kind != graph.KFunc || n.Lang != graph.LangCuda || n.Line != 1 {
		t.Errorf("cu:square_device node wrong: %+v", n)
	}
}

// TestCudaParserHostDeviceFunction covers a __host__ __device__ dual-mode
// function.
func TestCudaParserHostDeviceFunction(t *testing.T) {
	src := []byte(`__host__ __device__ int clamp_hd(int v, int lo, int hi) {
    if (v < lo) return lo;
    if (v > hi) return hi;
    return v;
}
`)
	pr, err := (CudaParser{}).Parse("sample.cu", src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := cudaNodesByID(pr)
	n, ok := byID["cu:clamp_hd"]
	if !ok {
		t.Fatalf("node cu:clamp_hd missing, got nodes %+v", pr.Nodes)
	}
	if n.Kind != graph.KFunc || n.Lang != graph.LangCuda || n.Line != 1 {
		t.Errorf("cu:clamp_hd node wrong: %+v", n)
	}
}

// TestCudaParserExternCBlock covers wrapper functions grouped inside an
// `extern "C" { ... }` block (avoids repeating `extern "C"` per function).
// Functions declared this way lack their own extern "C" prefix, so they
// previously never became nodes at all.
func TestCudaParserExternCBlock(t *testing.T) {
	src := []byte(`extern "C" {

int launch_scale(int n, float factor, float *data) {
    int threads = 256;
    int blocks = (n + threads - 1) / threads;
    scale_kernel<<<blocks, threads>>>(n, factor, data);
    return 0;
}

int launch_reduce(const float *in, float *out, int n) {
    reduce_kernel<<<1, 256, 256 * sizeof(float)>>>(in, out, n);
    return 0;
}

} // extern "C"
`)
	pr, err := (CudaParser{}).Parse("sample.cu", src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := cudaNodesByID(pr)

	scale, ok := byID["c:launch_scale"]
	if !ok {
		t.Fatalf("node c:launch_scale missing, got nodes %+v", pr.Nodes)
	}
	if scale.Kind != graph.KFunc || scale.Lang != graph.LangC || scale.Line != 3 {
		t.Errorf("c:launch_scale node wrong: %+v", scale)
	}

	reduce, ok := byID["c:launch_reduce"]
	if !ok {
		t.Fatalf("node c:launch_reduce missing, got nodes %+v", pr.Nodes)
	}
	if reduce.Kind != graph.KFunc || reduce.Lang != graph.LangC || reduce.Line != 10 {
		t.Errorf("c:launch_reduce node wrong: %+v", reduce)
	}

	// Statements inside the wrapper bodies (declarations, kernel launches,
	// returns) must not themselves be mistaken for new definitions.
	if len(pr.Nodes) != 2 {
		t.Errorf("got %d nodes, want exactly 2 (launch_scale, launch_reduce): %+v", len(pr.Nodes), pr.Nodes)
	}
}

// TestCudaParserDeviceStructMethod covers a C++ class/struct with a
// __device__ method (the thrust/cub-style functor idiom): the method is
// qualified as "<Struct>.<method>" and minted as a KMethod, distinct from
// a free __device__ function.
func TestCudaParserDeviceStructMethod(t *testing.T) {
	src := []byte(`struct AddFunctor {
    __device__ float operator()(float a, float b) const {
        return a + b;
    }
};
`)
	pr, err := (CudaParser{}).Parse("sample.cu", src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := cudaNodesByID(pr)
	n, ok := byID["cu:AddFunctor.operator()"]
	if !ok {
		t.Fatalf("node cu:AddFunctor.operator() missing, got nodes %+v", pr.Nodes)
	}
	if n.Kind != graph.KMethod || n.Lang != graph.LangCuda || n.Line != 2 {
		t.Errorf("cu:AddFunctor.operator() node wrong: %+v", n)
	}
}

func TestCudaParserNoEdges(t *testing.T) {
	pr := parseCudaDemo(t)
	if len(pr.Edges) != 0 {
		t.Errorf("CudaParser must not emit edges, got %+v", pr.Edges)
	}
}

func TestCudaParserDeterministic(t *testing.T) {
	pr1 := parseCudaDemo(t)
	pr2 := parseCudaDemo(t)
	if !reflect.DeepEqual(pr1, pr2) {
		t.Errorf("Parse is not deterministic across identical runs:\n%+v\nvs\n%+v", pr1, pr2)
	}
}

func TestCudaParserHashMatchesContent(t *testing.T) {
	pr := parseCudaDemo(t)
	if pr.Hash == ([32]byte{}) {
		t.Error("Hash is zero, want sha256 of source content")
	}
}

// The shipped example repo is the reference Go -> C -> CUDA triple-hop chain.
// With Go + CUDA parsers and the full resolver set active, the chain must
// fully materialize: go:saxpy.Saxpy -ECgo-> c:launch_saxpy -ELaunch->
// cu:saxpy_kernel, with c:launch_saxpy no longer a stub.
func TestIndexAllSaxpyExampleCudaChain(t *testing.T) {
	root, err := filepath.Abs(filepath.FromSlash("../../examples/saxpy"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(root); err != nil {
		t.Skipf("examples/saxpy not available: %v", err)
	}

	g := graph.NewMem()
	idx := New(g, store.NewMem(), []LanguageParser{GoParser{}, CudaParser{}}, resolve.Default().All())
	if _, err := idx.IndexAll(context.Background(), root); err != nil {
		t.Fatalf("IndexAll(%s): %v", root, err)
	}

	// (a) c:launch_saxpy is a real, located node — not a stub.
	wrapper, ok := g.Node("c:launch_saxpy")
	if !ok {
		t.Fatal("node c:launch_saxpy missing")
	}
	if want := filepath.ToSlash(filepath.Join("saxpy", "kernels", "saxpy.cu")); wrapper.File != want {
		t.Errorf("c:launch_saxpy File = %q, want %q", wrapper.File, want)
	}
	if wrapper.Line <= 0 {
		t.Errorf("c:launch_saxpy Line = %d, want > 0 (real node, not stub)", wrapper.Line)
	}
	if wrapper.Kind != graph.KFunc {
		t.Errorf("c:launch_saxpy Kind = %v, want KFunc", wrapper.Kind)
	}

	// (b) go:saxpy.Saxpy -ECgo-> c:launch_saxpy
	cgo := g.Neighbors("go:saxpy.Saxpy", graph.Out, []graph.EdgeKind{graph.ECgo})
	foundCgo := false
	for _, e := range cgo {
		if e.Dst == "c:launch_saxpy" {
			foundCgo = true
		}
	}
	if !foundCgo {
		t.Errorf("Neighbors(go:saxpy.Saxpy, Out, ECgo) = %+v, want edge to c:launch_saxpy", cgo)
	}

	// (c) c:launch_saxpy -ELaunch-> cu:saxpy_kernel
	launch := g.Neighbors("c:launch_saxpy", graph.Out, []graph.EdgeKind{graph.ELaunch})
	foundLaunch := false
	for _, e := range launch {
		if e.Dst == "cu:saxpy_kernel" {
			foundLaunch = true
		}
	}
	if !foundLaunch {
		t.Errorf("Neighbors(c:launch_saxpy, Out, ELaunch) = %+v, want edge to cu:saxpy_kernel", launch)
	}

	// cu:saxpy_kernel itself is a real, located kernel node.
	kernel, ok := g.Node("cu:saxpy_kernel")
	if !ok {
		t.Fatal("node cu:saxpy_kernel missing")
	}
	if kernel.Kind != graph.KKernel || kernel.Lang != graph.LangCuda {
		t.Errorf("cu:saxpy_kernel = %+v, want Kind=KKernel Lang=cu", kernel)
	}
}
