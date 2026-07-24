package resolve

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jxsl13/spectacle/internal/graph"
)

// cudaFS is a tiny in-memory FileSet exposing only .cu/.cuh files.
type cudaFS struct {
	files map[string]string
}

func (f cudaFS) ByLang(l graph.Lang) []string {
	if l != graph.LangCuda {
		return nil
	}
	var paths []string
	for p := range f.files {
		paths = append(paths, p)
	}
	return paths
}

func (f cudaFS) Read(path string) ([]byte, error) {
	src, ok := f.files[path]
	if !ok {
		return nil, errors.New("cudaFS: no such file")
	}
	return []byte(src), nil
}

// resolveCuda runs CudaResolver over an in-memory file set and fails the test
// on error.
func resolveCuda(t *testing.T, fs FileSet) []graph.Edge {
	t.Helper()
	edges, err := (CudaResolver{}).Resolve(context.Background(), graph.NewMem(), fs)
	if err != nil {
		t.Fatalf("CudaResolver.Resolve returned error: %v", err)
	}
	return edges
}

// saxpyCu is the reference triple-hop chain source (see
// examples/saxpy/saxpy/kernels/saxpy.cu): a __global__ kernel plus an
// extern "C" wrapper that launches it.
const saxpyCu = `#include <cuda_runtime.h>

#include "saxpy.h"

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
    if ((err = cudaMalloc(&dy, bytes)) != cudaSuccess) goto out;
    if ((err = cudaMemcpy(dx, x, bytes, cudaMemcpyHostToDevice)) != cudaSuccess) goto out;
    if ((err = cudaMemcpy(dy, y, bytes, cudaMemcpyHostToDevice)) != cudaSuccess) goto out;

    {
        int block = 256;
        int grid = (n + block - 1) / block;
        saxpy_kernel<<<grid, block>>>(n, a, dx, dy);
        if ((err = cudaGetLastError()) != cudaSuccess) goto out;
    }

    err = cudaMemcpy(y, dy, bytes, cudaMemcpyDeviceToHost);

out:
    if (dx) cudaFree(dx);
    if (dy) cudaFree(dy);
    return (int)err;
}
`

func lineOfCuda(t *testing.T, src, substr string) int {
	t.Helper()
	i := strings.Index(src, substr)
	if i < 0 {
		t.Fatalf("substring %q not found in source", substr)
	}
	return 1 + strings.Count(src[:i], "\n")
}

func TestCudaResolverSaxpyLaunch(t *testing.T) {
	fs := cudaFS{files: map[string]string{"kernels/saxpy.cu": saxpyCu}}
	edges := resolveCuda(t, fs)
	if len(edges) != 1 {
		t.Fatalf("want exactly 1 ELaunch edge, got %d: %+v", len(edges), edges)
	}
	e := edges[0]
	if e.Src != "c:launch_saxpy" || e.Dst != "cu:saxpy_kernel" {
		t.Errorf("edge endpoints = %q -> %q, want c:launch_saxpy -> cu:saxpy_kernel", e.Src, e.Dst)
	}
	if e.Kind != graph.ELaunch {
		t.Errorf("edge kind = %v, want ELaunch", e.Kind)
	}
	if e.File != "kernels/saxpy.cu" {
		t.Errorf("edge file = %q, want kernels/saxpy.cu", e.File)
	}
	if want := lineOfCuda(t, saxpyCu, "saxpy_kernel<<<"); e.Line != want {
		t.Errorf("edge line = %d, want %d (the launch statement)", e.Line, want)
	}
}

func TestCudaResolverLaunchOutsideWrapperSkipped(t *testing.T) {
	src := `__global__ void other_kernel(int n) {
    int i = 0;
}

void unrelated_helper(void) {
    other_kernel<<<1, 1>>>(1);
}
`
	fs := cudaFS{files: map[string]string{"stray.cu": src}}
	edges := resolveCuda(t, fs)
	if len(edges) != 0 {
		t.Fatalf("launch outside an extern \"C\" wrapper must yield no edge, got %+v", edges)
	}
}

func TestCudaResolverDedupesRepeatedLaunch(t *testing.T) {
	src := `__global__ void k(int n) {
    int i = 0;
}

extern "C" void run(int n) {
    k<<<1, 1>>>(n);
    k<<<1, 1>>>(n);
}
`
	fs := cudaFS{files: map[string]string{"dup.cu": src}}
	edges := resolveCuda(t, fs)
	if len(edges) != 1 {
		t.Fatalf("repeated launch of the same kernel from the same wrapper must dedupe to 1 edge, got %d: %+v", len(edges), edges)
	}
	e := edges[0]
	if e.Src != "c:run" || e.Dst != "cu:k" {
		t.Errorf("edge endpoints = %q -> %q, want c:run -> cu:k", e.Src, e.Dst)
	}
	if want := lineOfCuda(t, src, "k<<<1, 1>>>(n);"); e.Line != want {
		t.Errorf("edge line = %d, want %d (first call site)", e.Line, want)
	}
}

func TestCudaResolverNilFileSet(t *testing.T) {
	edges, err := (CudaResolver{}).Resolve(context.Background(), graph.NewMem(), nil)
	if err != nil {
		t.Fatalf("fs=nil must not error, got %v", err)
	}
	if edges != nil {
		t.Fatalf("fs=nil must yield nil edges, got %+v", edges)
	}
}

func TestCudaResolverMultipleWrappersInOneFile(t *testing.T) {
	src := `__global__ void a_kernel(int n) {
    int i = 0;
}

__global__ void b_kernel(int n) {
    int i = 0;
}

extern "C" void run_a(int n) {
    a_kernel<<<1, 1>>>(n);
}

extern "C" void run_b(int n) {
    b_kernel<<<1, 1>>>(n);
}
`
	fs := cudaFS{files: map[string]string{"multi.cu": src}}
	edges := resolveCuda(t, fs)
	if len(edges) != 2 {
		t.Fatalf("want 2 edges (one per wrapper), got %d: %+v", len(edges), edges)
	}
	got := map[string]string{}
	for _, e := range edges {
		got[string(e.Src)] = string(e.Dst)
	}
	if got["c:run_a"] != "cu:a_kernel" || got["c:run_b"] != "cu:b_kernel" {
		t.Errorf("edges = %+v, want c:run_a -> cu:a_kernel and c:run_b -> cu:b_kernel", got)
	}
}
