package forge

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/jxsl13/spectackle/internal/wt"
)

// Offline implements Forge with no network at all: PRs are tracked
// in-process, and Merge performs a REAL local merge into the repository's
// default branch — a merge commit, never squash, never a bare
// fast-forward — rather than a no-op that silently pretends.
//
// SINCE T-01KYHAH1GJ this type is a TEST DOUBLE only: `git: mode: offline`
// runs commit-only edges and constructs no forge at all, so no production
// path reaches here. It survives because the hermetic online-shape tests
// (Server.forgeOverride + a local bare remote) exercise the full
// branch/PR/merge mapping through it, with no network and no stub server
// standing in for GitHub.
type Offline struct {
	RepoRoot string // repository checkout the merge runs against
	Base     string // default branch merged into

	// StatePath is where the tracked pull requests are persisted, so the
	// lifecycle survives across PROCESSES.
	//
	// In-memory alone is not enough and integration proved it: every
	// `spectackle call` is a fresh process, so a PR opened by the move into
	// active was already forgotten by the move into done, which then reported
	// "no open pr" for a branch it had just created. A resident server would
	// lose it on restart for the same reason. Empty disables persistence,
	// which is what the unit tests use.
	StatePath string

	mu   sync.Mutex
	prs  map[string]*PR // by branch
	next int
}

// offlineState is the on-disk shape. Deliberately a plain file of records
// rather than anything clever: losing it degrades to "no PR known", which the
// callers already handle, so it does not warrant durability machinery.
type offlineState struct {
	Next int   `json:"next"`
	PRs  []*PR `json:"prs"`
}

// load reads StatePath if set. A missing or unreadable file is not an error:
// the caller continues with an empty set, which is the same position it would
// be in on a first run.
func (o *Offline) load() {
	if o.StatePath == "" {
		return
	}
	raw, err := os.ReadFile(o.StatePath)
	if err != nil {
		return
	}
	var st offlineState
	if json.Unmarshal(raw, &st) != nil {
		return
	}
	o.next = st.Next
	for _, pr := range st.PRs {
		o.prs[pr.Branch] = pr
	}
}

// save persists the tracked set. Errors are swallowed for the same reason
// load's are: this is a convenience store for a mode that has no forge, and
// failing a merge because a cache file could not be written would be worse
// than forgetting the pull request.
func (o *Offline) save() {
	if o.StatePath == "" {
		return
	}
	st := offlineState{Next: o.next}
	for _, pr := range o.prs {
		st.PRs = append(st.PRs, pr)
	}
	sort.Slice(st.PRs, func(i, j int) bool { return st.PRs[i].Number < st.PRs[j].Number })
	raw, err := json.Marshal(st)
	if err != nil {
		return
	}
	if os.MkdirAll(filepath.Dir(o.StatePath), 0o755) == nil {
		_ = os.WriteFile(o.StatePath, raw, 0o644)
	}
}

var _ Forge = (*Offline)(nil)

// NewOffline builds an Offline forge that merges into base inside repoRoot.
func NewOffline(repoRoot, base string) *Offline {
	return &Offline{RepoRoot: repoRoot, Base: base, prs: map[string]*PR{}}
}

// NewOfflinePersistent is NewOffline whose tracked pull requests survive
// across processes, which every real caller needs (see Offline.StatePath).
func NewOfflinePersistent(repoRoot, base, statePath string) *Offline {
	o := &Offline{RepoRoot: repoRoot, Base: base, prs: map[string]*PR{}, StatePath: statePath}
	o.load()
	return o
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
	o.save()
	return pr, nil
}

// Ready flips the tracked draft to ready for review. The flip is saved like
// Open's and Merge's mutations are (B-01KYDV): every `spectackle call` is
// its own process, so an unsaved flip is invisible to the next Find, which
// then reports a stale draft and makes the archive path flip (and gate) a
// second time.
func (o *Offline) Ready(pr PR) (PR, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	rec, ok := o.prs[pr.Branch]
	if !ok || rec.Number != pr.Number {
		return PR{}, notFoundErr(pr.Branch, pr.Number)
	}
	rec.Draft = false
	o.save()
	cp := *rec
	return cp, nil
}

// Draft converts the tracked pull request back to draft — the mirror of
// Ready, kept as forge capability (no lifecycle edge calls it since
// PR-DRAFT-001). Saved like every other mutation (B-01KYDV): an unsaved
// flip would be invisible to the next process's Find.
func (o *Offline) Draft(pr PR) (PR, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	rec, ok := o.prs[pr.Branch]
	if !ok || rec.Number != pr.Number {
		return PR{}, notFoundErr(pr.Branch, pr.Number)
	}
	rec.Draft = true
	o.save()
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
	// Identity is inherited from the repository and host config, exactly
	// like every wt commit (B-01KYDK) — a hardcoded override here would
	// stamp the placeholder onto every offline merge commit and bypass the
	// user's signing config. The fallback pair applies only on a host that
	// resolves no identity at all.
	idArgs := []string{}
	if !wt.IdentityConfigured(o.RepoRoot) {
		idArgs = wt.FallbackIdentity
	}
	gitArgs := func(args ...string) []string {
		return append(append([]string{"-C", o.RepoRoot}, idArgs...), args...)
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
	o.save()
	o.mu.Unlock()

	return MergeResult{Merged: true, SHA: strings.TrimSpace(string(sha))}, nil
}

// Checks answers ChecksNone always: offline has no CI, and None is exactly
// the state that tells the caller there is nothing to wait for. Answering
// Passing instead would be a claim about checks that never ran.
func (o *Offline) Checks(pr PR) (CheckState, error) { return ChecksNone, nil }

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
