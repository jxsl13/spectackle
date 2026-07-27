package mcpserver

// Verdict compaction survival (T-01KYFXEQ, ADR-01KYES0TT): a journal fold
// keeps EvReview/EvValidate; live items' verdicts stay byte-complete,
// terminal items' verdicts shed Keys/Wv but keep identity/hash/pass;
// reviewState still resolves a retained verdict after the fold; archived
// items' packs suppress the computed sections.

import (
	"strings"
	"testing"

	"github.com/jxsl13/spectackle/internal/item"
	"github.com/jxsl13/spectackle/internal/journal"
	"github.com/jxsl13/spectackle/internal/lifecycle"
)

func TestVerdictsSurviveFold(t *testing.T) {
	root := t.TempDir()
	srv, sess := connectRootWithServer(t, root)

	// live item with grill render + verdict
	live, err := lifecycle.Draft(srv.ws, srv.minter(), "task", "live verdict item", ambFixturePad, "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.Append(srv.ws, "", journal.Event{
		Ev: journal.EvGrill, ID: live.ID, Hash: "hh", Open: 2, Keys: []string{"a:x", "b:y"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := journal.Append(srv.ws, "", journal.Event{
		Ev: journal.EvReview, ID: live.ID, Hash: "hh", Pass: true,
		Wv: []string{"a:x waived for reasons"}, Ln: []string{"refute"},
	}); err != nil {
		t.Fatal(err)
	}
	// terminal item with a validate verdict
	dead, err := lifecycle.Draft(srv.ws, srv.minter(), "task", "terminal verdict item", ambFixturePad, "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.Append(srv.ws, "", journal.Event{
		Ev: journal.EvValidate, Op: "verdict", ID: dead.ID, Hash: "dh", Pass: true,
		Keys: []string{"v:z"}, Wv: []string{"v:z gone"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := journal.Append(srv.ws, "", journal.Event{
		Ev: journal.EvReject, ID: dead.ID, Note: "terminal fixture",
	}); err != nil {
		t.Fatal(err)
	}

	srv.ws.Cfg.Compact.JournalMax = 1
	out := callText(t, sess, "compact", map[string]any{"apply": true})
	if !strings.Contains(out, "ok folded") {
		t.Fatalf("fold did not run: %q", out)
	}

	events, err := journal.Read(srv.ws, "")
	if err != nil {
		t.Fatal(err)
	}
	var liveV, deadV *journal.Event
	for i := range events {
		e := &events[i]
		if e.Ev == journal.EvReview && e.ID == live.ID {
			liveV = e
		}
		if e.Ev == journal.EvValidate && e.ID == dead.ID {
			deadV = e
		}
	}
	if liveV == nil || deadV == nil {
		t.Fatalf("verdicts dropped by the fold: live=%v dead=%v", liveV, deadV)
	}
	if len(liveV.Wv) != 1 || len(liveV.Ln) != 1 || !liveV.Pass || liveV.Hash != "hh" {
		t.Fatalf("live verdict must stay byte-complete: %+v", liveV)
	}
	if deadV.Keys != nil || deadV.Wv != nil {
		t.Fatalf("terminal verdict must shed Keys/Wv: %+v", deadV)
	}
	if !deadV.Pass || deadV.Hash != "dh" || deadV.Op != "verdict" {
		t.Fatalf("terminal verdict identity must survive: %+v", deadV)
	}

	// reviewState resolves the retained verdict post-fold
	_, _, _, _, rev, err := srv.reviewState(live.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rev == nil || !rev.Pass {
		t.Fatalf("reviewState lost the retained verdict: %+v", rev)
	}
}

func TestArchivedPackSuppressed(t *testing.T) {
	root := t.TempDir()
	srv, sess := connectRootWithServer(t, root)
	it, err := lifecycle.Draft(srv.ws, srv.minter(), "task", "archived pack fixture", ambFixturePad, "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	// The REAL archive path (B-01KYHQ8TQ6): lifecycle removes the work.md
	// block, so item.Get must read !ok — the old fixture Upserted an item
	// with State archived still IN work.md, a state no transition
	// produces, and exercised only a dead branch while the live path fell
	// through to nearest()'s bare nf journal refs.
	if _, err := lifecycle.Move(srv.ws, it.ID, item.StateArchived, "fixture closure"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := item.Get(srv.ws, it.ID); ok {
		t.Fatal("fixture broken: archived item still in work.md")
	}
	srv.markDirty()
	out := callText(t, sess, "validate", map[string]any{"id": it.ID})
	if strings.HasPrefix(out, "nf ") || strings.Contains(out, "j:") {
		t.Fatalf("archived pack fell through to nearest-match refs: %q", out)
	}
	if !strings.Contains(out, "computed: suppressed (archived)") {
		t.Fatalf("archived validate pack must suppress honestly: %q", out)
	}
	if strings.Contains(out, "v offscope") || strings.Contains(out, "v untested") {
		t.Fatalf("archived validate pack must carry no v classes: %q", out)
	}
	// op=verdict on the tombstone refuses honestly instead of nf noise
	out = callText(t, sess, "validate", map[string]any{
		"id": it.ID, "op": "verdict", "pass": true, "agent": "post-hoc"})
	if !strings.Contains(out, "archived; verdicts bind to live items") {
		t.Fatalf("verdict on a tombstone must refuse honestly: %q", out)
	}
	// grill carried the identical bare-nf bug (round-2 validator finding) —
	// its pack suppresses and its verdict refuses through the same helper
	out = callText(t, sess, "grill", map[string]any{"id": it.ID})
	if strings.HasPrefix(out, "nf ") || !strings.Contains(out, "computed: suppressed (archived)") {
		t.Fatalf("archived grill pack must suppress, not nf: %q", out)
	}
	out = callText(t, sess, "grill", map[string]any{
		"id": it.ID, "op": "verdict", "pass": true, "agent": "post-hoc"})
	if !strings.Contains(out, "archived; review verdicts bind to live items") {
		t.Fatalf("grill verdict on a tombstone must refuse honestly: %q", out)
	}
}

// The union-root arm: a worktree-homed server must resolve a tombstone
// that lives in MAIN's journal — every sibling lookup path already does
// (the get tool's established fallback); without it the serving-worktree
// topology reproduced the bare-nf bug verbatim.
func TestArchivedPackSuppressedAcrossUnionRoot(t *testing.T) {
	mainRoot, wtDir := servedWorktree(t, "")
	mainSess := connectRoot(t, mainRoot)
	id := draftID(t, mainSess, map[string]any{
		"kind": "task", "title": "archived on main, judged from the worktree", "body": ambFixturePad})
	callText(t, mainSess, "move", map[string]any{"id": id, "to": "archived", "note": "closed on main"})

	wtSess := connectRoot(t, wtDir)
	out := callText(t, wtSess, "validate", map[string]any{"id": id})
	if strings.HasPrefix(out, "nf ") || strings.Contains(out, "j:") {
		t.Fatalf("worktree-homed pack fell to nearest for a main tombstone: %q", out)
	}
	if !strings.Contains(out, "computed: suppressed (archived)") {
		t.Fatalf("main-root tombstone must render suppressed: %q", out)
	}
}

// Issue 178 defect 3: a research tombstone RETAINS the finding — the body
// is the artifact (claim, source, confidence); archiving a task still
// compacts to a summary tombstone.
func TestResearchTombstoneRetainsBody(t *testing.T) {
	root := t.TempDir()
	srv, sess := connectRootWithServer(t, root)
	body := "BPE encode measured 28.2 MB/s.\n\nSource: https://example.org/paper-XYZ-2026\nConfidence: high" + "\n\n" + ambFixturePad
	r, err := lifecycle.Draft(srv.ws, srv.minter(), "research", "tokenizer throughput finding", body, "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	// consume it so the research return-path gate passes
	callText(t, sess, "rule", map[string]any{
		"op": "add", "dir": "", "stem": "TOKENIZER", "pattern": "U",
		"system": "tokenizer", "response": "sustain 28.2 MB/s per " + r.ID,
	})
	if _, err := lifecycle.Move(srv.ws, r.ID, item.StateArchived, "completed in a previous era"); err != nil {
		t.Fatal(err)
	}
	srv.markDirty()

	got := callText(t, sess, "get", map[string]any{"id": r.ID})
	if !strings.Contains(got, "paper-XYZ-2026") {
		t.Fatalf("the tombstone must retain the citation: %q", got)
	}
	hist := callText(t, sess, "find", map[string]any{"q": "paper-XYZ-2026", "scope": "history"})
	if !strings.Contains(hist, r.ID[:8]) && !strings.Contains(hist, "paper-XYZ-2026") {
		t.Fatalf("the citation must be searchable in history: %q", hist)
	}

	// the fold keeps it: EvArchive events survive compaction verbatim
	out := callText(t, sess, "compact", map[string]any{"apply": true})
	_ = out
	srv.markDirty()
	got = callText(t, sess, "get", map[string]any{"id": r.ID})
	if !strings.Contains(got, "paper-XYZ-2026") {
		t.Fatalf("the retained body must survive a fold: %q", got)
	}

	// a TASK tombstone stays a compact summary — no body retention
	task, err := lifecycle.Draft(srv.ws, srv.minter(), "task", "ordinary task", "task body with a marker NOTRETAINED plus filler. "+ambFixturePad, "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lifecycle.Move(srv.ws, task.ID, item.StateArchived, "done"); err != nil {
		t.Fatal(err)
	}
	srv.markDirty()
	got = callText(t, sess, "get", map[string]any{"id": task.ID})
	if strings.Contains(got, "NOTRETAINED") {
		t.Fatalf("task tombstones must stay compact summaries: %q", got)
	}
}

// Round 2 (cross-val-research finding 5): a research CHILD folded into its
// parent's archive keeps its finding — the second archive path in
// archive() lost the citation exactly like the first used to.
func TestResearchChildFoldedWithParentRetainsBody(t *testing.T) {
	root := t.TempDir()
	srv, sess := connectRootWithServer(t, root)
	parent, err := lifecycle.Draft(srv.ws, srv.minter(), "task", "parent closure", ambFixturePad, "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	child, err := lifecycle.Draft(srv.ws, srv.minter(), "research", "child finding",
		"Latency knee at 4k batch.\n\nSource: https://example.org/paper-CHILD-9999\nConfidence: medium\n\n"+ambFixturePad,
		"", parent.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	callText(t, sess, "rule", map[string]any{
		"op": "add", "dir": "", "stem": "CHILDLAT", "pattern": "U",
		"system": "batcher", "response": "cap batches at 4096 per " + child.ID,
	})
	if _, err := lifecycle.Move(srv.ws, child.ID, item.StateDone, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := lifecycle.Move(srv.ws, parent.ID, item.StateArchived, "parent closed"); err != nil {
		t.Fatal(err)
	}
	srv.markDirty()
	got := callText(t, sess, "get", map[string]any{"id": child.ID})
	if !strings.Contains(got, "paper-CHILD-9999") {
		t.Fatalf("the folded research child must keep its citation: %q", got)
	}
}

// Round 2 (finding 2): the worktree-homed union fallback renders the
// retained body too — a research item archived on MAIN must not lose its
// citation when read from a serving worktree.
func TestResearchTombstoneBodyAcrossUnionRoot(t *testing.T) {
	mainRoot, wtDir := servedWorktree(t, "")
	mainSess := connectRoot(t, mainRoot)
	rid := draftID(t, mainSess, map[string]any{
		"kind": "research", "title": "main-root finding",
		"body": "Cache hit rate 97 percent.\n\nSource: https://example.org/paper-UNION-1\nConfidence: high\n\n" + ambFixturePad})
	fullRID := fullIDOf(t, mainRoot, rid)
	callText(t, mainSess, "rule", map[string]any{
		"op": "add", "dir": "", "stem": "CACHEHIT", "pattern": "U",
		"system": "cache", "response": "sustain 97 percent hits per " + fullRID,
	})
	callText(t, mainSess, "move", map[string]any{"id": rid, "to": "archived", "note": "consumed"})

	wtSess := connectRoot(t, wtDir)
	out := callText(t, wtSess, "get", map[string]any{"id": rid})
	if !strings.Contains(out, "paper-UNION-1") {
		t.Fatalf("union-root get must render the retained finding: %q", out)
	}
}
