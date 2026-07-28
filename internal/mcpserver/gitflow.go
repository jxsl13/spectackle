package mcpserver

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jxsl13/spectackle/internal/forge"
	"github.com/jxsl13/spectackle/internal/graph"
	"github.com/jxsl13/spectackle/internal/ids"
	"github.com/jxsl13/spectackle/internal/item"
	"github.com/jxsl13/spectackle/internal/journal"
	"github.com/jxsl13/spectackle/internal/workspace"
	"github.com/jxsl13/spectackle/internal/wt"
)

// The git workflow, driven by the state machine instead of by the LLM
// (P-01KYDB, ADR-01KYDB).
//
// TWO MODES (GIT-DEFAULT-001, T-01KYHAH1GJ). Offline — the default — is
// commit-only: every edge commits code and records on the CURRENT branch
// (gitFlowOffline), with no branches, no forge, no pushes, no base
// checkouts. Online, the explicit `git: mode: online` opt-in, maps:
//
//	into active   ensure the task branch, commit code, push, open a DRAFT PR
//	while active  commit and push, so no change is ever only local
//	into done     local gates + CI await; the PR STAYS draft (PR-DRAFT-001)
//	into archived the single draft->ready flip, then merge commit, never squash
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
	// closureComplete: an archive closure either merged, legitimately had
	// nothing to merge, or runs without a remote. Anything else strands a
	// tombstoned item with an open PR (B-01KYGADQ, PRs 142/149) — the move
	// handler compensates archived->done so a retry re-drives the edge.
	closureComplete bool
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

// dietGreen implements RENDER-PARITY-001: a fully green edge renders ONE
// line naming its outcome artifact — the PR URL, the CI verdict, the merge
// SHA — because everything else (branch, records, gates, flips) is MCP
// automation the LLM does not need narrated. The collapse happens ONLY
// when nothing on the edge warned or failed: any "!" refusal or
// informational notice keeps the full surface — never-silent means
// failures speak, not that success narrates. The "w " guard is insurance
// for future in-function warnings: today's only w-line (the divergence
// note) is appended by gitFlowFor AFTER this collapse ran and is never
// subject to it.
func (r *gitFlowResult) dietGreen(artifact string) {
	if artifact == "" {
		return
	}
	for _, l := range r.lines {
		if strings.HasPrefix(l, "! ") || strings.HasPrefix(l, "w ") ||
			strings.Contains(l, "human-flipped") || strings.Contains(l, "deferred") ||
			strings.Contains(l, "conflict") || strings.Contains(l, "restored") ||
			strings.Contains(l, "stranded") || strings.Contains(l, "identity fallback") ||
			strings.Contains(l, "-close") || strings.Contains(l, "records-only closure") {
			return
		}
	}
	r.lines = []string{artifact}
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
	case !s.effectiveGit().IsEnabled():
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
// shortDisplayID renders an ID for HUMAN-facing git surfaces — branch
// names, PR titles, commit subjects (user requirement 2026-07-27): the
// kind prefix plus the first ids.MinRecordPrefixLen characters of the
// body, which pin all 48 timestamp bits and stay unambiguous for the
// repository lifetime. Machine surfaces (Spectackle-Item/Eid trailers,
// PR body first line, every .spectackle record) keep the FULL ID — the
// audit join and replay resolve exact IDs, never prefixes. Legacy or
// short-shaped IDs pass through unchanged.
func shortDisplayID(id string) string {
	kind, body, ok := strings.Cut(id, "-")
	if !ok || len(body) <= ids.MinRecordPrefixLen {
		return id
	}
	return kind + "-" + body[:ids.MinRecordPrefixLen]
}

// taskBranch names the item's branch with the SHORT display form: branches
// need uniqueness at creation (the 13-char floor provides it) and are never
// prefix-resolved afterward, so later mints cannot break them. Existing
// full-length branches stay valid — only new branches shorten.
func taskBranch(id string) string { return "spectackle/" + shortDisplayID(id) }

// itemBranch resolves the branch an item actually lives on: the short form
// for everything created after the display change, falling back to the
// full-length name for in-flight items whose branch predates it — without
// this, an item activated under the old naming would strand on its own
// later transitions.
func (s *Server) itemBranch(id string) string {
	short := taskBranch(id)
	if legacy := "spectackle/" + id; legacy != short &&
		!wt.BranchExists(s.main.Dir, short) && wt.BranchExists(s.main.Dir, legacy) {
		return legacy
	}
	return short
}

// forgeFor builds the client for the configured mode.
//
// Offline is a real implementation, not a stub: it performs the same sequence
// against the local repository and merges into the base branch with a merge
// commit. That is what lets the whole lifecycle run with no network, and what
// lets the tests exercise the mapping without standing up a forge.
func (s *Server) forgeFor() (forge.Forge, error) {
	if s.forgeOverride != nil {
		return s.forgeOverride()
	}
	// mode: offline never reaches this — its edges are commit-only
	// (gitFlowOffline, T-01KYHAH1GJ) and construct no forge; the offline
	// forge type survives solely as the hermetic test double behind
	// forgeOverride. A legacy cache/forge-offline.json is inert.
	cfg := s.effectiveGit()
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

// gitFlowOffline is the commit-only edge for mode: offline (T-01KYHAH1GJ,
// GIT-DEFAULT-001): no branch, no forge, no push, no base checkout — code
// and records commit on the CURRENT branch, the code commit rendered as
// `g offline commit <short-sha> <subject>`. gate runs the local verify
// gate after the commits; closure marks the archive edge, whose
// closureComplete follows the records commit alone — a records failure
// leaves it false so the move handler's archived→done compensation fires
// exactly as it does online (flowAttemptedMerge matches the records line).
func (s *Server) gitFlowOffline(it item.Item, subject string, state string, gate, closure bool) *gitFlowResult {
	res := &gitFlowResult{}
	dir := s.ws.Dir
	// A floating HEAD gets a refusal, not a silent commit onto nothing:
	// with the branch-creating normalizer gone, unborn/detached is the
	// caller's to resolve (mirrors the edge-commit engine's stand-down).
	if br, err := wt.CurrentBranch(dir); err != nil || strings.TrimSpace(br) == "HEAD" {
		res.addf("! GIT E %s offline: HEAD is unborn or detached — check out a branch, then retry", shortDisplayID(it.ID))
		return res
	}
	if committed, err := wt.CommitCode(dir, "spectackle "+shortDisplayID(it.ID)+": "+subject); err != nil {
		res.addf("! GIT E %s commit: %s", shortDisplayID(it.ID), err)
		return res
	} else if committed {
		res.addf("g offline commit %s %s", wt.HeadShortSHA(dir), subject)
	}
	pre := len(res.lines)
	s.gitCommitRecords(res, it, state)
	recordsOK := true
	for _, l := range res.lines[pre:] {
		if strings.HasPrefix(l, "! GIT E") {
			recordsOK = false
		}
	}
	if gate {
		if g := s.runGate(it.Goal); g != "" {
			res.addf("%s", g)
			res.addf("! GATE E %s local gate failed — fix and retry the move", shortDisplayID(it.ID))
			s.journalGateFail(it)
			return res
		}
		res.addf("g local gates passed")
	}
	if closure {
		res.closureComplete = recordsOK
	}
	return res
}

// gitFlowStart runs the into-active half — online: branch, commit, push,
// draft PR; offline: the commit-only edge above.
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
	if s.effectiveGit().Mode == "offline" {
		return s.gitFlowOffline(it, it.Title, item.StateActive, false, false)
	}
	branch := s.itemBranch(it.ID)
	dir := s.ws.Dir

	if err := wt.EnsureBranch(dir, branch, ""); err != nil {
		res.addf("! GIT E %s branch: %s", it.ID, err)
		return res
	}
	if _, err := wt.CommitCode(dir, "spectackle "+shortDisplayID(it.ID)+": "+it.Title); err != nil {
		res.addf("! GIT E %s commit: %s", it.ID, err)
	}
	s.gitCommitRecords(res, it, item.StateActive)
	if err := s.gitPush(dir, branch); err != nil {
		res.addf("! GIT E %s push: %s", it.ID, err)
		return res
	}
	res.addf("g branch %s pushed", branch)

	f, err := s.forgeFor()
	if err != nil {
		if errors.Is(err, errNoRemote) {
			res.addf("g git n/a: no remote %s", s.effectiveGit().Remote)
			res.closureComplete = true
		} else {
			res.addf("! GIT E %s forge: %s", it.ID, err)
		}
		return res
	}
	res.lines = append(res.lines, s.gitOpenPR(f, it, branch).lines...)
	// No reopen draft-flip (PR-DRAFT-001, superseding T-01KYDKNR8's two-way
	// mirror): the PR stays draft until archive, so a ready PR seen here is
	// either human-flipped (respected, never fought) or already merged —
	// reopen cycles must not toggle PR state back and forth.
	res.dietGreen(lastPRLine(res.lines))
	return res
}

// lastPRLine returns the newest "g pr ..." line — the edge's outcome
// artifact for the RENDER-PARITY-001 collapse.
func lastPRLine(lines []string) string {
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.HasPrefix(lines[i], "g pr ") {
			return lines[i]
		}
	}
	return ""
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
		res.addf("! GIT E %s pr lookup: %s", it.ID, err)
		return res
	} else if ok {
		res.addf("g pr %d %s (already open)", pr.Number, pr.URL)
		return res
	}
	ahead, err := wt.IsAheadOfRemote(s.ws.Dir, branch, s.effectiveGit().Remote, s.gitBase())
	if err != nil {
		// An error is not a deferral: rendering a failed probe as "not ahead
		// yet" (B-01KYDY's reporting smell) dresses breakage up as a
		// plausible wait state.
		res.addf("! GIT E %s ahead probe: %s", it.ID, err)
		return res
	}
	if !ahead {
		// A forge refuses a pull request with no commits between head and
		// base, so it opens at the first commit instead — and says so, since a
		// silently deferred PR is indistinguishable from a broken one.
		res.addf("g pr deferred: %s not ahead of %s yet", branch, s.gitBase())
		return res
	}
	pr, err := f.Open(branch, s.gitBase(), shortDisplayID(it.ID)+" "+it.Title, gitPRBody(it))
	if err != nil {
		res.addf("! GIT E %s pr open: %s", it.ID, err)
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
	if s.effectiveGit().Mode == "offline" {
		return s.gitFlowOffline(it, "checkpoint", item.StateDone, false, false)
	}
	branch := s.itemBranch(it.ID)
	dir := s.ws.Dir
	if _, err := wt.CommitCode(dir, "spectackle "+shortDisplayID(it.ID)+": checkpoint"); err != nil {
		res.addf("! GIT E %s commit: %s", it.ID, err)
		return res
	}
	s.gitCommitRecords(res, it, item.StateDone)
	unpushed, err := wt.HasUnpushedCommits(dir, s.effectiveGit().Remote, branch)
	if err != nil || !unpushed {
		res.addf("g %s up to date", branch)
		return res
	}
	if err := s.gitPush(dir, branch); err != nil {
		res.addf("! GIT E %s push: %s", it.ID, err)
		return res
	}
	res.addf("g pushed %s", branch)
	return res
}

// gitFlowReady runs the done edge's git closure: local gates, head pinning
// and the CI await — WITHOUT flipping the PR out of draft (PR-DRAFT-001:
// the single draft->ready flip belongs to the archive edge, and a
// human-flipped ready PR is respected untouched).
func (s *Server) gitFlowReady(it item.Item) *gitFlowResult {
	res := &gitFlowResult{}
	if !s.gitEnabled() {
		return res
	}
	if s.effectiveGit().Mode == "offline" {
		// done offline = commit + local gate verdict; no PR to flip, no
		// checks to await. A red gate reads "fix and retry the move".
		return s.gitFlowOffline(it, "checkpoint", item.StateDone, true, false)
	}
	branch := s.itemBranch(it.ID)
	if r := s.gitFlowSync(it); r.String() != "" {
		res.lines = append(res.lines, r.lines...)
	}
	f, err := s.forgeFor()
	if err != nil {
		if errors.Is(err, errNoRemote) {
			res.addf("g git n/a: no remote %s", s.effectiveGit().Remote)
			res.closureComplete = true
		} else {
			res.addf("! GIT E %s forge: %s", it.ID, err)
		}
		return res
	}
	pr, ok, err := f.Find(branch)
	if err != nil {
		res.addf("! GIT E %s pr lookup: %s", it.ID, err)
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
	// LOCAL gates before the runner is ever involved: the workspace verify
	// commands (plus the item's own goal) run here, at done, and a red gate
	// leaves the pull request a DRAFT — reported as GATE E with the failing
	// command, CI await skipped. A failure that never reaches a runner costs
	// zero runner minutes, which is the requirement's core: runners fire only
	// at finalization, after local gates pass. Passing is said in one record.
	if gate := s.runGate(it.Goal); gate != "" {
		res.addf("%s", gate)
		res.addf("! GATE E %s local gate failed — pr %d stays draft, fix and move to done again", it.ID, pr.Number)
		s.journalGateFail(it)
		return res
	}
	res.addf("g local gates passed")
	// The PR is NOT flipped to ready here (PR-DRAFT-001): draft holds
	// through every lifecycle edge and reopen cycle, and the single
	// draft->ready flip happens at the archive edge immediately before
	// merge. A ready PR at done is human-flipped and stays untouched.
	if pr.Draft {
		res.addf("g pr %d stays draft until archive", pr.Number)
	} else {
		res.addf("g pr %d ready (human-flipped, untouched)", pr.Number)
	}
	// done then WAITS for the head's CI verdict and carries it in this very
	// result — the blocking-await model (T-01KYDJC): the LLM waits on the
	// transition while the server polls, and learns about a red head in the
	// same breath as declaring done, not at archive time. The verdict is
	// pinned to the exact local head just pushed (B-01KYDN).
	s.pinHead(&pr, branch, res)
	awaitChecksReport(f, pr, mergeWaitBudget, mergePollInterval, res)
	// green done collapses to its outcome artifact: the CI verdict on the
	// still-draft PR (gates passing and PR-DRAFT-001 are implied by
	// reaching it) — any CI W/E or gate line keeps the full surface. The
	// artifact derives from the REAL verdict line: "checks passing" gains
	// the draft marker; a checks-none workspace keeps its honest line.
	artifact := lastPRLine(res.lines)
	if artifact == fmt.Sprintf("g pr %d checks passing", pr.Number) {
		artifact = fmt.Sprintf("g pr %d draft checks passing", pr.Number)
	}
	res.dietGreen(artifact)
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
	// One line per transition when the host resolves no commit identity: the
	// commits below then carry the spectackle@localhost placeholder
	// (B-01KYDK), and a fallback that changes commit attribution must never
	// be silent (ADR-01KYDG). Emitted here because every committing
	// transition passes through this function exactly once.
	if !wt.IdentityConfigured(s.ws.Dir) {
		res.addf("g identity fallback spectackle@localhost — set git user.name and user.email to attribute and sign commits")
	}
	committed, err := wt.CommitRecords(s.ws.Dir, "spectackle("+to+"): "+shortDisplayID(it.ID)+" records")
	if err != nil {
		res.addf("! GIT E %s records: %s", it.ID, err)
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
	if s.effectiveGit().Mode == "offline" {
		// The archive edge offline is records + straggler code on the
		// CURRENT branch — no merge exists to fail, so closureComplete
		// follows the records commit alone. The local gate mirrors the
		// online draft-flip arm exactly: it runs only for an item that WAS
		// active but never passed done's gate (still-draft PR equivalent) —
		// never-active archives (research closures, records-only items)
		// carry no code and meet no gate, same as before.
		gate := s.everActive(it.ID) && !strings.Contains(s.lastGateResult(it.ID), "last=pass")
		return s.gitFlowOffline(it, "closure", item.StateArchived, gate, true)
	}
	// Edge atomicity includes the WORKING TREE (B-01KYGADQ): a stranded
	// closure must leave the serving checkout on the branch it found, or
	// the archived->done compensation lands on the item branch and the
	// retry inherits a stale tree. Restore on every incomplete exit.
	startCur, _ := wt.CurrentBranch(s.ws.Dir)
	defer func() {
		if res.closureComplete || startCur == "" {
			return
		}
		if cur, err := wt.CurrentBranch(s.ws.Dir); err == nil && cur != startCur {
			if err := wt.EnsureBranch(s.ws.Dir, startCur, ""); err != nil {
				res.addf("! GIT E %s restore checkout %s: %s", it.ID, startCur, err)
				return
			}
			res.addf("g checkout restored to %s (stranded closure leaves the tree where it found it)", startCur)
		}
	}()
	branch := s.itemBranch(it.ID)
	// B-01KYDS: an item archived without ever entering active has no feature
	// branch — the old code committed the records onto whatever branch
	// happened to be checked out, then pushed the nonexistent item ref ("src
	// refspec does not match any") and stranded the closure. Create the
	// branch now, at the current head, exactly what the active transition
	// would have done; the unchanged machinery below then applies.
	//
	// B-01KYDY, the second stranding variant: the item branch EXISTS from an
	// earlier era but is not the current branch. Checking it out would
	// rewind the working tree to the code of that era, so the closure runs
	// on a fresh -close branch at the current head instead; the old branch
	// is never touched. When the item branch IS current (the whole normal
	// lifecycle), nothing changes.
	if !wt.BranchExists(s.ws.Dir, branch) {
		if err := wt.EnsureBranch(s.ws.Dir, branch, ""); err != nil {
			res.addf("! GIT E %s branch: %s", it.ID, err)
			return res
		}
		res.addf("g branch %s created: records-only closure of a never-active item", branch)
	} else if cur, err := wt.CurrentBranch(s.ws.Dir); err == nil && cur != branch {
		// B-01KYG56Y: the -close sidestep is legal ONLY for a stale-era
		// branch. A live branch ahead of base carries validated unmerged
		// code — closing records-only splits the record from the code
		// (PR 142/143 incident, user-caught). Ahead -> check the item
		// branch out and run the normal closure on it, exactly what the
		// lifecycle does when it is current; the rewind concern only ever
		// applied to already-merged eras.
		ahead, aerr := wt.IsAheadOfRemote(s.ws.Dir, branch, s.effectiveGit().Remote, s.gitBase())
		if aerr != nil {
			res.addf("! GIT E %s ahead check: %s — refusing the records-only sidestep on unknown branch state", it.ID, aerr)
			return res
		}
		if ahead {
			if err := wt.EnsureBranch(s.ws.Dir, branch, ""); err != nil {
				res.addf("! GIT E %s checkout live item branch: %s", it.ID, err)
				return res
			}
			res.addf("g branch %s checked out: live item branch ahead of %s — closing on it, never records-only", branch, s.gitBase())
			// The Move's edge commits (archive event, work.md tombstone)
			// landed on the PREVIOUS checkout before this switch — without
			// reconciling, the forge merge conflicts on work.md (the exact
			// PR 142 incident mechanics). Merge base in, resolving
			// .spectackle conflicts to base's side (the post-archive
			// tombstone is authoritative — the same resolution the manual
			// repair applied); any conflict OUTSIDE .spectackle aborts and
			// refuses by name, never auto-resolved.
			if err := s.reconcileClosureBranch(res, it.ID); err != nil {
				return res
			}
		} else {
			branch += "-close"
			if err := wt.EnsureBranch(s.ws.Dir, branch, ""); err != nil {
				res.addf("! GIT E %s closure branch: %s", it.ID, err)
				return res
			}
			res.addf("g branch %s created: stale-era item branch (fully merged) — closing on a fresh branch at the current head", branch)
		}
	}
	// The archive journal event was written by Move before this runs, so it is
	// on disk and must ride the branch INTO the merge — committed and pushed
	// first, or the record of the archival would be stranded on a branch whose
	// pull request has already closed.
	s.gitCommitRecords(res, it, item.StateArchived)
	if err := s.gitPush(s.ws.Dir, branch); err != nil {
		res.addf("! GIT E %s push: %s", it.ID, err)
	}
	f, err := s.forgeFor()
	if err != nil {
		if errors.Is(err, errNoRemote) {
			res.addf("g git n/a: no remote %s", s.effectiveGit().Remote)
			res.closureComplete = true
		} else {
			res.addf("! GIT E %s forge: %s", it.ID, err)
		}
		return res
	}
	pr, ok, err := f.Find(branch)
	if err != nil {
		res.addf("! GIT E %s pr lookup: %s", it.ID, err)
		return res
	}
	if !ok {
		// No open pull request can also mean one was never opened: the
		// records-only closure above, or a branch that was empty until this
		// very records commit. Try to open it before concluding there is
		// nothing to merge — gitOpenPR's not-ahead guard keeps this honest.
		res.lines = append(res.lines, s.gitOpenPR(f, it, branch).lines...)
		if pr, ok, err = f.Find(branch); err != nil || !ok {
			res.addf("g merge skipped: no open pr for %s (already merged, or nothing to review)", branch)
			res.closureComplete = true
			return res
		}
	}
	// THE single draft->ready flip of the whole lifecycle (PR-DRAFT-001):
	// every PR arrives here still draft — done no longer flips, reopens
	// never toggled — and a draft can never merge (GitHub answers 405,
	// which the not-ready retry loop would burn its whole budget on).
	// Flip exactly once, behind the same local gate done runs — runners
	// fire only after local gates pass, whichever transition fronts them.
	if pr.Draft {
		if gate := s.runGate(it.Goal); gate != "" {
			res.addf("%s", gate)
			res.addf("! GATE E %s local gate failed — pr %d stays draft, merge refused", it.ID, pr.Number)
			s.journalGateFail(it)
			return res
		}
		res.addf("g local gates passed")
		if pr, err = f.Ready(pr); err != nil {
			res.addf("! GIT E %s pr ready: %s", it.ID, err)
			return res
		}
		res.addf("g pr %d ready %s", pr.Number, pr.URL)
	}
	s.pinHead(&pr, branch, res)
	awaitChecksAndMerge(f, pr, mergeWaitBudget, mergePollInterval, res)
	// Post-condition (B-01KYG56Y, the meta-lesson made mechanical):
	// per-diff review cannot see cross-feature interaction bugs, so the
	// flow checks its own invariant — after an archive closure no open PR
	// may still reference the item. Covers both naming eras: the short
	// branch form is a prefix of the legacy full form.
	itemRef := taskBranch(it.ID)
	if spr, ok, ferr := f.Find(itemRef); ferr == nil && ok && spr.Number != pr.Number {
		res.addf("! GIT E %s stranded pr %d — validated code left unmerged on %s", it.ID, spr.Number, itemRef)
	}
	// green archive collapses to the merge SHA — the ORCH-SYNC-001 anchor
	// line itself (ready flip, gates and checks are implied by the merge)
	res.dietGreen(lastPRLine(res.lines))
	return res
}

// reconcileClosureBranch merges base into the checked-out live item branch
// so the closure merge is conflict-free (B-01KYG56Y). .spectackle conflicts
// resolve to base's side — the post-archive records on base are
// authoritative; journal.ndjson is union-merged by attribute anyway. A
// conflict outside .spectackle aborts the merge and refuses by name: code
// conflicts are a human judgment, never auto-resolved.
func (s *Server) reconcileClosureBranch(res *gitFlowResult, id string) error {
	git := func(args ...string) (string, error) {
		cmd := exec.Command("git", append([]string{"-C", s.ws.Dir}, args...)...)
		cmd.Env = append(os.Environ(), "LC_ALL=C")
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	base := s.gitBase()
	if _, err := git("merge", "--no-ff", "-m", "spectackle(reconcile): "+shortDisplayID(id)+" base into live closure branch", base); err == nil {
		return nil
	}
	conflicted, _ := git("diff", "--name-only", "--diff-filter=U")
	var outside []string
	var inside []string
	for _, f := range strings.Fields(conflicted) {
		if strings.Contains(f, workspace.Dot+"/") || strings.HasPrefix(f, workspace.Dot) {
			inside = append(inside, f)
		} else {
			outside = append(outside, f)
		}
	}
	if len(outside) > 0 {
		_, _ = git("merge", "--abort")
		res.addf("! GIT E %s closure reconcile: code conflicts with %s in %s — resolve by hand, then re-archive", id, base, strings.Join(outside, " "))
		return errors.New("closure reconcile conflict")
	}
	for _, f := range inside {
		if _, err := git("checkout", "--theirs", "--", f); err != nil {
			_, _ = git("merge", "--abort")
			res.addf("! GIT E %s closure reconcile: %s", id, err)
			return errors.New("closure reconcile failed")
		}
	}
	if _, err := git("add", "-A", "--", workspace.Dot); err != nil {
		_, _ = git("merge", "--abort")
		res.addf("! GIT E %s closure reconcile add: %s", id, err)
		return errors.New("closure reconcile failed")
	}
	if _, err := git("commit", "--no-edit"); err != nil {
		_, _ = git("merge", "--abort")
		res.addf("! GIT E %s closure reconcile commit: %s", id, err)
		return errors.New("closure reconcile failed")
	}
	res.addf("g reconciled %s into the live closure branch (.spectackle resolved to base)", base)
	return nil
}

// pinHead stamps the pull request with the local branch head, so the CI await
// polls the commit that was actually pushed (B-01KYDN: a branch ref asked
// seconds after a push can still answer for the predecessor). Failure to
// resolve is said and the await falls back to the branch ref — degraded, not
// silent.
func (s *Server) pinHead(pr *forge.PR, branch string, res *gitFlowResult) {
	sha, err := wt.HeadSHA(s.ws.Dir, branch)
	if err != nil {
		res.addf("! GIT E %s head resolve: %s — awaiting by branch ref instead", branch, err)
		return
	}
	pr.HeadSHA = strings.TrimSpace(sha)
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
		res.addf("! GIT E pr %d left open: checks failing — a red head is never merged mechanically", pr.Number)
		return
	case forge.ChecksPending:
		res.addf("! GIT E pr %d left open: checks still pending after %s — retry budget spent, merge it once CI concludes", pr.Number, waitBudget)
		return
	case "":
		return // Checks itself failed; awaitChecks already reported it
	case forge.ChecksPassing:
		res.addf("g pr %d checks passing", pr.Number)
	}

	for attempt := 1; ; attempt++ {
		mr, err := f.Merge(pr)
		if err != nil {
			res.addf("! GIT E pr %d merge: %s", pr.Number, err)
			return
		}
		if mr.Merged {
			res.addf("g pr %d merged %s", pr.Number, mr.SHA)
			res.closureComplete = true
			return
		}
		if mr.Reason != forge.ReasonNotReady {
			res.addf("! GIT E pr %d left open: %s", pr.Number, mr.Reason)
			return
		}
		if attempt >= mergeRetryBudget {
			res.addf("! GIT E pr %d left open: still not mergeable after %d attempts — retry budget spent, merge it by hand", pr.Number, attempt)
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
			res.addf("! GIT E pr %d checks: %s", pr.Number, err)
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
	if b := s.effectiveGit().Base; b != "" {
		return b
	}
	return "main"
}

// gitPush pushes in online mode only. Offline mode does the whole lifecycle in
// the local repository, so there is nothing to push to and a missing remote
// must not read as a failure.
func (s *Server) gitPush(dir, branch string) error {
	if s.effectiveGit().Mode == "offline" {
		return nil
	}
	return wt.Push(dir, s.effectiveGit().Remote, branch)
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
			// The divergence note renders on the refusal path too — the
			// gate failing BECAUSE of a diverging field (ws disabled, main
			// enabled) is precisely the case that must not stay silent
			// (cross-val-modesplit hole: the line was success-path-only).
			if d := s.gitDivergence(); d != "" {
				res.lines = append([]string{d}, res.lines...)
			}
		}
		return res
	}
	var res *gitFlowResult
	switch to {
	case item.StateActive:
		res = s.gitFlowStart(it)
	case item.StateDone:
		res = s.gitFlowReady(it)
	case item.StateArchived:
		res = s.gitFlowMerge(it)
	default:
		return &gitFlowResult{}
	}
	// The divergence note is stateless and per-transition (never-silent,
	// B-01KYHTJ4AP): a serving root whose committed config disagrees with
	// the primary checkout's must say which one is steering.
	if d := s.gitDivergence(); d != "" {
		res.lines = append([]string{d}, res.lines...)
	}
	return res
}

// gitScopeRefusal is the pre-Move guard for issue 178 defect 2: a
// transition whose git edge commits (active/done/archived, git enabled,
// not the swarm worktree flow) refuses while the working tree holds
// non-records changes OUTSIDE the item's declared targets — the commit
// sweep absorbed unrelated files (a SECRET_NOTE, an entire spec-system
// removal) under item names that gave no hint. nil = proceed.
func (s *Server) gitScopeRefusal(id, to string) *mcp.CallToolResult {
	switch to {
	case item.StateActive, item.StateDone, item.StateArchived:
	default:
		return nil
	}
	if ok, _ := s.gitGate(); !ok {
		return nil
	}
	it, ok, err := item.Get(s.ws, id)
	if err != nil || !ok {
		return nil // resolution problems are the move handler's to report
	}
	dirty, derr := wt.DirtyFiles(s.ws.Dir)
	if derr != nil {
		return nil // an unreadable tree fails later, with the git edge's own error
	}
	scope := normalizeTargets(it.Targets)
	// Node-ID targets (go:pkg.Fn) resolve to their FILE: the schema
	// advertises node targets, and an item scoped purely by nodes would
	// otherwise have every real edit refused (cross-val-sweep finding 1).
	for _, t := range it.Targets {
		if _, isPath := targetPath(t); !isPath {
			if n, ok := s.g.Node(graph.NodeID(t)); ok && n.File != "" {
				scope = append(scope, n.File)
			}
		}
	}
	var out []string
	for _, f := range dirty {
		if f == workspace.Dot || strings.HasPrefix(f, workspace.Dot+"/") {
			continue // records are the server's own commit
		}
		if inTargetScope(f, scope) {
			continue
		}
		out = append(out, f)
	}
	if len(out) == 0 {
		return nil
	}
	list := strings.Join(out[:min(3, len(out))], ", ")
	if len(out) > 3 {
		list += fmt.Sprintf(", +%d more", len(out)-3)
	}
	r, _, _ := refuse("! GIT E " + shortDisplayID(it.ID) + " transition refused: " + fmt.Sprint(len(out)) + " changed file(s) outside the declared targets (" + list + ") — commit or stash them; targets are revisable only in draft (reject back to draft to widen them)")
	return r
}

// inTargetScope reports whether a repo-relative file falls under any
// declared target: an exact file match, or a prefix match when the target
// names a directory.
func inTargetScope(f string, scope []string) bool {
	for _, t := range scope {
		t = strings.TrimSuffix(filepath.ToSlash(t), "/")
		if t == "" || t == "." {
			return true // a root-dir target deliberately allows everything
		}
		if f == t || strings.HasPrefix(f, t+"/") {
			return true
		}
	}
	return false
}

// journalGateFail appends the machine-readable gate outcome the render
// line alone cannot provide (B-01KYHV740T): a same-state EvMove carrying
// the established "gate fail" note (swarm.gateFail's shape, minus its
// rounds machinery — this is evidence, not escalation). lastGateResult
// keys on it, so a red-gated done edge stops reading as a pass at the
// archive gate and in the validate pack.
func (s *Server) journalGateFail(it item.Item) {
	_ = journal.Append(s.ws, it.Dir, journal.Event{
		Ev: journal.EvMove, ID: it.ID, Fr: it.State, To: it.State, Dir: it.Dir,
		Note: "gate fail",
	})
	s.markDirty()
}

// effectiveGit resolves the git configuration ONE way for every engine
// (B-01KYHTJ4AP): behavioral fields — Mode, Enabled, Commits — come from
// the SERVING root, because the tree the edges run on decides how they run
// and s.ws is the convention every other feedback gate already follows;
// repo-global plumbing — Remote, Base — stays with the primary checkout,
// whose .git/config all linked worktrees share. The asymmetry is
// deliberate: a branch may legitimately carry a different config ERA than
// main, but it must never re-point pushes at a different remote or base.
func (s *Server) effectiveGit() workspace.GitCfg {
	g := s.ws.Cfg.Git
	g.Remote = s.main.Cfg.Git.Remote
	g.Base = s.main.Cfg.Git.Base
	return g
}

// gitDivergence renders the never-silent note when the two roots'
// behavioral git fields disagree: one stateless line naming each differing
// field, both values, and the winner. Empty when the roots coincide or
// agree — the common case stays byte-identical.
func (s *Server) gitDivergence() string {
	if s.ws.Dir == s.main.Dir {
		return ""
	}
	w, m := s.ws.Cfg.Git, s.main.Cfg.Git
	var parts []string
	if w.Mode != m.Mode {
		parts = append(parts, fmt.Sprintf("mode ws=%s main=%s -> %s", w.Mode, m.Mode, w.Mode))
	}
	if w.IsEnabled() != m.IsEnabled() {
		parts = append(parts, fmt.Sprintf("enabled ws=%t main=%t -> %t", w.IsEnabled(), m.IsEnabled(), w.IsEnabled()))
	}
	if w.EdgeCommits() != m.EdgeCommits() {
		parts = append(parts, fmt.Sprintf("commits ws=%t main=%t -> %t", w.EdgeCommits(), m.EdgeCommits(), w.EdgeCommits()))
	}
	if len(parts) == 0 {
		return ""
	}
	return "w git config diverges (serving root wins): " + strings.Join(parts, ", ")
}
