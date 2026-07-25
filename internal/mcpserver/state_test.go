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
	if !strings.Contains(out, "ok items total=1 active=0 approved=0 draft=1 submitted=0 done=0") {
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
}
