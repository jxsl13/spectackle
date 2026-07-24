package replay

import (
	"os"
	"testing"

	"github.com/jxsl13/spectacle/internal/coord"
	"github.com/jxsl13/spectacle/internal/drift"
	"github.com/jxsl13/spectacle/internal/graph"
	"github.com/jxsl13/spectacle/internal/item"
	"github.com/jxsl13/spectacle/internal/journal"
	"github.com/jxsl13/spectacle/internal/workspace"
	"github.com/jxsl13/spectacle/internal/wt"
)

// mainRepo sets up a git-backed main workspace so replay's baseline/recent
// eid lookups (which shell out to git show) have something to read.
func mainRepo(t *testing.T) (workspace.Root, string) {
	t.Helper()
	dir := t.TempDir()
	main := workspace.Root{Dir: dir}
	if err := main.EnsureScaffold(""); err != nil {
		t.Fatal(err)
	}
	if err := wt.InitTestRepo(dir); err != nil {
		t.Skipf("git unavailable: %v", err)
	}
	base, err := wt.Head(dir)
	if err != nil {
		t.Fatal(err)
	}
	return main, base
}

func openCoord(t *testing.T, main workspace.Root) *coord.DB {
	t.Helper()
	if err := os.MkdirAll(main.CacheDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	cd, err := coord.Open(main.CoordPath(), "tester", 1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cd.Close() })
	return cd
}

// TestRunToleratesFeedbackEvents replays a worktree journal delta that mixes
// ordinary create/move events with the new SDD orchestration v2 event kinds
// (grill/escalate/decide, T-0030) and checks Run applies the whole delta
// without erroring — old replay code has no bespoke case for these, but the
// default no-op switch branch plus the touchedItems reconciliation must
// still tolerate them cleanly.
func TestRunToleratesFeedbackEvents(t *testing.T) {
	main, base := mainRepo(t)

	wtDir := t.TempDir()
	wtWS := workspace.Root{Dir: wtDir}
	if err := wtWS.EnsureScaffold(""); err != nil {
		t.Fatal(err)
	}

	mixed := []journal.Event{
		{Ev: journal.EvCreate, ID: "T-0001", K: "task", Ti: "grilled task"},
		{Ev: journal.EvMove, ID: "T-0001", Fr: item.StateDraft, To: item.StateDone},
		{Ev: journal.EvGrill, ID: "T-0001", Gr: "needs more test coverage"},
		{Ev: journal.EvMove, ID: "T-0001", Fr: item.StateDone, To: item.StateActive, Rnd: 3},
		{Ev: journal.EvEscalate, ID: "T-0001", Note: "D-0001", Rnd: 3, Nd: []string{"D-0001"}},
		{Ev: journal.EvDecide, ID: "T-0001", To: item.StateDraft, Note: "rescope: split into two tasks"},
	}
	for _, e := range mixed {
		if err := journal.Append(wtWS, "", e); err != nil {
			t.Fatal(err)
		}
	}
	// final materialized state in the worktree's work.md, as lifecycle would
	// leave it after the mixed delta above (escalated then resolved via
	// rescope back to draft).
	final := item.Item{
		ID: "T-0001", Kind: "task", State: item.StateDraft, Title: "grilled task",
		Grilled: "needs more test coverage",
	}
	if err := item.Upsert(wtWS, final); err != nil {
		t.Fatal(err)
	}

	cd := openCoord(t, main)
	rep, err := Run(main, wtWS, "T-0001", base, cd, graph.NewMem())
	if err != nil {
		t.Fatalf("Run returned error on mixed grill/escalate/decide journal: %v", err)
	}
	if rep.Events != len(mixed) {
		t.Fatalf("Events = %d, want %d", rep.Events, len(mixed))
	}

	// every event kind landed verbatim in main's journal (history, even the
	// ones replay does not specially interpret)
	mainEvents, err := journal.Read(main, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(mainEvents) != len(mixed) {
		t.Fatalf("main journal has %d events, want %d", len(mainEvents), len(mixed))
	}
	kinds := map[string]bool{}
	for _, e := range mainEvents {
		kinds[e.Ev] = true
	}
	for _, k := range []string{journal.EvGrill, journal.EvEscalate, journal.EvDecide} {
		if !kinds[k] {
			t.Fatalf("event kind %s missing from replayed main journal", k)
		}
	}

	// item state reconciled onto main from the worktree's final snapshot
	got, ok, err := item.Get(main, "T-0001")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || got.State != item.StateDraft || got.Grilled != "needs more test coverage" {
		t.Fatalf("item not reconciled onto main: %+v ok=%v", got, ok)
	}
}

// TestRunSyncsItemOnDecideOnlyDelta isolates the touchedItems effect of a
// decide-only delta (no accompanying create/move in the same batch, as would
// happen when a later replay round only carries the escalation's
// resolution): the item's post-decision state from the worktree's work.md
// must still land on main.
func TestRunSyncsItemOnDecideOnlyDelta(t *testing.T) {
	main, base := mainRepo(t)

	wtDir := t.TempDir()
	wtWS := workspace.Root{Dir: wtDir}
	if err := wtWS.EnsureScaffold(""); err != nil {
		t.Fatal(err)
	}
	if err := journal.Append(wtWS, "", journal.Event{
		Ev: journal.EvDecide, ID: "T-0001", To: item.StateActive, Ov: true,
	}); err != nil {
		t.Fatal(err)
	}
	resolved := item.Item{ID: "T-0001", Kind: "task", State: item.StateActive, Title: "override once", Override: true}
	if err := item.Upsert(wtWS, resolved); err != nil {
		t.Fatal(err)
	}

	cd := openCoord(t, main)
	rep, err := Run(main, wtWS, "T-0001", base, cd, graph.NewMem())
	if err != nil {
		t.Fatalf("Run returned error on decide-only delta: %v", err)
	}
	if rep.Events != 1 {
		t.Fatalf("Events = %d, want 1", rep.Events)
	}
	got, ok, err := item.Get(main, "T-0001")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || got.State != item.StateActive || !got.Override {
		t.Fatalf("decide-only delta not reconciled onto main: %+v ok=%v", got, ok)
	}
}

// TestRunReconcilesStaleAnchorsOnRuleEdit is the regression test for T-0044
// (DRF-001): replaying a worktree journal that adds a rule anchored to two
// nodes and then edits it down to one node must converge main's
// anchors.tsv to exactly the final applies set — not merely upsert the
// surviving node and leave the dropped one's row stale forever, which is
// what left IDX-001's mistyped go:index.Indexer.IndexAll row behind live in
// this repo.
func TestRunReconcilesStaleAnchorsOnRuleEdit(t *testing.T) {
	main, base := mainRepo(t)

	wtDir := t.TempDir()
	wtWS := workspace.Root{Dir: wtDir}
	if err := wtWS.EnsureScaffold(""); err != nil {
		t.Fatal(err)
	}

	const sentence = "The system SHALL persist the snapshot to `disk.log`."
	if err := journal.Append(wtWS, "", journal.Event{
		Ev: journal.EvRule, Op: "add", Rule: "DRF-PRB-001", Txt: sentence,
		Ap: []string{"go:pkg.A", "go:pkg.B"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := journal.Append(wtWS, "", journal.Event{
		Ev: journal.EvRule, Op: "edit", Rule: "DRF-PRB-001", Txt: sentence,
		Ap: []string{"go:pkg.B"},
	}); err != nil {
		t.Fatal(err)
	}

	cd := openCoord(t, main)
	rep, err := Run(main, wtWS, "DRF-PRB-001", base, cd, graph.NewMem())
	if err != nil {
		t.Fatalf("Run returned error replaying a rule add+edit: %v", err)
	}
	if rep.Rules != 2 {
		t.Fatalf("Rules = %d, want 2", rep.Rules)
	}

	anchors, err := drift.Load(main)
	if err != nil {
		t.Fatal(err)
	}
	rows := map[string]bool{}
	for _, a := range anchors {
		if a.Rule == "DRF-PRB-001" {
			rows[string(a.Node)] = true
		}
	}
	if len(rows) != 1 || !rows["go:pkg.B"] {
		t.Fatalf("replayed edit must reconcile away go:pkg.A, leaving only go:pkg.B: %v", rows)
	}
}
