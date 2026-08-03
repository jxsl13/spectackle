package mcpserver

// The compact-due advisory is a SOFT nudge whose trigger is elapsed journal
// events, not a code change. B-01KYRJ3WSCE14: it was appended into the same
// `lines` slice whose emptiness gates check's `ok check 0 findings` summary,
// so crossing the threshold REPLACED the verification answer with a nudge —
// and postCall prepended a second copy of the very same record. The required
// CI self-hosting gate asserts a single `ok check ` line, so a workspace whose
// code and spec were both clean failed it on whatever branch happened to cross
// the threshold.
//
// Everything below pins what the CALLER observes from `check`/`get`, never a
// helper's return value: the defect was entirely in how the pieces compose.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/jxsl13/spectackle/internal/journal"
	"github.com/jxsl13/spectackle/internal/workspace"
)

// seedCompactDueRoot scaffolds a workspace that is CLEAN for check (no rules,
// no items, no drift) but whose root journal already sits above journalMax, so
// the only thing check can possibly say beyond its summary is the compact-due
// advisory.
//
// The events are written straight into journal.ndjson rather than driven
// through tool calls so the session connected afterwards starts with a COLD
// compact-hint cache — its very first call re-counts the journal regardless of
// the 30s debounce (s.lastCompactCheck is the zero value), which is exactly
// the state a fresh CI `serve` process is in. Same reasoning as
// TestCompactHintFiresOncePerCrossing (swarm_test.go).
func seedCompactDueRoot(t *testing.T, journalMax, events int) string {
	t.Helper()
	root := t.TempDir()
	ws := workspace.Root{Dir: root}
	if err := ws.EnsureScaffold(""); err != nil {
		t.Fatal(err)
	}
	cfg := fmt.Sprintf("schema: v1\ngit:\n  mode: offline\ncompact:\n  journal_max: %d\n", journalMax)
	if err := os.WriteFile(filepath.Join(root, ".spectackle", "config.yaml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < events; i++ {
		if err := journal.Append(ws, "", journal.Event{Ev: journal.EvCreate, ID: fmt.Sprintf("T-%04d", i)}); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// rootJournalAdvisoryRe matches the LIVE root compact advisory and captures
// its count, so a test can tell a real emission apart from a record body that
// merely quotes the same sentence.
var rootJournalAdvisoryRe = regexp.MustCompile(`(?m)^c \. journal (\d+) events since last compact$`)

// TestCheckAdvisoryComposesWithCleanSummary is the core of B-01KYRJ3WSCE14:
// on a workspace with ZERO findings whose journal has crossed the compaction
// threshold, check must still answer the verification question — the advisory
// is ADDITIONAL, not a replacement — and it must say it once.
//
// The two assertions are deliberately separate t.Errorf calls, not a single
// fatal: they cover two independent defects (displacement in check(), and
// duplication between check() and postCall) and each has to be able to regress
// on its own.
func TestCheckAdvisoryComposesWithCleanSummary(t *testing.T) {
	root := seedCompactDueRoot(t, 1, 1)
	sess := connectRoot(t, root)

	out := callText(t, sess, "check", map[string]any{})

	// Defect 1: the advisory displaced the summary. `lines` was the slice the
	// zero-findings test read, so a `c` record in it made check answer a
	// verification call with a housekeeping nudge and nothing else.
	if !strings.Contains(out, "ok check 0 findings (E=0 W=0)") {
		t.Errorf("compact-due advisory suppressed the clean-check summary: %q", out)
	}
	// Defect 2: the same record twice — once from check's own
	// compactCandidates, once prepended by postCall's proactive hint. Counting
	// occurrences, not asserting a substring: a message-shape assertion passes
	// while the caller still pays for the duplicate on every single check.
	if got := strings.Count(out, "events since last compact"); got != 1 {
		t.Errorf("compact advisory rendered %d times, want exactly 1: %q", got, out)
	}
}

// TestCIGateExpressionAcceptsCompactAdvisory re-implements, in Go, the
// self-hosting gate predicate from .github/workflows/ci.yml:89-96 and runs it
// over real check output from a workspace that is clean but compact-due.
//
// The workflow and this render are ONE contract enforced in two places: the
// gate string-matches check's output, so a change to either alone re-opens
// B-01KYRJ3WSCE14 — that is why the predicate is duplicated here instead of
// only asserting the output shape. Keep the two in step.
func TestCIGateExpressionAcceptsCompactAdvisory(t *testing.T) {
	root := seedCompactDueRoot(t, 1, 1)
	sess := connectRoot(t, root)
	out := callText(t, sess, "check", map[string]any{})

	// ci.yml:89 — sibling learnings and soft advisories are dropped; findings
	// (!), graph gaps (g) and drift (d) all survive and fail the gate.
	var lines []string
	for _, l := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if strings.HasPrefix(l, "sw ") || strings.HasPrefix(l, "c ") {
			continue
		}
		lines = append(lines, l)
	}
	// ci.yml:95-96 — exactly one surviving line, the clean-check shape.
	clean := len(lines) == 1 &&
		strings.HasPrefix(lines[0], "ok check ") &&
		strings.Contains(lines[0], "0 findings (E=0 W=0)")
	if !clean {
		t.Fatalf("CI gate predicate rejects a clean compact-due workspace; surviving lines %q from %q", lines, out)
	}
}

// TestAdvisoryDedupeIsStructuralNotTextual guards the dedupe MECHANISM, not
// just its effect. Suppressing postCall's hint by substring-matching the
// rendered result for "c . journal " looks equivalent and is not: it swallows
// a LIVE nudge whenever the result merely QUOTES that sentence — which any
// `get` on a record discussing this bug does — and because compactHint()
// advances s.hintedAt as a side effect of being called, the crossing is then
// marked surfaced by a call that never surfaced it. The nudge is lost, not
// deferred.
func TestAdvisoryDedupeIsStructuralNotTextual(t *testing.T) {
	root := seedCompactDueRoot(t, 1, 1)

	// A record whose BODY quotes the advisory sentence verbatim. Drafted from
	// its own session so the session that reads it back below still starts
	// cold.
	quoted := "the render printed `c . journal 999 events since last compact` twice while the ok summary vanished entirely, " +
		"which is what turned the required self-hosting gate red on a workspace whose code and spec were both clean, " +
		"and the fix has to compose the advisory with the summary instead of letting it displace the answer"
	id := draftID(t, connectRoot(t, root), map[string]any{
		"kind": "bug", "title": "advisory quoting must not silence the advisory", "body": quoted,
	})

	// (a) reading that record must STILL surface the live root advisory.
	reader := connectRoot(t, root)
	out := callText(t, reader, "get", map[string]any{"id": id})
	live := false
	for _, m := range rootJournalAdvisoryRe.FindAllStringSubmatch(out, -1) {
		if m[1] != "999" { // 999 is the count the quoted BODY names, not a real one
			live = true
		}
	}
	if !live {
		t.Errorf("a record body quoting the advisory swallowed the live one: %q", out)
	}

	// (b) a call whose BODY rendered the advisory must still let the hint's
	// bookkeeping run, so the NEXT call does not repeat what was just read.
	// This is what breaks if the guard is reordered to short-circuit the
	// compactHint() call away.
	second := connectRoot(t, root)
	first := callText(t, second, "check", map[string]any{})
	if got := strings.Count(first, "events since last compact"); got != 1 {
		t.Fatalf("setup: check must render the advisory exactly once, got %d: %q", got, first)
	}
	next := callText(t, second, "swarm", map[string]any{})
	if strings.Contains(next, "journal") && rootJournalAdvisoryRe.MatchString(next) {
		t.Errorf("crossing already surfaced by check's body was re-nudged on the next call: %q", next)
	}
}

// TestCheckAdvisoryClearsAfterCompact proves the advisory is still
// CONDITIONAL — the composition fix must not turn it into an unconditional
// line that the CI gate would then be filtering away permanently.
func TestCheckAdvisoryClearsAfterCompact(t *testing.T) {
	root := seedCompactDueRoot(t, 20, 25)
	sess := connectRoot(t, root)
	if out := callText(t, sess, "check", map[string]any{}); !strings.Contains(out, "events since last compact") {
		t.Fatalf("setup: the seeded journal must be compact-due: %q", out)
	}

	if out := callText(t, sess, "compact", map[string]any{"apply": true}); !strings.Contains(out, "ok folded") {
		t.Fatalf("compact did not fold the root journal: %q", out)
	}

	out := callText(t, sess, "check", map[string]any{})
	for _, l := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if strings.HasPrefix(l, "c ") {
			t.Errorf("advisory survived a compact: %q in %q", l, out)
		}
	}
	if !strings.Contains(out, "ok check 0 findings (E=0 W=0)") {
		t.Errorf("post-compact check must be the clean summary: %q", out)
	}
}
