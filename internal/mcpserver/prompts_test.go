package mcpserver

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// connectRootWithPrompts is connectRoot (tools_test.go) plus registerPrompts
// — New does not wire prompts yet (orchestrator's job), so tests that need
// them call it directly on the freshly built server.
func connectRootWithPrompts(t *testing.T, root string) *mcp.ClientSession {
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
	return sess
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

	callText(t, sess, "draft", map[string]any{"kind": "task", "title": "wire prompts"})
	callText(t, sess, "move", map[string]any{"id": "T-0001", "to": "submitted"})
	callText(t, sess, "move", map[string]any{"id": "T-0001", "to": "approved"})

	out := getPromptText(t, sess, "next", nil)
	if !strings.Contains(out, "T-0001") {
		t.Fatalf("next did not surface the approved task: %q", out)
	}
	if !strings.Contains(out, "lease") {
		t.Fatalf("next missing the implementer protocol's lease step: %q", out)
	}
}
