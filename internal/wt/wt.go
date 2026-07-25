// Package wt wraps the git operations behind worktree orchestration. All
// functions shell out to git (worktree/merge plumbing via a library is far
// riskier than the battle-tested CLI); commits are authored as "spectackle"
// so agent commits are attributable.
//
// The invariant the whole merge strategy rests on: CommitCode excludes every
// .spectackle directory, so branches carry CODE ONLY — spec state reaches
// main exclusively through the semantic replay, and .spectackle files are
// never textually merged.
package wt

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func git(dir string, args ...string) (string, error) {
	full := append([]string{"-C", dir,
		"-c", "user.name=spectackle", "-c", "user.email=spectackle@localhost"}, args...)
	out, err := exec.Command("git", full...).CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// CommonRoot resolves the MAIN repository root for a dir that may be inside
// a linked worktree. isWT reports whether dir lives in a linked worktree.
func CommonRoot(dir string) (main string, isWT bool, err error) {
	out, err := git(dir, "rev-parse", "--path-format=absolute", "--show-toplevel", "--git-common-dir")
	if err != nil {
		return "", false, err
	}
	lines := strings.Split(out, "\n")
	if len(lines) < 2 {
		return "", false, fmt.Errorf("wt: unexpected rev-parse output %q", out)
	}
	top := strings.TrimSpace(lines[0])
	common := strings.TrimSpace(lines[1])
	main = filepath.Dir(common) // <main>/.git -> <main>
	return main, !samePath(top, main), nil
}

func samePath(a, b string) bool {
	ra, err1 := filepath.EvalSymlinks(a)
	rb, err2 := filepath.EvalSymlinks(b)
	if err1 != nil || err2 != nil {
		return a == b
	}
	return ra == rb
}

// IsRepo reports whether dir is inside a git repository.
func IsRepo(dir string) bool {
	_, err := git(dir, "rev-parse", "--git-dir")
	return err == nil
}

// Head returns the current commit sha of dir's checkout.
func Head(dir string) (string, error) { return git(dir, "rev-parse", "HEAD") }

// CurrentBranch returns the checked-out branch name of dir.
func CurrentBranch(dir string) (string, error) {
	return git(dir, "rev-parse", "--abbrev-ref", "HEAD")
}

// Add creates a worktree at wtRoot on a fresh branch from startPoint,
// recovering from leftovers of a crashed prior run.
func Add(mainRoot, wtRoot, branch, startPoint string) error {
	_, _ = git(mainRoot, "worktree", "prune")
	if _, err := os.Stat(wtRoot); err == nil {
		if _, err := git(mainRoot, "worktree", "remove", "--force", wtRoot); err != nil {
			_ = os.RemoveAll(wtRoot)
			_, _ = git(mainRoot, "worktree", "prune")
		}
	}
	_, _ = git(mainRoot, "branch", "-D", branch)
	if err := os.MkdirAll(filepath.Dir(wtRoot), 0o755); err != nil {
		return err
	}
	_, err := git(mainRoot, "worktree", "add", "-b", branch, wtRoot, startPoint)
	return err
}

// Remove tears a worktree down (working files are discarded).
func Remove(mainRoot, wtRoot string) error {
	if _, err := git(mainRoot, "worktree", "remove", "--force", wtRoot); err != nil {
		if rmErr := os.RemoveAll(wtRoot); rmErr != nil {
			return err
		}
	}
	_, _ = git(mainRoot, "worktree", "prune")
	return nil
}

// DeleteBranch removes a branch (post-abort cleanup).
func DeleteBranch(mainRoot, branch string) error {
	_, err := git(mainRoot, "branch", "-D", branch)
	return err
}

// codeOnly is the pathspec that keeps every .spectackle dir out of branch
// commits (git >= 2.13 pathspec magic).
var codeOnly = []string{":(exclude).spectackle", ":(exclude)*/.spectackle", ":(exclude)**/.spectackle/**"}

// CommitCode stages and commits everything EXCEPT .spectackle state.
// committed=false means the tree had no code changes (still fine to replay).
func CommitCode(wtRoot, msg string) (bool, error) {
	if _, err := git(wtRoot, append([]string{"add", "-A", "--"}, codeOnly...)...); err != nil {
		return false, err
	}
	// stagedness comes from an exit-code probe, never from parsing porcelain
	// output: the shared git() helper trims the combined output, which eats
	// the significant leading space off the first status line and misread an
	// unstaged-only .spectackle change as staged (B-0005).
	staged := false
	if _, err := git(wtRoot, "diff", "--cached", "--quiet"); err != nil {
		staged = true
	}
	if !staged && !MergeInProgress(wtRoot) {
		return false, nil
	}
	if _, err := git(wtRoot, "commit", "-m", msg); err != nil {
		return false, err
	}
	return true, nil
}

// MergeMain merges the primary checkout's current branch INTO the worktree
// branch. On conflict the merge is left in progress (resumable) and the
// conflicted files are returned. The target is resolved, never assumed: a
// hardcoded "main" silently merged a stale ref in repos developing on a
// differently named branch, and the submit then died at the fast-forward
// with a diverging-branches error (B-0004).
func MergeMain(wtRoot string) (conflicts []string, err error) {
	target := "main"
	if mainRoot, isWT, crErr := CommonRoot(wtRoot); crErr == nil && isWT {
		if b, brErr := git(mainRoot, "symbolic-ref", "--short", "HEAD"); brErr == nil && b != "" {
			target = b
		} else if sha, shErr := git(mainRoot, "rev-parse", "HEAD"); shErr == nil && sha != "" {
			target = sha // detached primary checkout: merge the commit itself
		}
	}
	// The worktree's .spectackle files are live replay input, deliberately
	// uncommitted (copyBundles seeds them, CommitCode excludes them) — yet
	// they sit in git's working tree, so a merge whose tip touches the same
	// paths refuses to proceed (B-0006). Preserve their exact bytes, clear
	// them for the merge, and put them back: replay stays the sole owner of
	// record-state reconciliation, git never sees the dirt.
	if !MergeInProgress(wtRoot) {
		restore, perr := preserveSpectackle(wtRoot)
		if perr != nil {
			return nil, perr
		}
		defer func() {
			if rerr := restore(); rerr != nil && err == nil {
				conflicts, err = nil, rerr
			}
		}()
	}
	if _, err := git(wtRoot, "merge", "--no-edit", target); err != nil {
		out, lsErr := git(wtRoot, "diff", "--name-only", "--diff-filter=U")
		if lsErr == nil && out != "" {
			return strings.Split(out, "\n"), nil
		}
		return nil, err
	}
	return nil, nil
}

// preserveSpectackle saves the byte content of every working-tree-modified
// (tracked) and untracked path under a .spectackle dir, clears them so a
// merge can run against a clean tree, and returns a restore that writes the
// exact bytes back. See MergeMain (B-0006).
func preserveSpectackle(wtRoot string) (restore func() error, err error) {
	inBundle := func(p string) bool {
		return p != "" && strings.Contains("/"+filepath.ToSlash(p), "/.spectackle/")
	}
	saved := map[string][]byte{}
	var tracked []string
	if out, dErr := git(wtRoot, "diff", "--name-only"); dErr == nil {
		for _, p := range strings.Split(out, "\n") {
			if inBundle(p) {
				tracked = append(tracked, p)
			}
		}
	}
	var untracked []string
	if out, uErr := git(wtRoot, "ls-files", "--others", "--exclude-standard"); uErr == nil {
		for _, p := range strings.Split(out, "\n") {
			if inBundle(p) {
				untracked = append(untracked, p)
			}
		}
	}
	for _, p := range append(append([]string{}, tracked...), untracked...) {
		b, rErr := os.ReadFile(filepath.Join(wtRoot, p))
		if rErr != nil {
			return nil, rErr
		}
		saved[p] = b
	}
	if len(tracked) > 0 {
		if _, cErr := git(wtRoot, append([]string{"checkout", "--"}, tracked...)...); cErr != nil {
			return nil, cErr
		}
	}
	for _, p := range untracked {
		if rmErr := os.Remove(filepath.Join(wtRoot, p)); rmErr != nil {
			return nil, rmErr
		}
	}
	return func() error {
		for p, b := range saved {
			abs := filepath.Join(wtRoot, p)
			if mkErr := os.MkdirAll(filepath.Dir(abs), 0o755); mkErr != nil {
				return mkErr
			}
			if wErr := os.WriteFile(abs, b, 0o644); wErr != nil {
				return wErr
			}
		}
		return nil
	}, nil
}

// MergeInProgress reports whether a conflicted merge awaits resolution.
func MergeInProgress(wtRoot string) bool {
	gitDir, err := git(wtRoot, "rev-parse", "--git-dir")
	if err != nil {
		return false
	}
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(wtRoot, gitDir)
	}
	_, statErr := os.Stat(filepath.Join(gitDir, "MERGE_HEAD"))
	return statErr == nil
}

// FFMain fast-forwards main to the branch. Under the integrate lock (main
// was just merged into the branch) this cannot conflict; git still aborts
// safely if the user's main checkout is dirty on touched files.
func FFMain(mainRoot, branch string) error {
	_, err := git(mainRoot, "merge", "--ff-only", branch)
	return err
}

// ShowFile reads a file's content at a ref ("" content for a missing path —
// the delta baseline for journals that did not exist at branch point).
func ShowFile(mainRoot, ref, relPath string) ([]byte, error) {
	out, err := exec.Command("git", "-C", mainRoot, "show", ref+":"+relPath).Output()
	if err != nil {
		return nil, nil // missing at ref = empty baseline
	}
	return out, nil
}

// TouchedFiles lists the files a branch changed relative to a base commit.
func TouchedFiles(mainRoot, base, branch string) ([]string, error) {
	out, err := git(mainRoot, "diff", "--name-only", base, branch)
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}

// DirtyOverlap returns uncommitted main-checkout paths that intersect files.
func DirtyOverlap(mainRoot string, files []string) []string {
	out, err := git(mainRoot, "status", "--porcelain")
	if err != nil || out == "" {
		return nil
	}
	dirty := map[string]bool{}
	for _, l := range strings.Split(out, "\n") {
		if len(l) > 3 {
			dirty[strings.TrimSpace(l[3:])] = true
		}
	}
	var overlap []string
	for _, f := range files {
		if dirty[f] {
			overlap = append(overlap, f)
		}
	}
	return overlap
}

// InitTestRepo creates a git repo with an initial commit on branch main —
// used by tests and nowhere else.
func InitTestRepo(dir string) error {
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"add", "-A"},
		{"commit", "--allow-empty", "-m", "init"},
	} {
		if _, err := git(dir, args...); err != nil {
			return err
		}
	}
	return nil
}
