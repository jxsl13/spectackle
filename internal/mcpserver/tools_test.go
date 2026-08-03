package mcpserver

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/jxsl13/spectackle/internal/drift"
	"github.com/jxsl13/spectackle/internal/item"
	"github.com/jxsl13/spectackle/internal/workspace"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// draftID calls the draft tool and returns the ID the server minted for the
// new item, read off the `i <id> ...` record draft answers with.
//
// Every test that used to hardcode "P-0001" goes through here instead: since
// ADR-0013 (T-0135) item IDs are UUIDv7-derived, so a minted ID is different
// on every run and cannot appear in a test literal. Legacy IDs are still
// perfectly valid to write by hand — tests that seed a pre-migration
// workspace do exactly that.
func draftID(t *testing.T, sess *mcp.ClientSession, args map[string]any) string {
	t.Helper()
	return idOfRecord(t, callText(t, sess, "draft", args), "i")
}

// idOfRecord picks the ID out of the first record line of out carrying the
// given tag. Scanning lines rather than reading the head of the output is
// deliberate: several tools prepend an `h ...` health hint, and the context
// pack follows the item record, so the record is not reliably the whole text.
//
// It matches idPrefixRe, not item.IDRe: since T-0136 every emitted ID is in
// the DISPLAY form (the shortest unambiguous prefix), which item.IDRe rejects
// by design — it is the acceptance test for a complete, storable ID. What
// this returns is therefore exactly what an agent reading the same output
// would copy, and every tool must accept it back verbatim. That is the
// ADR-0013 round-trip property, and letting the whole suite depend on it is
// worth more than any single test asserting it.
func idOfRecord(t *testing.T, out, tag string) string {
	t.Helper()
	for _, l := range strings.Split(out, "\n") {
		f := strings.Fields(l)
		if len(f) < 2 || f[0] != tag {
			continue
		}
		for _, tok := range f[1:] {
			if idPrefixRe.MatchString(tok) && !reRuleID.MatchString(tok) {
				return tok
			}
		}
	}
	t.Fatalf("no %q record with an item ID in %q", tag, out)
	return ""
}

// fullID expands a displayed ID back to the stored one.
//
// Tool arguments never need this — accepting the display form is the whole
// point of T-0136. It exists for assertions that bypass the tool boundary and
// read the store directly (item.Get, journal events, work.md bytes), where the
// key is the canonical ID and a prefix simply is not it. A test reaching for
// this is asserting about persistence; a test asserting about the surface
// should keep using the displayed form.
func fullID(t *testing.T, s *Server, id string) string {
	t.Helper()
	sc, err := s.idScope()
	if err != nil {
		t.Fatalf("idScope: %v", err)
	}
	full, bad := sc.expand(id)
	if bad != nil {
		t.Fatalf("expand %s: %s", id, resultText(bad))
	}
	return full
}

// draftFullID drafts and immediately resolves the answer to the stored ID.
//
// Long tests that mint many records need this. A displayed prefix is unique
// only against the record set at the instant it was emitted, and a UUIDv7 puts
// the millisecond clock first, so every record minted in the same ~17-minute
// window shares the six-character floor — the prefix a test captured up front
// turns ambiguous a few drafts later. Resolving at capture time, while the
// display form is still unique, pins the identity for the rest of the test.
// Full IDs are accepted by every tool, so this changes nothing about what is
// being exercised except that it stops testing the clock.
func draftFullID(t *testing.T, s *Server, sess *mcp.ClientSession, args map[string]any) string {
	t.Helper()
	return fullID(t, s, draftID(t, sess, args))
}

// storedID resolves a displayed ID against a workspace read straight off disk.
//
// It exists for the tests that hold only a client session (two-agent and
// worktree end-to-end setups) and so cannot reach a *Server to call fullID.
// Live items are enough for those: they resolve records they just drafted.
func storedID(t *testing.T, root, display string) string {
	t.Helper()
	items, err := item.LoadAll(workspace.Root{Dir: root})
	if err != nil {
		t.Fatalf("LoadAll(%s): %v", root, err)
	}
	var hits []string
	for _, it := range items {
		if strings.HasPrefix(it.ID, display) {
			hits = append(hits, it.ID)
		}
	}
	if len(hits) != 1 {
		t.Fatalf("storedID(%q): %d matches %v", display, len(hits), hits)
	}
	return hits[0]
}

// storedIDAfterDraft drafts through sess and returns the stored ID, for the
// session-only tests. storedID's counterpart to draftFullID.
func storedIDAfterDraft(t *testing.T, root string, sess *mcp.ClientSession, args map[string]any) string {
	t.Helper()
	return storedID(t, root, draftID(t, sess, args))
}

// wantItemRecord asserts out carries an `i` record naming the stored ID want,
// followed by rest.
//
// It compares the record's ID field by prefix instead of matching the whole
// line as a literal, because the displayed ID's LENGTH is not stable: it grows
// as sibling records accumulate (see draftFullID). Prefix-matching the field
// is precise anyway — a displayed ID is only ever a prefix of the record it
// names.
func wantItemRecord(t *testing.T, out, want, rest string) {
	t.Helper()
	for _, l := range strings.Split(out, "\n") {
		f := strings.Fields(l)
		if len(f) < 2 || f[0] != "i" || !strings.HasPrefix(want, f[1]) {
			continue
		}
		if strings.Contains(l, rest) {
			return
		}
	}
	t.Fatalf("no `i` record for %s with %q in %q", want, rest, out)
}

// shortID renders a stored ID the way the tools emit it, for assertions that
// hold a canonical ID (out of a Go struct or a seeded fixture) and have to
// match it against tool output. The inverse direction of fullID.
func shortID(t *testing.T, s *Server, id string) string {
	t.Helper()
	sc, err := s.idScope()
	if err != nil {
		t.Fatalf("idScope: %v", err)
	}
	return sc.short(id)
}

// askID calls decide op=ask and returns the ID of the adr item it minted,
// read off the "need decision <id> ..." record. Same reason as draftID: the
// ID is minted, so it cannot be a literal.
func askID(t *testing.T, sess *mcp.ClientSession, args map[string]any) string {
	t.Helper()
	out := callText(t, sess, "decide", args)
	for _, l := range strings.Split(out, "\n") {
		f := strings.Fields(l)
		if len(f) >= 3 && f[0] == "need" && f[1] == "decision" && idPrefixRe.MatchString(f[2]) {
			return f[2]
		}
	}
	t.Fatalf("decide ask did not answer with a need-decision record: %q", out)
	return ""
}

// connectRoot spins up the server over an in-memory transport against a
// workspace root and returns a live client session.
func connectRoot(t *testing.T, root string) *mcp.ClientSession {
	t.Helper()
	_, sess := connectRootWithServer(t, root)
	return sess
}

// connectRootWithServer is connectRoot for tests that also need the *Server —
// to resolve a displayed ID against the store (fullID/shortID) or to read
// internal state directly.
func connectRootWithServer(t *testing.T, root string) (*Server, *mcp.ClientSession) {
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
	return s, sess
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
		"knowledge": false, "validate": false, "bench": false,
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
	// The identity binding is discoverable BEFORE the first refusal
	// (B-01KYK5FNM4F3F): the work tool's description names
	// SPECTACKLE_AGENT, not only the hint/refusal renders.
	for _, tool := range res.Tools {
		if tool.Name == "work" && !strings.Contains(tool.Description, "SPECTACKLE_AGENT") {
			t.Errorf("work tool description must name the SPECTACKLE_AGENT binding:\n%s", tool.Description)
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
	f := strings.Fields(out)
	if len(f) < 2 || f[0] != "i" || !idPrefixRe.MatchString(f[1]) {
		t.Fatalf("draft: %q", out)
	}
	prop := f[1]
	propFull := storedID(t, root, prop)
	if !strings.Contains(out, "i "+prop+" proposal draft") {
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
	task := draftID(t, sess, map[string]any{
		"kind": "task", "title": "adjust kernel indexing", "parent": prop,
	})

	for _, mv := range [][2]string{
		{prop, "submitted"}, {prop, "approved"}, {prop, "active"},
		{task, "active"}, {task, "done"},
	} {
		out = callText(t, sess, "move", map[string]any{"id": mv[0], "to": mv[1]})
		if !strings.Contains(out, mv[1]) {
			t.Fatalf("move %s->%s: %q", mv[0], mv[1], out)
		}
	}

	// archive is a legal forward skip straight from active (implies done);
	// the only guard left is open children, and the task is already done
	out = callText(t, sess, "move", map[string]any{"id": prop, "to": "archived"})
	if !strings.Contains(out, "archived") {
		t.Fatalf("archive from active (forward skip, implies done): %q", out)
	}

	// work.md is empty again; intent carries the merged delta
	spec, err := os.ReadFile(filepath.Join(root, ".spectackle", "spec.md"))
	if err != nil {
		t.Fatal(err)
	}
	// spec.md stores the canonical ID, not the displayed prefix: the intent
	// line is persisted state, and persisted state is never abbreviated.
	if !strings.Contains(string(spec), "## intent") || !strings.Contains(string(spec), propFull+" strided saxpy access") {
		t.Fatalf("archive did not merge into intent:\n%s", spec)
	}
	// gone from work.md, but never from the referenceable universe: get
	// resolves it as a journal tombstone (LCY-001) instead of nf
	out = callText(t, sess, "get", map[string]any{"id": prop})
	if !strings.Contains(out, "archived") || !strings.Contains(out, "journal tombstone") {
		t.Fatalf("archived item should resolve via tombstone, not nf: %q", out)
	}
	// history still knows it
	out = callText(t, sess, "find", map[string]any{"q": "strided", "scope": "history"})
	if !strings.Contains(out, prop) {
		t.Fatalf("history lost the archived item: %q", out)
	}
}

// TestRejectionCorpusAndRevocation: rejection requires a note, becomes
// searchable, and can be revoked back into a previous state.
func TestRejectionCorpusAndRevocation(t *testing.T) {
	root := t.TempDir()
	sess := connectRoot(t, root)

	prop := draftID(t, sess, map[string]any{
		"kind": "proposal", "title": "cache kernels in VRAM",
		"body": "Keep compiled kernels resident.",
	})
	// note is mandatory
	out := callText(t, sess, "move", map[string]any{"id": prop, "to": "rejected"})
	if !strings.Contains(out, "! ARG E") || !strings.Contains(out, "note") {
		t.Fatalf("rejection without note must fail: %q", out)
	}
	callText(t, sess, "move", map[string]any{
		"id": prop, "to": "rejected",
		"note": "VRAM residency breaks multi-tenant GPU scheduling",
	})

	// searchable corpus
	out = callText(t, sess, "find", map[string]any{"q": "multi-tenant scheduling", "scope": "rejection"})
	if !strings.Contains(out, prop) {
		t.Fatalf("rejection not searchable: %q", out)
	}

	// revocable: back to draft, item restored with body
	out = callText(t, sess, "move", map[string]any{"id": prop, "to": "draft"})
	if !strings.Contains(out, "i "+prop+" proposal draft") {
		t.Fatalf("revocation failed: %q", out)
	}
	out = callText(t, sess, "get", map[string]any{"id": prop})
	if !strings.Contains(out, "Keep compiled kernels resident.") {
		t.Fatalf("revoked item lost its body: %q", out)
	}
	// the reject event stays in history even after revocation
	out = callText(t, sess, "find", map[string]any{"q": "multi-tenant", "scope": "rejection"})
	if !strings.Contains(out, prop) {
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
	task := draftID(t, sess, map[string]any{
		"kind": "task", "title": "touch F", "targets": []string{"go:demo.F"},
	})
	out = callText(t, sess, "move", map[string]any{"id": task, "to": "active"})
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
	out = callText(t, sess, "move", map[string]any{"id": task, "to": "done"})
	want := "! GATE E " + task + " audit GATE-TST-001 go:demo.F tightened"
	if !strings.Contains(out, want) {
		t.Fatalf("expected the audit-gate refusal %q, got: %q", want, out)
	}

	// the item must still be active — the refusal did not let the move land
	out = callText(t, sess, "get", map[string]any{"id": task})
	if !strings.Contains(out, "i "+task+" task active") {
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
	out = callText(t, sess, "move", map[string]any{"id": task, "to": "done"})
	if !strings.Contains(out, "i "+task+" task done") {
		t.Fatalf("move to=done must succeed once the anchor is reconciled: %q", out)
	}
}

// TestCompactKeepsRejections: journal folds drop noise but never reject lines.
func TestCompactKeepsRejections(t *testing.T) {
	root := t.TempDir()
	sess := connectRoot(t, root)

	bug := draftID(t, sess, map[string]any{"kind": "bug", "title": "nan in reduction"})
	callText(t, sess, "move", map[string]any{"id": bug, "to": "rejected", "note": "not reproducible on sm90"})
	callText(t, sess, "draft", map[string]any{"kind": "task", "title": "noise a"})
	callText(t, sess, "draft", map[string]any{"kind": "task", "title": "noise b"})

	// force the fold threshold down via config
	cfg := filepath.Join(root, ".spectackle", "config.yaml")
	if err := os.WriteFile(cfg, []byte("schema: v1\ncompact:\n  journal_max: 2\n  done_max: 1\n"), 0o644); err != nil {
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
	if !strings.Contains(out, bug) {
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
			// transient advisory lines are appended per CALL, not per record
			// stream (the MCP-010 stale-binary hint fires once per crossing,
			// sw sibling learnings once per event), so they legitimately
			// appear on one page and not the next — pagination equality is a
			// record-stream contract and excludes them.
			if strings.HasPrefix(l, "h ") || strings.HasPrefix(l, "sw ") {
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
	// the repo's own module builds clean under the toolchain running this
	// test, so the typed-call pass is healthy — issue 28's output-diet
	// contract says a healthy pass adds NOTHING, asserted as an absence.
	for _, line := range out1 {
		if strings.HasPrefix(line, "! TYPED") {
			t.Fatalf("check on own (healthy) repo must not emit a typed-pass finding: %q", line)
		}
	}

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

// TestCheckReportsTypedPassDegradation is issue 28's acceptance test for the
// `check` half: mirrors TestStateReportsTypedPassDegradation (state_test.go)
// — same forced failure (a real go.mod plus a package importing a path that
// does not exist), same finding, same gate — so `check` never disagrees with
// `state` about when the typed-call pass is worth reporting.
func TestCheckReportsTypedPassDegradation(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/typedfail\n\ngo 1.21\n")
	writeFile(t, root, "pkg1/a.go", "package pkg1\n\nimport \"example.com/typedfail/nope1\"\n\nfunc A() { nope1.Foo() }\n")
	writeFile(t, root, "pkg2/b.go", "package pkg2\n\nimport \"example.com/typedfail/nope2\"\n\nfunc B() { nope2.Bar() }\n")

	sess := connectRoot(t, root)
	out := callText(t, sess, "check", map[string]any{})

	if !strings.Contains(out, "! TYPED W") {
		t.Fatalf("check did not surface the forced typed-pass failure as a finding: %q", out)
	}
	if !strings.Contains(out, "packages=2") {
		t.Fatalf("check's typed-pass finding must name the affected package count (pkg1 and pkg2): %q", out)
	}
	if !strings.Contains(out, "nope1") && !strings.Contains(out, "nope2") {
		t.Fatalf("check's typed-pass finding must name the cause (an example failing import): %q", out)
	}
}

// TestCheckHealthyRepoOmitsTypedPassFinding (issue 28): a workspace with a
// real, resolvable go.mod — the typed-call pass genuinely runs and succeeds
// — must not add a `! TYPED` line. Complements TestCheckOnOwnRepo's absence
// assertion with a minimal fixture instead of the whole live repo, and
// complements TestCheckReportsTypedPassDegradation by pinning the other side
// of the same gate (healthy vs. forced failure) in one file.
func TestCheckHealthyRepoOmitsTypedPassFinding(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/typedok\n\ngo 1.21\n")
	writeFile(t, root, "pkg1/a.go", "package pkg1\n\nfunc A() {}\n")

	sess := connectRoot(t, root)
	out := callText(t, sess, "check", map[string]any{})
	if strings.Contains(out, "! TYPED") {
		t.Fatalf("check on a healthy Go module must not emit a typed-pass finding: %q", out)
	}

	out = callText(t, sess, "state", map[string]any{})
	if strings.Contains(out, "! TYPED") {
		t.Fatalf("state on a healthy Go module must not emit a typed-pass finding: %q", out)
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
// that the server still TEACHES brownfield-repo onboarding and the records
// token-economy guardrail, including the American English language mandate
// (MCP-007). The obligations are unchanged; their ADDRESS moved with the
// manifest diet (T-01KYE3): brownfield lives in the guide prompt now, paid
// only when onboarding actually happens, while the records rules stay in
// the always-read core — and the manifest must keep pointing at the moved
// playbook or nothing would ever find it.
func TestInstructionsTeachBrownfieldImportAndRecords(t *testing.T) {
	brownfield := guideTopics["brownfield"]
	if !strings.Contains(brownfield, "BROWNFIELD IMPORT") {
		t.Errorf("guide topic brownfield missing its paragraph")
	}
	if !strings.Contains(brownfield, "Survey in parallel") {
		t.Errorf("guide topic brownfield missing 'Survey in parallel' step")
	}
	if !strings.Contains(instructions, "brownfield") {
		t.Errorf("manifest lost the pointer to the brownfield playbook")
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

	adr := askID(t, sess, map[string]any{
		"op": "ask", "question": "which backend?", "options": []string{"grpc", "rest"},
		"context": "Latency-sensitive service; current REST gateway is the bottleneck.",
	})
	callText(t, sess, "decide", map[string]any{
		"op": "answer", "id": adr, "choose": "grpc",
		"consequences": "Clients must add a gRPC dependency; REST gateway is deprecated over two releases.",
	})

	out := callText(t, sess, "get", map[string]any{"id": adr})
	for _, want := range []string{
		"context: Latency-sensitive service; current REST gateway is the bottleneck.\n",
		"decision: grpc\n",
		"consequences: Clients must add a gRPC dependency; REST gateway is deprecated over two releases.\n",
		"status: accepted\n",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("get %s missing %q in output: %q", adr, want, out)
		}
	}
	// classic ADR order: context, decision, consequences, status.
	if strings.Index(out, "context:") > strings.Index(out, "decision:") ||
		strings.Index(out, "decision:") > strings.Index(out, "consequences:") ||
		strings.Index(out, "consequences:") > strings.Index(out, "status:") {
		t.Fatalf("ADR fields out of order: %q", out)
	}
}

// TestDraftRefsLiveItem (T-0117): drafting with refs to a live item persists
// them in the same write and get renders the refs line after rules.
func TestDraftRefsLiveItem(t *testing.T) {
	root := t.TempDir()
	sess := connectRoot(t, root)

	res := draftID(t, sess, map[string]any{"kind": "research", "title": "prefetch survey"})
	prop := draftID(t, sess, map[string]any{
		"kind": "proposal", "title": "cache kernels in VRAM",
		"refs": []string{res},
	})
	out := callText(t, sess, "get", map[string]any{"id": prop})
	if !strings.Contains(out, "refs "+res+"\n") {
		t.Fatalf("get %s missing refs line: %q", prop, out)
	}
}

// TestDraftUnknownRefsRefused (T-0117): a ref to an ID that resolves nowhere
// (neither a live item nor a journal tombstone) refuses with the ! ARG E
// prefix, names the unknown ID, and persists nothing.
func TestDraftUnknownRefsRefused(t *testing.T) {
	root := t.TempDir()
	sess := connectRoot(t, root)

	out := callText(t, sess, "draft", map[string]any{
		"kind": "proposal", "title": "cache kernels in VRAM",
		"refs": []string{"R-9999"},
	})
	if !strings.Contains(out, "! ARG E") || !strings.Contains(out, "R-9999") {
		t.Fatalf("unknown ref not refused: %q", out)
	}
	// nothing persisted: no item exists at all, so any well-formed ID misses.
	nf := callText(t, sess, "get", map[string]any{"id": "P-0001"})
	if !strings.HasPrefix(nf, "nf") {
		t.Fatalf("draft with unknown ref must not persist an item: get P-0001 = %q", nf)
	}
	if items := callText(t, sess, "state", map[string]any{"section": "items"}); strings.Contains(items, "cache kernels in VRAM") {
		t.Fatalf("draft with unknown ref persisted an item: %q", items)
	}
}

// TestDraftRefsArchivedItem (T-0117): a ref to an archived (tombstoned)
// item is a legitimate citation and must pass validation.
func TestDraftRefsArchivedItem(t *testing.T) {
	root := t.TempDir()
	sess := connectRoot(t, root)

	prop := draftID(t, sess, map[string]any{"kind": "proposal", "title": "old idea"})
	callText(t, sess, "move", map[string]any{"id": prop, "to": "archived", "note": "shipped"})

	task := draftID(t, sess, map[string]any{
		"kind": "task", "title": "follow-up work",
		"refs": []string{prop},
	})
	got := callText(t, sess, "get", map[string]any{"id": task})
	if !strings.Contains(got, "refs "+prop+"\n") {
		t.Fatalf("get %s missing refs line for archived citation: %q", task, got)
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

	prop := draftID(t, sess, map[string]any{
		"kind": "proposal", "title": "cache kernels in VRAM",
		"body": "Keep compiled kernels resident.",
	})
	out := callText(t, sess, "get", map[string]any{"id": prop})
	if !strings.Contains(out, "i "+prop+" proposal draft") {
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

// TestNeedSlotFallbackIsError pins B-01KYE0RCT: rule op=add with missing
// slots and no elicitation UI performs NO action, so the need response must
// carry IsError — a shell-driven agent keys on the exit code, and a live
// judge issued nine junk-pattern retries because this read as success. The
// need grammar itself stays the text. Contrast deliberately pinned in
// TestDecideNeedFallbackStaysSuccess: decide's need-decision line IS a
// performed action (the ask is registered, the answer arrives later).
func TestNeedSlotFallbackIsError(t *testing.T) {
	sess := connectRoot(t, t.TempDir())
	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "rule", Arguments: map[string]any{"op": "add"},
	})
	if err != nil {
		t.Fatalf("transport: %v", err)
	}
	if !res.IsError {
		t.Fatal("need slot fallback without a performed action must carry IsError")
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok || !strings.Contains(tc.Text, "need pattern") {
		t.Fatalf("need grammar lost from the refusal text: %v", res.Content[0])
	}
}

// noNewlineDirUnder walks the tree and fails if any directory name carries a
// line break. `ls -b` showed a directory literally named `a\nb` under the
// workspace root before B-01KYRN4VBEEXQ; asserting on the filesystem rather
// than on the refusal message is the point — a test that only reads the
// message passes while the directory is still created.
func noNewlineDirUnder(t *testing.T, root string) {
	t.Helper()
	if err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.ContainsAny(d.Name(), "\r\n") {
			t.Errorf("a path with an embedded line break was created: %q", p)
		}
		return nil
	}); err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
}

// TestDraftRefusesNewlineInDir pins O2 of B-01KYRN4VBEEXQ through the wire.
// `draft {dir: "a\nb"}` exited 0, created a directory whose name held a
// newline, and split the dense `i ...` record line in two — so the one
// grammar every caller parses was broken for the record it had just asked
// for. sess.CallTool directly, not callText, because SRF-001's requirement is
// the non-zero exit and callText cannot see IsError.
func TestDraftRefusesNewlineInDir(t *testing.T) {
	root := t.TempDir()
	sess := connectRoot(t, root)
	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "draft", Arguments: map[string]any{
			"kind": "task", "title": "split me", "dir": "a\nb",
		},
	})
	if err != nil {
		t.Fatalf("transport: %v", err)
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("content is %T, want TextContent", res.Content[0])
	}
	// t.Error, not t.Fatal: the filesystem assertion below is the one that
	// sees the actual damage, and it must still run when the call succeeded.
	if !res.IsError {
		t.Errorf("a dir holding a newline must refuse with IsError per SRF-001: %q", tc.Text)
	} else {
		if !strings.Contains(tc.Text, "! ARG E") || !strings.Contains(tc.Text, "dir") {
			t.Errorf("refusal must name the dir argument: %q", tc.Text)
		}
		if got := len(strings.Split(strings.TrimRight(tc.Text, "\n"), "\n")); got != 1 {
			t.Errorf("the refusal itself spans %d lines, breaking the record grammar: %q", got, tc.Text)
		}
	}
	noNewlineDirUnder(t, root)
}

// TestDraftRefusesDirEscapingRoot: the same argument, the other way out. A
// "../" dir scaffolded a .spectackle tree OUTSIDE the workspace root, which no
// context dir may name. Asserted on the filesystem — the parent of the temp
// root is where it landed.
func TestDraftRefusesDirEscapingRoot(t *testing.T) {
	root := t.TempDir()
	sess := connectRoot(t, root)
	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "draft", Arguments: map[string]any{
			"kind": "task", "title": "escape me", "dir": "../draftescape",
		},
	})
	if err != nil {
		t.Fatalf("transport: %v", err)
	}
	if !res.IsError {
		t.Error("a dir escaping the workspace root must refuse with IsError")
	}
	if _, err := os.Stat(filepath.Join(root, "..", "draftescape")); !os.IsNotExist(err) {
		t.Fatalf("scaffolding landed outside the workspace root: %v", err)
	}
}

// TestRuleAddRefusesNewlineInDir: rule op=add takes the same caller-supplied
// dir and hands it to spec.AddRule, which scaffolds it — a second live route
// to the same corruption, and the reason the guard sits on the argument at
// every call site rather than once inside the writer.
func TestRuleAddRefusesNewlineInDir(t *testing.T) {
	root := t.TempDir()
	sess := connectRoot(t, root)
	for _, dir := range []string{"a\nb", "../ruleescape"} {
		res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{
			Name: "rule", Arguments: map[string]any{
				"op": "add", "dir": dir, "pattern": "E", "stem": "ESC-DIR",
				"system":   "host wrapper",
				"response": "check cudaGetLastError and propagate its numeric value to the caller",
				"trigger":  "a kernel launch statement returns",
			},
		})
		if err != nil {
			t.Fatalf("transport: %v", err)
		}
		tc, ok := res.Content[0].(*mcp.TextContent)
		if !ok {
			t.Fatalf("content is %T, want TextContent", res.Content[0])
		}
		if !res.IsError {
			t.Errorf("rule add accepted dir %q: %q", dir, tc.Text)
			continue
		}
		if !strings.Contains(tc.Text, "! ARG E") || !strings.Contains(tc.Text, "dir") {
			t.Errorf("refusal must name the dir argument: %q", tc.Text)
		}
		if got := len(strings.Split(strings.TrimRight(tc.Text, "\n"), "\n")); got != 1 {
			t.Errorf("the refusal spans %d lines: %q", got, tc.Text)
		}
	}
	noNewlineDirUnder(t, root)
	if _, err := os.Stat(filepath.Join(root, "..", "ruleescape")); !os.IsNotExist(err) {
		t.Fatalf("rule scaffolding landed outside the workspace root: %v", err)
	}
}

// TestDraftRefusesHeadingShapedBody pins O1 of B-01KYRN4VBEEXQ at the gate:
// `draft {body: "## T-9999 phantom\nkind: bug\nstate: archived"}` exited 0 and
// was accepted, and on reload a phantom record appeared carrying the caller's
// kind and a TERMINAL state no transition can reach. The assertion is on the
// ACTION — what `state` counts afterward — not only on the refusal wording.
func TestDraftRefusesHeadingShapedBody(t *testing.T) {
	sess := connectRoot(t, t.TempDir())
	// One legitimate item first, so the count afterward is a real number
	// rather than an empty render, and so the phantom would be the SECOND.
	draftID(t, sess, map[string]any{"kind": "task", "title": "real", "body": "real body"})
	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "draft", Arguments: map[string]any{
			"kind": "task", "title": "host", "body": "mine\n## T-9999 phantom\nkind: bug\nstate: archived",
		},
	})
	if err != nil {
		t.Fatalf("transport: %v", err)
	}
	if !res.IsError {
		t.Fatal("a body forging an item heading must refuse with IsError")
	}
	assertNoMintedIDAdvertised(t, res)
	// A leading space must not buy the caller a pass: lifecycle.Draft
	// TrimSpaces the body itself, so an untrimmed check would accept this and
	// then trim it INTO a heading one call later.
	res, err = sess.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "draft", Arguments: map[string]any{
			"kind": "task", "title": "host", "body": "  ## T-9999 phantom\nkind: bug\nstate: archived",
		},
	})
	if err != nil {
		t.Fatalf("transport: %v", err)
	}
	if !res.IsError {
		t.Fatal("a leading-space body that trims into a heading must refuse too")
	}
	assertNoMintedIDAdvertised(t, res)
	out := callText(t, sess, "state", nil)
	if strings.Contains(out, "T-9999") {
		t.Fatalf("phantom record reachable from state: %q", out)
	}
	if !strings.Contains(out, "total=1") {
		t.Fatalf("expected total=1 after two refused drafts: %q", out)
	}
}

// reMintedIDAdvertised matches headerSafe's refusal, the one the WRITE path
// produces: `refused: <id> not written — ...`. Its ID has already been minted,
// which is precisely why draft pre-checks the body before it mints
// (tools.go, B-01KYRN4VBEEXQ) — a refusal that names a record the caller can
// never get sends agents looking for it.
//
// This is an assertion on the message and nothing else, deliberately: the
// pre-check and the write-path guard refuse the SAME calls, so no observable
// side effect distinguishes them. The dangerous action itself — the phantom
// record — is asserted separately by each caller via `state`.
var reMintedIDAdvertised = regexp.MustCompile(`refused: ` + anchoredIDPat() + ` not written`)

// anchoredIDPat borrows item.IDRe's own ID grammar rather than restating it —
// the ID schemes are versioned (ADR-0013 added a second one) and a copy here
// would stop matching the day a third lands, quietly turning the assertion
// above into a no-op.
func anchoredIDPat() string {
	return strings.TrimSuffix(strings.TrimPrefix(item.IDRe.String(), "^"), "$")
}

func assertNoMintedIDAdvertised(t *testing.T, res *mcp.CallToolResult) {
	t.Helper()
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("content is %T, want TextContent", res.Content[0])
	}
	if !strings.Contains(tc.Text, "! ARG E") {
		t.Fatalf("refusal must be an argument refusal: %q", tc.Text)
	}
	if reMintedIDAdvertised.MatchString(tc.Text) {
		t.Fatalf("refusal advertises a record that was never written: %q", tc.Text)
	}
}

// TestReviseRefusesHeadingShapedBody: revision is the SECOND live route into a
// body and it bypasses draft's pre-mint check entirely, so the guard has to be
// on the revise path too. Asserted through `state`'s total — before the fix it
// reported two items, the second a `done` proposal the caller chose.
func TestReviseRefusesHeadingShapedBody(t *testing.T) {
	sess := connectRoot(t, t.TempDir())
	id := draftID(t, sess, map[string]any{
		"kind": "task", "title": "host", "body": "the host body",
	})
	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "draft", Arguments: map[string]any{
			"id": id, "body": "## P-0001 injected\nkind: proposal\nstate: done",
		},
	})
	if err != nil {
		t.Fatalf("transport: %v", err)
	}
	if !res.IsError {
		t.Fatal("revision accepted a body that forges an item heading")
	}
	// Without the pre-check the refusal is item.Upsert's raw error, which
	// carries no `! ARG E` prefix at all — an SRF-001 grammar an agent parses
	// silently degraded into free text.
	assertNoMintedIDAdvertised(t, res)
	out := callText(t, sess, "state", nil)
	if !strings.Contains(out, "total=1") {
		t.Fatalf("expected total=1 after the refused revision: %q", out)
	}
	if strings.Contains(out, "P-0001") {
		t.Fatalf("phantom proposal reachable from state: %q", out)
	}
}

// TestDecideNeedFallbackStaysSuccess: the other half of the B-01KYE0RCT
// boundary — decide op=ask without an elicitation UI parks the question and
// mints the decision record; the action WAS performed, so IsError would be
// wrong and would teach agents to retry an ask that already registered.
func TestDecideNeedFallbackStaysSuccess(t *testing.T) {
	sess := connectRoot(t, t.TempDir())
	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "decide", Arguments: map[string]any{
			"op": "ask", "question": "which color?", "options": []string{"red", "blue"},
		},
	})
	if err != nil {
		t.Fatalf("transport: %v", err)
	}
	tc, _ := res.Content[0].(*mcp.TextContent)
	if res.IsError {
		t.Fatalf("registered ask wrongly marked IsError: %v", tc)
	}
	if tc == nil || !strings.Contains(tc.Text, "need decision") {
		t.Fatalf("expected the need decision fallback line: %v", res.Content[0])
	}
}

// TestNeedRefusalCarriesCallShape pins T-01KYE5: the rule need refusal
// carries the assembled call shape exactly once, naming all six fields,
// alongside every missing slot question — the tricky judges showed slot
// questions alone cost 3-5 groping attempts per rule.
func TestNeedRefusalCarriesCallShape(t *testing.T) {
	sess := connectRoot(t, t.TempDir())
	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "rule", Arguments: map[string]any{"op": "add"},
	})
	if err != nil {
		t.Fatalf("transport: %v", err)
	}
	if !res.IsError {
		t.Fatal("need refusal lost IsError")
	}
	txt := res.Content[0].(*mcp.TextContent).Text
	if got := strings.Count(txt, "shape: rule "); got != 1 {
		t.Fatalf("shape line count = %d, want exactly 1:\n%s", got, txt)
	}
	for _, field := range []string{`"op":"add"`, `"dir"`, `"stem"`, `"pattern":"U|E|S|N|O|C"`, `"system"`, `"response"`} {
		if !strings.Contains(txt, field) {
			t.Fatalf("shape line missing %s:\n%s", field, txt)
		}
	}
	if !strings.Contains(txt, "need pattern") {
		t.Fatalf("slot questions lost:\n%s", txt)
	}
}

// TestApprovedTransitionNamesWorkFlow pins T-01KYEBT: the approved result
// carries the one next-step line pointing at the work flow — two of four
// live worktree judges edited main directly because nothing in the
// per-call surface named it — and every other transition stays clean of
// the line.
func TestApprovedTransitionNamesWorkFlow(t *testing.T) {
	sess := connectRoot(t, t.TempDir())
	id := draftID(t, sess, map[string]any{"kind": "task", "title": "flow pointer"})

	out := callText(t, sess, "move", map[string]any{"id": id, "to": "approved"})
	if !strings.Contains(out, "next: work op=start item=") {
		t.Fatalf("approved result missing the work-flow pointer:\n%s", out)
	}
	out = callText(t, sess, "move", map[string]any{"id": id, "to": "active"})
	if strings.Contains(out, "next: work op=start") {
		t.Fatalf("non-approved transition carries the pointer:\n%s", out)
	}
}

// TestCheckRebindsAndAuditsCrossFile pins B-01KYJB3SGK at the check-flow
// layer over a real index: two main packages collide on go:main.main, the
// walk order assigns the tilde, and deleting one flips the assignment.
// The anchor must follow its CONTENT (d rebound), and an ID that re-binds
// to an unrelated file without a hash match must audit (crossfile), never
// heal.
func TestCheckRebindsAndAuditsCrossFile(t *testing.T) {
	root := t.TempDir()
	cliSrc := "package main\n\nfunc main() {\n\tprintln(\"cli\")\n}\n"
	exSrc := "package main\n\nfunc main() {\n\tprintln(\"example\")\n}\n"
	if err := os.MkdirAll(filepath.Join(root, "examples", "saxpy"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte(cliSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "examples", "saxpy", "main.go"), []byte(exSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	sess := connectRoot(t, root)

	// discover which ID the walk gave the CLI main (examples/ sorts first,
	// so the CLI is expected to carry the tilde — but ASK, don't assume)
	found := callText(t, sess, "find", map[string]any{"q": "main.main", "scope": "code"})
	cliID, exID := "", ""
	for _, l := range strings.Split(found, "\n") {
		f := strings.Fields(l)
		if len(f) < 4 || f[0] != "n" {
			continue
		}
		switch {
		case strings.HasPrefix(f[3], "main.go:"):
			cliID = f[1]
		case strings.HasPrefix(f[3], "examples/saxpy/main.go:"):
			exID = f[1]
		}
	}
	if cliID == "" || exID == "" || cliID == exID {
		t.Fatalf("fixture: could not discover both main IDs:\n%s", found)
	}

	out := callText(t, sess, "rule", map[string]any{
		"op": "add", "dir": "", "pattern": "U", "stem": "RBD-TST",
		"system":   "the CLI entrypoint",
		"response": "print the literal cli exactly once via println",
		"applies":  []string{cliID},
	})
	if !strings.Contains(out, "ok RBD-TST-001") {
		t.Fatalf("rule add: %q", out)
	}
	if out = callText(t, sess, "check", map[string]any{}); strings.Contains(out, "d ") {
		t.Fatalf("fresh anchor must not drift: %q", out)
	}

	// delete the example: the walk reassigns the surviving main a NEW ID
	// (the bare stem) — the stored ID either vanishes or crosses files;
	// only the content hash identifies the true target. No re-touch
	// needed: the tree-shape probe sees the deletion (B-01KYK7W45HF54).
	if err := os.Remove(filepath.Join(root, "examples", "saxpy", "main.go")); err != nil {
		t.Fatal(err)
	}
	out = callText(t, sess, "check", map[string]any{})
	if !strings.Contains(out, "d rebound RBD-TST-001 "+cliID) {
		t.Fatalf("the anchor must follow its content to the reassigned ID:\n%s", out)
	}
	if strings.Contains(out, "crossfile") {
		t.Fatalf("a hash-matching rebind must not audit:\n%s", out)
	}
	// the rebind sticks: a second check is clean
	if out = callText(t, sess, "check", map[string]any{}); strings.Contains(out, "d ") {
		t.Fatalf("the rebind must persist: %q", out)
	}

	// crossfile: re-create the example, anchor IT, then delete it — the
	// surviving CLI main takes over the stored ID with foreign content.
	if err := os.WriteFile(filepath.Join(root, "examples", "saxpy", "main.go"), []byte(exSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	// the tree-shape probe sees the creation too — check rebuilds and the
	// new file is indexed without touching any anchored file
	callText(t, sess, "check", map[string]any{})
	found = callText(t, sess, "find", map[string]any{"q": "main.main", "scope": "code"})
	exID = ""
	for _, l := range strings.Split(found, "\n") {
		f := strings.Fields(l)
		if len(f) >= 4 && f[0] == "n" && strings.HasPrefix(f[3], "examples/saxpy/main.go:") {
			exID = f[1]
		}
	}
	if exID == "" {
		t.Fatalf("fixture: example ID missing after re-create:\n%s", found)
	}
	out = callText(t, sess, "rule", map[string]any{
		"op": "add", "dir": "", "pattern": "U", "stem": "XFL-TST",
		"system":   "the example entrypoint",
		"response": "print the literal example exactly once via println",
		"applies":  []string{exID},
	})
	if !strings.Contains(out, "ok XFL-TST-001") {
		t.Fatalf("rule add: %q", out)
	}
	if err := os.Remove(filepath.Join(root, "examples", "saxpy", "main.go")); err != nil {
		t.Fatal(err)
	}
	out = callText(t, sess, "check", map[string]any{})
	if !strings.Contains(out, "d audit XFL-TST-001") || !strings.Contains(out, "crossfile now=main.go") {
		t.Fatalf("an ID crossing files without a hash match must audit naming the impostor:\n%s", out)
	}
	if strings.Contains(out, "d healed XFL-TST-001") || strings.Contains(out, "d rebound XFL-TST-001") {
		t.Fatalf("crossfile must never heal or rebind:\n%s", out)
	}
	if !strings.Contains(out, "audit=1") {
		t.Fatalf("the audit counter must count crossfile:\n%s", out)
	}
}

// TestCheckDoubleRebindSwapKeepsBothAnchors pins cross-val-rebind finding
// 1: one rule anchoring BOTH nodes of a tilde collision, both IDs swapped
// in one pass — the two-phase write-back must keep both rows; the inline
// delete+Upsert interleave silently ate one.
func TestCheckDoubleRebindSwapKeepsBothAnchors(t *testing.T) {
	root := t.TempDir()
	cliSrc := "package main\n\nfunc main() {\n\tprintln(\"cli\")\n}\n"
	exSrc := "package main\n\nfunc main() {\n\tprintln(\"example\")\n}\n"
	if err := os.MkdirAll(filepath.Join(root, "examples", "saxpy"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte(cliSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "examples", "saxpy", "main.go"), []byte(exSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	sess := connectRoot(t, root)
	found := callText(t, sess, "find", map[string]any{"q": "main.main", "scope": "code"})
	var ids []string
	for _, l := range strings.Split(found, "\n") {
		if f := strings.Fields(l); len(f) >= 2 && f[0] == "n" {
			ids = append(ids, f[1])
		}
	}
	if len(ids) != 2 {
		t.Fatalf("fixture: want the two colliding mains:\n%s", found)
	}
	out := callText(t, sess, "rule", map[string]any{
		"op": "add", "dir": "", "pattern": "U", "stem": "SWP-TST",
		"system":   "every program entrypoint",
		"response": "call println exactly 1 time",
		"applies":  ids,
	})
	if !strings.Contains(out, "ok SWP-TST-001") {
		t.Fatalf("rule add: %q", out)
	}
	// simulate the walk-order ID flip by swapping the two anchor rows'
	// Node columns on disk — content stays where it is
	ws := workspace.Root{Dir: root}
	anchors, err := drift.Load(ws)
	if err != nil {
		t.Fatal(err)
	}
	if len(anchors) != 2 {
		t.Fatalf("want 2 anchors, got %d", len(anchors))
	}
	anchors[0].Node, anchors[1].Node = anchors[1].Node, anchors[0].Node
	if err := drift.Save(ws, anchors); err != nil {
		t.Fatal(err)
	}

	out = callText(t, sess, "check", map[string]any{})
	if got := strings.Count(out, "d rebound SWP-TST-001"); got != 2 {
		t.Fatalf("both swapped anchors must rebind, got %d:\n%s", got, out)
	}
	after, err := drift.Load(ws)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 2 {
		t.Fatalf("the swap rebind must keep BOTH rows, got %d: %+v", len(after), after)
	}
	files := map[string]bool{}
	for _, a := range after {
		files[a.File] = true
	}
	if !files["main.go"] || !files["examples/saxpy/main.go"] {
		t.Fatalf("each file must keep its anchor: %+v", after)
	}
	// stable: a second check is clean
	if out = callText(t, sess, "check", map[string]any{}); strings.Contains(out, "d ") {
		t.Fatalf("the double rebind must persist cleanly: %q", out)
	}
}

// TestCheckSeesDeletionsAndCreations pins B-01KYK7W45HF54: file mtimes are
// blind to creations and deletions, so the tree-shape probe (dir count +
// newest dir mtime) must trigger the rebuild — ghost nodes drop after a
// delete, and a brand-new file is indexed by check without any anchored
// file being touched.
func TestCheckSeesDeletionsAndCreations(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "keep.go"), []byte("package demo\n\nfunc Keep() int {\n\treturn 1\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "doomed.go"), []byte("package demo\n\nfunc Doomed() int {\n\treturn 2\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sess := connectRoot(t, root)
	// anchor ONLY keep.go — the deletion below must be seen without any
	// anchored file changing
	out := callText(t, sess, "rule", map[string]any{
		"op": "add", "dir": "", "pattern": "U", "stem": "SHP-TST",
		"system":   "the kept function",
		"response": "return the constant 1 unchanged",
		"applies":  []string{"go:demo.Keep"},
	})
	if !strings.Contains(out, "ok SHP-TST-001") {
		t.Fatalf("rule add: %q", out)
	}
	if out := callText(t, sess, "find", map[string]any{"q": "demo.Doomed", "scope": "code"}); !strings.Contains(out, "go:demo.Doomed") {
		t.Fatalf("fixture: doomed not indexed: %q", out)
	}

	// deletion: no anchored file changes, only the tree shape
	if err := os.Remove(filepath.Join(root, "doomed.go")); err != nil {
		t.Fatal(err)
	}
	callText(t, sess, "check", map[string]any{})
	if out := callText(t, sess, "find", map[string]any{"q": "demo.Doomed", "scope": "code"}); strings.Contains(out, "go:demo.Doomed") {
		t.Fatalf("ghost node survived the deletion: %q", out)
	}

	// creation: a brand-new file is indexed by the next check, no touches
	if err := os.WriteFile(filepath.Join(root, "fresh.go"), []byte("package demo\n\nfunc Fresh() int {\n\treturn 3\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	callText(t, sess, "check", map[string]any{})
	if out := callText(t, sess, "find", map[string]any{"q": "demo.Fresh", "scope": "code"}); !strings.Contains(out, "go:demo.Fresh") {
		t.Fatalf("new file not indexed after check: %q", out)
	}
}

// TestCheckFlagsVacuousDirtyTests pins T-01KYKGZT0S: the validate pack's
// AST vacuous-test detector runs in-loop at check over DIRTY test files —
// two outcome judge batches reached done with assertion-free tests that
// only post-hoc scoring caught. Committed legacy tests stay quiet.
func TestCheckFlagsVacuousDirtyTests(t *testing.T) {
	root := gitRoot(t)
	sess := connectRoot(t, root)

	vacSrc := `package demo

import "testing"

func TestSmoke(t *testing.T) {
	t.Run("does not crash", func(t *testing.T) {
		_ = 1 + 1
	})
}
`
	if err := os.WriteFile(filepath.Join(root, "demo_test.go"), []byte(vacSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	// (a) dirty + vacuous: check flags it with file:line
	out := callText(t, sess, "check", map[string]any{})
	if !strings.Contains(out, "! VAC W demo_test.go:") || !strings.Contains(out, "subtest without assertion") {
		t.Fatalf("dirty vacuous test must surface at check:\n%s", out)
	}

	// (b) committed: the dirty-only scope silences it — check is bare ok
	if err := exec.Command("git", "-C", root,
		"-c", "user.name=t", "-c", "user.email=t@t", "add", "-A").Run(); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command("git", "-C", root,
		"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "legacy").Run(); err != nil {
		t.Fatal(err)
	}
	if out = callText(t, sess, "check", map[string]any{}); strings.Contains(out, "VAC") {
		t.Fatalf("committed legacy tests must stay quiet:\n%s", out)
	}

	// (c) a dirty test whose subtests assert stays quiet
	goodSrc := `package demo

import "testing"

func TestReal(t *testing.T) {
	t.Run("asserts", func(t *testing.T) {
		if 1+1 != 2 {
			t.Fatal("math broke")
		}
	})
}
`
	if err := os.WriteFile(filepath.Join(root, "good_test.go"), []byte(goodSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	if out = callText(t, sess, "check", map[string]any{}); strings.Contains(out, "VAC") {
		t.Fatalf("an asserting dirty test must not flag:\n%s", out)
	}

	// (d) the cap: 12 vacuous subtests render 10 findings + a +2 more tail
	var b strings.Builder
	b.WriteString("package demo\n\nimport \"testing\"\n\nfunc TestMany(t *testing.T) {\n")
	for i := 0; i < 12; i++ {
		fmt.Fprintf(&b, "\tt.Run(\"c%d\", func(t *testing.T) { _ = %d })\n", i, i)
	}
	b.WriteString("}\n")
	if err := os.WriteFile(filepath.Join(root, "many_test.go"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	out = callText(t, sess, "check", map[string]any{})
	if got := strings.Count(out, "! VAC W many_test.go:"); got != 10 {
		t.Fatalf("cap must render 10 findings, got %d:\n%s", got, out)
	}
	if !strings.Contains(out, "! VAC W +2 more") {
		t.Fatalf("the overflow tail is missing:\n%s", out)
	}
	if err := os.Remove(filepath.Join(root, "many_test.go")); err != nil {
		t.Fatal(err)
	}

	// (e)+(f) delegation is assertion (cross-val-vac findings 1+2): a
	// subtest whose only call passes t onward — testify-style or a local
	// helper — must stay quiet; the detector is AST-only so the testify
	// import need not resolve.
	delegSrc := `package demo

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func assertFoo(t *testing.T, got, want int) {
	t.Helper()
	if got != want {
		t.Fatalf("got %d want %d", got, want)
	}
}

func viaHelper(t *testing.T, ok bool) {
	if !ok {
		assertFoo(t, 0, 1)
	}
}

func TestDelegated(t *testing.T) {
	t.Run("testify style", func(t *testing.T) {
		err := error(nil)
		require.NoError(t, err)
	})
	t.Run("local helper", func(t *testing.T) {
		assertFoo(t, 1+1, 2)
	})
	t.Run("transitive helper", func(t *testing.T) {
		viaHelper(t, true)
	})
}
`
	if err := os.WriteFile(filepath.Join(root, "deleg_test.go"), []byte(delegSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	if out = callText(t, sess, "check", map[string]any{}); strings.Contains(out, "VAC") {
		t.Fatalf("genuinely delegating subtests must not flag:\n%s", out)
	}
	if err := os.Remove(filepath.Join(root, "deleg_test.go")); err != nil {
		t.Fatal(err)
	}

	// (g) the round-2 bypass shapes (cross-val-vac): passing t to a
	// no-op or a formatter earns NO credit — both must still flag.
	bypassSrc := `package demo

import (
	"fmt"
	"testing"
)

func noop(t *testing.T) {}

func TestBypass(t *testing.T) {
	t.Run("noop delegation", func(t *testing.T) {
		noop(t)
	})
	t.Run("formatter touch", func(t *testing.T) {
		_ = fmt.Sprintf("%v", t)
	})
}
`
	if err := os.WriteFile(filepath.Join(root, "bypass_test.go"), []byte(bypassSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	out = callText(t, sess, "check", map[string]any{})
	if got := strings.Count(out, "! VAC W bypass_test.go:"); got != 2 {
		t.Fatalf("both bypass shapes must flag, got %d:\n%s", got, out)
	}
}

// TestValidateWarnNamesItsSoftness pins B-01KYKMPAFNEW3: two benchmark
// judges could not tell whether the archive-time VALIDATE W was advisory
// or actionable — the warning now says so itself.
func TestValidateWarnNamesItsSoftness(t *testing.T) {
	root := gitRoot(t)
	writeOfflineGitConfig(t, root)
	s, sess := connectRootWithServer(t, root)
	id := draftFullID(t, s, sess, map[string]any{"kind": "task", "title": "warn softness probe", "targets": []string{"probe.go"}})
	callText(t, sess, "move", map[string]any{"id": id, "to": "active"})
	if err := os.WriteFile(filepath.Join(root, "probe.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	callText(t, sess, "move", map[string]any{"id": id, "to": "done"})
	out := callText(t, sess, "move", map[string]any{"id": id, "to": "archived", "note": "warn probe closed"})
	if !strings.Contains(out, "! VALIDATE W") {
		t.Fatalf("archiving without a verdict must warn:\n%s", out)
	}
	if !strings.Contains(out, "advisory: archive proceeds") {
		t.Fatalf("the warn must name its softness:\n%s", out)
	}
	if !strings.Contains(out, "archived") {
		t.Fatalf("the advisory must not block the archive:\n%s", out)
	}
}

// TestFindRuleLineCarriesPattern pins B-01KYMCHPF6EXZ (found by the
// knowledge/compact gap hunt): the documented grammar is
// `r <ruleID> <P> <scopeDir> <text>` and rule/get both emit the pattern,
// but find dropped it — so a parser following the docs read scopeDir as
// the pattern and the sentence's first word as scopeDir.
func TestFindRuleLineCarriesPattern(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "demo.go"), []byte("package demo\n\nfunc F() int { return 1 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sess := connectRoot(t, root)
	out := callText(t, sess, "rule", map[string]any{
		"op": "add", "dir": "", "pattern": "U", "stem": "PAT-TST",
		"system": "the pattern probe", "response": "return the constant 1",
	})
	if !strings.Contains(out, "ok PAT-TST-001") {
		t.Fatalf("rule add: %q", out)
	}
	fieldsOf := func(src, tag string) []string {
		for _, l := range strings.Split(src, "\n") {
			if f := strings.Fields(l); len(f) > 1 && f[0] == tag && f[1] == "PAT-TST-001" {
				return f
			}
		}
		t.Fatalf("no %q line for the rule in:\n%s", tag, src)
		return nil
	}
	// get is the reference renderer; find must agree field-for-field on
	// the first three tokens (id, pattern, scopeDir)
	ref := fieldsOf(callText(t, sess, "get", map[string]any{"id": "PAT-TST-001"}), "r")
	got := fieldsOf(callText(t, sess, "find", map[string]any{"q": "pattern probe", "scope": "rule"}), "r")
	for i := 0; i < 4; i++ {
		if got[i] != ref[i] {
			t.Fatalf("find r-line field %d = %q, get says %q\nfind: %v\nget:  %v", i, got[i], ref[i], got, ref)
		}
	}
	if got[2] != "U" {
		t.Fatalf("the pattern token must be the EARS pattern, got %q: %v", got[2], got)
	}
}

// TestDraftNodeTargetLandsInItsContextDir pins B-01KYMCJFC3EMN (found by
// the knowledge/compact gap hunt): the documented derivation is
// "targets → deepest common context, else root", and it held for PATH
// targets while node-ID targets — the advertised currency — silently
// landed at root, sending their archive delta to the wrong intent.
func TestDraftNodeTargetLandsInItsContextDir(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "pkg", "foo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "pkg", "foo", "foo.go"),
		[]byte("package foo\n\nfunc Frobnicate() int { return 1 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sess := connectRoot(t, root)
	// a rule makes pkg/foo an unambiguous context dir
	if out := callText(t, sess, "rule", map[string]any{
		"op": "add", "dir": "pkg/foo", "pattern": "U", "stem": "FOO",
		"system": "the foo module", "response": "return the constant 1 from Frobnicate",
	}); !strings.Contains(out, "ok FOO-001") {
		t.Fatalf("rule add: %q", out)
	}
	// discover the node ID the indexer minted
	nodeID := ""
	for _, l := range strings.Split(callText(t, sess, "find", map[string]any{"q": "Frobnicate", "scope": "code"}), "\n") {
		if f := strings.Fields(l); len(f) > 1 && f[0] == "n" {
			nodeID = f[1]
		}
	}
	if nodeID == "" {
		t.Fatal("fixture: Frobnicate was not indexed")
	}

	dirOfDraft := func(args map[string]any) string {
		t.Helper()
		for _, l := range strings.Split(callText(t, sess, "draft", args), "\n") {
			// i <id> <kind> <state> <dir> <title>
			if f := strings.Fields(l); len(f) >= 5 && f[0] == "i" {
				return f[4]
			}
		}
		t.Fatal("no i record in the draft render")
		return ""
	}
	byNode := dirOfDraft(map[string]any{"kind": "task", "title": "via node", "targets": []string{nodeID}})
	byPath := dirOfDraft(map[string]any{"kind": "task", "title": "via path", "targets": []string{"pkg/foo/foo.go"}})
	if byNode != byPath {
		t.Fatalf("the same target as a node ID (%q) and as a path (%q) must land in the same dir", byNode, byPath)
	}
	if byNode != "pkg/foo" {
		t.Fatalf("a node target must land in its context dir, got %q", byNode)
	}
	// an unresolvable node still falls back to root rather than erroring
	if got := dirOfDraft(map[string]any{"kind": "task", "title": "ghost", "targets": []string{"go:nope.Missing"}}); got != "." {
		t.Fatalf("an unresolvable node target must fall back to root, got %q", got)
	}

	// MIXED node+path across different context dirs must land on the
	// common ancestor exactly as two paths would — the shape
	// cross-val-node found landing on the path's own dir, because a
	// derived-root answer was being re-derived node-blind downstream.
	if err := os.MkdirAll(filepath.Join(root, "pkg", "bar"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "pkg", "bar", "bar.go"),
		[]byte("package bar\n\nfunc Bar() int { return 2 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out := callText(t, sess, "rule", map[string]any{
		"op": "add", "dir": "pkg/bar", "pattern": "U", "stem": "BAR",
		"system": "the bar module", "response": "return the constant 2 from Bar",
	}); !strings.Contains(out, "ok BAR-001") {
		t.Fatalf("rule add: %q", out)
	}
	mixed := dirOfDraft(map[string]any{"kind": "task", "title": "mixed",
		"targets": []string{nodeID, "pkg/bar/bar.go"}})
	paths := dirOfDraft(map[string]any{"kind": "task", "title": "paths",
		"targets": []string{"pkg/foo/foo.go", "pkg/bar/bar.go"}})
	if mixed != paths {
		t.Fatalf("a mixed node+path set must scope like the equivalent all-path set: mixed=%q paths=%q", mixed, paths)
	}
}

// TestDocsDocumentEveryRegisteredTool pins B-01KYMCKDD5FYB's second half:
// docs/tools.md claims normativity (SPX-REPO-001) but had no `validate`
// section at all and omitted `grill op=verdict` — a gap hunt could only
// discover validate by chasing an unexplained warning into source.
func TestDocsDocumentEveryRegisteredTool(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "tools.md"))
	if err != nil {
		t.Skipf("docs unavailable: %v", err)
	}
	doc := string(raw)
	sess := connectRoot(t, t.TempDir())
	res, err := sess.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range res.Tools {
		if !strings.Contains(doc, "`"+tool.Name+"`") {
			t.Errorf("registered tool %q has no section in docs/tools.md", tool.Name)
		}
	}
	// the two ops whose absence sent the gap hunt into the source
	for _, frag := range []string{"op=verdict", "feedback.validate"} {
		if !strings.Contains(doc, frag) {
			t.Errorf("docs/tools.md never mentions %q", frag)
		}
	}
}

// TestMoveRefusalRendersShortID: the ARG E refusal names the record in the
// SAME display form the success line of the same session printed.
//
// Every lifecycle error carries the STORED id, because lifecycle composes the
// message and knows nothing about display prefixes — so `move` used to answer
// a refusal with a 28-character id one call after answering a success with the
// 15-character one, and an agent copying the id out of the refusal got a
// different vocabulary than the rest of the surface (B-01KYS6ZJQSE1E). The
// GATE E branch beside it already substituted; this is the same substitution
// on the branch every other lifecycle refusal takes.
//
// The archived record is the sharpest case to assert it on: it is the refusal
// a caller is most likely to be reading an id out of, since the id is no
// longer in work.md for them to look up.
func TestMoveRefusalRendersShortID(t *testing.T) {
	root := t.TempDir()
	s, sess := connectRootWithServer(t, root)

	full := draftFullID(t, s, sess, map[string]any{
		"kind": "proposal", "title": "closed for good", "body": ambFixturePad})
	// a sibling, so the display form is genuinely computed against a
	// contested floor rather than falling out of an uncontested default
	draftID(t, sess, map[string]any{
		"kind": "proposal", "title": "a contender", "body": ambFixturePad})

	out := callText(t, sess, "move", map[string]any{
		"id": full, "to": "archived", "note": "shipped"})
	emitted := idOfRecord(t, out, "i")
	if emitted == full {
		t.Fatalf("fixture: the success line already printed the full id, so there is no width to compare: %q", out)
	}

	ref, isErr := callRaw(t, sess, "move", map[string]any{"id": full, "to": "active"})
	if !isErr {
		t.Fatalf("moving out of archived was accepted: %q", ref)
	}
	if !strings.Contains(ref, "! ARG E") {
		t.Fatalf("refusal is not an ARG E record: %q", ref)
	}
	// The width, stated as the defect: the full id must not appear at all.
	// Checked before the presence assertion below, since the display form is
	// a PREFIX of the stored one and "contains the short id" is satisfied by
	// a line carrying the long one.
	if strings.Contains(ref, full) {
		t.Fatalf("refusal printed the %d-char stored id; the success line of the same session printed the %d-char display form %s: %q",
			len(full), len(emitted), emitted, ref)
	}
	if !strings.Contains(ref, emitted) {
		t.Fatalf("refusal does not name the record as %s at all: %q", emitted, ref)
	}
	// The refusal is about the record's terminality, not about the id being
	// unresolvable — `get` answers the same id in the same session.
	if strings.Contains(ref, "unknown item") {
		t.Fatalf("archived record refused as unknown: %q", ref)
	}
	// And the ACTION: a refused move must not have put the record back.
	if got, ok, err := item.Get(s.ws, full); err != nil {
		t.Fatal(err)
	} else if ok {
		t.Fatalf("the refused move put %s back into work.md as %s", full, got.State)
	}
}
