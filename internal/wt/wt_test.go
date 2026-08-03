package wt

import (
	"io/fs"
	"os"
	"os/exec"
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
	if err := Add(root, wtRoot, "spectackle/T-0001", "HEAD", "main", false); err != nil {
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
	if err := Add(root, wtRoot, "spectackle/T-0001", "HEAD", "main", false); err != nil {
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
	if err := Add(root, wtRoot, "spectackle/T-0002", "HEAD", "main", false); err != nil {
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
	if err := Add(root, wtRoot, "spectackle/T-0001", "HEAD", "main", false); err != nil {
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
	// A bare remote is written into by the Push below, and a receiving repo
	// runs its own auto-maintenance (B-01KYQA4WXEFAT).
	if err := QuietMaintenance(bareDir); err != nil {
		t.Fatal(err)
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
	if err := Add(root, wtRoot, "spectackle/T-0001", "HEAD", "main", false); err != nil {
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

// TestCommitInheritsRepoIdentity pins B-01KYDK's core: an automation commit
// carries the identity the repository's config declares — never the old
// hardcoded spectackle@localhost override, which misattributed every commit
// and made signing structurally impossible. GIT_CONFIG_GLOBAL/SYSTEM are
// pointed at /dev/null so the assertion cannot pass by inheriting the
// host's config instead of the fixture's.
func TestCommitInheritsRepoIdentity(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_SYSTEM", "/dev/null")
	root := repo(t) // InitTestRepo sets spectackle-test <test@spectackle.local>

	if !IdentityConfigured(root) {
		t.Fatal("fixture repo must report a configured identity")
	}
	if err := os.WriteFile(filepath.Join(root, "b.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := CommitCode(root, "identity probe"); err != nil {
		t.Fatalf("CommitCode: %v", err)
	}
	got, err := git(root, "log", "-1", "--format=%an <%ae> / %cn <%ce>")
	if err != nil {
		t.Fatal(err)
	}
	want := "spectackle-test <test@spectackle.local> / spectackle-test <test@spectackle.local>"
	if got != want {
		t.Fatalf("commit identity = %q, want %q", got, want)
	}
}

// TestCommitFallbackOnBareHost: with no identity in ANY config scope the
// commit still succeeds, carrying the placeholder — automation on a bare CI
// container must degrade, not die at tell-me-who-you-are. IdentityConfigured
// must say false so the transition layer can report the fallback loudly.
func TestCommitFallbackOnBareHost(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_SYSTEM", "/dev/null")
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A bare repo: no InitTestRepo (it would configure an identity). The
	// init commit itself needs the fallback too.
	if _, err := git(root, "init", "-b", "main"); err != nil {
		t.Skipf("git unavailable: %v", err)
	}
	// InitTestRepo is deliberately not used here, so its maintenance
	// suppression has to be spelled out — this fixture commits, and a
	// committing fixture spawns the detached child (B-01KYQA4WXEFAT).
	if err := QuietMaintenance(root); err != nil {
		t.Fatal(err)
	}
	if IdentityConfigured(root) {
		t.Skip("host leaks an identity despite GIT_CONFIG_GLOBAL/SYSTEM=/dev/null; cannot exercise the bare path")
	}
	if _, err := git(root, "add", "-A"); err != nil {
		t.Fatal(err)
	}
	if _, err := CommitCode(root, "bare host probe"); err != nil {
		t.Fatalf("CommitCode on a bare host: %v", err)
	}
	got, err := git(root, "log", "-1", "--format=%an <%ae>")
	if err != nil {
		t.Fatal(err)
	}
	if got != "spectackle <spectackle@localhost>" {
		t.Fatalf("fallback identity = %q, want the documented placeholder", got)
	}
}

// TestSnapshotSpectackleSkipsSiblingsAndRuntime pins the second adversarial
// finding on B-01KYED3D: the vacate snapshot must capture the MAIN
// checkout's records only — never sibling worktree trees (linked worktrees
// carry a .git FILE the name-based skip misses) and never the shared
// runtime state under .spectackle/cache (a live multi-process SQLite WAL) —
// because restore() rewrites everything captured, silently reverting any
// sibling write that landed during the checkout window.
func TestSnapshotSpectackleSkipsSiblingsAndRuntime(t *testing.T) {
	root := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(".spectackle/journal.ndjson", "own-v1")
	write(".spectackle/wt/sib/.git", "gitdir: elsewhere") // linked-worktree marker FILE
	write(".spectackle/wt/sib/.spectackle/journal.ndjson", "sib-v1")
	write(".spectackle/cache/coord.db", "db-v1")
	write("api/.spectackle/spec.md", "spec-v1")

	restore, err := snapshotSpectackle(root)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate the checkout window: own records rewound, sibling and
	// runtime state legitimately advanced by other processes.
	write(".spectackle/journal.ndjson", "own-REWOUND")
	write(".spectackle/wt/sib/.spectackle/journal.ndjson", "sib-v2")
	write(".spectackle/cache/coord.db", "db-v2")
	write("api/.spectackle/spec.md", "spec-REWOUND")
	if err := restore(); err != nil {
		t.Fatal(err)
	}

	read := func(rel string) string {
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	if read(".spectackle/journal.ndjson") != "own-v1" || read("api/.spectackle/spec.md") != "spec-v1" {
		t.Fatal("own records not restored")
	}
	if read(".spectackle/wt/sib/.spectackle/journal.ndjson") != "sib-v2" {
		t.Fatal("sibling worktree write silently reverted by restore")
	}
	if read(".spectackle/cache/coord.db") != "db-v2" {
		t.Fatal("live runtime state rewritten by restore")
	}
}

// TestVacateRefusesItemNamespaceBase pins the third adversarial finding: a
// configured base that is itself an item branch (auto-filled from a main
// checkout parked on spectackle/X by the very bug this fix repairs) must
// never become the vacate target — the later submit would merge into and
// fast-forward the SIBLING item's ref while claiming main.
func TestVacateRefusesItemNamespaceBase(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := InitTestRepo(root); err != nil {
		t.Skipf("git unavailable: %v", err)
	}
	mustGit := func(args ...string) {
		if _, err := git(root, args...); err != nil {
			t.Fatal(err)
		}
	}
	mustGit("branch", "spectackle/X")
	mustGit("checkout", "-b", "spectackle/Y")

	if err := vacateBranch(root, "spectackle/Y", "spectackle/X"); err != nil {
		t.Fatal(err)
	}
	cur, err := CurrentBranch(root)
	if err != nil {
		t.Fatal(err)
	}
	if cur == "spectackle/X" {
		t.Fatal("vacate landed on a sibling item branch")
	}
	if cur != "main" {
		t.Fatalf("vacate landed on %q, want main", cur)
	}
}

// Stray-directory guard (B-01KYHHCFCW): a leftover at the target path with
// no ledger row may hold a crashed agent's work — Add refuses unless the
// stray is empty, records-only, or force is passed.
func TestAddStrayGuard(t *testing.T) {
	root := repo(t)
	wtRoot := filepath.Join(root, ".spectackle", "wt", "T-0009")

	// empty stray clears silently
	if err := os.MkdirAll(wtRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Add(root, wtRoot, "spectackle/T-0009", "HEAD", "main", false); err != nil {
		t.Fatalf("empty stray must clear: %v", err)
	}
	// a real worktree with an uncommitted agent file: orphan it (drop the
	// ledgerless dir marker path by removing the admin link is overkill —
	// Add's stray arm triggers on ANY existing dir, worktree or not), then
	// re-Add without force must refuse naming the path
	precious := filepath.Join(wtRoot, "precious.go")
	if err := os.WriteFile(precious, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := Add(root, wtRoot, "spectackle/T-0009", "HEAD", "main", false)
	if err == nil || !strings.Contains(err.Error(), "uncommitted") || !strings.Contains(err.Error(), wtRoot) {
		t.Fatalf("dirty stray worktree must refuse naming the path: %v", err)
	}
	if _, serr := os.Stat(precious); serr != nil {
		t.Fatal("refused Add must not touch the stray's files")
	}
	// force clears deliberately
	if err := Add(root, wtRoot, "spectackle/T-0009", "HEAD", "main", true); err != nil {
		t.Fatalf("forced Add must clear the stray: %v", err)
	}
	if _, serr := os.Stat(precious); serr == nil {
		t.Fatal("force must actually discard the stray's files")
	}
	Remove(root, wtRoot)
}

func TestAddStrayNonWorktreeDir(t *testing.T) {
	root := repo(t)
	wtRoot := filepath.Join(root, ".spectackle", "wt", "T-0010")

	// records-only plain dir clears silently
	if err := os.MkdirAll(filepath.Join(wtRoot, dotDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Add(root, wtRoot, "spectackle/T-0010", "HEAD", "main", false); err != nil {
		t.Fatalf("records-only stray must clear: %v", err)
	}
	Remove(root, wtRoot)

	// plain dir with an agent file refuses without force
	if err := os.MkdirAll(wtRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wtRoot, "notes.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := Add(root, wtRoot, "spectackle/T-0010", "HEAD", "main", false)
	if err == nil || !strings.Contains(err.Error(), "not a worktree") {
		t.Fatalf("non-worktree stray with files must refuse: %v", err)
	}
	if err := Add(root, wtRoot, "spectackle/T-0010", "HEAD", "main", true); err != nil {
		t.Fatalf("forced Add must clear: %v", err)
	}
	Remove(root, wtRoot)
}

// Self-collision guard (B-01KYHQ8A7N): Add with wtRoot resolving to the
// main checkout — directly, via symlink, or any path whose .git is a
// directory — refuses before ANY removal, force notwithstanding. The
// pre-guard code empirically deleted an entire main checkout here.
func TestAddRefusesSelfCollision(t *testing.T) {
	root := repo(t)
	sentinel := filepath.Join(root, "untracked-note.txt")
	if err := os.WriteFile(sentinel, []byte("must survive"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, force := range []bool{false, true} {
		err := Add(root, root, "spectackle/T-0011", "HEAD", "main", force)
		if err == nil || !strings.Contains(err.Error(), "main checkout") {
			t.Fatalf("force=%v: self-collision must refuse naming the collision: %v", force, err)
		}
	}
	// symlink alias to the main checkout
	link := filepath.Join(filepath.Dir(root), filepath.Base(root)+"-alias")
	if err := os.Symlink(root, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	defer os.Remove(link)
	if err := Add(root, link, "spectackle/T-0011", "HEAD", "main", true); err == nil {
		t.Fatal("symlinked self-collision must refuse")
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Fatal("untracked file destroyed by a refused Add")
	}
	if _, err := os.Stat(filepath.Join(root, ".git")); err != nil {
		t.Fatal("the main checkout's .git was destroyed")
	}
}

// A FULL secondary checkout (its .git is a directory) at the target path is
// never cleared either — only linked worktrees (.git file) and plain strays
// are Add's to manage.
func TestAddRefusesFullCheckoutAtTarget(t *testing.T) {
	root := repo(t)
	second := repo(t) // an unrelated full checkout
	err := Add(root, second, "spectackle/T-0012", "HEAD", "main", true)
	if err == nil || !strings.Contains(err.Error(), ".git is a directory") {
		t.Fatalf("full checkout at target must refuse: %v", err)
	}
	if _, serr := os.Stat(filepath.Join(second, ".git")); serr != nil {
		t.Fatal("the second checkout was destroyed")
	}
}

// --- B-01KYQA4WXEFAT: fixture repositories must schedule no background work --

// The knob assertion. A fixture repo is created inside a t.TempDir that the
// harness unlinks the instant the test returns, so anything the repo has
// scheduled to run later races that removal — and the race surfaces as a
// cleanup error (`TempDir RemoveAll cleanup: unlinkat .../.git/objects:
// directory not empty`) that no amount of reading the test body explains.
//
// `git config --get` exits 1 for an unset key, so on a fixture built before
// this fix both lookups fail outright rather than returning a wrong value.
func TestInitTestRepoDisablesBackgroundMaintenance(t *testing.T) {
	root := repo(t)
	for _, tc := range []struct{ key, want string }{
		{"maintenance.auto", "false"},
		{"gc.auto", "0"},
	} {
		got, err := git(root, "config", "--get", tc.key)
		if err != nil {
			t.Errorf("git config --get %s: %v — an unset key exits 1, so this fixture never disabled it", tc.key, err)
			continue
		}
		if got != tc.want {
			t.Errorf("git config --get %s = %q, want %q", tc.key, got, tc.want)
		}
	}
}

// The MECHANISM assertion, and the one that actually matters: no detached
// child, however git spells the knob that suppresses it. Pinning the trace
// rather than the config means a future git renaming or reorganizing
// maintenance.auto turns this red — a knob assertion alone would keep passing
// while the child came back.
//
// Measured on git 2.50.1: three `maintenance run --auto` dispatches per
// fixture commit before the fix, zero after.
func TestFixtureCommitSpawnsNoDetachedMaintenance(t *testing.T) {
	root := repo(t)
	cmd := exec.Command("git", "-C", root, "commit", "-q", "--allow-empty", "-m", "trace probe")
	cmd.Env = append(os.Environ(), "GIT_TRACE=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("fixture commit: %v\n%s", err, out)
	}
	trace := string(out)
	if !strings.Contains(trace, "trace: ") {
		// A git built without trace support, or one that routes GIT_TRACE
		// somewhere else, cannot answer the question. Skipping is honest;
		// reading "no matches" off empty output would be a test that passes
		// because it saw nothing.
		t.Skipf("this git emits no GIT_TRACE output, so the dispatch cannot be observed:\n%s", trace)
	}
	if n := strings.Count(trace, "maintenance run --auto"); n != 0 {
		t.Fatalf("a fixture commit dispatched %d detached maintenance child(ren); each one outlives the commit and races t.TempDir's removal:\n%s", n, trace)
	}
}

// The enumeration guard the record's VERIFY line asks for: "the helper must
// be used by every test that runs git in a temp dir — enumerate them rather
// than fixing the two known ones". Two packages were hit in CI; the argument
// for a shared helper is exactly that there was never any reason to expect
// only those two, and a sweep with no guard rots at the next new test file.
//
// Deliberately crude, because crude is what survives: any test file that
// spells a git `"init"` argument must also reach one of the helpers that
// disables maintenance. It is FILE-scoped, so a file that already uses a
// helper can still smuggle in a second hand-built fixture — tightening that
// would mean parsing, and a parser is a thing that breaks quietly. The
// allowlist below is the escape hatch, and it is deliberately a source edit
// so that an exception is a decision somebody made on purpose.
func TestEveryGitFixtureDisablesMaintenance(t *testing.T) {
	root := moduleRoot(t)
	// EMPTY, ON PURPOSE — every git fixture in this module is swept. An entry
	// here is a promise that the file's fixture cannot race a temp-dir
	// removal (it owns its directory, or never commits into one), and the
	// reason belongs in the value.
	allow := map[string]string{}
	guards := []string{"wttest.Repo(", "InitTestRepo(", "QuietMaintenance("}

	var scanned, fixtures int
	var unguarded []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// Dot directories are skipped WHOLESALE: they carry no Go source
			// this module builds, and .spectackle in particular is
			// server-owned — nothing in this repository reads it off disk.
			if path != root && strings.HasPrefix(d.Name(), ".") {
				return fs.SkipDir
			}
			return nil
		}
		// EVERY .go file, not just _test.go. Scoping this to tests is what
		// let B-01KYQA4WXEFAT's first landing be refuted: bench.Fixture is
		// ORDINARY CODE that tests call, so a *_test.go walk could not see
		// it, and a PATH shim over the whole suite measured 283 detached
		// maintenance children still spawning through that one function while
		// this guard reported green.
		if !strings.HasSuffix(d.Name(), ".go") {
			return nil
		}
		scanned++
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		src, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		// The `"init"` literal alone is not a git fixture: it also appears as
		// an Objective-C selector (internal/langspec/objc.go) and as a corpus
		// word (poc/wasmparse). Require a git invocation in the same file —
		// either the literal command or this package's git() wrapper — so the
		// guard means "git init" rather than "the string init".
		text := string(src)
		if !strings.Contains(text, `"init"`) {
			return nil
		}
		if !strings.Contains(text, `"git"`) && !strings.Contains(text, "git(") {
			return nil
		}
		fixtures++
		if _, ok := allow[rel]; ok {
			return nil
		}
		for _, g := range guards {
			if strings.Contains(string(src), g) {
				return nil
			}
		}
		unguarded = append(unguarded, rel)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// A walk that found nothing would pass silently and guard nothing, which
	// is the failure mode of every enumeration test ever written.
	if scanned < 20 || fixtures == 0 {
		t.Fatalf("the walk is broken, not the tree: %d test files scanned, %d git fixtures found under %s", scanned, fixtures, root)
	}
	if len(unguarded) > 0 {
		t.Fatalf("%d test file(s) build a git fixture without disabling background maintenance (B-01KYQA4WXEFAT): %s\n"+
			"use wttest.Repo, or call wt.QuietMaintenance on the repo right after init and before any commit",
			len(unguarded), strings.Join(unguarded, ", "))
	}
}

// moduleRoot walks up from the test's working directory to the directory
// holding go.mod. Computed rather than hardcoded as "../.." so the guard
// keeps working if this package moves.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the test's working directory")
		}
		dir = parent
	}
}
