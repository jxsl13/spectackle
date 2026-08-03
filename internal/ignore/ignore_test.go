package ignore

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/jxsl13/spectackle/internal/wt"
)

// initRepo creates a bare git repo (no commits needed: `ls-files --others
// --ignored` reads the working tree and .gitignore directly, independent of
// the index history) or skips the test if git itself is unavailable.
func initRepo(t *testing.T, dir string) {
	t.Helper()
	if out, err := exec.Command("git", "init", "-q", dir).CombinedOutput(); err != nil {
		t.Skipf("git unavailable: %v: %s", err, out)
	}
	// No commit is made here, so no detached maintenance child is spawned
	// TODAY — but "today" is exactly the reasoning that let B-01KYQA4WXEFAT
	// reach CI from two directions. The moment a test in this file grows a
	// commit, the fixture starts racing t.TempDir's removal, and nobody
	// adding that commit will think about it. Disabled up front instead.
	if err := wt.QuietMaintenance(dir); err != nil {
		t.Fatalf("QuietMaintenance: %v", err)
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestWhollyIgnoredDirectory covers the core case this package exists for:
// a directory excluded outright (no negation inside) collapses to one
// ls-files entry (see New's --directory note), and Ignored must still
// report every path beneath it as ignored even though none of those deeper
// paths appear in git's output individually.
func TestWhollyIgnoredDirectory(t *testing.T) {
	root := t.TempDir()
	initRepo(t, root)
	write(t, filepath.Join(root, ".gitignore"), "build/\n")
	write(t, filepath.Join(root, "build", "a.go"), "package build\n")
	write(t, filepath.Join(root, "build", "sub", "b.go"), "package sub\n")
	write(t, filepath.Join(root, "keep.go"), "package keep\n")

	m := New(root)
	cases := map[string]bool{
		"build":          true,
		"build/a.go":     true,
		"build/sub":      true,
		"build/sub/b.go": true,
		"keep.go":        false,
		"":               false,
	}
	for rel, want := range cases {
		if got := m.Ignored(rel); got != want {
			t.Errorf("Ignored(%q) = %v, want %v", rel, got, want)
		}
	}
}

// TestNegationInsideIgnoredDir is the case a prefix matcher gets wrong: a
// directory-wide glob combined with a negated exception must keep the
// negated file reachable while still excluding its siblings, and the
// directory itself must NOT be reported as ignored (the walk still has to
// descend into it to find the kept file).
func TestNegationInsideIgnoredDir(t *testing.T) {
	root := t.TempDir()
	initRepo(t, root)
	// "keepdir/" alone would exclude the directory itself and defeat the
	// negation below (git never looks inside an excluded directory) — the
	// standard gitignore idiom for "keep one file" is a content glob instead.
	write(t, filepath.Join(root, ".gitignore"), "keepdir/*\n!keepdir/keep.go\n")
	write(t, filepath.Join(root, "keepdir", "keep.go"), "package keepdir\n")
	write(t, filepath.Join(root, "keepdir", "drop.go"), "package keepdir\n")

	m := New(root)
	if m.Ignored("keepdir") {
		t.Error(`Ignored("keepdir") = true, want false: a negated file inside means the walk must still descend`)
	}
	if m.Ignored("keepdir/keep.go") {
		t.Error(`Ignored("keepdir/keep.go") = true, want false: negated by !keepdir/keep.go`)
	}
	if !m.Ignored("keepdir/drop.go") {
		t.Error(`Ignored("keepdir/drop.go") = false, want true`)
	}
}

// TestNestedGitignore proves a .gitignore deeper in the tree (not just the
// root one) is honored, since --exclude-standard walks every directory's
// own .gitignore rather than only the root's.
func TestNestedGitignore(t *testing.T) {
	root := t.TempDir()
	initRepo(t, root)
	write(t, filepath.Join(root, "sub", ".gitignore"), "gen/\n")
	write(t, filepath.Join(root, "sub", "gen", "x.go"), "package gen\n")
	write(t, filepath.Join(root, "sub", "keep", "y.go"), "package keep\n")

	m := New(root)
	if !m.Ignored("sub/gen") {
		t.Error(`Ignored("sub/gen") = false, want true: nested sub/.gitignore excludes gen/`)
	}
	if m.Ignored("sub/keep") {
		t.Error(`Ignored("sub/keep") = true, want false`)
	}
	if m.Ignored("sub") {
		t.Error(`Ignored("sub") = true, want false: sub itself is not ignored, only its gen/ child`)
	}
}

// TestNotAGitRepo proves the required degradation: a plain directory with
// no .git at all must never error and must never report anything as
// ignored, so a non-git workspace walks exactly as it did before this
// package existed.
func TestNotAGitRepo(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "vendor", "anything.go"), "package vendor\n")

	m := New(root)
	if m.Ignored("vendor") || m.Ignored("vendor/anything.go") || m.Ignored("anything") {
		t.Error("a non-git directory must never be reported as ignored")
	}
}

// TestGitUnavailable simulates git missing from PATH entirely (rather than
// merely absent a repo): New must still return a working, all-false Matcher
// and must never surface an error to the caller — SkipDir has no error
// return to give it one.
func TestGitUnavailable(t *testing.T) {
	root := t.TempDir()
	initRepo(t, root)
	write(t, filepath.Join(root, ".gitignore"), "shouldstillbevisible/\n")
	write(t, filepath.Join(root, "shouldstillbevisible", "a.go"), "package a\n")

	t.Setenv("PATH", t.TempDir()) // an empty dir: exec.LookPath("git") fails
	m := New(root)
	if m.Ignored("shouldstillbevisible") {
		t.Error("Ignored(...) = true with git unavailable, want false (degrade, don't guess)")
	}
}
