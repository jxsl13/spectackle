package mcpserver

// The record/code split fix (B-01KYG56Y): a LIVE item branch (ahead of
// base) is closed ON the branch, never records-only; a stale-era branch
// (fully merged) keeps the -close sidestep; the stranded-PR post-condition
// fires when an open PR still references the item after a closure; orphaned
// items render in state's #health and never in check.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jxsl13/spectackle/internal/journal"
)

func closureGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir,
		"-c", "user.name=closure", "-c", "user.email=closure@localhost"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
	return string(out)
}

// TestArchiveLiveBranchNeverRecordsOnly is incident shape (a): the item
// branch carries a code commit absent from main and is NOT the current
// branch at archive time. The closure must land the code — main contains
// it afterward — and never sidestep to -close.
func TestArchiveLiveBranchNeverRecordsOnly(t *testing.T) {
	root := gitRoot(t)
	writeOfflineGitConfig(t, root)
	sess := connectRoot(t, root)
	id := draftID(t, sess, map[string]any{
		"kind": "task", "title": "live branch closure fixture", "body": ambFixturePad})
	callText(t, sess, "move", map[string]any{"id": id, "to": "active"})
	full := fullIDOf(t, root, id)

	// implement on the item branch (the activation checked it out)
	if err := os.WriteFile(filepath.Join(root, "livework.go"),
		[]byte("package main\n\n// livework is the code the closure must land.\nfunc livework() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCommitAs(t, root, full, "implement livework")
	callText(t, sess, "move", map[string]any{"id": id, "to": "done"})

	// move the checkout OFF the item branch — the incident precondition
	closureGit(t, root, "checkout", "-q", "main")

	out := callText(t, sess, "move", map[string]any{"id": id, "to": "archived", "note": "live-branch closure test"})
	if strings.Contains(out, "-close") {
		t.Fatalf("live branch must not take the records-only sidestep: %q", out)
	}
	if !strings.Contains(out, "merged") {
		t.Fatalf("closure must merge: %q", out)
	}
	logOut := closureGit(t, root, "log", "--format=%s", "main")
	if !strings.Contains(logOut, "implement livework") {
		t.Fatalf("the code commit did not reach main:\n%s", logOut)
	}
}

// Stale-era shape (b): the item branch exists but is fully contained in
// main — the -close records-only path stands.
func TestArchiveStaleEraStillClose(t *testing.T) {
	root := gitRoot(t)
	writeOfflineGitConfig(t, root)
	sess := connectRoot(t, root)
	id := draftID(t, sess, map[string]any{
		"kind": "task", "title": "stale era closure fixture", "body": ambFixturePad})
	full := fullIDOf(t, root, id)
	// a branch at the current head: exists, zero commits ahead
	closureGit(t, root, "branch", "spectackle/"+shortDisplayID(full))
	out := callText(t, sess, "move", map[string]any{"id": id, "to": "archived", "note": "stale era test"})
	if !strings.Contains(out, "-close") {
		t.Fatalf("fully-merged branch must keep the -close path: %q", out)
	}
}

func TestOrphanedItemsSweep(t *testing.T) {
	root := t.TempDir()
	s, sess := connectRootWithServer(t, root)
	// a live item: not orphaned
	live := draftID(t, sess, map[string]any{"kind": "task", "title": "live not orphaned", "body": ambFixturePad})
	_ = live
	// an orphan: create event only, no work.md block
	if err := journal.Append(s.ws, "", journal.Event{
		Ev: journal.EvCreate, ID: "T-01ORPHANFIXTURE0000000000",
	}); err != nil {
		t.Fatal(err)
	}
	s.markDirty()
	got := s.orphanedItems()
	if len(got) != 1 || !strings.Contains(got[0], "T-01ORPHANFIXTURE0000000000") {
		t.Fatalf("orphan sweep wrong: %v", got)
	}
	// state carries it, check does not
	stateOut := callText(t, sess, "state", map[string]any{})
	if !strings.Contains(stateOut, "w orphaned T-01ORPHANFIXTURE") {
		t.Fatalf("state must render the orphan: %q", stateOut)
	}
	checkOut := callText(t, sess, "check", map[string]any{})
	if strings.Contains(checkOut, "orphaned") {
		t.Fatalf("check must never carry the orphan sweep: %q", checkOut)
	}
}
