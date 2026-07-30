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
