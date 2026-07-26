package mcpserver

import (
	"strings"
	"testing"
)

// RED-RUN (B-01KYER, written first): a draft-state body revision through
// the draft tool must exist — grill feedback could not amend the record it
// critiqued, forcing successor-drafts and rejection-corpus pollution.
func TestDraftRevisionAmendsBody(t *testing.T) {
	root := requireGrillRoot(t)
	t.Setenv("SPECTACKLE_AGENT", "author-a")
	sess := connectRoot(t, root)
	prop := draftID(t, sess, map[string]any{
		"kind": "proposal", "title": "first cut",
		"body": "names internal/missing/path.go which does not exist. The remainder of this brief restates scope, constraints, verification commands, and rollback in enough real sentences that the calibrated ambiguity floor does not fire on a fixture that exists to probe a different class entirely. It names its files, states its exit criterion, records what is deliberately out of scope for the probe at hand, and notes which sibling machinery the assertions below actually exercise."})
	out := callText(t, sess, "grill", map[string]any{"id": prop})
	if !strings.Contains(out, "g nopath internal/missing/path.go") {
		t.Fatalf("fixture finding missing: %q", out)
	}
	out = callText(t, sess, "draft", map[string]any{
		"id": prop, "body": "the path concern is resolved: no file references remain. The remainder of this brief restates scope, constraints, verification commands, and rollback in enough real sentences that the calibrated ambiguity floor does not fire on a fixture that exists to probe a different class entirely. It names its files, states its exit criterion, records what is deliberately out of scope for the probe at hand, and notes which sibling machinery the assertions below actually exercise."})
	if !strings.Contains(out, "i ") || strings.Contains(out, "! ") {
		t.Fatalf("revision refused: %q", out)
	}
	out = callText(t, sess, "grill", map[string]any{"id": prop})
	if strings.Contains(out, "g nopath") {
		t.Fatalf("revised body still carries the old finding: %q", out)
	}
}

// Revision is draft-state only: submitted and later are the frozen review
// subject.
func TestDraftRevisionRefusedPastDraft(t *testing.T) {
	root := requireGrillRoot(t)
	t.Setenv("SPECTACKLE_AGENT", "author-a")
	sess := connectRoot(t, root)
	prop := draftID(t, sess, map[string]any{
		"kind": "proposal", "title": "freezes at submitted"})
	callText(t, sess, "move", map[string]any{"id": prop, "to": "submitted"})
	out := callText(t, sess, "draft", map[string]any{"id": prop, "body": "too late"})
	if !strings.Contains(out, "draft-state only") {
		t.Fatalf("post-draft revision not refused: %q", out)
	}
}

// A verdict stamped on the old substance no longer gates the revised one —
// the existing hash machinery, exercised through the new write path.
func TestDraftRevisionExpiresVerdict(t *testing.T) {
	root := requireGrillRoot(t)
	t.Setenv("SPECTACKLE_AGENT", "author-a")
	author := connectRoot(t, root)
	prop := draftID(t, author, map[string]any{
		"kind": "proposal", "title": "reviewed then revised", "body": "The remainder of this brief restates scope, constraints, verification commands, and rollback in enough real sentences that the calibrated ambiguity floor does not fire on a fixture that exists to probe a different class entirely. It names its files, states its exit criterion, records what is deliberately out of scope for the probe at hand, and notes which sibling machinery the assertions below actually exercise."})
	callText(t, author, "grill", map[string]any{"id": prop})
	t.Setenv("SPECTACKLE_AGENT", "reviewer-b")
	reviewer := connectRoot(t, root)
	callText(t, reviewer, "grill", map[string]any{"op": "verdict", "id": prop, "pass": true})
	callText(t, author, "draft", map[string]any{"id": prop, "body": "materially different plan after review. The remainder of this brief restates scope, constraints, verification commands, and rollback in enough real sentences that the calibrated ambiguity floor does not fire on a fixture that exists to probe a different class entirely. It names its files, states its exit criterion, records what is deliberately out of scope for the probe at hand, and notes which sibling machinery the assertions below actually exercise."})
	out := callText(t, author, "move", map[string]any{"id": prop, "to": "approved"})
	if !strings.Contains(out, "stale review") {
		t.Fatalf("revision did not expire the verdict: %q", out)
	}
}
