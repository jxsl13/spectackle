package mcpserver

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// connect spins up the server against the real repo root over an in-memory
// transport and returns a live client session.
func connect(t *testing.T) *mcp.ClientSession {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return connectRoot(t, root)
}

func connectRoot(t *testing.T, root string) *mcp.ClientSession {
	t.Helper()
	s := New(root)
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

func TestToolRegistration(t *testing.T) {
	sess := connect(t)
	res, err := sess.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"sym": false, "map": false, "impact": false, "contracts": false,
		"plan_change": false, "lint_ears": false, "coverage": false,
		"link": false, "reindex": false, "add_rule": false, "rm_rule": false,
	}
	for _, tool := range res.Tools {
		if _, ok := want[tool.Name]; ok {
			want[tool.Name] = true
		}
	}
	for name, seen := range want {
		if !seen {
			t.Errorf("tool %q not registered", name)
		}
	}
}

func callText(t *testing.T, sess *mcp.ClientSession, name string, args map[string]any) string {
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
	return tc.Text
}

func TestLintEarsTool(t *testing.T) {
	sess := connect(t)

	out := callText(t, sess, "lint_ears", map[string]any{
		"text": "The server should reply eventually.",
	})
	if !strings.Contains(out, "E001") {
		t.Errorf("expected E001 finding, got %q", out)
	}

	out = callText(t, sess, "lint_ears", map[string]any{
		"text": "WHEN a request arrives, the server SHALL respond within 2 seconds.",
	})
	if strings.TrimSpace(out) != "ok" {
		t.Errorf("expected ok, got %q", out)
	}
}

func TestContractsToolCascades(t *testing.T) {
	sess := connect(t)
	out := callText(t, sess, "contracts", map[string]any{
		"paths": []string{"examples/saxpy/saxpy/kernels/saxpy.cu"},
	})
	for _, id := range []string{"SPX-ARC-001", "SXP-API-001", "CUDA-KRN-001"} {
		if !strings.Contains(out, id) {
			t.Errorf("contracts output missing %s:\n%s", id, out)
		}
	}
	// global rules must come before leaf rules
	if strings.Index(out, "SPX-ARC-001") > strings.Index(out, "CUDA-KRN-001") {
		t.Errorf("cascade order wrong (global after leaf):\n%s", out)
	}
}

func TestPlanChangeToolSections(t *testing.T) {
	sess := connect(t)
	out := callText(t, sess, "plan_change", map[string]any{
		"targets": []string{"examples/saxpy/saxpy/saxpy.go:24"},
		"intent":  "support strided access",
	})
	for _, sec := range []string{"#impact", "#contracts", "#gaps"} {
		if !strings.Contains(out, sec) {
			t.Errorf("plan_change missing section %s:\n%s", sec, out)
		}
	}
	if !strings.Contains(out, "SXP-API-001") {
		t.Errorf("plan_change did not resolve contracts for path target:\n%s", out)
	}
}

// TestAddRuleLifecycle drives the server-managed authoring path end to end
// against a temp repo: compose from slots, auto-ID, lint gate, removal.
func TestAddRuleLifecycle(t *testing.T) {
	root := t.TempDir()
	sess := connectRoot(t, root)

	// missing everything, client has no elicitation UI -> need records
	out := callText(t, sess, "add_rule", map[string]any{})
	if !strings.Contains(out, "need pattern") {
		t.Fatalf("expected need records, got %q", out)
	}

	// full slots -> composed, linted, written with auto-ID ...-001
	out = callText(t, sess, "add_rule", map[string]any{
		"dir": "cuda_kernels", "pattern": "E", "stem": "CUDA-KRN",
		"system":   "host wrapper",
		"response": "check cudaGetLastError and propagate its numeric value to the caller",
		"trigger":  "a kernel launch statement returns",
	})
	if !strings.Contains(out, "ok CUDA-KRN-001") || !strings.Contains(out, "WHEN a kernel launch statement returns, the host wrapper SHALL") {
		t.Fatalf("add_rule failed: %q", out)
	}

	// second rule gets the next ID
	out = callText(t, sess, "add_rule", map[string]any{
		"dir": "cuda_kernels", "pattern": "U",
		"system":   "kernel",
		"response": "guard every element access with an explicit `if (i < n)` bound check",
	})
	if !strings.Contains(out, "ok CUDA-KRN-002") {
		t.Fatalf("expected auto-increment to CUDA-KRN-002: %q", out)
	}

	// vague slots are rejected before anything is written
	out = callText(t, sess, "add_rule", map[string]any{
		"dir": "cuda_kernels", "pattern": "U",
		"system": "kernel", "response": "handle memory appropriately",
	})
	if !strings.Contains(out, "E004") || !strings.Contains(out, "REJECTED") {
		t.Fatalf("expected E004 rejection: %q", out)
	}

	// the file now resolves through the cascade
	out = callText(t, sess, "contracts", map[string]any{"paths": []string{"cuda_kernels/saxpy.cu"}})
	if !strings.Contains(out, "CUDA-KRN-001") || !strings.Contains(out, "CUDA-KRN-002") {
		t.Fatalf("contracts missing authored rules: %q", out)
	}

	// remove one and verify it is gone
	out = callText(t, sess, "rm_rule", map[string]any{"rule": "CUDA-KRN-001"})
	if !strings.Contains(out, "ok CUDA-KRN-001 removed") {
		t.Fatalf("rm_rule failed: %q", out)
	}
	out = callText(t, sess, "contracts", map[string]any{"paths": []string{"cuda_kernels/saxpy.cu"}})
	if strings.Contains(out, "CUDA-KRN-001") || !strings.Contains(out, "CUDA-KRN-002") {
		t.Fatalf("rule not removed cleanly: %q", out)
	}
}
