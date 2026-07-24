package lifecycle

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/jxsl13/spectackle/internal/drift"
	"github.com/jxsl13/spectackle/internal/graph"
	"github.com/jxsl13/spectackle/internal/item"
	"github.com/jxsl13/spectackle/internal/journal"
	"github.com/jxsl13/spectackle/internal/workspace"
)

func ws(t *testing.T) workspace.Root {
	t.Helper()
	root := workspace.Root{Dir: t.TempDir()}
	if err := root.EnsureScaffold(""); err != nil {
		t.Fatal(err)
	}
	// nested context dir so scope mapping has something to snap to
	if err := root.EnsureScaffold("gpu"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root.SpecPath("gpu"), []byte("---\nschema: v0\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestDraftScopeMapping(t *testing.T) {
	root := ws(t)
	// path targets under gpu/ snap to the gpu context
	it, err := Draft(root, nil, "proposal", "kernel work", "", "", "", []string{"gpu/kern.cu", "gpu/host.go"})
	if err != nil || it.Dir != "gpu" || it.ID != "P-0001" || it.State != item.StateDraft {
		t.Fatalf("Draft = %+v, %v", it, err)
	}
	// no targets -> root; per-kind counter independent of proposals
	it2, err := Draft(root, nil, "task", "cleanup", "", "", "", nil)
	if err != nil || it2.Dir != "" || it2.ID != "T-0001" {
		t.Fatalf("Draft = %+v, %v", it2, err)
	}
	// explicit dir wins
	it3, err := Draft(root, nil, "bug", "b", "", "gpu", "", nil)
	if err != nil || it3.Dir != "gpu" {
		t.Fatalf("Draft dir = %+v, %v", it3, err)
	}
	// unknown kind and unknown parent are rejected
	if _, err := Draft(root, nil, "epic", "x", "", "", "", nil); err == nil {
		t.Fatal("unknown kind accepted")
	}
	if _, err := Draft(root, nil, "task", "x", "", "", "P-9999", nil); err == nil {
		t.Fatal("unknown parent accepted")
	}
}

func TestMoveGuards(t *testing.T) {
	root := ws(t)
	Draft(root, nil, "proposal", "p", "", "", "", nil)

	// forward skips are legal now (draft -> done in one call)
	if it, err := Move(root, "P-0001", item.StateDone, ""); err != nil || it.State != item.StateDone {
		t.Fatalf("forward skip draft->done: %+v, %v", it, err)
	}
	// backward, non-revocation transitions still name the allowed set
	if _, err := Move(root, "P-0001", item.StateSubmitted, ""); err == nil || !strings.Contains(err.Error(), "allowed") {
		t.Fatalf("illegal backward transition: %v", err)
	}
	// rejection requires a note
	if _, err := Move(root, "P-0001", item.StateRejected, " "); err == nil || !strings.Contains(err.Error(), "note") {
		t.Fatalf("noteless rejection: %v", err)
	}
	// unknown item
	if _, err := Move(root, "P-4242", item.StateActive, ""); err == nil {
		t.Fatal("unknown item accepted")
	}
}

// TestForwardSkipShortPath drives a proposal straight from draft to archived
// in two calls (draft->active, active->archived) and checks the archive
// effects fire exactly once even though `done` was never visited explicitly.
func TestForwardSkipShortPath(t *testing.T) {
	root := ws(t)
	Draft(root, nil, "proposal", "fast path", "Delta text.", "gpu", "", nil)

	if it, err := Move(root, "P-0001", item.StateActive, ""); err != nil || it.State != item.StateActive {
		t.Fatalf("draft->active: %+v, %v", it, err)
	}
	if it, err := Move(root, "P-0001", item.StateArchived, "shipped fast"); err != nil || it.State != item.StateArchived {
		t.Fatalf("active->archived (implies done): %+v, %v", it, err)
	}
	// intent line merged into spec.md
	raw, _ := os.ReadFile(root.SpecPath("gpu"))
	if !strings.Contains(string(raw), "P-0001 fast path: shipped fast") {
		t.Fatalf("intent merge missing:\n%s", raw)
	}
	// item left work.md
	if _, ok, _ := item.Get(root, "P-0001"); ok {
		t.Fatal("archived item still in work.md")
	}
	// archive effect ran exactly once
	archived := 0
	events, _ := journal.ReadAll(root)
	for _, e := range events {
		if e.Ev == journal.EvArchive {
			archived++
		}
	}
	if archived != 1 {
		t.Fatalf("expected 1 archive event, got %d", archived)
	}
}

// TestForwardSkipSingleCall checks that a single move can jump two states
// ahead (draft -> approved), skipping submitted.
func TestForwardSkipSingleCall(t *testing.T) {
	root := ws(t)
	Draft(root, nil, "proposal", "skip submitted", "", "", "", nil)
	it, err := Move(root, "P-0001", item.StateApproved, "")
	if err != nil || it.State != item.StateApproved {
		t.Fatalf("draft->approved: %+v, %v", it, err)
	}
}

// TestDoneRejectAndRevoke checks done items can be rejected (with a note),
// that the rejection snapshots the item, and that it can be revoked back
// into active (never straight back into done).
func TestDoneRejectAndRevoke(t *testing.T) {
	root := ws(t)
	Draft(root, nil, "task", "flaky check", "", "", "", nil)
	Move(root, "T-0001", item.StateDone, "")

	if _, err := Move(root, "T-0001", item.StateRejected, ""); err == nil {
		t.Fatal("noteless done->rejected accepted")
	}
	if it, err := Move(root, "T-0001", item.StateRejected, "flaked in CI"); err != nil || it.State != item.StateRejected {
		t.Fatalf("done->rejected: %+v, %v", it, err)
	}
	events, _ := journal.ReadAll(root)
	var rej *journal.Event
	for i := range events {
		if events[i].Ev == journal.EvReject {
			rej = &events[i]
		}
	}
	if rej == nil || rej.Note != "flaked in CI" {
		t.Fatalf("reject snapshot incomplete: %+v", rej)
	}
	// rejected -> done stays forbidden
	if _, err := Move(root, "T-0001", item.StateDone, ""); err == nil {
		t.Fatal("rejected->done accepted")
	}
	// revocation lands back in active, not done
	it, err := Move(root, "T-0001", item.StateActive, "")
	if err != nil || it.State != item.StateActive {
		t.Fatalf("rejected->active revocation: %+v, %v", it, err)
	}
}

// TestArchivedIsTerminal checks archived items reject every further move.
func TestArchivedIsTerminal(t *testing.T) {
	root := ws(t)
	Draft(root, nil, "task", "terminal", "", "", "", nil)
	Move(root, "T-0001", item.StateArchived, "")

	for _, to := range []string{item.StateDraft, item.StateSubmitted, item.StateApproved, item.StateActive, item.StateDone, item.StateRejected} {
		if _, err := Move(root, "T-0001", to, "note"); err == nil {
			t.Fatalf("archived->%s accepted", to)
		}
	}
}

// TestDoneActiveReopen checks the one preserved backward hop still works
// alongside the new forward-skip rules.
func TestDoneActiveReopen(t *testing.T) {
	root := ws(t)
	Draft(root, nil, "task", "reopen me", "", "", "", nil)
	Move(root, "T-0001", item.StateDone, "")
	it, err := Move(root, "T-0001", item.StateActive, "")
	if err != nil || it.State != item.StateActive {
		t.Fatalf("done->active reopen: %+v, %v", it, err)
	}
}

func TestRejectionSnapshotAndRevocation(t *testing.T) {
	root := ws(t)
	Draft(root, nil, "proposal", "vram cache", "Keep kernels resident.", "", "", []string{"gpu/kern.cu"})
	if _, err := Move(root, "P-0001", item.StateRejected, "breaks multi-tenant scheduling"); err != nil {
		t.Fatal(err)
	}
	// gone from work.md
	if _, ok, _ := item.Get(root, "P-0001"); ok {
		t.Fatal("rejected item still in work.md")
	}
	// snapshot in journal
	events, _ := journal.ReadAll(root)
	var rej *journal.Event
	for i := range events {
		if events[i].Ev == journal.EvReject {
			rej = &events[i]
		}
	}
	if rej == nil || rej.Body != "Keep kernels resident." || len(rej.Tg) != 1 || rej.Note == "" {
		t.Fatalf("reject snapshot incomplete: %+v", rej)
	}
	// revocation restores the full item into a previous state
	it, err := Move(root, "P-0001", item.StateDraft, "")
	if err != nil || it.State != item.StateDraft {
		t.Fatalf("revocation = %+v, %v", it, err)
	}
	got, ok, _ := item.Get(root, "P-0001")
	if !ok || got.Body != "Keep kernels resident." || len(got.Targets) != 1 {
		t.Fatalf("restored item incomplete: %+v", got)
	}
	// ID minting does not reuse the rejected/restored number
	it2, _ := Draft(root, nil, "proposal", "next", "", "", "", nil)
	if it2.ID != "P-0002" {
		t.Fatalf("counter reused an ID: %s", it2.ID)
	}
}

// foldToCompactSurvivors rewrites a context dir's journal down to only the
// event kinds that survive compaction (see internal/mcpserver's compact
// tool: it folds create/move/rule/drift away and keeps only
// reject/archive/compact/escalate/decide). Used by the maxNum regression
// tests below to simulate "the create event for this id is gone".
func foldToCompactSurvivors(t *testing.T, root workspace.Root, ctx string) {
	t.Helper()
	events, err := journal.Read(root, ctx)
	if err != nil {
		t.Fatal(err)
	}
	var kept []journal.Event
	for _, e := range events {
		switch e.Ev {
		case journal.EvReject, journal.EvArchive, journal.EvCompact,
			journal.EvEscalate, journal.EvDecide:
			kept = append(kept, e)
		}
	}
	if len(kept) == 0 {
		t.Fatal("nothing survived the simulated fold — test setup is wrong")
	}
	if err := journal.Rewrite(root, ctx, kept); err != nil {
		t.Fatal(err)
	}
}

// TestMaxNumSurvivesCompactAfterArchive is the regression for the maxNum
// bug: it used to scan only journal.EvCreate events for the highest used
// number, but compact folds create events away and keeps only the archive
// tombstone. After a compact, an archived item's id looked unused and got
// minted again — this happened live twice in this repo (ADR-0001..0004 and
// P-0067 both re-minted after a compact). maxNum must also count archive
// events as witnesses of a used id.
func TestMaxNumSurvivesCompactAfterArchive(t *testing.T) {
	root := ws(t)
	if _, err := Draft(root, nil, "task", "flaky check", "", "", "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := Move(root, "T-0001", item.StateArchived, ""); err != nil {
		t.Fatal(err)
	}

	foldToCompactSurvivors(t, root, "")

	// only the archive tombstone remains for T-0001 — no create event, no
	// work.md row (archived items leave work.md). The next task draft must
	// not reuse T-0001.
	it, err := Draft(root, nil, "task", "second task", "", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if it.ID != "T-0002" {
		t.Fatalf("expected T-0002 after compact, got %s (reused a folded-away archived id)", it.ID)
	}
}

// TestMaxNumSurvivesCompactAfterReject mirrors the archive regression above
// for rejected items: reject events also survive compaction and must also
// count as witnesses of a used id.
func TestMaxNumSurvivesCompactAfterReject(t *testing.T) {
	root := ws(t)
	if _, err := Draft(root, nil, "bug", "flaky", "", "", "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := Move(root, "B-0001", item.StateRejected, "not reproducible"); err != nil {
		t.Fatal(err)
	}

	foldToCompactSurvivors(t, root, "")

	it, err := Draft(root, nil, "bug", "second bug", "", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if it.ID != "B-0002" {
		t.Fatalf("expected B-0002 after compact, got %s (reused a folded-away rejected id)", it.ID)
	}
}

// TestMaxNumBoundByLiveWorkItem checks the floor still honors an item that
// exists only in work.md (no journal event at all yet) — the fix widens the
// journal scan but must not stop bounding the floor by live items.
func TestMaxNumBoundByLiveWorkItem(t *testing.T) {
	root := ws(t)
	if err := item.Upsert(root, item.Item{
		ID: "P-0005", Kind: "proposal", State: item.StateActive, Title: "manually seeded",
	}); err != nil {
		t.Fatal(err)
	}
	it, err := Draft(root, nil, "proposal", "next", "", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if it.ID != "P-0006" {
		t.Fatalf("expected P-0006, got %s", it.ID)
	}
}

// TestMaxNumEmptyWorkspaceStartsAtOne checks an empty workspace (no journal
// events, no items of the kind) still starts minting at 0001.
func TestMaxNumEmptyWorkspaceStartsAtOne(t *testing.T) {
	root := ws(t)
	it, err := Draft(root, nil, "research", "first", "", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if it.ID != "R-0001" {
		t.Fatalf("expected R-0001 in an empty workspace, got %s", it.ID)
	}
}

// TestReopenIncrementsAndEscalates drives an item through the feedback loop:
// two reopens succeed and increment Rounds, the third exhausts the
// (deliberately small) round budget — Move returns ErrRoundsExhausted and
// leaves the item on done, and the caller (simulated here, mcpserver in a
// later task) escalates it into blocked with a linked ADR- adr item.
func TestReopenIncrementsAndEscalates(t *testing.T) {
	root := ws(t)
	root.Cfg.Feedback.MaxRounds = 2
	Draft(root, nil, "task", "flaky feedback loop", "", "", "", nil)
	if _, err := Move(root, "T-0001", item.StateDone, ""); err != nil {
		t.Fatal(err)
	}

	// reopen 1: succeeds, Rounds becomes 1
	it, err := Move(root, "T-0001", item.StateActive, "")
	if err != nil || it.State != item.StateActive || it.Rounds != 1 {
		t.Fatalf("reopen 1 = %+v, %v", it, err)
	}
	if _, err := Move(root, "T-0001", item.StateDone, ""); err != nil {
		t.Fatal(err)
	}

	// reopen 2 hits MaxRounds(2): Move refuses with ErrRoundsExhausted and
	// leaves the item on done with the counter persisted at 2.
	_, err = Move(root, "T-0001", item.StateActive, "")
	var exhausted ErrRoundsExhausted
	if !errors.As(err, &exhausted) {
		t.Fatalf("expected ErrRoundsExhausted, got %v", err)
	}
	if exhausted.Item.Rounds != 2 {
		t.Fatalf("exhausted item rounds = %d, want 2", exhausted.Item.Rounds)
	}
	stuck, ok, _ := item.Get(root, "T-0001")
	if !ok || stuck.State != item.StateDone || stuck.Rounds != 2 {
		t.Fatalf("item did not stay done with persisted rounds: %+v", stuck)
	}

	// caller catches the error and escalates
	escalated, decision, err := Escalate(root, nil, exhausted.Item)
	if err != nil {
		t.Fatal(err)
	}
	if escalated.State != item.StateBlocked {
		t.Fatalf("escalated state = %s, want blocked", escalated.State)
	}
	if decision.Kind != "adr" || decision.ID != "ADR-0001" || decision.State != item.StateDraft {
		t.Fatalf("decision item = %+v", decision)
	}
	if len(escalated.Needs) != 1 || escalated.Needs[0] != decision.ID {
		t.Fatalf("Needs not linked: %+v", escalated.Needs)
	}
	if !strings.Contains(decision.Title, "rescope") || !strings.Contains(decision.Title, "reject") ||
		!strings.Contains(decision.Title, "override-once") {
		t.Fatalf("decision title missing options: %q", decision.Title)
	}
	if decision.Parent != "T-0001" {
		t.Fatalf("decision parent = %q, want T-0001", decision.Parent)
	}

	// persisted: blocked item shows up as blocked on reload
	blocked, ok, _ := item.Get(root, "T-0001")
	if !ok || blocked.State != item.StateBlocked {
		t.Fatalf("blocked item not persisted: %+v", blocked)
	}
	// journal carries the escalate event
	events, _ := journal.ReadAll(root)
	found := false
	for _, e := range events {
		if e.Ev == journal.EvEscalate && e.ID == "T-0001" {
			found = true
			if len(e.Nd) != 1 || e.Nd[0] != "ADR-0001" {
				t.Fatalf("escalate event Nd = %+v", e.Nd)
			}
		}
	}
	if !found {
		t.Fatal("no escalate journal event")
	}

	// blocked items refuse every move, naming the linked decision
	if _, err := Move(root, "T-0001", item.StateActive, ""); err == nil ||
		!strings.Contains(err.Error(), "blocked — resolve via decide ADR-0001") {
		t.Fatalf("blocked item movable or wrong message: %v", err)
	}
	if _, err := Move(root, "T-0001", item.StateDraft, ""); err == nil ||
		!strings.Contains(err.Error(), "blocked") {
		t.Fatalf("blocked item movable to draft: %v", err)
	}
}

// TestResolveBlockedOutcomes exercises all three ResolveBlocked outcomes and
// checks override-once really is one-shot.
func TestResolveBlockedOutcomes(t *testing.T) {
	setupBlocked := func(t *testing.T) workspace.Root {
		t.Helper()
		root := ws(t)
		root.Cfg.Feedback.MaxRounds = 1
		Draft(root, nil, "task", "needs a decision", "", "", "", nil)
		Move(root, "T-0001", item.StateDone, "")
		_, err := Move(root, "T-0001", item.StateActive, "")
		var exhausted ErrRoundsExhausted
		if !errors.As(err, &exhausted) {
			t.Fatalf("setup: expected exhaustion, got %v", err)
		}
		if _, _, err := Escalate(root, nil, exhausted.Item); err != nil {
			t.Fatalf("setup: escalate: %v", err)
		}
		return root
	}

	t.Run("rescope", func(t *testing.T) {
		root := setupBlocked(t)
		resc, err := ResolveBlocked(root, "T-0001", "rescope", "narrower scope")
		if err != nil || resc.State != item.StateDraft || resc.Rounds != 0 {
			t.Fatalf("rescope = %+v, %v", resc, err)
		}
		got, ok, _ := item.Get(root, "T-0001")
		if !ok || got.State != item.StateDraft {
			t.Fatalf("rescope not persisted: %+v", got)
		}
	})

	t.Run("reject", func(t *testing.T) {
		root := setupBlocked(t)
		if _, err := ResolveBlocked(root, "T-0001", "reject", ""); err == nil {
			t.Fatal("noteless blocked-reject accepted")
		}
		rej, err := ResolveBlocked(root, "T-0001", "reject", "not worth it")
		if err != nil || rej.State != item.StateRejected {
			t.Fatalf("reject = %+v, %v", rej, err)
		}
		if _, ok, _ := item.Get(root, "T-0001"); ok {
			t.Fatal("rejected-via-decision item still in work.md")
		}
		events, _ := journal.ReadAll(root)
		var found bool
		for _, e := range events {
			if e.Ev == journal.EvReject && e.ID == "T-0001" {
				found = true
				if e.Rnd != 1 {
					t.Fatalf("reject snapshot Rnd = %d, want 1", e.Rnd)
				}
			}
		}
		if !found {
			t.Fatal("no reject journal event from ResolveBlocked")
		}
	})

	t.Run("override-once", func(t *testing.T) {
		root := setupBlocked(t)
		ov, err := ResolveBlocked(root, "T-0001", "override-once", "")
		if err != nil || ov.State != item.StateActive || ov.Rounds != 0 || !ov.Override {
			t.Fatalf("override-once = %+v, %v", ov, err)
		}
		// exhaust again — override already spent this time
		Move(root, "T-0001", item.StateDone, "")
		_, err = Move(root, "T-0001", item.StateActive, "")
		var exhausted ErrRoundsExhausted
		if !errors.As(err, &exhausted) {
			t.Fatalf("expected 2nd exhaustion, got %v", err)
		}
		if !exhausted.Item.Override {
			t.Fatal("Override not preserved across escalation")
		}
		_, decision, err := Escalate(root, nil, exhausted.Item)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(decision.Title, "override-once") {
			t.Fatalf("override-once still offered after being spent: %q", decision.Title)
		}
		if _, err := ResolveBlocked(root, "T-0001", "override-once", ""); err == nil {
			t.Fatal("second override-once accepted")
		}
	})
}

// TestRejectSnapshotKeepsFeedbackFields checks the reject journal snapshot
// (taken via Move, not ResolveBlocked) carries Rounds/Grilled/Needs/Override
// through a reject -> revoke roundtrip.
func TestRejectSnapshotKeepsFeedbackFields(t *testing.T) {
	root := ws(t)
	Draft(root, nil, "task", "snapshot", "", "", "", nil)
	it, _, _ := item.Get(root, "T-0001")
	it.Rounds = 2
	it.Grilled = "improve coverage"
	it.Needs = []string{"D-0007"}
	it.Override = true
	if err := item.Upsert(root, it); err != nil {
		t.Fatal(err)
	}
	if _, err := Move(root, "T-0001", item.StateRejected, "not needed anymore"); err != nil {
		t.Fatal(err)
	}
	revoked, err := Move(root, "T-0001", item.StateDraft, "")
	if err != nil {
		t.Fatal(err)
	}
	if revoked.Rounds != 2 || revoked.Grilled != "improve coverage" ||
		len(revoked.Needs) != 1 || revoked.Needs[0] != "D-0007" || !revoked.Override {
		t.Fatalf("snapshot roundtrip lost feedback fields: %+v", revoked)
	}
}

// TestTombstone checks the read-only afterlife of an archived item: Tombstone
// reconstructs it from its archive journal event (state=archived, kind/title
// carried over, body = journal summary), and reports ok=false for anything
// that was never archived.
func TestTombstone(t *testing.T) {
	root := ws(t)
	Draft(root, nil, "proposal", "vram cache", "Keep kernels resident.", "gpu", "", nil)
	if _, err := Move(root, "P-0001", item.StateArchived, "shipped"); err != nil {
		t.Fatal(err)
	}

	tomb, ok, err := Tombstone(root, "P-0001")
	if err != nil || !ok {
		t.Fatalf("Tombstone(P-0001) = %+v, %v, %v", tomb, ok, err)
	}
	if tomb.State != item.StateArchived || tomb.Kind != "proposal" || tomb.Title != "vram cache" {
		t.Fatalf("tombstone incomplete: %+v", tomb)
	}

	if _, ok, err := Tombstone(root, "P-9999"); err != nil || ok {
		t.Fatalf("Tombstone(unknown) = ok=%v, err=%v, want ok=false", ok, err)
	}
}

// TestDraftArchivedParent checks Draft accepts an archived (Tombstone-only)
// parent as provenance while still rejecting a parent that resolves nowhere.
func TestDraftArchivedParent(t *testing.T) {
	root := ws(t)
	Draft(root, nil, "proposal", "vram cache", "", "gpu", "", nil)
	if _, err := Move(root, "P-0001", item.StateArchived, "shipped"); err != nil {
		t.Fatal(err)
	}

	it, err := Draft(root, nil, "task", "follow-up", "", "", "P-0001", nil)
	if err != nil || it.Parent != "P-0001" {
		t.Fatalf("Draft with archived parent = %+v, %v", it, err)
	}
	if _, err := Draft(root, nil, "task", "orphan", "", "", "P-9999", nil); err == nil {
		t.Fatal("Draft with unknown parent accepted")
	}
}

func TestArchiveMergesIntentAndFoldsChildren(t *testing.T) {
	root := ws(t)
	Draft(root, nil, "proposal", "strided access", "Delta text.", "gpu", "", nil)
	Draft(root, nil, "task", "kernel change", "", "gpu", "P-0001", nil)

	Move(root, "P-0001", item.StateSubmitted, "")
	Move(root, "P-0001", item.StateApproved, "")
	Move(root, "P-0001", item.StateActive, "")
	Move(root, "T-0001", item.StateActive, "")

	// open child blocks the parent's done->archived path
	Move(root, "P-0001", item.StateDone, "")
	if _, err := Move(root, "P-0001", item.StateArchived, ""); err == nil || !strings.Contains(err.Error(), "T-0001") {
		t.Fatalf("open child not enforced: %v", err)
	}
	Move(root, "T-0001", item.StateDone, "")
	if _, err := Move(root, "P-0001", item.StateArchived, "shipped"); err != nil {
		t.Fatal(err)
	}
	// delta merged into the gpu spec's intent section
	raw, _ := os.ReadFile(root.SpecPath("gpu"))
	if !strings.Contains(string(raw), "## intent") || !strings.Contains(string(raw), "P-0001 strided access: shipped") {
		t.Fatalf("intent merge missing:\n%s", raw)
	}
	// both parent and done child left work.md
	all, _ := item.LoadAll(root)
	if len(all) != 0 {
		t.Fatalf("items left behind after archive: %+v", all)
	}
	// archive events journaled for both
	archived := 0
	events, _ := journal.ReadAll(root)
	for _, e := range events {
		if e.Ev == journal.EvArchive {
			archived++
		}
	}
	if archived != 2 {
		t.Fatalf("expected 2 archive events, got %d", archived)
	}
}

// --- audit gate (WithAuditGate / auditGate) -------------------------------
//
// T-0089: an item cannot reach done while its bound contracts carry
// unresolved audit-class drift (drift.Tightened or drift.Diverged).
// drift.Evolved is the mechanically healable class and must never block.

// ruleTexts builds the func(string)(string,bool) closure WithAuditGate
// expects, mirroring the one internal/mcpserver/tools.go's check tool builds
// from a loaded spec.Cascade (rule ID -> current sentence, still-exists).
func ruleTexts(m map[string]string) func(string) (string, bool) {
	return func(id string) (string, bool) {
		t, ok := m[id]
		return t, ok
	}
}

// auditFixture writes a one-line Go "file", indexes it as a single graph
// node, and stamps+saves an anchor binding ruleID/ruleText to that node —
// the minimal setup every audit-gate test below builds on.
func auditFixture(t *testing.T, root workspace.Root, ruleID, ruleText string) (graph.Graph, graph.NodeID) {
	t.Helper()
	const node graph.NodeID = "go:pkg.Func"
	if err := os.MkdirAll(root.Dir+"/pkg", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root.Dir+"/pkg/x.go", []byte("func Func() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	g := graph.NewMem()
	g.Upsert([]graph.Node{{ID: node, File: "pkg/x.go", Line: 1, EndLine: 1}}, nil)
	a := drift.Stamp(root, g, ruleID, ruleText, node)
	if err := drift.Save(root, []drift.Anchor{a}); err != nil {
		t.Fatal(err)
	}
	return g, node
}

// TestAuditGateBlocksTightened: rule sentence changes, code doesn't -> the
// anchor classifies Tightened. Move to done must refuse, naming the rule and
// node in a dense "! GATE E ..." record, and must leave the item untouched.
func TestAuditGateBlocksTightened(t *testing.T) {
	root := ws(t)
	g, node := auditFixture(t, root, "EARS-001", "old rule text")
	it, err := Draft(root, nil, "task", "audited work", "", "", "", []string{string(node)})
	if err != nil {
		t.Fatal(err)
	}
	rt := ruleTexts(map[string]string{"EARS-001": "new rule text"})

	_, err = Move(root, it.ID, item.StateDone, "", WithAuditGate(g, rt))
	if err == nil {
		t.Fatal("tightened anchor did not block move to done")
	}
	want := "! GATE E " + it.ID + " audit EARS-001 " + string(node) + " tightened"
	if err.Error() != want {
		t.Fatalf("refusal text = %q, want %q", err.Error(), want)
	}
	got, ok, _ := item.Get(root, it.ID)
	if !ok || got.State != item.StateDraft {
		t.Fatalf("item moved despite refusal: %+v", got)
	}
}

// TestAuditGateSucceedsAfterReconcile: once the anchor is re-stamped against
// the current rule text (the human resolved the drift), the same move to
// done succeeds.
func TestAuditGateSucceedsAfterReconcile(t *testing.T) {
	root := ws(t)
	g, node := auditFixture(t, root, "EARS-001", "old rule text")
	it, err := Draft(root, nil, "task", "audited work", "", "", "", []string{string(node)})
	if err != nil {
		t.Fatal(err)
	}
	rt := ruleTexts(map[string]string{"EARS-001": "new rule text"})
	if _, err := Move(root, it.ID, item.StateDone, "", WithAuditGate(g, rt)); err == nil {
		t.Fatal("expected the tightened anchor to block the first attempt")
	}

	anchors, err := drift.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	reconciled := drift.Upsert(anchors, drift.Stamp(root, g, "EARS-001", "new rule text", node))
	if err := drift.Save(root, reconciled); err != nil {
		t.Fatal(err)
	}

	done, err := Move(root, it.ID, item.StateDone, "", WithAuditGate(g, rt))
	if err != nil || done.State != item.StateDone {
		t.Fatalf("move after reconcile = %+v, %v", done, err)
	}
}

// TestAuditGateEvolvedNeverBlocks: code changes, rule sentence identical ->
// Evolved, the mechanically-healable class. It must never block done.
func TestAuditGateEvolvedNeverBlocks(t *testing.T) {
	root := ws(t)
	g, node := auditFixture(t, root, "EARS-001", "same rule text")
	it, err := Draft(root, nil, "task", "audited work", "", "", "", []string{string(node)})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root.Dir+"/pkg/x.go", []byte("func Func() { /* changed */ }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rt := ruleTexts(map[string]string{"EARS-001": "same rule text"})

	done, err := Move(root, it.ID, item.StateDone, "", WithAuditGate(g, rt))
	if err != nil || done.State != item.StateDone {
		t.Fatalf("evolved anchor blocked move to done: %+v, %v", done, err)
	}
}

// TestAuditGateNoBoundAnchorsUnaffected: an anchor bound to a node the item
// does NOT target must not affect that item's move, tightened or not.
func TestAuditGateNoBoundAnchorsUnaffected(t *testing.T) {
	root := ws(t)
	g, _ := auditFixture(t, root, "EARS-001", "old rule text") // bound to go:pkg.Func
	it, err := Draft(root, nil, "task", "unrelated work", "", "", "", []string{"go:pkg.Other"})
	if err != nil {
		t.Fatal(err)
	}
	rt := ruleTexts(map[string]string{"EARS-001": "new rule text"}) // would classify Tightened, but for a different node

	done, err := Move(root, it.ID, item.StateDone, "", WithAuditGate(g, rt))
	if err != nil || done.State != item.StateDone {
		t.Fatalf("unrelated anchor blocked move to done: %+v, %v", done, err)
	}
}

// TestAuditGateSkippedWithoutOption: every pre-existing call site of Move
// (positional ws, id, to, note — no opts) must keep behaving exactly as
// before this gate was added, even when a tightened anchor is bound to the
// item's target.
func TestAuditGateSkippedWithoutOption(t *testing.T) {
	root := ws(t)
	_, node := auditFixture(t, root, "EARS-001", "old rule text")
	it, err := Draft(root, nil, "task", "audited work", "", "", "", []string{string(node)})
	if err != nil {
		t.Fatal(err)
	}
	// rule text changed underneath (would classify Tightened), but Move is
	// called the old way — no WithAuditGate, so no drift check runs at all.
	done, err := Move(root, it.ID, item.StateDone, "")
	if err != nil || done.State != item.StateDone {
		t.Fatalf("move without WithAuditGate was blocked: %+v, %v", done, err)
	}
}
