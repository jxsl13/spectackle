package workspace

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestDetect(t *testing.T) {
	root := t.TempDir()
	mk := func(rel string, content string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// marker: config.yaml at root, nested .spectackle must NOT win
	mk(".spectackle/config.yaml", "schema: v0\n")
	mk("sub/deep/.spectackle/spec.md", "---\nschema: v0\n---\n")

	ws, err := Detect(filepath.Join(root, "sub", "deep"), "")
	if err != nil {
		t.Fatal(err)
	}
	if ws.Dir != root {
		t.Fatalf("Detect from nested dir = %s, want %s (nested .spectackle must not shadow the root marker)", ws.Dir, root)
	}

	// git fallback: no config.yaml anywhere
	root2 := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root2, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root2, "a", "b"), 0o755); err != nil {
		t.Fatal(err)
	}
	ws2, err := Detect(filepath.Join(root2, "a", "b"), "")
	if err != nil {
		t.Fatal(err)
	}
	if ws2.Dir != root2 {
		t.Fatalf("git fallback = %s, want %s", ws2.Dir, root2)
	}

	// flag fallback: bare dir
	root3 := t.TempDir()
	ws3, err := Detect(root3, root3)
	if err != nil {
		t.Fatal(err)
	}
	if ws3.Dir != root3 {
		t.Fatalf("flag fallback = %s, want %s", ws3.Dir, root3)
	}
}

func TestEnsureScaffoldAndContextDirs(t *testing.T) {
	root := t.TempDir()
	ws := Root{Dir: root, Cfg: defaultConfig()}
	if err := ws.EnsureScaffold(""); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{".gitignore", ".gitattributes", "config.yaml"} {
		if !fileExists(filepath.Join(root, Dot, f)) {
			t.Errorf("root scaffold missing %s", f)
		}
	}
	if err := ws.EnsureScaffold("gpu/kernels"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ws.WorkPath("gpu/kernels"), []byte("---\nschema: v0\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ws.SpecPath(""), []byte("---\nschema: v0\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctxs, err := ws.ContextDirs()
	if err != nil {
		t.Fatal(err)
	}
	if len(ctxs) != 2 || ctxs[0] != "" || ctxs[1] != "gpu/kernels" {
		t.Fatalf("ContextDirs = %v", ctxs)
	}
	if got := NearestContext(ctxs, "gpu/kernels/saxpy.cu"); got != "gpu/kernels" {
		t.Fatalf("NearestContext = %q", got)
	}
	if got := NearestContext(ctxs, "pkg/util.go"); got != "" {
		t.Fatalf("NearestContext fallback = %q", got)
	}

	// A bundle nested under any subdirectory that is itself a separate git
	// boundary (a linked worktree, submodule, or nested clone — signaled by
	// its own .git entry, file or dir) is agent/tooling worktree state, not
	// project spec content — ContextDirs must skip the whole subtree. This is
	// the generic, harness-independent mechanism that replaced a hardcoded
	// '.claude' name check: no literal '.claude' appears anywhere below.
	gitFileBundle := filepath.Join(root, "tmp", "worktrees", "x", ".spectackle")
	if err := os.MkdirAll(gitFileBundle, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitFileBundle, "spec.md"), []byte("---\nschema: v0\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "tmp", "worktrees", "x", ".git"),
		[]byte("gitdir: /elsewhere/.git/worktrees/x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctxs2, err := ws.ContextDirs()
	if err != nil {
		t.Fatal(err)
	}
	if len(ctxs2) != 2 || ctxs2[0] != "" || ctxs2[1] != "gpu/kernels" {
		t.Fatalf("ContextDirs after .git-FILE worktree bundle = %v, want unchanged %v", ctxs2, ctxs)
	}

	// same, but the nested boundary is a .git DIRECTORY (a full nested clone
	// rather than a linked worktree) — also skipped.
	gitDirBundle := filepath.Join(root, "vendored-repo", ".spectackle")
	if err := os.MkdirAll(gitDirBundle, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDirBundle, "spec.md"), []byte("---\nschema: v0\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "vendored-repo", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	ctxs3, err := ws.ContextDirs()
	if err != nil {
		t.Fatal(err)
	}
	if len(ctxs3) != 2 || ctxs3[0] != "" || ctxs3[1] != "gpu/kernels" {
		t.Fatalf("ContextDirs after .git-DIR nested clone bundle = %v, want unchanged %v", ctxs3, ctxs)
	}
}

// TestSkipDirConfigIgnore proves Root.SkipDir — the single shared entry point
// behind ContextDirs, spec.Load, the coverage-gap walk, and (via
// DefaultSkipName/IsNestedGitBoundary) the indexer — honors both
// user-extensible ignore mechanisms in config.yaml: Ignore globs and the new
// IgnoreRegex RE2 patterns, on top of the built-in defaults and the
// nested-git-boundary check.
func TestSkipDirConfigIgnore(t *testing.T) {
	root := t.TempDir()
	ws := Root{Dir: root, Cfg: Config{
		Ignore:      []string{"generated/**"},
		IgnoreRegex: []string{`^vendor-[a-z]+$`},
	}}

	cases := []struct {
		name string
		rel  string
		dir  string
		want bool
	}{
		{"built-in default name", "node_modules", "node_modules", true},
		{"configured glob", "generated", "generated", true},
		{"configured glob, nested dir", "generated/sub", "sub", true},
		{"configured regex", "vendor-acme", "vendor-acme", true},
		{"regex must match whole rel, not a substring of an unrelated dir", "src/vendor-acme-extra", "vendor-acme-extra", false},
		{"ordinary dir", "pkg", "pkg", false},
	}
	for _, tc := range cases {
		if got := ws.SkipDir(tc.rel, tc.dir); got != tc.want {
			t.Errorf("%s: SkipDir(%q, %q) = %v, want %v", tc.name, tc.rel, tc.dir, got, tc.want)
		}
	}

	// end-to-end through ContextDirs: a glob-ignored dir and a regex-ignored
	// dir both hide their .spectackle bundles from discovery.
	mk := func(rel string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(p, "spec.md"), []byte("---\nschema: v0\n---\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mk("generated/.spectackle")
	mk("vendor-acme/.spectackle")
	mk(".spectackle") // the one bundle that must survive

	ctxs, err := ws.ContextDirs()
	if err != nil {
		t.Fatal(err)
	}
	if len(ctxs) != 1 || ctxs[0] != "" {
		t.Fatalf("ContextDirs = %v, want only the root bundle (glob/regex ignores must prune generated/ and vendor-acme/)", ctxs)
	}
}

func TestFeedbackConfigDefaults(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, Dot), 0o755); err != nil {
		t.Fatal(err)
	}

	// no config.yaml at all -> defaultConfig()'s default applies
	ws, err := Detect(root, root)
	if err != nil {
		t.Fatal(err)
	}
	if ws.Cfg.Feedback.MaxRounds != 3 {
		t.Fatalf("default MaxRounds = %d, want 3", ws.Cfg.Feedback.MaxRounds)
	}

	// explicit feedback.max_rounds: 0 (zero-block) still defaults to 3
	if err := os.WriteFile(filepath.Join(root, Dot, "config.yaml"),
		[]byte("schema: v0\nfeedback:\n  max_rounds: 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ws2, err := Detect(root, root)
	if err != nil {
		t.Fatal(err)
	}
	if ws2.Cfg.Feedback.MaxRounds != 3 {
		t.Fatalf("zero-block MaxRounds = %d, want 3", ws2.Cfg.Feedback.MaxRounds)
	}

	// explicit non-zero value and grill command are respected
	if err := os.WriteFile(filepath.Join(root, Dot, "config.yaml"),
		[]byte("schema: v0\nfeedback:\n  max_rounds: 5\n  grill: \"go vet ./...\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ws3, err := Detect(root, root)
	if err != nil {
		t.Fatal(err)
	}
	if ws3.Cfg.Feedback.MaxRounds != 5 || ws3.Cfg.Feedback.Grill != "go vet ./..." {
		t.Fatalf("explicit feedback cfg = %+v", ws3.Cfg.Feedback)
	}
}

// TestCompactConfigDefaults locks the T-0093 threshold change (500 -> 300)
// at both defaulting sites: defaultConfig() itself and load()'s zero-block
// fallback for an explicit `journal_max: 0` — while an explicit non-zero
// value is still respected verbatim.
func TestCompactConfigDefaults(t *testing.T) {
	if got := defaultConfig().Compact.JournalMax; got != 300 {
		t.Fatalf("defaultConfig().Compact.JournalMax = %d, want 300", got)
	}

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, Dot), 0o755); err != nil {
		t.Fatal(err)
	}

	// no config.yaml at all -> defaultConfig()'s default applies
	ws, err := Detect(root, root)
	if err != nil {
		t.Fatal(err)
	}
	if ws.Cfg.Compact.JournalMax != 300 {
		t.Fatalf("default JournalMax = %d, want 300", ws.Cfg.Compact.JournalMax)
	}

	// explicit compact.journal_max: 0 (zero-block) still defaults to 300
	if err := os.WriteFile(filepath.Join(root, Dot, "config.yaml"),
		[]byte("schema: v0\ncompact:\n  journal_max: 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ws2, err := Detect(root, root)
	if err != nil {
		t.Fatal(err)
	}
	if ws2.Cfg.Compact.JournalMax != 300 {
		t.Fatalf("zero-block JournalMax = %d, want 300", ws2.Cfg.Compact.JournalMax)
	}

	// explicit non-zero value is respected
	if err := os.WriteFile(filepath.Join(root, Dot, "config.yaml"),
		[]byte("schema: v0\ncompact:\n  journal_max: 42\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ws3, err := Detect(root, root)
	if err != nil {
		t.Fatal(err)
	}
	if ws3.Cfg.Compact.JournalMax != 42 {
		t.Fatalf("explicit JournalMax = %d, want 42", ws3.Cfg.Compact.JournalMax)
	}
}

// TestEnsureScaffoldGeneratesSelfDocumentingConfig proves a NEWLY created
// config.yaml (a) documents every setting with its default value and a
// trailing comment, and (b) parses back through load() into a Config equal
// to defaultConfig(), field by field. Slice fields whose zero value is a nil
// slice (ignore_regex, verify) are compared by emptiness rather than
// pointer-identity-sensitive reflect equality, since an explicit `[]`
// literal round-trips to a non-nil empty slice while an omitted key stays
// nil — both mean "no entries configured" and must be treated as equal here.
func TestEnsureScaffoldGeneratesSelfDocumentingConfig(t *testing.T) {
	root := t.TempDir()
	ws := Root{Dir: root, Cfg: defaultConfig()}
	if err := ws.EnsureScaffold(""); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(root, Dot, "config.yaml")
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)

	// every documented setting appears, each with a trailing comment
	// (top-level scalar/list-header keys only — nested keys are checked via
	// their own "key: value  # ..." lines below).
	for _, key := range []string{
		"schema:", "langs:", "ignore:", "ignore_regex:", "budget_default:",
		"journal_max:", "done_max:", "lease_ttl:", "agent_ttl:",
		"max_rounds:", "grill:", "worktrees_dir:",
	} {
		idx := strings.Index(text, key)
		if idx < 0 {
			t.Fatalf("generated config.yaml missing documented key %q:\n%s", key, text)
		}
		line := text[idx:]
		if nl := strings.IndexByte(line, '\n'); nl >= 0 {
			line = line[:nl]
		}
		if !strings.Contains(line, "#") {
			t.Errorf("key %q line has no trailing comment: %q", key, line)
		}
	}
	// verify is intentionally undocumented (no meaningful default — see
	// scaffoldConfigYAML's doc comment)
	if strings.Contains(text, "verify:") {
		t.Errorf("verify should not appear in the generated template (no default to document):\n%s", text)
	}

	reloaded, err := Detect(root, root)
	if err != nil {
		t.Fatal(err)
	}
	got, want := reloaded.Cfg, defaultConfig()

	if got.Schema != want.Schema {
		t.Errorf("Schema = %q, want %q", got.Schema, want.Schema)
	}
	if !slices.Equal(got.Langs, want.Langs) {
		t.Errorf("Langs = %v, want %v", got.Langs, want.Langs)
	}
	if !slices.Equal(got.Ignore, want.Ignore) {
		t.Errorf("Ignore = %v, want %v", got.Ignore, want.Ignore)
	}
	if len(got.IgnoreRegex) != 0 || len(want.IgnoreRegex) != 0 {
		t.Errorf("IgnoreRegex = %v, want empty (both sides)", got.IgnoreRegex)
	}
	if got.BudgetDefault != want.BudgetDefault {
		t.Errorf("BudgetDefault = %d, want %d", got.BudgetDefault, want.BudgetDefault)
	}
	if got.Compact != want.Compact {
		t.Errorf("Compact = %+v, want %+v", got.Compact, want.Compact)
	}
	if got.Swarm != want.Swarm {
		t.Errorf("Swarm = %+v, want %+v", got.Swarm, want.Swarm)
	}
	if len(got.Verify) != 0 || len(want.Verify) != 0 {
		t.Errorf("Verify = %v, want empty (both sides)", got.Verify)
	}
	if got.WorktreesDir != want.WorktreesDir {
		t.Errorf("WorktreesDir = %q, want %q", got.WorktreesDir, want.WorktreesDir)
	}
	if got.Feedback != want.Feedback {
		t.Errorf("Feedback = %+v, want %+v", got.Feedback, want.Feedback)
	}
}

// TestEnsureScaffoldNeverRewritesExistingConfig is the write-once guarantee:
// a pre-existing config.yaml (however it got there — an older scaffold, a
// hand-tuned one) must never be regenerated or otherwise touched by a later
// EnsureScaffold call.
func TestEnsureScaffoldNeverRewritesExistingConfig(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, Dot), 0o755); err != nil {
		t.Fatal(err)
	}
	custom := "schema: v0\nlangs: [rust]\ncompact:\n  journal_max: 77\n"
	cfgPath := filepath.Join(root, Dot, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}

	ws := Root{Dir: root, Cfg: defaultConfig()}
	if err := ws.EnsureScaffold(""); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != custom {
		t.Fatalf("EnsureScaffold rewrote an existing config.yaml:\ngot:  %q\nwant: %q", raw, custom)
	}
}
