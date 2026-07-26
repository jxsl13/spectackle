package mcpserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jxsl13/spectackle/internal/item"
	"github.com/jxsl13/spectackle/internal/journal"
)

// TestGrillUnknownItem: an unknown ID falls back to nearest() (the same nf
// correction get() uses), not an error.
func TestGrillUnknownItem(t *testing.T) {
	s := newTestServer(t, t.TempDir())
	res, _, err := s.grill(grillIn{ID: "P-9999"})
	out := resText(t, res, err)
	if !strings.HasPrefix(out, "nf ") {
		t.Fatalf("grill unknown item: %q", out)
	}
}

// TestGrillTargetsAndContracts: a node target missing from the graph -> nf,
// one present but never anchored -> g unanchored (#targets); a path target
// with zero resolved rules -> g nocontract (#contracts).
func TestGrillTargetsAndContracts(t *testing.T) {
	root := t.TempDir()
	src := "package demo\n\nfunc Foo() {}\n\nfunc Bar() {}\n"
	if err := os.WriteFile(filepath.Join(root, "demo.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	s := newTestServer(t, root)

	prop := serverDraftID(t, s, draftIn{
		Kind: "proposal", Title: "demo review",
		Targets: []string{"go:demo.Foo", "go:demo.Missing", "pkg/new.go"},
	})

	res, _, err := s.grill(grillIn{ID: prop})
	out := resText(t, res, err)

	if !strings.Contains(out, "#targets") {
		t.Fatalf("missing #targets: %q", out)
	}
	if !strings.Contains(out, "nf go:demo.Missing") {
		t.Fatalf("targets missing nf: %q", out)
	}
	if !strings.Contains(out, "g unanchored go:demo.Foo") {
		t.Fatalf("targets missing unanchored: %q", out)
	}
	if !strings.Contains(out, "#contracts") {
		t.Fatalf("missing #contracts: %q", out)
	}
	if !strings.Contains(out, "g nocontract pkg/new.go") {
		t.Fatalf("contracts missing nocontract: %q", out)
	}
}

// TestGrillBriefHeuristicsDeleted pins the DELETION (T-01KYD94KP4): the
// short-body/no-path/no-verify heuristics were word-presence checks, and a
// thin child brief must no longer produce a #briefs section or b-lines —
// brief quality is the independent reviewer's judgment now.
func TestGrillBriefHeuristicsDeleted(t *testing.T) {
	root := t.TempDir()
	s := newTestServer(t, root)

	parent := serverDraftID(t, s, draftIn{Kind: "proposal", Title: "parent"})
	serverDraftID(t, s, draftIn{Kind: "task", Title: "thin child", Body: "too short", Parent: parent})

	res, _, err := s.grill(grillIn{ID: parent})
	out := resText(t, res, err)
	if strings.Contains(out, "#briefs") || strings.Contains(out, "\nb ") {
		t.Fatalf("deleted brief heuristics resurfaced: %q", out)
	}
}

func TestGrillTestsGap(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "internal", "widget"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "internal", "widget", "widget.go"), []byte("package widget\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := newTestServer(t, root)

	task := serverDraftID(t, s, draftIn{
		Kind: "task", Title: "add widget", Targets: []string{"internal/widget/widget.go"},
	})

	res, _, err := s.grill(grillIn{ID: task})
	out := resText(t, res, err)
	if !strings.Contains(out, "#tests") {
		t.Fatalf("missing #tests: %q", out)
	}
	if !strings.Contains(out, "g notest internal/widget") {
		t.Fatalf("tests missing gap: %q", out)
	}

	if err := os.WriteFile(filepath.Join(root, "internal", "widget", "widget_test.go"), []byte("package widget\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res2, _, err2 := s.grill(grillIn{ID: task})
	out2 := resText(t, res2, err2)
	if strings.Contains(out2, "#tests") {
		t.Fatalf("tests section should be gone once the test file exists: %q", out2)
	}
}

// TestGrillRejectionsAndQuestions: a prior rejection with overlapping
// vocabulary surfaces under #rejections; the #questions checklist flags only
// the checklist items the item's own body never addresses (rollback is
// covered here, scope and exit criterion are not).
func TestGrillRejectionsAndQuestions(t *testing.T) {
	root := t.TempDir()
	s := newTestServer(t, root)

	old := serverDraftID(t, s, draftIn{Kind: "proposal", Title: "widget batching plan"})
	if _, _, err := s.move(moveIn{ID: old, To: "rejected", Note: "too risky"}); err != nil {
		t.Fatal(err)
	}
	if err := s.scan.Refresh(); err != nil {
		t.Fatal(err)
	}

	v2 := serverDraftID(t, s, draftIn{
		Kind: "proposal", Title: "widget batching plan v2",
		Body: "Batches widgets. Rollback: revert the commit if perf regresses.",
	})

	res, _, err := s.grill(grillIn{ID: v2})
	out := resText(t, res, err)

	if !strings.Contains(out, "#rejections") {
		t.Fatalf("missing #rejections: %q", out)
	}
	if !strings.Contains(out, "widget batching plan") {
		t.Fatalf("rejections missing match: %q", out)
	}
	if !strings.Contains(out, "#questions") {
		t.Fatalf("missing #questions: %q", out)
	}
	// The substring questions (scope/rollback/exit criterion) are DELETED
	// word-presence checks (T-01KYD94KP4); only the refs-only deliberation
	// question may render.
	for _, dead := range []string{"q scope", "q rollback", "q exit"} {
		if strings.Contains(out, dead) {
			t.Fatalf("deleted substring question resurfaced (%s): %q", dead, out)
		}
	}
	if !strings.Contains(out, "q no deliberation recorded") {
		t.Fatalf("missing refs-only deliberation question: %q", out)
	}
}

// TestGrillQuestionsDeliberation (T-0117): grillQuestions asks "no
// deliberation recorded" for a proposal with neither an ADR-/R- ref nor
// rejected-alternative prose, stays silent once either shows up, and never
// asks it at all for a non-proposal kind.
func TestGrillQuestionsDeliberation(t *testing.T) {
	const want = "q no deliberation recorded: no ADR or research ref"
	body := "Body text that satisfies scope, rollback and exit criterion, and done when verified."

	cases := []struct {
		name string
		it   item.Item
		ask  bool
	}{
		{
			name: "proposal with no refs and no rejected prose asks",
			it:   item.Item{Kind: "proposal", Body: body},
			ask:  true,
		},
		{
			name: "proposal with an ADR ref stays silent",
			it:   item.Item{Kind: "proposal", Body: body, Refs: []string{"ADR-0001"}},
			ask:  false,
		},
		{
			name: "proposal with a research ref stays silent",
			it:   item.Item{Kind: "proposal", Body: body, Refs: []string{"R-0001"}},
			ask:  false,
		},
		{
			// prose NEVER counts now — the "rejected" substring path was a
			// word-presence check of exactly the deleted species.
			name: "proposal whose body mentions a rejected alternative still asks",
			it:   item.Item{Kind: "proposal", Body: body + " Considered X; rejected: too slow."},
			ask:  true,
		},
		{
			name: "task kind never asks, even with no refs and no rejected prose",
			it:   item.Item{Kind: "task", Body: body},
			ask:  false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := grillQuestions(c.it)
			got := false
			for _, q := range out {
				if q == want {
					got = true
				}
			}
			if got != c.ask {
				t.Fatalf("grillQuestions(%+v) = %v, want contains %q: %v", c.it, out, want, c.ask)
			}
		})
	}
}

// TestGrillStampsGrilledAndJournal is grill's write-side-effect contract:
// exactly one write — it.Grilled=today (persisted to work.md), one EvGrill
// journal event, one swarm learning. Unlike research, grill is NOT read-only.
func TestGrillStampsGrilledAndJournal(t *testing.T) {
	root := t.TempDir()
	s := newTestServer(t, root)

	prop := serverDraftID(t, s, draftIn{Kind: "proposal", Title: "stamp me"})

	before, ok, err := item.Get(s.ws, fullID(t, s, prop))
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("item not found before grill")
	}
	if before.Grilled != "" {
		t.Fatalf("item pre-grilled unexpectedly: %+v", before)
	}

	res, _, err := s.grill(grillIn{ID: prop})
	out := resText(t, res, err)
	today := time.Now().UTC().Format("2006-01-02")
	if !strings.Contains(out, "ok grilled "+prop+" "+today+" open=") {
		t.Fatalf("grill did not confirm the stamp: %q", out)
	}

	after, ok, err := item.Get(s.ws, fullID(t, s, prop))
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("item vanished after grill")
	}
	if !strings.HasPrefix(after.Grilled, today+" open=") {
		t.Fatalf("Grilled = %q, want %q open=<n>", after.Grilled, today)
	}

	// grilled: header set after call — asserted against the persisted
	// work.md bytes, not just the in-memory item.
	raw, err := os.ReadFile(filepath.Join(root, ".spectackle", "work.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "grilled: "+today+" open=") {
		t.Fatalf("work.md missing grilled header: %s", raw)
	}

	events, err := journal.Read(s.ws, "")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	full := fullID(t, s, prop)
	for _, e := range events {
		if e.Ev == journal.EvGrill && e.ID == full && strings.HasPrefix(e.Gr, today+" open=") && e.Hash != "" {
			found = true
		}
	}
	if !found {
		t.Fatalf("journal missing EvGrill event: %+v", events)
	}

	swEvents, err := s.cd.SearchEvents("", []string{"grill"}, 5)
	if err != nil {
		t.Fatal(err)
	}
	foundSw := false
	for _, e := range swEvents {
		if e.Ev == "grill" && e.Ref == full {
			foundSw = true
		}
	}
	if !foundSw {
		t.Fatalf("swarm learning missing grill event: %+v", swEvents)
	}
}
