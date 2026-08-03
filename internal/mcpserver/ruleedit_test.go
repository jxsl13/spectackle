package mcpserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The tool-layer half of T-01KYD2 / GitHub issue 25: `rule op=edit` used to
// answer ok while storing the old text, whenever the caller supplied EARS slots
// without a pattern. The serializer half (issue 30) is pinned in
// internal/spec/ruleedit_test.go.

// ruleIDOf pulls the rule ID out of an `ok <id> <path>` record.
func ruleIDOf(t *testing.T, out string) string {
	t.Helper()
	for _, l := range strings.Split(out, "\n") {
		f := strings.Fields(l)
		if len(f) >= 2 && f[0] == "ok" && reRuleID.MatchString(f[1]) {
			return f[1]
		}
	}
	t.Fatalf("no `ok <ruleID>` record in %q", out)
	return ""
}

// seedRule adds one Event-pattern rule and returns its ID.
func seedRule(t *testing.T, sess *mcp.ClientSession) string {
	t.Helper()
	out := callText(t, sess, "rule", map[string]any{
		"op": "add", "dir": "", "stem": "EDT-API", "pattern": "E",
		"trigger": "a rule is edited", "system": "serializer",
		"response": "keep one blank line before the next rule",
	})
	return ruleIDOf(t, out)
}

// TestRuleEditSlotsWithoutPatternRecomposes is issue 25's fix: a slot edit with
// no pattern= now recovers the pattern from the stored rule and recomposes,
// where it used to answer `ok` with the text untouched. spec.EditRule's sentence
// fallback kicked in, the write genuinely happened and `Written` was true — so
// `ok` was truthful about the write and a lie about the edit, and the
// `! REJECTED E - nothing was written` branch was simply never reachable.
//
// The full slot set for the recovered pattern is supplied here; a partial set is
// the sibling test below.
func TestRuleEditSlotsWithoutPatternRecomposes(t *testing.T) {
	_, sess := connectRootWithServer(t, t.TempDir())
	id := seedRule(t, sess)

	before := callText(t, sess, "get", map[string]any{"id": id})

	out := callText(t, sess, "rule", map[string]any{
		"op": "edit", "id": id, // no pattern=
		"trigger": "a rule is edited", "system": "serializer",
		"response": "keep exactly one blank line before the next rule",
	})
	if !strings.Contains(out, "ok "+id) {
		t.Fatalf("pattern-less slot edit refused: %q", out)
	}
	after := callText(t, sess, "get", map[string]any{"id": id})
	if after == before {
		t.Fatalf("edit answered ok but stored text is unchanged:\n%s", after)
	}
	if !strings.Contains(after, "keep exactly one blank line") {
		t.Fatalf("the new response did not land: %q", after)
	}
	// the pattern really came from storage: E composes the WHEN clause, and
	// nothing in the call named the pattern.
	if !strings.Contains(after, "WHEN a rule is edited") {
		t.Fatalf("the stored pattern was not recovered — WHEN clause lost: %q", after)
	}
}

// TestRuleEditUbiquitousNeedsNoPattern: for a U rule, system + response IS the
// complete slot set, so the issue-25 call shape that used to degrade silently
// now simply works — the clearest demonstration of the recovery.
func TestRuleEditUbiquitousNeedsNoPattern(t *testing.T) {
	_, sess := connectRootWithServer(t, t.TempDir())
	id := ruleIDOf(t, callText(t, sess, "rule", map[string]any{
		"op": "add", "dir": "", "stem": "EDT-API", "pattern": "U",
		"system": "linter", "response": "report every finding once",
	}))

	out := callText(t, sess, "rule", map[string]any{
		"op": "edit", "id": id,
		"system": "linter", "response": "report every finding exactly once",
	})
	if !strings.Contains(out, "ok "+id) {
		t.Fatalf("pattern-less U edit refused: %q", out)
	}
	got := callText(t, sess, "get", map[string]any{"id": id})
	if !strings.Contains(got, "report every finding exactly once") {
		t.Fatalf("the edit did not land: %q", got)
	}
}

// TestRuleEditPartialSlotsRefuseLoudly: supplying some but not all slots for the
// recovered pattern must name what is missing and write nothing.
//
// This is the deliberate limit of the fix. Slots are composed into the sentence
// and never stored separately, so an unsupplied clause cannot fall back the way
// rationale and applies do without decomposing the stored sentence back into
// slots — and a decomposer that got it subtly wrong would silently store
// something the caller never wrote, which is the exact class of defect issue 25
// is. A refusal naming the missing slot satisfies the brief's requirement
// ("must change the stored text or refuse, never answer ok unchanged") and
// leaves the ergonomics question as its own decision.
func TestRuleEditPartialSlotsRefuseLoudly(t *testing.T) {
	_, sess := connectRootWithServer(t, t.TempDir())
	id := seedRule(t, sess)
	before := callText(t, sess, "get", map[string]any{"id": id})

	for _, args := range []map[string]any{
		{"op": "edit", "id": id, "response": "do something else"},               // no system
		{"op": "edit", "id": id, "system": "serializer", "response": "tighten"}, // E needs a trigger
	} {
		out := callText(t, sess, "rule", args)
		if !strings.Contains(out, "! ARG E") || !strings.Contains(out, "missing slots") {
			t.Fatalf("partial slot set %v was not refused: %q", args, out)
		}
		if after := callText(t, sess, "get", map[string]any{"id": id}); after != before {
			t.Fatalf("refused edit still changed the stored rule:\n%s", after)
		}
	}
}

// TestRuleEditMetadataOnlyStillWorks: rationale-only and applies-only edits
// need no pattern and must keep working — they are what spec.EditRule's
// fallbacks exist for, so the fix must not turn a missing pattern into an error.
func TestRuleEditMetadataOnlyStillWorks(t *testing.T) {
	_, sess := connectRootWithServer(t, t.TempDir())
	id := seedRule(t, sess)

	out := callText(t, sess, "rule", map[string]any{
		"op": "edit", "id": id, "rationale": "layout drift is invisible to review",
	})
	if !strings.Contains(out, "ok "+id) {
		t.Fatalf("rationale-only edit refused: %q", out)
	}
	got := callText(t, sess, "get", map[string]any{"id": id})
	if !strings.Contains(got, "layout drift is invisible to review") {
		t.Fatalf("rationale did not land: %q", got)
	}
	if !strings.Contains(got, "WHEN a rule is edited") {
		t.Fatalf("rationale-only edit lost the sentence: %q", got)
	}

	out = callText(t, sess, "rule", map[string]any{
		"op": "edit", "id": id, "applies": []string{"go:pkg.Fn"},
	})
	if !strings.Contains(out, "ok "+id) {
		t.Fatalf("applies-only edit refused: %q", out)
	}
	got = callText(t, sess, "get", map[string]any{"id": id})
	if !strings.Contains(got, "WHEN a rule is edited") {
		t.Fatalf("applies-only edit lost the sentence: %q", got)
	}
}

// TestRuleEditExplicitPatternStillWins: passing pattern= explicitly keeps
// working and overrides the stored one, so a rule can be re-patterned.
func TestRuleEditExplicitPatternStillWins(t *testing.T) {
	_, sess := connectRootWithServer(t, t.TempDir())
	id := seedRule(t, sess)

	out := callText(t, sess, "rule", map[string]any{
		"op": "edit", "id": id, "pattern": "U",
		"system": "serializer", "response": "emit one canonical layout",
	})
	if !strings.Contains(out, "ok "+id) {
		t.Fatalf("explicit-pattern edit refused: %q", out)
	}
	got := callText(t, sess, "get", map[string]any{"id": id})
	if strings.Contains(got, "WHEN") {
		t.Fatalf("re-patterning to U left the WHEN clause: %q", got)
	}
	if !strings.Contains(got, "emit one canonical layout") {
		t.Fatalf("the new sentence did not land: %q", got)
	}

	// and a bogus pattern is refused by name
	out = callText(t, sess, "rule", map[string]any{
		"op": "edit", "id": id, "pattern": "Z", "system": "x", "response": "y",
	})
	if !strings.Contains(out, "! ARG E") || !strings.Contains(out, "U|E|S|N|O|C") {
		t.Fatalf("invalid pattern not refused clearly: %q", out)
	}
}

// TestRuleEditIDOnlyRefusesWithBaseline pins B-01KYKQA6N1FDQ (found by a
// worktree-batch judge): edit with only id used to rewrite the rule
// unchanged and answer ok — a no-op reported as success. It now refuses,
// teaching the slots AND showing the current sentence so the caller
// learns both in one render.
func TestRuleEditIDOnlyRefusesWithBaseline(t *testing.T) {
	_, sess := connectRootWithServer(t, t.TempDir())
	id := seedRule(t, sess)

	out := callText(t, sess, "rule", map[string]any{"op": "edit", "id": id})
	if !strings.Contains(out, "! ARG E") || !strings.Contains(out, "at least one change") {
		t.Fatalf("id-only edit must refuse loudly: %q", out)
	}
	if !strings.Contains(out, "current: ") || !strings.Contains(out, "WHEN a rule is edited") {
		t.Fatalf("the refusal must show the current sentence: %q", out)
	}
	// the rule is untouched
	got := callText(t, sess, "get", map[string]any{"id": id})
	if !strings.Contains(got, "WHEN a rule is edited") {
		t.Fatalf("the refused edit must not change the rule: %q", got)
	}
}

// TestRuleEditNamesStaleAnchors is B-01KYQ87KTBFVV's VERIFY line: a rule is
// added WITH applies (so an anchor really lands), then its sentence is edited
// WITHOUT applies. Before the fix the whole answer was `ok TGT-EDT-001
// .spectackle/spec.md` and nothing else, while every anchor for the rule had
// just moved into drift.Tightened — a state the same server's `check` audits
// and `move to=done` refuses, armed with zero signal at the moment it was
// armed.
//
// Three halves are pinned here on purpose:
//
//  1. the notice fires, names the node, and prints the re-stamp call;
//  2. the edit still SUCCEEDS (`ok`) and `check` still audits the anchor —
//     the fix must not auto-heal, because a rule/code divergence is exactly
//     when a human should look (this is the refutation of option (a), and
//     TestCheckAuditsTightenedNeverHeals / TestMoveGateBlocksDoneOnTightenedAnchor
//     go red under an auto-re-stamp);
//  3. the call the notice PRINTS is re-issued verbatim and actually clears
//     the audit — a suggested remedy that does not work is worse than none.
func TestRuleEditNamesStaleAnchors(t *testing.T) {
	root := t.TempDir()
	src := "package demo\n\nfunc F() int {\n\treturn 1\n}\n"
	if err := os.WriteFile(filepath.Join(root, "demo.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	sess := connectRoot(t, root)

	out := callText(t, sess, "rule", map[string]any{
		"op": "add", "dir": "", "pattern": "U", "stem": "TGT-EDT",
		"system":   "the stale-anchor test workspace",
		"response": "always return the constant 1 from F",
		"applies":  []string{"go:demo.F"},
	})
	if !strings.Contains(out, "a TGT-EDT-001 go:demo.F") {
		t.Fatalf("the anchor did not stamp, so the rest of this test proves nothing: %q", out)
	}

	// the defect's trigger: a response-only edit, no applies.
	out = callText(t, sess, "rule", map[string]any{
		"op": "edit", "id": "TGT-EDT-001", "pattern": "U",
		"system":   "the stale-anchor test workspace",
		"response": "always return the constant 2 from F",
	})
	// regression guard: the write happened and is still reported as success.
	if !strings.Contains(out, "ok TGT-EDT-001") {
		t.Fatalf("the sentence edit must still succeed: %q", out)
	}
	if !strings.Contains(out, "! DRIFT W TGT-EDT-001") {
		t.Fatalf("expected the stale-anchor notice: %q", out)
	}
	if !strings.Contains(out, "go:demo.F") {
		t.Fatalf("the notice must NAME the stale anchor: %q", out)
	}
	if !strings.Contains(out, `"applies":["go:demo.F"]`) {
		t.Fatalf("the notice must print the re-stamp call verbatim: %q", out)
	}

	// the fix must NOT auto-heal: check still audits the tightened anchor.
	out = callText(t, sess, "check", map[string]any{})
	if !strings.Contains(out, "d audit TGT-EDT-001 go:demo.F") || !strings.Contains(out, "tightened") {
		t.Fatalf("the notice must not heal the drift away — check must still audit it: %q", out)
	}

	// re-issue the printed call verbatim; it is what the notice promised.
	out = callText(t, sess, "rule", map[string]any{
		"op": "edit", "id": "TGT-EDT-001", "applies": []string{"go:demo.F"},
	})
	if !strings.Contains(out, "a TGT-EDT-001 go:demo.F") {
		t.Fatalf("the printed re-stamp call did not re-stamp: %q", out)
	}
	out = callText(t, sess, "check", map[string]any{})
	if strings.Contains(out, "d audit TGT-EDT-001") {
		t.Fatalf("the printed re-stamp call did not clear the audit: %q", out)
	}
}

// TestRuleEditWithAppliesEmitsNoStaleNotice is the no-false-positive half of
// B-01KYQ87KTBFVV. A notice that fires on every edit is ambient noise, and
// noise on the most frequent rule write is exactly what the omit-if-empty
// discipline elsewhere in this surface exists to avoid. Two shapes must stay
// silent: a sentence edit that DOES carry applies (it re-stamps, so nothing
// is stale), and a rationale-only edit on a rule that has no anchors at all.
func TestRuleEditWithAppliesEmitsNoStaleNotice(t *testing.T) {
	root := t.TempDir()
	src := "package demo\n\nfunc F() int {\n\treturn 1\n}\n"
	if err := os.WriteFile(filepath.Join(root, "demo.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	sess := connectRoot(t, root)

	callText(t, sess, "rule", map[string]any{
		"op": "add", "dir": "", "pattern": "U", "stem": "QUIET",
		"system":   "the quiet-notice test workspace",
		"response": "always return the constant 1 from F",
		"applies":  []string{"go:demo.F"},
	})

	// sentence edit WITH applies: stamps, so there is nothing stale to say.
	out := callText(t, sess, "rule", map[string]any{
		"op": "edit", "id": "QUIET-001", "pattern": "U",
		"system":   "the quiet-notice test workspace",
		"response": "always return the constant 2 from F",
		"applies":  []string{"go:demo.F"},
	})
	if !strings.Contains(out, "a QUIET-001 go:demo.F") {
		t.Fatalf("the re-stamp leg must still stamp: %q", out)
	}
	if strings.Contains(out, "! DRIFT W") {
		t.Fatalf("an edit that re-stamped its own anchors must stay quiet: %q", out)
	}

	// rationale-only edit on an ANCHORED rule whose anchors are fresh: this
	// one really does take the notice path (no applies passed), so it is the
	// assertion that pins the RHash comparison. Without it the notice could
	// name every anchor of every edited rule and still look correct.
	out = callText(t, sess, "rule", map[string]any{
		"op": "edit", "id": "QUIET-001", "rationale": "constants are cheaper to verify",
	})
	if !strings.Contains(out, "ok QUIET-001") {
		t.Fatalf("rationale-only edit refused: %q", out)
	}
	if strings.Contains(out, "! DRIFT W") {
		t.Fatalf("an edit that did not change the sentence has nothing stale to report: %q", out)
	}

	// rationale-only edit on an anchorless rule: nothing stamped, nothing stale.
	callText(t, sess, "rule", map[string]any{
		"op": "add", "dir": "", "pattern": "U", "stem": "BARE",
		"system": "the quiet-notice test workspace", "response": "stay anchorless",
	})
	out = callText(t, sess, "rule", map[string]any{
		"op": "edit", "id": "BARE-001", "rationale": "recorded after the fact",
	})
	if !strings.Contains(out, "ok BARE-001") {
		t.Fatalf("rationale-only edit refused: %q", out)
	}
	if strings.Contains(out, "! DRIFT W") {
		t.Fatalf("a rule with no anchors can have no stale ones: %q", out)
	}
}
