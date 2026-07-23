package spec

import (
	"os"
	"path/filepath"
	"testing"
)

// buildTree writes a fake repo exercising cascade, override and scope.
func buildTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		".spectacle/global.ears.md": `---
prefix: GLB
---
## GLB-ARC-001
The system SHALL log to ` + "`stderr`" + ` only.
`,
		".spectacle.ears.md": `---
prefix: ROOT
---
## ROOT-STY-001
The repository SHALL keep every exported symbol documented in doc.go files.
`,
		"gpu/.spectacle.ears.md": `---
prefix: GPU
overrides: [ROOT-STY-001]
---
## GPU-KRN-001
WHEN a kernel is launched, the wrapper SHALL check cudaGetLastError.
`,
		"gpu/metal/.spectacle.ears.md": `---
prefix: MTL
scope: ["*.metal"]
---
## MTL-KRN-001
The shader SHALL declare each threadgroup size as a numeric literal such as 64.
`,
		"gpu/kern.cu":        "// code\n",
		"gpu/metal/f.metal":  "// code\n",
		"gpu/metal/host.go":  "// code\n",
		"pkg/util.go":        "// code\n",
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
		t.Fatalf("bootstrap tree should lint clean, got %v", fs)
	}

	// root-level file: global + root rules
	got := ids(c.ForPath("pkg/util.go"))
	want := []string{"GLB-ARC-001", "ROOT-STY-001"}
	assertEq(t, "pkg/util.go", got, want)

	// gpu file: ROOT-STY-001 overridden away, GPU rule added
	got = ids(c.ForPath("gpu/kern.cu"))
	want = []string{"GLB-ARC-001", "GPU-KRN-001"}
	assertEq(t, "gpu/kern.cu", got, want)

	// scoped file matches *.metal only
	got = ids(c.ForPath("gpu/metal/f.metal"))
	want = []string{"GLB-ARC-001", "GPU-KRN-001", "MTL-KRN-001"}
	assertEq(t, "gpu/metal/f.metal", got, want)

	got = ids(c.ForPath("gpu/metal/host.go"))
	want = []string{"GLB-ARC-001", "GPU-KRN-001"}
	assertEq(t, "gpu/metal/host.go", got, want)
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

// TestSelfSpecsLintClean dogfoods: every committed spec file in this
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
