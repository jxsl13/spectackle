package mcpserver

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jxsl13/spectackle/internal/item"
	"github.com/jxsl13/spectackle/internal/journal"
	"github.com/jxsl13/spectackle/internal/lifecycle"
)

// registerDecide wires the decide tool onto s for tests only. Production
// registration into tools.go's registerTools() is a separate task's scope
// (see decide.go's top-of-file comment) — this repo currently has no other
// way to drive decide over the wire.
func registerDecide(s *Server) {
	mcp.AddTool(s.mcp, &mcp.Tool{Name: "decide", Description: "test-only registration"},
		func(ctx context.Context, req *mcp.CallToolRequest, in decideIn) (*mcp.CallToolResult, any, error) {
			s.mu.Lock()
			defer s.mu.Unlock()
			if err := s.preCall(); err != nil {
				return nil, nil, err
			}
			res, out, err := s.decide(ctx, req, in)
			return s.postCall(res), out, err
		})
}

// connectDecide spins up a server with decide wired in and connects a
// client. elicit == nil reproduces the headless / "different harness" case
// documented for decide op=ask: the in-memory client advertises no
// elicitation capability, so req.Session.Elicit errors and decideAsk falls
// back to the need-decision record instead of blocking.
func connectDecide(t *testing.T, root string, elicit func(context.Context, *mcp.ElicitRequest) (*mcp.ElicitResult, error)) (*Server, *mcp.ClientSession) {
	t.Helper()
	s, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	registerDecide(s)
	t.Cleanup(func() { _ = s.Close() })
	ct, st := mcp.NewInMemoryTransports()
	go func() {
		if err := s.MCP().Run(context.Background(), st); err != nil {
			t.Logf("server stopped: %v", err)
		}
	}()
	var copts *mcp.ClientOptions
	if elicit != nil {
		copts = &mcp.ClientOptions{ElicitationHandler: elicit}
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, copts)
	sess, err := client.Connect(context.Background(), ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sess.Close() })
	return s, sess
}

// TestDecideAskHeadlessNeedPath: an in-memory client with no elicitation
// handler cannot render the native UI — req.Session.Elicit errors, so
// decideAsk must NOT block; it leaves the ADR-item open (state=submitted) and
// returns the "need decision" record so the orchestrator can keep working
// other disjoint tasks.
func TestDecideAskHeadlessNeedPath(t *testing.T) {
	root := t.TempDir()
	s, sess := connectDecide(t, root, nil)

	adr := askID(t, sess, map[string]any{
		"op": "ask", "question": "which backend?", "options": []string{"grpc", "rest"},
	})

	d, ok, err := item.Get(s.ws, fullID(t, s, adr))
	if err != nil || !ok {
		t.Fatalf("%s not persisted: %v %v", adr, ok, err)
	}
	if d.State != item.StateSubmitted {
		t.Fatalf("undelivered decision state = %s, want submitted", d.State)
	}

	out := callText(t, sess, "decide", map[string]any{"op": "ls"})
	if !strings.Contains(out, adr) {
		t.Fatalf("ls should list the still-open decision: %q", out)
	}
}

// TestDecideAskAcceptResolvesImmediately: with an elicitation handler that
// accepts, the decision resolves in the same call — no separate answer
// round-trip needed.
func TestDecideAskAcceptResolvesImmediately(t *testing.T) {
	root := t.TempDir()
	s, sess := connectDecide(t, root, func(_ context.Context, req *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
		return &mcp.ElicitResult{Action: "accept", Content: map[string]any{"choice": "rest"}}, nil
	})

	out := callText(t, sess, "decide", map[string]any{
		"op": "ask", "question": "which backend?", "options": []string{"grpc", "rest"},
	})
	f := strings.Fields(out)
	if len(f) < 3 || f[0] != "ok" || !idPrefixRe.MatchString(f[1]) || f[2] != "rest" {
		t.Fatalf("accepted ask should resolve immediately: %q", out)
	}
	adr := f[1]
	d, ok, err := item.Get(s.ws, fullID(t, s, adr))
	if err != nil || !ok || d.State != item.StateDone {
		t.Fatalf("%s not done: %+v %v %v", adr, d, ok, err)
	}
}

// TestDecideAnswerResolvesAndClearsNeeds: ask op=ask with item=<id> links
// the minted decision into that item's Needs; a later, separate answer=
// call (simulating "from any session, any time") resolves it, clears the
// Need and journals ev=decide.
func TestDecideAnswerResolvesAndClearsNeeds(t *testing.T) {
	root := t.TempDir()
	s, sess := connectDecide(t, root, nil)

	task := draftID(t, sess, map[string]any{"kind": "task", "title": "ship the new backend"})
	adr := askID(t, sess, map[string]any{
		"op": "ask", "question": "which backend?", "kind": "radio",
		"options": []string{"grpc", "rest"}, "item": task,
	})

	// resolve both to their stored IDs NOW: a displayed prefix is unique only
	// against the record set at the moment it was emitted, and this test mints
	// a second ADR below whose tail shares the first one's leading run.
	taskFull, adrFull := fullID(t, s, task), fullID(t, s, adr)
	blocked, ok, err := item.Get(s.ws, taskFull)
	if err != nil || !ok || len(blocked.Needs) != 1 || blocked.Needs[0] != adrFull {
		t.Fatalf("%s Needs not linked to %s: %+v %v", task, adr, blocked, err)
	}

	out := callText(t, sess, "decide", map[string]any{"op": "answer", "id": adr, "choose": "grpc"})
	if !strings.Contains(out, "ok "+adr+" grpc") {
		t.Fatalf("answer should resolve: %q", out)
	}

	// a second, confirm-kind decision on the same item: its stored options
	// (forced to yes/no regardless of what ask was called with) must
	// actually constrain answer=
	adr2 := askID(t, sess, map[string]any{"op": "ask", "question": "yes or no?", "kind": "confirm", "item": task})
	out = callText(t, sess, "decide", map[string]any{"op": "answer", "id": adr2, "choose": "maybe"})
	if !strings.Contains(out, "! ARG E") {
		t.Fatalf("confirm answer outside yes|no must be rejected: %q", out)
	}

	resolved, ok, err := item.Get(s.ws, taskFull)
	if err != nil || !ok {
		t.Fatalf("%s gone: %v", task, err)
	}
	if len(resolved.Needs) != 1 || resolved.Needs[0] != fullID(t, s, adr2) {
		t.Fatalf("%s should be cleared from Needs, %s still open: %+v", adr, adr2, resolved.Needs)
	}

	events, err := journal.ReadAll(s.ws)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range events {
		if e.Ev == journal.EvDecide && e.ID == adrFull && e.Note == "grpc" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no ev=decide journal event for %s's resolution", adr)
	}
}

// TestDecideAskArchivedItemProvenanceOnly: decide op=ask item=<archived id>
// must succeed, keep 'blocks: <id>' in the new decision's body as
// provenance, and leave the archived item strictly untouched — there is no
// work.md home for it to receive a Needs backlink write.
func TestDecideAskArchivedItemProvenanceOnly(t *testing.T) {
	root := t.TempDir()
	s, sess := connectDecide(t, root, nil)

	it, err := lifecycle.Draft(s.ws, s.minter(), "task", "old work", "", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lifecycle.Move(s.ws, it.ID, item.StateArchived, "shipped"); err != nil {
		t.Fatal(err)
	}

	adr := askID(t, sess, map[string]any{
		"op": "ask", "question": "still relevant?", "kind": "confirm", "item": it.ID,
	})

	d, ok, err := item.Get(s.ws, fullID(t, s, adr))
	if err != nil || !ok {
		t.Fatalf("%s not persisted: %v %v", adr, ok, err)
	}
	if !strings.Contains(d.Body, "blocks: "+it.ID) {
		t.Fatalf("%s body missing blocks provenance: %q", adr, d.Body)
	}

	// the archived item itself is gone from work.md and Tombstone must still
	// report it as archived, unmodified by the ask (no Needs write anywhere:
	// there was never a work.md record to write it into).
	if _, ok, _ := item.Get(s.ws, it.ID); ok {
		t.Fatalf("archived item reappeared in work.md: %s", it.ID)
	}
	tomb, tombOk, err := lifecycle.Tombstone(s.ws, it.ID)
	if err != nil || !tombOk || len(tomb.Needs) != 0 {
		t.Fatalf("archived item tombstone unexpectedly carries Needs: %+v %v %v", tomb, tombOk, err)
	}
}

// TestDecideOptionFidelityAndLegacyCompat: options containing commas must
// round-trip byte-identical through ask -> answer (MCP-001's one `option: `
// line per option, not the old comma-joined `options: ` line which would
// shatter them); a legacy comma-joined body (as decideAsk used to write) must
// remain answerable via its comma-split fragments without any migration.
func TestDecideOptionFidelityAndLegacyCompat(t *testing.T) {
	root := t.TempDir()
	s, sess := connectDecide(t, root, nil)

	adr := askID(t, sess, map[string]any{
		"op": "ask", "question": "pick one", "kind": "radio",
		"options": []string{"defer, revisit later", "go now"},
	})
	d, ok, err := item.Get(s.ws, fullID(t, s, adr))
	if err != nil || !ok {
		t.Fatalf("%s not persisted: %v %v", adr, ok, err)
	}
	if !strings.Contains(d.Body, "option: defer, revisit later\n") || !strings.Contains(d.Body, "option: go now") {
		t.Fatalf("%s body missing per-line option: %q", adr, d.Body)
	}
	if strings.Contains(d.Body, "options: ") {
		t.Fatalf("%s body still uses the legacy comma-joined line: %q", adr, d.Body)
	}

	out := callText(t, sess, "decide", map[string]any{
		"op": "answer", "id": adr, "choose": "defer, revisit later",
	})
	if !strings.Contains(out, "ok "+adr+" defer, revisit later") {
		t.Fatalf("byte-identical comma-containing option should resolve: %q", out)
	}

	// legacy body shape (comma-joined) must still be answerable, unmigrated.
	legacy, err := lifecycle.Draft(s.ws, s.minter(), "adr", "legacy ask",
		"kind: radio\noptions: alpha, beta", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lifecycle.Move(s.ws, legacy.ID, item.StateSubmitted, ""); err != nil {
		t.Fatal(err)
	}
	out = callText(t, sess, "decide", map[string]any{"op": "answer", "id": legacy.ID, "choose": "beta"})
	if !strings.Contains(out, "ok "+shortID(t, s, legacy.ID)+" beta") {
		t.Fatalf("legacy comma-split body should still be answerable: %q", out)
	}
}

// TestDecideAskStoresContextAndProposedStatus: op=ask with a context= arg
// mints an ADR whose Context field carries it verbatim and whose Status
// starts at "proposed" — the classic ADR template fields, not the lifecycle
// State (which is "submitted" here, per the headless need-decision path).
func TestDecideAskStoresContextAndProposedStatus(t *testing.T) {
	root := t.TempDir()
	s, sess := connectDecide(t, root, nil)

	adr := askID(t, sess, map[string]any{
		"op": "ask", "question": "which backend?", "options": []string{"grpc", "rest"},
		"context": "Latency-sensitive service; current REST gateway is the bottleneck.",
	})

	d, ok, err := item.Get(s.ws, fullID(t, s, adr))
	if err != nil || !ok {
		t.Fatalf("%s not persisted: %v %v", adr, ok, err)
	}
	if d.Context != "Latency-sensitive service; current REST gateway is the bottleneck." {
		t.Fatalf("Context not stored: %+v", d)
	}
	if d.Status != "proposed" {
		t.Fatalf("Status = %q, want proposed", d.Status)
	}
	if d.State != item.StateSubmitted {
		t.Fatalf("lifecycle State = %s, want submitted", d.State)
	}
	if d.Decision != "" {
		t.Fatalf("Decision should be empty before an answer: %+v", d)
	}
}

// TestDecideAnswerRecordsDecisionStatusAndConsequences: op=answer sets
// Decision to the chosen option, Status to "accepted", and stores an
// optional consequences= argument verbatim — all while leaving the existing
// choice/state/journal/needs-clearing behavior untouched.
func TestDecideAnswerRecordsDecisionStatusAndConsequences(t *testing.T) {
	root := t.TempDir()
	s, sess := connectDecide(t, root, nil)

	adr := askID(t, sess, map[string]any{
		"op": "ask", "question": "which backend?", "options": []string{"grpc", "rest"},
		"context": "Latency-sensitive service.",
	})
	out := callText(t, sess, "decide", map[string]any{
		"op": "answer", "id": adr, "choose": "grpc",
		"consequences": "Clients must add a gRPC dependency; REST gateway is deprecated over two releases.",
	})
	if !strings.Contains(out, "ok "+adr+" grpc") {
		t.Fatalf("answer should resolve: %q", out)
	}

	d, ok, err := item.Get(s.ws, fullID(t, s, adr))
	if err != nil || !ok {
		t.Fatalf("%s not persisted: %v %v", adr, ok, err)
	}
	if d.Decision != "grpc" {
		t.Fatalf("Decision = %q, want grpc", d.Decision)
	}
	if d.Status != "accepted" {
		t.Fatalf("Status = %q, want accepted", d.Status)
	}
	if d.Consequences != "Clients must add a gRPC dependency; REST gateway is deprecated over two releases." {
		t.Fatalf("Consequences not stored: %+v", d)
	}
	// Context set at ask-time survives the answer unmodified.
	if d.Context != "Latency-sensitive service." {
		t.Fatalf("Context lost across answer: %+v", d)
	}
	// pre-existing behavior untouched: state=done, choice line in body.
	if d.State != item.StateDone || !strings.Contains(d.Body, "choice: grpc") {
		t.Fatalf("existing choice/state behavior regressed: %+v", d)
	}
}

// TestDecideBlockedOverrideOnce: a pre-escalated item (item.StateBlocked,
// minted the way lifecycle.Escalate does when work op=submit's gate-fail
// rounds are exhausted — see swarm.go gateFail) resolves via decide
// op=answer choose=override-once, exactly the same path lifecycle's own
// TestResolveBlockedOutcomes exercises directly against ResolveBlocked.
// Escalation itself is out of this task's scope; this test sets it up with
// lifecycle.Escalate directly (bypassing the wire) and then drives the
// resolution through decide, which is in scope.
func TestDecideBlockedOverrideOnce(t *testing.T) {
	root := t.TempDir()
	s, sess := connectDecide(t, root, nil)
	s.ws.Cfg.Feedback.MaxRounds = 1

	it, err := lifecycle.Draft(s.ws, s.minter(), "task", "flaky feedback loop", "", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lifecycle.Move(s.ws, it.ID, item.StateDone, ""); err != nil {
		t.Fatal(err)
	}
	_, err = lifecycle.Move(s.ws, it.ID, item.StateActive, "")
	var exhausted lifecycle.ErrRoundsExhausted
	if !errors.As(err, &exhausted) {
		t.Fatalf("setup: expected ErrRoundsExhausted, got %v", err)
	}
	blocked, decision, err := lifecycle.Escalate(s.ws, s.minter(), exhausted.Item)
	if err != nil {
		t.Fatal(err)
	}
	// The escalation hint is the EXACT callable invocation
	// (B-01KYKEWMHEFW1: a judge following the old "decide <task-id>
	// outcome=..." text failed twice — no outcome field exists and the
	// target is the ADR). Pin: op=answer, the ADR's SHORT id, choose=.
	adr, ok, _ := item.Get(s.ws, decision.ID)
	if !ok ||
		!strings.Contains(adr.Body, "decide op=answer id="+shortDisplayID(decision.ID)+" choose=") ||
		strings.Contains(adr.Body, "outcome=") {
		t.Fatalf("escalation hint must name the callable decide invocation:\n%s", adr.Body)
	}
	// AND the parser must agree with the text above. The check above pinned that
	// the WRITER changed from outcome= to choose=, and nothing pinned that the
	// READER followed. It did not — the regex lived in two packages and was
	// updated in neither, so every escalation ADR accepted free text and a typo
	// stranded its item forever (B-01KYS7111XFHZ). Verifying one half of a
	// two-sided contract is how that survived.
	//
	// Scope of this assertion, stated because measuring it is the only way to
	// know: it does NOT individually kill either mutation. Escalate now carries
	// the options twice — the `choose=` prose and the `option:` lines — so
	// deleting one leaves the other parseable and this stays green either way.
	// The individual pins are elsewhere and were mutation-checked there:
	// lifecycle's TestEscalateBodyIsAnswerableByConstruction strips the prose
	// line, and item's TestParseOptionsAcceptsBothEscalationSpellings feeds a
	// prose-only body. What THIS adds is the end-to-end property at the boundary
	// that actually answers decisions: whatever Escalate writes, this package
	// can parse.
	if opts := item.ParseOptions(adr.Body); len(opts) != 3 {
		t.Fatalf("escalation body is not answerable: ParseOptions = %v, want 3 outcomes\n%s", opts, adr.Body)
	}
	if blocked.State != item.StateBlocked {
		t.Fatalf("setup: item not blocked: %+v", blocked)
	}

	out := callText(t, sess, "decide", map[string]any{
		"op": "answer", "id": decision.ID, "choose": "override-once",
	})
	if !strings.Contains(out, "ok "+shortID(t, s, decision.ID)+" override-once") {
		t.Fatalf("override-once answer: %q", out)
	}

	resolved, ok, err := item.Get(s.ws, it.ID)
	if err != nil || !ok {
		t.Fatalf("item gone: %v %v", ok, err)
	}
	if resolved.State != item.StateActive || resolved.Rounds != 0 || !resolved.Override {
		t.Fatalf("override-once did not resolve the block: %+v", resolved)
	}

	d, ok, err := item.Get(s.ws, decision.ID)
	if err != nil || !ok || d.State != item.StateDone {
		t.Fatalf("decision item not resolved: %+v %v", d, err)
	}

	// a rejected choose outside the decision's stored options (rescope,
	// reject, override-once, carried by lifecycle.Escalate's `option:` lines
	// and its `choose=` sentence — NOT the `outcome=` spelling this comment
	// used to name, which no writer has produced since the text was changed) is
	// refused before ResolveBlocked ever sees it — override-once is one-shot
	// and would otherwise error there too, but the option-set check catches
	// nonsense earlier and with a clearer message.
	out = callText(t, sess, "decide", map[string]any{
		"op": "answer", "id": decision.ID, "choose": "override-once",
	})
	if !strings.Contains(out, "! ARG E") {
		t.Fatalf("answering an already-done decision again must be rejected: %q", out)
	}
}

// TestBlockedExitAdvertisesOnlyLiveOutcomes pins what three rounds of this
// record kept getting wrong: a caller-facing message must advertise the
// outcomes the parser will ACCEPT, never a fixed list. override-once is
// one-shot, so a second escalation offers only rescope|reject — and every
// hard-coded copy kept naming override-once, which the parser then refused at
// exit 1, costing the caller a call to discover (B-01KYS7111XFHZ).
//
// A verifier mutation-tested the fix and found NOTHING pinned it: re-hard-coding
// the literal, bypassing the renderer, and dropping the unmapped-outcome
// fallthrough all survived the suite. That is the gap this test closes, and
// RECMERGE-003 is why it is not optional.
func TestBlockedExitAdvertisesOnlyLiveOutcomes(t *testing.T) {
	// A second escalation: override-once already spent.
	got := roundsRefusal("T-1", "active", "ADR-1", []string{"rescope", "reject"})
	if !strings.Contains(got, `"choose":"rescope|reject"`) {
		t.Errorf("refusal does not advertise the live set:\n%s", got)
	}
	if strings.Contains(got, "override-once") {
		t.Errorf("refusal advertises override-once, which this ADR's parser refuses:\n%s", got)
	}
	// A first escalation still offers all three.
	got = roundsRefusal("T-1", "active", "ADR-1", []string{"rescope", "reject", "override-once"})
	if !strings.Contains(got, `"choose":"rescope|reject|override-once"`) {
		t.Errorf("first escalation must offer all three:\n%s", got)
	}
	// An outcome with no effect mapping must still be OFFERED and must not
	// vanish from the effects list — it used to be silently dropped, and an
	// all-unmapped set rendered a bare "()".
	got = roundsRefusal("T-1", "active", "ADR-1", []string{"rescope", "defer-to-human"})
	// Check the EFFECTS list specifically. Asserting on the whole string passed
	// even with the fallthrough deleted, because the value still appears in the
	// `choose` field — a weaker assertion than it looked, proved by mutation.
	_, effects, _ := strings.Cut(got, "} (")
	if !strings.Contains(effects, "defer-to-human") {
		t.Errorf("an unmapped outcome was dropped from the EFFECTS list:\n%s", got)
	}
	if strings.Contains(got, "()") {
		t.Errorf("refusal rendered an empty effects list:\n%s", got)
	}
	// And the renderer itself must never emit an empty enumeration.
	if out := blockedExitOutcomes(nil); out == "" || !strings.Contains(out, "rescope") {
		t.Errorf("blockedExitOutcomes(nil) = %q; a caller-facing string must not render empty", out)
	}
}

// TestValidateEscalationAdvertisesLiveOutcomes covers the route the prior
// rounds missed entirely: a FAILING VERDICT reopens the item, and when that
// reopen exhausts the rounds the validate handler renders its own blocked
// message. That message hard-coded the outcome list, so on a second escalation
// it advertised override-once while the ADR offered only rescope|reject and the
// parser refused it — the identical defect, on arguably the commoner way into
// blocked, found only because a verifier swept for a third call site after two
// were fixed (B-01KYS7111XFHZ).
//
// Re-hard-coding that literal survived the whole suite until this test existed.
func TestValidateEscalationAdvertisesLiveOutcomes(t *testing.T) {
	root := t.TempDir()
	sess := connectRoot(t, root)
	out := callText(t, sess, "draft", map[string]any{
		"kind": "task", "title": "verdict escalation route", "body": "a body of ordinary length",
	})
	id := ""
	for _, f := range strings.Fields(out) {
		if strings.HasPrefix(f, "T-") {
			id = f
		}
	}
	if id == "" {
		t.Fatalf("setup: %s", out)
	}
	// Drive to blocked, spend override-once, and come back — so the NEXT
	// escalation can only offer rescope|reject. Answering by parsing the ADR out
	// of the refusal keeps this test honest about what the surface actually says.
	// The ID sits INSIDE the callable JSON object the refusal hands back, so it
	// is not a whitespace-delimited field — scan for the prefix and take the run
	// of ID characters after it.
	adrOf := func(s string) string {
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
	var last string
	for i := 0; i < 10 && !strings.Contains(last, "rounds exhausted"); i++ {
		to := "done"
		if i%2 == 1 {
			to = "active"
		}
		last = callText(t, sess, "move", map[string]any{"id": id, "to": to})
	}
	adr := adrOf(last)
	if adr == "" {
		t.Fatalf("no escalation ADR in: %s", last)
	}
	callText(t, sess, "decide", map[string]any{"op": "answer", "id": adr, "choose": "override-once"})
	// Second escalation, reached through the VALIDATE route rather than `move`:
	// stop one round short, leave the item in done, and let the failing verdict's
	// own done->active reopen trip the limit. Driving it with `move` instead
	// captures roundsRefusal's message and never enters the validate handler at
	// all — which is why re-hard-coding validate.go's literal survived the first
	// version of this test.
	callText(t, sess, "move", map[string]any{"id": id, "to": "done"})
	for i := 0; i < 2; i++ {
		callText(t, sess, "move", map[string]any{"id": id, "to": "active"})
		callText(t, sess, "move", map[string]any{"id": id, "to": "done"})
	}
	callText(t, sess, "validate", map[string]any{"id": id, "agent": "v"})
	last = callText(t, sess, "validate", map[string]any{
		"id": id, "op": "verdict", "agent": "v", "pass": false,
		"findings": "a findings string long enough to clear the eighty character tripwire about token-thin validations",
	})
	if !strings.Contains(last, "rounds exhausted") {
		t.Fatalf("validate route never escalated: %s", last)
	}
	if strings.Contains(last, "override-once") {
		t.Errorf("blocked message advertises override-once after it was spent — the parser refuses it:\n%s", last)
	}
	if !strings.Contains(last, "rescope|reject") {
		t.Errorf("blocked message does not advertise the live set:\n%s", last)
	}
}

// TestMoveEscalationAdvertisesLiveOutcomes pins the layer the other tests miss.
// TestBlockedExitAdvertisesOnlyLiveOutcomes calls roundsRefusal as a unit with
// hand-supplied options, so it cannot see a wrong ARGUMENT at a call site, and
// the validate test covers only the validate route. A verifier proved the gap:
// passing nil instead of the ADR's parsed options at the move-route call sites
// reproduces the original bug byte-for-byte — a second escalation advertising
// override-once that the parser then refuses — and survived the whole suite.
// The renderer was pinned; that its callers feed it the LIVE set was not
// (B-01KYS7111XFHZ).
func TestMoveEscalationAdvertisesLiveOutcomes(t *testing.T) {
	root := t.TempDir()
	sess := connectRoot(t, root)
	out := callText(t, sess, "draft", map[string]any{
		"kind": "task", "title": "move route escalation", "body": "a body of ordinary length",
	})
	id := ""
	for _, f := range strings.Fields(out) {
		if strings.HasPrefix(f, "T-") {
			id = f
		}
	}
	if id == "" {
		t.Fatalf("setup: %s", out)
	}
	adrOf := func(s string) string {
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
	escalate := func() string {
		var last string
		for i := 0; i < 12 && !strings.Contains(last, "rounds exhausted"); i++ {
			to := "done"
			if i%2 == 1 {
				to = "active"
			}
			last = callText(t, sess, "move", map[string]any{"id": id, "to": to})
		}
		if !strings.Contains(last, "rounds exhausted") {
			t.Fatalf("escalation never reached: %s", last)
		}
		return last
	}
	first := escalate()
	if !strings.Contains(first, "override-once") {
		t.Errorf("first escalation must offer override-once:\n%s", first)
	}
	callText(t, sess, "decide", map[string]any{"op": "answer", "id": adrOf(first), "choose": "override-once"})
	// Second escalation through the SAME route: override-once is spent, so the
	// refusal must not name it — that is the argument reaching the renderer, not
	// the renderer itself.
	second := escalate()
	if strings.Contains(second, "override-once") {
		t.Errorf("move-route refusal advertises override-once after it was spent — the parser refuses it:\n%s", second)
	}
	if !strings.Contains(second, "rescope|reject") {
		t.Errorf("move-route refusal does not advertise the live set:\n%s", second)
	}
}

// blockedFixture drives a fresh task into item.StateBlocked the way the
// gate-fail route does — a done->active reopen that exhausts the feedback
// budget, then lifecycle.Escalate — and returns the blocked item together
// with the escalation ADR linked into its Needs. This is the setup
// TestDecideBlockedOverrideOnce performs inline; the Needs-bookkeeping cases
// below (B-01KYSX35RKFYB) need it once per caller path, so it lives here
// rather than being copied four times. Escalation itself stays out of scope:
// it is driven through lifecycle directly, and only the resolution goes
// through decide.
func blockedFixture(t *testing.T, s *Server, title string) (item.Item, item.Item) {
	t.Helper()
	s.ws.Cfg.Feedback.MaxRounds = 1
	it, err := lifecycle.Draft(s.ws, s.minter(), "task", title, "", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	return escalateOnce(t, s, it.ID)
}

// escalateOnce takes an ALREADY EXISTING item from wherever it is to blocked.
// Separate from blockedFixture because the re-escalation cases need the
// SECOND trip: an item that came back from blocked via override-once and then
// exhausts its budget again, which is the state where a spent Needs link is
// observable.
func escalateOnce(t *testing.T, s *Server, id string) (item.Item, item.Item) {
	t.Helper()
	cur, ok, err := item.Get(s.ws, id)
	if err != nil || !ok {
		t.Fatalf("escalateOnce: %s not in work.md: %v %v", id, ok, err)
	}
	if cur.State != item.StateDone {
		if _, err := lifecycle.Move(s.ws, id, item.StateDone, ""); err != nil {
			t.Fatalf("escalateOnce: %s -> done: %v", id, err)
		}
	}
	_, err = lifecycle.Move(s.ws, id, item.StateActive, "")
	var exhausted lifecycle.ErrRoundsExhausted
	if !errors.As(err, &exhausted) {
		t.Fatalf("escalateOnce: expected ErrRoundsExhausted, got %v", err)
	}
	blocked, decision, err := lifecycle.Escalate(s.ws, s.minter(), exhausted.Item)
	if err != nil {
		t.Fatal(err)
	}
	if blocked.State != item.StateBlocked {
		t.Fatalf("escalateOnce: item not blocked: %+v", blocked)
	}
	return blocked, decision
}

// freshScope recomputes the ID display scope the way a real tool call does.
// Server.scCache memoizes the known-ID set for the duration of ONE call, and
// these tests mint records through lifecycle BETWEEN calls, so the cached
// scope can predate the ADR under assertion — shortening against a stale peer
// set can render a prefix that the next real call then finds ambiguous.
// Dropping the cache reproduces exactly what `prompt next` would compute.
func freshScope(t *testing.T, s *Server) idScope {
	t.Helper()
	s.scCache = nil
	sc, err := s.idScope()
	if err != nil {
		t.Fatalf("idScope: %v", err)
	}
	return sc
}

// hintID picks the id= argument out of a rendered nextAction hint — what an
// agent reading the hint would copy into its next call, character for
// character.
func hintID(t *testing.T, hint string) string {
	t.Helper()
	for _, f := range strings.Fields(hint) {
		if rest, ok := strings.CutPrefix(f, "id="); ok {
			return rest
		}
	}
	t.Fatalf("no id= argument in hint: %q", hint)
	return ""
}

// TestNeedsClearedOnEverySpentDecision pins the ONE rule resolveDecision now
// follows (B-01KYSX35RKFYB): the Needs link is cleared exactly when the
// decision that occupied it has been answered, whatever the answer did to the
// item's state. The bookkeeping used to be inverted — the three
// ResolveBlocked outcomes kept the spent ADR while the non-resolving path
// dropped it — and NOTHING asserted the post-answer Needs on any resolving
// path, which is how an inversion visible in the branch shape survived.
//
// One subtest per caller path into resolveDecision, because the paths differ
// in what they do to the item afterwards (upsert, remove-and-snapshot,
// nothing) and only reject exercises the clear-BEFORE ordering.
func TestNeedsClearedOnEverySpentDecision(t *testing.T) {
	t.Run("blocked+rescope", func(t *testing.T) {
		s, sess := connectDecide(t, t.TempDir(), nil)
		blocked, decision := blockedFixture(t, s, "rescope this work")

		out := callText(t, sess, "decide", map[string]any{
			"op": "answer", "id": decision.ID, "choose": "rescope",
		})
		if !strings.Contains(out, "ok "+shortID(t, s, decision.ID)+" rescope") {
			t.Fatalf("rescope answer: %q", out)
		}
		got, ok, err := item.Get(s.ws, blocked.ID)
		if err != nil || !ok {
			t.Fatalf("item gone: %v %v", ok, err)
		}
		if got.State != item.StateDraft || got.Rounds != 0 {
			t.Fatalf("rescope did not resolve the block: %+v", got)
		}
		if len(got.Needs) != 0 {
			t.Fatalf("spent decision still linked after rescope: Needs = %v", got.Needs)
		}
	})

	t.Run("blocked+override-once", func(t *testing.T) {
		s, sess := connectDecide(t, t.TempDir(), nil)
		blocked, decision := blockedFixture(t, s, "override this work")

		out := callText(t, sess, "decide", map[string]any{
			"op": "answer", "id": decision.ID, "choose": "override-once",
		})
		if !strings.Contains(out, "ok "+shortID(t, s, decision.ID)+" override-once") {
			t.Fatalf("override-once answer: %q", out)
		}
		got, ok, err := item.Get(s.ws, blocked.ID)
		if err != nil || !ok {
			t.Fatalf("item gone: %v %v", ok, err)
		}
		if got.State != item.StateActive || got.Rounds != 0 || !got.Override {
			t.Fatalf("override-once did not resolve the block: %+v", got)
		}
		if len(got.Needs) != 0 {
			t.Fatalf("spent decision still linked after override-once: Needs = %v", got.Needs)
		}
	})

	t.Run("blocked+reject survives the revoke", func(t *testing.T) {
		s, sess := connectDecide(t, t.TempDir(), nil)
		blocked, decision := blockedFixture(t, s, "reject this work")

		out := callText(t, sess, "decide", map[string]any{
			"op": "answer", "id": decision.ID, "choose": "reject",
		})
		if !strings.Contains(out, "ok "+shortID(t, s, decision.ID)+" reject") {
			t.Fatalf("reject answer: %q", out)
		}
		// the ACTION, not the wording: reject removes the item from work.md
		// and leaves a revocable snapshot behind.
		if _, ok, _ := item.Get(s.ws, blocked.ID); ok {
			t.Fatalf("rejected item still in work.md: %s", blocked.ID)
		}
		// This arm is what pins the clear-BEFORE ordering. ResolveBlocked's
		// reject outcome carryRecords the item into the rejection event and
		// then removes it, so a clear performed AFTER the call never reaches
		// the snapshot — the spent ADR would ride it back on every later
		// revoke, forever.
		revoked, err := lifecycle.Move(s.ws, blocked.ID, item.StateDraft, "revoked")
		if err != nil {
			t.Fatalf("revoke: %v", err)
		}
		if len(revoked.Needs) != 0 {
			t.Fatalf("rejection snapshot carried the spent decision back: Needs = %v", revoked.Needs)
		}
	})

	t.Run("non-blocking ask on an ordinary item", func(t *testing.T) {
		s, sess := connectDecide(t, t.TempDir(), nil)
		task := draftID(t, sess, map[string]any{"kind": "task", "title": "ship the new backend"})
		taskFull := fullID(t, s, task)
		adr := askID(t, sess, map[string]any{
			"op": "ask", "question": "which backend?", "kind": "radio",
			"options": []string{"grpc", "rest"}, "item": task,
		})

		out := callText(t, sess, "decide", map[string]any{"op": "answer", "id": adr, "choose": "grpc"})
		if !strings.Contains(out, "ok "+adr+" grpc") {
			t.Fatalf("ordinary answer: %q", out)
		}
		got, ok, err := item.Get(s.ws, taskFull)
		if err != nil || !ok {
			t.Fatalf("item gone: %v %v", ok, err)
		}
		// nothing to unblock — the item was never in item.StateBlocked — but
		// the link is spent all the same, and the state must be untouched.
		if got.State != item.StateDraft {
			t.Fatalf("an ordinary decision moved the item it named: %+v", got)
		}
		if len(got.Needs) != 0 {
			t.Fatalf("spent decision still linked after an ordinary answer: Needs = %v", got.Needs)
		}
	})
}

// TestBlockedReEscalationNamesTheLiveDecision is the reported symptom, end to
// end (B-01KYSX35RKFYB). An item that was unblocked by override-once and then
// escalates a second time used to carry TWO ADRs in Needs — the spent one
// first — so nextAction, which names Needs[0] as "the only exit", pointed at
// an already-decided ADR and following that hint returned
// `! ARG E - ADR-... already decided`. The item was reachable but its rendered
// exit was not.
//
// The load-bearing assertion is the last one: feeding the hint's own id=
// straight back into decide op=answer must SUCCEED. Asserting only on which
// ADR the string names would survive a Needs-ordering refactor that still
// leaves a spent link behind; asserting the round trip cannot.
func TestBlockedReEscalationNamesTheLiveDecision(t *testing.T) {
	s, sess := connectDecide(t, t.TempDir(), nil)
	blocked, first := blockedFixture(t, s, "twice-escalating work")

	out := callText(t, sess, "decide", map[string]any{
		"op": "answer", "id": first.ID, "choose": "override-once",
	})
	if !strings.Contains(out, "ok "+shortID(t, s, first.ID)+" override-once") {
		t.Fatalf("setup: override-once answer: %q", out)
	}
	_, second := escalateOnce(t, s, blocked.ID)

	it, ok, err := item.Get(s.ws, blocked.ID)
	if err != nil || !ok {
		t.Fatalf("item gone: %v %v", ok, err)
	}
	if len(it.Needs) != 1 || it.Needs[0] != second.ID {
		t.Fatalf("re-escalated item should need exactly the LIVE decision %s: Needs = %v", second.ID, it.Needs)
	}

	sc := freshScope(t, s)
	hint := nextAction(it, sc.short)
	if !strings.Contains(hint, sc.short(second.ID)) {
		t.Fatalf("hint does not name the live decision %s:\n%s", sc.short(second.ID), hint)
	}
	if strings.Contains(hint, sc.short(first.ID)) {
		t.Fatalf("hint names the already-decided %s as the exit:\n%s", sc.short(first.ID), hint)
	}

	// Follow the hint verbatim. rescope, not override-once: that one is spent,
	// so the second escalation's ADR does not offer it.
	out = callText(t, sess, "decide", map[string]any{
		"op": "answer", "id": hintID(t, hint), "choose": "rescope",
	})
	if strings.Contains(out, "already decided") || !strings.Contains(out, "ok ") {
		t.Fatalf("following the rendered exit must resolve the block, got: %q", out)
	}
	resolved, ok, err := item.Get(s.ws, blocked.ID)
	if err != nil || !ok {
		t.Fatalf("item gone: %v %v", ok, err)
	}
	if resolved.State != item.StateDraft || resolved.Rounds != 0 || len(resolved.Needs) != 0 {
		t.Fatalf("the hinted exit did not actually unblock the item: %+v", resolved)
	}
}

// TestBlockedResolveErrorKeepsLiveDecisionFollowable guards the error path,
// and exists to forbid a restore-on-error arm from ever being added back to
// resolveDecision (B-01KYSX35RKFYB). Restoring looks obviously right — "do not
// strand a blocked item with empty Needs" — and is measurably wrong: removeID
// drops only the ANSWERED id, so the blocked item still holds its live
// escalation ADR, while a restore would put a state=done ADR back into Needs
// and reproduce the reported symptom one escalation later.
//
// Reaching a ResolveBlocked refusal takes an ordinary decide op=ask decision
// whose options happen to spell a resolving outcome: the escalation ADR itself
// cannot get there, since Escalate stops offering override-once once Override
// is set and decideAnswer refuses unlisted choices before resolveDecision.
func TestBlockedResolveErrorKeepsLiveDecisionFollowable(t *testing.T) {
	s, sess := connectDecide(t, t.TempDir(), nil)
	blocked, first := blockedFixture(t, s, "already overridden work")
	callText(t, sess, "decide", map[string]any{"op": "answer", "id": first.ID, "choose": "override-once"})
	_, live := escalateOnce(t, s, blocked.ID)

	// A second decision on the same blocked item, minted the ordinary way, and
	// answered with a value ResolveBlocked will refuse.
	spent := askID(t, sess, map[string]any{
		"op": "ask", "question": "force it through again?", "kind": "radio",
		"options": []string{"override-once", "hold"}, "item": blocked.ID,
	})
	out := callText(t, sess, "decide", map[string]any{"op": "answer", "id": spent, "choose": "override-once"})
	if !strings.Contains(out, "! ARG E") || !strings.Contains(out, "override-once already used") {
		t.Fatalf("a second override-once must be refused by ResolveBlocked: %q", out)
	}

	it, ok, err := item.Get(s.ws, blocked.ID)
	if err != nil || !ok {
		t.Fatalf("item gone: %v %v", ok, err)
	}
	if it.State != item.StateBlocked {
		t.Fatalf("a refused resolution moved the item anyway: %+v", it)
	}
	// EXACTLY the live escalation ADR: the answered one is gone (it was spent,
	// refusal or not) and nothing was restored on top of it.
	if len(it.Needs) != 1 || it.Needs[0] != live.ID {
		t.Fatalf("blocked item should hold only its live decision %s: Needs = %v", live.ID, it.Needs)
	}

	sc := freshScope(t, s)
	hint := nextAction(it, sc.short)
	if !strings.Contains(hint, sc.short(live.ID)) {
		t.Fatalf("hint does not name the live decision %s:\n%s", sc.short(live.ID), hint)
	}
	out = callText(t, sess, "decide", map[string]any{
		"op": "answer", "id": hintID(t, hint), "choose": "rescope",
	})
	if strings.Contains(out, "already decided") || !strings.Contains(out, "ok ") {
		t.Fatalf("the exit rendered after a refused resolution must still be followable: %q", out)
	}
	resolved, ok, err := item.Get(s.ws, blocked.ID)
	if err != nil || !ok {
		t.Fatalf("item gone: %v %v", ok, err)
	}
	if resolved.State != item.StateDraft || len(resolved.Needs) != 0 {
		t.Fatalf("the hinted exit did not unblock the item: %+v", resolved)
	}
}
