package mcpserver

// The offline non-negotiables (T-01KYHAH1GJ P4): a full worktree-less
// lifecycle under mode: offline produces ZERO branches beyond the current
// one, ZERO PR records, ZERO push invocations, and a linear commit chain —
// and the archive edge keeps its atomicity when the records commit fails.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitPushSpy prepends a PATH shim that records every `git push` invocation
// to a log file before delegating to the real git. The server shells out
// to plain "git", so the shim sees every push the process could make.
func gitPushSpy(t *testing.T) (logPath string) {
	t.Helper()
	real, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git unavailable")
	}
	shimDir := t.TempDir()
	logPath = filepath.Join(shimDir, "push.log")
	shim := "#!/bin/sh\nfor a in \"$@\"; do\n  if [ \"$a\" = push ]; then echo \"push $*\" >> " + logPath + "; fi\ndone\nexec " + real + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(shimDir, "git"), []byte(shim), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", shimDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return logPath
}

func TestOfflineLifecycleSingleBranchOnly(t *testing.T) {
	pushLog := gitPushSpy(t)
	root := gitRoot(t)
	// the DEFAULT config path: no git block at all — GIT-DEFAULT-001 means
	// this runs offline without a single mode key written
	branchesBefore := closureGit(t, root, "branch", "--list")

	sess := connectRoot(t, root)
	id := draftID(t, sess, map[string]any{
		"kind": "task", "title": "offline single branch lifecycle", "body": ambFixturePad})
	out := callText(t, sess, "move", map[string]any{"id": id, "to": "active"})
	if err := os.WriteFile(filepath.Join(root, "offline_work.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out += callText(t, sess, "move", map[string]any{"id": id, "to": "done"})
	out += callText(t, sess, "move", map[string]any{"id": id, "to": "archived", "note": "offline e2e closure"})

	if !strings.Contains(out, "g offline commit ") {
		t.Fatalf("the commit-only surface must render its evidence line:\n%s", out)
	}
	if strings.Contains(out, "offline://") || strings.Contains(out, "g branch ") || strings.Contains(out, " pr ") {
		t.Fatalf("offline lifecycle staged PR theater:\n%s", out)
	}
	if branchesAfter := closureGit(t, root, "branch", "--list"); branchesAfter != branchesBefore {
		t.Fatalf("branch list changed:\nbefore:\n%s\nafter:\n%s", branchesBefore, branchesAfter)
	}
	if merges := strings.TrimSpace(closureGit(t, root, "rev-list", "--merges", "HEAD")); merges != "" {
		t.Fatalf("offline chain must be linear, found merge commits:\n%s", merges)
	}
	if raw, err := os.ReadFile(pushLog); err == nil && len(raw) > 0 {
		t.Fatalf("git push invoked under offline mode:\n%s", raw)
	}
	got := callText(t, sess, "get", map[string]any{"id": id})
	if !strings.Contains(got, "archived") {
		t.Fatalf("lifecycle did not close: %q", got)
	}
}

// The atomicity twin of the online stranded-closure pin: a records commit
// that FAILS at the archive edge must refuse whole and compensate
// archived->done — closureComplete follows the records commit alone.
func TestOfflineArchiveRefusesWholeOnRecordsFailure(t *testing.T) {
	root := gitRoot(t)
	writeOfflineGitConfig(t, root)
	sess := connectRoot(t, root)
	id := draftID(t, sess, map[string]any{
		"kind": "task", "title": "offline atomicity twin", "body": ambFixturePad})
	callText(t, sess, "move", map[string]any{"id": id, "to": "active"})
	callText(t, sess, "move", map[string]any{"id": id, "to": "done"})

	// rig every later commit to fail: a pre-commit hook that always exits 1
	hookDir := filepath.Join(root, ".git", "hooks")
	if err := os.MkdirAll(hookDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hookDir, "pre-commit"), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	out := callText(t, sess, "move", map[string]any{"id": id, "to": "archived", "note": "must refuse whole"})
	if !strings.Contains(out, "archive refused whole") {
		t.Fatalf("failed records commit must refuse the archive whole:\n%s", out)
	}
	got := callText(t, sess, "get", map[string]any{"id": id})
	if !strings.Contains(got, " done ") {
		t.Fatalf("compensation must land the item back on done: %q", got)
	}

	// un-rig and retry: the plain retry re-drives the edge to completion
	if err := os.Remove(filepath.Join(hookDir, "pre-commit")); err != nil {
		t.Fatal(err)
	}
	out = callText(t, sess, "move", map[string]any{"id": id, "to": "archived", "note": "retry once green"})
	if strings.Contains(out, "refused whole") {
		t.Fatalf("retry after un-rigging must complete:\n%s", out)
	}
	got = callText(t, sess, "get", map[string]any{"id": id})
	if !strings.Contains(got, "archived") {
		t.Fatalf("retry did not archive: %q", got)
	}
}
