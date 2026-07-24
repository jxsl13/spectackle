package resolve

import (
	"bufio"
	"bytes"
	"context"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/jxsl13/spectackle/internal/graph"
	"github.com/jxsl13/spectackle/internal/ids"
)

// VulkanResolver links Vulkan compute/graphics dispatch (edge kind ELaunch)
// where the binding is neither a symbol reference (cgo/cuda) nor a
// host-side kernel-name string literal (Metal/gpupipe, see gpupipe.go):
// Vulkan host code loads a compiled SPIR-V blob by FILE PATH via
// vkCreateShaderModule and names the entry point through
// VkPipelineShaderStageCreateInfo.pName — almost always the literal "main".
// Recovering the binding therefore requires tracing module *provenance*
// across three statements in one host file:
//
//  1. BLOB   a string literal ending in ".spv" is bound to an identifier
//     (its assignment target, if the line has one) or, absent one,
//     remembered as the file's most recent "pending" blob. The blob's
//     *stem* — its basename with every extension stripped
//     ("blur.comp.spv" -> "blur") — is what ties it back to the glsl
//     source node the primary indexing pass minted from "blur.comp".
//  2. MODULE a vkCreateShaderModule(...) call binds its output module
//     identifier (the call's last argument, leading '&' stripped) to a
//     blob stem: the stem bound to the createInfo's pCode identifier if
//     traceable, else the file's pending blob stem.
//  3. STAGE  a VkPipelineShaderStageCreateInfo struct literal whose
//     .module field names a bound module identifier and whose .pName
//     field carries a non-empty entry-point literal resolves to
//     (stem, entry). There is no default pName: an omitted or empty
//     pName means no edge (RSV-004) — this resolver never assumes "main".
//
// (stem, entry) is then resolved to a real glsl: node BY GRAPH LOOKUP
// (graph.Graph.Find), never by ids.Mint: GLSL is QualFlat
// (internal/langspec/glsl.go), so every shader's `void main()` mints the
// same base ID "glsl:main" and the indexer disambiguates same-named defs
// across files with "~2", "~3", ... in file-path order (internal/ids).
// Minting "glsl:<entry>" directly would silently target the first-indexed
// shader by that name and wire every other same-named shader's launch to
// the wrong node — RSV-001 also forbids resolvers from minting nodes
// outright; only the parser passes do that.
//
// Ambiguity is not an edge (RSV-004): if graph lookup finds zero or more
// than one glsl node matching (stem, entry), this resolver emits nothing
// for that dispatch rather than guessing.
type VulkanResolver struct{}

func (VulkanResolver) Name() string { return "vulkan" }

func (VulkanResolver) Langs() []graph.Lang {
	return []graph.Lang{graph.LangC, graph.LangCpp, graph.LangGo, graph.LangGLSL}
}

var (
	// vkSpvLitRe matches a string literal ending in ".spv", single or
	// double quoted, e.g. `"shaders/blur.comp.spv"`, `'blur.spv'`.
	vkSpvLitRe = regexp.MustCompile(`["']([^"']*\.spv)["']`)

	// vkCreateShaderModuleRe matches a vkCreateShaderModule(...) call,
	// capturing its full argument list (top-level comma split by
	// splitTopLevelArgs) so nested casts/struct literals in the common
	// one-liner form don't break parsing.
	vkCreateShaderModuleRe = regexp.MustCompile(`vkCreateShaderModule\s*\(([^;]*)\)\s*;?`)

	// vkPCodeRe matches a `.pCode = <ident>` (C) or `pCode: <ident>` (Go
	// binding) field assignment inside a VkShaderModuleCreateInfo
	// initializer, tolerating an intervening cast like `(uint32_t*)`.
	vkPCodeRe = regexp.MustCompile(`\bpCode\s*[:=]\s*(?:\([^)]*\)\s*)?&?(\w+)`)

	// vkStageTriggerRe marks the start of a VkPipelineShaderStageCreateInfo
	// struct literal.
	vkStageTriggerRe = regexp.MustCompile(`VkPipelineShaderStageCreateInfo`)

	// vkModuleFieldRe matches `.module = <ident>` (C) or `module: <ident>`
	// (Go) inside a stage struct literal.
	vkModuleFieldRe = regexp.MustCompile(`\bmodule\s*[:=]\s*&?(\w+)`)

	// vkPNameFieldRe matches `.pName = "entry"` (C) or `pName: "entry"`
	// (Go). No default: an empty capture ("") is treated as absent.
	vkPNameFieldRe = regexp.MustCompile(`\bpName\s*[:=]\s*"(\w*)"`)

	// vkGoFuncRe matches a Go function or method definition whose body
	// opens on this line, mirroring cFunctionRe's shape (defined in
	// gpupipe.go) for Go syntax. Group 1 is the receiver type (methods
	// only), group 2 the function name.
	vkGoFuncRe = regexp.MustCompile(`^\s*func\s+(?:\(\s*\w+\s+\*?(\w+)\s*\)\s+)?(\w+)\s*\([^)]*\)[^{]*\{\s*$`)

	// vkGoPackageRe matches a Go package clause, scanned once per Go file
	// to package-qualify host IDs the way goFuncID (cgo.go) does.
	vkGoPackageRe = regexp.MustCompile(`^\s*package\s+(\w+)\s*$`)

	// vkIdentTailRe pulls the trailing identifier off an assignment's LHS
	// text, e.g. "const char *p" -> "p".
	vkIdentTailRe = regexp.MustCompile(`\w+$`)
)

// vkLaunchKey dedupes edges per (host, target, file).
type vkLaunchKey struct {
	src, dst, file string
}

// Resolve scans every C, C++ and Go source file for the BLOB -> MODULE ->
// STAGE provenance chain described in the type doc and emits one ELaunch
// edge per resolved dispatch. Edges are sorted before returning so the
// result is deterministic regardless of FileSet.ByLang iteration order
// (map-backed FileSet implementations, including tests, do not guarantee
// order).
func (VulkanResolver) Resolve(ctx context.Context, g graph.Graph, fs FileSet) ([]graph.Edge, error) {
	if fs == nil {
		return nil, nil
	}
	var edges []graph.Edge
	seen := map[vkLaunchKey]bool{}
	for _, lang := range []graph.Lang{graph.LangC, graph.LangCpp, graph.LangGo} {
		for _, path := range fs.ByLang(lang) {
			src, err := fs.Read(path)
			if err != nil {
				continue
			}
			edges = append(edges, resolveVulkanFile(g, lang, path, src, seen)...)
		}
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].Src != edges[j].Src {
			return edges[i].Src < edges[j].Src
		}
		if edges[i].Dst != edges[j].Dst {
			return edges[i].Dst < edges[j].Dst
		}
		if edges[i].File != edges[j].File {
			return edges[i].File < edges[j].File
		}
		return edges[i].Line < edges[j].Line
	})
	return edges, nil
}

// resolveVulkanFile runs the single forward line scan described in the
// VulkanResolver doc over one host file, tracking the currently open host
// function (brace-depth walk, same shape as gpupipe.go/cuda.go) plus a tiny
// per-file symbol table for blob stems and module identifiers.
func resolveVulkanFile(g graph.Graph, lang graph.Lang, path string, src []byte, seen map[vkLaunchKey]bool) []graph.Edge {
	lines := vkSplitLines(src)

	goPkg := ""
	if lang == graph.LangGo {
		for _, l := range lines {
			if m := vkGoPackageRe.FindStringSubmatch(l); m != nil {
				goPkg = m[1]
				break
			}
		}
	}

	var edges []graph.Edge

	blobs := map[string]string{}   // identifier -> spv stem
	modules := map[string]string{} // module identifier -> spv stem
	pendingBlob := ""              // most recent unbound spv stem
	pendingPCode := ""             // most recent VkShaderModuleCreateInfo pCode identifier

	var hostID graph.NodeID
	inHost := false
	depth := 0

	inStage := false
	stageDepth := 0
	stageLine := 0
	stageModule := ""
	stagePName := ""
	stagePNameOK := false

	for i, line := range lines {
		lineNo := i + 1

		if !inHost {
			if id, _, ok := vkHostHeader(lang, goPkg, line); ok {
				hostID = id
				inHost = true
				depth = braceDelta(line)
				if depth <= 0 {
					// single-line definition (rare) — never actually open.
					inHost = false
				}
				continue
			}
		}

		// Step 1: BLOB.
		if lm := vkSpvLitRe.FindStringSubmatchIndex(line); lm != nil {
			lit := line[lm[2]:lm[3]]
			stem := vkStem(lit)
			if ident := vkAssignTarget(line[:lm[0]]); ident != "" {
				blobs[ident] = stem
			} else {
				pendingBlob = stem
			}
		}

		// Track the createInfo's pCode identifier (feeds step 2).
		if m := vkPCodeRe.FindStringSubmatch(line); m != nil {
			pendingPCode = m[1]
		}

		// Step 2: MODULE.
		if m := vkCreateShaderModuleRe.FindStringSubmatch(line); m != nil {
			args := splitTopLevelArgs(m[1])
			if len(args) > 0 {
				last := strings.TrimPrefix(strings.TrimSpace(args[len(args)-1]), "&")
				stem := ""
				if pendingPCode != "" {
					if s, ok := blobs[pendingPCode]; ok {
						stem = s
					}
				}
				if stem == "" {
					stem = pendingBlob
				}
				if stem != "" && last != "" {
					modules[last] = stem
				}
			}
			// Each vkCreateShaderModule call consumes the pending provenance;
			// a later call with no traceable blob must not silently inherit
			// an earlier, unrelated one (RSV-004).
			pendingBlob = ""
			pendingPCode = ""
		}

		// Step 3: STAGE.
		if !inStage {
			if vkStageTriggerRe.MatchString(line) {
				if d := braceDelta(line); d > 0 {
					inStage = true
					stageDepth = d
					stageLine = lineNo
					stageModule, stagePName, stagePNameOK = "", "", false
					vkScanStageFields(line, &stageModule, &stagePName, &stagePNameOK)
					if stageDepth <= 0 {
						edges = vkFinalizeStage(g, edges, seen, inHost, hostID, path, stageLine, stageModule, stagePName, stagePNameOK, modules)
						inStage = false
					}
				}
			}
		} else {
			vkScanStageFields(line, &stageModule, &stagePName, &stagePNameOK)
			stageDepth += braceDelta(line)
			if stageDepth <= 0 {
				edges = vkFinalizeStage(g, edges, seen, inHost, hostID, path, stageLine, stageModule, stagePName, stagePNameOK, modules)
				inStage = false
			}
		}

		if inHost {
			depth += braceDelta(line)
			if depth <= 0 {
				inHost = false
			}
		}
	}
	return edges
}

// vkHostHeader tries to match line as the opening line of a host function
// definition for lang, returning the host's node ID and display name.
func vkHostHeader(lang graph.Lang, goPkg, line string) (graph.NodeID, string, bool) {
	if lang == graph.LangGo {
		m := vkGoFuncRe.FindStringSubmatch(line)
		if m == nil {
			return "", "", false
		}
		recv, name := m[1], m[2]
		qual := goPkg + "." + name
		if recv != "" {
			qual = goPkg + "." + recv + "." + name
		}
		return graph.NodeID(ids.Mint("go", qual)), qual, true
	}
	m := cFunctionRe.FindStringSubmatch(line)
	if m == nil {
		return "", "", false
	}
	langTag := "c"
	if lang == graph.LangCpp {
		langTag = "cpp"
	}
	return graph.NodeID(ids.Mint(langTag, m[1])), m[1], true
}

// vkScanStageFields updates module/pName accumulators from one line inside
// a VkPipelineShaderStageCreateInfo struct literal span.
func vkScanStageFields(line string, module, pname *string, pnameOK *bool) {
	if m := vkModuleFieldRe.FindStringSubmatch(line); m != nil {
		*module = m[1]
	}
	if m := vkPNameFieldRe.FindStringSubmatch(line); m != nil {
		*pname = m[1]
		*pnameOK = true
	}
}

// vkFinalizeStage evaluates one completed stage struct literal and, if the
// full BLOB -> MODULE -> STAGE chain resolved to exactly one glsl node,
// appends the deduplicated ELaunch edge.
func vkFinalizeStage(g graph.Graph, edges []graph.Edge, seen map[vkLaunchKey]bool, inHost bool, hostID graph.NodeID, path string, line int, module, pname string, pnameOK bool, modules map[string]string) []graph.Edge {
	if !inHost || module == "" || !pnameOK || pname == "" {
		return edges
	}
	stem, ok := modules[module]
	if !ok {
		return edges
	}
	dst, ok := resolveGlslTarget(g, stem, pname)
	if !ok {
		return edges
	}
	key := vkLaunchKey{string(hostID), string(dst), path}
	if seen[key] {
		return edges
	}
	seen[key] = true
	return append(edges, graph.Edge{
		Src:  hostID,
		Dst:  dst,
		Kind: graph.ELaunch,
		File: path,
		Line: line,
	})
}

// resolveGlslTarget finds the single glsl node named entry whose file stem
// equals stem, by graph lookup (RSV-003) — never by ids.Mint (see the
// VulkanResolver type doc for why minting would be wrong here).
func resolveGlslTarget(g graph.Graph, stem, entry string) (graph.NodeID, bool) {
	if g == nil {
		return "", false
	}
	cands := g.Find(entry, 64, graph.KFunc)
	var keep []graph.Node
	for _, n := range cands {
		if n.Lang != graph.LangGLSL {
			continue
		}
		if vkBareName(n.ID) != entry {
			continue
		}
		if vkStem(n.File) != stem {
			continue
		}
		keep = append(keep, n)
	}
	if len(keep) != 1 {
		return "", false
	}
	return keep[0].ID, true
}

// vkBareName strips a NodeID's "~N" collision-disambiguation suffix
// (internal/ids), so a shader's ID "glsl:main~2" still compares equal to
// entry point "main".
func vkBareName(id graph.NodeID) string {
	_, qual, err := ids.Parse(string(id))
	if err != nil {
		return ""
	}
	if i := strings.LastIndex(qual, "~"); i >= 0 {
		if _, err := strconv.Atoi(qual[i+1:]); err == nil {
			return qual[:i]
		}
	}
	return qual
}

// vkStem strips the directory and every extension from a path:
// "shaders/blur.comp.spv" -> "blur", "blur.comp" -> "blur".
func vkStem(path string) string {
	name := filepath.Base(path)
	for {
		ext := filepath.Ext(name)
		if ext == "" {
			break
		}
		name = strings.TrimSuffix(name, ext)
	}
	return name
}

// vkAssignTarget returns the identifier being assigned to on a line, given
// the text preceding a matched literal: "code = " -> "code",
// "const char *p = " -> "p". "" means no assignment target on this line
// (the caller should treat the literal as a pending, unbound blob).
func vkAssignTarget(prefix string) string {
	idx := -1
	for i := len(prefix) - 1; i >= 0; i-- {
		if prefix[i] != '=' {
			continue
		}
		if i > 0 {
			switch prefix[i-1] {
			case '=', '!', '<', '>':
				continue
			}
		}
		if i+1 < len(prefix) && prefix[i+1] == '=' {
			continue
		}
		idx = i
		break
	}
	if idx < 0 {
		return ""
	}
	before := strings.TrimRight(prefix[:idx], " \t")
	return vkIdentTailRe.FindString(before)
}

// splitTopLevelArgs splits a call's argument-list text on commas that are
// not nested inside (), {} or [] — enough to pull the last argument out of
// the common Vulkan one-liner where the createInfo struct is built inline.
func splitTopLevelArgs(s string) []string {
	var args []string
	depth := 0
	start := 0
	for i, r := range s {
		switch r {
		case '(', '{', '[':
			depth++
		case ')', '}', ']':
			depth--
		case ',':
			if depth == 0 {
				args = append(args, s[start:i])
				start = i + 1
			}
		}
	}
	args = append(args, s[start:])
	return args
}

// vkSplitLines mirrors ffiSplitLines/resolveCudaFile's line splitting.
func vkSplitLines(src []byte) []string {
	sc := bufio.NewScanner(bytes.NewReader(src))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var lines []string
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	return lines
}
