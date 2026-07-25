package mcpserver

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jxsl13/spectackle/internal/item"
	"github.com/jxsl13/spectackle/internal/lifecycle"
)

// connectRootWithPrompts is connectRoot (tools_test.go) plus registerPrompts
// — New does not wire prompts yet (orchestrator's job), so tests that need
// them call it directly on the freshly built server.
func connectRootWithPrompts(t *testing.T, root string) *mcp.ClientSession {
	t.Helper()
	_, sess := connectRootWithPromptsServer(t, root)
	return sess
}

// connectRootWithPromptsServer is connectRootWithPrompts but also returns
// the *Server — some tests (open-needs/blocked skip logic) need direct
// access to s.ws to set up item state the wire has no path to yet (e.g.
// item.StateBlocked, only ever entered via lifecycle.Escalate).
func connectRootWithPromptsServer(t *testing.T, root string) (*Server, *mcp.ClientSession) {
	t.Helper()
	s, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	s.registerPrompts()
	t.Cleanup(func() { _ = s.Close() })
	ct, st := mcp.NewInMemoryTransports()
	go func() {
		if err := s.MCP().Run(context.Background(), st); err != nil {
			t.Logf("server stopped: %v", err)
		}
	}()
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	sess, err := client.Connect(context.Background(), ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sess.Close() })
	return s, sess
}

func getPromptText(t *testing.T, sess *mcp.ClientSession, name string, args map[string]string) string {
	t.Helper()
	res, err := sess.GetPrompt(context.Background(), &mcp.GetPromptParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if len(res.Messages) == 0 {
		t.Fatalf("%s: no messages", name)
	}
	tc, ok := res.Messages[0].Content.(*mcp.TextContent)
	if !ok {
		t.Fatalf("%s: content is %T, want TextContent", name, res.Messages[0].Content)
	}
	return tc.Text
}

func TestPromptWorkflow(t *testing.T) {
	sess := connectRootWithPrompts(t, t.TempDir())

	out := getPromptText(t, sess, "workflow", nil)
	if !strings.Contains(out, "LOOP") {
		t.Fatalf("workflow prompt missing LOOP section: %q", out)
	}
	if !strings.Contains(out, "ag ") {
		t.Fatalf("workflow prompt missing an ag line (self should be registered): %q", out)
	}
}

func TestPromptNextNoneApproved(t *testing.T) {
	sess := connectRootWithPrompts(t, t.TempDir())

	out := getPromptText(t, sess, "next", nil)
	if !strings.Contains(out, "nothing approved") {
		t.Fatalf("next with no approved items: %q", out)
	}
}

func TestPromptNextApprovedTask(t *testing.T) {
	root := t.TempDir()
	sess := connectRootWithPrompts(t, root)

	task := draftID(t, sess, map[string]any{"kind": "task", "title": "wire prompts"})
	callText(t, sess, "move", map[string]any{"id": task, "to": "submitted"})
	callText(t, sess, "move", map[string]any{"id": task, "to": "approved"})

	out := getPromptText(t, sess, "next", nil)
	if !strings.Contains(out, task) {
		t.Fatalf("next did not surface the approved task: %q", out)
	}
	if !strings.Contains(out, "lease") {
		t.Fatalf("next missing the implementer protocol's lease step: %q", out)
	}
}

// TestPromptWorkflowTaskArgument: with task set, the response prepends a
// LIFECYCLE block carrying the task text (the MCP-native form of
// `/spectackle <task>`, .claude/commands/spectackle.md) ahead of the usual
// live-state snapshot; bare (no task) stays exactly as before.
func TestPromptWorkflowTaskArgument(t *testing.T) {
	sess := connectRootWithPrompts(t, t.TempDir())

	out := getPromptText(t, sess, "workflow", map[string]string{"task": "add retry to the sync client"})
	if !strings.Contains(out, "LIFECYCLE add retry to the sync client") {
		t.Fatalf("workflow task arg missing LIFECYCLE block: %q", out)
	}
	if !strings.Contains(out, "decide op=ask") || !strings.Contains(out, "fanout") {
		t.Fatalf("LIFECYCLE block missing decide/fanout steps: %q", out)
	}
	if strings.Index(out, "LIFECYCLE") > strings.Index(out, "spectackle workflow - state below is live") {
		t.Fatalf("LIFECYCLE block must be prepended, not appended: %q", out)
	}

	bare := getPromptText(t, sess, "workflow", nil)
	if strings.Contains(bare, "LIFECYCLE") {
		t.Fatalf("bare workflow (no task) must not carry a LIFECYCLE block: %q", bare)
	}
}

// TestPromptNextArchivedNeedNotOpenNonexistentNeedStillOpen checks LCY-002's
// two halves of hasOpenNeeds's Tombstone fallback: a Need pointing at an
// archived item resolves (next does not skip the candidate over it), while a
// Need pointing at an ID that resolves nowhere at all — never created, or
// gone for any reason other than archiving — stays conservatively open (next
// keeps skipping).
func TestPromptNextArchivedNeedNotOpenNonexistentNeedStillOpen(t *testing.T) {
	root := t.TempDir()
	s, sess := connectRootWithPromptsServer(t, root)

	// needsArchived: approved, Needs an archived item — must resolve as satisfied.
	archived, err := lifecycle.Draft(s.ws, nil, "task", "long done", "", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lifecycle.Move(s.ws, archived.ID, item.StateArchived, "shipped"); err != nil {
		t.Fatal(err)
	}
	needsArchived := draftID(t, sess, map[string]any{"kind": "task", "title": "needs an archived item"})
	callText(t, sess, "move", map[string]any{"id": needsArchived, "to": "submitted"})
	callText(t, sess, "move", map[string]any{"id": needsArchived, "to": "approved"})
	it1, ok, err := item.Get(s.ws, needsArchived)
	if err != nil || !ok {
		t.Fatalf("%s missing: %v %v", needsArchived, ok, err)
	}
	it1.Needs = append(it1.Needs, archived.ID)
	if err := item.Upsert(s.ws, it1); err != nil {
		t.Fatal(err)
	}

	out := getPromptText(t, sess, "next", nil)
	if !strings.Contains(out, needsArchived) {
		t.Fatalf("next should surface a candidate whose only need is archived: %q", out)
	}

	// needsBogus: approved, Needs an ID that resolves nowhere — stays open,
	// so next must keep skipping it in favor of the plain task below.
	needsBogus := draftID(t, sess, map[string]any{"kind": "task", "title": "needs a bogus id"})
	callText(t, sess, "move", map[string]any{"id": needsBogus, "to": "submitted"})
	callText(t, sess, "move", map[string]any{"id": needsBogus, "to": "approved"})
	it3, ok, err := item.Get(s.ws, needsBogus)
	if err != nil || !ok {
		t.Fatalf("%s missing: %v %v", needsBogus, ok, err)
	}
	it3.Needs = append(it3.Needs, "D-9999")
	if err := item.Upsert(s.ws, it3); err != nil {
		t.Fatal(err)
	}
	plain := draftID(t, sess, map[string]any{"kind": "task", "title": "plain actionable work"})
	callText(t, sess, "move", map[string]any{"id": plain, "to": "submitted"})
	callText(t, sess, "move", map[string]any{"id": plain, "to": "approved"})

	// leaving needsArchived in the candidate pool would make its own case
	// ambiguous, so drain it out first and re-check need resolution directly
	// against needsBogus.
	callText(t, sess, "move", map[string]any{"id": needsArchived, "to": "done"})

	out = getPromptText(t, sess, "next", nil)
	if strings.Contains(out, needsBogus) {
		t.Fatalf("next surfaced a candidate whose need resolves nowhere: %q", out)
	}
	if !strings.Contains(out, plain) {
		t.Fatalf("next skipped past the actionable item: %q", out)
	}
}

// TestPromptNextSkipsBlockedAndOpenNeeds: `next`'s auto-pick must structurally
// skip (a) an approved item that is still blocked on an open decide op=ask
// need, and (b) an item escalated into item.StateBlocked (lifecycle.Escalate)
// — surfacing the next actually-actionable approved item instead.
func TestPromptNextSkipsBlockedAndOpenNeeds(t *testing.T) {
	root := t.TempDir()
	s, sess := connectRootWithPromptsServer(t, root)

	// needsDecision: approved, but still has an open need (decide op=ask on
	// any item — not necessarily an escalated one — appends to Needs).
	needsDecision := draftID(t, sess, map[string]any{"kind": "task", "title": "needs a decision first"})
	callText(t, sess, "move", map[string]any{"id": needsDecision, "to": "submitted"})
	callText(t, sess, "move", map[string]any{"id": needsDecision, "to": "approved"})
	d, err := lifecycle.Draft(s.ws, nil, "adr", "which approach", "", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	it1, ok, err := item.Get(s.ws, needsDecision)
	if err != nil || !ok {
		t.Fatalf("%s missing: %v %v", needsDecision, ok, err)
	}
	it1.Needs = append(it1.Needs, d.ID)
	if err := item.Upsert(s.ws, it1); err != nil {
		t.Fatal(err)
	}

	// escalated into item.StateBlocked directly via lifecycle.Escalate
	// (the wire has no path there yet outside work op=submit's gate-fail
	// rounds, see swarm.go gateFail — out of this test's concern).
	s.ws.Cfg.Feedback.MaxRounds = 1
	it2, err := lifecycle.Draft(s.ws, nil, "task", "flaky feedback loop", "", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lifecycle.Move(s.ws, it2.ID, item.StateDone, ""); err != nil {
		t.Fatal(err)
	}
	_, err = lifecycle.Move(s.ws, it2.ID, item.StateActive, "")
	var exhausted lifecycle.ErrRoundsExhausted
	if !errors.As(err, &exhausted) {
		t.Fatalf("expected ErrRoundsExhausted, got %v", err)
	}
	if _, _, err := lifecycle.Escalate(s.ws, nil, exhausted.Item); err != nil {
		t.Fatal(err)
	}
	// blocked items are not "approved" so they wouldn't normally reach
	// promptNext's candidate loop at all; approve it anyway to prove the
	// explicit item.StateBlocked check (not just the state!=approved
	// filter) is what is doing the skipping here — reaching into work.md
	// directly since blocked refuses every lifecycle.Move transition.
	blocked, ok, err := item.Get(s.ws, it2.ID)
	if err != nil || !ok || blocked.State != item.StateBlocked {
		t.Fatalf("setup: item not blocked: %+v %v", blocked, err)
	}

	// plain approved task with no needs — the one `next` should pick.
	actionable := draftID(t, sess, map[string]any{"kind": "task", "title": "actionable work"})
	callText(t, sess, "move", map[string]any{"id": actionable, "to": "submitted"})
	callText(t, sess, "move", map[string]any{"id": actionable, "to": "approved"})

	out := getPromptText(t, sess, "next", nil)
	if strings.Contains(out, needsDecision) {
		t.Fatalf("next surfaced an item with an open need: %q", out)
	}
	if strings.Contains(out, it2.ID) {
		t.Fatalf("next surfaced a blocked item: %q", out)
	}
	if !strings.Contains(out, actionable) {
		t.Fatalf("next skipped past the actionable item: %q", out)
	}
}
