package spec

import (
	"os"
	"path/filepath"
	"testing"
)

// buildTree writes a fake repo exercising cascade, override and scope in the
// bundled .spectackle layout.
func buildTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		".spectackle/spec.md": `---
schema: v1
prefix: GLB
---
## GLB-ARC-001
The system SHALL log to ` + "`stderr`" + ` only.

## ROOT-STY-001
The repository SHALL keep every exported symbol documented in doc.go files.
`,
		"gpu/.spectackle/spec.md": `---
schema: v1
prefix: GPU
overrides: [ROOT-STY-001]
---
## GPU-KRN-001
WHEN a kernel is launched, the wrapper SHALL check cudaGetLastError.
`,
		"gpu/metal/.spectackle/spec.md": `---
schema: v1
prefix: MTL
scope: ["*.metal"]
---
## intent
Metal shader rules.

## MTL-KRN-001
The shader SHALL declare each threadgroup size as a numeric literal such as 64.
`,
		"gpu/kern.cu":       "// code\n",
		"gpu/metal/f.metal": "// code\n",
		"gpu/metal/host.go": "// code\n",
		"pkg/util.go":       "// code\n",
	}
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func ids(rs []ResolvedRule) []string {
	var out []string
	for _, r := range rs {
		out = append(out, r.ID)
	}
	return out
}

func TestForPathCascade(t *testing.T) {
	c, err := Load(buildTree(t))
	if err != nil {
		t.Fatal(err)
	}
	if fs := c.Findings(); len(fs) != 0 {
		t.Fatalf("tree should lint clean, got %v", fs)
	}

	// root-level file: root rules only
	got := ids(c.ForPath("pkg/util.go"))
	assertEq(t, "pkg/util.go", got, []string{"GLB-ARC-001", "ROOT-STY-001"})

	// gpu file: ROOT-STY-001 overridden away, GPU rule added
	got = ids(c.ForPath("gpu/kern.cu"))
	assertEq(t, "gpu/kern.cu", got, []string{"GLB-ARC-001", "GPU-KRN-001"})

	// scoped file matches *.metal only
	got = ids(c.ForPath("gpu/metal/f.metal"))
	assertEq(t, "gpu/metal/f.metal", got, []string{"GLB-ARC-001", "GPU-KRN-001", "MTL-KRN-001"})

	got = ids(c.ForPath("gpu/metal/host.go"))
	assertEq(t, "gpu/metal/host.go", got, []string{"GLB-ARC-001", "GPU-KRN-001"})
}

// TestForNode (SPX-SPC-007): explicit applies bindings come first, the
// file's cascade rules follow, IDs never repeat.
func TestForNode(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		".spectackle/spec.md": `---
schema: v1
prefix: GLB
---
## GLB-ARC-001
The system SHALL log to ` + "`stderr`" + ` only.
`,
		"gpu/.spectackle/spec.md": `---
schema: v1
prefix: GPU
---
## GPU-KRN-001 {applies: cu:pkg.kernel}
WHEN a kernel is launched, the wrapper SHALL check cudaGetLastError.

## GPU-KRN-002 {applies: cu:pkg.kernel,cu:pkg.other}
WHEN a kernel exits, the wrapper SHALL synchronize the stream.
`,
		"gpu/kern.cu": "// code\n",
	}
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	c, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}

	// applies matches first (both binding rules), then the file cascade
	// (GLB root rule); the applies-bound GPU rules must not repeat.
	got := ids(c.ForNode("cu:pkg.kernel", "gpu/kern.cu"))
	assertEq(t, "cu:pkg.kernel", got, []string{"GPU-KRN-001", "GPU-KRN-002", "GLB-ARC-001"})

	// single-binding node: the other applies rule joins via the cascade only
	got = ids(c.ForNode("cu:pkg.other", "gpu/kern.cu"))
	assertEq(t, "cu:pkg.other", got, []string{"GPU-KRN-002", "GLB-ARC-001", "GPU-KRN-001"})

	// unknown node, no file: nothing
	if got := c.ForNode("go:none.Missing", ""); len(got) != 0 {
		t.Fatalf("unknown node without file must bind nothing, got %v", ids(got))
	}
}

// TestLoadSkipsNestedGitWorktree: a duplicate spec bundle nested under a
// directory that is itself a separate git boundary — a linked worktree
// (signaled by a .git FILE holding a `gitdir:` pointer, e.g. what any agent
// harness's worktrees look like, .claude included) or a nested clone
// (signaled by a .git DIRECTORY) — must NOT be discovered. This is the
// generic, harness-independent mechanism (workspace.Root.SkipDir /
// IsNestedGitBoundary) that replaced a hardcoded '.claude' name check: no
// literal '.claude' appears anywhere in this test.
func TestLoadSkipsNestedGitWorktree(t *testing.T) {
	root := buildTree(t)

	dup := `---
schema: v1
prefix: GLB
---
## GLB-ARC-001
The system SHALL log to ` + "`stderr`" + ` only.
`

	// a .git FILE worktree (tmp/worktrees/x), same shape git itself writes
	// for `git worktree add`.
	fileWorktreeBundle := filepath.Join(root, "tmp", "worktrees", "x", ".spectackle")
	if err := os.MkdirAll(fileWorktreeBundle, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fileWorktreeBundle, "spec.md"), []byte(dup), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "tmp", "worktrees", "x", ".git"),
		[]byte("gitdir: /elsewhere/.git/worktrees/x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	c, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if fs := c.Findings(); len(fs) != 0 {
		t.Fatalf(".git-FILE worktree bundle must not be discovered, got findings %v", fs)
	}
	if len(c.All()) != 3 {
		t.Fatalf("Load discovered %d spec files, want 3 (.git-FILE worktree subtree must be skipped): %+v", len(c.All()), c.All())
	}

	// a .git DIRECTORY nested clone (vendored-repo), also skipped.
	dirCloneBundle := filepath.Join(root, "vendored-repo", ".spectackle")
	if err := os.MkdirAll(dirCloneBundle, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dirCloneBundle, "spec.md"), []byte(dup), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "vendored-repo", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	c2, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if fs := c2.Findings(); len(fs) != 0 {
		t.Fatalf(".git-DIR nested clone bundle must not be discovered, got findings %v", fs)
	}
	if len(c2.All()) != 3 {
		t.Fatalf("Load discovered %d spec files, want 3 (.git-DIR nested clone subtree must be skipped): %+v", len(c2.All()), c2.All())
	}
}

// TestLoadHonorsConfigIgnore proves spec.Load consults the same Config.Ignore
// globs and Config.IgnoreRegex patterns as workspace.Root.ContextDirs and the
// coverage-gap walk (all four share workspace.Root.SkipDir), by reading
// root/.spectackle/config.yaml itself (via workspace.LoadRoot).
func TestLoadHonorsConfigIgnore(t *testing.T) {
	root := buildTree(t)

	dup := `---
schema: v1
prefix: GLB
---
## GLB-ARC-001
The system SHALL log to ` + "`stderr`" + ` only.
`
	globBundle := filepath.Join(root, "generated", ".spectackle")
	if err := os.MkdirAll(globBundle, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(globBundle, "spec.md"), []byte(dup), 0o644); err != nil {
		t.Fatal(err)
	}
	regexBundle := filepath.Join(root, "vendor-acme", ".spectackle")
	if err := os.MkdirAll(regexBundle, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(regexBundle, "spec.md"), []byte(dup), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := "schema: v1\nignore: [\"generated/**\"]\nignore_regex: [\"^vendor-[a-z]+$\"]\n"
	if err := os.MkdirAll(filepath.Join(root, ".spectackle"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".spectackle", "config.yaml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}

	c, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if fs := c.Findings(); len(fs) != 0 {
		t.Fatalf("ignore glob/regex bundles must not be discovered, got findings %v", fs)
	}
	if len(c.All()) != 3 {
		t.Fatalf("Load discovered %d spec files, want 3 (generated/ and vendor-acme/ must be pruned by config.yaml ignore): %+v", len(c.All()), c.All())
	}
}

func TestProseSectionsParsed(t *testing.T) {
	c, err := Load(buildTree(t))
	if err != nil {
		t.Fatal(err)
	}
	sf, ok := c.File("gpu/metal")
	if !ok {
		t.Fatal("gpu/metal spec file not loaded")
	}
	if len(sf.Sections) != 1 || sf.Sections[0].Name != "intent" || sf.Sections[0].Text != "Metal shader rules." {
		t.Fatalf("sections parsed wrong: %+v", sf.Sections)
	}
}

func TestSchemaStampRejected(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, ".spectackle", "spec.md")
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, []byte("---\nschema: v99\n---\n"), 0o644)
	if _, err := Load(root); err == nil {
		t.Fatal("expected schema mismatch error, got nil")
	}
}

func assertEq(t *testing.T, label string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: got %v, want %v", label, got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("%s: got %v, want %v", label, got, want)
		}
	}
}

// TestSelfSpecsLintClean dogfoods: every committed spec bundle in this
// repository must lint clean and carry no duplicate IDs (self-hosting seed).
func TestSelfSpecsLintClean(t *testing.T) {
	c, err := Load(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.All()) < 4 {
		t.Fatalf("expected the repo's own spec cascade to be discovered, got %d files", len(c.All()))
	}
	for _, f := range c.Findings() {
		t.Errorf("self-spec finding: %s", f)
	}
}
