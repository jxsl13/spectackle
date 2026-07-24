package mcpserver

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jxsl13/spectackle/internal/drift"
	"github.com/jxsl13/spectackle/internal/item"
	"github.com/jxsl13/spectackle/internal/journal"
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
		"knowledge": false,
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

// TestCheckHealsEvolvedAndReportsRule (T-0086): a rule anchored to two
// nodes; only one node's code changes while the rule sentence stays put ->
// Evolved -> mechanically healed. The trailing rule line must appear
// exactly once (deduped) even though the rule is anchored twice, and a
// second check must show the heal stuck.
func TestCheckHealsEvolvedAndReportsRule(t *testing.T) {
	root := t.TempDir()
	src := "package demo\n\nfunc F() int {\n\treturn 1\n}\n\nfunc G() int {\n\treturn 1\n}\n"
	if err := os.WriteFile(filepath.Join(root, "demo.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	sess := connectRoot(t, root)

	out := callText(t, sess, "rule", map[string]any{
		"op": "add", "dir": "", "pattern": "U", "stem": "HEAL-TST",
		"system":   "the heal test workspace",
		"response": "always return the constant 1 from every guarded function",
		"applies":  []string{"go:demo.F", "go:demo.G"},
	})
	if !strings.Contains(out, "ok HEAL-TST-001") {
		t.Fatalf("rule add: %q", out)
	}

	// fresh anchors: nothing drifted yet
	out = callText(t, sess, "check", map[string]any{})
	if strings.Contains(out, "d ") {
		t.Fatalf("freshly anchored rule should not drift: %q", out)
	}

	// F's body changes (same line count, so its position is untouched) but
	// the rule sentence does not: code changed, rule sentence identical =>
	// Evolved, mechanically healable.
	src2 := "package demo\n\nfunc F() int {\n\treturn 2\n}\n\nfunc G() int {\n\treturn 1\n}\n"
	if err := os.WriteFile(filepath.Join(root, "demo.go"), []byte(src2), 0o644); err != nil {
		t.Fatal(err)
	}

	out = callText(t, sess, "check", map[string]any{})
	if !strings.Contains(out, "d healed HEAL-TST-001 go:demo.F") {
		t.Fatalf("expected a heal record for the drifted anchor: %q", out)
	}
	if strings.Contains(out, "d healed HEAL-TST-001 go:demo.G") {
		t.Fatalf("G never drifted, must not be healed: %q", out)
	}
	if n := strings.Count(out, "r HEAL-TST-001 "); n != 1 {
		t.Fatalf("expected exactly one deduped rule line, got %d: %q", n, out)
	}
	if !strings.Contains(out, "always return the constant 1 from every guarded function") {
		t.Fatalf("deduped rule line missing the sentence: %q", out)
	}
	if !strings.Contains(out, "ok healed=1 audit=0") {
		t.Fatalf("expected the heal/audit trailer: %q", out)
	}

	// a second check must show the heal stuck: no more drift for this rule
	out = callText(t, sess, "check", map[string]any{})
	if strings.Contains(out, "d healed") || strings.Contains(out, "HEAL-TST-001") {
		t.Fatalf("heal did not stick, second check still reports drift: %q", out)
	}
}

// TestCheckAuditsTightenedNeverHeals (T-0086): editing only the rule
// sentence (not the code, not `applies`) leaves the anchor's code hash
// matching but its rule hash stale -> Tightened -> audited, never healed,
// and it must keep showing up on every subsequent check.
func TestCheckAuditsTightenedNeverHeals(t *testing.T) {
	root := t.TempDir()
	src := "package demo\n\nfunc F() int {\n\treturn 1\n}\n"
	if err := os.WriteFile(filepath.Join(root, "demo.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	sess := connectRoot(t, root)

	out := callText(t, sess, "rule", map[string]any{
		"op": "add", "dir": "", "pattern": "U", "stem": "TGT-TST",
		"system":   "the tighten test workspace",
		"response": "always return the constant 1 from F",
		"applies":  []string{"go:demo.F"},
	})
	if !strings.Contains(out, "ok TGT-TST-001") {
		t.Fatalf("rule add: %q", out)
	}
	out = callText(t, sess, "check", map[string]any{})
	if strings.Contains(out, "d ") {
		t.Fatalf("freshly anchored rule should not drift: %q", out)
	}

	// edit only the sentence: no `applies` passed, so stampAnchors is
	// skipped and anchors.tsv keeps the OLD rule hash. Code untouched,
	// rule sentence changed => Tightened.
	out = callText(t, sess, "rule", map[string]any{
		"op": "edit", "id": "TGT-TST-001", "pattern": "U",
		"system":   "the tighten test workspace",
		"response": "always return the constant 2 from F",
	})
	if !strings.Contains(out, "ok TGT-TST-001") {
		t.Fatalf("rule edit: %q", out)
	}

	out = callText(t, sess, "check", map[string]any{})
	if !strings.Contains(out, "d audit TGT-TST-001 go:demo.F") {
		t.Fatalf("expected an audit record for the tightened anchor: %q", out)
	}
	if !strings.Contains(out, "tightened") {
		t.Fatalf("audit record must name the tightened class: %q", out)
	}
	if strings.Contains(out, "d healed") {
		t.Fatalf("tightened drift must never be healed: %q", out)
	}

	// a following check still reports the same audit line — nothing healed it away
	out2 := callText(t, sess, "check", map[string]any{})
	if !strings.Contains(out2, "d audit TGT-TST-001 go:demo.F") {
		t.Fatalf("second check must still report the same audit: %q", out2)
	}
}

// TestMoveGateBlocksDoneOnTightenedAnchor (T-0091): arms the audit gate at
// the move tool's lifecycle.Move call site. A rule anchored to an indexed
// node, an active item targeting that node, then the rule sentence drifts
// (op=edit without applies — the Tightened class per
// TestCheckAuditsTightenedNeverHeals above) must refuse `move to=done` with
// the dense "! GATE E" record, leave the item active, and let the same move
// through once the anchor is reconciled (rule op=edit re-passing applies,
// which re-stamps the rule hash).
func TestMoveGateBlocksDoneOnTightenedAnchor(t *testing.T) {
	root := t.TempDir()
	src := "package demo\n\nfunc F() int {\n\treturn 1\n}\n"
	if err := os.WriteFile(filepath.Join(root, "demo.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	sess := connectRoot(t, root)

	out := callText(t, sess, "rule", map[string]any{
		"op": "add", "dir": "", "pattern": "U", "stem": "GATE-TST",
		"system":   "the move-gate test workspace",
		"response": "always return the constant 1 from F",
		"applies":  []string{"go:demo.F"},
	})
	if !strings.Contains(out, "ok GATE-TST-001") {
		t.Fatalf("rule add: %q", out)
	}

	// draft + activate an item whose targets include the anchored node
	out = callText(t, sess, "draft", map[string]any{
		"kind": "task", "title": "touch F", "targets": []string{"go:demo.F"},
	})
	if !strings.Contains(out, "i T-0001 task draft") {
		t.Fatalf("draft: %q", out)
	}
	out = callText(t, sess, "move", map[string]any{"id": "T-0001", "to": "active"})
	if !strings.Contains(out, "active") {
		t.Fatalf("move to active: %q", out)
	}

	// tighten: edit only the rule sentence, no applies -> anchors.tsv keeps
	// the old rule hash, code untouched -> drift.Tightened.
	out = callText(t, sess, "rule", map[string]any{
		"op": "edit", "id": "GATE-TST-001", "pattern": "U",
		"system":   "the move-gate test workspace",
		"response": "always return the constant 2 from F",
	})
	if !strings.Contains(out, "ok GATE-TST-001") {
		t.Fatalf("rule edit: %q", out)
	}

	// move to=done must refuse with the dense audit-gate record naming the
	// rule and the node, not silently succeed (the gate was dormant before
	// T-0091 wired WithAuditGate at this call site).
	out = callText(t, sess, "move", map[string]any{"id": "T-0001", "to": "done"})
	want := "! GATE E T-0001 audit GATE-TST-001 go:demo.F tightened"
	if !strings.Contains(out, want) {
		t.Fatalf("expected the audit-gate refusal %q, got: %q", want, out)
	}

	// the item must still be active — the refusal did not let the move land
	out = callText(t, sess, "get", map[string]any{"id": "T-0001"})
	if !strings.Contains(out, "i T-0001 task active") {
		t.Fatalf("item must still be active after the gate refused done: %q", out)
	}

	// reconcile: re-pass applies on the same (already-tightened) sentence,
	// which re-stamps the anchor's rule hash and clears the drift.
	out = callText(t, sess, "rule", map[string]any{
		"op": "edit", "id": "GATE-TST-001", "applies": []string{"go:demo.F"},
	})
	if !strings.Contains(out, "ok GATE-TST-001") {
		t.Fatalf("rule edit (reconcile): %q", out)
	}

	// the same move now succeeds
	out = callText(t, sess, "move", map[string]any{"id": "T-0001", "to": "done"})
	if !strings.Contains(out, "i T-0001 task done") {
		t.Fatalf("move to=done must succeed once the anchor is reconciled: %q", out)
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

// TestCursorsResumeAcrossPages (SPX-ARC-002/006): a tiny budget yields a
// trailing cur record; feeding it back resumes at the next record, and the
// concatenated pages equal the unbudgeted output exactly. A malformed cur
// degrades to page 0.
func TestCursorsResumeAcrossPages(t *testing.T) {
	root := t.TempDir()
	src := "package demo\n\nfunc A() { B() }\n\nfunc B() { C() }\n\nfunc C() { D() }\n\nfunc D() {}\n"
	if err := os.WriteFile(filepath.Join(root, "demo.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	sess := connectRoot(t, root)

	page := func(tool string, args map[string]any) (body, cur string) {
		t.Helper()
		out := callText(t, sess, tool, args)
		for _, l := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
			if strings.HasPrefix(l, "cur ") {
				cur = strings.TrimPrefix(l, "cur ")
				continue
			}
			body += l + "\n"
		}
		return body, cur
	}

	for _, tc := range []struct {
		tool string
		args map[string]any
	}{
		{"find", map[string]any{"q": "demo", "scope": "code", "k": 8}},
		{"get", map[string]any{"id": "go:demo.A", "depth": 3}},
	} {
		full, cur := page(tc.tool, tc.args)
		if cur != "" {
			t.Fatalf("%s: default budget should not truncate this tiny repo: %q", tc.tool, cur)
		}
		var paged string
		cur = ""
		for i := 0; ; i++ {
			if i > 20 {
				t.Fatalf("%s: paging did not terminate", tc.tool)
			}
			args := map[string]any{"budget": 30, "cur": cur}
			for k, v := range tc.args {
				args[k] = v
			}
			body, next := page(tc.tool, args)
			paged += body
			if next == "" {
				break
			}
			if next == cur {
				t.Fatalf("%s: cursor did not advance: %q", tc.tool, next)
			}
			cur = next
		}
		if paged != full {
			t.Fatalf("%s: concatenated pages diverge from unbudgeted output:\n-- paged --\n%s-- full --\n%s", tc.tool, paged, full)
		}
	}

	// malformed cursor: page 0, no error
	body, _ := page("find", map[string]any{"q": "demo", "scope": "code", "budget": 30, "cur": "not-a-cursor"})
	if body == "" || strings.Contains(body, "! ") {
		t.Fatalf("malformed cur must degrade to page 0: %q", body)
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

// TestCheckOnOwnRepo: the repository itself must come back clean. The
// direction-aware rule-hash axis (T-0086) surfaces two PRE-EXISTING,
// known desyncs between examples/metalcompute/.spectackle/spec.md and
// .spectackle/anchors.tsv (MTC-API-004/006 — a stray hand-edit from the
// M6 dogfood commit, long before Classify checked the rule hash at all).
// They are never auto-healed by design (Tightened = rule text changed);
// this test tolerates exactly those two known IDs and fails on anything
// else — a lint error, an orphan, a gone/diverged anchor, or an
// unexpected new tightened one.
func TestCheckOnOwnRepo(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	sess := connectRoot(t, root)
	knownTightened := map[string]bool{"MTC-API-004": true, "MTC-API-006": true}
	// Assertions are line-prefix based, not substring-of-whole-output: the
	// deduped `r` lines this feature adds echo full rule sentences verbatim,
	// and MCP-004's own sentence quotes the `g orphan <rule> <node>` grammar
	// — a naive strings.Contains(out, "g orphan") would false-positive on
	// that quoted text instead of an actual orphan gap record.
	check := func() []string { return strings.Split(callText(t, sess, "check", map[string]any{}), "\n") }
	assertClean := func(lines []string) {
		t.Helper()
		for _, line := range lines {
			switch {
			case strings.HasPrefix(line, "! "):
				// "! <code> <sev> <ref> <msg>" — only Error severity fails
				// the own-repo check, matching the original test's intent;
				// pre-existing W001/W002 warnings are tolerated.
				if f := strings.Fields(line); len(f) >= 3 && f[2] == "E" {
					t.Fatalf("unexpected lint error on own repo: %q", line)
				}
			case strings.HasPrefix(line, "g orphan "):
				t.Fatalf("unexpected orphan on own repo: %q", line)
			case strings.HasPrefix(line, "d gone ") || strings.HasPrefix(line, "d diverged ") || strings.HasPrefix(line, "d stale "):
				t.Fatalf("unexpected drift on own repo: %q", line)
			case strings.HasPrefix(line, "d audit "):
				f := strings.Fields(line)
				if len(f) < 3 || !knownTightened[f[2]] {
					t.Fatalf("unexpected drift audit on own repo: %q", line)
				}
			}
		}
	}
	out1 := check()
	assertClean(out1)

	// The MCP-004 anchor (this very check() function) heals on the first
	// call above since its own code just changed under T-0086. A second
	// run must not re-heal anything — the heal must have stuck.
	out2 := check()
	assertClean(out2)
	for _, line := range out2 {
		if strings.HasPrefix(line, "d healed ") {
			t.Fatalf("second check on own repo re-healed something that should already be settled: %q", line)
		}
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

// TestInstructionsTeachTokenEconomy asserts that the server's instructions
// const teaches token economy: prefer server tools over shell exploration.
func TestInstructionsTeachTokenEconomy(t *testing.T) {
	if !strings.Contains(instructions, "TOKEN ECONOMY") {
		t.Errorf("instructions missing TOKEN ECONOMY paragraph")
	}
	if !strings.Contains(instructions, "scope=code") {
		t.Errorf("instructions missing scope=code guidance")
	}
	if !strings.Contains(instructions, "NEVER grep or sed .spectackle/") {
		t.Errorf("instructions missing .spectackle/ protection")
	}
}

// TestInstructionsTeachBrownfieldImportAndRecords (T-0098, T-0101) asserts
// that the server's instructions const teaches brownfield-repo onboarding
// and the records token-economy guardrail, including the American English
// language mandate (MCP-007).
func TestInstructionsTeachBrownfieldImportAndRecords(t *testing.T) {
	if !strings.Contains(instructions, "BROWNFIELD IMPORT") {
		t.Errorf("instructions missing BROWNFIELD IMPORT paragraph")
	}
	if !strings.Contains(instructions, "Survey in parallel") {
		t.Errorf("instructions missing 'Survey in parallel' step")
	}
	if !strings.Contains(instructions, "RECORDS") {
		t.Errorf("instructions missing RECORDS paragraph")
	}
	if !strings.Contains(instructions, "Never paste verbatim") {
		t.Errorf("instructions missing 'Never paste verbatim' guardrail")
	}
	if !strings.Contains(instructions, "American English") {
		t.Errorf("instructions missing American English language mandate")
	}
}

// TestInstructionsTeachDefectReporting (T-0104, MCP-008) asserts that the
// composed manifest tells the agent to report defects it finds in the
// server itself as issues, carrying an analysis, at the module's derived
// repository URL — and never as a fix PR. The URL assertion is derived via
// moduleRepoURL() (not hardcoded here) so a regression to a hardcoded or
// missing URL in the manifest fails this test.
func TestInstructionsTeachDefectReporting(t *testing.T) {
	m := manifest()
	if !strings.Contains(m, "DEFECT REPORTS") {
		t.Errorf("manifest missing DEFECT REPORTS paragraph")
	}
	if !strings.Contains(m, "Do not send a fix PR") {
		t.Errorf("manifest missing the no-fix-PR policy")
	}
	if url := moduleRepoURL(); !strings.Contains(m, url) {
		t.Errorf("manifest missing derived repository URL %q", url)
	}
}

// TestModuleRepoURLFallback (T-0104) is a table-driven unit test for the
// module-URL derivation helper: an empty build-info path (test binaries,
// some build modes report one) must still yield a usable https:// URL via
// the compile-time modulePath fallback, and a non-empty path is prefixed
// as-is with no suffix appended.
func TestModuleRepoURLFallback(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{"empty path falls back to modulePath", "", "https://" + modulePath},
		{"non-empty path is prefixed as-is", "github.com/example/other", "https://github.com/example/other"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := moduleRepoURLFrom(tt.path); got != tt.want {
				t.Errorf("moduleRepoURLFrom(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
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

// TestGetItemRendersADRFields (T-0097): internal/item already stores the
// four classic ADR fields (Context/Decision/Consequences/Status) correctly,
// but getItem never printed them — an agent asking `get ADR-...` only saw
// the header, targets/rules and body, never the structured record the ADR
// feature exists to provide. Drive decide op=ask (context=) then op=answer
// (choose= + consequences=) over the wire — exactly the persistence path
// decide_test.go's TestDecideAskStoresContextAndProposedStatus /
// TestDecideAnswerRecordsDecisionStatusAndConsequences exercise directly
// against the item package — and assert `get` now surfaces all four fields,
// in the classic ADR order, dense one-field-per-line like the rest of the
// item header.
func TestGetItemRendersADRFields(t *testing.T) {
	root := t.TempDir()
	sess := connectRoot(t, root)

	callText(t, sess, "decide", map[string]any{
		"op": "ask", "question": "which backend?", "options": []string{"grpc", "rest"},
		"context": "Latency-sensitive service; current REST gateway is the bottleneck.",
	})
	callText(t, sess, "decide", map[string]any{
		"op": "answer", "id": "ADR-0001", "choose": "grpc",
		"consequences": "Clients must add a gRPC dependency; REST gateway is deprecated over two releases.",
	})

	out := callText(t, sess, "get", map[string]any{"id": "ADR-0001"})
	for _, want := range []string{
		"context: Latency-sensitive service; current REST gateway is the bottleneck.\n",
		"decision: grpc\n",
		"consequences: Clients must add a gRPC dependency; REST gateway is deprecated over two releases.\n",
		"status: accepted\n",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("get ADR-0001 missing %q in output: %q", want, out)
		}
	}
	// classic ADR order: context, decision, consequences, status.
	if strings.Index(out, "context:") > strings.Index(out, "decision:") ||
		strings.Index(out, "decision:") > strings.Index(out, "consequences:") ||
		strings.Index(out, "consequences:") > strings.Index(out, "status:") {
		t.Fatalf("ADR fields out of order: %q", out)
	}
}

// TestGetItemNonADRUnchanged (T-0097): a plain proposal's four ADR fields
// are always empty (only decide-minted `adr` items ever set them), so
// getItem's new field-emission must stay a no-op for it — output diet
// (R-0001), no stray empty context:/decision:/consequences:/status: lines,
// byte-identical to the pre-fix rendering.
func TestGetItemNonADRUnchanged(t *testing.T) {
	root := t.TempDir()
	sess := connectRoot(t, root)

	callText(t, sess, "draft", map[string]any{
		"kind": "proposal", "title": "cache kernels in VRAM",
		"body": "Keep compiled kernels resident.",
	})
	out := callText(t, sess, "get", map[string]any{"id": "P-0001"})
	if !strings.Contains(out, "i P-0001 proposal draft") {
		t.Fatalf("unexpected header: %q", out)
	}
	if !strings.Contains(out, "Keep compiled kernels resident.\n") {
		t.Fatalf("body missing: %q", out)
	}
	for _, field := range []string{"context:", "decision:", "consequences:", "status:"} {
		if strings.Contains(out, field) {
			t.Fatalf("non-ADR get output must not render empty ADR field %q: %q", field, out)
		}
	}
}

// TestDraftRefsPersistAndRender (MCP-012, T-0118): a refs list that only
// names known items persists on the item, and a follow-up get renders it in
// the same dense style as parent/targets/rules.
func TestDraftRefsPersistAndRender(t *testing.T) {
	root := t.TempDir()
	sess := connectRoot(t, root)

	callText(t, sess, "draft", map[string]any{
		"kind": "proposal", "title": "the cited proposal",
	})
	out := callText(t, sess, "draft", map[string]any{
		"kind": "task", "title": "cites the proposal", "refs": []string{"P-0001"},
	})
	if !strings.Contains(out, "i T-0001 task draft") {
		t.Fatalf("draft: %q", out)
	}

	out = callText(t, sess, "get", map[string]any{"id": "T-0001"})
	if !strings.Contains(out, "refs P-0001\n") {
		t.Fatalf("get T-0001 missing refs line: %q", out)
	}
}

// TestDraftUnknownRefRefused (MCP-012): a refs list naming an item the
// server cannot see is refused whole, with a dense ! ARG record naming it,
// and — the point of the contract — persists nothing at all: no item, no
// journal event, exactly as if the call had never been made.
func TestDraftUnknownRefRefused(t *testing.T) {
	root := t.TempDir()
	sess := connectRoot(t, root)
	ws := workspace.Root{Dir: root}

	before, err := item.LoadAll(ws)
	if err != nil {
		t.Fatal(err)
	}
	beforeEvents, err := journal.Read(ws, "")
	if err != nil {
		t.Fatal(err)
	}

	out := callText(t, sess, "draft", map[string]any{
		"kind": "task", "title": "cites nothing real", "refs": []string{"P-9999"},
	})
	if !strings.Contains(out, "! ARG") || !strings.Contains(out, "P-9999") {
		t.Fatalf("expected ARG refusal naming P-9999: %q", out)
	}

	after, err := item.LoadAll(ws)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("unknown ref draft must persist no item: before=%d after=%d", len(before), len(after))
	}
	if _, ok, _ := item.Get(ws, "T-0001"); ok {
		t.Fatalf("unknown ref draft must not persist T-0001")
	}
	afterEvents, err := journal.Read(ws, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(afterEvents) != len(beforeEvents) {
		t.Fatalf("unknown ref draft must append no journal event: before=%d after=%d", len(beforeEvents), len(afterEvents))
	}
}

// TestDraftSelfAndMalformedRefRefused (MCP-012): a ref equal to the item's
// own about-to-be-minted ID, and a ref that isn't even shaped like an item
// ID, are both caught by item.UnknownRefs and refused the same way — and
// neither leaves anything persisted.
func TestDraftSelfAndMalformedRefRefused(t *testing.T) {
	root := t.TempDir()
	sess := connectRoot(t, root)
	ws := workspace.Root{Dir: root}

	// the first task minted in a fresh workspace is always T-0001 — cite it
	// before it exists.
	out := callText(t, sess, "draft", map[string]any{
		"kind": "task", "title": "cites itself", "refs": []string{"T-0001"},
	})
	if !strings.Contains(out, "! ARG") || !strings.Contains(out, "T-0001") {
		t.Fatalf("expected ARG refusal naming the self-reference T-0001: %q", out)
	}
	if _, ok, _ := item.Get(ws, "T-0001"); ok {
		t.Fatalf("self-referencing draft must not persist T-0001")
	}

	out = callText(t, sess, "draft", map[string]any{
		"kind": "task", "title": "cites nonsense", "refs": []string{"not-an-id"},
	})
	if !strings.Contains(out, "! ARG") || !strings.Contains(out, "not-an-id") {
		t.Fatalf("expected ARG refusal naming the malformed ref: %q", out)
	}

	items, err := item.LoadAll(ws)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("both refused drafts must persist nothing, got %d items", len(items))
	}
}

// TestDraftNoRefsUnchanged is the regression guard: a draft call with no
// refs field behaves exactly as it did before this feature landed.
func TestDraftNoRefsUnchanged(t *testing.T) {
	root := t.TempDir()
	sess := connectRoot(t, root)

	out := callText(t, sess, "draft", map[string]any{
		"kind": "proposal", "title": "plain citation-free proposal",
		"body": "Nothing cited here.",
	})
	if !strings.Contains(out, "i P-0001 proposal draft") {
		t.Fatalf("draft: %q", out)
	}
	out = callText(t, sess, "get", map[string]any{"id": "P-0001"})
	if strings.Contains(out, "\nrefs ") {
		t.Fatalf("no-refs draft must not render a refs line: %q", out)
	}
}
