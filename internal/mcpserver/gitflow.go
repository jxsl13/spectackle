package mcpserver

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/jxsl13/spectackle/internal/forge"
	"github.com/jxsl13/spectackle/internal/item"
	"github.com/jxsl13/spectackle/internal/workspace"
	"github.com/jxsl13/spectackle/internal/wt"
)

// The git workflow, driven by the state machine instead of by the LLM
// (P-01KYDB, ADR-01KYDB).
//
// The mapping is the whole feature:
//
//	into active   ensure the task branch, commit code, push, open a DRAFT PR
//	while active  commit and push, so no change is ever only local
//	into done     flip the PR to ready for review
//	into archived merge, merge commit, never squash
//
// Every step is a no-op when it has already happened. That is not politeness:
// tool calls retry and agents die mid-sequence, so a mapping that only works
// on a clean first run produces duplicate pull requests in ordinary use. Each
// step below states how it is made repeatable.
//
// WHAT THIS DOES NOT DO, and the reason an earlier version of this idea was
// rejected: it commits and pushes CODE. Record state reaches the base branch
// through the semantic replay (SPX-SWM-001), never through a git merge of
// journal files — wt.CommitCode excludes .spectackle deliberately, and
// preserveSpectackle exists because concurrent journal writes were a real
// observed defect (B-0006). Nothing here git-merges .spectackle between
// branches, and nothing here should be changed to.

// gitFlowResult is what a transition's git work produced, rendered onto the
// tool result as records. Failures land here as notes rather than as a failed
// transition: the lifecycle state is the server's own, and a forge that is
// unreachable must not be able to prevent an item from moving.
//
// The one thing it will not do is claim success it did not have — an
// unreachable forge produces a visible note, never silence.
type gitFlowResult struct {
	lines []string
}

// errNoRemote marks "this workspace has no remote to talk to", which is a
// reason to do nothing quietly rather than a failure to report. Callers test
// for it before adding a note.
var errNoRemote = errors.New("mcpserver: no git remote configured")

func (r *gitFlowResult) addf(format string, a ...any) {
	r.lines = append(r.lines, fmt.Sprintf(format, a...))
}

func (r *gitFlowResult) String() string {
	if len(r.lines) == 0 {
		return ""
	}
	return strings.Join(r.lines, "\n") + "\n"
}

// gitEnabled reports whether the automation should run at all for this
// workspace. Two separate reasons to stay out of the way:
//
//   - Explicitly disabled in config. This restores today's behavior exactly,
//     which is the assertion protecting every existing user on upgrade.
//   - The workspace is not a git repository. Then the automation is not
//     failing, it is simply not applicable, and it must be SILENT rather than
//     warn. Warning here would put a "! GIT W ... not a git repository" line
//     on every single move in every non-git workspace — which is both the
//     "degrade quietly" requirement of the config task and the output diet
//     (SPX-ARC-002), and is exactly what the first wiring of this did.
//
// Note s.ws, not s.main: during `work` the active root is the task worktree,
// and that is the tree these primitives operate on.
func (s *Server) gitEnabled() bool {
	return s.main.Cfg.Git.IsEnabled() && wt.IsRepo(s.ws.Dir)
}

// taskBranch is the branch a task's work lives on. It mirrors the name the
// swarm's worktrees already use, so the PR boundary follows the branch
// boundary that exists rather than inventing a second naming scheme.
func taskBranch(id string) string { return "spectackle/" + id }

// forgeFor builds the client for the configured mode.
//
// Offline is a real implementation, not a stub: it performs the same sequence
// against the local repository and merges into the base branch with a merge
// commit. That is what lets the whole lifecycle run with no network, and what
// lets the tests exercise the mapping without standing up a forge.
func (s *Server) forgeFor() (forge.Forge, error) {
	cfg := s.main.Cfg.Git
	if cfg.Mode == "offline" {
		base := cfg.Base
		if base == "" {
			base = "main"
		}
		// Persistent, not the in-memory constructor: each tool call is its own
		// process, so a PR opened by the move into active must still be there
		// for the move into done. The cache dir is the right home — server
		// owned, gitignored, and losing it degrades to "no PR known", which
		// every caller already handles.
		return forge.NewOfflinePersistent(s.main.Dir, base,
			filepath.Join(s.main.CacheDir(), "forge-offline.json")), nil
	}
	remote, err := wt.RemoteURL(s.main.Dir, cfg.Remote)
	if err != nil {
		// No such remote: a git repository that was never given one. Same
		// judgment as gitEnabled — not applicable rather than broken, so the
		// caller stays silent instead of warning on every transition.
		return nil, errNoRemote
	}
	return forge.NewGitHub(remote, nil)
}

// gitFlowStart runs the into-active half: branch, commit, push, draft PR.
//
// Idempotent at every step. EnsureBranch checks out an existing branch instead
// of failing; CommitCode reports whether it had anything to commit; Push is
// safe to repeat; and the PR is looked up with Find before Open, because Open
// deliberately does not dedupe (the create endpoint 422s on a duplicate
// head/base pair, and silently returning the existing PR would hide a caller
// bug).
func (s *Server) gitFlowStart(it item.Item) *gitFlowResult {
	res := &gitFlowResult{}
	if !s.gitEnabled() {
		return res
	}
	branch := taskBranch(it.ID)
	dir := s.ws.Dir

	if err := wt.EnsureBranch(dir, branch, ""); err != nil {
		res.addf("! GIT W %s branch: %s", it.ID, err)
		return res
	}
	if _, err := wt.CommitCode(dir, "spectackle "+it.ID+": "+it.Title); err != nil {
		res.addf("! GIT W %s commit: %s", it.ID, err)
	}
	if err := s.gitPush(dir, branch); err != nil {
		res.addf("! GIT W %s push: %s", it.ID, err)
		return res
	}
	if s.main.Cfg.Git.Mode == "offline" {
		res.addf("g branch %s", branch)
	} else {
		res.addf("g branch %s pushed", branch)
	}

	f, err := s.forgeFor()
	if err != nil {
		if !errors.Is(err, errNoRemote) {
			res.addf("! GIT W %s forge: %s", it.ID, err)
		}
		return res
	}
	res.lines = append(res.lines, s.gitOpenPR(f, it, branch).lines...)
	return res
}

// gitOpenPR opens the draft pull request, but only once the branch actually
// has something in it.
//
// A branch that entered active a moment ago is identical to base: the work has
// not happened yet, so there is nothing to commit and nothing to review. GitHub
// refuses such a pull request outright — "No commits between main and
// <branch>", a 422 — and it is right to, since an empty pull request describes
// nothing. Seeding an empty commit to force it open was rejected: it pollutes
// the per-edge commit trail that the never-squash policy exists to protect.
//
// So the draft opens at the FIRST COMMIT rather than at a fixed transition,
// which still satisfies the requirement (a draft exists while work is ongoing)
// — every later step calls this, so whichever one first finds the branch ahead
// of base is the one that opens it.
//
// Found live: the offline forge has no such precondition, so the same sequence
// succeeded locally and only the run against a real forge exposed it. The
// offline implementation is a lifecycle double, not a fidelity double.
func (s *Server) gitOpenPR(f forge.Forge, it item.Item, branch string) *gitFlowResult {
	res := &gitFlowResult{}
	if pr, ok, err := f.Find(branch); err != nil {
		res.addf("! GIT W %s pr lookup: %s", it.ID, err)
		return res
	} else if ok {
		res.addf("g pr %d %s (already open)", pr.Number, pr.URL)
		return res
	}
	if ahead, err := wt.IsAheadOfRemote(s.ws.Dir, branch, s.main.Cfg.Git.Remote, s.gitBase()); err != nil || !ahead {
		return res // nothing to review yet; a later step opens it
	}
	pr, err := f.Open(branch, s.gitBase(), it.ID+" "+it.Title, gitPRBody(it))
	if err != nil {
		res.addf("! GIT W %s pr open: %s", it.ID, err)
		return res
	}
	res.addf("g pr %d draft %s", pr.Number, pr.URL)
	return res
}

// gitFlowSync is the while-active half: commit and push whatever the work
// produced, so no change is ever only local.
//
// It is deliberately NOT called per journal append. A push per write would put
// a network round trip on every tool call, which is the same mistake the
// gitignore matcher and the stale-binary hint each avoid by working per walk
// and per debounce window rather than per event. It runs on the transitions
// that bracket work, and HasUnpushedCommits keeps it cheap when there is
// nothing to send.
func (s *Server) gitFlowSync(it item.Item) *gitFlowResult {
	res := &gitFlowResult{}
	if !s.gitEnabled() {
		return res
	}
	branch := taskBranch(it.ID)
	dir := s.ws.Dir
	if _, err := wt.CommitCode(dir, "spectackle "+it.ID+": checkpoint"); err != nil {
		res.addf("! GIT W %s commit: %s", it.ID, err)
		return res
	}
	unpushed, err := wt.HasUnpushedCommits(dir, s.main.Cfg.Git.Remote, branch)
	if err != nil || !unpushed {
		return res // nothing to send, or no upstream yet: start handles that
	}
	if err := s.gitPush(dir, branch); err != nil {
		res.addf("! GIT W %s push: %s", it.ID, err)
		return res
	}
	// Offline mode has nowhere to push to, so saying "pushed" would be a claim
	// this call did not earn — the one thing this file must never do. The
	// commit is real either way, so that is what gets reported.
	if s.main.Cfg.Git.Mode == "offline" {
		res.addf("g committed %s", branch)
	} else {
		res.addf("g pushed %s", branch)
	}
	return res
}

// gitFlowReady flips the task's PR out of draft: the task is done, so the work
// is up for review. Already-ready is a no-op, not an error.
func (s *Server) gitFlowReady(it item.Item) *gitFlowResult {
	res := &gitFlowResult{}
	if !s.gitEnabled() {
		return res
	}
	branch := taskBranch(it.ID)
	if r := s.gitFlowSync(it); r.String() != "" {
		res.lines = append(res.lines, r.lines...)
	}
	f, err := s.forgeFor()
	if err != nil {
		if !errors.Is(err, errNoRemote) {
			res.addf("! GIT W %s forge: %s", it.ID, err)
		}
		return res
	}
	pr, ok, err := f.Find(branch)
	if err != nil {
		res.addf("! GIT W %s pr lookup: %s", it.ID, err)
		return res
	}
	if !ok {
		// Not opened yet because the branch was empty when the task started
		// (see gitOpenPR). Done is the last moment it can still be opened, and
		// by now there is certainly something to review.
		res.lines = append(res.lines, s.gitOpenPR(f, it, branch).lines...)
		if pr, ok, err = f.Find(branch); err != nil || !ok {
			return res
		}
	}
	if !pr.Draft {
		res.addf("g pr %d ready (already)", pr.Number)
		return res
	}
	if pr, err = f.Ready(pr); err != nil {
		res.addf("! GIT W %s pr ready: %s", it.ID, err)
		return res
	}
	res.addf("g pr %d ready %s", pr.Number, pr.URL)
	return res
}

// gitFlowMerge merges the task's PR after verification (ADR-01KYDB): a merge
// commit, never squash.
//
// Insufficient permission is a degradation, not a failure — the PR stays open
// and the record says why. The automation has to be safe to run as an identity
// that cannot merge, and that difference has to be visible rather than silent.
func (s *Server) gitFlowMerge(it item.Item) *gitFlowResult {
	res := &gitFlowResult{}
	if !s.gitEnabled() {
		return res
	}
	branch := taskBranch(it.ID)
	f, err := s.forgeFor()
	if err != nil {
		if !errors.Is(err, errNoRemote) {
			res.addf("! GIT W %s forge: %s", it.ID, err)
		}
		return res
	}
	pr, ok, err := f.Find(branch)
	if err != nil {
		res.addf("! GIT W %s pr lookup: %s", it.ID, err)
		return res
	}
	if !ok {
		return res // nothing open: already merged, or never opened
	}
	mr, err := f.Merge(pr)
	if err != nil {
		res.addf("! GIT W %s pr merge: %s", it.ID, err)
		return res
	}
	if !mr.Merged {
		res.addf("! GIT W %s pr %d left open: %s", it.ID, pr.Number, mr.Reason)
		return res
	}
	res.addf("g pr %d merged %s", pr.Number, mr.SHA)
	return res
}

// gitBase is the branch task branches target.
func (s *Server) gitBase() string {
	if b := s.main.Cfg.Git.Base; b != "" {
		return b
	}
	return "main"
}

// gitPush pushes in online mode only. Offline mode does the whole lifecycle in
// the local repository, so there is nothing to push to and a missing remote
// must not read as a failure.
func (s *Server) gitPush(dir, branch string) error {
	if s.main.Cfg.Git.Mode == "offline" {
		return nil
	}
	return wt.Push(dir, s.main.Cfg.Git.Remote, branch)
}

// gitPRBody is the pull request description: the item's own body, which is the
// task brief the implementer worked from. The reviewer reads what the work was
// supposed to be, next to what it turned out to be.
func gitPRBody(it item.Item) string {
	var b strings.Builder
	b.WriteString(it.ID + " " + it.Title + "\n\n")
	if it.Body != "" {
		b.WriteString(it.Body + "\n")
	}
	if closes := closesLines(it); len(closes) > 0 {
		b.WriteString("\n" + strings.Join(closes, "\n") + "\n")
	}
	return b.String()
}

// closesLines turns the item's external refs into forge closing keywords, so
// an issue a task cites is closed by the forge when the task's work MERGES.
//
// Driven by the structured refs field, never by prose in the body. A URL is
// unambiguous and carries its own repository; recognizing "GitHub issue 26" in
// free text would be a heuristic whose false positives close other people's
// issues. Refs that are internal item IDs are skipped — they cite records, not
// trackers.
//
// The close is tied to the MERGE rather than to a record transition on purpose.
// Archiving an item says its record is finished; it says nothing about whether
// the branch landed. An issue should close when the fix reaches the base
// branch, which is exactly what a closing keyword in the pull request body
// means, and it costs no API call and no permission of its own.
//
// Only issue URLs are emitted: a cited pull request or document is provenance,
// and turning it into a closing keyword would close something the task never
// claimed to finish.
func closesLines(it item.Item) []string {
	var out []string
	seen := map[string]bool{}
	for _, r := range it.Refs {
		if !item.ExternalRef(r) || !issueURLRe.MatchString(r) || seen[r] {
			continue
		}
		seen[r] = true
		out = append(out, "Closes "+r)
	}
	sort.Strings(out)
	return out
}

// issueURLRe matches a forge issue URL. Deliberately narrow: /issues/<n> only,
// so a pull-request URL or a link to a discussion is cited without being
// closed.
var issueURLRe = regexp.MustCompile(`^https?://[^/\s]+/[^/\s]+/[^/\s]+/issues/\d+/?$`)

// gitFlowFor dispatches on the destination state. One place decides what a
// transition implies, so the mapping is readable as a whole rather than
// scattered across the move handler.
func (s *Server) gitFlowFor(it item.Item, to string) *gitFlowResult {
	switch to {
	case item.StateActive:
		return s.gitFlowStart(it)
	case item.StateDone:
		return s.gitFlowReady(it)
	case item.StateArchived:
		return s.gitFlowMerge(it)
	}
	return &gitFlowResult{}
}

// gitCfgOf is a small read helper so tests can assert the effective config
// without reaching into the Server.
func gitCfgOf(ws workspace.Root) workspace.GitCfg { return ws.Cfg.Git }
