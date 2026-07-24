// Package drift binds EARS rules to code spans and detects when either side
// moves out from under the other. Bindings live in the root-only, versioned
// .spectacle/anchors.tsv; each row records a content hash of the normalized
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

	"github.com/jxsl13/spectacle/internal/graph"
	"github.com/jxsl13/spectacle/internal/workspace"
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
	OK      Class = "ok"
	Moved   Class = "moved"   // same content, new position — silently refreshed
	Changed Class = "changed" // code content under the rule changed
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

// Save rewrites anchors.tsv.
func Save(ws workspace.Root, anchors []Anchor) error {
	if err := ws.EnsureScaffold(""); err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString(header)
	for _, a := range anchors {
		fmt.Fprintf(&b, "%s\t%s\t%s\t%d-%d\t%s\t%s\n", a.Rule, a.Node, a.File, a.Start, a.End, a.CHash, a.RHash)
	}
	return os.WriteFile(ws.AnchorsPath(), []byte(b.String()), 0o644)
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
	Anchor Anchor
	Class  Class
	NewHash string
}

// Classify checks every anchor against the current graph and files.
// ruleExists reports whether a rule ID is still in the cascade. When the
// graph is empty (M0: indexer is an M1 stub), code-side classes degrade to
// Pending instead of false "gone" alarms.
func Classify(ws workspace.Root, g graph.Graph, anchors []Anchor, ruleExists func(string) bool) []Result {
	graphEmpty := len(g.Find("", 1, graph.KUnknown)) == 0
	var out []Result
	for _, a := range anchors {
		r := Result{Anchor: a}
		switch {
		case !ruleExists(a.Rule):
			r.Class = Stale
		case a.CHash == "-" || a.File == "-":
			r.Class = Pending
		case graphEmpty:
			r.Class = Pending
		default:
			n, ok := g.Node(a.Node)
			if !ok {
				r.Class = Gone
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
			switch {
			case h == a.CHash && n.File == a.File && n.Line == a.Start:
				r.Class = OK
			case h == a.CHash:
				r.Class = Moved // position-only change: caller refreshes silently
			default:
				r.Class = Changed
			}
		}
		out = append(out, r)
	}
	return out
}
