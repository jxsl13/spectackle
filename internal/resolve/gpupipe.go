package resolve

import (
	"bufio"
	"bytes"
	"context"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/jxsl13/spectackle/internal/graph"
	"github.com/jxsl13/spectackle/internal/ids"
)

// GpuPipeResolver links pipeline-based GPU dispatch (Metal, Vulkan) where the
// binding is by name at runtime, not by symbol (edge kind ELaunch, best
// effort).
//
// Detection contract (Metal implemented, Vulkan future):
//
//   - Metal: a string literal "kern_name" appearing as the argument of
//     newFunctionWithName: (ObjC) is matched against [[kernel]] entry points
//     of the same name in .metal files -> ELaunch edge from the enclosing host
//     function to "msl:<name>".
//   - Vulkan: host code binds a compiled SPIR-V module blob by FILE PATH via
//     vkCreateShaderModule and names the entry point through
//     VkPipelineShaderStageCreateInfo.pName (almost always the literal "main") —
//     unlike Metal's newFunctionWithName:@"<kernelName>", there is no
//     host-side kernel-name literal to string-match against a glsl entry point.
//     A meaningful Vulkan launch edge needs module-provenance tracking (which
//     .spv/.comp compiles into which module handle), a separate harder problem
//     than the line-scanner name-match this resolver does for Metal. GLSL shader
//     SOURCE is now parsed (glsl: nodes, P-0046) so the target nodes exist;
//     only the host->module binding is unresolved.
//   - These edges are heuristic string matches (RSV-002): the resolver
//     emits edges only — it never mints or mutates nodes (RSV-001).
type GpuPipeResolver struct{}

func (GpuPipeResolver) Name() string { return "gpupipe" }

func (GpuPipeResolver) Langs() []graph.Lang {
	return []graph.Lang{graph.LangGo, graph.LangObjC, graph.LangMSL}
}

var (
	// objcMethodRe matches Objective-C method definitions.
	// Captures the method name (first selector segment).
	objcMethodRe = regexp.MustCompile(`^\s*[-+]\s*\([^)]*\)\s*(\w+)`)

	// cFunctionRe matches C function definitions in .m files.
	// Captures the function name.
	cFunctionRe = regexp.MustCompile(`^\s*(?:static\s+)?(?:[A-Za-z_][\w\s*]*?)\s+(\w+)\s*\(([^;{]*)\)\s*\{`)

	// launchRe matches the newFunctionWithName: pattern for Metal kernel dispatch.
	// Captures the kernel name.
	launchRe = regexp.MustCompile(`newFunctionWithName:\s*@"(\w+)"`)
)

// launchKeyGpu dedupes edges per (host, kernel, file).
type launchKeyGpu struct {
	host, kernel, file string
}

// Resolve scans every Objective-C source file for host function bodies and the
// Metal kernel launches inside them.
func (GpuPipeResolver) Resolve(ctx context.Context, g graph.Graph, fs FileSet) ([]graph.Edge, error) {
	if fs == nil {
		return nil, nil
	}
	var edges []graph.Edge
	seen := map[launchKeyGpu]bool{}
	for _, path := range fs.ByLang(graph.LangObjC) {
		src, err := fs.Read(path)
		if err != nil {
			continue
		}
		edges = append(edges, resolveObjCFile(path, src, seen)...)
	}
	return edges, nil
}

// resolveObjCFile scans one file line by line, tracking the currently open
// host function (ObjC method or C function) via brace depth and emitting an
// ELaunch edge for every Metal kernel launch found inside it.
func resolveObjCFile(path string, src []byte, seen map[launchKeyGpu]bool) []graph.Edge {
	var edges []graph.Edge

	sc := bufio.NewScanner(bytes.NewReader(src))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var lines []string
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}

	host := "" // name of the currently open host function, "" if none
	depth := 0 // brace depth since the host's opening line
	inHost := false
	bodyNotYetOpened := false // special flag for ObjC methods with brace on next line

	for i, line := range lines {
		lineNo := i + 1
		if !inHost {
			// Try to match ObjC method
			if m := objcMethodRe.FindStringSubmatch(line); m != nil {
				host = m[1]
				inHost = true
				bodyNotYetOpened = true
				depth = braceDelta(line)
				if depth > 0 {
					bodyNotYetOpened = false
				}
				if depth <= 0 {
					// Single-line method (rare but possible)
					inHost = false
					host = ""
					bodyNotYetOpened = false
				}
				continue
			}
			// Try to match C function
			if m := cFunctionRe.FindStringSubmatch(line); m != nil {
				host = m[1]
				inHost = true
				bodyNotYetOpened = false
				depth = braceDelta(line)
				if depth <= 0 {
					// single-line definition (rare)
					inHost = false
					host = ""
				}
				continue
			}
		} else {
			// Inside a host body
			if lm := launchRe.FindAllStringSubmatchIndex(line, -1); lm != nil {
				for _, idx := range lm {
					kernel := line[idx[2]:idx[3]]
					key := launchKeyGpu{host, kernel, path}
					if seen[key] {
						continue
					}
					seen[key] = true
					stem := filepath.Base(path)
					// Remove extension
					stem = strings.TrimSuffix(stem, filepath.Ext(stem))
					edges = append(edges, graph.Edge{
						Src:  graph.NodeID(ids.Mint("objc", stem+"."+host)),
						Dst:  graph.NodeID(ids.Mint("msl", kernel)),
						Kind: graph.ELaunch,
						File: path,
						Line: lineNo,
					})
				}
			}

			// Update depth and check if host body closes
			delta := braceDelta(line)
			if bodyNotYetOpened && delta > 0 {
				bodyNotYetOpened = false
			}
			depth += delta
			if !bodyNotYetOpened && depth <= 0 {
				inHost = false
				host = ""
				bodyNotYetOpened = false
			}
		}
	}
	return edges
}
