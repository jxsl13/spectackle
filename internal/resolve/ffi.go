package resolve

import (
	"bufio"
	"bytes"
	"context"
	"regexp"

	"github.com/jxsl13/spectacle/internal/cspan"
	"github.com/jxsl13/spectacle/internal/graph"
	"github.com/jxsl13/spectacle/internal/ids"
)

// FFIResolver links the C <-> C++ FFI boundary left dangling by cSpec's and
// cppSpec's CallRe (LSP-001): a cpp: call site invoking a name that only
// exists as a c: symbol (extern "C" declarations, C headers called from
// C++) or vice versa.
//
// Detection contract (RSV-001: edges only, no node minting):
//
//   - This mirrors — rather than imports — cSpec's/cppSpec's function Defs
//     (plain function, and cppSpec's out-of-line `Foo::bar(` method) and
//     CallRe/Stop (see internal/langspec/c.go, cpp.go): resolve is a lower
//     layer than internal/index, which internal/langspec imports, so
//     resolve cannot depend on langspec (same reason CudaResolver mirrors
//     index.cudaExternCRe instead of importing internal/index). Body-span
//     scanning itself does NOT carry this restriction: T-0054 extracted
//     that logic into internal/cspan, a leaf package with zero non-stdlib
//     imports that both langspec and resolve depend on directly, so this
//     resolver's brace-span handling (K&R, prototypes, multi-line headers,
//     Allman) is byte-for-byte the same code the primary langspec pass
//     runs — not a second, independently-maintained copy (see
//     docs/validation-ddnet.md's "## ffi re-validation" section for why
//     that duplication used to block the cpp:->c: FFI crossing entirely).
//   - For every c:/cpp: function def found this way, every call inside its
//     brace-counted body that is neither Stop-listed nor the def's own name
//     is a callee candidate exactly like the primary langspec pass. If the
//     callee already resolves within the same language (g.Node finds
//     lang:callee), there is nothing to bridge — the primary pass already
//     connected it. Only when it is dangling in its own language AND the
//     sibling language's node (siblingLang:callee) exists in g does this
//     resolver emit a NEW edge Src -> sibling, Kind ECall, same File/Line as
//     the call site. Only g.Node lookups are used — no g.Find/g.Neighbors
//     graph enumeration, no node minting; the original dangling edge from
//     the primary pass is left untouched, resolvers only add.
type FFIResolver struct{}

// Name identifies the resolver.
func (FFIResolver) Name() string { return "ffi" }

// Langs are the languages whose changes require re-running this resolver.
func (FFIResolver) Langs() []graph.Lang { return []graph.Lang{graph.LangC, graph.LangCpp} }

// ffiFuncDef is one mirrored function-def shape: a match of re on a line
// gives the def's flat name in submatch group `name`.
type ffiFuncDef struct {
	re   *regexp.Regexp
	name int
}

// ffiPlainFuncRe mirrors cSpec's and cppSpec's plain-function Def: a
// definition or prototype line ending in ';' (no body) or '{' (body starts
// here), anchored at column zero so it cannot match an indented call or
// control-flow statement (see c.go's Def comment for the full argument).
var ffiPlainFuncRe = regexp.MustCompile(`^(?:[A-Za-z_][\w\s\*]*?[\s\*])(\w+)\s*\([^;{}]*\)\s*[;{]\s*$`)

// ffiMethodRe mirrors cppSpec's out-of-line method Def (`Foo::bar(`); only
// the flat method name (group 2) is captured, same v0 limitation as cppSpec.
var ffiMethodRe = regexp.MustCompile(`^(?:[A-Za-z_][\w:&<>,\s\*]*?[\s\*])?(\w+)::(\w+)\s*\(`)

// ffiFuncDefsFor mirrors each language's ordered Def list from
// internal/langspec/c.go, cpp.go (KFunc shapes only — KType/macro defs
// cannot themselves be FFI call sites).
func ffiFuncDefsFor(lang graph.Lang) []ffiFuncDef {
	if lang == graph.LangCpp {
		return []ffiFuncDef{
			{ffiMethodRe, 2},
			{ffiPlainFuncRe, 1},
		}
	}
	return []ffiFuncDef{
		{ffiPlainFuncRe, 1},
	}
}

// ffiCallRe mirrors cSpec's/cppSpec's CallRe.
var ffiCallRe = regexp.MustCompile(`\b([A-Za-z_]\w*)\s*\(`)

// ffiStop mirrors cFamilyCallStop (internal/langspec/c.go).
var ffiStop = map[string]bool{
	"if": true, "for": true, "while": true, "switch": true, "return": true,
	"sizeof": true, "defined": true, "static_assert": true, "alignof": true,
	"typeof": true,
}

// Resolve scans every C and C++ file for function defs and bridges any call
// inside a def's body that is dangling in its own language but resolves in
// the sibling C-family language.
func (FFIResolver) Resolve(ctx context.Context, g graph.Graph, fs FileSet) ([]graph.Edge, error) {
	if fs == nil {
		return nil, nil
	}
	var edges []graph.Edge
	for _, path := range fs.ByLang(graph.LangC) {
		src, err := fs.Read(path)
		if err != nil {
			continue
		}
		edges = append(edges, ffiScanFile(g, path, src, graph.LangC, graph.LangCpp)...)
	}
	for _, path := range fs.ByLang(graph.LangCpp) {
		src, err := fs.Read(path)
		if err != nil {
			continue
		}
		edges = append(edges, ffiScanFile(g, path, src, graph.LangCpp, graph.LangC)...)
	}
	return edges, nil
}

// ffiScanFile finds every mirrored function def in src (see
// ffiFuncDefsFor) and, for each call inside its brace-counted body, bridges
// it to sibling if it is dangling in lang but resolves in sibling.
func ffiScanFile(g graph.Graph, path string, src []byte, lang, sibling graph.Lang) []graph.Edge {
	lines := ffiSplitLines(src)
	defs := ffiFuncDefsFor(lang)
	var edges []graph.Edge
	for i, line := range lines {
		var name string
		for _, def := range defs {
			m := def.re.FindStringSubmatch(line)
			if m == nil || def.name >= len(m) {
				continue
			}
			name = m[def.name]
			break
		}
		if name == "" {
			continue
		}
		end, ok := cspan.Span(lines, i)
		if !ok {
			continue // prototype / bodyless def: nothing to scan
		}
		srcID := graph.NodeID(ids.Mint(string(lang), name))
		if _, ok := g.Node(srcID); !ok {
			// Not a node the primary pass minted (e.g. a collision-disambiguated
			// duplicate name) — RSV-001 requires edges only between minted nodes.
			continue
		}
		for j := i; j <= end; j++ {
			for _, cm := range ffiCallRe.FindAllStringSubmatch(lines[j], -1) {
				callee := cm[1]
				if callee == "" || callee == name || ffiStop[callee] {
					continue
				}
				if _, ok := g.Node(graph.NodeID(ids.Mint(string(lang), callee))); ok {
					continue // resolves within its own language already
				}
				sibID := graph.NodeID(ids.Mint(string(sibling), callee))
				if _, ok := g.Node(sibID); !ok {
					continue // dangling in both languages: leave it dangling
				}
				edges = append(edges, graph.Edge{
					Src: srcID, Dst: sibID, Kind: graph.ECall,
					File: path, Line: j + 1,
				})
			}
		}
	}
	return edges
}

// ffiSplitLines mirrors langspec.scanLines' line splitting (bufio.Scanner +
// ScanLines); scan errors are ignored, matching resolveCudaFile's style —
// resolvers skip bad input rather than erroring the whole run.
func ffiSplitLines(src []byte) []string {
	sc := bufio.NewScanner(bytes.NewReader(src))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var lines []string
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	return lines
}
