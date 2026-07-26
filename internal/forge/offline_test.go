package forge

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jxsl13/spectackle/internal/wt"
)

// gitOut runs git in dir and fails the test on error, for assertions only
// (production code paths are exercised through Offline itself).
func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// repoWithBranch builds a repo on main with a divergent feature branch, so
// a merge of the branch back into main cannot fast-forward — exactly the
// shape Offline.Merge must handle with --no-ff to still produce a merge
// commit.
func repoWithBranch(t *testing.T) (root, branch string) {
	t.Helper()
	root = t.TempDir()
	os.WriteFile(filepath.Join(root, "a.txt"), []byte("main\n"), 0o644)
	if err := wt.InitTestRepo(root); err != nil {
		t.Skipf("git unavailable: %v", err)
	}

	branch = "agent/forge-client"
	gitOut(t, root, "-c", "user.name=spectackle", "-c", "user.email=spectackle@localhost", "checkout", "-b", branch)
	os.WriteFile(filepath.Join(root, "b.txt"), []byte("feature\n"), 0o644)
	gitOut(t, root, "add", "-A")
	gitOut(t, root, "-c", "user.name=spectackle", "-c", "user.email=spectackle@localhost", "commit", "-m", "feature commit")

	gitOut(t, root, "-c", "user.name=spectackle", "-c", "user.email=spectackle@localhost", "checkout", "main")
	os.WriteFile(filepath.Join(root, "a.txt"), []byte("main changed\n"), 0o644)
	gitOut(t, root, "add", "-A")
	gitOut(t, root, "-c", "user.name=spectackle", "-c", "user.email=spectackle@localhost", "commit", "-m", "main diverges")

	return root, branch
}

func TestOfflineOpenReadyFind(t *testing.T) {
	root, branch := repoWithBranch(t)
	o := NewOffline(root, "main")

	if _, ok, err := o.Find(branch); err != nil || ok {
		t.Fatalf("Find before Open = %v %v, want false", ok, err)
	}

	pr, err := o.Open(branch, "main", "title", "body")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !pr.Draft {
		t.Fatalf("Open PR should start draft: %+v", pr)
	}

	if _, err := o.Open(branch, "main", "title", "body"); err == nil {
		t.Fatal("second Open for the same branch should error — caller must Find instead")
	}

	found, ok, err := o.Find(branch)
	if err != nil || !ok || found.Number != pr.Number {
		t.Fatalf("Find after Open = %+v %v %v", found, ok, err)
	}

	ready, err := o.Ready(pr)
	if err != nil {
		t.Fatalf("Ready: %v", err)
	}
	if ready.Draft {
		t.Fatalf("Ready did not clear draft: %+v", ready)
	}
}

func TestOfflineMergeProducesRealMergeCommit(t *testing.T) {
	root, branch := repoWithBranch(t)
	o := NewOffline(root, "main")

	pr, err := o.Open(branch, "main", "title", "body")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	res, err := o.Merge(pr)
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if !res.Merged || res.SHA == "" {
		t.Fatalf("Merge result = %+v", res)
	}

	// Ground truth lives in git history, not the return value: a real merge
	// commit has two parents. `git log --oneline -1 --format=%P` on HEAD
	// lists parent hashes space-separated.
	parents := gitOut(t, root, "log", "-1", "--format=%P", "HEAD")
	if got := len(strings.Fields(parents)); got != 2 {
		t.Fatalf("HEAD has %d parents (%q), want 2 — Merge must produce a real merge commit, not a fast-forward or no-op", got, parents)
	}

	headSHA := gitOut(t, root, "rev-parse", "HEAD")
	if res.SHA != headSHA {
		t.Fatalf("MergeResult.SHA = %q, want HEAD %q", res.SHA, headSHA)
	}

	// b.txt only exists on the feature branch: its presence on main after
	// merge additionally confirms the branch content actually landed.
	if _, err := os.Stat(filepath.Join(root, "b.txt")); err != nil {
		t.Fatalf("feature content missing from main after merge: %v", err)
	}

	// Merged PR is no longer tracked as open.
	if _, ok, err := o.Find(branch); err != nil || ok {
		t.Fatalf("Find after Merge = %v %v, want false (PR no longer open)", ok, err)
	}
}

// TestOfflineMergeForcesCommitEvenWhenFastForwardable proves --no-ff is
// load-bearing: when the branch is a pure fast-forward of main, a plain
// `git merge` produces NO merge commit at all (HEAD just moves), which is
// exactly the fast-forward-only shape the brief forbids. Offline.Merge must
// still leave a two-parent merge commit.
func TestOfflineMergeForcesCommitEvenWhenFastForwardable(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "a.txt"), []byte("main\n"), 0o644)
	if err := wt.InitTestRepo(root); err != nil {
		t.Skipf("git unavailable: %v", err)
	}
	branch := "agent/forge-client"
	gitOut(t, root, "-c", "user.name=spectackle", "-c", "user.email=spectackle@localhost", "checkout", "-b", branch)
	os.WriteFile(filepath.Join(root, "b.txt"), []byte("feature\n"), 0o644)
	gitOut(t, root, "add", "-A")
	gitOut(t, root, "-c", "user.name=spectackle", "-c", "user.email=spectackle@localhost", "commit", "-m", "feature commit")
	gitOut(t, root, "-c", "user.name=spectackle", "-c", "user.email=spectackle@localhost", "checkout", "main")

	o := NewOffline(root, "main")
	pr, err := o.Open(branch, "main", "title", "body")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := o.Merge(pr); err != nil {
		t.Fatalf("Merge: %v", err)
	}

	parents := gitOut(t, root, "log", "-1", "--format=%P", "HEAD")
	if got := len(strings.Fields(parents)); got != 2 {
		t.Fatalf("fast-forwardable merge produced %d parents (%q), want 2 — a bare fast-forward here would mean Merge is fast-forward-only, which the offline contract forbids", got, parents)
	}
}

func TestOfflineMergeUnknownPRErrors(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "a.txt"), []byte("x\n"), 0o644)
	if err := wt.InitTestRepo(root); err != nil {
		t.Skipf("git unavailable: %v", err)
	}
	o := NewOffline(root, "main")

	if _, err := o.Merge(PR{Number: 1, Branch: "nonexistent"}); err == nil {
		t.Fatal("Merge of an untracked PR should error")
	}
}

// TestOfflineReadySurvivesReload pins B-01KYDV: the draft flip must reach
// the state file, because every `spectackle call` is its own process — a
// flip held only in memory makes the NEXT process's Find report a stale
// draft, and the archive path then gates and flips a second time. Open and
// Merge already saved; Ready was the one mutation that did not.
func TestOfflineReadySurvivesReload(t *testing.T) {
	dir := t.TempDir()
	state := filepath.Join(dir, "forge-offline.json")

	o1 := NewOfflinePersistent(dir, "main", state)
	pr, err := o1.Open("feature/x", "main", "t", "b")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !pr.Draft {
		t.Fatal("Open must create a draft")
	}
	if _, err := o1.Ready(pr); err != nil {
		t.Fatalf("Ready: %v", err)
	}

	// A NEW instance over the same state file is the next process.
	o2 := NewOfflinePersistent(dir, "main", state)
	got, ok, err := o2.Find("feature/x")
	if err != nil || !ok {
		t.Fatalf("Find after reload: ok=%v err=%v", ok, err)
	}
	if got.Draft {
		t.Fatal("Ready's draft flip was lost across the reload — o.save() missing in Ready")
	}
}

// TestOfflineDraftFlipSurvivesReload: the reopen flip persists across
// processes like every other mutation (B-01KYDV precedent), and the full
// drafted-readied-drafted-readied sequence lands in the state file.
func TestOfflineDraftFlipSurvivesReload(t *testing.T) {
	dir := t.TempDir()
	state := filepath.Join(dir, "forge-offline.json")

	o1 := NewOfflinePersistent(dir, "main", state)
	pr, err := o1.Open("feature/reopen", "main", "t", "b")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if pr, err = o1.Ready(pr); err != nil {
		t.Fatalf("Ready: %v", err)
	}
	if pr, err = o1.Draft(pr); err != nil {
		t.Fatalf("Draft: %v", err)
	}
	if !pr.Draft {
		t.Fatal("Draft returned a still-ready PR")
	}

	o2 := NewOfflinePersistent(dir, "main", state)
	got, ok, err := o2.Find("feature/reopen")
	if err != nil || !ok {
		t.Fatalf("Find after reload: ok=%v err=%v", ok, err)
	}
	if !got.Draft {
		t.Fatal("reopen draft flip lost across reload")
	}
}
