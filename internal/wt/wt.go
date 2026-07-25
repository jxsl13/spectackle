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
	"strconv"
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

// DefaultBranch resolves the branch checked out at dir via HEAD's symbolic
// ref rather than CurrentBranch's rev-parse: workspace.Config.Git.Base reads
// this at config-load time, which can run before the repo has a single
// commit yet (rev-parse --abbrev-ref HEAD errors on an unborn HEAD;
// symbolic-ref resolves regardless) — and it exists at all so that default
// never has to be a hardcoded "main" (see MergeMain's B-0004 fix for the same
// principle applied to merge targets).
func DefaultBranch(dir string) (string, error) {
	// The remote's HEAD is the repository's actual default branch, and it is
	// what a forge compares a pull request against.
	//
	// `symbolic-ref --short HEAD` — the obvious-looking call, and what this
	// used to do — returns the CURRENTLY CHECKED OUT branch instead. That is a
	// different thing entirely, and it read as correct for as long as nobody
	// called it from a branch: driven from a task branch it returned the task
	// branch, so the base a pull request was opened against was the head.
	if out, err := git(dir, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil {
		if _, branch, ok := strings.Cut(strings.TrimSpace(out), "/"); ok && branch != "" {
			return branch, nil
		}
	}
	// No remote HEAD recorded (a repository created locally rather than
	// cloned). Fall back to the checked-out branch, which is right often
	// enough for a fresh repo and is what the caller had before.
	return git(dir, "symbolic-ref", "--short", "HEAD")
}

// HeadSHA resolves a local branch to its commit. The gitflow reads it right
// after pushing, so the CI await polls the exact commit that was pushed
// rather than a branch ref the forge may briefly resolve to the predecessor.
func HeadSHA(dir, branch string) (string, error) {
	return git(dir, "rev-parse", "--verify", branch)
}

// RemoteURL returns the configured URL of a named remote.
//
// It exists so the forge client can derive owner and repository from the same
// remote git itself pushes to, rather than from a second source that could
// disagree with it — a workspace whose remote was re-pointed must open pull
// requests against the repository it actually pushes to.
func RemoteURL(dir, remote string) (string, error) {
	return git(dir, "remote", "get-url", remote)
}

// IsAheadOf reports whether branch carries commits base does not have.
//
// It answers the question a forge asks before accepting a pull request: is
// there anything here to review? An unknown base (never fetched, or a fresh
// repository) counts as not-ahead rather than an error — the caller's response
// either way is to wait rather than to complain.
func IsAheadOf(dir, branch, base string) (bool, error) {
	out, err := git(dir, "rev-list", "--count", branch, "^"+base)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "0", nil
}

// IsAheadOfRemote is IsAheadOf against the REMOTE-tracking ref for base,
// falling back to the local ref when there is no remote-tracking one.
//
// This is the question a forge actually answers before accepting a pull
// request, and the local ref is the wrong oracle for it: in a worktree the
// local base branch is often stale — the primary checkout owns it and it is
// not updated by fetching — so a task branch can read as fifty commits ahead
// of a local base while the forge, comparing against its own current base,
// sees no commits at all and refuses the pull request. Asking the
// remote-tracking ref asks what the forge will say.
func IsAheadOfRemote(dir, branch, remote, base string) (bool, error) {
	if _, err := git(dir, "rev-parse", "--verify", "--quiet", remote+"/"+base); err == nil {
		return IsAheadOf(dir, branch, remote+"/"+base)
	}
	return IsAheadOf(dir, branch, base)
}

// EnsureBranch makes branch the checked-out branch of dir: creates it from
// startPoint if it doesn't exist yet, otherwise just checks it out. A retried
// transition (e.g. a task re-entering active after a reopen) must land on the
// SAME branch it used before, not fail because that branch is already there —
// so the second call on an existing branch is an ordinary checkout, never an
// error.
func EnsureBranch(dir, branch, startPoint string) error {
	if _, err := git(dir, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch); err != nil {
		// An empty startPoint means "branch from where we are". It must be
		// OMITTED rather than passed through: git reads an empty argument as a
		// pathspec and fails with "empty string is not a valid pathspec",
		// which is a confusing way to report a caller that simply had no
		// specific start point in mind.
		args := []string{"checkout", "-b", branch}
		if startPoint != "" {
			args = append(args, startPoint)
		}
		_, err := git(dir, args...)
		return err
	}
	_, err := git(dir, "checkout", branch)
	return err
}

// Push pushes branch to remote, setting upstream tracking so HasUnpushedCommits
// can later read it back. Safe to call on every submit round, not just the
// first: -u on an already-tracked branch just re-affirms the tracking config,
// it does not error.
func Push(dir, remote, branch string) error {
	_, err := git(dir, "push", "-u", remote, branch)
	return err
}

// HasUnpushedCommits reports whether branch carries commits remote does not
// have yet. Before the first Push there is no remote-tracking ref to compare
// against at all, so every local commit trivially counts as unpushed; after
// Push lands them, the comparison is a straight rev-list.
func HasUnpushedCommits(dir, remote, branch string) (bool, error) {
	if _, err := git(dir, "rev-parse", "--verify", "--quiet", remote+"/"+branch); err != nil {
		return true, nil
	}
	out, err := git(dir, "rev-list", "--count", branch, "^"+remote+"/"+branch)
	if err != nil {
		return false, err
	}
	n, convErr := strconv.Atoi(out)
	if convErr != nil {
		return false, fmt.Errorf("wt: unexpected rev-list output %q", out)
	}
	return n > 0, nil
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

// CommitRecords stages and commits ONLY .spectackle record state, the exact
// complement of CommitCode's pathspec split. committed=false means no record
// file was dirty.
//
// It exists because the split left a hole: CommitCode runs mechanically on
// every transition and excludes .spectackle (B-0006), so after a full
// lifecycle the record delta sat uncommitted in the checkout with nobody
// mechanical responsible for it — the one thing the always-pushed policy
// forbids. This is the other half of that responsibility.
//
// The files are enumerated first and then added AND committed by explicit
// path list. That is not a style choice: commit-by-explicit-pathspec is what
// guarantees a user's concurrently staged unrelated work is never swept into
// a records commit, and enumerating first avoids the pathspec-did-not-match
// failure that a blind glob add hits in a workspace with no nested context
// dirs. ls-files --others lists every untracked file individually, so a
// brand-new context dir cannot be collapsed into one directory entry the
// filter never sees.
func CommitRecords(wtRoot, msg string) (bool, error) {
	// Enumerated via diff --name-only (tracked modifications) plus ls-files
	// --others (untracked), NOT via status --porcelain. Porcelain's leading
	// XY column is positional, and the git() helper trims the whole output —
	// which eats the leading space of the FIRST line only, so a tracked
	// modification that happened to sort first parsed as a path missing its
	// leading dot and silently failed the .spectackle filter. Found live: a
	// dirty config.yaml survived every records commit of a full loop while
	// the untracked files beside it were committed. Name-only outputs carry
	// no columns, so there is nothing for the trim to corrupt.
	tracked, err := git(wtRoot, "diff", "--name-only")
	if err != nil {
		return false, err
	}
	untracked, err := git(wtRoot, "ls-files", "--others", "--exclude-standard")
	if err != nil {
		return false, err
	}
	var files []string
	for _, p := range strings.Split(tracked+"\n"+untracked, "\n") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if p == ".spectackle" || strings.HasPrefix(p, ".spectackle/") || strings.Contains(p, "/.spectackle/") {
			files = append(files, p)
		}
	}
	if len(files) == 0 {
		return false, nil
	}
	if _, err := git(wtRoot, append([]string{"add", "--"}, files...)...); err != nil {
		return false, err
	}
	if _, err := git(wtRoot, append([]string{"commit", "-m", msg, "--"}, files...)...); err != nil {
		return false, err
	}
	return true, nil
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
