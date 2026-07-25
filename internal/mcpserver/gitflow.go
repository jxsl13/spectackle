package mcpserver

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

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

// gitGate decides whether the automation runs for this transition, and when
// it does not, names the reason — because NOTHING in this flow is allowed to
// be silent. That is a reversal, by explicit user decision, of the first
// design here, which stayed quiet when the feature was disabled or not
// applicable: a quiet non-action reads identically to a broken one from the
// caller's side, and this session found two real defects (a PR silently
// still-draft, a config file silently never committed) hiding in exactly that
// ambiguity. One dense record per suppressed action is the price of never
// mistaking "off" for "failed" again.
//
// The reasons, in check order:
//
//   - disabled in config: the opt-out, honored and said.
//   - swarm worktree: work op=start / submit own that lifecycle (branch,
//     gate, ff-merge, semantic replay). If this flow committed records onto a
//     swarm worktree's branch, TouchedFiles would carry .spectackle paths and
//     submit's DirtyOverlap guard would refuse with main-dirty — B-0006 from
//     a new direction: in the swarm flow, records reach main via replay,
//     never via the branch.
//   - not a git repository: nothing to automate against.
//
// Note s.ws, not s.main: during `work` the active root is the task worktree,
// and that is the tree these primitives operate on.
func (s *Server) gitGate() (ok bool, reason string) {
	switch {
	case !s.main.Cfg.Git.IsEnabled():
		return false, "off: disabled in config"
	case s.wtItem != "":
		return false, "deferred: swarm worktree flow owns " + s.wtItem
	case !wt.IsRepo(s.ws.Dir):
		return false, "n/a: not a git repository"
	}
	return true, ""
}

// gitEnabled is gitGate for the call sites that only branch on the verdict;
// the reason is emitted once per transition by gitFlowFor.
func (s *Server) gitEnabled() bool {
	ok, _ := s.gitGate()
	return ok
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
		// No such remote: a git repository that was never given one. Not
		// applicable rather than broken — callers say so with a one-line
		// "g git n/a" record instead of a warning, distinguishable from a
		// real forge failure (nothing here is allowed to be silent).
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
	s.gitCommitRecords(res, it, item.StateActive)
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
		if errors.Is(err, errNoRemote) {
			res.addf("g git n/a: no remote %s", s.main.Cfg.Git.Remote)
		} else {
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
		// A forge refuses a pull request with no commits between head and
		// base, so it opens at the first commit instead — and says so, since a
		// silently deferred PR is indistinguishable from a broken one.
		res.addf("g pr deferred: %s not ahead of %s yet", branch, s.gitBase())
		return res
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
	s.gitCommitRecords(res, it, item.StateDone)
	unpushed, err := wt.HasUnpushedCommits(dir, s.main.Cfg.Git.Remote, branch)
	if err != nil || !unpushed {
		res.addf("g %s up to date", branch)
		return res
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
		if errors.Is(err, errNoRemote) {
			res.addf("g git n/a: no remote %s", s.main.Cfg.Git.Remote)
		} else {
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
	} else if pr, err = f.Ready(pr); err != nil {
		res.addf("! GIT W %s pr ready: %s", it.ID, err)
		return res
	} else {
		res.addf("g pr %d ready %s", pr.Number, pr.URL)
	}
	// done then WAITS for the head's CI verdict and carries it in this very
	// result — the blocking-await model (T-01KYDJC): the LLM waits on the
	// transition while the server polls, and learns about a red head in the
	// same breath as declaring done, not at archive time.
	awaitChecksReport(f, pr, mergeWaitBudget, mergePollInterval, res)
	return res
}

// gitCommitRecords commits whatever .spectackle record state is dirty in the
// active checkout, as its own commit, separate from the code commit.
//
// This is the mechanical answer to "why are uncommitted files lying in the
// repo after a full loop": CommitCode excludes .spectackle by design (B-0006),
// so the record delta every transition writes used to sit uncommitted with
// nobody mechanical responsible for it — the exact state the always-pushed
// policy forbids. Committing ALL dirty record files rather than only this
// transition's makes each transition self-healing: whatever a draft, grill or
// rule call left behind between transitions is swept up by the next one.
//
// Kept separate from the code commit so the history stays readable as
// decision-vs-work, which is the property the never-squash policy preserves
// on merge. The full per-edge engine (one commit per journal event, structured
// messages) is specified separately; this is the invariant it will refine, not
// replace.
func (s *Server) gitCommitRecords(res *gitFlowResult, it item.Item, to string) {
	committed, err := wt.CommitRecords(s.ws.Dir, "spectackle("+to+"): "+it.ID+" records")
	if err != nil {
		res.addf("! GIT W %s records: %s", it.ID, err)
		return
	}
	if committed {
		res.addf("g records committed")
	} else {
		res.addf("g records clean")
	}
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
	// The archive journal event was written by Move before this runs, so it is
	// on disk and must ride the branch INTO the merge — committed and pushed
	// first, or the record of the archival would be stranded on a branch whose
	// pull request has already closed.
	s.gitCommitRecords(res, it, item.StateArchived)
	if err := s.gitPush(s.ws.Dir, branch); err != nil {
		res.addf("! GIT W %s push: %s", it.ID, err)
	}
	f, err := s.forgeFor()
	if err != nil {
		if errors.Is(err, errNoRemote) {
			res.addf("g git n/a: no remote %s", s.main.Cfg.Git.Remote)
		} else {
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
		res.addf("g merge skipped: no open pr for %s (already merged, or never opened)", branch)
		return res
	}
	awaitChecksAndMerge(f, pr, mergeWaitBudget, mergePollInterval, res)
	return res
}

// Budgets for the mechanical merge. The wait budget covers this repository's
// own CI (about two minutes) with headroom; the poll interval keeps the forge
// polling to a handful of cheap reads per merge. Package variables rather
// than constants so the tests can shrink them to milliseconds — production
// never mutates them.
//
// Known cost, accepted deliberately: the archive tool call holds the server
// mutex while it waits, so a merge gated on live CI blocks other tool calls
// from THIS server for up to the budget. The alternative — merging in the
// background — leaves the archive transition reporting a merge it has not
// performed yet, which the never-silent decision (ADR-01KYDG) forbids more
// strongly than it forbids latency. A background finisher with its own
// notification channel is future work, and the budget bounds the damage.
var (
	mergeWaitBudget   = 5 * time.Minute
	mergePollInterval = 10 * time.Second
	mergeRetryBudget  = 8 // not-ready retries, one poll interval apart
)

// awaitChecksAndMerge is the merge gate (ADR-01KYDB: merge after
// verification), in its mechanically checkable half: the head's CI verdict.
//
//	failing  refuse loudly and leave the pull request open — red is never
//	         merged mechanically, whatever the branch protection situation is
//	none     proceed; a repository without CI has nothing to wait for, and
//	         waiting for checks that will never arrive is a hang, not a gate
//	pending  poll within the budget; a budget spent on still-pending checks
//	         refuses with the budget named, so the operator knows the loop
//	         closed without landing (B-01KYDH's required wording)
//	passing  merge, retrying the forge's transient not-ready refusals — 405
//	         mergeability recompute and 409 head-out-of-date, both observed
//	         live seconds after the records push
//
// Free-standing rather than a Server method so the tests can drive it with a
// scripted Forge and millisecond budgets, no Server required.
//
// The waiting itself lives in awaitChecks, SHARED with done's report-only
// conclusion (awaitChecksReport): one polling implementation, two endings —
// done reports the verdict, archive gates the merge on it. Two copies of a
// budgeted polling loop is how the two would drift.
func awaitChecksAndMerge(f forge.Forge, pr forge.PR, waitBudget, poll time.Duration, res *gitFlowResult) {
	state := awaitChecks(f, pr, waitBudget, poll, res)
	switch state {
	case forge.ChecksFailing:
		res.addf("! GIT W pr %d left open: checks failing — a red head is never merged mechanically", pr.Number)
		return
	case forge.ChecksPending:
		res.addf("! GIT W pr %d left open: checks still pending after %s — retry budget spent, merge it once CI concludes", pr.Number, waitBudget)
		return
	case "":
		return // Checks itself failed; awaitChecks already reported it
	case forge.ChecksPassing:
		res.addf("g pr %d checks passing", pr.Number)
	}

	for attempt := 1; ; attempt++ {
		mr, err := f.Merge(pr)
		if err != nil {
			res.addf("! GIT W pr %d merge: %s", pr.Number, err)
			return
		}
		if mr.Merged {
			res.addf("g pr %d merged %s", pr.Number, mr.SHA)
			return
		}
		if mr.Reason != forge.ReasonNotReady {
			res.addf("! GIT W pr %d left open: %s", pr.Number, mr.Reason)
			return
		}
		if attempt >= mergeRetryBudget {
			res.addf("! GIT W pr %d left open: still not mergeable after %d attempts — retry budget spent, merge it by hand", pr.Number, attempt)
			return
		}
		time.Sleep(poll)
	}
}

// awaitChecks polls the head's CI verdict until it concludes or the budget is
// spent, emitting the waiting record once. It returns the FINAL observed
// state: Passing or None concluded; Failing is a conclusion the caller words
// (done reports it, archive refuses on it); Pending means the budget ran out;
// "" means Checks itself errored, already reported here.
func awaitChecks(f forge.Forge, pr forge.PR, waitBudget, poll time.Duration, res *gitFlowResult) forge.CheckState {
	deadline := time.Now().Add(waitBudget)
	waited := false
	for {
		state, err := f.Checks(pr)
		if err != nil {
			res.addf("! GIT W pr %d checks: %s", pr.Number, err)
			return ""
		}
		switch state {
		case forge.ChecksNone:
			res.addf("g pr %d checks none — nothing to wait for", pr.Number)
			return state
		case forge.ChecksPassing:
			return state
		case forge.ChecksFailing:
			return state
		case forge.ChecksPending:
			if time.Now().After(deadline) {
				return state
			}
			if !waited {
				res.addf("g pr %d checks pending — waiting up to %s", pr.Number, waitBudget)
				waited = true
			}
			time.Sleep(poll)
		}
	}
}

// awaitChecksReport is done's conclusion of the shared wait: the LLM waits on
// the transition while the server polls, and the verdict lands in this very
// result (the blocking-await model the user chose over a background push —
// see T-01KYDJC's record of that refinement). The item stays done whatever
// the verdict: the lifecycle state is the server's own and a forge cannot
// veto it. What changes is what the caller KNOWS — an implementer that just
// declared done learns that CI disagrees in the same breath, while the
// context of what changed is still hot.
func awaitChecksReport(f forge.Forge, pr forge.PR, waitBudget, poll time.Duration, res *gitFlowResult) {
	switch awaitChecks(f, pr, waitBudget, poll, res) {
	case forge.ChecksFailing:
		res.addf("! CI E pr %d checks failing %s — item stays done; fix and reopen, or archive will refuse the merge", pr.Number, pr.URL)
	case forge.ChecksPending:
		res.addf("! CI W pr %d checks still pending after %s — verdict unknown at done; archive will wait again", pr.Number, waitBudget)
	case forge.ChecksPassing:
		res.addf("g pr %d checks passing", pr.Number)
	}
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
	// One gate, one stated reason. Emitted only on the transitions the flow
	// would otherwise act on — draft->submitted and friends imply no git work,
	// so there is nothing being suppressed there to report.
	if ok, reason := s.gitGate(); !ok {
		res := &gitFlowResult{}
		switch to {
		case item.StateActive, item.StateDone, item.StateArchived:
			res.addf("g git %s", reason)
		}
		return res
	}
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
