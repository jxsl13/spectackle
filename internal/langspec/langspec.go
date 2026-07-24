// Package langspec is a data-driven framework for line-oriented language
// parsers: a language is defined by exactly one Spec value (a set of regex
// Defs plus a qualification mode), not by a new Go type. Adding a language
// to spectackle means adding one Spec (see python.go, javascript.go) — the
// indexing pipeline (internal/index) is never touched (SPX-LSP-001).
//
// SpecParser adapts a Spec to index.LanguageParser so it plugs into
// index.New's parsers slice exactly like a hand-written parser (see
// index.CudaParser, index.AsmParser). Parsing is a single-pass line scan:
// no AST, no cgo, no external grammar — deliberately the cheapest possible
// backend for languages where an approximate symbol table (function/class
// definitions, not full semantics) is enough for the cross-language graph.
// Parsing is deterministic: identical bytes yield identical results
// (SPX-GRA-001).
package langspec

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/jxsl13/spectackle/internal/cspan"
	"github.com/jxsl13/spectackle/internal/graph"
	"github.com/jxsl13/spectackle/internal/ids"
	"github.com/jxsl13/spectackle/internal/index"
)

// QualMode selects how a matched symbol name is qualified into the
// "<lang>:<qualified-name>" NodeID (see internal/ids).
type QualMode uint8

const (
	// QualFileStem qualifies with the file's base name minus extension,
	// e.g. app.py's `def run` -> "py:app.run". Matches Python's one file
	// == one importable module convention.
	QualFileStem QualMode = iota
	// QualDirPkg qualifies with the containing directory's base name,
	// e.g. mat/ops.s's TEXT ·mulVec -> "asm:mat.mulVec" (see
	// index.AsmParser, which this mode mirrors for source layouts where
	// a directory is the unit of namespacing, not the file).
	QualDirPkg
	// QualFlat uses the bare matched name with no qualification prefix,
	// e.g. a CUDA kernel -> "cu:saxpy_kernel" (see index.CudaParser).
	QualFlat
)

// Def is one regex-driven symbol shape within a Spec: a line matching Re
// mints a node of Kind, with Re's submatch group Name giving the symbol
// name and (optionally) submatch group Sig giving a compact signature.
type Def struct {
	Kind graph.NodeKind
	Re   *regexp.Regexp
	Name int // submatch group index (>=1) holding the symbol name
	Sig  int // submatch group index (>=1) holding Node.Sig; 0 = no signature
}

// Spec is the whole definition of a language for langspec purposes: the
// language tag, the file extensions it claims, how matched names are
// qualified into NodeIDs, and the ordered list of regex Defs tried against
// each source line. One Spec value == one language; see python.go and
// javascript.go for the reference specs this framework ships with.
type Spec struct {
	Lang graph.Lang
	Exts []string
	Qual QualMode
	Defs []Def

	// CallRe is the optional call-site regex for LSP-001: capture group 1 is
	// the callee name. Unset (nil) is the framework default and means zero
	// behavior change from before this field existed — Parse emits no edges,
	// exactly like every language that doesn't set it (see
	// TestSpecParserNoEdgesWithoutCallRe). Set it only for brace languages
	// where a def's body is delimited by '{'/'}' (see c.go, cpp.go); a
	// language with no braces has nothing for the brace-counted body span to
	// bound CallRe to, so it simply leaves CallRe nil.
	CallRe *regexp.Regexp
	// Stop lists callee names CallRe must never turn into a call edge —
	// language keywords/operators whose syntax happens to look like a call
	// (`if (`, `sizeof(`, ...). Only consulted when CallRe is set.
	Stop []string
}

// SpecParser adapts a Spec to index.LanguageParser.
type SpecParser struct {
	S Spec
}

// Lang identifies the parser's language.
func (p SpecParser) Lang() graph.Lang { return p.S.Lang }

// Extensions are the file suffixes this parser claims.
func (p SpecParser) Extensions() []string { return p.S.Exts }

// Parse scans one source file line by line, trying each Def in order
// against every line and minting a node per match. When the Spec sets
// CallRe, each KFunc/KMethod hit also gets its body span brace-counted and
// scanned for call edges (LSP-001: "WHEN a Spec sets CallRe, the SpecParser
// SHALL emit ECall edges from each Def's brace-counted body span"). With
// CallRe unset — the default — no edges are emitted and EndLine stays equal
// to Line, exactly as before this field existed: same-language and
// cross-language relations are otherwise a resolver's job (see
// internal/resolve), not a langspec.Spec's.
func (p SpecParser) Parse(path string, src []byte) (index.ParseResult, error) {
	lines, err := scanLines(src)
	if err != nil {
		return index.ParseResult{}, err
	}

	var stop map[string]bool
	if p.S.CallRe != nil {
		stop = make(map[string]bool, len(p.S.Stop))
		for _, s := range p.S.Stop {
			stop[s] = true
		}
	}

	var nodes []graph.Node
	var edges []graph.Edge
	for i, line := range lines {
		lineNo := i + 1
		for _, def := range p.S.Defs {
			m := def.Re.FindStringSubmatch(line)
			if m == nil || def.Name >= len(m) {
				continue
			}
			name := m[def.Name]
			if name == "" {
				continue
			}
			sig := ""
			if def.Sig > 0 && def.Sig < len(m) {
				sig = m[def.Sig]
			}
			id := graph.NodeID(ids.Mint(string(p.S.Lang), p.qualify(path, name)))
			endLine := lineNo
			if p.S.CallRe != nil && (def.Kind == graph.KFunc || def.Kind == graph.KMethod) {
				if bodyEnd, ok := cspan.Span(lines, i); ok {
					endLine = bodyEnd + 1
					edges = append(edges, p.callEdges(id, name, path, lines, i, bodyEnd, stop)...)
				}
			}
			nodes = append(nodes, graph.Node{
				ID: id, Kind: def.Kind, Lang: p.S.Lang, File: path,
				Line: lineNo, EndLine: endLine, Sig: sig,
			})
		}
	}

	return index.ParseResult{Nodes: nodes, Edges: edges, Hash: sha256.Sum256(src)}, nil
}

// scanLines splits src into its lines the same way the pre-LSP-001 Parse did
// (bufio.Scanner + ScanLines, 1MiB max line length), just materialized into
// a slice up front so brace-span computation can look ahead from any def
// line instead of only forward from a single streaming cursor.
func scanLines(src []byte) ([]string, error) {
	sc := bufio.NewScanner(bytes.NewReader(src))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var lines []string
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return lines, nil
}

// callEdges applies Spec.CallRe to every line in [start, end] (a Def hit's
// brace-counted body span, inclusive of the def line itself) and emits one
// ECall edge per callee that is neither Stop-listed nor the def's own name —
// the own-name check catches both the def line's self-match (e.g. `foo(...)
// {` also matches CallRe as a call to "foo") and direct recursion in the
// body. Destinations are minted in the same language and may be dangling,
// exactly like Go's syntactic pass — Impact tolerates it, and resolvers
// (e.g. internal/resolve.FFIResolver) may later bridge them.
func (p SpecParser) callEdges(defID graph.NodeID, defName, path string, lines []string, start, end int, stop map[string]bool) []graph.Edge {
	var edges []graph.Edge
	for i := start; i <= end; i++ {
		for _, m := range p.S.CallRe.FindAllStringSubmatch(lines[i], -1) {
			if len(m) < 2 {
				continue
			}
			callee := m[1]
			if callee == "" || callee == defName || stop[callee] {
				continue
			}
			edges = append(edges, graph.Edge{
				Src:  defID,
				Dst:  graph.NodeID(ids.Mint(string(p.S.Lang), callee)),
				Kind: graph.ECall,
				File: path,
				Line: i + 1,
			})
		}
	}
	return edges
}

// qualify builds the qualified name (everything after "<lang>:" in the
// minted NodeID) for a matched symbol name, per the Spec's QualMode.
func (p SpecParser) qualify(path, name string) string {
	switch p.S.Qual {
	case QualFileStem:
		base := filepath.Base(path)
		stem := strings.TrimSuffix(base, filepath.Ext(base))
		return stem + "." + name
	case QualDirPkg:
		dirbase := filepath.Base(filepath.Dir(path))
		if dirbase == "." {
			return name
		}
		return dirbase + "." + name
	default: // QualFlat
		return name
	}
}

// registry lists every Spec this framework ships with. Adding a language
// means appending its Spec here (and defining it in its own file) — no
// other file in the repository changes.
var registry = []Spec{
	pythonSpec,
	javascriptSpec,
}

// All returns a SpecParser for every registered Spec, ready to append to
// an index.New parsers slice alongside hand-written parsers such as
// index.GoParser.
func All() []index.LanguageParser {
	parsers := make([]index.LanguageParser, 0, len(registry))
	for _, s := range registry {
		parsers = append(parsers, SpecParser{S: s})
	}
	return parsers
}
