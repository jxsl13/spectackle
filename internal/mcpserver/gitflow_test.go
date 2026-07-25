package mcpserver

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jxsl13/spectackle/internal/item"
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

	id := draftFullID(t, s, sess, map[string]any{"kind": "task", "title": "leave nothing behind"})
	callText(t, sess, "move", map[string]any{"id": id, "to": "active"})
	// work happens: a code change AND more record writes (a second draft)
	if err := os.WriteFile(filepath.Join(root, "work.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	draftID(t, sess, map[string]any{"kind": "bug", "title": "left behind between transitions"})
	callText(t, sess, "move", map[string]any{"id": id, "to": "done"})
	callText(t, sess, "move", map[string]any{"id": id, "to": "archived", "note": "loop closed"})

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
		if !ok || !strings.Contains(subject, "records") {
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
