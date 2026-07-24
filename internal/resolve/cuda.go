package resolve

import (
	"bufio"
	"bytes"
	"context"
	"regexp"

	"github.com/jxsl13/spectacle/internal/graph"
	"github.com/jxsl13/spectacle/internal/ids"
)

// CudaResolver links host code to CUDA kernels (edge kind ELaunch).
//
// Detection contract:
//
//   - `__global__` function definitions in .cu/.cuh files become KKernel
//     nodes with IDs "cu:<name>" (index.CudaParser owns that).
//   - `extern "C"` host wrapper functions in .cu files are minted with "c:"
//     IDs (not "cu:"), so the cgo resolver chains Go -> c:wrapper and this
//     resolver chains c:wrapper -launch-> cu:kernel — completing the
//     Go -> C -> CUDA impact path.
//   - A triple-chevron launch `name<<<grid, block>>>(args)` inside the body
//     of the *currently open* extern "C" wrapper yields an ELaunch edge from
//     "c:<wrapper>" to "cu:<name>". A launch outside any wrapper (e.g. inside
//     a plain __device__/__global__ helper) is skipped — this resolver only
//     tracks the host FFI boundary, not device-side dynamic parallelism.
//
// This is a line-oriented scanner mirroring index.CudaParser (no cgo
// tree-sitter dependency); it tracks the "currently active wrapper" via the
// same naive brace-depth walk used there.
type CudaResolver struct{}

func (CudaResolver) Name() string { return "cuda" }

func (CudaResolver) Langs() []graph.Lang {
	return []graph.Lang{graph.LangC, graph.LangCpp, graph.LangCuda}
}

var (
	// cudaResolverExternCRe mirrors index.cudaExternCRe.
	cudaResolverExternCRe = regexp.MustCompile(`^\s*extern\s+"C"\s+[\w\*\s]+?(\w+)\s*\(`)
	// cudaLaunchRe matches a kernel launch `name<<<grid, block>>>(`.
	cudaLaunchRe = regexp.MustCompile(`(\w+)\s*<<<`)
)

// launchKey dedupes edges per (wrapper, kernel, file).
type launchKey struct {
	wrapper, kernel, file string
}

// Resolve scans every CUDA source file for extern "C" wrapper bodies and the
// kernel launches inside them.
func (CudaResolver) Resolve(ctx context.Context, g graph.Graph, fs FileSet) ([]graph.Edge, error) {
	if fs == nil {
		return nil, nil
	}
	var edges []graph.Edge
	seen := map[launchKey]bool{}
	for _, path := range fs.ByLang(graph.LangCuda) {
		src, err := fs.Read(path)
		if err != nil {
			continue
		}
		edges = append(edges, resolveCudaFile(path, src, seen)...)
	}
	return edges, nil
}

// resolveCudaFile scans one file line by line, tracking the currently open
// extern "C" wrapper (by brace depth) and emitting an ELaunch edge for every
// kernel launch found inside it.
func resolveCudaFile(path string, src []byte, seen map[launchKey]bool) []graph.Edge {
	var edges []graph.Edge

	sc := bufio.NewScanner(bytes.NewReader(src))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var lines []string
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}

	wrapper := "" // name of the currently open extern "C" wrapper, "" if none
	depth := 0    // brace depth since the wrapper's opening line
	inWrapper := false

	for i, line := range lines {
		lineNo := i + 1
		if !inWrapper {
			if m := cudaResolverExternCRe.FindStringSubmatch(line); m != nil {
				wrapper = m[1]
				inWrapper = true
				depth = 0
				depth += braceDelta(line)
				if depth <= 0 {
					// single-line definition (rare, e.g. forward decl with body
					// on one line) — wrapper closes immediately.
					inWrapper = false
					wrapper = ""
				}
				continue
			}
		} else {
			if lm := cudaLaunchRe.FindAllStringSubmatchIndex(line, -1); lm != nil {
				for _, idx := range lm {
					kernel := line[idx[2]:idx[3]]
					key := launchKey{wrapper, kernel, path}
					if seen[key] {
						continue
					}
					seen[key] = true
					edges = append(edges, graph.Edge{
						Src:  graph.NodeID(ids.Mint("c", wrapper)),
						Dst:  graph.NodeID(ids.Mint("cu", kernel)),
						Kind: graph.ELaunch,
						File: path,
						Line: lineNo,
					})
				}
			}
			depth += braceDelta(line)
			if depth <= 0 {
				inWrapper = false
				wrapper = ""
			}
		}
	}
	return edges
}

// braceDelta returns the net change in brace depth contributed by one line.
func braceDelta(line string) int {
	delta := 0
	for _, r := range line {
		switch r {
		case '{':
			delta++
		case '}':
			delta--
		}
	}
	return delta
}
