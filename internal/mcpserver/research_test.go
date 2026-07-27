package mcpserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// newTestServer builds a *Server directly against root (no MCP session, no
// transport) — research and grill are deliberately NOT registered
// (tools.go/server.go stay untouched by this task), so tests must call the
// handler methods directly, the same pattern state_test.go's connectRoot
// wraps for the registered tools.
func newTestServer(t *testing.T, root string) *Server {
	t.Helper()
	s, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// resText unwraps a direct handler call's result into its text body, failing
// the test on error or unexpected shape. Callers must evaluate the handler
// call first (res, _, err := s.research(...)) — Go's multi-value call
// forwarding cannot be combined with a leading *testing.T argument.
func resText(t *testing.T, res *mcp.CallToolResult, err error) string {
	t.Helper()
	if err != nil {
		t.Fatalf("call error: %v", err)
	}
	if res == nil || len(res.Content) == 0 {
		t.Fatalf("empty content")
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("content is %T, want TextContent", res.Content[0])
	}
	return tc.Text
}

// serverDraftID drafts through the handler method (the direct-call pattern
// newTestServer exists for) and returns the ID the server minted. Since
// ADR-0013 (T-0135) item IDs are UUIDv7-derived, so no test can spell one
// out; it has to be read back off the `i <id> ...` record draft answers with.
func serverDraftID(t *testing.T, s *Server, in draftIn) string {
	t.Helper()
	res, _, err := s.draft(in)
	return idOfRecord(t, resText(t, res, err), "i")
}

// TestResearchEmptyFallback: no q, no targets — nothing to search, and the
// tool must still return a non-empty, informative result rather than "".
func TestResearchEmptyFallback(t *testing.T) {
	s := newTestServer(t, t.TempDir())
	res, _, err := s.research(researchIn{})
	out := resText(t, res, err)
	if !strings.Contains(out, "ok research empty") {
		t.Fatalf("research empty fallback: %q", out)
	}
	for _, sec := range []string{"#impact", "#contracts", "#rejections", "#history", "#docs", "#gaps", "#open"} {
		if strings.Contains(out, sec) {
			t.Fatalf("research emitted %s on empty input: %q", sec, out)
		}
	}
}

// TestResearchImpact: a node-ID target seeds the BFS directly; a matching
// q seeds it via graph.Find — both reach the same reachable subgraph.
func TestResearchImpact(t *testing.T) {
	root := t.TempDir()
	src := "package demo\n\nfunc Foo() { Bar() }\n\nfunc Bar() {}\n"
	if err := os.WriteFile(filepath.Join(root, "demo.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	s := newTestServer(t, root)

	res, _, err := s.research(researchIn{Targets: []string{"go:demo.Foo"}, Depth: 2})
	out := resText(t, res, err)
	if !strings.Contains(out, "#impact") {
		t.Fatalf("missing #impact: %q", out)
	}
	if !strings.Contains(out, "n go:demo.Foo") || !strings.Contains(out, "n go:demo.Bar") {
		t.Fatalf("impact missing nodes: %q", out)
	}
	if !strings.Contains(out, "e go:demo.Foo") {
		t.Fatalf("impact missing edge: %q", out)
	}

	// query-seeded: "Foo" matches go:demo.Foo by ID suffix (graph.Find), no
	// targets needed.
	res2, _, err2 := s.research(researchIn{Q: "Foo"})
	out2 := resText(t, res2, err2)
	if !strings.Contains(out2, "n go:demo.Foo") {
		t.Fatalf("q-seeded impact missing go:demo.Foo: %q", out2)
	}
}

// TestResearchContracts: a root-scoped rule collapses to r-root for any
// path target, mirroring draft's #contracts section.
func TestResearchContracts(t *testing.T) {
	root := t.TempDir()
	s := newTestServer(t, root)

	rres, _, rerr := s.rule(ruleIn{
		Op: "add", Pattern: "U", Stem: "DEMO",
		System: "demo pkg", Response: "do the demo thing",
	})
	ruleOut := resText(t, rres, rerr)
	if !strings.Contains(ruleOut, "ok DEMO-001") {
		t.Fatalf("rule add: %q", ruleOut)
	}

	res, _, err := s.research(researchIn{Targets: []string{"demo.go"}})
	out := resText(t, res, err)
	if !strings.Contains(out, "#contracts") {
		t.Fatalf("missing #contracts: %q", out)
	}
	if !strings.Contains(out, "r-root DEMO-001") {
		t.Fatalf("contracts missing r-root: %q", out)
	}
}

// TestResearchGapsGraphSide: a node target absent from the graph -> g
// unknown; a node target present but never bound by any rule's applies -> g
// unanchored.
func TestResearchGapsGraphSide(t *testing.T) {
	root := t.TempDir()
	src := "package demo\n\nfunc Foo() {}\n\nfunc Bar() {}\n"
	if err := os.WriteFile(filepath.Join(root, "demo.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	s := newTestServer(t, root)

	res, _, err := s.research(researchIn{Targets: []string{"go:demo.Foo", "go:demo.Missing"}})
	out := resText(t, res, err)
	if !strings.Contains(out, "#gaps") {
		t.Fatalf("missing #gaps: %q", out)
	}
	if !strings.Contains(out, "g unknown go:demo.Missing") {
		t.Fatalf("gaps missing unknown: %q", out)
	}
	if !strings.Contains(out, "g unanchored go:demo.Foo") {
		t.Fatalf("gaps missing unanchored: %q", out)
	}
}

// TestResearchGapsCoverage: a path target under a directory with zero
// applicable rules reuses check()'s coverageGaps walk.
func TestResearchGapsCoverage(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "pkg", "code.go"), []byte("package pkg\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := newTestServer(t, root)

	res, _, err := s.research(researchIn{Targets: []string{"pkg/code.go"}})
	out := resText(t, res, err)
	if !strings.Contains(out, "#gaps") {
		t.Fatalf("missing #gaps: %q", out)
	}
	if !strings.Contains(out, "g uncovered pkg") {
		t.Fatalf("gaps missing uncovered: %q", out)
	}
}

// TestResearchRejectionsAndHistory: a rejected item's summary is searchable
// under #rejections (kind=rejection); its create event surfaces under
// #history (kind=journal).
func TestResearchRejectionsAndHistory(t *testing.T) {
	root := t.TempDir()
	s := newTestServer(t, root)

	prop := serverDraftID(t, s, draftIn{Kind: "proposal", Title: "widget batching plan"})
	if _, _, err := s.move(moveIn{ID: prop, To: "rejected", Note: "too risky for now"}); err != nil {
		t.Fatal(err)
	}
	if err := s.scan.Refresh(); err != nil {
		t.Fatal(err)
	}

	res, _, err := s.research(researchIn{Q: "widget batching"})
	out := resText(t, res, err)
	if !strings.Contains(out, "#rejections") {
		t.Fatalf("missing #rejections: %q", out)
	}
	if !strings.Contains(out, prop) {
		t.Fatalf("rejections missing %s: %q", prop, out)
	}
	if !strings.Contains(out, "#history") {
		t.Fatalf("missing #history: %q", out)
	}
}

// TestResearchOpenQuestions: a target with no binding rule and a topic with
// no similar rejection both surface as q-lines under #open.
func TestResearchOpenQuestions(t *testing.T) {
	s := newTestServer(t, t.TempDir())

	res, _, err := s.research(researchIn{Q: "novel feature", Targets: []string{"pkg/new.go"}})
	out := resText(t, res, err)
	if !strings.Contains(out, "#open") {
		t.Fatalf("missing #open: %q", out)
	}
	if !strings.Contains(out, "q pkg/new.go has no binding rule") {
		t.Fatalf("open missing no-binding-rule question: %q", out)
	}
	if !strings.Contains(out, "q no similar rejection found - risk unknown") {
		t.Fatalf("open missing risk-unknown question: %q", out)
	}
}

// TestResearchReadOnly is the read-only contract test (mirrors state_test.go's
// TestStateReadOnly): mtimes of every versioned .spectackle bundle file, plus
// the journal's byte length, must be bit-for-bit identical before and after
// any number of research calls, including ones that exercise every section.
func TestResearchReadOnly(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "demo.go"), []byte("package demo\n\nfunc Foo() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := newTestServer(t, root)

	if _, _, err := s.draft(draftIn{Kind: "task", Title: "seed"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.rule(ruleIn{
		Op: "add", Pattern: "U", Stem: "DEMO", System: "demo", Response: "do the thing",
		Applies: []string{"go:demo.Foo"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.scan.Refresh(); err != nil {
		t.Fatal(err)
	}

	files := []string{
		filepath.Join(root, ".spectackle", "spec.md"),
		filepath.Join(root, ".spectackle", "work.md"),
		filepath.Join(root, ".spectackle", "journal.ndjson"),
		filepath.Join(root, ".spectackle", "anchors.tsv"),
	}
	before := map[string]os.FileInfo{}
	for _, f := range files {
		st, err := os.Stat(f)
		if err != nil {
			t.Fatalf("stat %s: %v", f, err)
		}
		before[f] = st
	}
	journalBefore, err := os.ReadFile(filepath.Join(root, ".spectackle", "journal.ndjson"))
	if err != nil {
		t.Fatal(err)
	}

	for _, in := range []researchIn{
		{},
		{Q: "demo"},
		{Targets: []string{"go:demo.Foo", "demo.go"}},
		{Q: "demo", Targets: []string{"go:demo.Foo"}, Depth: 3, Budget: 100},
	} {
		if _, _, err := s.research(in); err != nil {
			t.Fatal(err)
		}
	}

	for _, f := range files {
		st, err := os.Stat(f)
		if err != nil {
			t.Fatalf("stat %s after: %v", f, err)
		}
		if st.Size() != before[f].Size() || !st.ModTime().Equal(before[f].ModTime()) {
			t.Fatalf("research wrote to %s: size %d->%d mtime %s->%s",
				f, before[f].Size(), st.Size(), before[f].ModTime(), st.ModTime())
		}
	}
	journalAfter, err := os.ReadFile(filepath.Join(root, ".spectackle", "journal.ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	if string(journalAfter) != string(journalBefore) {
		t.Fatalf("research changed the journal:\nbefore=%q\nafter=%q", journalBefore, journalAfter)
	}
}
