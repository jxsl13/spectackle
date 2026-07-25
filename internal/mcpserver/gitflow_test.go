package mcpserver

import (
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
