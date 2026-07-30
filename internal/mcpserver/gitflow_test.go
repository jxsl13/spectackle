package mcpserver

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jxsl13/spectackle/internal/forge"

	"github.com/jxsl13/spectackle/internal/item"
	"github.com/jxsl13/spectackle/internal/lifecycle"
	"github.com/jxsl13/spectackle/internal/workspace"
	"github.com/jxsl13/spectackle/internal/wt"
)

// Issue closing is driven by the STRUCTURED refs field carrying real URLs, not
// by prose in the body. A URL cannot be confused with a rule ID, a record
// count or a version number — and the cost of confusing them is closing a
// stranger's issue, so the parser is narrow on purpose.

func TestClosesLinesFromIssueURLRefs(t *testing.T) {
	it := item.Item{
		ID: "T-01X", Title: "fix the thing", Body: "GitHub issue 26 is not parsed from here.",
		Refs: []string{
			"https://github.com/jxsl13/spectackle/issues/26",
			"https://github.com/jxsl13/spectackle/issues/27",
		},
	}
	body := gitPRBody(it)
	for _, want := range []string{
		"Closes https://github.com/jxsl13/spectackle/issues/26",
		"Closes https://github.com/jxsl13/spectackle/issues/27",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q:\n%s", want, body)
		}
	}
}

// TestClosesLinesIgnoreProse is the false-positive guard, and the reason this
// is refs-driven: a body mentioning issues in prose closes nothing. Acting on
// prose would mean a sentence like "unlike GitHub issue 26" closes issue 26.
func TestClosesLinesIgnoreProse(t *testing.T) {
	it := item.Item{
		ID: "T-01X", Title: "GitHub issue 26 in the title too",
		Body: "GitHub issue 26, and issues 27 and 28. Also #29 and SPX-ARC-002.",
	}
	if got := closesLines(it); len(got) != 0 {
		t.Fatalf("prose produced closing keywords: %v", got)
	}
}

// TestClosesLinesOnlyIssues: a cited pull request or discussion is provenance.
// Turning it into a closing keyword would close something the task never
// claimed to finish.
func TestClosesLinesOnlyIssues(t *testing.T) {
	it := item.Item{
		ID: "T-01X",
		Refs: []string{
			"https://github.com/jxsl13/spectackle/pull/40",
			"https://github.com/jxsl13/spectackle/discussions/7",
			"https://example.com/tracker/issues/99", // not owner/repo shaped
			"ADR-01KYDBQ8E7E970WQKGQ2MTR7XYZABC",
			"https://github.com/jxsl13/spectackle/issues/26",
		},
	}
	got := closesLines(it)
	if len(got) != 1 || !strings.HasSuffix(got[0], "/issues/26") {
		t.Fatalf("want only the forge issue URL, got %v", got)
	}
	// The foreign tracker URL is deliberately NOT emitted: a closing keyword
	// is interpreted by the forge hosting the pull request, so it can only
	// close an issue in that forge's own owner/repo namespace. Emitting one
	// for an unrelated host would be dead text at best.
}

// TestClosesLinesDedupeAndOrder keeps the pull request body stable rather than
// dependent on the order refs happened to be written in.
func TestClosesLinesDedupeAndOrder(t *testing.T) {
	u1 := "https://github.com/jxsl13/spectackle/issues/26"
	u2 := "https://github.com/jxsl13/spectackle/issues/27"
	got := closesLines(item.Item{Refs: []string{u2, u1, u2}})
	if len(got) != 2 || got[0] != "Closes "+u1 || got[1] != "Closes "+u2 {
		t.Fatalf("dedupe/order wrong: %v", got)
	}
}

// TestPRBodyUnchangedWithoutIssueRefs: the common case must be provably
// untouched, so this feature cannot change any pull request that cites nothing.
func TestPRBodyUnchangedWithoutIssueRefs(t *testing.T) {
	it := item.Item{ID: "T-01X", Title: "no refs", Body: "A body."}
	want := "T-01X no refs\n\nA body.\n"
	if got := gitPRBody(it); got != want {
		t.Fatalf("body changed for an item with no issue refs:\ngot  %q\nwant %q", got, want)
	}
}

// TestExternalRefAcceptedByValidation: a URL ref must survive draft-time
// validation, which otherwise rejects anything that is not a known item ID —
// that check is what made structured issue citations impossible before.
func TestExternalRefAcceptedByValidation(t *testing.T) {
	// a STORED id, not a displayed prefix: UnknownRefs works on what work.md
	// holds, and the display form is deliberately not a durable handle.
	const adr = "ADR-01KYDBQ8E7E970WQKGQ2MTR7AB"
	known := map[string]bool{adr: true}
	refs := []string{"https://github.com/jxsl13/spectackle/issues/26", adr}
	if bad := item.UnknownRefs("T-01X", refs, known); len(bad) != 0 {
		t.Fatalf("valid refs rejected: %v", bad)
	}
	if bad := item.UnknownRefs("T-01X", []string{"T-nope"}, known); len(bad) != 1 {
		t.Fatalf("an unknown item ID must still be rejected: %v", bad)
	}
}

// TestFullLoopLeavesNothingUncommitted pins the invariant directly: after a
// complete lifecycle driven through the server, no file — record or code — sits
// uncommitted in the checkout. This was field-reported as exactly that
// question: "why are uncommitted files lying in the repo after a full loop?"
// The answer was that CommitCode excludes .spectackle by design (B-0006) and
// nothing mechanical owned the other half. gitCommitRecords now does.
func TestFullLoopLeavesNothingUncommitted(t *testing.T) {
	root := gitRoot(t)
	writeOfflineGitConfig(t, root)
	s, sess := connectRootWithServer(t, root)

	id := draftFullID(t, s, sess, map[string]any{"kind": "task", "title": "leave nothing behind",
		"targets": []string{"work.go"}})
	callText(t, sess, "move", map[string]any{"id": id, "to": "active"})
	// work happens: a code change AND more record writes (a second draft)
	if err := os.WriteFile(filepath.Join(root, "work.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	draftID(t, sess, map[string]any{"kind": "bug", "title": "left behind between transitions"})
	callText(t, sess, "move", map[string]any{"id": id, "to": "done"})
	callText(t, sess, "move", map[string]any{"id": id, "to": "archived", "note": "loop closed"})
	// the loop must actually CLOSE — a scope refusal stalling the item in
	// active passed the old .spectackle-only assertion silently
	if got := callText(t, sess, "get", map[string]any{"id": id}); !strings.Contains(got, "archived") {
		t.Fatalf("lifecycle did not close: %q", got)
	}

	out, err := exec.Command("git", "-C", root, "status", "--porcelain", "-uall").Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.Contains(line, ".spectackle/") {
			t.Fatalf("record file uncommitted after a full loop:\n%s", out)
		}
	}
}

// TestRecordsCommitContainsOnlyRecords: the records commit is by explicit
// path list, so nothing outside .spectackle may appear in it — not even code
// the caller had staged. (The CODE commit sweeping staged code is by design:
// committing the task's work is CommitCode's entire job. The invariant under
// test is the records/code SEPARATION, not staging etiquette.)
func TestRecordsCommitContainsOnlyRecords(t *testing.T) {
	root := gitRoot(t)
	writeOfflineGitConfig(t, root)
	s, sess := connectRootWithServer(t, root)

	if err := os.WriteFile(filepath.Join(root, "bystander.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	id := draftFullID(t, s, sess, map[string]any{"kind": "task", "title": "separation"})
	callText(t, sess, "move", map[string]any{"id": id, "to": "active"})

	// every commit whose subject marks it as a records commit carries only
	// .spectackle paths
	logOut, err := exec.Command("git", "-C", root, "log", "--format=%H %s").Output()
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, line := range strings.Split(strings.TrimSpace(string(logOut)), "\n") {
		sha, subject, ok := strings.Cut(line, " ")
		// records commits: the gitflow sweep ("… records") and the
		// structured edge commits ("spectackle(<ev>): …") both qualify —
		// code checkpoints ("spectackle <id>: …") do not.
		if !ok || (!strings.Contains(subject, "records") && !strings.HasPrefix(subject, "spectackle(")) {
			continue
		}
		checked++
		names, err := exec.Command("git", "-C", root, "show", "--name-only", "--format=", sha).Output()
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range strings.Split(strings.TrimSpace(string(names)), "\n") {
			if f != "" && !strings.Contains(f, ".spectackle/") {
				t.Fatalf("records commit %s carries a non-record path %q", sha, f)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no records commit found to check")
	}
}

// writeOfflineGitConfig points the workspace at the offline forge so the
// lifecycle runs with no network and no remote.
func writeOfflineGitConfig(t *testing.T, root string) {
	t.Helper()
	p := filepath.Join(root, ".spectackle", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("schema: v1\ngit:\n  mode: offline\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeOnlineGitConfig preps the hermetic ONLINE fixture (T-01KYHAH1GJ P1):
// a local bare repo becomes origin (push and remote-ahead probes resolve),
// config pins mode: online, and the returned inject hook installs the
// persistent offline forge as the forge double on a connected server — the
// full branch/PR shape runs with no network and no GitHub. This is where
// the branch-dance coverage lives now that mode: offline is commit-only.
func writeOnlineGitConfig(t *testing.T, root string) (inject func(*Server)) {
	t.Helper()
	p := filepath.Join(root, ".spectackle", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("schema: v1\ngit:\n  mode: online\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bare := filepath.Join(t.TempDir(), "origin.git")
	if out, err := exec.Command("git", "init", "--bare", "-q", bare).CombinedOutput(); err != nil {
		t.Fatalf("init bare: %v %s", err, out)
	}
	closureGit(t, root, "remote", "add", "origin", bare)
	closureGit(t, root, "push", "-q", "origin", "HEAD:main")
	statePath := filepath.Join(t.TempDir(), "forge.json")
	return func(s *Server) {
		s.mu.Lock()
		s.forgeOverride = func() (forge.Forge, error) {
			return forge.NewOfflinePersistent(root, "main", statePath), nil
		}
		s.mu.Unlock()
	}
}

// scriptedForge serves awaitChecksAndMerge tests: Checks answers states in
// order (last one repeats), Merge answers results in order.
type scriptedForge struct {
	checks  []forge.CheckState
	merges  []forge.MergeResult
	nChecks int
	nMerges int
}

func (f *scriptedForge) Open(branch, base, title, body string) (forge.PR, error) {
	return forge.PR{}, nil
}
func (f *scriptedForge) Ready(pr forge.PR) (forge.PR, error) { return pr, nil }
func (f *scriptedForge) Draft(pr forge.PR) (forge.PR, error) {
	pr.Draft = true
	return pr, nil
}
func (f *scriptedForge) Find(branch string) (forge.PR, bool, error) { return forge.PR{}, false, nil }
func (f *scriptedForge) Checks(pr forge.PR) (forge.CheckState, error) {
	i := f.nChecks
	if i >= len(f.checks) {
		i = len(f.checks) - 1
	}
	f.nChecks++
	return f.checks[i], nil
}
func (f *scriptedForge) Merge(pr forge.PR) (forge.MergeResult, error) {
	i := f.nMerges
	if i >= len(f.merges) {
		i = len(f.merges) - 1
	}
	f.nMerges++
	return f.merges[i], nil
}

func runGate(t *testing.T, f *scriptedForge, budget time.Duration) string {
	t.Helper()
	res := &gitFlowResult{}
	awaitChecksAndMerge(f, forge.PR{Number: 7}, budget, time.Millisecond, res)
	return res.String()
}

// TestMergeGateWaitsForPendingThenMerges: the CI wait the LLM used to perform
// by hand, now mechanical — pending twice, then passing, then merged.
func TestMergeGateWaitsForPendingThenMerges(t *testing.T) {
	f := &scriptedForge{
		checks: []forge.CheckState{forge.ChecksPending, forge.ChecksPending, forge.ChecksPassing},
		merges: []forge.MergeResult{{Merged: true, SHA: "abc"}},
	}
	out := runGate(t, f, time.Second)
	if !strings.Contains(out, "checks pending — waiting") || !strings.Contains(out, "merged abc") {
		t.Fatalf("wait-then-merge not reported: %q", out)
	}
}

// TestMergeGateRefusesRed: a red head is never merged mechanically, and the
// refusal says so. Merge must not have been called at all.
func TestMergeGateRefusesRed(t *testing.T) {
	f := &scriptedForge{checks: []forge.CheckState{forge.ChecksFailing}, merges: []forge.MergeResult{{Merged: true}}}
	out := runGate(t, f, time.Second)
	if !strings.Contains(out, "checks failing") {
		t.Fatalf("red head not refused loudly: %q", out)
	}
	if f.nMerges != 0 {
		t.Fatalf("Merge was called on a red head %d times", f.nMerges)
	}
}

// TestMergeGateBudgetSpentOnPending: checks that never conclude spend the
// budget and refuse with the budget named (B-01KYDH's required wording), so
// the operator knows the loop closed without landing.
func TestMergeGateBudgetSpentOnPending(t *testing.T) {
	f := &scriptedForge{checks: []forge.CheckState{forge.ChecksPending}}
	out := runGate(t, f, 5*time.Millisecond)
	if !strings.Contains(out, "still pending after") || !strings.Contains(out, "retry budget spent") {
		t.Fatalf("budget-spent refusal missing: %q", out)
	}
	if f.nMerges != 0 {
		t.Fatal("Merge was called while checks never concluded")
	}
}

// TestMergeGateRetriesNotReady: the two transient refusals observed live —
// 405 mergeability recompute, 409 head-out-of-date — surface as
// ReasonNotReady and are retried until the forge settles.
func TestMergeGateRetriesNotReady(t *testing.T) {
	f := &scriptedForge{
		checks: []forge.CheckState{forge.ChecksNone},
		merges: []forge.MergeResult{
			{Merged: false, Reason: forge.ReasonNotReady},
			{Merged: false, Reason: forge.ReasonNotReady},
			{Merged: true, SHA: "def"},
		},
	}
	out := runGate(t, f, time.Second)
	if !strings.Contains(out, "merged def") {
		t.Fatalf("transient refusals not retried to success: %q", out)
	}
	if f.nMerges != 3 {
		t.Fatalf("expected 3 merge attempts, got %d", f.nMerges)
	}
}

// TestMergeGateNoPermissionNeverRetried: no-permission is a stable answer,
// not a transient one — one attempt, loud refusal, done.
func TestMergeGateNoPermissionNeverRetried(t *testing.T) {
	f := &scriptedForge{
		checks: []forge.CheckState{forge.ChecksNone},
		merges: []forge.MergeResult{{Merged: false, Reason: forge.ReasonNoPermission}},
	}
	out := runGate(t, f, time.Second)
	if !strings.Contains(out, forge.ReasonNoPermission) {
		t.Fatalf("no-permission refusal missing: %q", out)
	}
	if f.nMerges != 1 {
		t.Fatalf("no-permission was retried: %d attempts", f.nMerges)
	}
}

func runReport(t *testing.T, f *scriptedForge, budget time.Duration) string {
	t.Helper()
	res := &gitFlowResult{}
	awaitChecksReport(f, forge.PR{Number: 9, URL: "u"}, budget, time.Millisecond, res)
	return res.String()
}

// TestDoneReportPassing / Failing / BudgetSpent pin done's half of the shared
// wait (T-01KYDJC): the LLM waits on the transition, the verdict lands in the
// result, and a red head is a finding — not a refusal, since the lifecycle
// state is the server's own and a forge cannot veto it.
func TestDoneReportPassing(t *testing.T) {
	f := &scriptedForge{checks: []forge.CheckState{forge.ChecksPending, forge.ChecksPassing}}
	out := runReport(t, f, time.Second)
	if !strings.Contains(out, "checks pending — waiting") || !strings.Contains(out, "checks passing") {
		t.Fatalf("wait-then-pass not reported: %q", out)
	}
}

func TestDoneReportFailing(t *testing.T) {
	f := &scriptedForge{checks: []forge.CheckState{forge.ChecksFailing}}
	out := runReport(t, f, time.Second)
	if !strings.Contains(out, "! CI E") || !strings.Contains(out, "checks failing") {
		t.Fatalf("red head not reported as a finding: %q", out)
	}
	if !strings.Contains(out, "item stays done") {
		t.Fatalf("the report must say the state did not move: %q", out)
	}
}

func TestDoneReportBudgetSpent(t *testing.T) {
	f := &scriptedForge{checks: []forge.CheckState{forge.ChecksPending}}
	out := runReport(t, f, 5*time.Millisecond)
	if !strings.Contains(out, "verdict unknown at done") {
		t.Fatalf("budget-spent wording missing: %q", out)
	}
}

// TestSharedWaitOneImplementation: both endings run the same loop — asserted
// by behavior, not by inspection: the scripted forge's pending sequence is
// consumed identically by both, so a drift in either loop's polling shows as
// a different consumption count.
func TestSharedWaitOneImplementation(t *testing.T) {
	fa := &scriptedForge{checks: []forge.CheckState{forge.ChecksPending, forge.ChecksPending, forge.ChecksPassing},
		merges: []forge.MergeResult{{Merged: true, SHA: "x"}}}
	runGate(t, fa, time.Second)
	fb := &scriptedForge{checks: []forge.CheckState{forge.ChecksPending, forge.ChecksPending, forge.ChecksPassing}}
	runReport(t, fb, time.Second)
	if fa.nChecks != fb.nChecks {
		t.Fatalf("gate polled %d times, report %d — the two loops have drifted", fa.nChecks, fb.nChecks)
	}
}

// TestUnrecoverableFailuresAreErrors pins the severity taxonomy (B-01KYDM):
// a failure that leaves the workflow obligation unmet must reach the LLM as
// E, not W — an unrecoverable failure labeled warning is an error message
// received unclearly, which fails the requirement even though the bytes
// arrive. The one genuine W is verdict-unknown-at-done, where nothing failed.
func TestUnrecoverableFailuresAreErrors(t *testing.T) {
	// red at merge: obligation unmet, must be E
	f := &scriptedForge{checks: []forge.CheckState{forge.ChecksFailing}}
	if out := runGate(t, f, time.Second); !strings.Contains(out, "! GIT E") {
		t.Fatalf("red-at-merge not E severity: %q", out)
	}
	// budget spent at merge: obligation unmet, must be E
	f = &scriptedForge{checks: []forge.CheckState{forge.ChecksPending}}
	if out := runGate(t, f, 5*time.Millisecond); !strings.Contains(out, "! GIT E") {
		t.Fatalf("budget-spent-at-merge not E severity: %q", out)
	}
	// verdict unknown at done: item is done, nothing failed — stays W
	f = &scriptedForge{checks: []forge.CheckState{forge.ChecksPending}}
	if out := runReport(t, f, 5*time.Millisecond); !strings.Contains(out, "! CI W") {
		t.Fatalf("verdict-unknown-at-done lost its W: %q", out)
	}
}

// TestArchiveNeverActiveItemLandsRecords pins B-01KYDS's first half: a
// records-only closure (draft -> archived, item never entered active) must
// not reference a branch that was never created. The old code committed the
// records onto whatever was checked out, then pushed the nonexistent item
// ref ("src refspec does not match any") and stranded the closure. Now the
// merge path creates the branch at the current head, opens the pull request
// itself, flips it ready, and merges — the closure reaches the default
// branch mechanically.
func TestArchiveNeverActiveItemLandsRecords(t *testing.T) {
	root := gitRoot(t)
	writeOfflineGitConfig(t, root)
	s, sess := connectRootWithServer(t, root)

	id := draftFullID(t, s, sess, map[string]any{"kind": "bug", "title": "closed on paper only"})
	out := callText(t, sess, "move", map[string]any{"id": id, "to": "archived", "note": "records-only closure"})

	// The invariant survives the offline collapse (T-01KYHAH1GJ): the
	// closure lands MECHANICALLY — as commits on the current branch now,
	// with no branch dance, no PR theater, and no git error. The old
	// "merged"/created-branch evidence lines are gone by design.
	if strings.Contains(out, "! GIT E") {
		t.Fatalf("closure raised a git error:\n%s", out)
	}
	if strings.Contains(out, "offline://") || strings.Contains(out, "g branch ") {
		t.Fatalf("offline closure must not stage PR theater:\n%s", out)
	}

	// The archival record commit must be reachable from the CURRENT branch.
	logOut, err := exec.Command("git", "-C", root, "log", "--format=%s").Output()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(logOut), "spectackle(archived): "+shortDisplayID(id)+" records") &&
		!strings.Contains(string(logOut), "spectackle(archive): "+shortDisplayID(id)) {
		t.Fatalf("archival records commit not reachable from the current branch:\n%s", logOut)
	}
	// and zero item branches exist — the non-negotiable, worktree-less case
	branches, err := exec.Command("git", "-C", root, "branch", "--list", "spectackle/*").Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(branches)) != "" {
		t.Fatalf("offline lifecycle minted an item branch:\n%s", branches)
	}
}

// TestArchiveForwardSkipFlipsDraftReady pins B-01KYDS's second half: a legal
// active -> archived forward skip reaches the merge with the pull request
// still draft (done's ready flip never ran). A draft can never merge — the
// merge path must flip it ready itself, behind the same local gate done
// uses, and then merge.
func TestArchiveForwardSkipFlipsDraftReady(t *testing.T) {
	root := gitRoot(t)
	inject := writeOnlineGitConfig(t, root)
	s, sess := connectRootWithServer(t, root)
	inject(s)

	id := draftFullID(t, s, sess, map[string]any{"kind": "task", "title": "skip done entirely", "targets": []string{"skipped.go"}})
	callText(t, sess, "move", map[string]any{"id": id, "to": "active"})
	if err := os.WriteFile(filepath.Join(root, "skipped.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := callText(t, sess, "move", map[string]any{"id": id, "to": "archived", "note": "forward skip"})

	// RENDER-PARITY-001: the green skip collapses to the merge artifact —
	// the gate and the ready flip still RUN, proven below via the forge
	// state, they are just no longer narrated
	if !strings.Contains(out, "merged") {
		t.Fatalf("forward skip did not merge:\n%s", out)
	}
	// the merge completed mechanically: the offline forge deletes a merged
	// PR, so its absence IS the completion proof (the flip itself is
	// pinned pre-merge by TestSingleReadyFlipAtArchive's draft-state
	// assertions; the double cannot witness it post-merge)
	f, ferr := s.forgeOverride()
	if ferr != nil {
		t.Fatal(ferr)
	}
	if pr, ok, _ := f.Find(taskBranch(id)); ok {
		t.Fatalf("a merged PR must leave the open set: %+v", pr)
	}
}

// TestIdentityFallbackIsSaidOnTransition pins B-01KYDK's never-silent half:
// on a host resolving no git identity, the committing transition must SAY
// that its commits carry the placeholder — a fallback that changes commit
// attribution may never be invisible (ADR-01KYDG). On a configured host the
// line must be absent.
func TestIdentityFallbackIsSaidOnTransition(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_SYSTEM", "/dev/null")

	// Bare repo: gitRoot/InitTestRepo would configure an identity, so build
	// the fixture by hand without one.
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".spectackle"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".spectackle", "config.yaml"),
		[]byte("schema: v1\ngit:\n  mode: offline\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", root, "init", "-b", "main").CombinedOutput(); err != nil {
		t.Skipf("git unavailable: %v: %s", err, out)
	}
	if wt.IdentityConfigured(root) {
		t.Skip("host leaks an identity despite GIT_CONFIG_GLOBAL/SYSTEM=/dev/null")
	}
	// The fixture's own baseline commit, via the fallback-aware helper.
	if out, err := exec.Command("git", "-C", root, "add", "-A").CombinedOutput(); err != nil {
		t.Fatalf("add: %v: %s", err, out)
	}
	if out, err := exec.Command("git", "-C", root,
		"-c", "user.name=fixture", "-c", "user.email=fixture@localhost",
		"commit", "-q", "-m", "init").CombinedOutput(); err != nil {
		t.Fatalf("init commit: %v: %s", err, out)
	}

	s, sess := connectRootWithServer(t, root)
	id := draftFullID(t, s, sess, map[string]any{"kind": "task", "title": "identity fallback probe"})
	out := callText(t, sess, "move", map[string]any{"id": id, "to": "active"})
	if !strings.Contains(out, "g identity fallback spectackle@localhost") {
		t.Fatalf("bare-host transition did not report the identity fallback:\n%s", out)
	}
}

// TestSingleReadyFlipAtArchive pins PR-DRAFT-001 end to end over the
// offline lifecycle double (superseding T-01KYDKNR8's two-way mirror):
// done leaves the pull request DRAFT and says so, a reopen cycle never
// toggles PR state, the second done still leaves it draft, and the archive
// performs the lifecycle's single draft->ready flip immediately before the
// merge. Review cycles must never churn the PR state back and forth.
func TestSingleReadyFlipAtArchive(t *testing.T) {
	root := gitRoot(t)
	inject := writeOnlineGitConfig(t, root)
	s, sess := connectRootWithServer(t, root)
	inject(s)

	id := draftFullID(t, s, sess, map[string]any{"kind": "task", "title": "single flip", "targets": []string{"work.go"}})
	callText(t, sess, "move", map[string]any{"id": id, "to": "active"})
	if err := os.WriteFile(filepath.Join(root, "work.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	firstDone := callText(t, sess, "move", map[string]any{"id": id, "to": "done"})
	// RENDER-PARITY-001: a green done collapses to its single artifact
	// line and never narrates a ready flip
	if strings.Contains(firstDone, " ready") {
		t.Fatalf("done must not flip the pull request ready:\n%s", firstDone)
	}
	if got := strings.Count(firstDone, "\ng pr "); got > 1 {
		t.Fatalf("a green done must render ONE pr line, got %d:\n%s", got, firstDone)
	}
	// the draft state is proven mechanically, not by narration
	prDraftAfter := func(step string) {
		t.Helper()
		f, ferr := s.forgeOverride()
		if ferr != nil {
			t.Fatal(ferr)
		}
		if pr, ok, _ := f.Find(taskBranch(id)); !ok || !pr.Draft {
			t.Fatalf("%s must leave the pull request DRAFT in the forge state: found=%v %+v", step, ok, pr)
		}
	}
	prDraftAfter("first done")

	reopen := callText(t, sess, "move", map[string]any{"id": id, "to": "active"})
	if strings.Contains(reopen, "back to draft") {
		t.Fatalf("a reopen must not toggle PR state (it is already draft):\n%s", reopen)
	}
	prDraftAfter("reopen")

	if err := os.WriteFile(filepath.Join(root, "work.go"), []byte("package main\n\n// v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	secondDone := callText(t, sess, "move", map[string]any{"id": id, "to": "done"})
	if strings.Contains(secondDone, " ready") {
		t.Fatalf("the second done must still leave the pull request draft:\n%s", secondDone)
	}
	prDraftAfter("second done")
	archived := callText(t, sess, "move", map[string]any{"id": id, "to": "archived", "note": "single flip closed"})
	if !strings.Contains(archived, "merged") {
		t.Fatalf("archive after the flip did not merge:\n%s", archived)
	}
	// the merge closed the PR (the offline forge deletes merged records);
	// the flip's own pin is the prDraftAfter chain above — draft held
	// through both dones and the reopen, so the one flip can only have
	// happened on this archive edge
	f, ferr := s.forgeOverride()
	if ferr != nil {
		t.Fatal(ferr)
	}
	if pr, ok, _ := f.Find(taskBranch(id)); ok {
		t.Fatalf("a merged PR must leave the open set: %+v", pr)
	}
}

// TestFreshActivationDoesNotClaimDraftFlip: a first activation opens a
// draft pull request (or none yet); the reopen flip must not fire and must
// not be claimed on that path.
func TestFreshActivationDoesNotClaimDraftFlip(t *testing.T) {
	root := gitRoot(t)
	inject := writeOnlineGitConfig(t, root)
	s, sess := connectRootWithServer(t, root)
	inject(s)

	id := draftFullID(t, s, sess, map[string]any{"kind": "task", "title": "fresh start"})
	out := callText(t, sess, "move", map[string]any{"id": id, "to": "active"})
	if strings.Contains(out, "back to draft") {
		t.Fatalf("fresh activation claimed a reopen draft flip:\n%s", out)
	}
}

// TestArchiveWithStaleItemBranchUsesClosureBranch pins B-01KYDY: an item
// whose feature branch exists from an earlier era but is NOT the current
// branch must close on a fresh -close branch at the current head — never by
// checking out the old branch (which would rewind the working tree), and
// never by stranding the records commit on whatever branch happens to be
// checked out, which is how B-01KYDE7's closure records ended up riding a
// sibling item's pull request.
func TestArchiveWithStaleItemBranchUsesClosureBranch(t *testing.T) {
	root := gitRoot(t)
	inject := writeOnlineGitConfig(t, root)
	s, sess := connectRootWithServer(t, root)
	inject(s)

	id := draftFullID(t, s, sess, map[string]any{"kind": "task", "title": "stale branch closure",
		"targets": []string{"work.go"}})
	callText(t, sess, "move", map[string]any{"id": id, "to": "active"})
	if err := os.WriteFile(filepath.Join(root, "work.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	callText(t, sess, "move", map[string]any{"id": id, "to": "done"})

	// Simulate the stale-era situation the live B-01KYDE7 closure hit: the
	// item's work (records included) reached the default branch through an
	// earlier merge, the feature branch was left behind, and the checkout
	// now sits on the default branch. A bare checkout would not do — it
	// would rewind the records and the item would become invisible.
	branch, err := exec.Command("git", "-C", root, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", root, "checkout", "-q", "main").CombinedOutput(); err != nil {
		t.Fatalf("checkout main: %v: %s", err, out)
	}
	if out, err := exec.Command("git", "-C", root, "-c", "user.name=t", "-c", "user.email=t@l",
		"merge", "--no-ff", "-m", "earlier era merge", strings.TrimSpace(string(branch))).CombinedOutput(); err != nil {
		t.Fatalf("merge stale branch: %v: %s", err, out)
	}

	// online discriminator compares against origin — sync the merged main
	// there so the feature branch reads fully merged, the stale-era premise
	closureGit(t, root, "push", "-q", "origin", "main")
	out := callText(t, sess, "move", map[string]any{"id": id, "to": "archived", "note": "closed from elsewhere"})
	if !strings.Contains(out, "-close created: stale-era item branch (fully merged)") {
		t.Fatalf("stale-branch archive did not use a closure branch:\n%s", out)
	}
	if !strings.Contains(out, "merged") {
		t.Fatalf("closure branch did not merge:\n%s", out)
	}

	logOut, err := exec.Command("git", "-C", root, "log", "--format=%s", "main").Output()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(logOut), "spectackle(archived): "+shortDisplayID(id)+" records") &&
		!strings.Contains(string(logOut), "spectackle(archive): "+shortDisplayID(id)) {
		t.Fatalf("archival records commit not reachable from main:\n%s", logOut)
	}
}

// TestOnlineRenderParity pins RENDER-PARITY-001's measurable core: a fully
// green ONLINE lifecycle renders at most one g-line more per edge than the
// offline collapse (the PR/merge artifact) — everything else the old
// surface narrated (branch, records, gates, flips, waits) is MCP
// automation the LLM never needed to read.
func TestOnlineRenderParity(t *testing.T) {
	gLines := func(out string) int {
		n := 0
		for _, l := range strings.Split(out, "\n") {
			if strings.HasPrefix(l, "g ") {
				n++
			}
		}
		return n
	}
	run := func(t *testing.T, online bool) map[string]int {
		root := gitRoot(t)
		var inject func(*Server)
		if online {
			inject = writeOnlineGitConfig(t, root)
		} else {
			writeOfflineGitConfig(t, root)
		}
		s, sess := connectRootWithServer(t, root)
		if inject != nil {
			inject(s)
		}
		id := draftFullID(t, s, sess, map[string]any{"kind": "task", "title": "parity probe", "targets": []string{"probe.go"}})
		counts := map[string]int{}
		counts["active"] = gLines(callText(t, sess, "move", map[string]any{"id": id, "to": "active"}))
		if err := os.WriteFile(filepath.Join(root, "probe.go"), []byte("package main\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		counts["done"] = gLines(callText(t, sess, "move", map[string]any{"id": id, "to": "done"}))
		counts["archived"] = gLines(callText(t, sess, "move", map[string]any{"id": id, "to": "archived", "note": "parity probe closed"}))
		return counts
	}
	on := run(t, true)
	off := run(t, false)
	for _, edge := range []string{"active", "done", "archived"} {
		if on[edge] > off[edge]+1 {
			t.Fatalf("edge %s: online renders %d g-lines vs offline %d — parity allows at most +1 (the artifact)",
				edge, on[edge], off[edge])
		}
	}
}

// TestGitScopeRefusalIgnoresNestedRecords pins the deadlock B-01KYSDBZTEF1A
// recorded, at the exact expression that caused it. gitScopeRefusal exempts
// server-owned records, but the exemption used to read
//
//	f == workspace.Dot || strings.HasPrefix(f, workspace.Dot+"/")
//
// which only matches the ROOT context. Context dirs nest, so when the server
// re-scoped an item into internal/mcpserver and wrote that item's own block
// into internal/mcpserver/.spectackle/, the gate counted the write as
// undeclared work and refused the item's archive — a state no transition could
// clear, since the same gate guards every direction. Reproduced live twice
// before the fix.
//
// This compares the old and new expressions directly, which pins the PREDICATE
// only. That is not sufficient on its own and the original version of this file
// pretended otherwise: it claimed a gate-level test was impractical because the
// re-scope needs an established context dir. That excuse was wrong — a verifier
// built the full scenario through the public path — and the cost of believing it
// was measured: re-inlining the old expression in the gate left the entire suite
// green. TestGitScopeRefusalUsesTheRecordsPredicate below is the test that
// actually holds the line; this one narrows the failure when that one trips.
func TestGitScopeRefusalIgnoresNestedRecords(t *testing.T) {
	rootAnchored := func(f string) bool {
		return f == workspace.Dot || strings.HasPrefix(f, workspace.Dot+"/")
	}
	// Every path the server can write records to, root and nested.
	for _, f := range []string{
		".spectackle/work.md",
		".spectackle/journal.ndjson",
		"internal/mcpserver/.spectackle/work.md",
		"internal/mcpserver/.spectackle/journal.ndjson",
		"a/b/c/.spectackle/spec.md",
	} {
		if !workspace.IsRecordsPath(f) {
			t.Errorf("IsRecordsPath(%q) = false; the gate would blame the caller for a server write", f)
		}
	}
	// The nested cases are exactly what the old spelling missed — assert that,
	// so this test fails if anyone reintroduces the root-anchored form.
	for _, f := range []string{
		"internal/mcpserver/.spectackle/work.md",
		"a/b/c/.spectackle/spec.md",
	} {
		if rootAnchored(f) {
			t.Fatalf("fixture is wrong: %q was already matched by the root-anchored form, so this test proves nothing", f)
		}
	}
	// And ordinary work must still be counted, including the near miss the old
	// HasPrefix spelling would have swallowed.
	for _, f := range []string{"pkg/a.go", ".spectacklefoo", "internal/.spectacklefoo/x.go"} {
		if workspace.IsRecordsPath(f) {
			t.Errorf("IsRecordsPath(%q) = true; real work would bypass the scope gate", f)
		}
	}
}

// TestSiblingTestFileIsInScope pins the deliberate decision that a declared
// source file covers its exact _test.go sibling, and only that: an unrelated
// test file must still be refused, or the gate would stop catching the case it
// was written for.
func TestSiblingTestFileIsInScope(t *testing.T) {
	scope := normalizeTargets([]string{"pkg/a.go", "internal/x/y.go"})
	for _, tgt := range scope {
		if strings.HasSuffix(tgt, ".go") && !strings.HasSuffix(tgt, "_test.go") {
			scope = append(scope, strings.TrimSuffix(tgt, ".go")+"_test.go")
		}
	}
	for _, c := range []struct {
		f    string
		want bool
	}{
		{"pkg/a.go", true},
		{"pkg/a_test.go", true},         // the sibling, now implicit
		{"internal/x/y_test.go", true},  // nested sibling
		{"pkg/b_test.go", false},        // an UNRELATED test file: still refused
		{"internal/x/z_test.go", false}, // same dir, different source: refused
		{"pkg/a_test.go.bak", false},
	} {
		if got := inTargetScope(c.f, scope); got != c.want {
			t.Errorf("inTargetScope(%q) = %v, want %v", c.f, got, c.want)
		}
	}
}

// TestGitScopeRefusalUsesTheRecordsPredicate and its sibling below drive
// gitScopeRefusal ITSELF, not the predicate it calls. The first versions of
// these tests did not, and an independent verifier proved the consequence by
// mutation: re-inlining the old root-anchored expression here, or deleting the
// sibling widening entirely, left the whole suite GREEN. Pinning a helper is not
// pinning the gate that uses it — the same green-test/dead-gate shape this
// record set out to remove (B-01KYSDBZTEF1A).
func TestGitScopeRefusalUsesTheRecordsPredicate(t *testing.T) {
	root := t.TempDir()
	gitInitForScope(t, root)
	s := newTestServer(t, root)
	id := draftForScope(t, s, root, []string{"pkg/a.go"})

	// A NESTED context's records write must not be blamed on the caller. This is
	// the deadlock: the server re-scopes an item into a nested context, writes
	// that item's own block there, and the gate then refuses the item's own
	// transition with nothing able to clear it.
	writeForScope(t, root, "internal/mcpserver/.spectackle/journal.ndjson", "{}\n")
	if r := s.gitScopeRefusal(id, item.StateDone); r != nil {
		t.Errorf("gate refused a NESTED records write — the deadlock is back: %v", r)
	}
	// The root context's records too, which the old expression did cover.
	writeForScope(t, root, ".spectackle/journal.ndjson", "{}\n")
	if r := s.gitScopeRefusal(id, item.StateDone); r != nil {
		t.Errorf("gate refused a ROOT records write: %v", r)
	}
	// And genuine undeclared work must STILL be refused, including the near miss
	// the old HasPrefix spelling would have excused.
	for _, f := range []string{"SECRET_NOTE.md", ".spectacklefoo/x.go", "internal/.spectacklefoo/x.go"} {
		writeForScope(t, root, f, "x\n")
		if r := s.gitScopeRefusal(id, item.StateDone); r == nil {
			t.Errorf("gate ALLOWED undeclared work %q — the gate is bypassable", f)
		}
		rmForScope(t, root, f)
	}
}

// TestGitScopeRefusalCoversSiblingTest drives the sibling widening through the
// gate. Deleting the widening from production must fail this.
func TestGitScopeRefusalCoversSiblingTest(t *testing.T) {
	root := t.TempDir()
	gitInitForScope(t, root)
	s := newTestServer(t, root)
	id := draftForScope(t, s, root, []string{"pkg/a.go"})

	writeForScope(t, root, "pkg/a_test.go", "package pkg\n")
	if r := s.gitScopeRefusal(id, item.StateDone); r != nil {
		t.Errorf("gate refused the declared file's own _test.go sibling: %v", r)
	}
	rmForScope(t, root, "pkg/a_test.go")

	// Only the EXACT sibling. An unrelated test file, a near-miss name, and a
	// same-named test in another directory must all still be refused.
	for _, f := range []string{"pkg/b_test.go", "pkg/a_test.go.bak", "pkg/sub/a_test.go"} {
		writeForScope(t, root, f, "x\n")
		if r := s.gitScopeRefusal(id, item.StateDone); r == nil {
			t.Errorf("gate ALLOWED %q — the sibling rule is broader than the exact sibling", f)
		}
		rmForScope(t, root, f)
	}
}

func gitInitForScope(t *testing.T, root string) {
	t.Helper()
	for _, args := range [][]string{
		{"init", "-q"}, {"config", "user.email", "t@t.t"}, {"config", "user.name", "t"},
		{"config", "gc.auto", "0"}, {"config", "maintenance.auto", "false"},
		{"commit", "-q", "--allow-empty", "-m", "init"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
}

func draftForScope(t *testing.T, s *Server, root string, targets []string) string {
	t.Helper()
	it, err := lifecycle.Draft(s.ws, s.minter(), "task", "scope fixture", "body", "", "", targets)
	if err != nil {
		t.Fatal(err)
	}
	return it.ID
}

func writeForScope(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func rmForScope(t *testing.T, root, rel string) {
	t.Helper()
	if err := os.Remove(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
		t.Fatal(err)
	}
}
