package mcpserver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jxsl13/spectackle/internal/spec"
	"github.com/jxsl13/spectackle/internal/workspace"
)

func contextCall(t *testing.T, sess *mcp.ClientSession, args map[string]any) (*mcp.CallToolResult, string) {
	t.Helper()
	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{Name: "context", Arguments: args})
	if err != nil {
		t.Fatalf("context transport: %v", err)
	}
	if len(res.Content) == 0 {
		t.Fatal("context returned no content")
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("context content is %T", res.Content[0])
	}
	return res, tc.Text
}

func TestStateReportsRecordsOnlyContextClasses(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "main.go", "package main\n\nfunc Bound() {}\n")
	sess := connectRoot(t, root)

	history := filepath.Join(root, "history", workspace.Dot)
	if err := os.MkdirAll(history, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(history, "journal.ndjson"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	callText(t, sess, "rule", map[string]any{
		"op": "add", "dir": "live", "stem": "LIVE", "pattern": "U",
		"system": "live context", "response": "return code 0",
	})

	out := callText(t, sess, "state", map[string]any{})
	for _, want := range []string{
		"#contexts",
		"cx history class=historical-only mode=- rules=0 open=0 unbound=0",
		"cx live class=records-only-live mode=- rules=1 open=0 unbound=1",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("state missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(callText(t, sess, "state", map[string]any{"path": "elsewhere"}), "#contexts") {
		t.Fatal("path-scoped state leaked unrelated contexts")
	}
}

func TestContextLogicalMutationAndValidation(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "main.go", "package main\n\nfunc Bound() {}\n")
	sess := connectRoot(t, root)
	callText(t, sess, "rule", map[string]any{
		"op": "add", "dir": "logical", "stem": "LOG", "pattern": "U",
		"system": "logical context", "response": "return code 0",
		"applies": []string{"go:main.Bound"},
	})

	before, err := os.ReadFile(filepath.Join(root, "logical", workspace.Dot, "spec.md"))
	if err != nil {
		t.Fatal(err)
	}
	res, out := contextCall(t, sess, map[string]any{"op": "set", "dir": "logical", "mode": "logical"})
	if res.IsError || !strings.Contains(out, "mode=logical class=records-only-live") {
		t.Fatalf("logical set failed: isError=%v out=%q", res.IsError, out)
	}
	after, err := os.ReadFile(filepath.Join(root, "logical", workspace.Dot, "spec.md"))
	if err != nil {
		t.Fatal(err)
	}
	_, beforeBody := splitSpecBody(t, before)
	_, afterBody := splitSpecBody(t, after)
	if beforeBody != afterBody {
		t.Fatalf("context mutation changed spec body:\nbefore=%q\nafter =%q", beforeBody, afterBody)
	}

	state := callText(t, sess, "state", map[string]any{})
	if !strings.Contains(state, "cx logical class=records-only-live mode=logical rules=1 open=0 unbound=0") {
		t.Fatalf("logical state missing:\n%s", state)
	}
	check := callText(t, sess, "check", map[string]any{})
	if strings.Contains(check, "! CTX") {
		t.Fatalf("valid logical context failed check:\n%s", check)
	}

	res, out = contextCall(t, sess, map[string]any{"op": "set", "dir": "logical", "mode": "physical"})
	if res.IsError || !strings.Contains(out, "mode=-") {
		t.Fatalf("clear failed: isError=%v out=%q", res.IsError, out)
	}
	check = callText(t, sess, "check", map[string]any{})
	if !strings.Contains(check, "context-records-only-live") {
		t.Fatalf("cleared live context was not rejected:\n%s", check)
	}
}

func TestContextLogicalRefusalsAndTypedFindings(t *testing.T) {
	t.Run("unbound", func(t *testing.T) {
		root := t.TempDir()
		sess := connectRoot(t, root)
		callText(t, sess, "rule", map[string]any{
			"op": "add", "dir": "ctx", "stem": "CTX", "pattern": "U",
			"system": "context", "response": "return code 0",
		})
		res, out := contextCall(t, sess, map[string]any{"op": "set", "dir": "ctx", "mode": "logical"})
		if !res.IsError || !strings.Contains(out, "context-logical-unbound-rule") {
			t.Fatalf("unbound logical set = isError=%v out=%q", res.IsError, out)
		}
		if err := spec.SetContextMode(workspace.Root{Dir: root}, "ctx", spec.ContextModeLogical); err != nil {
			t.Fatal(err)
		}
		check := callText(t, sess, "check", map[string]any{})
		if !strings.Contains(check, "context-logical-unbound-rule CTX-001") {
			t.Fatalf("typed unbound finding missing:\n%s", check)
		}
	})

	t.Run("open work", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, root, "main.go", "package main\n\nfunc Bound() {}\n")
		sess := connectRoot(t, root)
		callText(t, sess, "rule", map[string]any{
			"op": "add", "dir": "ctx", "stem": "CTX", "pattern": "U",
			"system": "context", "response": "return code 0", "applies": []string{"go:main.Bound"},
		})
		callText(t, sess, "draft", map[string]any{"kind": "task", "title": "open context work", "dir": "ctx"})
		res, out := contextCall(t, sess, map[string]any{"op": "set", "dir": "ctx", "mode": "logical"})
		if !res.IsError || !strings.Contains(out, "context-logical-open-work") {
			t.Fatalf("open-work logical set = isError=%v out=%q", res.IsError, out)
		}
		if err := spec.SetContextMode(workspace.Root{Dir: root}, "ctx", spec.ContextModeLogical); err != nil {
			t.Fatal(err)
		}
		check := callText(t, sess, "check", map[string]any{})
		if !strings.Contains(check, "context-logical-open-work") {
			t.Fatalf("typed open-work finding missing:\n%s", check)
		}
	})

	t.Run("stale declaration", func(t *testing.T) {
		root := t.TempDir()
		ws := workspace.Root{Dir: root}
		if err := ws.EnsureScaffold("ctx"); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(ws.SpecPath("ctx"), []byte("---\nschema: v1\n---\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := spec.SetContextMode(ws, "ctx", spec.ContextModeLogical); err != nil {
			t.Fatal(err)
		}
		sess := connectRoot(t, root)
		check := callText(t, sess, "check", map[string]any{})
		if !strings.Contains(check, "context-logical-stale class=historical-only") {
			t.Fatalf("stale declaration warning missing:\n%s", check)
		}
	})
}

func splitSpecBody(t *testing.T, raw []byte) (string, string) {
	t.Helper()
	s := string(raw)
	if !strings.HasPrefix(s, "---\n") {
		t.Fatalf("spec has no frontmatter: %q", s)
	}
	i := strings.Index(s[4:], "\n---\n")
	if i < 0 {
		t.Fatalf("spec has no closing frontmatter: %q", s)
	}
	i += 4
	return s[:i+5], s[i+5:]
}
