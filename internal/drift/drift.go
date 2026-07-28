// Package drift binds EARS rules to code spans and detects when either side
// moves out from under the other. Bindings live in the root-only, versioned
// .spectackle/anchors.tsv; each row records a content hash of the normalized
// code span (not its position), so pure line shifts are drift-free — the
// server silently refreshes file/span when only the position moved.
package drift

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"

	"github.com/jxsl13/spectackle/internal/graph"
	"github.com/jxsl13/spectackle/internal/workspace"
)

// Anchor is one rule<->node binding.
type Anchor struct {
	Rule  string
	Node  graph.NodeID
	File  string // repo-relative; "-" = pending (node was unknown at link time)
	Start int
	End   int
	CHash string // 16 hex chars of sha256 over the normalized code span; "-" = pending
	RHash string // same over the normalized rule sentence
}

// Class is the drift classification of one anchor.
type Class string

const (
	OK    Class = "ok"
	Moved Class = "moved" // same content, new position — silently refreshed

	// Evolved/Tightened/Diverged replace the old single-axis "changed" class
	// with a direction-aware read of drift: which side moved, the code or
	// the rule sentence. Only Evolved is mechanically healable — the other
	// two mean a human has to look, so they are never auto-healed.
	Evolved   Class = "evolved"   // code changed, rule sentence identical — mechanically healable
	Tightened Class = "tightened" // code identical, rule sentence changed — never healed
	Diverged  Class = "diverged"  // both code and rule sentence changed — never healed

	// CrossFile: the anchor's node ID now resolves to a DIFFERENT file
	// whose content does not match the stored hash, and no hash-matching
	// candidate exists anywhere under the ID stem — an ID re-binding
	// (walk-order renumbering, B-01KYJB3SGK), not an evolution. Audited,
	// NEVER healed: healing would silently cross files, the exact
	// incident this class exists to prevent.
	CrossFile Class = "crossfile"

	Gone    Class = "gone"    // node no longer exists
	Stale   Class = "stale"   // rule no longer exists but anchor does
	Pending Class = "pending" // anchor written before the node was indexable
)

// NormHash hashes a normalized span: CRLF->LF, per-line trailing whitespace
// stripped, leading/trailing blank lines dropped. Indentation is semantic
// (Plan 9 asm, Makefiles) and is preserved.
func NormHash(span []byte) string {
	s := strings.ReplaceAll(string(span), "\r\n", "\n")
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " \t")
	}
	for len(lines) > 0 && lines[0] == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(sum[:8])
}

// SpanHash reads and hashes lines [start,end] (1-based, inclusive) of a file.
func SpanHash(ws workspace.Root, file string, start, end int) (string, error) {
	raw, err := os.ReadFile(ws.Dir + "/" + file)
	if err != nil {
		return "", err
	}
	lines := strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")
	if start < 1 {
		start = 1
	}
	if end > len(lines) {
		end = len(lines)
	}
	if start > end {
		return NormHash(nil), nil
	}
	return NormHash([]byte(strings.Join(lines[start-1:end], "\n"))), nil
}

const header = "# rule\tnode\tfile\tspan\tchash\trhash\n"

// Load reads anchors.tsv (missing file = empty).
func Load(ws workspace.Root) ([]Anchor, error) {
	f, err := os.Open(ws.AnchorsPath())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Anchor
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		l := sc.Text()
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		p := strings.Split(l, "\t")
		if len(p) != 6 {
			continue
		}
		a := Anchor{Rule: p[0], Node: graph.NodeID(p[1]), File: p[2], CHash: p[4], RHash: p[5]}
		fmt.Sscanf(p[3], "%d-%d", &a.Start, &a.End)
		out = append(out, a)
	}
	return out, sc.Err()
}

// Save rewrites anchors.tsv under WithAnchorsLock, so two concurrent Save
// calls can never interleave into a torn file.
//
// Save alone does not close the lost-update race for a caller that Loads,
// mutates, and then Saves as three separate steps (mcpserver's check and
// stampAnchors do exactly that): the Load already happened before this
// call's lock is even requested, so a sibling's Save landing in between
// is invisible to the mutation this Save is about to write over it. A
// caller in that shape must wrap its OWN Load-mutate-Save sequence in
// WithAnchorsLock (calling the unexported save below inside it, not this
// function, to avoid acquiring the same name twice) — see the doc on
// WithAnchorsLock.
func Save(ws workspace.Root, anchors []Anchor) error {
	if err := ws.EnsureScaffold(""); err != nil {
		return err
	}
	return WithAnchorsLock(ws, func() error { return save(ws, anchors) })
}

func save(ws workspace.Root, anchors []Anchor) error {
	var b strings.Builder
	b.WriteString(header)
	for _, a := range anchors {
		fmt.Fprintf(&b, "%s\t%s\t%s\t%d-%d\t%s\t%s\n", a.Rule, a.Node, a.File, a.Start, a.End, a.CHash, a.RHash)
	}
	return os.WriteFile(ws.AnchorsPath(), []byte(b.String()), 0o644)
}

// WithAnchorsLock runs fn while holding coord.db's "anchors" lock (see
// coord.DB.WithLock and workspace.Locker), releasing on every exit path
// including panic. ws.Lock is nil outside a swarm-aware caller (tests,
// migrate, a one-shot CLI run), where there is at most one writer already,
// so running unlocked is correct there, not merely tolerated.
//
// anchors.tsv is root-only — one file for the whole workspace, unlike
// work.md/spec.md which are per context dir — so there is exactly one scope
// to serialize and the lock name carries no context.
//
// A caller that Loads, mutates in memory, and Saves as separate steps MUST
// run all three inside one WithAnchorsLock call, not rely on Save's own
// locking: locking the write alone still lets two readers load the same
// stale rows first and race to overwrite each other, which is the same
// defect item.Upsert had before it locked its whole read-modify-write
// instead of just writeWork. Today's in-repo callers of this shape
// (mcpserver's check and stampAnchors) are unlocked and out of this task's
// scope (internal/mcpserver) — this export is what closes that gap for
// them, or for TestConcurrentAnchorsRewriteNoLostUpdate's shape, without
// this package needing to know what mcpserver does with it.
func WithAnchorsLock(ws workspace.Root, fn func() error) error {
	if ws.Lock == nil {
		return fn()
	}
	return ws.Lock.WithLock("anchors", fn)
}

// Upsert replaces the anchor for (rule, node) or appends it.
func Upsert(anchors []Anchor, a Anchor) []Anchor {
	for i := range anchors {
		if anchors[i].Rule == a.Rule && anchors[i].Node == a.Node {
			anchors[i] = a
			return anchors
		}
	}
	return append(anchors, a)
}

// Reconcile drops every anchor row for rule whose Node is not in keep,
// leaving rows of other rules untouched and preserving order. Callers stamp
// a rule's current applies list through this first so an edit (or retire,
// with keep=nil) converges anchors.tsv to exactly that set instead of
// accumulating stale rows for nodes the rule no longer binds.
func Reconcile(anchors []Anchor, rule string, keep []graph.NodeID) []Anchor {
	if len(anchors) == 0 {
		return anchors
	}
	keepSet := make(map[graph.NodeID]bool, len(keep))
	for _, n := range keep {
		keepSet[n] = true
	}
	out := make([]Anchor, 0, len(anchors))
	for _, a := range anchors {
		if a.Rule == rule && !keepSet[a.Node] {
			continue
		}
		out = append(out, a)
	}
	return out
}

// Stamp builds an anchor for a rule bound to a node, hashing the node's
// current definition span. A node that is not (yet) in the graph produces a
// pending anchor — the M1 indexer resolves it on the next check.
func Stamp(ws workspace.Root, g graph.Graph, rule, ruleText string, node graph.NodeID) Anchor {
	a := Anchor{Rule: rule, Node: node, File: "-", CHash: "-", RHash: NormHash([]byte(ruleText))}
	n, ok := g.Node(node)
	if !ok || n.File == "" {
		return a
	}
	end := n.EndLine
	if end == 0 {
		end = n.Line
	}
	if h, err := SpanHash(ws, n.File, n.Line, end); err == nil {
		a.File, a.Start, a.End, a.CHash = n.File, n.Line, end, h
	}
	return a
}

// Result is one classified anchor.
type Result struct {
	Anchor   Anchor
	Class    Class
	NewHash  string // current code-span hash; set whenever the span could be re-hashed (i.e. not Stale)
	NewRHash string // current rule-sentence hash; set whenever the rule still exists (i.e. not Stale)
	// NewNode is set when hash-first re-resolution re-bound the anchor to
	// a different node ID (the stored ID renumbered or crossed files while
	// a hash-matching candidate existed) — the caller's Moved refresh must
	// write it back. Empty = the stored ID still names the right node.
	NewNode graph.NodeID
	// OtherFile is set on CrossFile: the file the stored ID resolves to
	// now, named in the audit render beside the anchor's own file.
	OtherFile string
}

// Classify checks every anchor against the current graph and files, along
// two independent axes: the code hash (has the bound span's content
// changed since the anchor was stamped?) and the rule hash (has the rule's
// sentence changed?). ruleText returns a rule's current sentence and
// whether the rule is still in the cascade; false routes straight to Stale
// (unchanged from before). When the graph is empty (M0: indexer is an M1
// stub), code-side classes degrade to Pending instead of false "gone"
// alarms — that Pending/Gone short-circuit runs ahead of the two-axis
// classification below.
//
// stale (DRF-003) lets the caller report that the graph is older than a
// given file on disk — e.g. the resident -http server only rebuilds the
// graph in Server.reindex (startup/reroot), so an on-disk edit the server
// never observed leaves a node's Line/EndLine pointing at the wrong lines
// of the NEW file content; hashing that stale range silently manufactures
// a hash for a span that isn't the node, which classified as Evolved and
// auto-healed a false "code changed" verdict (measured twice on
// go:main.main in this repo). A func, not a timestamp, so a caller with no
// way to know (a bare cache, a test, internal/lifecycle's audit gate) can
// pass nil and get exactly today's behavior — nil/zero here always means
// "no staleness information", never "definitely fresh". When stale(file)
// reports true for the anchor's current node, Classify short-circuits to
// Pending — the same "cannot judge yet" verdict an empty graph already
// gets — before ever computing SpanHash, so a stale graph can't be
// mistaken for a real hash comparison.
//
// Once both axes are known:
//
//	code \ rule   same           changed
//	same          OK / Moved     Tightened  (never healed)
//	changed       Evolved        Diverged   (never healed)
//
// OK requires the node's current file, start line AND end line (DRF-002)
// to match the anchor exactly; a pure end-line drift (Anchor.End stale
// while Anchor.Start still matches) falls through to Moved instead of
// staying OK forever with a wrong printed range — Anchor.End is stamped
// but was never read by any branch before this fix.
//
// Evolved (code moved, rule sentence identical) is the only mechanically
// healable case: the rule still describes the code correctly, only the
// anchor's recorded code hash is stale. Tightened and Diverged both involve
// a rule-sentence change and are never auto-healed — the spec author's
// intent may have changed the contract, so a human has to look.
// spanHashOfNode hashes a node's current span.
func spanHashOfNode(ws workspace.Root, n graph.Node) (string, error) {
	end := n.EndLine
	if end == 0 {
		end = n.Line
	}
	return SpanHash(ws, n.File, n.Line, end)
}

// rebindByHash re-resolves an anchor whose node ID vanished or crossed
// files: candidates are every graph node under the anchor's ID STEM (the
// ID with any ~N disambiguator stripped — the numeral is walk-order
// fragile and never trusted), and the SINGLE candidate whose current span
// hash equals the stored CHash wins. Two or more hash matches is genuine
// ambiguity — refuse to guess, exactly like ids.ResolveRecordID. Stale
// candidate files are skipped: their line spans cannot be trusted (DRF-003).
func rebindByHash(ws workspace.Root, g graph.Graph, a Anchor, stale func(file string) bool) (graph.Node, bool) {
	stem := string(a.Node)
	if i := strings.LastIndex(stem, "~"); i > 0 {
		stem = stem[:i]
	}
	var hit graph.Node
	hits := 0
	for _, cand := range g.Find(stem, 32, graph.KUnknown) {
		id := string(cand.ID)
		// Find matches substrings; the exact stem guard is load-bearing
		if id != stem && !strings.HasPrefix(id, stem+"~") {
			continue
		}
		if stale != nil && stale(cand.File) {
			continue
		}
		if h, err := spanHashOfNode(ws, cand); err == nil && h == a.CHash {
			hit = cand
			hits++
		}
	}
	if hits != 1 {
		return graph.Node{}, false
	}
	return hit, true
}

func Classify(ws workspace.Root, g graph.Graph, anchors []Anchor, ruleText func(string) (string, bool), stale func(file string) bool) []Result {
	graphEmpty := len(g.Find("", 1, graph.KUnknown)) == 0
	var out []Result
	for _, a := range anchors {
		r := Result{Anchor: a}
		text, ok := ruleText(a.Rule)
		if !ok {
			r.Class = Stale
			out = append(out, r)
			continue
		}
		curR := NormHash([]byte(text))
		r.NewRHash = curR
		switch {
		case a.CHash == "-" || a.File == "-":
			r.Class = Pending
		case graphEmpty:
			r.Class = Pending
		default:
			n, ok := g.Node(a.Node)
			// Hash-first re-resolution (B-01KYJB3SGK): a tilde numeral is
			// walk-order fragile and a bare ID re-binds when a same-name
			// symbol appears, so an ID that vanished or crossed files is
			// re-resolved by (stem, CHash) before any classification.
			// Content is identity; the ID is a hint.
			if !ok {
				if cand, found := rebindByHash(ws, g, a, stale); found {
					n, ok = cand, true
					r.NewNode = cand.ID
				} else {
					r.Class = Gone
					break
				}
			} else if n.File != a.File {
				if h, err := spanHashOfNode(ws, n); err != nil || h != a.CHash {
					if cand, found := rebindByHash(ws, g, a, stale); found {
						n = cand
						r.NewNode = cand.ID
					} else {
						// the stored ID resolves to an unrelated file and
						// no hash-matching candidate exists — audit, never
						// heal across files
						r.Class = CrossFile
						r.OtherFile = n.File
						break
					}
				}
			}
			if stale != nil && stale(n.File) {
				// Graph older than the file: Line/EndLine cannot be trusted,
				// so no SpanHash is computed at all (DRF-003).
				r.Class = Pending
				break
			}
			end := n.EndLine
			if end == 0 {
				end = n.Line
			}
			h, err := SpanHash(ws, n.File, n.Line, end)
			if err != nil {
				r.Class = Gone
				break
			}
			r.NewHash = h
			codeSame := h == a.CHash
			ruleSame := curR == a.RHash
			switch {
			case codeSame && ruleSame && n.File == a.File && n.Line == a.Start && end == a.End:
				// a re-bound anchor is never OK even at an unchanged
				// position: the stored ID is stale and OK would leave it
				// in anchors.tsv, re-paying the rebind scan every check —
				// Moved routes it through the caller's write-back
				if r.NewNode != "" {
					r.Class = Moved
					break
				}
				r.Class = OK
			case codeSame && ruleSame:
				r.Class = Moved // position-only change (incl. end-line-only drift): caller refreshes silently
			case !codeSame && ruleSame:
				r.Class = Evolved
			case codeSame && !ruleSame:
				r.Class = Tightened
			default:
				r.Class = Diverged
			}
		}
		out = append(out, r)
	}
	return out
}
