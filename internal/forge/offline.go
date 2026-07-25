package forge

import (
	"fmt"
	"os/exec"
	"strings"
	"sync"
)

// Offline implements Forge with no network at all: PRs are tracked
// in-process, and Merge performs a REAL local merge into the repository's
// default branch — a merge commit, never squash, never a bare
// fast-forward — rather than a no-op that silently pretends. This is what
// lets the whole item lifecycle run, and the rest of the codebase test
// against a forge, with no network and no stub server standing in for
// GitHub.
type Offline struct {
	RepoRoot string // repository checkout the merge runs against
	Base     string // default branch merged into

	mu   sync.Mutex
	prs  map[string]*PR // by branch
	next int
}

var _ Forge = (*Offline)(nil)

// NewOffline builds an Offline forge that merges into base inside repoRoot.
func NewOffline(repoRoot, base string) *Offline {
	return &Offline{RepoRoot: repoRoot, Base: base, prs: map[string]*PR{}}
}

// Open records a draft PR for branch. Mirrors GitHub's create endpoint,
// which 422s on a duplicate head/base pair: a second Open for a branch
// already tracked is a caller bug (it should have called Find), not a
// silent dedupe, so both implementations answer misuse the same way.
func (o *Offline) Open(branch, base, title, body string) (PR, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.prs == nil {
		o.prs = map[string]*PR{}
	}
	if _, ok := o.prs[branch]; ok {
		return PR{}, fmt.Errorf("forge: offline: PR for %s already open, call Find instead of Open", branch)
	}
	o.next++
	pr := PR{
		Number: o.next,
		Branch: branch,
		Base:   base,
		URL:    fmt.Sprintf("offline://pr/%d", o.next),
		Draft:  true,
	}
	o.prs[branch] = &pr
	return pr, nil
}

// Ready flips the tracked draft to ready for review.
func (o *Offline) Ready(pr PR) (PR, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	rec, ok := o.prs[pr.Branch]
	if !ok || rec.Number != pr.Number {
		return PR{}, notFoundErr(pr.Branch, pr.Number)
	}
	rec.Draft = false
	cp := *rec
	return cp, nil
}

// Merge performs a genuine `git merge --no-ff` of pr.Branch into o.Base
// inside o.RepoRoot, then drops the tracked PR (matching GitHub's own
// pulls API, where a merged PR no longer shows up as open). --no-ff is not
// cosmetic: without it a fast-forwardable branch merges with NO commit at
// all, and the whole point of this offline path is exercising the same
// merge-commit shape (two parents) that merge_method=merge produces on the
// GitHub side — see the test that reads git history rather than trusting
// this return value.
func (o *Offline) Merge(pr PR) (MergeResult, error) {
	o.mu.Lock()
	rec, ok := o.prs[pr.Branch]
	o.mu.Unlock()
	if !ok || rec.Number != pr.Number {
		return MergeResult{}, notFoundErr(pr.Branch, pr.Number)
	}

	base := o.Base
	if base == "" {
		base = "main"
	}
	gitArgs := func(args ...string) []string {
		return append([]string{"-C", o.RepoRoot,
			"-c", "user.name=spectackle", "-c", "user.email=spectackle@localhost"}, args...)
	}
	if out, err := exec.Command("git", gitArgs("checkout", base)...).CombinedOutput(); err != nil {
		return MergeResult{}, fmt.Errorf("forge: offline merge %s: checkout %s: %w: %s", rec.Branch, base, err, strings.TrimSpace(string(out)))
	}

	msg := fmt.Sprintf("Merge pull request #%d from %s", rec.Number, rec.Branch)
	if out, err := exec.Command("git", gitArgs("merge", "--no-ff", "-m", msg, rec.Branch)...).CombinedOutput(); err != nil {
		return MergeResult{}, fmt.Errorf("forge: offline merge %s: %w: %s", rec.Branch, err, strings.TrimSpace(string(out)))
	}

	sha, err := exec.Command("git", gitArgs("rev-parse", "HEAD")...).Output()
	if err != nil {
		return MergeResult{}, fmt.Errorf("forge: offline merge %s: resolve HEAD: %w", rec.Branch, err)
	}

	o.mu.Lock()
	delete(o.prs, pr.Branch)
	o.mu.Unlock()

	return MergeResult{Merged: true, SHA: strings.TrimSpace(string(sha))}, nil
}

// Find returns the tracked PR for branch, if any.
func (o *Offline) Find(branch string) (PR, bool, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	rec, ok := o.prs[branch]
	if !ok {
		return PR{}, false, nil
	}
	return *rec, true, nil
}
