package workspace

import (
	"os"
	"path/filepath"
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
