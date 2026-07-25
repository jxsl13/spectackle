package wt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func repo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "a.go"), []byte("package a\n"), 0o644)
	if err := InitTestRepo(root); err != nil {
		t.Skipf("git unavailable: %v", err)
	}
	return root
}

func TestCommonRootMainAndLinked(t *testing.T) {
	root := repo(t)
	main, isWT, err := CommonRoot(root)
	if err != nil || isWT || !samePath(main, root) {
		t.Fatalf("CommonRoot(main) = %q %v %v", main, isWT, err)
	}
	wtRoot := filepath.Join(root, ".spectackle", "wt", "T-0001")
	if err := Add(root, wtRoot, "spectackle/T-0001", "HEAD"); err != nil {
		t.Fatal(err)
	}
	main2, isWT2, err := CommonRoot(wtRoot)
	if err != nil || !isWT2 || !samePath(main2, root) {
		t.Fatalf("CommonRoot(worktree) = %q %v %v", main2, isWT2, err)
	}
	if err := Remove(root, wtRoot); err != nil {
		t.Fatal(err)
	}
	// leftover recovery: Add over a stale dir succeeds
	os.MkdirAll(wtRoot, 0o755)
	if err := Add(root, wtRoot, "spectackle/T-0001", "HEAD"); err != nil {
		t.Fatalf("Add over leftover: %v", err)
	}
	Remove(root, wtRoot)
}

func TestCommitCodeExcludesSpectackle(t *testing.T) {
	root := repo(t)
	// code change + .spectackle change side by side
	os.WriteFile(filepath.Join(root, "b.go"), []byte("package a\n"), 0o644)
	os.MkdirAll(filepath.Join(root, ".spectackle"), 0o755)
	os.WriteFile(filepath.Join(root, ".spectackle", "work.md"), []byte("---\nschema: v1\n---\n"), 0o644)
	os.MkdirAll(filepath.Join(root, "sub", ".spectackle"), 0o755)
	os.WriteFile(filepath.Join(root, "sub", ".spectackle", "spec.md"), []byte("---\nschema: v1\n---\n"), 0o644)

	committed, err := CommitCode(root, "code only")
	if err != nil || !committed {
		t.Fatalf("CommitCode = %v %v", committed, err)
	}
	out, err := git(root, "show", "--name-only", "--format=", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "b.go") {
		t.Fatalf("code missing from commit: %q", out)
	}
	if strings.Contains(out, ".spectackle") {
		t.Fatalf("commit leaked .spectackle state — the whole merge strategy rests on this exclude: %q", out)
	}
	// nothing left to commit -> committed=false
	if committed, _ := CommitCode(root, "empty"); committed {
		t.Fatal("empty tree reported a commit")
	}
}

func TestMergeConflictAndTouched(t *testing.T) {
	root := repo(t)
	wtRoot := filepath.Join(root, ".spectackle", "wt", "T-0002")
	base, _ := Head(root)
	if err := Add(root, wtRoot, "spectackle/T-0002", "HEAD"); err != nil {
		t.Fatal(err)
	}
	// conflicting edits to the same file on both sides
	os.WriteFile(filepath.Join(root, "a.go"), []byte("package a // main\n"), 0o644)
	if _, err := git(root, "commit", "-am", "main edit"); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(wtRoot, "a.go"), []byte("package a // branch\n"), 0o644)
	if _, err := CommitCode(wtRoot, "branch edit"); err != nil {
		t.Fatal(err)
	}
	conflicts, err := MergeMain(wtRoot)
	if err != nil || len(conflicts) != 1 || conflicts[0] != "a.go" {
		t.Fatalf("MergeMain = %v %v", conflicts, err)
	}
	if !MergeInProgress(wtRoot) {
		t.Fatal("conflicted merge not detected as in progress")
	}
	// resolve + conclude, then ff works
	os.WriteFile(filepath.Join(wtRoot, "a.go"), []byte("package a // merged\n"), 0o644)
	if committed, err := CommitCode(wtRoot, "resolve"); err != nil || !committed {
		t.Fatalf("conclude merge: %v %v", committed, err)
	}
	if err := FFMain(root, "spectackle/T-0002"); err != nil {
		t.Fatalf("FFMain: %v", err)
	}
	touched, _ := TouchedFiles(root, base, "spectackle/T-0002")
	if len(touched) != 1 || touched[0] != "a.go" {
		t.Fatalf("TouchedFiles = %v", touched)
	}
}

// TestMergeMainResolvesPrimaryBranch: the integration target is whatever
// branch the primary checkout has checked out, not a ref literally named
// "main" — a repo developing on another branch (with a stale local main)
// otherwise gets a silent no-op merge and a diverging-branches fast-forward
// failure at submit (B-0004).
func TestMergeMainResolvesPrimaryBranch(t *testing.T) {
	root := repo(t)
	// develop on a differently named branch; the stale "main" stays behind
	if _, err := git(root, "checkout", "-b", "feature/dev"); err != nil {
		t.Fatal(err)
	}
	wtRoot := filepath.Join(root, ".spectackle", "wt", "T-0001")
	if err := Add(root, wtRoot, "spectackle/T-0001", "HEAD"); err != nil {
		t.Fatal(err)
	}
	// the primary branch advances after the worktree branched
	os.WriteFile(filepath.Join(root, "b.go"), []byte("package a\n"), 0o644)
	if _, err := git(root, "add", "-A"); err != nil {
		t.Fatal(err)
	}
	if _, err := git(root, "commit", "-m", "advance feature/dev"); err != nil {
		t.Fatal(err)
	}

	conflicts, err := MergeMain(wtRoot)
	if err != nil || len(conflicts) > 0 {
		t.Fatalf("MergeMain: %v %v", conflicts, err)
	}
	// the worktree picked up the primary branch's commit...
	if _, err := os.Stat(filepath.Join(wtRoot, "b.go")); err != nil {
		t.Fatalf("worktree missing primary-branch commit: %v", err)
	}
	// ...so the primary branch fast-forwards to the worktree branch
	if err := FFMain(root, "spectackle/T-0001"); err != nil {
		t.Fatalf("FFMain after resolved merge: %v", err)
	}
}

// TestCommitCodeNoOpsOnUnstagedSpectackleOnly: when the only dirt is
// .spectackle state (excluded from the code-only pathspec), CommitCode must
// report committed=false with no error. The old porcelain parsing read the
// first status line's trimmed-away leading space as a staged marker and
// issued an empty commit that errored (B-0005) — the near-universal retry
// case, where the code commit already exists from a prior gate round.
func TestCommitCodeNoOpsOnUnstagedSpectackleOnly(t *testing.T) {
	root := repo(t)
	specDir := filepath.Join(root, ".spectackle")
	os.MkdirAll(specDir, 0o755)
	os.WriteFile(filepath.Join(specDir, "journal.ndjson"), []byte("{}\n"), 0o644)
	if _, err := git(root, "add", "-A"); err != nil {
		t.Fatal(err)
	}
	if _, err := git(root, "commit", "-m", "track spectackle state"); err != nil {
		t.Fatal(err)
	}
	// dirty ONLY the .spectackle file — first (and only) porcelain line is
	// an unstaged " M" entry, the exact shape the trim corrupted
	os.WriteFile(filepath.Join(specDir, "journal.ndjson"), []byte("{}\n{}\n"), 0o644)

	committed, err := CommitCode(root, "should be a no-op")
	if err != nil {
		t.Fatalf("CommitCode must no-op cleanly, got error: %v", err)
	}
	if committed {
		t.Fatal("CommitCode reported a commit with nothing code-side to commit")
	}
}

// TestEnsureBranchIdempotent: a second call on a branch that already exists
// must be a plain checkout, never an error — the transition mapping retries
// the same task's branch across multiple rounds (gate fails, reopens, ...)
// and can't treat "the branch is already there" as a failure.
func TestEnsureBranchIdempotent(t *testing.T) {
	root := repo(t)
	branch := "spectackle/T-9000"
	if err := EnsureBranch(root, branch, "HEAD"); err != nil {
		t.Fatalf("EnsureBranch (create): %v", err)
	}
	if got, err := CurrentBranch(root); err != nil || got != branch {
		t.Fatalf("CurrentBranch after create = %q %v, want %q", got, err, branch)
	}
	// switch away, then ensure again against the SAME branch
	if _, err := git(root, "checkout", "main"); err != nil {
		t.Fatal(err)
	}
	if err := EnsureBranch(root, branch, "HEAD"); err != nil {
		t.Fatalf("EnsureBranch (second call, existing branch) errored: %v", err)
	}
	if got, err := CurrentBranch(root); err != nil || got != branch {
		t.Fatalf("CurrentBranch after second call = %q %v, want %q", got, err, branch)
	}
}

// TestPushSetsUpstreamAndUnpushedCommitsReport proves Push and
// HasUnpushedCommits against a REAL local bare remote rather than a mock:
// unpushed is true before the first Push (no remote-tracking ref exists yet
// to compare against) and false once Push has landed the commits and set
// upstream tracking.
func TestPushSetsUpstreamAndUnpushedCommitsReport(t *testing.T) {
	root := repo(t)
	bareDir := t.TempDir()
	if _, err := git(bareDir, "init", "--bare", "-q", "-b", "main"); err != nil {
		t.Skipf("git unavailable: %v", err)
	}
	if _, err := git(root, "remote", "add", "origin", bareDir); err != nil {
		t.Fatal(err)
	}
	branch := "spectackle/T-9001"
	if err := EnsureBranch(root, branch, "HEAD"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "c.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if committed, err := CommitCode(root, "code change"); err != nil || !committed {
		t.Fatalf("CommitCode = %v %v", committed, err)
	}

	if unpushed, err := HasUnpushedCommits(root, "origin", branch); err != nil || !unpushed {
		t.Fatalf("HasUnpushedCommits before push = %v %v, want true", unpushed, err)
	}

	if err := Push(root, "origin", branch); err != nil {
		t.Fatalf("Push: %v", err)
	}

	if up, err := git(root, "rev-parse", "--abbrev-ref", branch+"@{upstream}"); err != nil || up != "origin/"+branch {
		t.Fatalf("upstream after first push = %q %v, want %q", up, err, "origin/"+branch)
	}

	if unpushed, err := HasUnpushedCommits(root, "origin", branch); err != nil || unpushed {
		t.Fatalf("HasUnpushedCommits after push = %v %v, want false", unpushed, err)
	}
}

// TestBranchEnsureAndCommitNeverLeakSpectackle proves the new branch-ensure
// primitive doesn't reopen the door B-0006 closed: committing through
// EnsureBranch + CommitCode must still exclude every .spectackle path, since
// that exclusion — not a git merge — is the whole reason record state is safe
// to keep uncommitted in a worktree at all.
func TestBranchEnsureAndCommitNeverLeakSpectackle(t *testing.T) {
	root := repo(t)
	if err := EnsureBranch(root, "spectackle/T-9002", "HEAD"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "d.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".spectackle"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".spectackle", "journal.ndjson"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	committed, err := CommitCode(root, "primitives commit")
	if err != nil || !committed {
		t.Fatalf("CommitCode = %v %v", committed, err)
	}
	out, err := git(root, "show", "--name-only", "--format=", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, ".spectackle") {
		t.Fatalf("commit made via the branch-ensure + commit primitives leaked .spectackle: %q", out)
	}
}

// TestMergeMainPreservesSpectackleState: the worktree's uncommitted
// .spectackle files (live replay input) must neither block a merge whose tip
// touches the same paths nor be altered by it (B-0006).
func TestMergeMainPreservesSpectackleState(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "a.go"), []byte("package a\n"), 0o644)
	os.MkdirAll(filepath.Join(root, ".spectackle"), 0o755)
	os.WriteFile(filepath.Join(root, ".spectackle", "journal.ndjson"), []byte("{\"v\":1}\n"), 0o644)
	if err := InitTestRepo(root); err != nil {
		t.Skipf("git unavailable: %v", err)
	}
	wtRoot := filepath.Join(root, ".spectackle", "wt", "T-0001")
	if err := Add(root, wtRoot, "spectackle/T-0001", "HEAD"); err != nil {
		t.Fatal(err)
	}
	// the worktree's live record delta: an uncommitted local journal edit
	local := "{\"v\":1}\n{\"wt-event\":true}\n"
	os.WriteFile(filepath.Join(wtRoot, ".spectackle", "journal.ndjson"), []byte(local), 0o644)
	// main advances, committing a change to the SAME journal file plus code
	os.WriteFile(filepath.Join(root, ".spectackle", "journal.ndjson"), []byte("{\"v\":1}\n{\"main-event\":true}\n"), 0o644)
	os.WriteFile(filepath.Join(root, "b.go"), []byte("package a\n"), 0o644)
	if _, err := git(root, "add", "-A"); err != nil {
		t.Fatal(err)
	}
	if _, err := git(root, "commit", "-m", "advance main incl journal"); err != nil {
		t.Fatal(err)
	}

	conflicts, err := MergeMain(wtRoot)
	if err != nil || len(conflicts) > 0 {
		t.Fatalf("MergeMain must not be blocked by live .spectackle state: %v %v", conflicts, err)
	}
	// the merge landed (code arrived) and the live record bytes survived
	if _, err := os.Stat(filepath.Join(wtRoot, "b.go")); err != nil {
		t.Fatalf("merge did not land: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(wtRoot, ".spectackle", "journal.ndjson"))
	if err != nil || string(got) != local {
		t.Fatalf("live .spectackle bytes not preserved: %q err=%v", got, err)
	}
	if err := FFMain(root, "spectackle/T-0001"); err != nil {
		t.Fatalf("FFMain after preserved merge: %v", err)
	}
}
