package mcpserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jxsl13/spectackle/internal/item"
	"github.com/jxsl13/spectackle/internal/journal"
)

// RED-RUN (T-01KYD88M, written first): an unconsumed research item with no
// closing note must not archive — research that changes nothing is pure
// token cost, and until this gate nothing noticed.
func TestUnconsumedResearchRefusedAtArchive(t *testing.T) {
	root := gitRoot(t)
	t.Setenv("SPECTACKLE_AGENT", "researcher")
	sess := connectRoot(t, root)
	r := draftID(t, sess, map[string]any{
		"kind": "research", "title": "a study nothing ever cited"})
	out := callText(t, sess, "move", map[string]any{"id": r, "to": "archived"})
	if !strings.Contains(out, "! BACKPROP E") || !strings.Contains(out, "unconsumed research") {
		t.Fatalf("unconsumed research archived silently: %q", out)
	}
	// an 80+ character closing note is the explicit no-action return path
	out = callText(t, sess, "move", map[string]any{"id": r, "to": "archived",
		"note": "closed without action: the study found the existing design already covers every case it examined, so no follow-up item is warranted"})
	if !strings.Contains(out, "archived") {
		t.Fatalf("explicit closing note refused: %q", out)
	}
}

// A consumer's Refs citation is the primary return path — live or archived.
func TestConsumedResearchArchives(t *testing.T) {
	root := gitRoot(t)
	t.Setenv("SPECTACKLE_AGENT", "researcher")
	sess := connectRoot(t, root)
	r := draftID(t, sess, map[string]any{
		"kind": "research", "title": "a study with a consumer"})
	full := fullIDOf(t, root, r)
	draftID(t, sess, map[string]any{
		"kind": "task", "title": "acts on the study", "refs": []string{full}})
	out := callText(t, sess, "move", map[string]any{"id": r, "to": "archived"})
	if !strings.Contains(out, "archived") {
		t.Fatalf("consumed research refused: %q", out)
	}
}

// A rule whose rationale cites the R-id counts; so does an ARCHIVED
// consumer (its refs survive in the archive journal trail).
func TestResearchConsumerViaRuleAndArchive(t *testing.T) {
	root := gitRoot(t)
	t.Setenv("SPECTACKLE_AGENT", "researcher")
	sess := connectRoot(t, root)
	r := draftID(t, sess, map[string]any{
		"kind": "research", "title": "cited by a rule rationale"})
	full := fullIDOf(t, root, r)
	s, _ := connectRootWithServer(t, root)
	items, _ := item.LoadAll(s.ws)
	events, _ := journalReadAllForTest(s)
	if researchConsumedIn(items, events, nil, full) {
		t.Fatal("unconsumed study read as consumed")
	}
	// a consumer task, then archived: the citation must survive archival
	c := draftID(t, sess, map[string]any{
		"kind": "task", "title": "consumes then archives", "refs": []string{full}})
	callText(t, sess, "move", map[string]any{"id": c, "to": "done"})
	// validation gate: independent pass
	t.Setenv("SPECTACKLE_AGENT", "val-x")
	val := connectRoot(t, root)
	callText(t, val, "validate", map[string]any{"id": c})
	callText(t, val, "validate", map[string]any{"op": "verdict", "id": c, "pass": true})
	callText(t, sess, "move", map[string]any{"id": c, "to": "archived"})
	out := callText(t, sess, "move", map[string]any{"id": r, "to": "archived"})
	if !strings.Contains(out, "archived") {
		t.Fatalf("archived consumer did not count: %q", out)
	}
}

// Cost flatness: the pure decision answers correctly with the .spectackle
// tree GONE — zero filesystem reads by construction.
func TestResearchGateReadsNothing(t *testing.T) {
	root := gitRoot(t)
	t.Setenv("SPECTACKLE_AGENT", "researcher")
	sess := connectRoot(t, root)
	r := draftID(t, sess, map[string]any{"kind": "research", "title": "no-read study"})
	full := fullIDOf(t, root, r)
	c := draftID(t, sess, map[string]any{
		"kind": "task", "title": "cites it", "refs": []string{full}})
	_ = c
	s, _ := connectRootWithServer(t, root)
	items, err := item.LoadAll(s.ws)
	if err != nil {
		t.Fatal(err)
	}
	events, _ := journalReadAllForTest(s)
	// rename the tree out from under the loaded state (portable across CI,
	// unlike chmod 0 which root ignores)
	if err := os.Rename(filepath.Join(root, ".spectackle"), filepath.Join(root, ".spectackle-gone")); err != nil {
		t.Fatal(err)
	}
	defer os.Rename(filepath.Join(root, ".spectackle-gone"), filepath.Join(root, ".spectackle"))
	if !researchConsumedIn(items, events, nil, full) {
		t.Fatal("consumed study unread as unconsumed on loaded state")
	}
	if researchConsumedIn(items, events, nil, "R-01NONEXISTENT") {
		t.Fatal("phantom study read as consumed")
	}
}

func journalReadAllForTest(s *Server) ([]journal.Event, error) { return journal.ReadAll(s.ws) }
