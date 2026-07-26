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
	it.State = item.StateArchived
	if err := item.Upsert(srv.ws, it); err != nil {
		t.Fatal(err)
	}
	out := callText(t, sess, "grill", map[string]any{"id": it.ID})
	if !strings.Contains(out, "computed: suppressed (archived)") {
		t.Fatalf("archived grill pack must suppress: %q", out)
	}
	if strings.Contains(out, "#computed") || strings.Contains(out, "g amb-") {
		t.Fatalf("archived pack must carry no findings: %q", out)
	}
	out = callText(t, sess, "validate", map[string]any{"id": it.ID})
	if !strings.Contains(out, "computed: suppressed (archived)") {
		t.Fatalf("archived validate pack must suppress: %q", out)
	}
	if strings.Contains(out, "v offscope") || strings.Contains(out, "v untested") {
		t.Fatalf("archived validate pack must carry no v classes: %q", out)
	}
}
