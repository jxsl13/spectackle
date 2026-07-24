package resolve

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jxsl13/spectackle/internal/graph"
)

// gpupipeFS is a tiny in-memory FileSet exposing only .m files.
type gpupipeFS struct {
	files map[string]string
}

func (f gpupipeFS) ByLang(l graph.Lang) []string {
	if l != graph.LangObjC {
		return nil
	}
	var paths []string
	for p := range f.files {
		paths = append(paths, p)
	}
	return paths
}

func (f gpupipeFS) Read(path string) ([]byte, error) {
	src, ok := f.files[path]
	if !ok {
		return nil, errors.New("gpupipeFS: no such file")
	}
	return []byte(src), nil
}

// resolveGpuPipe runs GpuPipeResolver over an in-memory file set and fails the
// test on error.
func resolveGpuPipe(t *testing.T, fs FileSet) []graph.Edge {
	t.Helper()
	edges, err := (GpuPipeResolver{}).Resolve(context.Background(), graph.NewMem(), fs)
	if err != nil {
		t.Fatalf("GpuPipeResolver.Resolve returned error: %v", err)
	}
	return edges
}

// lineOfGpuPipe returns the line number (1-indexed) of a substring in the
// source, for test assertions.
func lineOfGpuPipe(t *testing.T, src, substr string) int {
	t.Helper()
	i := strings.Index(src, substr)
	if i < 0 {
		t.Fatalf("substring %q not found in source", substr)
	}
	return 1 + strings.Count(src[:i], "\n")
}

// fixtureM is a fixture .m file with ObjC methods and C functions calling
// Metal kernels via newFunctionWithName:.
const fixtureM = `#import <Foundation/Foundation.h>
#import <Metal/Metal.h>

// ObjC method with kernel call
- (void)runKernel {
    id<MTLDevice> device = MTLCreateSystemDefaultDevice();
    id<MTLLibrary> library = [device newDefaultLibrary];
    id<MTLFunction> function = [library newFunctionWithName:@"add_arrays"];
    // duplicate call to same kernel in same method (should dedupe)
    id<MTLFunction> function2 = [library newFunctionWithName:@"add_arrays"];
}

// C function with kernel call
static void setupPipeline(void) {
    id<MTLDevice> device = MTLCreateSystemDefaultDevice();
    id<MTLLibrary> library = [device newDefaultLibrary];
    id<MTLFunction> function = [library newFunctionWithName:@"scale_rows"];
}

// Top-level orphan kernel call outside any host function body (should NOT edge)
id<MTLFunction> orphanFunc = nil;
orphanFunc = [library newFunctionWithName:@"orphan_kernel"];
`

func TestGpuPipeResolverObjCMethods(t *testing.T) {
	fs := gpupipeFS{files: map[string]string{"fixture.m": fixtureM}}
	edges := resolveGpuPipe(t, fs)
	if len(edges) != 2 {
		t.Fatalf("want exactly 2 ELaunch edges, got %d: %+v", len(edges), edges)
	}

	// Collect edges into a map for easier lookup
	edgeMap := make(map[string]string)
	for _, e := range edges {
		key := string(e.Src) + " -> " + string(e.Dst)
		edgeMap[key] = key
	}

	// Check expected edges
	want := map[string]bool{
		"objc:fixture.runKernel -> msl:add_arrays":     false,
		"objc:fixture.setupPipeline -> msl:scale_rows": false,
	}

	for key := range want {
		if _, ok := edgeMap[key]; !ok {
			t.Errorf("missing expected edge: %s", key)
		} else {
			want[key] = true
		}
	}

	// Check that edges have correct metadata
	for _, e := range edges {
		if e.Kind != graph.ELaunch {
			t.Errorf("edge kind = %v, want ELaunch", e.Kind)
		}
		if e.File != "fixture.m" {
			t.Errorf("edge file = %q, want fixture.m", e.File)
		}
	}

	// Verify the deduplicated add_arrays call is from runKernel
	for _, e := range edges {
		if e.Src == "objc:fixture.runKernel" && e.Dst == "msl:add_arrays" {
			// The line number should point to the first call site
			if want := lineOfGpuPipe(t, fixtureM, `newFunctionWithName:@"add_arrays"`); e.Line != want {
				t.Errorf("edge line = %d, want %d (the first launch statement)", e.Line, want)
			}
		}
	}
}

func TestGpuPipeResolverNilFileSet(t *testing.T) {
	edges, err := (GpuPipeResolver{}).Resolve(context.Background(), graph.NewMem(), nil)
	if err != nil {
		t.Fatalf("fs=nil must not error, got %v", err)
	}
	if edges != nil {
		t.Fatalf("fs=nil must yield nil edges, got %+v", edges)
	}
}

func TestGpuPipeResolverOrphanCallOutsideBody(t *testing.T) {
	src := `
- (void)method {
    newFunctionWithName:@"in_method";
}

// Call outside any body
newFunctionWithName:@"orphan";
`
	fs := gpupipeFS{files: map[string]string{"test.m": src}}
	edges := resolveGpuPipe(t, fs)
	if len(edges) != 1 {
		t.Fatalf("want 1 edge (only the one inside the method), got %d: %+v", len(edges), edges)
	}
	e := edges[0]
	if e.Dst != "msl:in_method" {
		t.Errorf("edge dst = %q, want msl:in_method", e.Dst)
	}
}
