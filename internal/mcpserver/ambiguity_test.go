package mcpserver

// Ambiguity classes (T-01KYFXBY): computed from post-deletion signals only
// — body bytes, coverage novelty, graph incoherence — closed mechanically
// by a landed decide round-trip or accountably by waiver. The 400-byte
// floor is calibrated: the reject corpus's 22 task/proposal bodies bottom
// out at 418 bytes, so none would have fired.

import (
	"strings"
	"testing"

	"github.com/jxsl13/spectackle/internal/graph"
	"github.com/jxsl13/spectackle/internal/item"
	"github.com/jxsl13/spectackle/internal/lifecycle"
	"github.com/jxsl13/spectackle/internal/spec"
)

// ambFixturePad keeps OTHER tests' fixture drafts above the calibrated
// thin floor without duplicating a 400-byte literal at every site (the
// dup detector rightly flagged the copies).
const ambFixturePad = "The remainder of this brief restates scope, constraints, verification commands, and rollback in enough real sentences that the calibrated ambiguity floor does not fire on a fixture that exists to probe a different class entirely. It names its files, states its exit criterion, records what is deliberately out of scope for the probe at hand, and notes which sibling machinery the assertions below actually exercise."

func TestAmbThinFiresAndClears(t *testing.T) {
	s := newTestServer(t, t.TempDir())
	c := loadCascadeFor(t, s)

	thin := item.Item{ID: "T-1", Kind: "task", State: item.StateDraft,
		Body: "make it faster somehow"}
	out := s.grillAmbiguity(thin, c, 0)
	if len(out) == 0 || !strings.Contains(out[0], "amb-thin") {
		t.Fatalf("22-byte body must fire amb-thin: %v", out)
	}

	// 401+ bytes of real sentences: no thin finding, and with no targets
	// the other classes stay silent too.
	fat := thin
	fat.Body = strings.Repeat("the refill accrues fractionally and clamps at capacity under the injected clock. ", 6)
	if got := s.grillAmbiguity(fat, c, 0); len(got) != 0 {
		t.Fatalf("calibrated floor must not fire on a real-sized body: %v", got)
	}
}

func TestAmbAskClosure(t *testing.T) {
	s := newTestServer(t, t.TempDir())
	c := loadCascadeFor(t, s)

	adr, err := lifecycle.Draft(s.ws, s.minter(), "adr", "resolved decision", "", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	adr.State = item.StateDone
	if err := item.Upsert(s.ws, adr); err != nil {
		t.Fatal(err)
	}
	open, err := lifecycle.Draft(s.ws, s.minter(), "adr", "pending decision", "", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	open.State = item.StateSubmitted
	if err := item.Upsert(s.ws, open); err != nil {
		t.Fatal(err)
	}

	thin := item.Item{ID: "T-1", Kind: "task", State: item.StateDraft, Body: "vague"}

	// no refs: open finding
	if got := s.grillAmbiguity(thin, c, 0); len(got) != 1 {
		t.Fatalf("want one open amb finding: %v", got)
	}
	// cited open ADR: awaiting
	thin.Refs = []string{open.ID}
	got := s.grillAmbiguity(thin, c, 0)
	if len(got) != 1 || !strings.Contains(got[0], "awaiting ADR-") {
		t.Fatalf("open ADR must render awaiting: %v", got)
	}
	// cited done ADR: closed
	thin.Refs = []string{adr.ID}
	if got := s.grillAmbiguity(thin, c, 0); len(got) != 0 {
		t.Fatalf("done ADR must close the ambiguity: %v", got)
	}
}

func TestAmbIncoherent(t *testing.T) {
	g := graph.NewMem()
	g.Upsert([]graph.Node{
		{ID: "go:alpha.A", File: "alpha/a.go"},
		{ID: "go:beta.B", File: "beta/b.go"},
		{ID: "go:gamma.C", File: "gamma/c.go"},
	}, nil)
	paths := []string{"alpha/a.go", "beta/b.go", "gamma/c.go"}
	if !incoherentTargets(g, paths) {
		t.Fatal("three unlinked top-level subtrees must be incoherent")
	}
	// one connecting edge makes it coherent
	g.Upsert(nil, []graph.Edge{{Src: "go:alpha.A", Dst: "go:beta.B", Kind: graph.ECall}})
	if incoherentTargets(g, paths) {
		t.Fatal("a connected pair must defuse incoherence")
	}
	// two targets never fire
	if incoherentTargets(g, paths[:2]) {
		t.Fatal("two targets must never fire incoherence")
	}
}

// An amb key is waivable through the ordinary addressal machinery.
func TestAmbKeyWaivable(t *testing.T) {
	keys := []string{"amb-thin:body 22B < 400B floor"}
	gap, wv, _ := addressalGap(1, keys, true, "", map[string]string{
		keys[0]: "the brief is intentionally terse; scope is a one-line rename",
	})
	if gap != "" {
		t.Fatalf("waived amb key must clear the gap: %q", gap)
	}
	if len(wv) != 1 {
		t.Fatalf("waiver must be recorded: %v", wv)
	}
}

// Integration: a thin draft grilled through the real tool surface renders
// the finding and counts it open.
func TestGrillRendersAmbThin(t *testing.T) {
	root := t.TempDir()
	sess := connectRoot(t, root)
	out := callText(t, sess, "draft", map[string]any{
		"kind": "task", "title": "ambiguity integration probe", "body": "do the thing",
	})
	id := idOfRecord(t, out, "i")
	out = callText(t, sess, "grill", map[string]any{"id": id})
	if !strings.Contains(out, "g amb-thin") {
		t.Fatalf("grill must render amb-thin for a thin draft: %q", out)
	}
	if strings.Contains(out, "open=0") {
		t.Fatalf("the finding must count open: %q", out)
	}
}

// loadCascadeFor loads the workspace cascade the way the grill handler
// does, so unit tests exercise grillAmbiguity with real inputs.
func loadCascadeFor(t *testing.T, s *Server) *spec.Cascade {
	t.Helper()
	c, err := spec.Load(s.ws.Dir)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
