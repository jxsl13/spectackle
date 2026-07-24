package mcpserver

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
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

// TestCommandsGenClaudeWritesBothFiles: gen harness=claude regenerates every
// .claude/commands file (the original workflow/state pair plus the six
// T-0113 additions) with the generated-header stamp, each command's own
// frontmatter description, and the workflow file's two-mode $ARGUMENTS
// dispatch content intact.
func TestCommandsGenClaudeWritesBothFiles(t *testing.T) {
	root := t.TempDir()
	_, sess := connectCommands(t, root, nil)

	out := callText(t, sess, "commands", map[string]any{"op": "gen", "harness": []string{"claude"}})

	wantFiles := []string{
		"spectackle.md", "spectackle-state.md", "spectackle-find.md", "spectackle-get.md",
		"spectackle-research.md", "spectackle-swarm.md", "spectackle-export.md", "spectackle-merge.md",
	}
	for _, name := range wantFiles {
		if !strings.Contains(out, "ok gen claude .claude/commands/"+name) {
			t.Fatalf("expected %s to be written; got: %q", name, out)
		}
	}

	// every commandSpec's file carries its own description and the header
	for _, spec := range commandSpecs {
		p := filepath.Join(root, ".claude", "commands", claudeFilename("spectackle", spec.Name))
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		if !strings.Contains(string(b), generatedHeader) {
			t.Fatalf("%s missing generated header:\n%s", p, b)
		}
		if !strings.Contains(string(b), "description: "+spec.Description) {
			t.Fatalf("%s missing frontmatter description %q:\n%s", p, spec.Description, b)
		}
	}

	wf, err := os.ReadFile(filepath.Join(root, ".claude", "commands", "spectackle.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(wf), "If `$ARGUMENTS` is empty:") || !strings.Contains(string(wf), "If `$ARGUMENTS` is not empty:") {
		t.Fatalf("spectackle.md lost the two-mode $ARGUMENTS dispatch:\n%s", wf)
	}

	// export/merge must not imply an apply command exists
	ef, err := os.ReadFile(filepath.Join(root, ".claude", "commands", "spectackle-export.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(ef), "/spectackle-apply") || !strings.Contains(string(ef), "wrong front\ndoor") {
		t.Fatalf("spectackle-export.md should explain why there is no apply command:\n%s", ef)
	}
	mf, err := os.ReadFile(filepath.Join(root, ".claude", "commands", "spectackle-merge.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(mf), "/spectackle-apply") || !strings.Contains(string(mf), "wrong front\ndoor") {
		t.Fatalf("spectackle-merge.md should explain why there is no apply command:\n%s", mf)
	}
}

// TestCommandsGenCopilotWritesPromptFiles: gen harness=copilot writes the
// GitHub Copilot dialect under .github/prompts — the original workflow/state
// pair plus the six T-0113 additions — with `mode: agent` frontmatter and
// the generated-header stamp.
func TestCommandsGenCopilotWritesPromptFiles(t *testing.T) {
	root := t.TempDir()
	_, sess := connectCommands(t, root, nil)

	out := callText(t, sess, "commands", map[string]any{"op": "gen", "harness": []string{"copilot"}})

	names := []string{
		"spectackle.prompt.md", "spectackle-state.prompt.md", "spectackle-find.prompt.md", "spectackle-get.prompt.md",
		"spectackle-research.prompt.md", "spectackle-swarm.prompt.md", "spectackle-export.prompt.md", "spectackle-merge.prompt.md",
	}
	for _, name := range names {
		if !strings.Contains(out, "ok gen copilot .github/prompts/"+name) {
			t.Fatalf("expected %s to be written; got: %q", name, out)
		}
	}

	for _, name := range names {
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
// managed section into AGENTS.md — one "## spectackle <heading>" subsection
// per commandSpec, in order, so the six T-0113 additions read as one
// coherent block alongside the original workflow/state pair rather than
// being appended blindly — and a second run must produce a byte-identical
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
	// every commandSpec's heading appears, in commandSpecs order (coherent
	// concatenation, not appended blindly)
	lastIdx := -1
	for _, spec := range commandSpecs {
		heading := "## spectackle " + spec.Heading
		idx := strings.Index(string(first), heading)
		if idx == -1 {
			t.Fatalf("AGENTS.md missing heading %q:\n%s", heading, first)
		}
		if idx <= lastIdx {
			t.Fatalf("AGENTS.md heading %q out of commandSpecs order:\n%s", heading, first)
		}
		lastIdx = idx
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

// newCommandTemplateNames are the six templates T-0113 adds on top of the
// original workflow/state pair — used by every test below that needs to
// enumerate just the new ones rather than the full commandSpecs set.
var newCommandTemplateNames = []string{
	"find.md.tmpl", "get.md.tmpl", "research.md.tmpl",
	"swarm.md.tmpl", "export.md.tmpl", "merge.md.tmpl",
}

// TestCommandsNewTemplatesRenderNonEmpty covers TEST 1: every new template
// renders without error and produces non-empty output.
func TestCommandsNewTemplatesRenderNonEmpty(t *testing.T) {
	data := commandsData{Binary: "spectackle", Tool: "spectackle", RepoURL: "https://example.com/spectackle"}
	for _, name := range newCommandTemplateNames {
		body, err := renderCommandTemplate(name, data)
		if err != nil {
			t.Fatalf("%s: render error: %v", name, err)
		}
		if strings.TrimSpace(body) == "" {
			t.Fatalf("%s: rendered empty", name)
		}
	}
}

// TestCommandsNewTemplatesNoUnresolvedActions covers TEST 2: rendered output
// contains no unresolved {{ }} — a template typo (e.g. referencing a field
// commandsData doesn't have) currently ships silently since text/template
// only errors on parse, not on every possible bad field reference caught at
// render time; this guards the render-time-visible subset (literal
// double-brace leftovers, e.g. from a copy-pasted but unexecuted action).
func TestCommandsNewTemplatesNoUnresolvedActions(t *testing.T) {
	data := commandsData{Binary: "spectackle", Tool: "spectackle", RepoURL: "https://example.com/spectackle"}
	for _, name := range newCommandTemplateNames {
		body, err := renderCommandTemplate(name, data)
		if err != nil {
			t.Fatalf("%s: render error: %v", name, err)
		}
		if strings.Contains(body, "{{") || strings.Contains(body, "}}") {
			t.Fatalf("%s: unresolved template action in output:\n%s", name, body)
		}
	}
}

// TestCommandsFindTemplateScopesMatchScopeKinds covers TEST 3: the find
// template names only scopes that exist in scopeKinds (tools.go) — asserted
// against the map itself (not a hand-copied literal list) so this test
// breaks the moment a scope is added to or removed from scopeKinds without
// the template being updated to match. find's scope=code path is handled
// outside scopeKinds (tools.go's find() special-cases it before the map
// lookup — it drives the graph, not cache.Search), so it is checked
// separately rather than folded into the scopeKinds comparison.
func TestCommandsFindTemplateScopesMatchScopeKinds(t *testing.T) {
	data := commandsData{Binary: "spectackle", Tool: "spectackle", RepoURL: "https://example.com/spectackle"}
	body, err := renderCommandTemplate("find.md.tmpl", data)
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile("`([a-z]+(?:\\|[a-z]+)+)`")
	m := re.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("find.md.tmpl: no backtick-quoted pipe-delimited scope list found:\n%s", body)
	}
	tokens := strings.Split(m[1], "|")
	got := map[string]bool{}
	for _, tok := range tokens {
		got[tok] = true
	}
	if !got["code"] {
		t.Fatalf("find.md.tmpl scope list %v missing scope=code (find's graph-search scope)", tokens)
	}
	delete(got, "code")
	if len(got) != len(scopeKinds) {
		t.Fatalf("find.md.tmpl scope list %v (%d non-code entries) does not match scopeKinds (%d keys: %v)",
			tokens, len(got), len(scopeKinds), scopeKinds)
	}
	for k := range scopeKinds {
		if !got[k] {
			t.Fatalf("find.md.tmpl scope list %v is missing scopeKinds key %q", tokens, k)
		}
	}
	for tok := range got {
		if _, ok := scopeKinds[tok]; !ok {
			t.Fatalf("find.md.tmpl invents scope %q, not a key of scopeKinds %v", tok, scopeKinds)
		}
	}
}

// TestCommandsGenEveryFileCarriesGeneratedHeader covers TEST 4: op=gen for
// every harness writes files that all carry the do-not-edit header — walked
// generically over commandSpecs so a newly added command is covered without
// touching this test again.
func TestCommandsGenEveryFileCarriesGeneratedHeader(t *testing.T) {
	root := t.TempDir()
	_, sess := connectCommands(t, root, nil)

	out := callText(t, sess, "commands", map[string]any{"op": "gen", "harness": []string{"claude", "copilot", "codex"}})
	if strings.Contains(out, "! ") {
		t.Fatalf("unexpected error in gen output: %q", out)
	}

	for _, spec := range commandSpecs {
		claudeFile := filepath.Join(root, ".claude", "commands", claudeFilename("spectackle", spec.Name))
		b, err := os.ReadFile(claudeFile)
		if err != nil {
			t.Fatalf("%s: %v", claudeFile, err)
		}
		if !strings.Contains(string(b), generatedHeader) {
			t.Fatalf("%s missing generated header:\n%s", claudeFile, b)
		}

		copilotFile := filepath.Join(root, ".github", "prompts", copilotFilename("spectackle", spec.Name))
		b, err = os.ReadFile(copilotFile)
		if err != nil {
			t.Fatalf("%s: %v", copilotFile, err)
		}
		if !strings.Contains(string(b), generatedHeader) {
			t.Fatalf("%s missing generated header:\n%s", copilotFile, b)
		}
	}

	agents, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(agents), generatedHeader) != 1 {
		t.Fatalf("AGENTS.md expected exactly one generated header (single managed section):\n%s", agents)
	}
}
