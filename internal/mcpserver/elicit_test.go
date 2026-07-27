package mcpserver

// ELICIT-001 pins (B-01KYHCHRN0): Session.Elicit is reserved for decide
// op=ask — the one place the HUMAN is the addressee. rule op=add missing
// slots are authored by the calling agent and must come back as need
// records even when the session advertises elicitation capability.

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// connectRootElicit is connectRoot with an elicitation-capable client, so
// tests can prove a tool does NOT reach for the native form when it could.
func connectRootElicit(t *testing.T, root string, elicit func(context.Context, *mcp.ElicitRequest) (*mcp.ElicitResult, error)) *mcp.ClientSession {
	t.Helper()
	s, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	ct, st := mcp.NewInMemoryTransports()
	go func() {
		if err := s.MCP().Run(context.Background(), st); err != nil {
			t.Logf("server stopped: %v", err)
		}
	}()
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"},
		&mcp.ClientOptions{ElicitationHandler: elicit})
	sess, err := client.Connect(context.Background(), ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sess.Close() })
	return sess
}

func TestRuleAddNeverElicits(t *testing.T) {
	root := gitRoot(t)
	elicited := 0
	sess := connectRootElicit(t, root, func(_ context.Context, req *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
		elicited++
		return &mcp.ElicitResult{Action: "accept", Content: map[string]any{
			"trigger": "a build finishes",
		}}, nil
	})
	out := callText(t, sess, "rule", map[string]any{
		"op": "add", "dir": "", "stem": "ELICITPIN", "pattern": "E",
		"system": "test system", "response": "write 1 artifact",
	})
	if elicited != 0 {
		t.Fatalf("rule op=add elicited %d time(s); slots are agent-authored", elicited)
	}
	if !strings.Contains(out, "need trigger") {
		t.Fatalf("missing slot must come back as a need record: %q", out)
	}
	if !strings.Contains(out, "shape: rule ") {
		t.Fatalf("refusal must carry the assembled-call shape line: %q", out)
	}
}
