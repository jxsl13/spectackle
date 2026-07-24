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

	out := callText(t, sess, "decide", map[string]any{
		"op": "ask", "question": "which backend?", "options": []string{"grpc", "rest"},
	})
	if !strings.Contains(out, "need decision ADR-0001 which backend? | grpc, rest") {
		t.Fatalf("headless ask should return the need record, got: %q", out)
	}

	d, ok, err := item.Get(s.ws, "ADR-0001")
	if err != nil || !ok {
		t.Fatalf("ADR-0001 not persisted: %v %v", ok, err)
	}
	if d.State != item.StateSubmitted {
		t.Fatalf("undelivered decision state = %s, want submitted", d.State)
	}

	out = callText(t, sess, "decide", map[string]any{"op": "ls"})
	if !strings.Contains(out, "ADR-0001") {
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
	if !strings.Contains(out, "ok ADR-0001 rest") {
		t.Fatalf("accepted ask should resolve immediately: %q", out)
	}
	d, ok, err := item.Get(s.ws, "ADR-0001")
	if err != nil || !ok || d.State != item.StateDone {
		t.Fatalf("ADR-0001 not done: %+v %v %v", d, ok, err)
	}
}

// TestDecideAnswerResolvesAndClearsNeeds: ask op=ask with item=<id> links
// the minted decision into that item's Needs; a later, separate answer=
// call (simulating "from any session, any time") resolves it, clears the
// Need and journals ev=decide.
func TestDecideAnswerResolvesAndClearsNeeds(t *testing.T) {
	root := t.TempDir()
	s, sess := connectDecide(t, root, nil)

	callText(t, sess, "draft", map[string]any{"kind": "task", "title": "ship the new backend"})
	callText(t, sess, "decide", map[string]any{
		"op": "ask", "question": "which backend?", "kind": "radio",
		"options": []string{"grpc", "rest"}, "item": "T-0001",
	})

	blocked, ok, err := item.Get(s.ws, "T-0001")
	if err != nil || !ok || len(blocked.Needs) != 1 || blocked.Needs[0] != "ADR-0001" {
		t.Fatalf("T-0001 Needs not linked to ADR-0001: %+v %v", blocked, err)
	}

	out := callText(t, sess, "decide", map[string]any{"op": "answer", "id": "ADR-0001", "choose": "grpc"})
	if !strings.Contains(out, "ok ADR-0001 grpc") {
		t.Fatalf("answer should resolve: %q", out)
	}

	// a second, confirm-kind decision on the same item: its stored options
	// (forced to yes/no regardless of what ask was called with) must
	// actually constrain answer=
	callText(t, sess, "decide", map[string]any{"op": "ask", "question": "yes or no?", "kind": "confirm", "item": "T-0001"})
	out = callText(t, sess, "decide", map[string]any{"op": "answer", "id": "ADR-0002", "choose": "maybe"})
	if !strings.Contains(out, "! ARG E") {
		t.Fatalf("confirm answer outside yes|no must be rejected: %q", out)
	}

	resolved, ok, err := item.Get(s.ws, "T-0001")
	if err != nil || !ok {
		t.Fatalf("T-0001 gone: %v", err)
	}
	if len(resolved.Needs) != 1 || resolved.Needs[0] != "ADR-0002" {
		t.Fatalf("ADR-0001 should be cleared from Needs, ADR-0002 still open: %+v", resolved.Needs)
	}

	events, err := journal.ReadAll(s.ws)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range events {
		if e.Ev == journal.EvDecide && e.ID == "ADR-0001" && e.Note == "grpc" {
			found = true
		}
	}
	if !found {
		t.Fatal("no ev=decide journal event for ADR-0001's resolution")
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

	out := callText(t, sess, "decide", map[string]any{
		"op": "ask", "question": "still relevant?", "kind": "confirm", "item": it.ID,
	})
	if !strings.Contains(out, "need decision ADR-0001") {
		t.Fatalf("ask on archived item should still succeed: %q", out)
	}

	d, ok, err := item.Get(s.ws, "ADR-0001")
	if err != nil || !ok {
		t.Fatalf("ADR-0001 not persisted: %v %v", ok, err)
	}
	if !strings.Contains(d.Body, "blocks: "+it.ID) {
		t.Fatalf("ADR-0001 body missing blocks provenance: %q", d.Body)
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

	callText(t, sess, "decide", map[string]any{
		"op": "ask", "question": "pick one", "kind": "radio",
		"options": []string{"defer, revisit later", "go now"},
	})
	d, ok, err := item.Get(s.ws, "ADR-0001")
	if err != nil || !ok {
		t.Fatalf("ADR-0001 not persisted: %v %v", ok, err)
	}
	if !strings.Contains(d.Body, "option: defer, revisit later\n") || !strings.Contains(d.Body, "option: go now") {
		t.Fatalf("ADR-0001 body missing per-line option: %q", d.Body)
	}
	if strings.Contains(d.Body, "options: ") {
		t.Fatalf("ADR-0001 body still uses the legacy comma-joined line: %q", d.Body)
	}

	out := callText(t, sess, "decide", map[string]any{
		"op": "answer", "id": "ADR-0001", "choose": "defer, revisit later",
	})
	if !strings.Contains(out, "ok ADR-0001 defer, revisit later") {
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
	if !strings.Contains(out, "ok "+legacy.ID+" beta") {
		t.Fatalf("legacy comma-split body should still be answerable: %q", out)
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
	if blocked.State != item.StateBlocked {
		t.Fatalf("setup: item not blocked: %+v", blocked)
	}

	out := callText(t, sess, "decide", map[string]any{
		"op": "answer", "id": decision.ID, "choose": "override-once",
	})
	if !strings.Contains(out, "ok "+decision.ID+" override-once") {
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
	// reject, override-once from lifecycle.Escalate's outcome= sentence) is
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
