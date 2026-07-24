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

	if _, _, err := s.draft(draftIn{
		Kind: "proposal", Title: "demo review",
		Targets: []string{"go:demo.Foo", "go:demo.Missing", "pkg/new.go"},
	}); err != nil {
		t.Fatal(err)
	}

	res, _, err := s.grill(grillIn{ID: "P-0001"})
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

// TestGrillBriefsHeuristicsMatrix exercises all three brief heuristics
// (short-body, no-path, no-verify) independently, plus a clean brief that
// must trigger none of them — the heuristic matrix the task item asks for.
func TestGrillBriefsHeuristicsMatrix(t *testing.T) {
	root := t.TempDir()
	s := newTestServer(t, root)

	if _, _, err := s.draft(draftIn{Kind: "proposal", Title: "parent"}); err != nil {
		t.Fatal(err)
	}

	clean := "internal/mcpserver/grill.go implements the grill tool. " +
		"Run `go test ./internal/mcpserver/...` to verify. " + strings.Repeat("detail ", 40)
	shortBody := "too short"
	noPath := strings.Repeat("word ", 80) + "run go test to verify"
	noVerify := strings.Repeat("x/y/z path segment ", 40)

	for _, c := range []struct{ title, body string }{
		{"clean brief", clean},
		{"short only", shortBody},
		{"no path", noPath},
		{"no verify", noVerify},
	} {
		if _, _, err := s.draft(draftIn{Kind: "task", Title: c.title, Body: c.body, Parent: "P-0001"}); err != nil {
			t.Fatalf("draft %s: %v", c.title, err)
		}
	}

	res, _, err := s.grill(grillIn{ID: "P-0001"})
	out := resText(t, res, err)

	if !strings.Contains(out, "#briefs") {
		t.Fatalf("missing #briefs: %q", out)
	}
	// T-0001 (clean) must fail nothing.
	if strings.Contains(out, "b T-0001 ") {
		t.Fatalf("clean brief flagged: %q", out)
	}
	// T-0002 (short only) fails all three heuristics.
	for _, want := range []string{"b T-0002 short-body", "b T-0002 no-path", "b T-0002 no-verify"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q: %q", want, out)
		}
	}
	// T-0003 (no path) fails only no-path.
	if !strings.Contains(out, "b T-0003 no-path") {
		t.Fatalf("missing T-0003 no-path: %q", out)
	}
	if strings.Contains(out, "b T-0003 short-body") || strings.Contains(out, "b T-0003 no-verify") {
		t.Fatalf("T-0003 over-flagged: %q", out)
	}
	// T-0004 (no verify) fails only no-verify.
	if !strings.Contains(out, "b T-0004 no-verify") {
		t.Fatalf("missing T-0004 no-verify: %q", out)
	}
	if strings.Contains(out, "b T-0004 short-body") || strings.Contains(out, "b T-0004 no-path") {
		t.Fatalf("T-0004 over-flagged: %q", out)
	}
}

// TestGrillTestsGap: a target package under internal/ with no *_test.go file
// surfaces a g notest line; once the test file exists, the section (and the
// line) disappears — omit-if-empty.
func TestGrillTestsGap(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "internal", "widget"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "internal", "widget", "widget.go"), []byte("package widget\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := newTestServer(t, root)

	if _, _, err := s.draft(draftIn{
		Kind: "task", Title: "add widget", Targets: []string{"internal/widget/widget.go"},
	}); err != nil {
		t.Fatal(err)
	}

	res, _, err := s.grill(grillIn{ID: "T-0001"})
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
	res2, _, err2 := s.grill(grillIn{ID: "T-0001"})
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

	if _, _, err := s.draft(draftIn{Kind: "proposal", Title: "widget batching plan"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.move(moveIn{ID: "P-0001", To: "rejected", Note: "too risky"}); err != nil {
		t.Fatal(err)
	}
	if err := s.scan.Refresh(); err != nil {
		t.Fatal(err)
	}

	if _, _, err := s.draft(draftIn{
		Kind: "proposal", Title: "widget batching plan v2",
		Body: "Batches widgets. Rollback: revert the commit if perf regresses.",
	}); err != nil {
		t.Fatal(err)
	}

	res, _, err := s.grill(grillIn{ID: "P-0002"})
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
	if strings.Contains(out, "q rollback not addressed") {
		t.Fatalf("rollback question should be answered: %q", out)
	}
	if !strings.Contains(out, "q scope disjointness not addressed") {
		t.Fatalf("missing scope question: %q", out)
	}
	if !strings.Contains(out, "q exit criterion not addressed") {
		t.Fatalf("missing exit criterion question: %q", out)
	}
}

// TestGrillStampsGrilledAndJournal is grill's write-side-effect contract:
// exactly one write — it.Grilled=today (persisted to work.md), one EvGrill
// journal event, one swarm learning. Unlike research, grill is NOT read-only.
func TestGrillStampsGrilledAndJournal(t *testing.T) {
	root := t.TempDir()
	s := newTestServer(t, root)

	if _, _, err := s.draft(draftIn{Kind: "proposal", Title: "stamp me"}); err != nil {
		t.Fatal(err)
	}

	before, ok, err := item.Get(s.ws, "P-0001")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("item not found before grill")
	}
	if before.Grilled != "" {
		t.Fatalf("item pre-grilled unexpectedly: %+v", before)
	}

	res, _, err := s.grill(grillIn{ID: "P-0001"})
	out := resText(t, res, err)
	today := time.Now().UTC().Format("2006-01-02")
	if !strings.Contains(out, "ok grilled P-0001 "+today) {
		t.Fatalf("grill did not confirm the stamp: %q", out)
	}

	after, ok, err := item.Get(s.ws, "P-0001")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("item vanished after grill")
	}
	if after.Grilled != today {
		t.Fatalf("Grilled = %q, want %q", after.Grilled, today)
	}

	// grilled: header set after call — asserted against the persisted
	// work.md bytes, not just the in-memory item.
	raw, err := os.ReadFile(filepath.Join(root, ".spectackle", "work.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "grilled: "+today) {
		t.Fatalf("work.md missing grilled header: %s", raw)
	}

	events, err := journal.Read(s.ws, "")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range events {
		if e.Ev == journal.EvGrill && e.ID == "P-0001" && e.Gr == today {
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
		if e.Ev == "grill" && e.Ref == "P-0001" {
			foundSw = true
		}
	}
	if !foundSw {
		t.Fatalf("swarm learning missing grill event: %+v", swEvents)
	}
}

// boilerplateBody answers grillQuestions' three pre-existing checklist items
// (scope, rollback, exit criterion) so these tests isolate the new
// deliberation question instead of tripping the others too.
const boilerplateBody = "Scope: this file only. Rollback: revert. Exit criterion: tests pass. "

// TestGrillQuestionsFlagsUndeliberatedProposal (T-0118): a proposal with no
// refs to an adr/research item and no body line naming a considered or
// rejected alternative gets asked about it.
func TestGrillQuestionsFlagsUndeliberatedProposal(t *testing.T) {
	root := t.TempDir()
	s := newTestServer(t, root)

	if _, _, err := s.draft(draftIn{
		Kind: "proposal", Title: "single obvious approach",
		Body: boilerplateBody + "Just do the thing.",
	}); err != nil {
		t.Fatal(err)
	}

	res, _, err := s.grill(grillIn{ID: "P-0001"})
	out := resText(t, res, err)
	if !strings.Contains(out, "q deliberation not addressed") {
		t.Fatalf("undeliberated proposal should be asked about it: %q", out)
	}
}

// TestGrillQuestionsSkipsWithADRRef: a proposal that cites an adr item via
// refs has structurally recorded its weighing — no question.
func TestGrillQuestionsSkipsWithADRRef(t *testing.T) {
	root := t.TempDir()
	s := newTestServer(t, root)

	if _, _, err := s.draft(draftIn{Kind: "adr", Title: "backend choice"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.draft(draftIn{
		Kind: "proposal", Title: "adopt the chosen backend",
		Body: boilerplateBody + "Do what the ADR says.",
		Refs: []string{"ADR-0001"},
	}); err != nil {
		t.Fatal(err)
	}

	res, _, err := s.grill(grillIn{ID: "P-0001"})
	out := resText(t, res, err)
	if strings.Contains(out, "q deliberation not addressed") {
		t.Fatalf("proposal citing an ADR should not be asked: %q", out)
	}
}

// TestGrillQuestionsSkipsWithRejectedAlternativeBody: a proposal naming a
// rejected/considered alternative in its own body — no refs needed — also
// counts as recorded deliberation.
func TestGrillQuestionsSkipsWithRejectedAlternativeBody(t *testing.T) {
	root := t.TempDir()
	s := newTestServer(t, root)

	if _, _, err := s.draft(draftIn{
		Kind: "proposal", Title: "batches writes",
		Body: boilerplateBody + "Considered an in-memory queue as an alternative but " +
			"rejected it: unbounded memory growth under load.",
	}); err != nil {
		t.Fatal(err)
	}

	res, _, err := s.grill(grillIn{ID: "P-0001"})
	out := resText(t, res, err)
	if strings.Contains(out, "q deliberation not addressed") {
		t.Fatalf("proposal naming a rejected alternative should not be asked: %q", out)
	}
}

// TestGrillQuestionsSkipsWithRejectedSectionMarker: this repo's proposals
// write "Rejected: <approach>. <why>" and never use the word "alternative".
// Requiring that word made the check fire on well-deliberated proposals —
// a false negative on exactly the population it exists to leave alone.
func TestGrillQuestionsSkipsWithRejectedSectionMarker(t *testing.T) {
	root := t.TempDir()
	s := newTestServer(t, root)

	if _, _, err := s.draft(draftIn{
		Kind: "proposal", Title: "ulid item ids",
		Body: boilerplateBody + "Rejected: UUIDv7 in canonical hex. Same 128 bits " +
			"and the same properties for ten more characters per id.",
	}); err != nil {
		t.Fatal(err)
	}

	res, _, err := s.grill(grillIn{ID: "P-0001"})
	out := resText(t, res, err)
	if strings.Contains(out, "q deliberation not addressed") {
		t.Fatalf("proposal with a Rejected: section should not be asked: %q", out)
	}
}

// TestGrillQuestionsNeverFiresForTasks: the deliberation question is
// proposal-only — a task with neither refs nor an alternative line never
// gets it.
func TestGrillQuestionsNeverFiresForTasks(t *testing.T) {
	root := t.TempDir()
	s := newTestServer(t, root)

	if _, _, err := s.draft(draftIn{
		Kind: "proposal", Title: "parent",
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.draft(draftIn{
		Kind: "task", Title: "plain task", Parent: "P-0001",
		Body: boilerplateBody + "Just implement it.",
	}); err != nil {
		t.Fatal(err)
	}

	res, _, err := s.grill(grillIn{ID: "T-0001"})
	out := resText(t, res, err)
	if strings.Contains(out, "q deliberation not addressed") {
		t.Fatalf("tasks must never get the deliberation question: %q", out)
	}
}
