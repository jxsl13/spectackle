package index

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"regexp"

	"github.com/jxsl13/spectackle/internal/graph"
	"github.com/jxsl13/spectackle/internal/ids"
)

// CudaParser is a line-oriented regex scanner for .cu/.cuh files (no cgo
// tree-sitter dependency yet — see docs/architecture.md §2 for the planned
// wazero-hosted grammar migration). It extracts two symbol shapes:
//
//   - `__global__` kernel definitions -> KKernel nodes with IDs "cu:<name>".
//   - `extern "C"` host wrapper functions -> KFunc nodes with IDs "c:<name>".
//     The wrapper IS the C symbol that resolve.CgoResolver's Go -> c:<name>
//     edge already targets as a stub; this parser upgrades that stub to a
//     real, located node.
//
// No edges are emitted here — the kernel launch (ELaunch) is a cross-file,
// cross-symbol relation and belongs to resolve.CudaResolver. Parsing is
// deterministic: identical bytes yield identical results (SPX-GRA-001).
type CudaParser struct{}

// Lang identifies the parser's language.
func (CudaParser) Lang() graph.Lang { return graph.LangCuda }

// Extensions are the file suffixes this parser claims.
func (CudaParser) Extensions() []string { return []string{".cu", ".cuh"} }

var (
	// cudaKernelRe matches a __global__ kernel definition, e.g.
	// `__global__ void saxpy_kernel(int n, float a, const float *x, float *y) {`
	cudaKernelRe = regexp.MustCompile(`^\s*__global__\s+[\w:<>]+\s+(\w+)\s*\(`)
	// cudaExternCRe matches an extern "C" host wrapper definition, e.g.
	// `extern "C" int launch_saxpy(int n, float a, const float *x, float *y) {`
	cudaExternCRe = regexp.MustCompile(`^\s*extern\s+"C"\s+[\w\*\s]+?(\w+)\s*\(`)
)

// Parse scans one .cu/.cuh file line by line for kernel and extern "C"
// wrapper definitions.
func (CudaParser) Parse(path string, src []byte) (ParseResult, error) {
	var nodes []graph.Node

	sc := bufio.NewScanner(bytes.NewReader(src))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lineNo := 0
	var lines []string
	for sc.Scan() {
		lineNo++
		lines = append(lines, sc.Text())
	}
	if err := sc.Err(); err != nil {
		return ParseResult{}, err
	}

	for i, line := range lines {
		ln := i + 1
		if m := cudaKernelRe.FindStringSubmatch(line); m != nil {
			name := m[1]
			id := graph.NodeID(ids.Mint("cu", name))
			nodes = append(nodes, graph.Node{
				ID: id, Kind: graph.KKernel, Lang: graph.LangCuda, File: path,
				Line: ln, EndLine: braceEndLine(lines, i),
			})
			continue
		}
		if m := cudaExternCRe.FindStringSubmatch(line); m != nil {
			name := m[1]
			id := graph.NodeID(ids.Mint("c", name))
			nodes = append(nodes, graph.Node{
				ID: id, Kind: graph.KFunc, Lang: graph.LangC, File: path,
				Line: ln, EndLine: braceEndLine(lines, i),
			})
		}
	}

	return ParseResult{Nodes: nodes, Edges: nil, Hash: sha256.Sum256(src)}, nil
}

// braceEndLine does a naive brace-depth walk starting at lines[start] (the
// definition line) and returns the 1-based line number where the depth
// returns to zero after having gone positive. If the definition line has no
// opening brace, or the closing brace is never found, it returns the
// definition's own 1-based line number (Line == EndLine, "unknown span").
func braceEndLine(lines []string, start int) int {
	depth := 0
	seenOpen := false
	for i := start; i < len(lines); i++ {
		for _, r := range lines[i] {
			switch r {
			case '{':
				depth++
				seenOpen = true
			case '}':
				depth--
			}
		}
		if seenOpen && depth <= 0 {
			return i + 1
		}
	}
	return start + 1
}
