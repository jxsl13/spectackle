package main

import (
	"os"
	"path/filepath"
	"testing"
)

func writeSpec(t *testing.T, root, content string) {
	t.Helper()
	p := filepath.Join(root, ".spectackle", "spec.md")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLintExitCodes(t *testing.T) {
	clean := t.TempDir()
	writeSpec(t, clean, "---\nschema: v0\n---\n## TST-ARC-001\nThe tool SHALL exit with code 0 on clean specs.\n")
	if code := lint([]string{clean}); code != 0 {
		t.Fatalf("clean tree: lint = %d, want 0", code)
	}

	dirty := t.TempDir()
	writeSpec(t, dirty, "---\nschema: v0\n---\n## TST-ARC-001\nThe tool should handle things appropriately.\n")
	if code := lint([]string{dirty}); code != 1 {
		t.Fatalf("dirty tree: lint = %d, want 1", code)
	}
}

func TestReindexExitCode(t *testing.T) {
	root := t.TempDir()
	writeSpec(t, root, "---\nschema: v0\n---\n")
	if code := reindex([]string{"-root", root}); code != 0 {
		t.Fatalf("reindex = %d, want 0", code)
	}
	// cache landed inside .spectackle/cache, never elsewhere in the workspace
	if _, err := os.Stat(filepath.Join(root, ".spectackle", "cache", "index.db")); err != nil {
		t.Fatalf("cache not created where expected: %v", err)
	}
}
