package index

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"regexp"
	"strings"

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

// cudaParserCacheVersion discriminates cached parse blobs produced by this
// parser. BUMP IT whenever Parse's output changes for input it already
// accepted (B-0007); T-0126 (static __global__, __launch_bounds__,
// __device__ functions and methods, extern "C" blocks) is exactly such a
// change.
const cudaParserCacheVersion = "cu-2"

// CacheVersion implements CacheVersioner.
func (CudaParser) CacheVersion() string { return cudaParserCacheVersion }

var (
	// cudaKernelRe matches a __global__ kernel definition, e.g.
	// `__global__ void saxpy_kernel(int n, float a, const float *x, float *y) {`
	// Also handles two modifier-order variants:
	//   - an optional leading `static` qualifier (translation-unit-local
	//     kernel): `static __global__ void reduce_kernel(...) {`
	//   - an optional `__launch_bounds__(N)` attribute between the return
	//     type and the kernel name (a perf idiom): previously this made the
	//     regex mint a wrong node named `__launch_bounds__` instead of
	//     skipping over it to reach the real kernel name.
	cudaKernelRe = regexp.MustCompile(`^\s*(?:static\s+)?__global__\s+[\w:<>]+\s+(?:__launch_bounds__\s*\([^()]*\)\s+)?(\w+)\s*\(`)
	// cudaExternCRe matches a single-line extern "C" host wrapper
	// definition, e.g.
	// `extern "C" int launch_saxpy(int n, float a, const float *x, float *y) {`
	cudaExternCRe = regexp.MustCompile(`^\s*extern\s+"C"\s+[\w\*\s]+?(\w+)\s*\(`)
	// cudaExternCBlockOpenRe matches the opening line of a grouping
	// `extern "C" { ... }` block; the wrapper functions declared inside
	// lack their own `extern "C"` prefix (see cudaBlockFuncRe).
	cudaExternCBlockOpenRe = regexp.MustCompile(`^\s*extern\s+"C"\s*\{\s*$`)
	// cudaBlockFuncRe matches a plain function-definition line (no
	// qualifier) at the immediate top level of an extern "C" { ... } block,
	// e.g. `int launch_scale(int n, float factor, float *data) {`. Gated by
	// brace-depth tracking in Parse so statement/expression lines inside a
	// wrapper's own body (which sit one level deeper) never match.
	cudaBlockFuncRe = regexp.MustCompile(`^\s*[\w:<>*&]+\s+(\w+)\s*\(([^;{}]*)\)\s*\{\s*$`)
	// cudaDeviceRe matches a __device__ function definition, including the
	// __host__ __device__ dual-mode form (either modifier order) and an
	// optional leading `static`. Also matches a C++ `operator()` member
	// when found at the top level of a struct/class body (see cudaStructRe
	// / structDepth tracking in Parse), the thrust/cub-style functor idiom.
	cudaDeviceRe = regexp.MustCompile(`^\s*(?:static\s+)?(?:__host__\s+__device__|__device__\s+__host__|__device__)\s+[\w:<>]+\s+(operator\(\)|\w+)\s*\(`)
	// cudaStructRe matches a struct/class definition opening line, e.g.
	// `struct AddFunctor {`, used to qualify __device__ member names as
	// "<Struct>.<method>" and to distinguish them (KMethod) from free
	// __device__ functions (KFunc).
	cudaStructRe = regexp.MustCompile(`^\s*(?:struct|class)\s+(\w+)\b`)
)

// cudaControlKeywords excludes C/C++ control-flow statements that could
// otherwise be mistaken for a bare function definition by cudaBlockFuncRe
// (e.g. `if (n > 0) {`  never actually matches the regex today because it
// requires a leading type token before the name, but the guard is kept as a
// defense-in-depth belt for less obvious keyword/macro shapes).
var cudaControlKeywords = map[string]bool{
	"if": true, "for": true, "while": true, "switch": true,
	"else": true, "do": true, "return": true,
}

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

	// depth is a running brace-nesting count over the whole file, used only
	// to scope the two block-grouping forms below (extern "C" { ... } and
	// struct/class { ... }) so their interior statement/expression lines
	// (which sit one level deeper, inside a wrapper's or method's own body)
	// never get mistaken for new definitions.
	depth := 0
	var inExternCBlock bool
	var externCBlockDepth int // depth level of the block's own top level; 0 = unset (a real block is always >=1)
	var inStruct bool
	var structDepth int // depth level of the struct/class's own top level; 0 = unset
	var structName string

	for i, line := range lines {
		ln := i + 1
		depthBefore := depth

		switch {
		case cudaKernelRe.MatchString(line):
			m := cudaKernelRe.FindStringSubmatch(line)
			nodes = append(nodes, cudaNode("cu", m[1], graph.KKernel, graph.LangCuda, path, ln, braceEndLine(lines, i)))
		case cudaExternCRe.MatchString(line):
			m := cudaExternCRe.FindStringSubmatch(line)
			nodes = append(nodes, cudaNode("c", m[1], graph.KFunc, graph.LangC, path, ln, braceEndLine(lines, i)))
		case cudaDeviceRe.MatchString(line):
			m := cudaDeviceRe.FindStringSubmatch(line)
			name := m[1]
			if inStruct && depthBefore == structDepth {
				nodes = append(nodes, cudaNode("cu", structName+"."+name, graph.KMethod, graph.LangCuda, path, ln, braceEndLine(lines, i)))
			} else {
				nodes = append(nodes, cudaNode("cu", name, graph.KFunc, graph.LangCuda, path, ln, braceEndLine(lines, i)))
			}
		case inExternCBlock && depthBefore == externCBlockDepth && cudaBlockFuncRe.MatchString(line):
			m := cudaBlockFuncRe.FindStringSubmatch(line)
			name := m[1]
			if !cudaControlKeywords[name] {
				nodes = append(nodes, cudaNode("c", name, graph.KFunc, graph.LangC, path, ln, braceEndLine(lines, i)))
			}
		case cudaExternCBlockOpenRe.MatchString(line):
			inExternCBlock = true
		case cudaStructRe.MatchString(line):
			inStruct = true
			structName = cudaStructRe.FindStringSubmatch(line)[1]
		}

		// Update the running brace depth, then latch the block/struct's own
		// interior depth the first time we see it (right after its opening
		// line has been counted), and pop out once depth falls back below
		// it (the block/struct's closing brace).
		depth += strings.Count(line, "{") - strings.Count(line, "}")

		if inExternCBlock && externCBlockDepth == 0 {
			externCBlockDepth = depth
		}
		if inStruct && structDepth == 0 {
			structDepth = depth
		}
		if inExternCBlock && depth < externCBlockDepth {
			inExternCBlock = false
			externCBlockDepth = 0
		}
		if inStruct && depth < structDepth {
			inStruct = false
			structDepth = 0
			structName = ""
		}
	}

	return ParseResult{Nodes: nodes, Edges: nil, Hash: sha256.Sum256(src)}, nil
}

// cudaNode builds one graph.Node from a minted "<lang>:<name>" ID.
func cudaNode(idLang, name string, kind graph.NodeKind, lang graph.Lang, path string, line, endLine int) graph.Node {
	return graph.Node{
		ID: graph.NodeID(ids.Mint(idLang, name)), Kind: kind, Lang: lang, File: path,
		Line: line, EndLine: endLine,
	}
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
