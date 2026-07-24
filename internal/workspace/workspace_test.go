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

	// marker: config.yaml at root, nested .spectacle must NOT win
	mk(".spectacle/config.yaml", "schema: v0\n")
	mk("sub/deep/.spectacle/spec.md", "---\nschema: v0\n---\n")

	ws, err := Detect(filepath.Join(root, "sub", "deep"), "")
	if err != nil {
		t.Fatal(err)
	}
	if ws.Dir != root {
		t.Fatalf("Detect from nested dir = %s, want %s (nested .spectacle must not shadow the root marker)", ws.Dir, root)
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
}
