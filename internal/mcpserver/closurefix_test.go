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
	inject := writeOnlineGitConfig(t, root)
	s, sess := connectRootWithServer(t, root)
	inject(s)
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
	inject := writeOnlineGitConfig(t, root)
	s, sess := connectRootWithServer(t, root)
	inject(s)
	id := draftID(t, sess, map[string]any{
		"kind": "task", "title": "stale era closure fixture", "body": ambFixturePad})
	full := fullIDOf(t, root, id)
	// a branch at the current head: exists, zero commits ahead
	closureGit(t, root, "branch", "spectackle/"+shortDisplayID(full))
	// online discriminator compares against origin — sync it so the branch
	// reads fully merged there too, matching the stale-era premise
	closureGit(t, root, "push", "-q", "origin", "main")
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

// Atomic archive edge (B-01KYGADQ): a stranded closure refuses the WHOLE
// transition — the item compensates back to done and a plain retry after
// the head is green completes archive + merge.
func TestArchiveRefusesWholeOnStrandedClosure(t *testing.T) {
	root := gitRoot(t)
	inject := writeOnlineGitConfig(t, root)
	s, sess := connectRootWithServer(t, root)
	inject(s)
	id := draftID(t, sess, map[string]any{
		"kind": "task", "title": "atomic archive fixture", "body": ambFixturePad})
	callText(t, sess, "move", map[string]any{"id": id, "to": "active"})
	full := fullIDOf(t, root, id)
	if err := os.WriteFile(filepath.Join(root, "conflict.go"),
		[]byte("package main\n\n// branch side of the rigged conflict.\nfunc branchSide() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCommitAs(t, root, full, "branch side")
	callText(t, sess, "move", map[string]any{"id": id, "to": "done"})

	// rig: a CONFLICTING commit on main touching the same file
	closureGit(t, root, "checkout", "-q", "main")
	if err := os.WriteFile(filepath.Join(root, "conflict.go"),
		[]byte("package main\n\n// main side of the rigged conflict.\nfunc mainSide() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	closureGit(t, root, "add", "-A")
	closureGit(t, root, "commit", "-q", "-m", "main side")

	out := callText(t, sess, "move", map[string]any{"id": id, "to": "archived", "note": "rigged"})
	if !strings.Contains(out, "archive refused whole") {
		t.Fatalf("stranded closure must refuse the whole transition: %q", out)
	}
	// the item is retryable done, not tombstoned
	stateOut := callText(t, sess, "state", map[string]any{})
	if !strings.Contains(stateOut, "atomic archive fixture") || !strings.Contains(stateOut, " done ") {
		t.Fatalf("item must stay done after the refusal: %q", stateOut)
	}

	// unrig: build the resolved merge commit with PLUMBING ONLY — no
	// working tree is ever danced, exactly like production where nobody
	// checks branches out under the resident (the uncommitted archived->
	// done compensation on the serving checkout must survive untouched).
	branch := "spectackle/" + shortDisplayID(full)
	branchTip := strings.TrimSpace(closureGit(t, root, "rev-parse", branch))
	mainTip := strings.TrimSpace(closureGit(t, root, "rev-parse", "main"))
	resolved := filepath.Join(t.TempDir(), "conflict.go")
	if err := os.WriteFile(resolved,
		[]byte("package main\n\n// resolved.\nfunc mainSide() {}\nfunc branchSide() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	blob := strings.TrimSpace(closureGit(t, root, "hash-object", "-w", resolved))
	idx := filepath.Join(t.TempDir(), "idx")
	plumb := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root,
			"-c", "user.name=closure", "-c", "user.email=closure@localhost"}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_INDEX_FILE="+idx)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
		return string(out)
	}
	plumb("read-tree", branch)
	plumb("update-index", "--cacheinfo", "100644,"+blob+",conflict.go")
	tree := strings.TrimSpace(plumb("write-tree"))
	commit := strings.TrimSpace(plumb("commit-tree", tree, "-p", branchTip, "-p", mainTip, "-m", "resolve rigged conflict"))
	closureGit(t, root, "update-ref", "refs/heads/"+branch, commit)

	stateOut = callText(t, sess, "state", map[string]any{})
	if !strings.Contains(stateOut, "atomic archive fixture") {
		t.Fatalf("item lost across the unrig dance: %q", stateOut)
	}
	out = callText(t, sess, "move", map[string]any{"id": id, "to": "archived", "note": "retry after resolve"})
	if !strings.Contains(out, "merged") {
		t.Fatalf("retry after resolve must complete the closure: %q", out)
	}
	_ = s
}
