package mcpserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestStateEmptyWorkspace: a freshly scaffolded workspace has no items, no
// rules, no graph, no anchors, no compact/coverage signal — every
// count-bearing section is omitted outright (SPX-MCP-004 spirit), not
// filled with a "0" filler line. #version is always present.
func TestStateEmptyWorkspace(t *testing.T) {
	sess := connectRoot(t, t.TempDir())
	out := callText(t, sess, "state", map[string]any{})

	if !strings.Contains(out, "#version") {
		t.Fatalf("state missing #version: %q", out)
	}
	if !strings.Contains(out, "ok spectackle") {
		t.Fatalf("state missing version summary line: %q", out)
	}
	for _, sec := range []string{"#items", "#rules", "#graph", "#drift", "#health"} {
		if strings.Contains(out, sec) {
			t.Fatalf("state emitted %s on an empty workspace: %q", sec, out)
		}
	}
}

// TestStateSeededSections: after a draft item and an authored rule (with an
// applies target, forcing a pending anchor) the corresponding sections
// appear with the documented summary line shape.
func TestStateSeededSections(t *testing.T) {
	root := t.TempDir()
	sess := connectRoot(t, root)

	task := draftID(t, sess, map[string]any{"kind": "task", "title": "wire the state tool"})
	callText(t, sess, "rule", map[string]any{
		"op": "add", "pattern": "U", "stem": "STATE",
		"system": "state tool", "response": "report items, rules, graph, swarm, drift and health without writing anything",
		"applies": []string{"go:fake.Node"}, // forces a pending anchor
	})

	out := callText(t, sess, "state", map[string]any{})
	for _, sec := range []string{"#version", "#items", "#rules", "#swarm", "#drift"} {
		if !strings.Contains(out, sec) {
			t.Fatalf("state missing %s on a seeded workspace: %q", sec, out)
		}
	}
	if !strings.Contains(out, "ok items total=1 draft=1") {
		t.Fatalf("state items summary: %q", out)
	}
	if !strings.Contains(out, "i "+task+" task draft") {
		t.Fatalf("state missing the seeded item's i line: %q", out)
	}
	if !strings.Contains(out, "ok rules total=1 dirs=1") {
		t.Fatalf("state rules summary: %q", out)
	}
	if !strings.Contains(out, "ok anchors total=1 ok=0 pending=1 moved=0") {
		t.Fatalf("state drift summary: %q", out)
	}
	// self is always registered in coord.db on Open — #swarm must at least
	// name this agent.
	if !strings.Contains(out, "ag ") {
		t.Fatalf("state swarm section missing an ag line: %q", out)
	}
	// strictly read-only: nothing from check()'s write side effects (no
	// backprop item, no re-stamped anchor hash) leaked in.
	if strings.Contains(out, "item=") {
		t.Fatalf("state leaked a check()-style backprop annotation: %q", out)
	}
}

// TestStateReadOnly is the read-only contract test: mtimes of every
// versioned .spectackle bundle file, plus the journal's byte length, must be
// bit-for-bit identical before and after any number of state calls.
func TestStateReadOnly(t *testing.T) {
	root := t.TempDir()
	sess := connectRoot(t, root)

	callText(t, sess, "draft", map[string]any{"kind": "task", "title": "seed"})
	callText(t, sess, "rule", map[string]any{
		"op": "add", "pattern": "U", "stem": "STATE",
		"system": "state tool", "response": "report without writing anything",
		"applies": []string{"go:fake.Node"},
	})

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

	// exercise both the tool and the prompt path, repeatedly and with a
	// subtree filter, before re-checking.
	callText(t, sess, "state", map[string]any{})
	callText(t, sess, "state", map[string]any{"path": "."})
	callText(t, sess, "state", map[string]any{"budget": 50})

	for _, f := range files {
		st, err := os.Stat(f)
		if err != nil {
			t.Fatalf("stat %s after: %v", f, err)
		}
		if st.Size() != before[f].Size() || !st.ModTime().Equal(before[f].ModTime()) {
			t.Fatalf("state wrote to %s: size %d->%d mtime %s->%s",
				f, before[f].Size(), st.Size(), before[f].ModTime(), st.ModTime())
		}
	}
	journalAfter, err := os.ReadFile(filepath.Join(root, ".spectackle", "journal.ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	if string(journalAfter) != string(journalBefore) {
		t.Fatalf("state changed the journal:\nbefore=%q\nafter=%q", journalBefore, journalAfter)
	}
}

// TestStatePromptMatchesTool: prompts/get state and tools/call state share
// the stateText builder, so the same sections must appear in both (the tool
// additionally carries a budget cursor and any sw piggyback lines, which the
// prompt path — bypassing gate()/postCall — never adds).
func TestStatePromptMatchesTool(t *testing.T) {
	root := t.TempDir()
	sess := connectRootWithPrompts(t, root)

	callText(t, sess, "draft", map[string]any{"kind": "task", "title": "seed"})

	toolOut := callText(t, sess, "state", map[string]any{})
	promptOut := getPromptText(t, sess, "state", nil)

	for _, sec := range []string{"#version", "#items"} {
		if !strings.Contains(toolOut, sec) {
			t.Fatalf("tool output missing %s: %q", sec, toolOut)
		}
		if !strings.Contains(promptOut, sec) {
			t.Fatalf("prompt output missing %s: %q", sec, promptOut)
		}
	}
	if !strings.Contains(promptOut, "ok items total=1") {
		t.Fatalf("state prompt items summary: %q", promptOut)
	}

	// path argument scopes the prompt exactly like the tool's path field.
	scoped := getPromptText(t, sess, "state", map[string]string{"path": "nowhere"})
	if strings.Contains(scoped, "#items") {
		t.Fatalf("state prompt path filter did not scope items away: %q", scoped)
	}
}

// TestStateOnOwnRepo: the repository's own live workspace must produce a
// full, crash-free snapshot with a real graph behind it. #items is NOT
// asserted: it tracks the live backlog, which is legitimately empty right
// after a wave archives (elided per SPX-MCP-004, like every empty section).
func TestStateOnOwnRepo(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	sess := connectRoot(t, root)
	out := callText(t, sess, "state", map[string]any{})
	for _, sec := range []string{"#version", "#rules", "#graph", "#swarm"} {
		if !strings.Contains(out, sec) {
			t.Fatalf("state on own repo missing %s: %q", sec, out)
		}
	}
	if !strings.Contains(out, "ok graph nodes=") {
		t.Fatalf("state graph summary missing: %q", out)
	}
	// the repo's own module builds clean under the toolchain running this
	// test (go build ./... is part of this same task's verification), so the
	// typed-call pass is healthy here — the output-diet contract (issue 28,
	// SPX-ARC-002) says a healthy pass must add NOTHING, asserted as an
	// absence rather than merely "the happy-path assertions above passed".
	if strings.Contains(out, "! TYPED") {
		t.Fatalf("state on own (healthy) repo must not emit a typed-pass finding: %q", out)
	}
}

// TestStateReportsTypedPassDegradation is issue 28's acceptance test for the
// `state` half: a forced typed-call pass failure (a real go.mod plus a
// package importing a path that does not exist — packages.Load itself
// succeeds, but the affected packages' own Errors are populated, the same
// shape a Go-version toolchain mismatch takes in the field, see
// index.TypedPassError's doc comment) must turn "ok graph nodes=N edges=M"
// into a record naming the cause and the affected package count, not a bare
// graph summary with no hint anything is missing.
func TestStateReportsTypedPassDegradation(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/typedfail\n\ngo 1.21\n")
	writeFile(t, root, "pkg1/a.go", "package pkg1\n\nimport \"example.com/typedfail/nope1\"\n\nfunc A() { nope1.Foo() }\n")
	writeFile(t, root, "pkg2/b.go", "package pkg2\n\nimport \"example.com/typedfail/nope2\"\n\nfunc B() { nope2.Bar() }\n")

	// New() reindexes once during connectRoot, so the fixture above is what
	// the typed pass sees on its one and only run in this test.
	sess := connectRoot(t, root)
	out := callText(t, sess, "state", map[string]any{})

	if !strings.Contains(out, "ok graph nodes=") {
		t.Fatalf("state graph summary missing even though the syntactic pass must have indexed pkg1/pkg2: %q", out)
	}
	if !strings.Contains(out, "! TYPED W") {
		t.Fatalf("state did not surface the forced typed-pass failure as a finding: %q", out)
	}
	if !strings.Contains(out, "packages=2") {
		t.Fatalf("state's typed-pass finding must name the affected package count (pkg1 and pkg2): %q", out)
	}
	if !strings.Contains(out, "nope1") && !strings.Contains(out, "nope2") {
		t.Fatalf("state's typed-pass finding must name the cause (an example failing import): %q", out)
	}
}

// writeFile is a small helper local to this test file (mirrors
// internal/index's test helper of the same name, which mcpserver's tests
// cannot import since it's unexported in a different package).
func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestStateRulesInventoryCollapses (T-01KYDQ): healthy dirs live in the
// summary line only; a per-dir line appears exactly where findings exist —
// the case an agent must act on. The full inventory cost ~500B per state
// call on a mid-sized repo and answered a question nobody asked from state;
// A/B-proven and real-repo-measured at -702B per call at equal validity.
func TestStateRulesInventoryCollapses(t *testing.T) {
	root := t.TempDir()
	sess := connectRootWithPrompts(t, root)
	// root: a lint-clean rule — its dir must NOT be listed.
	callText(t, sess, "rule", map[string]any{
		"op": "add", "dir": "", "stem": "CLP-API", "pattern": "U",
		"system": "collapser", "response": "terminate with exit code 0",
	})
	// sub: a rule whose response names nothing verifiable — W001, so its dir
	// carries a finding and MUST be listed.
	callText(t, sess, "rule", map[string]any{
		"op": "add", "dir": "sub", "stem": "CLP-SUB", "pattern": "U",
		"system": "collapser", "response": "behave nicely",
	})
	out := callText(t, sess, "state", map[string]any{})
	if !strings.Contains(out, "ok rules total=2 dirs=2 findings=1") {
		t.Fatalf("summary line wrong: %q", out)
	}
	if strings.Contains(out, "ok dir . ") {
		t.Fatalf("healthy dir still listed: %q", out)
	}
	if !strings.Contains(out, "ok dir sub rules=1") {
		t.Fatalf("finding dir not listed: %q", out)
	}
}

// TestItemsSummaryNonZeroBucketsAndSideStates pins T-01KYE8 from both
// sides: zero buckets never render, and the side states DO — a blocked
// item awaiting its decide was invisible in the summary line before, and
// the summary is the one line agents read first.
func TestItemsSummaryNonZeroBucketsAndSideStates(t *testing.T) {
	root := gitRoot(t)
	writeOfflineGitConfig(t, root)
	s, sess := connectRootWithServer(t, root)

	id := draftFullID(t, s, sess, map[string]any{"kind": "task", "title": "will be blocked"})
	// max_rounds default 3: exhaust via reopen cycles until the server
	// side-steps the item to blocked.
	callText(t, sess, "move", map[string]any{"id": id, "to": "active"})
	for range 3 {
		callText(t, sess, "move", map[string]any{"id": id, "to": "done"})
		callText(t, sess, "move", map[string]any{"id": id, "to": "active"})
	}

	out := callText(t, sess, "state", map[string]any{})
	if !strings.Contains(out, " blocked=1") {
		t.Fatalf("blocked bucket missing from the items summary:\n%s", out)
	}
	for _, zero := range []string{"active=0", "approved=0", "submitted=0", "done=0", "draft=0"} {
		if strings.Contains(out, zero) {
			t.Fatalf("zero bucket %q rendered:\n%s", zero, out)
		}
	}
}
