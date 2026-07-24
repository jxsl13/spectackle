package mcpserver

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// connectRoot spins up the server over an in-memory transport against a
// workspace root and returns a live client session.
func connectRoot(t *testing.T, root string) *mcp.ClientSession {
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
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	sess, err := client.Connect(context.Background(), ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sess.Close() })
	return sess
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

func TestToolSurface(t *testing.T) {
	sess := connectRoot(t, t.TempDir())
	res, err := sess.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"find": false, "get": false, "draft": false, "rule": false,
		"move": false, "check": false, "compact": false,
		"lease": false, "work": false, "swarm": false,
	}
	for _, tool := range res.Tools {
		if _, ok := want[tool.Name]; !ok {
			t.Errorf("unexpected tool %q (surface must stay minimal)", tool.Name)
			continue
		}
		want[tool.Name] = true
	}
	for name, seen := range want {
		if !seen {
			t.Errorf("tool %q not registered", name)
		}
	}
}

// TestLifecycleE2E drives draft -> submit -> approve -> active -> done ->
// archive over the wire and asserts the persistence effects.
func TestLifecycleE2E(t *testing.T) {
	root := t.TempDir()
	sess := connectRoot(t, root)

	out := callText(t, sess, "draft", map[string]any{
		"kind": "proposal", "title": "strided saxpy access",
		"body":    "Support strided x/y access in the saxpy chain.",
		"targets": []string{"gpu/kernels/saxpy.cu", "gpu/saxpy.go"},
	})
	if !strings.Contains(out, "i P-0001 proposal draft") {
		t.Fatalf("draft: %q", out)
	}
	for _, sec := range []string{"#impact", "#contracts", "#rejections"} {
		if !strings.Contains(out, sec) {
			t.Fatalf("draft missing context pack section %s: %q", sec, out)
		}
	}

	// child task
	out = callText(t, sess, "draft", map[string]any{
		"kind": "task", "title": "adjust kernel indexing", "parent": "P-0001",
	})
	if !strings.Contains(out, "i T-0001 task draft") {
		t.Fatalf("task draft: %q", out)
	}

	for _, mv := range [][2]string{
		{"P-0001", "submitted"}, {"P-0001", "approved"}, {"P-0001", "active"},
		{"T-0001", "active"}, {"T-0001", "done"},
	} {
		out = callText(t, sess, "move", map[string]any{"id": mv[0], "to": mv[1]})
		if !strings.Contains(out, mv[1]) {
			t.Fatalf("move %s->%s: %q", mv[0], mv[1], out)
		}
	}

	// archive blocked while proposal not done; done blocked? active->done ok
	out = callText(t, sess, "move", map[string]any{"id": "P-0001", "to": "archived"})
	if !strings.Contains(out, "! ARG E") {
		t.Fatalf("archive from active must fail: %q", out)
	}
	callText(t, sess, "move", map[string]any{"id": "P-0001", "to": "done"})
	out = callText(t, sess, "move", map[string]any{"id": "P-0001", "to": "archived"})
	if !strings.Contains(out, "archived") {
		t.Fatalf("archive: %q", out)
	}

	// work.md is empty again; intent carries the merged delta
	spec, err := os.ReadFile(filepath.Join(root, ".spectacle", "spec.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(spec), "## intent") || !strings.Contains(string(spec), "P-0001 strided saxpy access") {
		t.Fatalf("archive did not merge into intent:\n%s", spec)
	}
	out = callText(t, sess, "get", map[string]any{"id": "P-0001"})
	if !strings.Contains(out, "nf") {
		t.Fatalf("archived item should be gone from work.md: %q", out)
	}
	// history still knows it
	out = callText(t, sess, "find", map[string]any{"q": "strided", "scope": "history"})
	if !strings.Contains(out, "P-0001") {
		t.Fatalf("history lost the archived item: %q", out)
	}
}

// TestRejectionCorpusAndRevocation: rejection requires a note, becomes
// searchable, and can be revoked back into a previous state.
func TestRejectionCorpusAndRevocation(t *testing.T) {
	root := t.TempDir()
	sess := connectRoot(t, root)

	callText(t, sess, "draft", map[string]any{
		"kind": "proposal", "title": "cache kernels in VRAM",
		"body": "Keep compiled kernels resident.",
	})
	// note is mandatory
	out := callText(t, sess, "move", map[string]any{"id": "P-0001", "to": "rejected"})
	if !strings.Contains(out, "! ARG E") || !strings.Contains(out, "note") {
		t.Fatalf("rejection without note must fail: %q", out)
	}
	callText(t, sess, "move", map[string]any{
		"id": "P-0001", "to": "rejected",
		"note": "VRAM residency breaks multi-tenant GPU scheduling",
	})

	// searchable corpus
	out = callText(t, sess, "find", map[string]any{"q": "multi-tenant scheduling", "scope": "rejection"})
	if !strings.Contains(out, "P-0001") {
		t.Fatalf("rejection not searchable: %q", out)
	}

	// revocable: back to draft, item restored with body
	out = callText(t, sess, "move", map[string]any{"id": "P-0001", "to": "draft"})
	if !strings.Contains(out, "i P-0001 proposal draft") {
		t.Fatalf("revocation failed: %q", out)
	}
	out = callText(t, sess, "get", map[string]any{"id": "P-0001"})
	if !strings.Contains(out, "Keep compiled kernels resident.") {
		t.Fatalf("revoked item lost its body: %q", out)
	}
	// the reject event stays in history even after revocation
	out = callText(t, sess, "find", map[string]any{"q": "multi-tenant", "scope": "rejection"})
	if !strings.Contains(out, "P-0001") {
		t.Fatalf("revocation must not erase the rejection corpus: %q", out)
	}
}

// TestRuleLifecycle: add via slots (lint gate, auto-ID), edit, retire.
func TestRuleLifecycle(t *testing.T) {
	root := t.TempDir()
	sess := connectRoot(t, root)

	// missing slots, no elicitation UI -> need records
	out := callText(t, sess, "rule", map[string]any{"op": "add"})
	if !strings.Contains(out, "need pattern") {
		t.Fatalf("expected need records: %q", out)
	}

	out = callText(t, sess, "rule", map[string]any{
		"op": "add", "dir": "cuda_kernels", "pattern": "E", "stem": "CUDA-KRN",
		"system":   "host wrapper",
		"response": "check cudaGetLastError and propagate its numeric value to the caller",
		"trigger":  "a kernel launch statement returns",
	})
	if !strings.Contains(out, "ok CUDA-KRN-001") {
		t.Fatalf("rule add: %q", out)
	}

	// vague slots rejected before write
	out = callText(t, sess, "rule", map[string]any{
		"op": "add", "dir": "cuda_kernels", "pattern": "U",
		"system": "kernel", "response": "handle memory appropriately",
	})
	if !strings.Contains(out, "E004") || !strings.Contains(out, "REJECTED") {
		t.Fatalf("expected E004 rejection: %q", out)
	}

	// resolves through the cascade
	out = callText(t, sess, "get", map[string]any{"id": "cuda_kernels/saxpy.cu"})
	if !strings.Contains(out, "CUDA-KRN-001") {
		t.Fatalf("contracts missing authored rule: %q", out)
	}

	// edit: recompose
	out = callText(t, sess, "rule", map[string]any{
		"op": "edit", "id": "CUDA-KRN-001", "pattern": "E",
		"system":   "host wrapper",
		"response": "check cudaGetLastError and return its numeric value as int",
		"trigger":  "a kernel launch statement returns",
	})
	if !strings.Contains(out, "ok CUDA-KRN-001") {
		t.Fatalf("rule edit: %q", out)
	}

	// retire: gone from cascade, text survives in journal
	out = callText(t, sess, "rule", map[string]any{"op": "retire", "id": "CUDA-KRN-001"})
	if !strings.Contains(out, "retired") {
		t.Fatalf("rule retire: %q", out)
	}
	out = callText(t, sess, "find", map[string]any{"q": "cudaGetLastError", "scope": "history"})
	if !strings.Contains(out, "CUDA-KRN-001") {
		t.Fatalf("retired rule text lost from journal: %q", out)
	}
}

// TestCompactKeepsRejections: journal folds drop noise but never reject lines.
func TestCompactKeepsRejections(t *testing.T) {
	root := t.TempDir()
	sess := connectRoot(t, root)

	callText(t, sess, "draft", map[string]any{"kind": "bug", "title": "nan in reduction"})
	callText(t, sess, "move", map[string]any{"id": "B-0001", "to": "rejected", "note": "not reproducible on sm90"})
	callText(t, sess, "draft", map[string]any{"kind": "task", "title": "noise a"})
	callText(t, sess, "draft", map[string]any{"kind": "task", "title": "noise b"})

	// force the fold threshold down via config
	cfg := filepath.Join(root, ".spectacle", "config.yaml")
	if err := os.WriteFile(cfg, []byte("schema: v0\ncompact:\n  journal_max: 2\n  done_max: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sess2 := connectRoot(t, root) // fresh server picks up the config

	out := callText(t, sess2, "compact", map[string]any{})
	if !strings.Contains(out, "c . journal") {
		t.Fatalf("expected journal fold candidate: %q", out)
	}
	out = callText(t, sess2, "compact", map[string]any{"apply": true})
	if !strings.Contains(out, "ok folded") {
		t.Fatalf("compact apply: %q", out)
	}
	// rejection survives the fold
	out = callText(t, sess2, "find", map[string]any{"q": "sm90", "scope": "rejection"})
	if !strings.Contains(out, "B-0001") {
		t.Fatalf("compact dropped a rejection: %q", out)
	}
}

// TestCheckOnOwnRepo: the repository itself must come back clean.
func TestCheckOnOwnRepo(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	sess := connectRoot(t, root)
	out := callText(t, sess, "check", map[string]any{})
	if strings.Contains(out, "! E") || strings.Contains(out, "d changed") || strings.Contains(out, "d gone") {
		t.Fatalf("check on own repo not clean:\n%s", out)
	}
}

// TestConcurrentDraftsMintUniqueIDs is the regression test for the race
// found while dogfooding: the MCP SDK dispatches tool calls concurrently,
// and unserialised drafts minted the same item ID and clobbered work.md.
func TestConcurrentDraftsMintUniqueIDs(t *testing.T) {
	sess := connectRoot(t, t.TempDir())

	const n = 8
	results := make(chan string, n)
	for i := 0; i < n; i++ {
		go func(i int) {
			out := callText(t, sess, "draft", map[string]any{
				"kind": "task", "title": fmt.Sprintf("concurrent %d", i),
			})
			results <- strings.Fields(out)[1] // "i <ID> ..."
		}(i)
	}
	seen := map[string]bool{}
	for i := 0; i < n; i++ {
		id := <-results
		if seen[id] {
			t.Fatalf("duplicate item ID minted concurrently: %s", id)
		}
		seen[id] = true
	}
	// all n blocks must have survived the concurrent work.md rewrites
	out := callText(t, sess, "get", map[string]any{"id": "."})
	for id := range seen {
		if !strings.Contains(out, id) {
			t.Fatalf("item %s lost by concurrent write:\n%s", id, out)
		}
	}
	// and check must not report E101 duplicates
	out = callText(t, sess, "check", map[string]any{})
	if strings.Contains(out, "E101") {
		t.Fatalf("duplicate IDs after concurrent drafts:\n%s", out)
	}
}
