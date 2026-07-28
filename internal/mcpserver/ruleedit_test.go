package mcpserver

import (
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
