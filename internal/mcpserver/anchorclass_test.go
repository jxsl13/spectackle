package mcpserver

// Tool-layer half of B-01KYN5ZYM1FY2: an anchor that can NEVER resolve (its
// applies names a file path, not a graph node) used to render byte-identically
// to one that is merely waiting on the next index, and a waiting one had no
// way to stop waiting. The classifier half is pinned in
// internal/drift/drift_test.go; these tests pin what the CALLER observes —
// the rendered records and the anchors.tsv rows behind them.

import (
	"strings"
	"testing"

	"github.com/jxsl13/spectackle/internal/drift"
	"github.com/jxsl13/spectackle/internal/workspace"
)

// anchorRow returns the stored anchor for (rule, node), and whether it exists.
func anchorRow(t *testing.T, root, rule, node string) (drift.Anchor, bool) {
	t.Helper()
	anchors, err := drift.Load(workspace.Root{Dir: root})
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range anchors {
		if a.Rule == rule && string(a.Node) == node {
			return a, true
		}
	}
	return drift.Anchor{}, false
}

// TestRuleAddNamesUnresolvableApplies is the incident, reproduced at the tool
// boundary: `rule op=add applies=[<a file path>]` was accepted and rendered
// "pending (node not indexed yet)", which reads as a state the next reindex
// clears. It never clears. The add must now say unresolvable, must not use
// the pending wording, and should point at the node the caller probably meant.
func TestRuleAddNamesUnresolvableApplies(t *testing.T) {
	root := t.TempDir()
	writeFileT(t, root, "demo.go", "package demo\n\n// Widget does a thing.\nfunc Widget() int {\n\treturn 1\n}\n")
	sess := connectRoot(t, root)

	out := callText(t, sess, "rule", map[string]any{
		"op": "add", "dir": "", "pattern": "U", "stem": "ANC-PATH",
		"system":   "the widget",
		"response": "return one",
		"applies":  []string{"demo.go"},
	})
	if !strings.Contains(out, "unresolvable") {
		t.Fatalf("a path-shaped applies must be named unresolvable: %q", out)
	}
	if strings.Contains(out, "pending (node not indexed yet)") {
		t.Fatalf("a path-shaped applies must NOT render as a transient pending wait: %q", out)
	}
	// The hint is best-effort, but "best-effort" must not mean "absent": the
	// whole point of catching this at add time is that the caller can still
	// fix the argument, and the node ID is the one thing they do not know.
	if !strings.Contains(out, "did you mean go:demo.Widget?") {
		t.Fatalf("the refusal must suggest the node declared in that file: %q", out)
	}
	// ACTION, not wording: the row is still written. A rule whose applies has
	// no anchor row at all reads as an orphan to MCP-004's detector, so the
	// warning must not come at the price of a silent hole.
	if _, ok := anchorRow(t, root, "ANC-PATH-001", "demo.go"); !ok {
		t.Fatalf("the anchor row must still be written for the unresolvable node")
	}
}

// TestCheckReportsUnresolvableAnchorWithRemedy: check must lift the same
// anchor out of the benign `ok N anchors pending` tally into an E finding —
// and the finding MUST name the clearing path. An emptied applies is refused
// by ruleEdit, so re-editing applies to the correct node is the only escape;
// an E finding without it would redden this repository's own exact-shape CI
// gate permanently over a typo.
func TestCheckReportsUnresolvableAnchorWithRemedy(t *testing.T) {
	root := t.TempDir()
	writeFileT(t, root, "demo.go", "package demo\n\n// Widget does a thing.\nfunc Widget() int {\n\treturn 1\n}\n")
	sess := connectRoot(t, root)

	callText(t, sess, "rule", map[string]any{
		"op": "add", "dir": "", "pattern": "U", "stem": "ANC-PATH",
		"system":   "the widget",
		"response": "return one",
		"applies":  []string{"demo.go"},
	})

	out := callText(t, sess, "check", map[string]any{})
	var finding string
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "! ANCHOR E ANC-PATH-001") {
			finding = l
		}
	}
	if finding == "" {
		t.Fatalf("check must raise an E finding naming the rule: %q", out)
	}
	if !strings.Contains(finding, "demo.go") {
		t.Fatalf("the finding must name the offending node: %q", finding)
	}
	if !strings.Contains(finding, `rule op=edit id=ANC-PATH-001 applies=["<correct node>"]`) {
		t.Fatalf("the finding must name the only measured clearing path: %q", finding)
	}
	// It must NOT be folded into the pending tally — that line promises a
	// state the next index clears, which is exactly the false promise the
	// bug is about.
	if strings.Contains(out, "anchors pending") {
		t.Fatalf("an unresolvable anchor must not be counted as pending: %q", out)
	}
}

// TestCheckBindsPendingAnchorOnceNodeAppears is the half that makes the
// distinction worth drawing. A genuine pending anchor — added before its
// symbol was indexed — must BIND on the first check that sees the node, and
// the SECOND check must then report it clean. Before this fix nothing ever
// re-stamped a "-" code hash, so such an anchor stayed pending for the life
// of the repository and kept `check` off its bare-ok shape forever.
func TestCheckBindsPendingAnchorOnceNodeAppears(t *testing.T) {
	root := t.TempDir()
	s, sess := connectRootWithServer(t, root)

	// The node does not exist yet: this is a legitimate pending anchor.
	out := callText(t, sess, "rule", map[string]any{
		"op": "add", "dir": "", "pattern": "U", "stem": "ANC-WAIT",
		"system":   "the widget",
		"response": "return one",
		"applies":  []string{"go:demo.Widget"},
	})
	if !strings.Contains(out, "pending (node not indexed yet)") {
		t.Fatalf("a not-yet-indexed node ID must still render as pending: %q", out)
	}
	if a, ok := anchorRow(t, root, "ANC-WAIT-001", "go:demo.Widget"); !ok || a.CHash != "-" {
		t.Fatalf("setup: want a pending row with CHash \"-\", got %+v (found=%v)", a, ok)
	}

	// The symbol lands. (check() only re-indexes on a stale anchor file or a
	// tree-shape change; neither fires for a pending anchor plus a new file
	// in an existing directory, so the index is refreshed explicitly here —
	// the behavior under test is check()'s classification, not its
	// re-index heuristic.)
	writeFileT(t, root, "demo.go", "package demo\n\n// Widget does a thing.\nfunc Widget() int {\n\treturn 1\n}\n")
	s.markDirty()
	s.reindex()

	out = callText(t, sess, "check", map[string]any{})
	if !strings.Contains(out, "d bound ANC-WAIT-001 go:demo.Widget demo.go:") {
		t.Fatalf("the first check after indexing must bind the anchor: %q", out)
	}
	bound, ok := anchorRow(t, root, "ANC-WAIT-001", "go:demo.Widget")
	if !ok {
		t.Fatalf("the anchor row disappeared")
	}
	// ACTION: a REAL hash was persisted. Refreshing only File/Start/End (what
	// the Moved arm does) leaves "-" here and the anchor silently re-pends.
	if bound.CHash == "-" || bound.CHash == "" {
		t.Fatalf("binding must persist a real code hash, got %q", bound.CHash)
	}
	if bound.File != "demo.go" || bound.Start == 0 {
		t.Fatalf("binding must persist the node's span, got %+v", bound)
	}

	// The proof that this RESOLVES rather than re-defers: a second check sees
	// a settled anchor — no pending tally, and nothing left to bind.
	out = callText(t, sess, "check", map[string]any{})
	if strings.Contains(out, "anchors pending") {
		t.Fatalf("the bound anchor fell back to pending on the second check: %q", out)
	}
	if strings.Contains(out, "d bound") {
		t.Fatalf("a bound anchor must not re-bind on every check: %q", out)
	}
	if strings.Contains(out, "ANC-WAIT-001") {
		t.Fatalf("a settled anchor must produce no drift record at all: %q", out)
	}
}

// TestStateCountsUnresolvableAnchor pins the read-only surface (hunk 6 of
// B-01KYN5ZYM1FY2). state runs the same classifier as check but saves
// nothing, so an unhandled class would fall into its default arm and print a
// `d unresolvable` line while the summary quietly stopped summing to total —
// and, for a bound anchor, would print `d bound` in EVERY snapshot forever,
// since state never writes the hash back. Both must be counted, not rendered.
func TestStateCountsUnresolvableAnchor(t *testing.T) {
	root := t.TempDir()
	writeFileT(t, root, "demo.go", "package demo\n\n// Widget does a thing.\nfunc Widget() int {\n\treturn 1\n}\n")
	sess := connectRoot(t, root)

	callText(t, sess, "rule", map[string]any{
		"op": "add", "dir": "", "pattern": "U", "stem": "ANC-PATH",
		"system":   "the widget",
		"response": "return one",
		"applies":  []string{"demo.go"},
	})

	out := callText(t, sess, "state", map[string]any{})
	if strings.Contains(out, "pending=1") {
		t.Fatalf("the unresolvable anchor must not be counted as pending: %q", out)
	}
	if !strings.Contains(out, "ok anchors total=1 ok=0 pending=0 moved=0 bound=0 unresolvable=1") {
		t.Fatalf("state must COUNT the unresolvable anchor, keeping the summary summing to total: %q", out)
	}
	if strings.Contains(out, "d unresolvable") {
		t.Fatalf("state must not render an unresolvable anchor as a drift record: %q", out)
	}
}
