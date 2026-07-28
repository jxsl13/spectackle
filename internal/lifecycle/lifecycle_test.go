package lifecycle

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

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
	if err := os.WriteFile(root.SpecPath("gpu"), []byte("---\nschema: v1\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// draftID drafts an item and returns the ID the server minted for it.
//
// Since ADR-0013 no test may hardcode a minted ID: item.MintID derives a
// UUIDv7, so the value is different on every run by construction. Reading the
// ID back from the item under test is also the more honest assertion — it
// checks the lifecycle wired the item it says it created, rather than that a
// counter happened to be at a particular value.
func draftID(t *testing.T, root workspace.Root, kind, title, body, dir, parent string, targets []string) string {
	t.Helper()
	it, err := Draft(root, nil, kind, title, body, dir, parent, targets)
	if err != nil {
		t.Fatalf("Draft(%s, %q): %v", kind, title, err)
	}
	if !item.IDRe.MatchString(it.ID) || !strings.HasPrefix(it.ID, item.Letter(kind)+"-") {
		t.Fatalf("Draft(%s) minted %q, which is not a %s ID", kind, it.ID, kind)
	}
	return it.ID
}

func TestDraftScopeMapping(t *testing.T) {
	root := ws(t)
	// path targets under gpu/ snap to the gpu context
	it, err := Draft(root, nil, "proposal", "kernel work", "", "", "", []string{"gpu/kern.cu", "gpu/host.go"})
	if err != nil || it.Dir != "gpu" || it.State != item.StateDraft {
		t.Fatalf("Draft = %+v, %v", it, err)
	}
	// ADR-0013: the ID is a kind-prefixed record ID, not a counter
	if !item.IDRe.MatchString(it.ID) || !strings.HasPrefix(it.ID, "P-") ||
		item.LegacyIDRe.MatchString(it.ID) {
		t.Fatalf("Draft minted %q, want a P- record ID", it.ID)
	}
	// no targets -> root
	it2, err := Draft(root, nil, "task", "cleanup", "", "", "", nil)
	if err != nil || it2.Dir != "" || !strings.HasPrefix(it2.ID, "T-") {
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

// TestDraftMintsUniqueIDsInTightLoop is the lifecycle half of the ADR-0013
// collision fix. R-0006 reproduced two clones minting the same sequential ID
// for different work, which replay then collapsed into one record. Drafting
// back to back — the tightest loop the lifecycle allows, many drafts inside
// the same millisecond — must never produce the same ID twice, and every one
// of them must still be a well-formed, resolvable item.
func TestDraftMintsUniqueIDsInTightLoop(t *testing.T) {
	root := ws(t)
	const n = 60
	seen := make(map[string]bool, n)
	for i := 0; i < n; i++ {
		it, err := Draft(root, nil, "task", "tight loop", "", "", "", nil)
		if err != nil {
			t.Fatalf("draft %d: %v", i, err)
		}
		if seen[it.ID] {
			t.Fatalf("draft %d re-minted %q", i, it.ID)
		}
		seen[it.ID] = true
		if got, ok, err := item.Get(root, it.ID); err != nil || !ok || got.Title != "tight loop" {
			t.Fatalf("draft %d not retrievable by its own ID %q: %+v ok=%v err=%v", i, it.ID, got, ok, err)
		}
	}
	all, err := item.LoadAll(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != n {
		t.Fatalf("work.md holds %d items after %d drafts — IDs collapsed", len(all), n)
	}
}

func TestMoveGuards(t *testing.T) {
	root := ws(t)
	p := draftID(t, root, "proposal", "p", "", "", "", nil)

	// forward skips are legal now (draft -> done in one call)
	if it, err := Move(root, p, item.StateDone, ""); err != nil || it.State != item.StateDone {
		t.Fatalf("forward skip draft->done: %+v, %v", it, err)
	}
	// backward, non-revocation transitions still name the allowed set
	if _, err := Move(root, p, item.StateSubmitted, ""); err == nil || !strings.Contains(err.Error(), "allowed") {
		t.Fatalf("illegal backward transition: %v", err)
	}
	// rejection requires a note
	if _, err := Move(root, p, item.StateRejected, " "); err == nil || !strings.Contains(err.Error(), "note") {
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
	p := draftID(t, root, "proposal", "fast path", "Delta text.", "gpu", "", nil)

	if it, err := Move(root, p, item.StateActive, ""); err != nil || it.State != item.StateActive {
		t.Fatalf("draft->active: %+v, %v", it, err)
	}
	if it, err := Move(root, p, item.StateArchived, "shipped fast"); err != nil || it.State != item.StateArchived {
		t.Fatalf("active->archived (implies done): %+v, %v", it, err)
	}
	// intent line merged into spec.md
	raw, _ := os.ReadFile(root.SpecPath("gpu"))
	if !strings.Contains(string(raw), p+" fast path: shipped fast") {
		t.Fatalf("intent merge missing:\n%s", raw)
	}
	// item left work.md
	if _, ok, _ := item.Get(root, p); ok {
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
	p := draftID(t, root, "proposal", "skip submitted", "", "", "", nil)
	it, err := Move(root, p, item.StateApproved, "")
	if err != nil || it.State != item.StateApproved {
		t.Fatalf("draft->approved: %+v, %v", it, err)
	}
}

// TestDoneRejectAndRevoke checks done items can be rejected (with a note),
// that the rejection snapshots the item, and that it can be revoked back
// into active (never straight back into done).
func TestDoneRejectAndRevoke(t *testing.T) {
	root := ws(t)
	task := draftID(t, root, "task", "flaky check", "", "", "", nil)
	Move(root, task, item.StateDone, "")

	if _, err := Move(root, task, item.StateRejected, ""); err == nil {
		t.Fatal("noteless done->rejected accepted")
	}
	if it, err := Move(root, task, item.StateRejected, "flaked in CI"); err != nil || it.State != item.StateRejected {
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
	if _, err := Move(root, task, item.StateDone, ""); err == nil {
		t.Fatal("rejected->done accepted")
	}
	// revocation lands back in active, not done
	it, err := Move(root, task, item.StateActive, "")
	if err != nil || it.State != item.StateActive {
		t.Fatalf("rejected->active revocation: %+v, %v", it, err)
	}
}

// TestArchivedIsTerminal checks archived items reject every further move.
func TestArchivedIsTerminal(t *testing.T) {
	root := ws(t)
	task := draftID(t, root, "task", "terminal", "", "", "", nil)
	Move(root, task, item.StateArchived, "")

	for _, to := range []string{item.StateDraft, item.StateSubmitted, item.StateApproved, item.StateActive, item.StateDone, item.StateRejected} {
		if _, err := Move(root, task, to, "note"); err == nil {
			t.Fatalf("archived->%s accepted", to)
		}
	}
}

// TestDoneActiveReopen checks the one preserved backward hop still works
// alongside the new forward-skip rules.
func TestDoneActiveReopen(t *testing.T) {
	root := ws(t)
	task := draftID(t, root, "task", "reopen me", "", "", "", nil)
	Move(root, task, item.StateDone, "")
	it, err := Move(root, task, item.StateActive, "")
	if err != nil || it.State != item.StateActive {
		t.Fatalf("done->active reopen: %+v, %v", it, err)
	}
}

func TestRejectionSnapshotAndRevocation(t *testing.T) {
	root := ws(t)
	p := draftID(t, root, "proposal", "vram cache", "Keep kernels resident.", "", "", []string{"gpu/kern.cu"})
	if _, err := Move(root, p, item.StateRejected, "breaks multi-tenant scheduling"); err != nil {
		t.Fatal(err)
	}
	// gone from work.md
	if _, ok, _ := item.Get(root, p); ok {
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
	it, err := Move(root, p, item.StateDraft, "")
	if err != nil || it.State != item.StateDraft {
		t.Fatalf("revocation = %+v, %v", it, err)
	}
	got, ok, _ := item.Get(root, p)
	if !ok || got.Body != "Keep kernels resident." || len(got.Targets) != 1 {
		t.Fatalf("restored item incomplete: %+v", got)
	}
	// minting does not hand the restored item's ID to the next draft
	it2, _ := Draft(root, nil, "proposal", "next", "", "", "", nil)
	if it2.ID == p {
		t.Fatalf("minting reused a live ID: %s", it2.ID)
	}
}

// foldToCompactSurvivors rewrites a context dir's journal down to only the
// event kinds that survive compaction (see internal/mcpserver's compact
// tool: it folds create/move/rule/drift away and keeps only
// reject/archive/compact/escalate/decide). Used by the minting regression
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

// TestMintingSurvivesCompactAfterArchive is the descendant of the maxNum
// regression. The counter used to derive the next ID from the highest one it
// could still see, so when compact folded an archived item's create event
// away — leaving only the tombstone — the id looked unused and was minted
// again. That happened live twice here (ADR-0001..0004 and P-0067). Under
// ADR-0013 minting reads nothing at all, so no amount of history loss can
// resurrect an id; the test stays as the regression's tombstone, now checking
// the property directly rather than a counter's floor.
func TestMintingSurvivesCompactAfterArchive(t *testing.T) {
	root := ws(t)
	first := draftID(t, root, "task", "flaky check", "", "", "", nil)
	if _, err := Move(root, first, item.StateArchived, ""); err != nil {
		t.Fatal(err)
	}

	foldToCompactSurvivors(t, root, "")

	// only the archive tombstone remains for the first task — no create
	// event, no work.md row. The next draft must not land on its ID, and the
	// tombstone must stay reachable by that ID.
	second := draftID(t, root, "task", "second task", "", "", "", nil)
	if second == first {
		t.Fatalf("reused a folded-away archived id: %s", second)
	}
	tomb, ok, err := Tombstone(root, first)
	if err != nil || !ok || tomb.Title != "flaky check" {
		t.Fatalf("archived tombstone unreachable by ID %s: %+v ok=%v err=%v", first, tomb, ok, err)
	}
}

// TestMintingSurvivesCompactAfterReject mirrors the archive case for rejected
// items, whose reject events also survive compaction.
func TestMintingSurvivesCompactAfterReject(t *testing.T) {
	root := ws(t)
	first := draftID(t, root, "bug", "flaky", "", "", "", nil)
	if _, err := Move(root, first, item.StateRejected, "not reproducible"); err != nil {
		t.Fatal(err)
	}

	foldToCompactSurvivors(t, root, "")

	second := draftID(t, root, "bug", "second bug", "", "", "", nil)
	if second == first {
		t.Fatalf("reused a folded-away rejected id: %s", second)
	}
	// the rejected item is still revocable by its own ID
	if _, err := Move(root, first, item.StateDraft, ""); err != nil {
		t.Fatalf("rejected item not revocable by ID %s: %v", first, err)
	}
}

// TestMintingIgnoresSeededLegacyItem: a work.md seeded with a legacy
// sequential item (what every pre-migration workspace looks like) neither
// steers nor blocks minting. The legacy item stays loadable alongside the new
// one — the mixed state a workspace lives in until the migration runs.
func TestMintingIgnoresSeededLegacyItem(t *testing.T) {
	root := ws(t)
	if err := item.Upsert(root, item.Item{
		ID: "P-0005", Kind: "proposal", State: item.StateActive, Title: "manually seeded",
	}); err != nil {
		t.Fatal(err)
	}
	fresh := draftID(t, root, "proposal", "next", "", "", "", nil)
	if fresh == "P-0005" {
		t.Fatalf("minted over the seeded legacy item: %s", fresh)
	}
	all, err := item.LoadAll(root)
	if err != nil || len(all) != 2 {
		t.Fatalf("LoadAll = %+v, %v — legacy and new must coexist", all, err)
	}
	if _, ok, _ := item.Get(root, "P-0005"); !ok {
		t.Fatal("legacy item no longer resolvable after a new-scheme draft")
	}
	// and the legacy item still moves through the state machine
	if it, err := Move(root, "P-0005", item.StateDone, ""); err != nil || it.State != item.StateDone {
		t.Fatalf("legacy item not movable: %+v, %v", it, err)
	}
}

// TestMintingInEmptyWorkspace checks minting needs no prior state whatsoever —
// the property that lets two clones with no shared counter mint safely.
func TestMintingInEmptyWorkspace(t *testing.T) {
	root := ws(t)
	it, err := Draft(root, nil, "research", "first", "", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !item.IDRe.MatchString(it.ID) || !strings.HasPrefix(it.ID, "R-") {
		t.Fatalf("first draft in an empty workspace minted %q", it.ID)
	}

	// the cross-clone case R-0006 reproduced: a second, independent workspace
	// that has never seen the first must not mint the same ID.
	other := ws(t)
	it2, err := Draft(other, nil, "research", "first", "", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if it2.ID == it.ID {
		t.Fatalf("two independent workspaces minted the same ID %q — R-0006 all over again", it.ID)
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
	task := draftID(t, root, "task", "flaky feedback loop", "", "", "", nil)
	if _, err := Move(root, task, item.StateDone, ""); err != nil {
		t.Fatal(err)
	}

	// reopen 1: succeeds, Rounds becomes 1
	it, err := Move(root, task, item.StateActive, "")
	if err != nil || it.State != item.StateActive || it.Rounds != 1 {
		t.Fatalf("reopen 1 = %+v, %v", it, err)
	}
	if _, err := Move(root, task, item.StateDone, ""); err != nil {
		t.Fatal(err)
	}

	// reopen 2 hits MaxRounds(2): Move refuses with ErrRoundsExhausted and
	// leaves the item on done with the counter persisted at 2.
	_, err = Move(root, task, item.StateActive, "")
	var exhausted ErrRoundsExhausted
	if !errors.As(err, &exhausted) {
		t.Fatalf("expected ErrRoundsExhausted, got %v", err)
	}
	if exhausted.Item.Rounds != 2 {
		t.Fatalf("exhausted item rounds = %d, want 2", exhausted.Item.Rounds)
	}
	stuck, ok, _ := item.Get(root, task)
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
	// Escalate mints through the same path as Draft: a record ID, ADR-prefixed
	if decision.Kind != "adr" || decision.State != item.StateDraft ||
		!item.IDRe.MatchString(decision.ID) || !strings.HasPrefix(decision.ID, "ADR-") {
		t.Fatalf("decision item = %+v", decision)
	}
	if len(escalated.Needs) != 1 || escalated.Needs[0] != decision.ID {
		t.Fatalf("Needs not linked: %+v", escalated.Needs)
	}
	if !strings.Contains(decision.Title, "rescope") || !strings.Contains(decision.Title, "reject") ||
		!strings.Contains(decision.Title, "override-once") {
		t.Fatalf("decision title missing options: %q", decision.Title)
	}
	if decision.Parent != task {
		t.Fatalf("decision parent = %q, want %q", decision.Parent, task)
	}

	// persisted: blocked item shows up as blocked on reload
	blocked, ok, _ := item.Get(root, task)
	if !ok || blocked.State != item.StateBlocked {
		t.Fatalf("blocked item not persisted: %+v", blocked)
	}
	// journal carries the escalate event
	events, _ := journal.ReadAll(root)
	found := false
	for _, e := range events {
		if e.Ev == journal.EvEscalate && e.ID == task {
			found = true
			if len(e.Nd) != 1 || e.Nd[0] != decision.ID {
				t.Fatalf("escalate event Nd = %+v", e.Nd)
			}
		}
	}
	if !found {
		t.Fatal("no escalate journal event")
	}

	// blocked items refuse every move, naming the linked decision
	if _, err := Move(root, task, item.StateActive, ""); err == nil ||
		!strings.Contains(err.Error(), "blocked — resolve via decide "+decision.ID) {
		t.Fatalf("blocked item movable or wrong message: %v", err)
	}
	if _, err := Move(root, task, item.StateDraft, ""); err == nil ||
		!strings.Contains(err.Error(), "blocked") {
		t.Fatalf("blocked item movable to draft: %v", err)
	}
}

// TestResolveBlockedOutcomes exercises all three ResolveBlocked outcomes and
// checks override-once really is one-shot.
func TestResolveBlockedOutcomes(t *testing.T) {
	setupBlocked := func(t *testing.T) (workspace.Root, string) {
		t.Helper()
		root := ws(t)
		root.Cfg.Feedback.MaxRounds = 1
		task := draftID(t, root, "task", "needs a decision", "", "", "", nil)
		Move(root, task, item.StateDone, "")
		_, err := Move(root, task, item.StateActive, "")
		var exhausted ErrRoundsExhausted
		if !errors.As(err, &exhausted) {
			t.Fatalf("setup: expected exhaustion, got %v", err)
		}
		if _, _, err := Escalate(root, nil, exhausted.Item); err != nil {
			t.Fatalf("setup: escalate: %v", err)
		}
		return root, task
	}

	t.Run("rescope", func(t *testing.T) {
		root, task := setupBlocked(t)
		resc, err := ResolveBlocked(root, task, "rescope", "narrower scope")
		if err != nil || resc.State != item.StateDraft || resc.Rounds != 0 {
			t.Fatalf("rescope = %+v, %v", resc, err)
		}
		got, ok, _ := item.Get(root, task)
		if !ok || got.State != item.StateDraft {
			t.Fatalf("rescope not persisted: %+v", got)
		}
	})

	t.Run("reject", func(t *testing.T) {
		root, task := setupBlocked(t)
		if _, err := ResolveBlocked(root, task, "reject", ""); err == nil {
			t.Fatal("noteless blocked-reject accepted")
		}
		rej, err := ResolveBlocked(root, task, "reject", "not worth it")
		if err != nil || rej.State != item.StateRejected {
			t.Fatalf("reject = %+v, %v", rej, err)
		}
		if _, ok, _ := item.Get(root, task); ok {
			t.Fatal("rejected-via-decision item still in work.md")
		}
		events, _ := journal.ReadAll(root)
		var found bool
		for _, e := range events {
			if e.Ev == journal.EvReject && e.ID == task {
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
		root, task := setupBlocked(t)
		ov, err := ResolveBlocked(root, task, "override-once", "")
		if err != nil || ov.State != item.StateActive || ov.Rounds != 0 || !ov.Override {
			t.Fatalf("override-once = %+v, %v", ov, err)
		}
		// exhaust again — override already spent this time
		Move(root, task, item.StateDone, "")
		_, err = Move(root, task, item.StateActive, "")
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
		if _, err := ResolveBlocked(root, task, "override-once", ""); err == nil {
			t.Fatal("second override-once accepted")
		}
	})
}

// TestRejectSnapshotKeepsFeedbackFields checks the reject journal snapshot
// (taken via Move, not ResolveBlocked) carries Rounds/Grilled/Needs/Override
// through a reject -> revoke roundtrip. Needs deliberately holds a LEGACY
// decision ID: a blocking link minted before ADR-0013 must survive the
// roundtrip untouched.
func TestRejectSnapshotKeepsFeedbackFields(t *testing.T) {
	root := ws(t)
	task := draftID(t, root, "task", "snapshot", "", "", "", nil)
	it, _, _ := item.Get(root, task)
	it.Rounds = 2
	it.Grilled = "improve coverage"
	it.Needs = []string{"D-0007"}
	it.Override = true
	if err := item.Upsert(root, it); err != nil {
		t.Fatal(err)
	}
	if _, err := Move(root, task, item.StateRejected, "not needed anymore"); err != nil {
		t.Fatal(err)
	}
	revoked, err := Move(root, task, item.StateDraft, "")
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
	p := draftID(t, root, "proposal", "vram cache", "Keep kernels resident.", "gpu", "", nil)
	if _, err := Move(root, p, item.StateArchived, "shipped"); err != nil {
		t.Fatal(err)
	}

	tomb, ok, err := Tombstone(root, p)
	if err != nil || !ok {
		t.Fatalf("Tombstone(%s) = %+v, %v, %v", p, tomb, ok, err)
	}
	if tomb.State != item.StateArchived || tomb.Kind != "proposal" || tomb.Title != "vram cache" {
		t.Fatalf("tombstone incomplete: %+v", tomb)
	}

	if _, ok, err := Tombstone(root, "P-9999"); err != nil || ok {
		t.Fatalf("Tombstone(unknown) = ok=%v, err=%v, want ok=false", ok, err)
	}
}

// TestTombstoneResolvesLegacyIDForever is the load-bearing legacy guarantee.
// An archived record leaves work.md and survives ONLY as a journal tombstone
// that Tombstone finds by exact ID. Every archived record this repository
// already has carries a legacy sequential ID, so if that shape ever stopped
// resolving, the archived half of the history would become unreachable — not
// after a deprecation window, immediately and permanently.
func TestTombstoneResolvesLegacyIDForever(t *testing.T) {
	root := ws(t)
	// a workspace as it existed before ADR-0013: a legacy item, archived
	if err := item.Upsert(root, item.Item{
		ID: "P-0042", Kind: "proposal", State: item.StateActive,
		Title: "legacy proposal", Dir: "gpu", Body: "Old body.",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := Move(root, "P-0042", item.StateArchived, "shipped long ago"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := item.Get(root, "P-0042"); ok {
		t.Fatal("archived item still in work.md — the premise of this test is gone")
	}

	// drafting under the new scheme must not disturb the tombstone
	if _, err := Draft(root, nil, "task", "modern work", "", "", "", nil); err != nil {
		t.Fatal(err)
	}

	tomb, ok, err := Tombstone(root, "P-0042")
	if err != nil || !ok || tomb.Title != "legacy proposal" || tomb.State != item.StateArchived {
		t.Fatalf("legacy tombstone unreachable: %+v ok=%v err=%v", tomb, ok, err)
	}
	// and a legacy ID is still valid provenance for new work
	child, err := Draft(root, nil, "task", "follow-up", "", "", "P-0042", nil)
	if err != nil || child.Parent != "P-0042" {
		t.Fatalf("legacy archived parent rejected: %+v, %v", child, err)
	}
}

// TestDraftArchivedParent checks Draft accepts an archived (Tombstone-only)
// parent as provenance while still rejecting a parent that resolves nowhere.
func TestDraftArchivedParent(t *testing.T) {
	root := ws(t)
	p := draftID(t, root, "proposal", "vram cache", "", "gpu", "", nil)
	if _, err := Move(root, p, item.StateArchived, "shipped"); err != nil {
		t.Fatal(err)
	}

	it, err := Draft(root, nil, "task", "follow-up", "", "", p, nil)
	if err != nil || it.Parent != p {
		t.Fatalf("Draft with archived parent = %+v, %v", it, err)
	}
	if _, err := Draft(root, nil, "task", "orphan", "", "", "P-9999", nil); err == nil {
		t.Fatal("Draft with unknown parent accepted")
	}
}

// TestDraftPersistsRefs checks Draft threads refs through to the persisted
// item in its one Upsert (no draft-then-patch second write): the item
// returned by Draft and the item reloaded from work.md both carry the refs,
// and omitting refs entirely (the pre-T-0117 call shape, still used by every
// other caller) leaves the field empty exactly as before.
func TestDraftPersistsRefs(t *testing.T) {
	root := ws(t)
	it, err := Draft(root, nil, "proposal", "cached prefetch", "", "", "", nil, "R-0001", "ADR-0001")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"R-0001", "ADR-0001"}; !reflect.DeepEqual(it.Refs, want) {
		t.Fatalf("Draft return Refs = %v, want %v", it.Refs, want)
	}
	reloaded, ok, err := item.Get(root, it.ID)
	if err != nil || !ok {
		t.Fatalf("item.Get(%s) = ok=%v, err=%v", it.ID, ok, err)
	}
	if want := []string{"R-0001", "ADR-0001"}; !reflect.DeepEqual(reloaded.Refs, want) {
		t.Fatalf("reloaded Refs = %v, want %v", reloaded.Refs, want)
	}

	it2, err := Draft(root, nil, "task", "no refs", "", "", "", nil)
	if err != nil || len(it2.Refs) != 0 {
		t.Fatalf("Draft without refs = %+v, %v", it2, err)
	}
}

func TestArchiveMergesIntentAndFoldsChildren(t *testing.T) {
	root := ws(t)
	p := draftID(t, root, "proposal", "strided access", "Delta text.", "gpu", "", nil)
	task := draftID(t, root, "task", "kernel change", "", "gpu", p, nil)

	Move(root, p, item.StateSubmitted, "")
	Move(root, p, item.StateApproved, "")
	Move(root, p, item.StateActive, "")
	Move(root, task, item.StateActive, "")

	// open child blocks the parent's done->archived path
	Move(root, p, item.StateDone, "")
	if _, err := Move(root, p, item.StateArchived, ""); err == nil || !strings.Contains(err.Error(), task) {
		t.Fatalf("open child not enforced: %v", err)
	}
	Move(root, task, item.StateDone, "")
	if _, err := Move(root, p, item.StateArchived, "shipped"); err != nil {
		t.Fatal(err)
	}
	// delta merged into the gpu spec's intent section
	raw, _ := os.ReadFile(root.SpecPath("gpu"))
	if !strings.Contains(string(raw), "## intent") || !strings.Contains(string(raw), p+" strided access: shipped") {
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

// Round 2 (cross-val-research finding 1): the retention cap never cuts
// mid-rune — a multi-byte character straddling the boundary must not leave
// a dangling lead byte in the journal.
func TestCapRetainedBodyRuneBoundary(t *testing.T) {
	pad := strings.Repeat("a", retainedBodyMax-1)
	in := pad + "—tail" // the em dash straddles the cap
	got := capRetainedBody(in)
	if !utf8.ValidString(got) {
		t.Fatalf("truncation produced invalid UTF-8 near the boundary: %q", got[len(got)-60:])
	}
	if !strings.HasSuffix(got, "[body truncated at tombstone retention cap]") {
		t.Fatalf("truncation marker missing: %q", got[len(got)-60:])
	}
	if short := "small body"; capRetainedBody(short) != short {
		t.Fatal("under-cap bodies must pass through untouched")
	}
}

// TestLongADRBodyKeepsItsDecision: capRetainedBody truncates from the end,
// so appending an ADR's structured fields after its body meant a body over
// the retention cap silently amputated `decision:` — the one line naming
// the winner, on the record whose entire purpose is to name it. The fields
// are budgeted out of the cap instead, making the body the only thing a cap
// can take.
func TestLongADRBodyKeepsItsDecision(t *testing.T) {
	it := item.Item{
		Kind:     "adr",
		Title:    "which serialization should the rpc layer use?",
		Body:     strings.Repeat("kind: radio\noption: repo-a: protobuf\noption: repo-b: json\n", 900),
		Decision: "repo-b: json",
		Status:   "accepted",
	}
	got := retainedBody(it)
	if !strings.Contains(got, "decision: repo-b: json") {
		t.Fatalf("a long body must not truncate away the winner (len=%d)", len(got))
	}
	if !strings.Contains(got, "status: accepted") {
		t.Fatalf("nor the status (len=%d)", len(got))
	}
	if len(got) > retainedBodyMax+len(truncationMarker) {
		t.Fatalf("the retention cap must still hold: len=%d > %d", len(got), retainedBodyMax+len(truncationMarker))
	}
	if !strings.Contains(got, "truncated at tombstone retention cap") {
		t.Fatalf("the body should be the part that was truncated:\n%s", got[:200])
	}
	// the un-truncated case is untouched: no cap marker, both halves whole
	short := item.Item{Kind: "adr", Title: "q", Body: "kind: radio\noption: a\noption: b", Decision: "b", Status: "accepted"}
	sg := retainedBody(short)
	if strings.Contains(sg, "truncated") || !strings.Contains(sg, "option: a") || !strings.Contains(sg, "decision: b") {
		t.Fatalf("a short ADR must be retained whole: %q", sg)
	}
}

// TestOversizedFieldKeepsTheOutcome is the regression for the defect the
// first budgeting fix missed by varying only the body: the four structured
// fields were joined in fixed order and capped from the END, so a large
// `context` — which nothing bounds — amputated `decision:` and `status:`
// exactly as before. Space is budgeted by importance now, not by position.
func TestOversizedFieldKeepsTheOutcome(t *testing.T) {
	for _, tc := range []struct {
		name string
		it   item.Item
	}{
		{"huge context", item.Item{
			Kind: "adr", Title: "q", Body: "kind: radio",
			Context: strings.Repeat("C", 50000), Decision: "protobuf", Status: "accepted",
		}},
		{"huge consequences", item.Item{
			Kind: "adr", Title: "q", Body: "kind: radio",
			Consequences: strings.Repeat("K", 50000), Decision: "protobuf", Status: "accepted",
		}},
		{"huge body and context together", item.Item{
			Kind: "adr", Title: "q", Body: strings.Repeat("B", 20000),
			Context: strings.Repeat("C", 20000), Decision: "protobuf", Status: "accepted",
		}},
		{"huge decision itself", item.Item{
			Kind: "adr", Title: "q", Body: "kind: radio",
			Decision: strings.Repeat("D", 9000), Status: "accepted",
		}},
	} {
		got := retainedBody(tc.it)
		if !strings.Contains(got, "decision: ") {
			t.Fatalf("%s: the decision must survive (len=%d)", tc.name, len(got))
		}
		if !strings.Contains(got, "status: accepted") {
			t.Fatalf("%s: the status must survive (len=%d)", tc.name, len(got))
		}
		if len(got) > retainedBodyMax+4*len(truncationMarker) {
			t.Fatalf("%s: cap blown: len=%d", tc.name, len(got))
		}
		if !utf8.ValidString(got) {
			t.Fatalf("%s: retained body is not valid UTF-8", tc.name)
		}
	}
}

// TestUnansweredGistNamesNoSide: the first `option:` line is a candidate
// nobody chose. Quoting it as an item's gist asserted an outcome the record
// does not have — the permanent spec.md trace read as a decision FOR that
// option, with no trace the others existed.
func TestUnansweredGistNamesNoSide(t *testing.T) {
	unanswered := item.Item{
		Kind: "adr", Title: "which serialization?",
		Body: "kind: radio\nknowledge-conflict: abcd\noption: repo-a: protobuf\noption: repo-b: json",
	}
	got := gistLine(unanswered)
	for _, side := range []string{"protobuf", "json"} {
		if strings.Contains(got, side) {
			t.Fatalf("an unanswered decision must name no side, got %q", got)
		}
	}
	if !strings.Contains(got, "undecided") || !strings.Contains(got, "2 options") {
		t.Fatalf("it should say what is actually true, got %q", got)
	}
	// answered: the decision itself, not the scaffolding
	answered := unanswered
	answered.Decision = "repo-a: protobuf"
	if got := gistLine(answered); got != "repo-a: protobuf" {
		t.Fatalf("an answered decision gists as its decision, got %q", got)
	}
	// non-adr kinds are untouched, including a body whose first line looks
	// like scaffolding
	task := item.Item{Kind: "task", Body: "status: this is prose, not a field\nmore"}
	if got := gistLine(task); got != "status: this is prose, not a field" {
		t.Fatalf("non-adr kinds keep firstLine(Body), got %q", got)
	}
}

// TestEmptyFieldDoesNotStarveLaterFields: the budgeting closure zeroed the
// shared budget when a field was EMPTY, conflating "nothing to write" with
// "no room left" — so an omitted context (an optional field, the ordinary
// case) deleted `consequences` outright, with no oversized input anywhere.
// A regression introduced BY the cap fix and worse than the defect it
// closed, because it needed nothing unusual to trigger.
func TestEmptyFieldDoesNotStarveLaterFields(t *testing.T) {
	for _, tc := range []struct {
		name string
		it   item.Item
		want []string
	}{
		{"no context", item.Item{
			Kind: "adr", Title: "q", Body: "kind: radio\noption: postgres\noption: sqlite",
			Decision: "postgres", Consequences: "needs a separate server in dev", Status: "accepted",
		}, []string{"option: postgres", "decision: postgres", "consequences: needs a separate server in dev", "status: accepted"}},
		{"no body and no context", item.Item{
			Kind: "adr", Title: "q",
			Context: "", Decision: "postgres", Consequences: "minor tradeoff", Status: "accepted",
		}, []string{"decision: postgres", "consequences: minor tradeoff", "status: accepted"}},
		{"no body, context present", item.Item{
			Kind: "adr", Title: "q",
			Context: "hello-context", Decision: "postgres", Consequences: "minor tradeoff", Status: "accepted",
		}, []string{"context: hello-context", "decision: postgres", "consequences: minor tradeoff", "status: accepted"}},
		{"only a decision", item.Item{Kind: "adr", Title: "q", Decision: "postgres"},
			[]string{"decision: postgres"}},
	} {
		got := retainedBody(tc.it)
		for _, want := range tc.want {
			if !strings.Contains(got, want) {
				t.Fatalf("%s: %q missing from retained body:\n%s", tc.name, want, got)
			}
		}
	}
}

// TestOutcomeFieldCapIsDeliberate pins the one place adrOutcome accepts a
// bounded loss, so it stays a decision on the record rather than drifting
// into an accident. decision and status are capped at outcomeFieldMax each
// — that cap is what makes the must-keep reservation computable, and
// therefore what guarantees neither field can be dropped entirely. A
// >2KB status is not a status (the field is the four-value ADR enum), and
// a >2KB decision is prose that belongs in context; both truncate visibly,
// with the marker, rather than vanishing.
func TestOutcomeFieldCapIsDeliberate(t *testing.T) {
	got := retainedBody(item.Item{
		Kind: "adr", Title: "q",
		Decision: strings.Repeat("d", outcomeFieldMax*2),
		Status:   strings.Repeat("s", outcomeFieldMax*2),
	})
	for _, want := range []string{"decision: ", "status: ", truncationMarker} {
		if !strings.Contains(got, want) {
			t.Fatalf("an oversized outcome field truncates visibly and stays present; %q missing", want)
		}
	}
	// every realistic status survives whole — the enum this field actually is
	for _, st := range []string{"proposed", "accepted", "superseded", "deprecated"} {
		out := retainedBody(item.Item{Kind: "adr", Title: "q", Decision: "x", Status: st})
		if !strings.Contains(out, "status: "+st) || strings.Contains(out, truncationMarker) {
			t.Fatalf("status %q must be retained whole and unmarked: %q", st, out)
		}
	}
}
