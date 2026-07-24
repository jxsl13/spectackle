package mcpserver

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jxsl13/spectackle/internal/item"
	"github.com/jxsl13/spectackle/internal/wt"
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

// defaultFileStems/optInFileStems are the slash-command name-stem suffixes
// (after "spectackle" / "spectackle-") for the default three and the six
// opt-in exploration commands respectively, used by the tests below to
// assert exactly which files a gen run did or did not write.
var (
	defaultFileStems = []string{"", "-state", "-generate"}
	optInFileStems   = []string{"-find", "-get", "-research", "-swarm", "-export", "-merge"}
)

func claudeNames(stems []string) []string {
	out := make([]string, len(stems))
	for i, s := range stems {
		out[i] = "spectackle" + s + ".md"
	}
	return out
}

func copilotNames(stems []string) []string {
	out := make([]string, len(stems))
	for i, s := range stems {
		out[i] = "spectackle" + s + ".prompt.md"
	}
	return out
}

// dirEntryNames lists the base names of files in dir (test helper; dir must
// exist).
func dirEntryNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

// TestCommandsGenDefaultWritesThreeCommands covers TEST 1: op=gen with no
// all=/commands= writes exactly the three default commands (entry point,
// state, generate) for both the claude and copilot dialects — no trace of
// the six opt-in exploration commands anywhere in the output or on disk.
func TestCommandsGenDefaultWritesThreeCommands(t *testing.T) {
	root := t.TempDir()
	_, sess := connectCommands(t, root, nil)

	out := callText(t, sess, "commands", map[string]any{"op": "gen", "harness": []string{"claude", "copilot"}})

	for _, name := range claudeNames(defaultFileStems) {
		if !strings.Contains(out, "ok gen claude .claude/commands/"+name) {
			t.Fatalf("expected %s to be written; got: %q", name, out)
		}
	}
	for _, name := range copilotNames(defaultFileStems) {
		if !strings.Contains(out, "ok gen copilot .github/prompts/"+name) {
			t.Fatalf("expected %s to be written; got: %q", name, out)
		}
	}
	for _, name := range append(claudeNames(optInFileStems), copilotNames(optInFileStems)...) {
		if strings.Contains(out, name) {
			t.Fatalf("default gen must not mention opt-in command %s; got: %q", name, out)
		}
	}

	gotClaude := sortedCopy(dirEntryNames(t, filepath.Join(root, ".claude", "commands")))
	wantClaude := sortedCopy(claudeNames(defaultFileStems))
	if strings.Join(gotClaude, ",") != strings.Join(wantClaude, ",") {
		t.Fatalf(".claude/commands entries = %v, want exactly %v", gotClaude, wantClaude)
	}
	gotCopilot := sortedCopy(dirEntryNames(t, filepath.Join(root, ".github", "prompts")))
	wantCopilot := sortedCopy(copilotNames(defaultFileStems))
	if strings.Join(gotCopilot, ",") != strings.Join(wantCopilot, ",") {
		t.Fatalf(".github/prompts entries = %v, want exactly %v", gotCopilot, wantCopilot)
	}

	// every written file carries its own description and the header
	for _, spec := range defaultCommandSpecs() {
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
}

// TestCommandsGenAllWritesEightCommands covers TEST 2: op=gen all=true
// writes every one of the eight commands (default three plus all six
// opt-in), for both the claude and copilot dialects, each carrying the
// generated header and its own frontmatter description.
func TestCommandsGenAllWritesEightCommands(t *testing.T) {
	root := t.TempDir()
	_, sess := connectCommands(t, root, nil)

	out := callText(t, sess, "commands", map[string]any{"op": "gen", "harness": []string{"claude", "copilot"}, "all": true})

	allStems := append(append([]string{}, defaultFileStems...), optInFileStems...)
	for _, name := range claudeNames(allStems) {
		if !strings.Contains(out, "ok gen claude .claude/commands/"+name) {
			t.Fatalf("expected %s to be written; got: %q", name, out)
		}
	}
	for _, name := range copilotNames(allStems) {
		if !strings.Contains(out, "ok gen copilot .github/prompts/"+name) {
			t.Fatalf("expected %s to be written; got: %q", name, out)
		}
	}

	gotClaude := sortedCopy(dirEntryNames(t, filepath.Join(root, ".claude", "commands")))
	wantClaude := sortedCopy(claudeNames(allStems))
	if strings.Join(gotClaude, ",") != strings.Join(wantClaude, ",") {
		t.Fatalf(".claude/commands entries = %v, want exactly %v", gotClaude, wantClaude)
	}

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

		cp := filepath.Join(root, ".github", "prompts", copilotFilename("spectackle", spec.Name))
		cb, err := os.ReadFile(cp)
		if err != nil {
			t.Fatalf("%s: %v", cp, err)
		}
		if !strings.Contains(string(cb), "mode: agent") {
			t.Fatalf("%s missing mode: agent frontmatter:\n%s", cp, cb)
		}
		if !strings.Contains(string(cb), generatedHeader) {
			t.Fatalf("%s missing generated header:\n%s", cp, cb)
		}
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

// TestCommandsGenCommandsArgUnionsWithDefaults: commands=[find,get] adds
// just those two opt-in commands on top of the always-present default
// three — five files total, not two, and not eight.
func TestCommandsGenCommandsArgUnionsWithDefaults(t *testing.T) {
	root := t.TempDir()
	_, sess := connectCommands(t, root, nil)

	callText(t, sess, "commands", map[string]any{
		"op": "gen", "harness": []string{"claude"}, "commands": []string{"find", "get"},
	})

	want := sortedCopy(claudeNames(append(append([]string{}, defaultFileStems...), "-find", "-get")))
	got := sortedCopy(dirEntryNames(t, filepath.Join(root, ".claude", "commands")))
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf(".claude/commands entries = %v, want exactly %v", got, want)
	}
}

// TestCommandsGenUnknownCommandNameErrors: an unrecognized commands= entry
// is a validation error, not a silent no-op.
func TestCommandsGenUnknownCommandNameErrors(t *testing.T) {
	root := t.TempDir()
	_, sess := connectCommands(t, root, nil)

	out := callText(t, sess, "commands", map[string]any{
		"op": "gen", "harness": []string{"claude"}, "commands": []string{"bogus"},
	})
	if !strings.HasPrefix(out, "! ARG E") {
		t.Fatalf("expected an ARG error for an unknown command name; got: %q", out)
	}
}

// TestCommandsGenDefaultPreservesExistingExplorationFiles covers TEST 3: a
// default gen run over a directory that already has the six opt-in
// exploration files (from an earlier all=true run) must leave every one of
// them byte-for-byte intact — gen adds and overwrites, it has never
// deleted, and silently dropping a file a user has in git would be the
// worst possible behavior here.
func TestCommandsGenDefaultPreservesExistingExplorationFiles(t *testing.T) {
	root := t.TempDir()
	_, sess := connectCommands(t, root, nil)

	callText(t, sess, "commands", map[string]any{"op": "gen", "harness": []string{"claude"}, "all": true})

	before := map[string][]byte{}
	for _, name := range claudeNames(optInFileStems) {
		p := filepath.Join(root, ".claude", "commands", name)
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		before[name] = b
	}

	callText(t, sess, "commands", map[string]any{"op": "gen", "harness": []string{"claude"}})

	for _, name := range claudeNames(optInFileStems) {
		p := filepath.Join(root, ".claude", "commands", name)
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("%s vanished after a default gen run: %v", name, err)
		}
		if string(b) != string(before[name]) {
			t.Fatalf("%s changed by a default gen run that never asked for it:\nbefore:\n%s\nafter:\n%s", name, before[name], b)
		}
	}
	// the default three were still (re)written
	for _, name := range claudeNames(defaultFileStems) {
		if _, err := os.Stat(filepath.Join(root, ".claude", "commands", name)); err != nil {
			t.Fatalf("default command %s missing: %v", name, err)
		}
	}
}

// TestCommandsGenerateTemplateNamesUnlockedCommands covers TEST 4: the
// generate template renders non-empty, and — once run through a real gen
// (so the generated-header stamp that writeClaudeDialect adds is present) —
// names every one of the six commands it unlocks.
func TestCommandsGenerateTemplateNamesUnlockedCommands(t *testing.T) {
	data := commandsData{Binary: "spectackle", Tool: "spectackle", RepoURL: "https://example.com/spectackle"}
	body, err := renderCommandTemplate("generate.md.tmpl", data)
	if err != nil {
		t.Fatalf("render error: %v", err)
	}
	if strings.TrimSpace(body) == "" {
		t.Fatal("generate.md.tmpl rendered empty")
	}
	for _, name := range []string{"find", "get", "research", "swarm", "export", "merge"} {
		if !strings.Contains(body, "`"+name+"`") {
			t.Fatalf("generate.md.tmpl does not name unlocked command %q:\n%s", name, body)
		}
	}

	root := t.TempDir()
	_, sess := connectCommands(t, root, nil)
	callText(t, sess, "commands", map[string]any{"op": "gen", "harness": []string{"claude"}})
	gf, err := os.ReadFile(filepath.Join(root, ".claude", "commands", "spectackle-generate.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(gf), generatedHeader) {
		t.Fatalf("spectackle-generate.md missing generated header:\n%s", gf)
	}
}

// TestCommandsGenCodexTwiceIdempotent: gen harness=codex all=true writes
// exactly one managed section into AGENTS.md — one "## spectackle <heading>"
// subsection per commandSpec, in order, so all eight commands read as one
// coherent block rather than being appended blindly — and a second run must
// produce a byte-identical file (no duplicate sections, no drift).
func TestCommandsGenCodexTwiceIdempotent(t *testing.T) {
	root := t.TempDir()
	_, sess := connectCommands(t, root, nil)

	callText(t, sess, "commands", map[string]any{"op": "gen", "harness": []string{"codex"}, "all": true})
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

	callText(t, sess, "commands", map[string]any{"op": "gen", "harness": []string{"codex"}, "all": true})
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

// TestCommandsGenAgentsDefaultPreservesOptInSections: the AGENTS.md managed
// block is rewritten wholesale each run (unlike the per-file dialects), so
// naively regenerating it from only this run's specs would silently delete
// any opt-in command's subsection an earlier all=true run had written. A
// default run after an all=true run must instead carry those five sections
// forward unchanged while still refreshing the three default ones.
func TestCommandsGenAgentsDefaultPreservesOptInSections(t *testing.T) {
	root := t.TempDir()
	_, sess := connectCommands(t, root, nil)

	callText(t, sess, "commands", map[string]any{"op": "gen", "harness": []string{"codex"}, "all": true})
	callText(t, sess, "commands", map[string]any{"op": "gen", "harness": []string{"codex"}})

	got, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(got), sectionBegin) != 1 {
		t.Fatalf("expected exactly one managed section:\n%s", got)
	}
	for _, spec := range commandSpecs {
		heading := "## spectackle " + spec.Heading
		if !strings.Contains(string(got), heading) {
			t.Fatalf("a default gen run dropped opt-in heading %q that an earlier all=true run wrote:\n%s", heading, got)
		}
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

	out := callText(t, sess, "commands", map[string]any{"op": "gen", "harness": []string{"claude", "copilot", "codex"}, "all": true})
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

// ---- install-hooks (T-0126, MCP-015) ----
//
// TestCommandsInstallHooks{TemplateRendersWithHeader,
// WritesExecutableFileAtCommonDir, Idempotent, RefusesForeignHook} are the
// four generator-level tests the exit criterion asks for.
// TestPreCommitHook{RejectsSpectacklePathInLinkedWorktree,
// AllowsCodeOnlyCommitInLinkedWorktree, AllowsSpectacklePathInMainWorktree}
// drive the generated script directly with `sh` against a real git
// worktree pair — the three script-behavior cases.

// TestCommandsInstallHooksTemplateRendersWithHeader covers TEST 1: the hook
// template renders, is non-empty, starts with a POSIX sh shebang (not
// bash), and carries the do-not-edit header install-hooks checks for on
// every subsequent run.
func TestCommandsInstallHooksTemplateRendersWithHeader(t *testing.T) {
	body, err := renderHookTemplate("pre-commit.tmpl", commandsData{Binary: "spectackle", Tool: "spectackle"})
	if err != nil {
		t.Fatalf("render error: %v", err)
	}
	if strings.TrimSpace(body) == "" {
		t.Fatal("pre-commit.tmpl rendered empty")
	}
	if !strings.HasPrefix(body, "#!/bin/sh") {
		t.Fatalf("pre-commit.tmpl must open with a POSIX sh shebang, not bash:\n%s", body)
	}
	if !strings.Contains(body, hookGeneratedHeader) {
		t.Fatalf("pre-commit.tmpl missing the generated do-not-edit header:\n%s", body)
	}
}

// TestCommandsInstallHooksWritesExecutableFileAtCommonDir covers TEST 2:
// installing writes hooks/pre-commit under the repository's
// git-common-dir, mode 0o755 — a non-executable hook is silently ignored
// by git, so the mode is asserted directly rather than just "the file
// exists".
func TestCommandsInstallHooksWritesExecutableFileAtCommonDir(t *testing.T) {
	root := gitRoot(t)
	_, sess := connectCommands(t, root, nil)

	out := callText(t, sess, "commands", map[string]any{"op": "install-hooks"})
	if !strings.HasPrefix(out, "ok hooks pre-commit ") {
		t.Fatalf("expected an ok record; got: %q", out)
	}

	commonDir, err := gitCommonDir(root)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(commonDir, "hooks", "pre-commit")
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("hook not written at the common-dir hooks path: %v", err)
	}
	if fi.Mode().Perm() != 0o755 {
		t.Fatalf("hook mode = %o, want 0755 (a non-executable hook is silently ignored by git)", fi.Mode().Perm())
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), hookGeneratedHeader) {
		t.Fatalf("installed hook missing generated header:\n%s", b)
	}
}

// TestCommandsInstallHooksIdempotent covers TEST 3: installing twice
// produces byte-identical content and keeps mode 0o755 — no drift, no
// duplicated markers, no error on the second run.
func TestCommandsInstallHooksIdempotent(t *testing.T) {
	root := gitRoot(t)
	_, sess := connectCommands(t, root, nil)

	callText(t, sess, "commands", map[string]any{"op": "install-hooks"})
	commonDir, err := gitCommonDir(root)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(commonDir, "hooks", "pre-commit")
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	out := callText(t, sess, "commands", map[string]any{"op": "install-hooks"})
	if !strings.HasPrefix(out, "ok hooks pre-commit ") {
		t.Fatalf("second install-hooks call should still succeed; got: %q", out)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("second install-hooks run not byte-identical:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o755 {
		t.Fatalf("hook mode after second run = %o, want 0755", fi.Mode().Perm())
	}
}

// TestCommandsInstallHooksRefusesForeignHook covers TEST 4: a pre-existing
// hooks/pre-commit that does NOT carry the generated marker is the repo
// owner's — install-hooks must refuse rather than clobber it, and the
// original bytes must survive untouched.
func TestCommandsInstallHooksRefusesForeignHook(t *testing.T) {
	root := gitRoot(t)
	commonDir, err := gitCommonDir(root)
	if err != nil {
		t.Fatal(err)
	}
	hooksDir := filepath.Join(commonDir, "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(hooksDir, "pre-commit")
	foreign := "#!/bin/sh\necho custom hook not written by spectackle\nexit 0\n"
	if err := os.WriteFile(path, []byte(foreign), 0o755); err != nil {
		t.Fatal(err)
	}

	_, sess := connectCommands(t, root, nil)
	out := callText(t, sess, "commands", map[string]any{"op": "install-hooks"})
	if !strings.HasPrefix(out, "! ARG E") {
		t.Fatalf("expected a refusal for a foreign pre-commit hook without the generated marker; got: %q", out)
	}

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != foreign {
		t.Fatalf("foreign hook was modified despite the refusal:\nbefore:\n%s\nafter:\n%s", foreign, b)
	}
}

// writePreCommitScript renders pre-commit.tmpl and writes it to a fresh
// executable file under t.TempDir() — used by the script-behavior tests
// below, which drive the hook directly with `sh` (proving MCP-015's
// staged-path logic itself) rather than through install-hooks (which the
// four tests above already cover).
func writePreCommitScript(t *testing.T) string {
	t.Helper()
	body, err := renderHookTemplate("pre-commit.tmpl", commandsData{Binary: "spectackle", Tool: "spectackle"})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "pre-commit")
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// runPreCommitHook runs script with dir as its working directory (the same
// way git invokes hooks/pre-commit — cwd at the worktree root) and returns
// its exit code plus combined stdout+stderr.
func runPreCommitHook(t *testing.T, script, dir string) (int, string) {
	t.Helper()
	cmd := exec.Command("sh", script)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err == nil {
		return 0, string(out)
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), string(out)
	}
	t.Fatalf("running hook: %v (output: %s)", err, out)
	return -1, ""
}

// gitAdd stages path (relative to dir) in the git repo at dir, failing the
// test on error.
func gitAdd(t *testing.T, dir, path string) {
	t.Helper()
	if out, err := exec.Command("git", "-C", dir, "add", path).CombinedOutput(); err != nil {
		t.Fatalf("git add %s: %v (%s)", path, err, out)
	}
}

// TestPreCommitHookRejectsSpectacklePathInLinkedWorktree drives the
// generated script directly: a linked worktree with a staged `.spectackle`
// path must be rejected (exit != 0) naming the offending path.
func TestPreCommitHookRejectsSpectacklePathInLinkedWorktree(t *testing.T) {
	main := gitRoot(t)
	wtRoot := filepath.Join(t.TempDir(), "wt")
	if err := wt.Add(main, wtRoot, "impl/test-mcp015-reject", "HEAD"); err != nil {
		t.Fatalf("wt.Add: %v", err)
	}

	cfgPath := filepath.Join(wtRoot, ".spectackle", "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("schema: v0\nverify: [\"test -f ok.txt\"]\nextra: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitAdd(t, wtRoot, ".spectackle/config.yaml")

	script := writePreCommitScript(t)
	code, out := runPreCommitHook(t, script, wtRoot)
	if code == 0 {
		t.Fatalf("expected a non-zero exit for a staged .spectackle path in a linked worktree; output: %s", out)
	}
	if !strings.Contains(out, ".spectackle/config.yaml") {
		t.Fatalf("expected the offending path named in the output; got: %s", out)
	}
}

// TestPreCommitHookAllowsCodeOnlyCommitInLinkedWorktree drives the
// generated script directly: a linked worktree with only a code file
// staged must pass (exit 0) — the sanctioned `work op=submit` flow commits
// code in a worktree, and this must never be blocked.
func TestPreCommitHookAllowsCodeOnlyCommitInLinkedWorktree(t *testing.T) {
	main := gitRoot(t)
	wtRoot := filepath.Join(t.TempDir(), "wt")
	if err := wt.Add(main, wtRoot, "impl/test-mcp015-code-only", "HEAD"); err != nil {
		t.Fatalf("wt.Add: %v", err)
	}

	if err := os.WriteFile(filepath.Join(wtRoot, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitAdd(t, wtRoot, "main.go")

	script := writePreCommitScript(t)
	code, out := runPreCommitHook(t, script, wtRoot)
	if code != 0 {
		t.Fatalf("a code-only commit in a linked worktree must pass; exit %d, output: %s", code, out)
	}
}

// TestPreCommitHookAllowsSpectacklePathInMainWorktree drives the generated
// script directly: a `.spectackle` path staged in the MAIN checkout (not a
// linked worktree) must pass — the rule is about worktree branches, not the
// main checkout, where .spectackle commits are normal and expected.
func TestPreCommitHookAllowsSpectacklePathInMainWorktree(t *testing.T) {
	main := gitRoot(t)

	cfgPath := filepath.Join(main, ".spectackle", "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("schema: v0\nverify: [\"test -f ok.txt\"]\nextra: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitAdd(t, main, ".spectackle/config.yaml")

	script := writePreCommitScript(t)
	code, out := runPreCommitHook(t, script, main)
	if code != 0 {
		t.Fatalf("a .spectackle path staged in the MAIN checkout must be allowed; exit %d, output: %s", code, out)
	}
}
