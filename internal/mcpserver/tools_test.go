package mcpserver

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jxsl13/spectackle/internal/drift"
	"github.com/jxsl13/spectackle/internal/workspace"
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
		"lease": false, "work": false, "swarm": false, "state": false,
		"research": false, "grill": false, "decide": false, "commands": false,
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
	// output diet (T-0015): empty sections are omitted entirely, not filled
	// with "ok ..." filler — an empty tempdir workspace has no indexed nodes,
	// no applicable rules and no similar rejections, so none of the three
	// context-pack headers should appear.
	for _, sec := range []string{"#impact", "#contracts", "#rejections"} {
		if strings.Contains(out, sec) {
			t.Fatalf("draft emitted empty context pack section %s: %q", sec, out)
		}
	}
	for _, filler := range []string{"ok radius empty", "ok no applicable rules", "ok none similar"} {
		if strings.Contains(out, filler) {
			t.Fatalf("draft emitted filler line %q: %q", filler, out)
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

	// archive is a legal forward skip straight from active (implies done);
	// the only guard left is open children, and T-0001 is already done
	out = callText(t, sess, "move", map[string]any{"id": "P-0001", "to": "archived"})
	if !strings.Contains(out, "archived") {
		t.Fatalf("archive from active (forward skip, implies done): %q", out)
	}

	// work.md is empty again; intent carries the merged delta
	spec, err := os.ReadFile(filepath.Join(root, ".spectackle", "spec.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(spec), "## intent") || !strings.Contains(string(spec), "P-0001 strided saxpy access") {
		t.Fatalf("archive did not merge into intent:\n%s", spec)
	}
	// gone from work.md, but never from the referenceable universe: get
	// resolves it as a journal tombstone (LCY-001) instead of nf
	out = callText(t, sess, "get", map[string]any{"id": "P-0001"})
	if !strings.Contains(out, "archived") || !strings.Contains(out, "journal tombstone") {
		t.Fatalf("archived item should resolve via tombstone, not nf: %q", out)
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

// TestRuleAnchorsReconcileOnEditAndRetire is the regression test for T-0044
// (DRF-001): a rule add anchors every node in applies; editing the rule to a
// smaller applies set must drop the anchor rows of the nodes that fell out,
// not just add rows for the ones that stayed — otherwise a mistyped node
// (e.g. IDX-001's original go:index.Indexer.IndexAll) leaves a permanently
// stale row once the typo is corrected via edit. Retiring a rule must drop
// every one of its remaining anchor rows.
func TestRuleAnchorsReconcileOnEditAndRetire(t *testing.T) {
	root := t.TempDir()
	sess := connectRoot(t, root)
	ws := workspace.Root{Dir: root}

	out := callText(t, sess, "rule", map[string]any{
		"op": "add", "dir": "drift_probe", "pattern": "U", "stem": "DRF-PRB",
		"system":   "the drift prober",
		"response": "anchor to two nodes for the reconcile regression test",
		"applies":  []string{"go:pkg.A", "go:pkg.B"},
	})
	if !strings.Contains(out, "ok DRF-PRB-001") {
		t.Fatalf("rule add: %q", out)
	}

	anchors, err := drift.Load(ws)
	if err != nil {
		t.Fatal(err)
	}
	rows := func(anchors []drift.Anchor, rule string) map[string]bool {
		m := map[string]bool{}
		for _, a := range anchors {
			if a.Rule == rule {
				m[string(a.Node)] = true
			}
		}
		return m
	}
	if got := rows(anchors, "DRF-PRB-001"); len(got) != 2 || !got["go:pkg.A"] || !got["go:pkg.B"] {
		t.Fatalf("add should anchor both nodes: %v", got)
	}

	// edit: drop go:pkg.A from applies
	out = callText(t, sess, "rule", map[string]any{
		"op": "edit", "id": "DRF-PRB-001",
		"applies": []string{"go:pkg.B"},
	})
	if !strings.Contains(out, "ok DRF-PRB-001") {
		t.Fatalf("rule edit: %q", out)
	}
	anchors, err = drift.Load(ws)
	if err != nil {
		t.Fatal(err)
	}
	if got := rows(anchors, "DRF-PRB-001"); len(got) != 1 || !got["go:pkg.B"] {
		t.Fatalf("edit must reconcile away the dropped node, leaving only go:pkg.B: %v", got)
	}

	// retire: every remaining anchor for the rule must go, not just the last applies
	out = callText(t, sess, "rule", map[string]any{"op": "retire", "id": "DRF-PRB-001"})
	if !strings.Contains(out, "retired") {
		t.Fatalf("rule retire: %q", out)
	}
	anchors, err = drift.Load(ws)
	if err != nil {
		t.Fatal(err)
	}
	if got := rows(anchors, "DRF-PRB-001"); len(got) != 0 {
		t.Fatalf("retire must drop all anchors of the rule: %v", got)
	}
}

// TestCheckOrphanApplies (MCP-004): a live rule whose {applies: node} pair
// has no anchors.tsv row is binding intent without a binding — check must
// surface it as one dense `g orphan <rule> <node>` record; a complete
// anchor set must stay silent.
func TestCheckOrphanApplies(t *testing.T) {
	root := t.TempDir()
	sess := connectRoot(t, root)
	ws := workspace.Root{Dir: root}

	out := callText(t, sess, "rule", map[string]any{
		"op": "add", "dir": "orphan_probe", "pattern": "U", "stem": "ORP-PRB",
		"system":   "the orphan prober",
		"response": "anchor to two nodes for the orphan coverage test",
		"applies":  []string{"go:pkg.A", "go:pkg.B"},
	})
	if !strings.Contains(out, "ok ORP-PRB-001") {
		t.Fatalf("rule add: %q", out)
	}

	// complete anchor set: no orphan record
	out = callText(t, sess, "check", map[string]any{})
	if strings.Contains(out, "g orphan ORP-PRB-001") {
		t.Fatalf("complete anchor set must not report orphans: %q", out)
	}

	// drop the go:pkg.A row behind the server's back (manual edit / replay loss)
	anchors, err := drift.Load(ws)
	if err != nil {
		t.Fatal(err)
	}
	kept := anchors[:0]
	for _, a := range anchors {
		if a.Rule == "ORP-PRB-001" && a.Node == "go:pkg.A" {
			continue
		}
		kept = append(kept, a)
	}
	if err := drift.Save(ws, kept); err != nil {
		t.Fatal(err)
	}

	out = callText(t, sess, "check", map[string]any{})
	if !strings.Contains(out, "g orphan ORP-PRB-001 go:pkg.A") {
		t.Fatalf("missing anchor row must surface as orphan record: %q", out)
	}
	if strings.Contains(out, "g orphan ORP-PRB-001 go:pkg.B") {
		t.Fatalf("still-anchored node must not be reported: %q", out)
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
	cfg := filepath.Join(root, ".spectackle", "config.yaml")
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

// TestCompactMergeableCandidates (MCP-005): two near-identical same-pattern
// rules in one spec file surface as one `c <dir> mergeable A+B` suggestion
// in the compact dry-run; a dissimilar third rule is never paired, and
// apply=true does not merge anything.
func TestCompactMergeableCandidates(t *testing.T) {
	root := t.TempDir()
	sess := connectRoot(t, root)

	add := func(stem, response string) {
		t.Helper()
		out := callText(t, sess, "rule", map[string]any{
			"op": "add", "dir": "merge_probe", "pattern": "U", "stem": stem,
			"system": "the merge prober", "response": response,
		})
		if !strings.Contains(out, "ok "+stem) {
			t.Fatalf("rule add %s: %q", stem, out)
		}
	}
	add("MRG-A", "write every anchor row for the rule to the anchors file on disk")
	add("MRG-B", "write every anchor row for the rule to the anchors file")
	add("MRG-C", "reject a claim whose scope overlaps a live foreign lease")

	out := callText(t, sess, "compact", map[string]any{})
	if !strings.Contains(out, "mergeable MRG-A-001+MRG-B-001") {
		t.Fatalf("near-identical pair not suggested: %q", out)
	}
	if strings.Contains(out, "MRG-C-001") {
		t.Fatalf("dissimilar rule must not be paired: %q", out)
	}

	out = callText(t, sess, "compact", map[string]any{"apply": true})
	if strings.Contains(out, "ok merged") {
		t.Fatalf("apply must never auto-merge: %q", out)
	}
	for _, id := range []string{"MRG-A-001", "MRG-B-001", "MRG-C-001"} {
		if got := callText(t, sess, "get", map[string]any{"id": id}); !strings.Contains(got, id) {
			t.Fatalf("rule %s must survive apply: %q", id, got)
		}
	}
}

// TestFindFocusReranks (SPX-GRA-004): with focus set, a low-degree direct
// callee of the focus node outranks a high-degree hub sitting elsewhere in
// the package; an unknown focus yields nf corrections, not an error.
func TestFindFocusReranks(t *testing.T) {
	root := t.TempDir()
	// Near is called only by Focus (degree 1); Hub is called by three
	// helpers (degree 3) and wins the global-rank ordering.
	src := `package demo

func Focus() { Near() }

func Near() {}

func Hub() {}

func A() { Hub() }

func B() { Hub() }

func C() { Hub() }
`
	if err := os.WriteFile(filepath.Join(root, "demo.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	sess := connectRoot(t, root)

	// global rank: the hub leads, the low-degree near node doesn't make
	// the cut (or trails the hub)
	out := callText(t, sess, "find", map[string]any{"q": "demo", "scope": "code", "k": 4})
	hubIdx, nearIdx := strings.Index(out, "go:demo.Hub"), strings.Index(out, "go:demo.Near")
	if hubIdx < 0 || (nearIdx >= 0 && nearIdx < hubIdx) {
		t.Fatalf("global rank should lead with the hub: %q", out)
	}

	// focused rank: the focus neighborhood leads
	out = callText(t, sess, "find", map[string]any{"q": "demo", "scope": "code", "k": 4, "focus": "go:demo.Focus"})
	hubIdx, nearIdx = strings.Index(out, "go:demo.Hub"), strings.Index(out, "go:demo.Near")
	if nearIdx < 0 {
		t.Fatalf("focused find lost the near node: %q", out)
	}
	if hubIdx >= 0 && hubIdx < nearIdx {
		t.Fatalf("focus must rank the direct callee above the far hub: %q", out)
	}

	out = callText(t, sess, "find", map[string]any{"q": "demo", "scope": "code", "focus": "go:demo.Missing"})
	if !strings.Contains(out, "nf ") {
		t.Fatalf("unknown focus must yield nf corrections: %q", out)
	}
}

// TestGetNodeShowsContracts (SPX-SPC-007): get on a code node appends the
// node's binding contracts — the applies-bound rule as a full r record,
// root-scoped cascade rules collapsed to one r-root ID record — while
// impact neighbors stay bare graph records.
func TestGetNodeShowsContracts(t *testing.T) {
	root := t.TempDir()
	src := "package demo\n\nfunc Foo() {}\n"
	if err := os.WriteFile(filepath.Join(root, "demo.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	sess := connectRoot(t, root)

	out := callText(t, sess, "rule", map[string]any{
		"op": "add", "dir": "demo_rules", "pattern": "U", "stem": "DMO",
		"system":   "the demo function",
		"response": "stay a stub for the ForNode contract test",
		"applies":  []string{"go:demo.Foo"},
	})
	if !strings.Contains(out, "ok DMO-001") {
		t.Fatalf("rule add: %q", out)
	}

	out = callText(t, sess, "get", map[string]any{"id": "go:demo.Foo"})
	if !strings.Contains(out, "n go:demo.Foo") {
		t.Fatalf("node record missing: %q", out)
	}
	if !strings.Contains(out, "r DMO-001") {
		t.Fatalf("applies-bound contract missing from get: %q", out)
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
	if strings.Contains(out, "! E") || strings.Contains(out, "d changed") || strings.Contains(out, "d gone") || strings.Contains(out, "g orphan") {
		t.Fatalf("check on own repo not clean:\n%s", out)
	}
}

// TestDraftContextPackElision (T-0015): root-scoped rules collapse to one
// r-root ID record (no full text repeated), and a draft with no similar
// rejections omits the #rejections header entirely.
func TestDraftContextPackElision(t *testing.T) {
	root := t.TempDir()
	sess := connectRoot(t, root)

	// author one root-scoped rule (dir="" => root context)
	out := callText(t, sess, "rule", map[string]any{
		"op": "add", "dir": "", "pattern": "U", "stem": "SPX-ARC",
		"system":   "the test workspace root",
		"response": "log every write to a file named `audit.log`",
	})
	if !strings.Contains(out, "ok SPX-ARC-001") {
		t.Fatalf("root rule add: %q", out)
	}

	// a path target with no nested .spectackle context resolves only the
	// root-scoped rule above
	out = callText(t, sess, "draft", map[string]any{
		"kind": "task", "title": "output diet probe",
		"targets": []string{"pkg/nested/file.go"},
	})
	if strings.Contains(out, "#rejections") {
		t.Fatalf("draft with no similar rejections must omit #rejections: %q", out)
	}
	if !strings.Contains(out, "#contracts") {
		t.Fatalf("draft missing #contracts: %q", out)
	}
	lines := strings.Split(out, "\n")
	var rootLines []string
	for _, l := range lines {
		if strings.HasPrefix(l, "r-root ") {
			rootLines = append(rootLines, l)
		}
		if strings.HasPrefix(l, "r SPX-ARC-001 ") {
			t.Fatalf("root rule leaked full text instead of r-root: %q", out)
		}
	}
	if len(rootLines) != 1 {
		t.Fatalf("expected exactly one r-root line, got %d: %q", len(rootLines), out)
	}
	if !strings.Contains(rootLines[0], "SPX-ARC-001") {
		t.Fatalf("r-root line missing SPX-ARC-001: %q", rootLines[0])
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

// TestFindCodeRendersEndLineSpan (T-0049, MCP-002): a node record renders
// '<file>:<start>-<end>' once EndLine is known and > Line (Bar, a multi-line
// func), and keeps the plain '<file>:<line>' form when EndLine == Line
// (Foo, a single-line func) — asserted over the wire via the "find"
// scope=code tool, the same connectRoot pattern the rest of this file uses.
func TestFindCodeRendersEndLineSpan(t *testing.T) {
	root := t.TempDir()
	src := "package demo\n\nfunc Foo() {}\n\nfunc Bar() {\n\t_ = 1\n}\n"
	if err := os.WriteFile(filepath.Join(root, "demo.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	sess := connectRoot(t, root)

	out := callText(t, sess, "find", map[string]any{"q": "demo", "scope": "code"})
	if !strings.Contains(out, "n go:demo.Foo fn demo.go:3 ") {
		t.Fatalf("single-line node must keep plain file:line form: %q", out)
	}
	if !strings.Contains(out, "n go:demo.Bar fn demo.go:5-7 ") {
		t.Fatalf("multi-line node must render file:start-end span: %q", out)
	}
	if strings.Contains(out, "demo.go:5 ") {
		t.Fatalf("multi-line node must not also render the old single-line form: %q", out)
	}
}
