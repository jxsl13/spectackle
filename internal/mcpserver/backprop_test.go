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

// TestArchiveFoldRunsChildGates: the gate above keyed on the item the CALL
// named, while lifecycle.archive folds every DONE child away with the parent
// through a second archive path that had no gates at all. So parenting the
// study to any item and archiving THAT item removed it anyway — two calls, no
// refusal, exit 0, and `move` on the study afterwards answered "unknown item"
// because its only remaining home was a tombstone (B-01KYS6ZKRQEHW finding 1).
func TestArchiveFoldRunsChildGates(t *testing.T) {
	root := gitRoot(t)
	t.Setenv("SPECTACKLE_AGENT", "researcher")
	sess := connectRoot(t, root)
	p := draftID(t, sess, map[string]any{
		"kind": "proposal", "title": "a parent nobody gated", "body": ambFixturePad})
	r := draftID(t, sess, map[string]any{
		"kind": "research", "title": "a study folded away through its parent", "parent": p})
	callText(t, sess, "move", map[string]any{"id": r, "to": "done"})

	out, isErr := callRaw(t, sess, "move", map[string]any{"id": p, "to": "archived", "note": "closing the parent"})
	if !isErr {
		t.Fatalf("archiving the parent folded the ungated study away, exit 0: %q", out)
	}
	// the gate that would have refused the study directly, naming the CHILD —
	// the parent's short ID alone would leave the operator hunting.
	if !strings.Contains(out, "! BACKPROP E") || !strings.Contains(out, "unconsumed research") {
		t.Fatalf("refusal must be the research gate's own record: %q", out)
	}
	if !strings.Contains(out, r) {
		t.Fatalf("refusal must name the child %s, got %q", r, out)
	}
	// THE ACTION, not the wording: nothing was removed. Both records are
	// still live and the child is still movable — before the fix `move` on
	// the child answered "unknown item".
	stateOut := callText(t, sess, "state", map[string]any{})
	if !strings.Contains(stateOut, "a parent nobody gated") {
		t.Fatalf("the parent left work.md despite the refusal: %q", stateOut)
	}
	if !strings.Contains(stateOut, "a study folded away through its parent") {
		t.Fatalf("the refused archive destroyed the child anyway: %q", stateOut)
	}
	if out, isErr := callRaw(t, sess, "move", map[string]any{"id": r, "to": "active", "note": "reopen the study"}); isErr ||
		strings.Contains(out, "unknown item") {
		t.Fatalf("the child is unreachable after a refused parent archive: %q", out)
	}
}

// TestArchiveFoldLetsGatedChildrenThrough is the other half: the fold is the
// normal way a closed subtree archives, so a child that passes its own gates
// must still fold. A gate that refuses everything is not a gate.
func TestArchiveFoldLetsGatedChildrenThrough(t *testing.T) {
	root := gitRoot(t)
	t.Setenv("SPECTACKLE_AGENT", "researcher")
	sess := connectRoot(t, root)
	p := draftID(t, sess, map[string]any{
		"kind": "proposal", "title": "a parent with a consumed child", "body": ambFixturePad})
	r := draftID(t, sess, map[string]any{
		"kind": "research", "title": "a study something cites", "parent": p})
	full := fullIDOf(t, root, r)
	// a live consumer: the primary return path, so the child's own gate passes
	draftID(t, sess, map[string]any{
		"kind": "task", "title": "acts on the folded study", "refs": []string{full}})
	callText(t, sess, "move", map[string]any{"id": r, "to": "done"})

	out, isErr := callRaw(t, sess, "move", map[string]any{"id": p, "to": "archived", "note": "closing the parent"})
	if isErr {
		t.Fatalf("a child that passes its own gates must still fold: %q", out)
	}
	stateOut := callText(t, sess, "state", map[string]any{})
	if strings.Contains(stateOut, "a study something cites") {
		t.Fatalf("the consumed child did not fold: %q", stateOut)
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
