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
schema: v0
prefix: GLB
---
## GLB-ARC-001
The system SHALL log to ` + "`stderr`" + ` only.

## ROOT-STY-001
The repository SHALL keep every exported symbol documented in doc.go files.
`,
		"gpu/.spectackle/spec.md": `---
schema: v0
prefix: GPU
overrides: [ROOT-STY-001]
---
## GPU-KRN-001
WHEN a kernel is launched, the wrapper SHALL check cudaGetLastError.
`,
		"gpu/metal/.spectackle/spec.md": `---
schema: v0
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
