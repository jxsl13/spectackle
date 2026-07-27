package mcpserver

// Offline gate strictness (B-01KYHV740T): a red-gated done edge journals a
// machine-readable "gate fail" round, the archive re-run refuses on it —
// including the SECOND attempt after a compensation — and the evidence
// survives journal compaction for live items.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jxsl13/spectackle/internal/item"
	"github.com/jxsl13/spectackle/internal/journal"
)

// gateFixture preps an offline root whose verify gate is red until the
// caller creates ok.txt.
func gateFixture(t *testing.T, journalMax int) (root string) {
	t.Helper()
	root = gitRoot(t)
	cfg := "schema: v1\nverify:\n  - \"test -f ok.txt\"\ngit:\n  mode: offline\n"
	if journalMax > 0 {
		cfg += "compact:\n  journal_max: 1\n"
	}
	if err := os.WriteFile(filepath.Join(root, ".spectackle", "config.yaml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// T1+T2: red-gate done journals the marker; archive refuses and stays
// done; the second attempt STILL refuses (the compensation event must not
// read as a passing done edge); green retry archives.
func TestOfflineRedGateBlocksArchiveUntilGreen(t *testing.T) {
	root := gateFixture(t, 0)
	sess := connectRoot(t, root)
	id := draftID(t, sess, map[string]any{
		"kind": "task", "title": "red gate cannot archive", "body": ambFixturePad})
	callText(t, sess, "move", map[string]any{"id": id, "to": "active"})
	out := callText(t, sess, "move", map[string]any{"id": id, "to": "done"})
	if !strings.Contains(out, "! GATE E") {
		t.Fatalf("red gate must be said at done: %q", out)
	}
	for attempt := 1; attempt <= 2; attempt++ {
		out = callText(t, sess, "move", map[string]any{"id": id, "to": "archived", "note": "must refuse"})
		if !strings.Contains(out, "GATE E") && !strings.Contains(out, "refused whole") {
			t.Fatalf("attempt %d: red-gated item must not archive:\n%s", attempt, out)
		}
		got := callText(t, sess, "get", map[string]any{"id": id})
		if !strings.Contains(got, " done ") {
			t.Fatalf("attempt %d: item must stay done: %q", attempt, got)
		}
	}
	// green retry completes
	if err := os.WriteFile(filepath.Join(root, "ok.txt"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	out = callText(t, sess, "move", map[string]any{"id": id, "to": "archived", "note": "green now"})
	if strings.Contains(out, "GATE E") || strings.Contains(out, "refused whole") {
		t.Fatalf("green retry must archive:\n%s", out)
	}
}

// T3: lastGateResult sequencing — pass, fail, pass again; the archived→done
// compensation shape is ignored.
func TestLastGateResultSequencing(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".spectackle"), 0o755); err != nil {
		t.Fatal(err)
	}
	s, _ := connectRootWithServer(t, root)
	const id = "T-01GATESEQ000000000000000000"
	ts := time.Now()
	app := func(fr, to, note string) {
		if err := journal.Append(s.ws, "", journal.Event{
			Ev: journal.EvMove, ID: id, Fr: fr, To: to, Note: note, T: ts,
		}); err != nil {
			t.Fatal(err)
		}
	}
	app(item.StateActive, item.StateDone, "")
	if got := s.lastGateResult(id); !strings.Contains(got, "last=pass") {
		t.Fatalf("done edge must read pass: %q", got)
	}
	app(item.StateDone, item.StateDone, "gate fail")
	if got := s.lastGateResult(id); !strings.Contains(got, "last=fail") {
		t.Fatalf("trailing gate fail must read fail: %q", got)
	}
	app(item.StateArchived, item.StateDone, "closure merge did not complete")
	if got := s.lastGateResult(id); !strings.Contains(got, "last=fail") {
		t.Fatalf("the compensation must NOT read as a pass: %q", got)
	}
	app(item.StateActive, item.StateDone, "")
	if got := s.lastGateResult(id); !strings.Contains(got, "last=pass") {
		t.Fatalf("a real later done edge must read pass again: %q", got)
	}
}

// T5: gate evidence and activation survive a journal fold — compacting
// between activation and archive must not disarm the gate; and a fully
// green lifecycle across a fold must still archive cleanly.
func TestOfflineGateSurvivesJournalFold(t *testing.T) {
	root := gateFixture(t, 1)
	sess := connectRoot(t, root)
	id := draftID(t, sess, map[string]any{
		"kind": "task", "title": "gate survives the fold", "body": ambFixturePad})
	callText(t, sess, "move", map[string]any{"id": id, "to": "active"})
	callText(t, sess, "move", map[string]any{"id": id, "to": "done"}) // red gate, journals the fail
	out := callText(t, sess, "compact", map[string]any{"apply": true})
	if !strings.Contains(out, "folded") {
		t.Fatalf("the tiny journal_max must force a fold: %q", out)
	}
	out = callText(t, sess, "move", map[string]any{"id": id, "to": "archived", "note": "post-fold"})
	if !strings.Contains(out, "GATE E") && !strings.Contains(out, "refused whole") {
		t.Fatalf("the fold disarmed the gate:\n%s", out)
	}
	// green retry across another fold archives cleanly
	if err := os.WriteFile(filepath.Join(root, "ok.txt"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	callText(t, sess, "move", map[string]any{"id": id, "to": "active"})
	callText(t, sess, "move", map[string]any{"id": id, "to": "done"})
	callText(t, sess, "compact", map[string]any{"apply": true})
	got := callText(t, sess, "get", map[string]any{"id": id})
	if !strings.Contains(got, "archived") {
		// the compact sweep itself archives gate-clean done items
		out = callText(t, sess, "move", map[string]any{"id": id, "to": "archived", "note": "green post-fold"})
		if strings.Contains(out, "GATE E") {
			t.Fatalf("green lifecycle across a fold must archive:\n%s", out)
		}
	}
}

// T9: the validate pack's gate line reflects the recorded failure.
func TestValidatePackShowsGateFail(t *testing.T) {
	root := gateFixture(t, 0)
	sess := connectRoot(t, root)
	id := draftID(t, sess, map[string]any{
		"kind": "task", "title": "pack shows the red gate", "body": ambFixturePad})
	callText(t, sess, "move", map[string]any{"id": id, "to": "active"})
	callText(t, sess, "move", map[string]any{"id": id, "to": "done"})
	out := callText(t, sess, "validate", map[string]any{"op": "pack", "id": id})
	if !strings.Contains(out, "last=fail") {
		t.Fatalf("pack must show the recorded gate failure, not a false pass:\n%s", out)
	}
}
