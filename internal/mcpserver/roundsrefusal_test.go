package mcpserver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jxsl13/spectackle/internal/journal"
)

// This file pins the two rounds-exhausted routes the original census missed.
// The move route (tools.go) was fixed first and is pinned by
// TestRoundsExhaustedRefusesInsteadOfReportingSuccess; a later sweep measured
// the SAME defect still live on the failing-verdict route (validate.go) and on
// both swarm submit routes — every one of them returning text(), i.e. exit 0,
// with an `i <id> blocked ...` record line for a state the caller never
// requested and a prose option list instead of a callable decide object
// (T-01KYQ503AGE6T).
//
// It lives in its own file deliberately: the move-route work is in tools.go
// and its tests in knowledge_test.go, and keeping these file-disjoint means a
// concurrent wave on that file cannot collide with this one.

// callRaw is callText that KEEPS res.IsError. callText (tools_test.go)
// discards it, which is exactly why the move-route test can see the prose half
// of a refusal and not the exit-code half — a site that returns text() with
// perfectly refusal-shaped prose passes every callText assertion while the CLI
// still exits 0 and a driver keying its retry on the exit code never sees it.
func callRaw(t *testing.T, sess *mcp.ClientSession, name string, args map[string]any) (string, bool) {
	t.Helper()
	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if len(res.Content) == 0 {
		t.Fatalf("%s: empty content", name)
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("%s: content is %T, want TextContent", name, res.Content[0])
	}
	return tc.Text, res.IsError
}

// assertRoundsRefusal is the whole contract of a rounds-exhausted escalation,
// asserted on what the CALLER observes. All four halves are checked because
// the defect had all four at once and any one of them alone would let a judge
// read the escalation as a success:
//
//   - IsError, which is what the call subcommand's exit code derives from;
//   - the refused destination named BEFORE the state that is now true;
//   - no `i ` record line — a state the caller never requested must not be
//     rendered in the shape a successful move renders. This is the
//     load-bearing half: a test checking only the new wording, or only the
//     exit code, passes with the old line still there;
//   - a callable decide object rather than a prose list of choices.
func assertRoundsRefusal(t *testing.T, route, out string, isErr bool) {
	t.Helper()
	if !isErr {
		t.Errorf("%s: a forced block returned success (IsError=false) — the exit code still says the call worked:\n%s", route, out)
	}
	if !strings.Contains(out, "! ROUNDS E") {
		t.Fatalf("%s: no rounds refusal record:\n%s", route, out)
	}
	refused, blocked := strings.Index(out, "REFUSED"), strings.Index(out, "item is now blocked")
	if refused < 0 || blocked < 0 || refused > blocked {
		t.Errorf("%s: the refusal must say what did NOT happen before what is now true:\n%s", route, out)
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "i ") {
			t.Errorf("%s: a refused call must not render a record line: %q\n%s", route, line, out)
		}
		if strings.HasPrefix(line, "ok ") {
			t.Errorf("%s: a forced block must not report ok: %q\n%s", route, line, out)
		}
	}
	if !strings.Contains(out, `decide {"op":"answer"`) || !strings.Contains(out, `"choose"`) {
		t.Errorf("%s: the refusal must hand back a callable decide object, not a prose list:\n%s", route, out)
	}
}

// adrIn pulls the escalation ADR's ID out of a refusal. It sits INSIDE the
// callable JSON object, so it is not a whitespace-delimited field: scan for
// the prefix and take the run of ID characters after it (same shape as the
// helper in decide_test.go).
func adrIn(s string) string {
	i := strings.Index(s, "ADR-")
	if i < 0 {
		return ""
	}
	j := i + len("ADR-")
	for j < len(s) && (s[j] >= '0' && s[j] <= '9' || s[j] >= 'A' && s[j] <= 'Z') {
		j++
	}
	return s[i:j]
}

// TestValidateVerdictRoundsExhaustedRefuses covers validate.go's
// failing-verdict route, which the code's own comment calls arguably the
// commoner way into blocked: a failing verdict on a done item reopens it
// through lifecycle.Move, and when that reopen exhausts the budget the handler
// renders its own escalation. It rendered
//
//	ok validate T-… fail by judge
//	i T-… blocked rounds exhausted — decide ADR-… (rescope|reject|override-once)
//
// at exit 0 — a literal leading `ok`, a record line for a state nobody
// requested, and a prose option list, on a call that had just FORCE-BLOCKED
// the item.
//
// The route differs from the move route in one way that matters: the verdict
// itself DID land. So this test also asserts the verdict is still journaled —
// a refusal that rolled it back, or that read as though nothing happened,
// would make the caller re-submit a verdict that is already recorded, which is
// the same lie pointing the other way.
func TestValidateVerdictRoundsExhaustedRefuses(t *testing.T) {
	root := t.TempDir()
	sess := connectRoot(t, root)
	id := draftID(t, sess, map[string]any{
		"kind": "task", "title": "verdict route escalation", "body": "a body of ordinary length",
	})
	// Spend the budget to one round short of the limit through plain moves,
	// leave the item in done, and let the failing verdict's OWN done->active
	// reopen trip it. Driving the last hop with `move` instead lands in
	// roundsRefusal via tools.go and never enters the validate handler at all.
	callText(t, sess, "move", map[string]any{"id": id, "to": "done"})
	for i := 0; i < 2; i++ {
		callText(t, sess, "move", map[string]any{"id": id, "to": "active"})
		callText(t, sess, "move", map[string]any{"id": id, "to": "done"})
	}
	callText(t, sess, "validate", map[string]any{"id": id, "agent": "judge"})
	out, isErr := callRaw(t, sess, "validate", map[string]any{
		"id": id, "op": "verdict", "agent": "judge", "pass": false,
		"findings": "a findings string long enough to clear the eighty character tripwire about token-thin validations",
	})
	if !strings.Contains(out, "rounds exhausted") {
		t.Fatalf("validate route never escalated:\n%s", out)
	}
	assertRoundsRefusal(t, "validate verdict", out, isErr)

	// The verdict is the half that DID happen; say so, and prove it on the
	// journal rather than on the prose, because prose is what was wrong here.
	events := mainJournal(t, root)
	landed := false
	for _, e := range events {
		if e.Ev == journal.EvValidate && e.Op == "verdict" && strings.HasPrefix(e.ID, id) && !e.Pass {
			landed = true
		}
	}
	if !landed {
		t.Errorf("the failing verdict must still be journaled — the refusal is about the reopen, not the verdict")
	}
	if !strings.Contains(out, "RECORDED") {
		t.Errorf("the refusal must separate the recorded verdict from the refused reopen:\n%s", out)
	}

	// and the object it handed back has to actually work
	adr := adrIn(out)
	if adr == "" {
		t.Fatalf("the refusal must name the decision:\n%s", out)
	}
	if got := callText(t, sess, "decide", map[string]any{"op": "answer", "id": adr, "choose": "rescope"}); strings.Contains(got, "! ") {
		t.Fatalf("the handed-back call must work: %q", got)
	}
}

// blockedByGateFail drives an approved item into a worktree and submits it
// until the gate has burned the whole feedback-round budget, leaving the item
// in blocked with its worktree still open. It returns the session plus the
// text and IsError of the ESCALATING submit — the one call whose exit code is
// the subject of the gateFail test. gitRoot's configured verify is
// `test -f ok.txt`, so simply never creating that file fails GATE 1 every
// time.
func blockedByGateFail(t *testing.T) (sess *mcp.ClientSession, root, id, last string, lastErr bool) {
	t.Helper()
	root = gitRoot(t)
	t.Setenv("SPECTACKLE_AGENT", "alice")
	sess = connectRoot(t, root)
	id = draftID(t, sess, map[string]any{
		"kind": "proposal", "title": "gate route escalation", "targets": []string{"main.go"}})
	callText(t, sess, "move", map[string]any{"id": id, "to": "submitted"})
	callText(t, sess, "move", map[string]any{"id": id, "to": "approved"})
	if out := callText(t, sess, "work", map[string]any{"op": "start", "item": id}); !strings.Contains(out, "wt "+id+" open ") {
		t.Fatalf("setup: work start: %s", out)
	}
	if _, err := os.Stat(filepath.Join(root, "ok.txt")); err == nil {
		t.Fatalf("setup: the gate is supposed to fail; ok.txt must not exist")
	}
	// Stop on the ITEM'S STATE flipping to blocked, never on the refusal text,
	// and never on wall-clock time (that flake class was filed twice here,
	// B-01KYQG88GZEM2). The distinction is load-bearing: keying the loop on
	// "ROUNDS E" made a mutant that reverts gateFail alone run one submit too
	// far, so the caller observed the ALREADY-BLOCKED refusal from the other
	// site and the gateFail assertions were silently pointed at the wrong call.
	// The state flip identifies the escalating submit exactly — it is the call
	// that caused it — so `last` is always that call's own result.
	blocked := false
	for i := 0; i < 6 && !blocked; i++ {
		last, lastErr = callRaw(t, sess, "work", map[string]any{"op": "submit"})
		blocked = strings.Contains(callText(t, sess, "get", map[string]any{"id": id}), id+" proposal blocked")
	}
	if !blocked {
		t.Fatalf("setup: the gate never exhausted the budget:\n%s", last)
	}
	return sess, root, id, last, lastErr
}

// TestSwarmGateFailRoundsExhaustedRefuses covers swarm.go's gateFail. A gate
// failure counts against the same server-counted feedback budget as a reopen,
// and at the limit the item is force-escalated into blocked — but the handler
// answered `i <id> blocked rounds=n/n — decide <adr>` through text(), exit 0.
// The submit did not run, nothing was integrated, and the item's state was
// changed out from under the caller; that is a refusal by every measure the
// move route is now held to.
func TestSwarmGateFailRoundsExhaustedRefuses(t *testing.T) {
	sess, _, _, out, isErr := blockedByGateFail(t)
	assertRoundsRefusal(t, "work op=submit gate fail", out, isErr)
	// the round counts the old `i` line carried are still available, just not
	// in a shape that can be mistaken for a record of a successful move
	if !strings.Contains(out, "gate failed on round") {
		t.Errorf("the refusal must still name the spent budget:\n%s", out)
	}
	// and the object it handed back has to actually work
	adr := adrIn(out)
	if adr == "" {
		t.Fatalf("the refusal must name the decision:\n%s", out)
	}
	if got := callText(t, sess, "decide", map[string]any{"op": "answer", "id": adr, "choose": "rescope"}); strings.Contains(got, "! ") {
		t.Fatalf("the handed-back call must work: %q", got)
	}
}

// TestSwarmSubmitOnBlockedRefuses covers the OTHER swarm site: a submit issued
// against an item a previous escalation already blocked. The gate deliberately
// stays unrun (it would only pile more rounds onto a spent budget), so nothing
// whatsoever happens — and it still answered `i <id> blocked rounds=n/n` at
// exit 0, while its three immediate neighbours in the same function already
// refuse(). Plain internal inconsistency on top of a misreadable shape.
func TestSwarmSubmitOnBlockedRefuses(t *testing.T) {
	sess, _, _, _, _ := blockedByGateFail(t)
	out, isErr := callRaw(t, sess, "work", map[string]any{"op": "submit"})
	assertRoundsRefusal(t, "work op=submit on blocked", out, isErr)
	if !strings.Contains(out, "the gate was not run") {
		t.Errorf("a submit that runs nothing must say so:\n%s", out)
	}
}
