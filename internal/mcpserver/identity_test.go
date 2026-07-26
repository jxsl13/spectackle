package mcpserver

// Per-call reviewer identity (B-01KYFPNCK): the agent field carries the
// real reviewer on shared sessions; every identity refusal applies to the
// overridden name; pack ops ignore it.

import (
	"strings"
	"testing"
)

func TestVerdictAgentOverride(t *testing.T) {
	root := requireGrillRoot(t)
	t.Setenv("SPECTACKLE_AGENT", "author-a")
	author := connectRoot(t, root)
	prop := draftID(t, author, map[string]any{
		"kind": "proposal", "title": "identity override fixture", "body": ambFixturePad})
	callText(t, author, "grill", map[string]any{"id": prop})

	// same SESSION identity (author-a), overridden per call: records under
	// the override, passing the author refusal because the override is the
	// judged name.
	out := callText(t, author, "grill", map[string]any{
		"op": "verdict", "id": prop, "pass": true, "agent": "remote-reviewer-x"})
	if !strings.Contains(out, "pass by remote-reviewer-x") {
		t.Fatalf("override identity must record: %q", out)
	}

	// spoofing the author through the field is refused
	prop2 := draftID(t, author, map[string]any{
		"kind": "proposal", "title": "identity override author spoof", "body": ambFixturePad})
	callText(t, author, "grill", map[string]any{"id": prop2})
	t.Setenv("SPECTACKLE_AGENT", "reviewer-b")
	reviewer := connectRoot(t, root)
	out = callText(t, reviewer, "grill", map[string]any{
		"op": "verdict", "id": prop2, "pass": true, "agent": "author-a"})
	if !strings.Contains(out, "reviewer is the author") {
		t.Fatalf("author spoof through the field must refuse: %q", out)
	}
	// ephemeral override refused
	out = callText(t, reviewer, "grill", map[string]any{
		"op": "verdict", "id": prop2, "pass": true, "agent": "ag-1a2b"})
	if !strings.Contains(out, "anonymous reviewer") {
		t.Fatalf("ephemeral override must refuse: %q", out)
	}
}
