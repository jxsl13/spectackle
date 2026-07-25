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
	// wtItem holds the STORED ID; prop is the displayed one the tool emitted,
	// which is by construction a prefix of it (ADR-0013). HasPrefix is the
	// assertion that stays true whichever length the display form happened to
	// need.
	if !strings.HasPrefix(s2.wtItem, prop) {
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

// TestNewMainResolutionUnchangedForNestedWorktree guards issue 27's fix
// (workspace.Detect now treats a .git FILE, not just a .git directory, as a
// boundary that stops the upward walk) against collapsing the ACTIVE root
// and the MAIN root into the same thing. New() resolves them separately on
// purpose: coordination state (coord.db, leases, agents) must keep living in
// the main checkout even when the server is rooted at a linked worktree, and
// main resolution goes through wt.CommonRoot's `git rev-parse
// --show-toplevel --git-common-dir`, not through Detect's marker walk — so
// it was never expected to move, but that is worth asserting rather than
// assuming, especially right after changing what Detect's walk stops at.
// work op=start's own worktree (created under .spectackle/wt/, carrying a
// committed .spectackle/config.yaml) is exactly this fixture, and doubles as
// the "work op=start's own worktrees resolve as before" check: New(wtRoot)
// must still adopt it (TestNewRebindsExistingWorktree covers that half; this
// test covers that .ws and .main land on the right directories).
func TestNewMainResolutionUnchangedForNestedWorktree(t *testing.T) {
	root := gitRoot(t)
	t.Setenv("SPECTACKLE_AGENT", "alice")
	alice := connectRoot(t, root)

	prop := draftID(t, alice, map[string]any{
		"kind": "proposal", "title": "main resolution probe", "targets": []string{"main.go"}})
	callText(t, alice, "move", map[string]any{"id": prop, "to": "approved"})
	out := callText(t, alice, "work", map[string]any{"op": "start", "item": prop})
	wtRoot := wtRootOf(t, out, prop)

	s2, err := New(wtRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if s2.ws.Dir != wtRoot {
		t.Fatalf("active root = %s, want the worktree %s", s2.ws.Dir, wtRoot)
	}
	// resolved through realPath: wt.CommonRoot's main resolution goes through
	// `git rev-parse`, which reports the symlink-resolved path (e.g.
	// /private/var/... on macOS, where t.TempDir() itself returns the
	// /var/... alias) — a byte-for-byte compare against root would fail on
	// that alone, not on anything this test actually cares about.
	if realPath(t, s2.main.Dir) != realPath(t, root) {
		t.Fatalf("main root = %s, want the enclosing checkout %s (coordination state must not have moved)", s2.main.Dir, root)
	}
}

func realPath(t *testing.T, p string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return r
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
