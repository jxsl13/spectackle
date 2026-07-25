package mcpserver

import (
	"strings"
	"testing"
)

// TestNewRebindsExistingWorktree: work op=start in one server process must be
// finishable from another — a fresh Server rooted at the worktree path with
// the same agent identity adopts the open worktree (B-0002: per-call stdio
// clients land submit in a different process than start, and a resident
// server that dies would otherwise orphan its worktree permanently). A
// different identity must NOT adopt it: recovering a dead sibling's worktree
// stays an explicit work op=abort decision.
func TestNewRebindsExistingWorktree(t *testing.T) {
	root := gitRoot(t)
	t.Setenv("SPECTACKLE_AGENT", "alice")
	alice := connectRoot(t, root)

	callText(t, alice, "draft", map[string]any{
		"kind": "proposal", "title": "rebind probe", "targets": []string{"main.go"}})
	callText(t, alice, "move", map[string]any{"id": "P-0001", "to": "approved"})
	out := callText(t, alice, "work", map[string]any{"op": "start", "item": "P-0001"})
	wtRoot := ""
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "wt P-0001 open ") {
			wtRoot = strings.TrimPrefix(l, "wt P-0001 open ")
		}
	}
	if wtRoot == "" {
		t.Fatalf("work start: %q", out)
	}

	s2, err := New(wtRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if s2.wtItem != "P-0001" {
		t.Fatalf("same-agent server rooted at the worktree: wtItem = %q, want P-0001", s2.wtItem)
	}

	t.Setenv("SPECTACKLE_AGENT", "mallory")
	s3, err := New(wtRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer s3.Close()
	if s3.wtItem != "" {
		t.Fatalf("mismatched-agent server must not adopt the worktree: wtItem = %q", s3.wtItem)
	}
}
