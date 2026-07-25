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
	"encoding/hex"
	"path/filepath"
	"regexp"
	"strconv"
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

	// EndSpan is the optional keyword-counting alternative to brace-counted
	// body spans, for end-terminated languages (Lua, Ruby, and similar,
	// where a def's body is closed by a keyword like "end" rather than by
	// '}'). Unset (nil) is the framework default and means zero behavior
	// change from before this field existed — Parse bounds a KFunc/KMethod
	// def's body with cspan.Span exactly as it always has (see CallRe's doc
	// for the same nil-means-unchanged guarantee this mirrors). Set it only
	// for languages with no braces at all; a brace language leaves EndSpan
	// nil and CallRe (if set) drives cspan.Span as before.
	EndSpan *EndSpanSpec
}

// EndSpanSpec is Spec.EndSpan's shape: the keyword regexes cspan.KeywordSpan
// depth-counts to find where a def's body ends, standing in for the
// '{'/'}' pair that cspan.Span counts for brace languages. Open matches
// increment depth, Close matches decrement it; see cspan.KeywordSpan's doc
// for the exact counting rules (per-line net sum, def line included).
type EndSpanSpec struct {
	Open  *regexp.Regexp
	Close *regexp.Regexp
}

// SpecParser adapts a Spec to index.LanguageParser.
type SpecParser struct {
	S Spec
}

// spanEligibleKinds is the set of graph.NodeKinds whose Def hits Parse gives
// a multi-line body span to and — when the Spec sets CallRe — scans for call
// edges. It is the named replacement for the hardcoded `KFunc || KMethod`
// gate that predated every kernel-bearing langspec language (B-0008).
//
// The membership rule is "the kind denotes a callable unit: a named body of
// executable statements whose calls belong to it". Admitted:
//   - KFunc, KMethod — the original pair, unchanged.
//   - KKernel — B-0008 proper. A metal `kernel`/`vertex`/`fragment` or glsl
//     entry point is an ordinary brace body that merely carries a distinct
//     kind because it is dispatched rather than called; T-0122 observed
//     msl:shade_pixels -> msl:computeNormal never minting on the gap-metal
//     fixture despite metalSpec setting CallRe. Nothing about the scan
//     differs for a kernel, so nothing justified excluding it.
//
// Deliberately still excluded, having been checked rather than inherited:
//   - KType — a class/struct/trait/enum is a *container*, not a callable
//     unit: the calls inside it belong to its member functions, which mint
//     their own KFunc/KMethod nodes and their own edges. Admitting it was
//     tried and rejected — it makes a C++ `class Shape { virtual double
//     area() = 0; }` mint a bogus cpp:Shape -> cpp:area ECall (the exact
//     case TestCppSpecNoEdges pins) and moves the EndLine of every class in
//     ~15 languages off the contract their per-language tests assert.
//   - KVar — a binding, not a unit of execution. The registry's only KVar
//     declarer (rustSpec's const/static Def) requires a `NAME:` type
//     annotation and therefore always matches a `;`-terminated line, which
//     cspan.Span already refuses; admitting it would buy nothing observable
//     and widen the gate on speculation.
//   - KUnknown is the zero value and names nothing; KFile and KDir are
//     synthetic container nodes the indexer mints, never a source line a Def
//     matches; KAsmProc belongs to the hand-written index.AsmParser, whose
//     TEXT procedures have neither braces nor an end keyword to count.
//
// Registry invariant C (registry_test.go) holds every kind a CallRe-setting
// Spec declares against this set plus that reviewed exclusion list, so a
// language introducing some *other* kind fails loudly instead of silently
// minting no edges the way KKernel did.
//
// Widening is safe by construction rather than by luck: cspan.Span already
// returns ok=false for any def line with no brace body (prototypes,
// `;`-terminated lines, braceless declarations) and cspan.KeywordSpan does
// the same when the open keyword does not match, so the kind gate was never
// what kept a braceless def from wandering into the next one — it only ever
// hid bodies that do exist.
var spanEligibleKinds = [...]graph.NodeKind{
	graph.KFunc, graph.KMethod, graph.KKernel,
}

// spanEligible reports whether Def hits of this kind get a body span and,
// with CallRe set, call edges. See spanEligibleKinds for the membership
// rationale.
func spanEligible(k graph.NodeKind) bool {
	for _, e := range spanEligibleKinds {
		if k == e {
			return true
		}
	}
	return false
}

// Lang identifies the parser's language.
func (p SpecParser) Lang() graph.Lang { return p.S.Lang }

// CacheVersion implements index.CacheVersioner by digesting the Spec itself.
// A langspec language IS its data, so the data is the version: edit a regex
// and every cached parse blob for that language — and only that language —
// stops matching, instead of replaying pre-edit nodes for unchanged files
// (B-0007). Nothing to bump by hand, which matters because a Spec edit is
// the single most common change in this package.
//
// Every input is walked in declaration order and never through a map, so the
// digest is stable across processes; the nil markers keep an unset CallRe or
// EndSpan from colliding with a set one that happens to stringify empty.
func (p SpecParser) CacheVersion() string {
	h := sha256.New()
	writeField := func(parts ...string) {
		for _, s := range parts {
			h.Write([]byte(s))
			h.Write([]byte{0})
		}
	}
	writeField(string(p.S.Lang), strconv.Itoa(int(p.S.Qual)))
	for _, e := range p.S.Exts {
		writeField(e)
	}
	for _, d := range p.S.Defs {
		writeField(strconv.Itoa(int(d.Kind)), d.Re.String(),
			strconv.Itoa(d.Name), strconv.Itoa(d.Sig))
	}
	if p.S.CallRe != nil {
		writeField("call", p.S.CallRe.String())
	} else {
		writeField("call-nil")
	}
	for _, s := range p.S.Stop {
		writeField(s)
	}
	if p.S.EndSpan != nil {
		writeField("endspan", p.S.EndSpan.Open.String(), p.S.EndSpan.Close.String())
	} else {
		writeField("endspan-nil")
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Extensions are the file suffixes this parser claims.
func (p SpecParser) Extensions() []string { return p.S.Exts }

// Parse scans one source file line by line, trying each Def in order
// against every line and minting a node per match. When the Spec sets
// CallRe, each callable-unit hit — KFunc, KMethod or KKernel, see
// spanEligibleKinds for why those three and not the rest — also gets its
// body span scanned for call edges (LSP-001: "WHEN a Spec sets CallRe, the
// SpecParser SHALL emit ECall edges from each Def's brace-counted body
// span"): the body span itself is
// brace-counted via cspan.Span, unless the Spec sets EndSpan, in which case
// it is keyword-counted via cspan.KeywordSpan instead (for end-terminated
// languages with no braces at all). With CallRe unset — the default — no
// edges are emitted and EndLine stays equal to Line, exactly as before this
// field existed: same-language and cross-language relations are otherwise a
// resolver's job (see internal/resolve), not a langspec.Spec's.
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
			if p.S.CallRe != nil && spanEligible(def.Kind) {
				var bodyEnd int
				var ok bool
				if p.S.EndSpan != nil {
					bodyEnd, ok = cspan.KeywordSpan(lines, i, p.S.EndSpan.Open, p.S.EndSpan.Close)
				} else {
					bodyEnd, ok = cspan.Span(lines, i)
				}
				if ok {
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
// qualified per the Spec QualMode (QualFlat: bare name, unchanged; QualFileStem: same-file resolution, cross-file dangles),
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
				Dst:  graph.NodeID(ids.Mint(string(p.S.Lang), p.qualify(path, callee))),
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
