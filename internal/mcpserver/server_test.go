package mcpserver

import (
	"os"
	"path/filepath"
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

	prop := draftID(t, alice, map[string]any{
		"kind": "proposal", "title": "rebind probe", "targets": []string{"main.go"}})
	callText(t, alice, "move", map[string]any{"id": prop, "to": "approved"})
	out := callText(t, alice, "work", map[string]any{"op": "start", "item": prop})
	wtRoot := ""
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "wt "+prop+" open ") {
			wtRoot = strings.TrimPrefix(l, "wt "+prop+" open ")
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
	if s2.wtItem != prop {
		t.Fatalf("same-agent server rooted at the worktree: wtItem = %q, want %s", s2.wtItem, prop)
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

// TestWorkAbortJournalsIntoItemDir: the abort event belongs in the item's
// context-dir journal — passing the item ID as the dir scaffolded a bogus
// <item-id>/.spectackle directory at the repo root (B-0003).
func TestWorkAbortJournalsIntoItemDir(t *testing.T) {
	root := gitRoot(t)
	t.Setenv("SPECTACKLE_AGENT", "alice")
	alice := connectRoot(t, root)

	prop := draftID(t, alice, map[string]any{
		"kind": "proposal", "title": "abort journal probe", "targets": []string{"main.go"}})
	callText(t, alice, "move", map[string]any{"id": prop, "to": "approved"})
	out := callText(t, alice, "work", map[string]any{"op": "start", "item": prop})
	if !strings.Contains(out, "wt "+prop+" open ") {
		t.Fatalf("work start: %q", out)
	}
	out = callText(t, alice, "work", map[string]any{"op": "abort", "item": prop})
	if !strings.Contains(out, "ok "+prop+" aborted") {
		t.Fatalf("work abort: %q", out)
	}
	if _, err := os.Stat(filepath.Join(root, prop)); err == nil {
		t.Fatal("abort scaffolded a bogus <item-id>/ context dir at the repo root")
	}
	raw, err := os.ReadFile(filepath.Join(root, ".spectackle", "journal.ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"ev":"abort"`) {
		t.Fatal("abort event missing from the item's context-dir journal")
	}
}
