package mcpserver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jxsl13/spectackle/internal/item"
)

// connectCommands spins up a server (commands is registered in production by
// registerTools, unlike decide — see decide_test.go's registerDecide) and
// connects a client, optionally wired with an elicitation handler. elicit ==
// nil reproduces the headless / no-UI case: req.Session.Elicit errors and
// commandsElicit reports ok=false.
func connectCommands(t *testing.T, root string, elicit func(context.Context, *mcp.ElicitRequest) (*mcp.ElicitResult, error)) (*Server, *mcp.ClientSession) {
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
	var copts *mcp.ClientOptions
	if elicit != nil {
		copts = &mcp.ClientOptions{ElicitationHandler: elicit}
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, copts)
	sess, err := client.Connect(context.Background(), ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sess.Close() })
	return s, sess
}

// TestCommandsDetectClaudeMarker: a workspace with only .claude/ present
// detects exactly the claude harness, no others.
func TestCommandsDetectClaudeMarker(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".claude", "commands"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, sess := connectCommands(t, root, nil)

	out := callText(t, sess, "commands", map[string]any{"op": "detect"})
	if !strings.Contains(out, "h claude .claude/") {
		t.Fatalf("expected h claude hit: %q", out)
	}
	if strings.Contains(out, "h copilot") || strings.Contains(out, "h codex") || strings.Contains(out, "h kimi") {
		t.Fatalf("unexpected extra harness hits: %q", out)
	}
}

// TestCommandsGenNoHarnessHeadlessMintsDecision: detect finds nothing (empty
// tempdir) and gen is called with no harness= and no elicitation handler —
// the headless/no-UI fallback must mint an open `adr` item instead of
// blocking, mirroring decideAsk's own no-UI behavior.
func TestCommandsGenNoHarnessHeadlessMintsDecision(t *testing.T) {
	root := t.TempDir()
	s, sess := connectCommands(t, root, nil)

	out := callText(t, sess, "commands", map[string]any{"op": "detect"})
	if !strings.HasPrefix(out, "nf harness") {
		t.Fatalf("empty workspace should detect nothing: %q", out)
	}

	out = callText(t, sess, "commands", map[string]any{"op": "gen"})
	if !strings.Contains(out, "need decision ADR-0001") {
		t.Fatalf("expected a need-decision fallback: %q", out)
	}
	d, ok, err := item.Get(s.ws, "ADR-0001")
	if err != nil || !ok {
		t.Fatalf("ADR-0001 not persisted: %v %v", ok, err)
	}
	if d.Kind != "adr" {
		t.Fatalf("minted item is not kind=adr: %+v", d)
	}
	if d.State != item.StateSubmitted {
		t.Fatalf("undelivered decision state = %s, want submitted", d.State)
	}
}

// TestCommandsGenClaudeWritesBothFiles: gen harness=claude regenerates both
// .claude/commands files with the generated-header stamp and the two-mode
// $ARGUMENTS dispatch content intact.
func TestCommandsGenClaudeWritesBothFiles(t *testing.T) {
	root := t.TempDir()
	_, sess := connectCommands(t, root, nil)

	out := callText(t, sess, "commands", map[string]any{"op": "gen", "harness": []string{"claude"}})
	if !strings.Contains(out, "ok gen claude .claude/commands/spectackle.md") ||
		!strings.Contains(out, "ok gen claude .claude/commands/spectackle-state.md") {
		t.Fatalf("expected both claude files written: %q", out)
	}

	wf, err := os.ReadFile(filepath.Join(root, ".claude", "commands", "spectackle.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(wf), generatedHeader) {
		t.Fatalf("spectackle.md missing generated header:\n%s", wf)
	}
	if !strings.Contains(string(wf), "description: spectackle entry point") {
		t.Fatalf("spectackle.md missing frontmatter description:\n%s", wf)
	}
	if !strings.Contains(string(wf), "If `$ARGUMENTS` is empty:") || !strings.Contains(string(wf), "If `$ARGUMENTS` is not empty:") {
		t.Fatalf("spectackle.md lost the two-mode $ARGUMENTS dispatch:\n%s", wf)
	}

	sf, err := os.ReadFile(filepath.Join(root, ".claude", "commands", "spectackle-state.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(sf), generatedHeader) {
		t.Fatalf("spectackle-state.md missing generated header:\n%s", sf)
	}
	if !strings.Contains(string(sf), "description: Render the current spectackle state") {
		t.Fatalf("spectackle-state.md missing frontmatter description:\n%s", sf)
	}
}

// TestCommandsGenCopilotWritesPromptFiles: gen harness=copilot writes the
// GitHub Copilot dialect under .github/prompts with `mode: agent`
// frontmatter and the generated-header stamp.
func TestCommandsGenCopilotWritesPromptFiles(t *testing.T) {
	root := t.TempDir()
	_, sess := connectCommands(t, root, nil)

	out := callText(t, sess, "commands", map[string]any{"op": "gen", "harness": []string{"copilot"}})
	if !strings.Contains(out, "ok gen copilot .github/prompts/spectackle.prompt.md") ||
		!strings.Contains(out, "ok gen copilot .github/prompts/spectackle-state.prompt.md") {
		t.Fatalf("expected both copilot files written: %q", out)
	}

	for _, name := range []string{"spectackle.prompt.md", "spectackle-state.prompt.md"} {
		b, err := os.ReadFile(filepath.Join(root, ".github", "prompts", name))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), "mode: agent") {
			t.Fatalf("%s missing mode: agent frontmatter:\n%s", name, b)
		}
		if !strings.Contains(string(b), generatedHeader) {
			t.Fatalf("%s missing generated header:\n%s", name, b)
		}
	}
}

// TestCommandsGenCodexTwiceIdempotent: gen harness=codex writes exactly one
// managed section into AGENTS.md; a second run must produce a byte-identical
// file (no duplicate sections, no drift).
func TestCommandsGenCodexTwiceIdempotent(t *testing.T) {
	root := t.TempDir()
	_, sess := connectCommands(t, root, nil)

	callText(t, sess, "commands", map[string]any{"op": "gen", "harness": []string{"codex"}})
	first, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(first), sectionBegin) != 1 {
		t.Fatalf("expected exactly one managed section, got:\n%s", first)
	}
	if !strings.Contains(string(first), "## spectackle workflow") || !strings.Contains(string(first), "## spectackle state") {
		t.Fatalf("AGENTS.md missing both command descriptions:\n%s", first)
	}

	callText(t, sess, "commands", map[string]any{"op": "gen", "harness": []string{"codex"}})
	second, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(second), sectionBegin) != 1 {
		t.Fatalf("second run duplicated the managed section:\n%s", second)
	}
	if string(first) != string(second) {
		t.Fatalf("second gen run not byte-identical:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

// TestCommandsGenAgentsPreservesOwnerContent: writeAgentsSection must only
// ever touch the text between the managed-section markers — pre-existing
// owner content outside the section survives untouched.
func TestCommandsGenAgentsPreservesOwnerContent(t *testing.T) {
	root := t.TempDir()
	owner := "# My repo\n\nSome owner-written notes.\n"
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte(owner), 0o644); err != nil {
		t.Fatal(err)
	}
	_, sess := connectCommands(t, root, nil)

	callText(t, sess, "commands", map[string]any{"op": "gen", "harness": []string{"kimi"}})
	got, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), owner) {
		t.Fatalf("owner content lost:\n%s", got)
	}
	if strings.Count(string(got), sectionBegin) != 1 {
		t.Fatalf("expected exactly one managed section:\n%s", got)
	}
}

// TestCommandsGenElicitationAcceptSelectsSubset: with an elicitation handler
// that accepts {claude:true, others:false}, gen with no harness= and nothing
// detected must write only the claude files.
func TestCommandsGenElicitationAcceptSelectsSubset(t *testing.T) {
	root := t.TempDir()
	_, sess := connectCommands(t, root, func(_ context.Context, req *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
		return &mcp.ElicitResult{Action: "accept", Content: map[string]any{
			"claude": true, "copilot": false, "codex": false, "kimi": false,
		}}, nil
	})

	out := callText(t, sess, "commands", map[string]any{"op": "gen"})
	if !strings.Contains(out, "ok gen claude") {
		t.Fatalf("expected claude files written: %q", out)
	}
	if strings.Contains(out, "ok gen copilot") || strings.Contains(out, "ok gen codex") || strings.Contains(out, "ok gen kimi") {
		t.Fatalf("only claude was selected, but other harnesses were written: %q", out)
	}
	if _, err := os.Stat(filepath.Join(root, ".claude", "commands", "spectackle.md")); err != nil {
		t.Fatalf("claude file missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".github", "prompts", "spectackle.prompt.md")); !os.IsNotExist(err) {
		t.Fatalf("copilot file should not have been written: %v", err)
	}
}
