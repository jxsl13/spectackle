package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jxsl13/spectackle/internal/migrate"
	"github.com/jxsl13/spectackle/internal/wt"
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
	mk(".spectackle/config.yaml", "schema: v1\n")
	mk("sub/deep/.spectackle/spec.md", "---\nschema: v1\n---\n")

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

	// flag fallback: bare dir. No git anywhere above (t.TempDir() is not
	// nested in a repo) — the workspace must anchor at the given directory
	// rather than walking all the way up to the filesystem root (issue 27's
	// second probe).
	root3 := t.TempDir()
	ws3, err := Detect(root3, root3)
	if err != nil {
		t.Fatal(err)
	}
	if ws3.Dir != root3 {
		t.Fatalf("flag fallback = %s, want %s", ws3.Dir, root3)
	}
}

// runGit runs git in dir with a fixed identity, matching internal/wt's own
// test fixtures, so these tests never depend on the host's git config. It
// skips the calling test (not fails it) when git itself is unavailable,
// matching internal/wt.InitTestRepo's convention.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	full := append([]string{"-C", dir,
		"-c", "user.name=spectackle", "-c", "user.email=spectackle@localhost"}, args...)
	if out, err := exec.Command("git", full...).CombinedOutput(); err != nil {
		if _, lookErr := exec.LookPath("git"); lookErr != nil {
			t.Skipf("git unavailable: %v", lookErr)
		}
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// initRepo git-inits dir and immediately silences its background
// maintenance (B-01KYQA4WXEFAT). Every fixture here commits into a
// t.TempDir, and a default repository answers each commit with a DETACHED
// `git maintenance run --auto` child that outlives the command — a process
// still holding .git/objects open while the harness unlinks it, which
// surfaces as a `TempDir RemoveAll cleanup: ... directory not empty` failure
// attached to whichever test happened to be running.
//
// The suppression belongs HERE and not in runGit, which runs per command:
// the keys are repo-local and only need setting once, right after init.
// extra carries whatever init flags the fixture needs (`-b main`, `-b
// trunk`), because the branch name is the one thing these fixtures disagree
// about.
func initRepo(t *testing.T, dir string, extra ...string) {
	t.Helper()
	runGit(t, dir, append([]string{"init", "-q"}, extra...)...)
	if err := wt.QuietMaintenance(dir); err != nil {
		t.Fatalf("QuietMaintenance: %v", err)
	}
}

// TestDetectNestedGitWorktreeIsOwnRoot is GitHub issue 27: a linked git
// worktree's .git is a FILE (a "gitdir: ..." pointer), not a directory.
// Detect's git-boundary walk used to test only dirExists(.git), so from
// inside such a worktree it skipped straight past that file and kept
// climbing until it hit the main checkout's real .git directory — an
// explicit -root naming the worktree therefore silently resolved to the
// enclosing checkout instead, and every bundle write landed there.
func TestDetectNestedGitWorktreeIsOwnRoot(t *testing.T) {
	main := t.TempDir()
	initRepo(t, main, "-b", "main")
	runGit(t, main, "commit", "-q", "--allow-empty", "-m", "init")
	wtDir := filepath.Join(main, "wt", "feature")
	runGit(t, main, "worktree", "add", "-q", wtDir, "--detach", "HEAD")

	ws, err := Detect(wtDir, wtDir)
	if err != nil {
		t.Fatal(err)
	}
	if ws.Dir != wtDir {
		t.Fatalf("Detect(nested worktree) = %s, want %s (a .git FILE must terminate the walk exactly like a .git dir)", ws.Dir, wtDir)
	}

	// the write lands in the worktree's OWN bundle...
	if err := ws.EnsureScaffold(""); err != nil {
		t.Fatal(err)
	}
	if !fileExists(filepath.Join(wtDir, Dot, "config.yaml")) {
		t.Fatal("worktree did not get its own .spectackle bundle")
	}
	// ...and the enclosing main checkout's bundle is untouched.
	if _, err := os.Stat(filepath.Join(main, Dot)); err == nil {
		t.Fatal("the enclosing main checkout's .spectackle must not have been written to")
	}
}

// TestDetectWorktreeOutsideMainCheckoutStillWorks is the field report's third
// probe: a worktree placed OUTSIDE the main checkout already got its own
// bundle before this fix (there is no enclosing .git directory to find), and
// must keep doing so — this guards against a fix to the nested case that
// accidentally widens the walk and starts skipping non-nested worktrees too.
func TestDetectWorktreeOutsideMainCheckoutStillWorks(t *testing.T) {
	main := t.TempDir()
	initRepo(t, main, "-b", "main")
	runGit(t, main, "commit", "-q", "--allow-empty", "-m", "init")
	wtDir := filepath.Join(t.TempDir(), "feature")
	runGit(t, main, "worktree", "add", "-q", wtDir, "--detach", "HEAD")

	ws, err := Detect(wtDir, wtDir)
	if err != nil {
		t.Fatal(err)
	}
	if ws.Dir != wtDir {
		t.Fatalf("Detect(worktree outside main checkout) = %s, want %s", ws.Dir, wtDir)
	}
}

// TestDetectRootFlagTargetsNamedRepo is the field report's first probe: an
// explicit root naming a different repository must target that repository,
// not the one detection would otherwise land in — the flag was never
// ignored, and this fix must not change that.
func TestDetectRootFlagTargetsNamedRepo(t *testing.T) {
	repoA := t.TempDir()
	repoB := t.TempDir()
	initRepo(t, repoA, "-b", "main")
	initRepo(t, repoB, "-b", "main")

	ws, err := Detect(repoA, repoA)
	if err != nil {
		t.Fatal(err)
	}
	if ws.Dir != repoA {
		t.Fatalf("Detect(-root repoA) = %s, want %s (must target the named repo, never repoB)", ws.Dir, repoA)
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
	if err := os.WriteFile(ws.WorkPath("gpu/kernels"), []byte("---\nschema: v1\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ws.SpecPath(""), []byte("---\nschema: v1\n---\n"), 0o644); err != nil {
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
	if err := os.WriteFile(filepath.Join(gitFileBundle, "spec.md"), []byte("---\nschema: v1\n---\n"), 0o644); err != nil {
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
	if err := os.WriteFile(filepath.Join(gitDirBundle, "spec.md"), []byte("---\nschema: v1\n---\n"), 0o644); err != nil {
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
		if err := os.WriteFile(filepath.Join(p, "spec.md"), []byte("---\nschema: v1\n---\n"), 0o644); err != nil {
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

// initGitRepo creates a real git repo at root or skips the test — the
// git-ignore layer of SkipDir (internal/ignore) only has anything to prove
// against an actual git checkout.
func initGitRepo(t *testing.T, root string) {
	t.Helper()
	if out, err := exec.Command("git", "init", "-q", root).CombinedOutput(); err != nil {
		t.Skipf("git unavailable: %v: %s", err, out)
	}
	// Init-only today, but disabled anyway — see initRepo (B-01KYQA4WXEFAT).
	if err := wt.QuietMaintenance(root); err != nil {
		t.Fatalf("QuietMaintenance: %v", err)
	}
}

// walkFiles mirrors ContextDirs' own walk (WalkDir + SkipDir at every
// directory) but collects plain files instead of .spectackle bundles, so it
// can stand in for "what would the indexer see" without reaching into
// internal/index, which is out of scope for this change.
func walkFiles(t *testing.T, ws Root) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(ws.Dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(ws.Dir, p)
		rel = filepath.ToSlash(rel)
		if rel == "." {
			rel = ""
		}
		if d.IsDir() {
			if rel != "" && ws.SkipDir(rel, d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		out = append(out, rel)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// TestSkipDirGitIgnoreDuplicateSymbol reproduces the issue 26 field report at
// the level this change owns: a gitignored directory holding a same-named
// copy of a file disappears from the walk entirely, leaving only the real,
// non-ignored copy reachable. (Which copy of a duplicate symbol keeps the
// unsuffixed node ID is decided downstream in internal/index's disambiguate,
// out of scope here — but it can only ever see the one copy this walk hands
// it.)
func TestSkipDirGitIgnoreDuplicateSymbol(t *testing.T) {
	root := t.TempDir()
	initGitRepo(t, root)
	mk := func(rel, content string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mk(".gitignore", ".venv/\n")
	// same basename, same content, one copy inside a gitignored virtualenv —
	// the exact shape of the collision the field report measured.
	mk(".venv/pkg/mod.go", "package pkg\nfunc Foo() {}\n")
	mk("pkg/mod.go", "package pkg\nfunc Foo() {}\n")

	ws, err := LoadRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	files := walkFiles(t, ws)

	if !slices.Contains(files, "pkg/mod.go") {
		t.Errorf("walk = %v, want it to contain pkg/mod.go (the real, non-ignored copy)", files)
	}
	if slices.Contains(files, ".venv/pkg/mod.go") {
		t.Errorf("walk = %v, must NOT contain .venv/pkg/mod.go (gitignored)", files)
	}
}

// TestSkipDirGitIgnoreNoGitRepo proves the required degradation: a
// workspace with no git repository at all must walk exactly as it did
// before internal/ignore existed — every path visited, nothing pruned by
// the new check.
func TestSkipDirGitIgnoreNoGitRepo(t *testing.T) {
	root := t.TempDir() // deliberately never git-init'd
	p := filepath.Join(root, "anydir", "keep.txt")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	ws, err := LoadRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	if ws.SkipDir("anydir", "anydir") {
		t.Fatal("SkipDir pruned a directory in a non-git workspace")
	}
	if files := walkFiles(t, ws); !slices.Contains(files, "anydir/keep.txt") {
		t.Errorf("walk = %v, want anydir/keep.txt reachable", files)
	}
}

// TestSkipDirGitIgnoreConfigPrecedence proves the config.yaml ignore knobs
// still work, and still run ahead of the git-backed check, once both are in
// play side by side: a glob-ignored dir that git knows nothing about is
// still pruned, and a git-ignored dir that config knows nothing about is
// also pruned — neither mechanism shadows the other.
func TestSkipDirGitIgnoreConfigPrecedence(t *testing.T) {
	root := t.TempDir()
	initGitRepo(t, root)
	mk := func(rel, content string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mk(".gitignore", ".venv/\n") // git-only ignore, config knows nothing of it
	mk(".venv/x.go", "package x\n")
	mk("generated/x.go", "package x\n") // config-only ignore, git knows nothing of it
	mk("kept/x.go", "package x\n")

	ws, err := LoadRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	ws.Cfg.Ignore = append(ws.Cfg.Ignore, "generated/**")

	files := walkFiles(t, ws)
	if slices.Contains(files, ".venv/x.go") {
		t.Errorf("walk = %v, must NOT contain .venv/x.go (git-ignored)", files)
	}
	if slices.Contains(files, "generated/x.go") {
		t.Errorf("walk = %v, must NOT contain generated/x.go (config-ignored)", files)
	}
	if !slices.Contains(files, "kept/x.go") {
		t.Errorf("walk = %v, want kept/x.go reachable", files)
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
		[]byte("schema: v1\nfeedback:\n  max_rounds: 0\n"), 0o644); err != nil {
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
		[]byte("schema: v1\nfeedback:\n  max_rounds: 5\n  grill: \"go vet ./...\"\n"), 0o644); err != nil {
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
		[]byte("schema: v1\ncompact:\n  journal_max: 0\n"), 0o644); err != nil {
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
		[]byte("schema: v1\ncompact:\n  journal_max: 42\n"), 0o644); err != nil {
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

// TestGitConfigDefaults proves the opt-out shape of Config.Git: a config.yaml
// that omits the `git` block entirely (which includes every workspace
// scaffolded before this field existed) must resolve to enabled, online,
// origin — the same as if the block were present and empty. An opt-out
// feature whose zero value silently opted out would be worse than no feature
// at all.
func TestGitConfigDefaults(t *testing.T) {
	root := t.TempDir()
	initRepo(t, root, "-b", "main")
	runGit(t, root, "commit", "-q", "--allow-empty", "-m", "init")
	if err := os.MkdirAll(filepath.Join(root, Dot), 0o755); err != nil {
		t.Fatal(err)
	}
	ws, err := Detect(root, root)
	if err != nil {
		t.Fatal(err)
	}
	if !ws.Cfg.Git.IsEnabled() {
		t.Fatal("Git.IsEnabled() = false, want true (opt-out default, git block omitted)")
	}
	// GIT-DEFAULT-001 (user decision 2026-07-27): an absent mode key means
	// OFFLINE — commit-only lifecycle edges; online is the explicit opt-in.
	if ws.Cfg.Git.Mode != "offline" {
		t.Fatalf("Git.Mode = %q, want %q", ws.Cfg.Git.Mode, "offline")
	}
	if ws.Cfg.Git.Remote != "origin" {
		t.Fatalf("Git.Remote = %q, want %q", ws.Cfg.Git.Remote, "origin")
	}
}

// TestGitConfigExplicitOnlineHonored: the opt-in the default flip demands —
// `git: mode: online` must survive load untouched.
func TestGitConfigExplicitOnlineHonored(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, Dot), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, Dot, "config.yaml"),
		[]byte("schema: v1\ngit:\n  mode: online\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ws, err := Detect(root, root)
	if err != nil {
		t.Fatal(err)
	}
	if ws.Cfg.Git.Mode != "online" {
		t.Fatalf("Git.Mode = %q, want %q", ws.Cfg.Git.Mode, "online")
	}
}

// TestGitConfigExplicitDisableHonored proves the one thing a plain bool
// couldn't: an explicit `enabled: false` must stick, not get overridden back
// to the opt-out default the way a zero-block int default would.
func TestGitConfigExplicitDisableHonored(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, Dot), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, Dot, "config.yaml"),
		[]byte("schema: v1\ngit:\n  enabled: false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ws, err := Detect(root, root)
	if err != nil {
		t.Fatal(err)
	}
	if ws.Cfg.Git.IsEnabled() {
		t.Fatal("Git.IsEnabled() = true, want false — an explicit disable must be honored")
	}
}

// TestGitConfigUnknownModeRejectedAtLoad proves an unknown git.mode is a
// config error AT LOAD — matching how ignore_regex is validated today —
// rather than a failure deferred to whichever internal/wt call happens to
// run first.
func TestGitConfigUnknownModeRejectedAtLoad(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, Dot), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, Dot, "config.yaml"),
		[]byte("schema: v1\ngit:\n  mode: bogus\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Detect(root, root)
	if err == nil {
		t.Fatal("expected an error for an unknown git.mode, got nil")
	}
	if !strings.Contains(err.Error(), "git.mode") {
		t.Fatalf("error %q does not name the offending key (git.mode)", err)
	}
}

// TestGitConfigBaseReadFromRepoDefaultBranch proves Base is READ from the
// repository rather than assumed: a repo whose default branch is deliberately
// not named "main" must still resolve Base to its actual branch.
func TestGitConfigBaseReadFromRepoDefaultBranch(t *testing.T) {
	root := t.TempDir()
	initRepo(t, root, "-b", "trunk")
	runGit(t, root, "commit", "-q", "--allow-empty", "-m", "init")
	if err := os.MkdirAll(filepath.Join(root, Dot), 0o755); err != nil {
		t.Fatal(err)
	}
	ws, err := Detect(root, root)
	if err != nil {
		t.Fatal(err)
	}
	if ws.Cfg.Git.Base != "trunk" {
		t.Fatalf("Git.Base = %q, want %q (read from the repo, never hardcoded main)", ws.Cfg.Git.Base, "trunk")
	}
}

// TestGitConfigDegradesQuietlyWithoutRepo proves the other half of the
// opt-out contract: a workspace with no git repository at all (so nothing to
// read a default branch from) must still load cleanly, Enabled/Mode/Remote
// still defaulted, Base simply left empty rather than the load failing.
func TestGitConfigDegradesQuietlyWithoutRepo(t *testing.T) {
	root := t.TempDir() // deliberately never git-init'd
	if err := os.MkdirAll(filepath.Join(root, Dot), 0o755); err != nil {
		t.Fatal(err)
	}
	ws, err := Detect(root, root)
	if err != nil {
		t.Fatal(err)
	}
	if ws.Cfg.Git.Base != "" {
		t.Fatalf("Git.Base = %q, want empty (no repository to read a default branch from)", ws.Cfg.Git.Base)
	}
	if !ws.Cfg.Git.IsEnabled() {
		t.Fatal("Git.IsEnabled() = false, want true — the absence of a repo must not disable the config default")
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
		"history:", // benchmarks.history (B-01KYJTB95HEPF: was missing from the scaffold)
		"max_rounds:", "grill:", "risk_files:", "dangerous_paths:", "worktrees_dir:", "coverage_gate:",
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
	if got.Benchmarks != want.Benchmarks {
		t.Errorf("Benchmarks = %+v, want %+v", got.Benchmarks, want.Benchmarks)
	}
	if len(got.Verify) != 0 || len(want.Verify) != 0 {
		t.Errorf("Verify = %v, want empty (both sides)", got.Verify)
	}
	if got.WorktreesDir != want.WorktreesDir {
		t.Errorf("WorktreesDir = %q, want %q", got.WorktreesDir, want.WorktreesDir)
	}
	// FeedbackCfg carries a slice since T-01KYFXDCH — compare fields.
	if got.Feedback.MaxRounds != want.Feedback.MaxRounds ||
		got.Feedback.Grill != want.Feedback.Grill ||
		got.Feedback.Validate != want.Feedback.Validate ||
		got.Feedback.RiskFiles != want.Feedback.RiskFiles ||
		!slices.Equal(got.Feedback.DangerousPaths, want.Feedback.DangerousPaths) {
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
	custom := "schema: v1\nlangs: [rust]\ncompact:\n  journal_max: 77\n"
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

// TestSchemaStampMatchesMigrationTarget pins the two constants together.
// SchemaStamp says what the server writes; migrate.To says what the migration
// produces. If they drift apart, every freshly written file is stamped with
// something the migration never emits — so a workspace would either be migrated
// in a loop or refused outright, depending on which way the drift went. Bumping
// the stamp therefore means bumping the migration, and this is the assertion
// that makes that non-optional.
func TestSchemaStampMatchesMigrationTarget(t *testing.T) {
	if SchemaStamp != migrate.To {
		t.Fatalf("SchemaStamp = %q but migrate.To = %q — bump both, and add the migration step between them",
			SchemaStamp, migrate.To)
	}
	if migrate.From == migrate.To {
		t.Fatalf("migrate.From == migrate.To == %q: the migration is a no-op", migrate.To)
	}
}

// TestDetectNestedWorktreeWinsOverAncestorBundle is the half of GitHub issue 27
// that a .git-file predicate alone does not reach, and the case the original
// report actually described: "not even with an absolute -root naming it".
//
// Detection tries the .spectackle/config.yaml marker BEFORE the .git marker, so
// when the ENCLOSING checkout already carries a bundle the walk used to sail
// past the worktree straight to the parent's config.yaml — and every write
// landed in the parent. Accepting .git files fixed only the case where no
// ancestor bundle existed yet, which is why a reproduction built on a bare
// parent passed while the reported scenario stayed broken.
func TestDetectNestedWorktreeWinsOverAncestorBundle(t *testing.T) {
	main := t.TempDir()
	initRepo(t, main, "-b", "main")
	runGit(t, main, "commit", "-q", "--allow-empty", "-m", "init")
	// the enclosing checkout owns a bundle, as any real repository would —
	// this is the ingredient the sibling reproduction lacked.
	if err := (Root{Dir: main}).EnsureScaffold(""); err != nil {
		t.Fatal(err)
	}
	if !fileExists(filepath.Join(main, Dot, "config.yaml")) {
		t.Fatal("fixture: the parent bundle was not scaffolded")
	}
	wtDir := filepath.Join(main, "wt", "feature")
	runGit(t, main, "worktree", "add", "-q", wtDir, "--detach", "HEAD")

	got, err := Detect(wtDir, wtDir)
	if err != nil {
		t.Fatal(err)
	}
	if !samePathTest(got.Dir, wtDir) {
		t.Fatalf("Detect(nested worktree) = %s, want %s — it resolved past the worktree to the ancestor bundle", got.Dir, wtDir)
	}

	// and the ordinary case must not regress: from a plain subdirectory of a
	// normal repo, the root bundle above is still what gets found.
	sub := filepath.Join(main, "deep", "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err = Detect(sub, sub)
	if err != nil {
		t.Fatal(err)
	}
	if !samePathTest(got.Dir, main) {
		t.Fatalf("Detect(subdir of a normal repo) = %s, want the repo root %s", got.Dir, main)
	}
}

func samePathTest(a, b string) bool {
	ra, err1 := filepath.EvalSymlinks(a)
	rb, err2 := filepath.EvalSymlinks(b)
	if err1 != nil || err2 != nil {
		return a == b
	}
	return ra == rb
}

// TestAwaitBudgetIsConfigurableAboveCI: the archive closure's CI wait was a
// hardcoded 5 minutes, which sat INSIDE this repository's own CI duration
// distribution (3m43s-5m38s over ten consecutive runs). So archives refused
// on builds that were merely unfinished — and because a refusal compensates
// by journaling an event, the retry pushed a new commit that started CI over,
// leaving the wait unable to converge (B-01KYQJDJJVFC2).
func TestAwaitBudgetIsConfigurableAboveCI(t *testing.T) {
	// the default must clear the observed ceiling with real margin, not sit
	// near a median — the failure is asymmetric, since expiring on a green
	// build costs a retry and the retry restarts CI
	if got := (GitCfg{}).AwaitBudget(); got <= 6*time.Minute {
		t.Fatalf("default await budget %v does not clear the observed CI ceiling", got)
	}
	// and it is a knob, because the right value is a property of the
	// repository's CI rather than of this program
	if got := (GitCfg{AwaitChecks: 90}).AwaitBudget(); got != 90*time.Second {
		t.Fatalf("configured await budget = %v, want 90s", got)
	}
	// 0 and negative both fall back rather than producing a zero wait, which
	// would refuse every archive instantly
	for _, n := range []int{0, -1} {
		if got := (GitCfg{AwaitChecks: n}).AwaitBudget(); got <= 6*time.Minute {
			t.Fatalf("await_checks=%d must fall back to the default, got %v", n, got)
		}
	}
}

// TestIsRecordsPath pins the segment semantics, including the two cases the
// three hand-written spellings disagreed on: a nested context dir (which the
// root-anchored spellings missed, deadlocking an item's own archive) and a
// near-miss filename (which the HasPrefix spelling would have swallowed as if
// it were server-owned).
func TestIsRecordsPath(t *testing.T) {
	for _, c := range []struct {
		in   string
		want bool
	}{
		{".spectackle", true},
		{".spectackle/work.md", true},
		{".spectackle/spec.md", true},
		{"internal/mcpserver/.spectackle/work.md", true}, // the deadlock case
		{"a/b/c/.spectackle/journal.ndjson", true},       // arbitrary depth
		{"internal/mcpserver/.spectackle", true},         // the dir itself
		{".spectacklefoo", false},                        // near miss: ordinary work
		{"internal/.spectacklefoo/x.go", false},          // near miss, nested
		{"internal/spectackle/x.go", false},              // no leading dot
		{"pkg/a.go", false},
		{"", false},

		// B-01KYSK7HQFFPM: the exemption is by NAME, not by directory. These
		// four were MEASURED passing the real scope gate at exit 0 under the
		// any-file-under-a-.spectackle-segment spelling, at the root and at
		// depth, in a compiled language and in a scripting one.
		{".spectackle/evil.go", false},
		{"internal/mcpserver/.spectackle/evil.go", false},
		{"a/b/c/.spectackle/payload.sh", false},
		{"a/b/c/.spectackle/notes.txt", false},
		// First-match anchoring, spelled out: under last-match this smuggles
		// a .go file back in one directory deeper.
		{".spectackle/evil/.spectackle/work.md", false},
		{".spectackle/evil/x.go", false},

		// Every allowlisted basename, at the root and nested, so dropping any
		// one of them from the list deadlocks the gate on a file the server
		// writes and cannot stop writing.
		{".spectackle/journal.ndjson", true},
		{".spectackle/bench.ndjson", true},
		{".spectackle/anchors.tsv", true},
		{".spectackle/config.yaml", true},
		{".spectackle/.gitignore", true},
		{".spectackle/.gitattributes", true},
		{"internal/x/.spectackle/spec.md", true},
		{"internal/x/.spectackle/bench.ndjson", true},
		{"internal/x/.spectackle/anchors.tsv", true},
		{"internal/x/.spectackle/config.yaml", true},
		{"internal/x/.spectackle/.gitignore", true},
		{"internal/x/.spectackle/.gitattributes", true},
		// The temp spellings: journal.Rewrite's and migrate's writeAtomic's.
		{"internal/x/.spectackle/journal-123456", true},
		{".spectackle/.work.md.tmp998877", true},
		{"internal/x/.spectackle/journal-notatemp.go", false},

		// The temp exemption must be exactly as narrow as its producers.
		// An independent validator drove every one of these past the REAL
		// scope gate and into wt.CommitRecords, using the earlier
		// `\.[^/]+\.tmp[0-9]*` spelling: `[^/]+` admits any basename and
		// `[0-9]*` admits none, so any dotfile ending in .tmp was exempt at
		// any depth — an arbitrary-content file with an author-chosen name.
		// os.CreateTemp always substitutes decimal digits, and migrate's temp
		// basename is always a bundle file, so neither slack was ever needed.
		{".spectackle/.evil.tmp", false},
		{".spectackle/.payload.sh.tmp", false},
		{".spectackle/.evil.go.tmp", false},
		{".spectackle/.x.tmp0", false},
		{"internal/x/.spectackle/.attack.sh.tmp9", false},
		{".spectackle/.work.md.tmp", false}, // real basename, but no digits
		{"internal/x/.spectackle/.spec.md.tmp42", true},

		// The backup subtree is anchored on the version stamp, not the bare
		// prefix: `migrate-backup-` alone was an attacker-nameable subtree
		// whose every file the gate waved through. Only migrate's own
		// backupPrefix+From shape ("v" plus digits) is exempt.
		{".spectackle/migrate-backup-/evil.sh", false},
		{".spectackle/migrate-backup-vx/evil.sh", false},
		{".spectackle/migrate-backup-v0/COMPLETE", true},
		{".spectackle/migrate-backup-v12/core/.spectackle/work.md", true},

		// The three subtree exemptions are ROOT-ONLY, because all three are
		// root-only by construction (CacheDir, the worktree path and migrate's
		// backup are all filepath.Join(root, Dot, …) with no context segment).
		// Anchored on the name alone, both of the next two were measured
		// reaching wt.DirtyFiles and passing the real gate at exit 0 — the
		// attacker cost is naming a directory `wt` inside a NESTED records
		// folder, where EnsureScaffold writes no .gitignore to mask it.
		{"internal/x/.spectackle/wt/evil.sh", false},
		{"internal/x/.spectackle/cache/evil.sh", false},
		{"internal/x/.spectackle/migrate-backup-v9/evil.go", false},
		{".spectackle/wt/anything/at/all", true},
		{".spectackle/cache/index.db", true},

		// The gitignored / server-owned subtrees, exempt wholesale because
		// their contents are not enumerable from here.
		{".spectackle/cache/parse.db", true},
		{".spectackle/wt/T-0001/internal/x/main.go", true},
	} {
		if got := IsRecordsPath(c.in); got != c.want {
			t.Errorf("IsRecordsPath(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

// TestIsRecordsPathCoversRetainedMigrationBackup is the correction the naive
// allowlist needed, kept as its own test because it is the one exemption whose
// justification is not "the server writes this name" but "the server writes
// this SUBTREE and never removes it".
//
// internal/migrate copies every bundle file into
// .spectackle/migrate-backup-<from>/<rel> and writes a COMPLETE marker beside
// them, and migrate.go:84 says the backup is RETAINED. Refusing those paths
// makes the scope gate — a PRE-Move guard — fire on files no downstream
// records commit can ever absorb, which is the unclearable deadlock this
// predicate exists to prevent. Driving the REAL gate against a post-migration
// tree refused all six backup files before this exemption existed, while
// `go test ./...` stayed 32-ok, because nothing else drives a migrated tree.
//
// Note the last row: the backup holds whole copies of NESTED bundles, so the
// path contains two `.spectackle` segments. It is exempt via the first
// segment's subtree rule, not the second's — which is why the anchor choice is
// documented on IsRecordsPath rather than left to the reader.
func TestIsRecordsPathCoversRetainedMigrationBackup(t *testing.T) {
	for _, p := range []string{
		".spectackle/migrate-backup-v0",
		".spectackle/migrate-backup-v0/COMPLETE",
		".spectackle/migrate-backup-v0/.spectackle/work.md",
		".spectackle/migrate-backup-v0/core/.spectackle/work.md",
		".spectackle/migrate-backup-v0/core/.spectackle/journal.ndjson",
	} {
		if !IsRecordsPath(p) {
			t.Errorf("IsRecordsPath(%q) = false — the retained migration backup would hit the pre-Move scope gate, which no transition can clear", p)
		}
	}
	// The exemption is anchored on the prefix, not on "anything hyphenated".
	for _, p := range []string{
		".spectackle/migrate-backupv0/COMPLETE",
		".spectackle/backup-v0/COMPLETE",
	} {
		if IsRecordsPath(p) {
			t.Errorf("IsRecordsPath(%q) = true — the backup exemption is wider than %q*", p, "migrate-backup-")
		}
	}
}

// TestScaffoldIgnoresRetainedMigrationBackup pins the OTHER half of the same
// correction, and it has to be a separate assertion: the two fixes mask each
// other. With the backup gitignored it never reaches DirtyFiles, so the
// predicate exemption alone is untestable through the gate on a freshly
// scaffolded workspace — and with the predicate exemption in place the missing
// ignore line costs nothing at the gate, only a dozen untracked files that
// wt.CommitRecords would sweep into a records commit (wt.go:559 matches on
// `.spectackle/` prefix and would take the whole backup with it).
//
// ensure-lines, not write-if-absent, is the point: this must reach workspaces
// that were scaffolded before the line existed, which is every workspace that
// has already migrated.
func TestScaffoldIgnoresRetainedMigrationBackup(t *testing.T) {
	root := t.TempDir()
	dot := filepath.Join(root, Dot)
	if err := os.MkdirAll(dot, 0o755); err != nil {
		t.Fatal(err)
	}
	// A workspace scaffolded by an older build: cache/ and wt/ only.
	if err := os.WriteFile(filepath.Join(dot, ".gitignore"), []byte("cache/\nwt/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := (Root{Dir: root, Cfg: defaultConfig()}).EnsureScaffold(""); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dot, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, l := range strings.Split(string(raw), "\n") {
		got[strings.TrimSpace(l)] = true
	}
	for _, want := range []string{"cache/", "wt/", "migrate-backup-*/"} {
		if !got[want] {
			t.Errorf("scaffolded .gitignore is missing %q; have %q", want, raw)
		}
	}
}
