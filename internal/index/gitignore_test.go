package index

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/jxsl13/spectackle/internal/graph"
	"github.com/jxsl13/spectackle/internal/wt"
)

// The indexer's half of GitHub issue 26.
//
// The gitignore matcher was first wired into workspace.Root.SkipDir, which
// covers ContextDirs, the spec cascade, the swarm walk and the coverage-gap
// walk — but NOT this walk. IndexAll has its own ignoreDirs set (typespass.go's
// moduleHashKey walk has to mirror it exactly), so the graph, and therefore
// find scope=code, stayed full of gitignored copies. These tests pin the walk
// itself, which is where the reported symptom actually lived.

// gitInit makes dir a git repository, or skips the test. git is a hard
// dependency of the matcher by design (it answers gitignore semantics), so a
// machine without it legitimately cannot run these.
func gitInit(t *testing.T, dir string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH; the ignore matcher delegates to it by design")
	}
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	// Init-only fixtures make no commit and so spawn no detached maintenance
	// child today; disabled anyway, so that adding a commit here later cannot
	// quietly reintroduce B-01KYQA4WXEFAT's temp-dir removal race.
	if err := wt.QuietMaintenance(dir); err != nil {
		t.Fatalf("QuietMaintenance: %v", err)
	}
}

func indexedFiles(t *testing.T, root string) []string {
	t.Helper()
	g := graph.NewMem()
	if _, err := newTestIndexer(g).IndexAll(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	// Find over the whole graph: the Graph interface exposes no bulk
	// enumeration, and a wide-enough Find is exactly what `find scope=code`
	// itself runs, so this asserts against the surface the issue was reported
	// on rather than an internal one.
	var out []string
	for _, n := range g.Find("", 1000, graph.KUnknown) {
		out = append(out, string(n.ID))
	}
	return out
}

// TestIndexAllSkipsGitignoredDirectory is the field report's exact shape: one
// real symbol plus a gitignored copy of it. Before the fix the copy was indexed
// too, and — the damaging part — sorted FIRST, taking the unsuffixed node ID,
// so a rule anchored via applies pinned its contract inside a virtualenv.
func TestIndexAllSkipsGitignoredDirectory(t *testing.T) {
	root := t.TempDir()
	gitInit(t, root)
	writeFile(t, root, ".gitignore", ".venv/\n")
	writeFile(t, root, "real/saxpy.go", "package real\n\nfunc Saxpy() {}\n")
	writeFile(t, root, ".venv/lib/copy/saxpy.go", "package copy\n\nfunc Saxpy() {}\n")

	ids := indexedFiles(t, root)
	for _, id := range ids {
		if strings.Contains(id, "copy") {
			t.Fatalf("a gitignored copy reached the graph: %v", ids)
		}
	}
	var found bool
	for _, id := range ids {
		if id == "go:real.Saxpy" {
			found = true
		}
	}
	if !found {
		// the unsuffixed ID specifically: a "~2" suffix would mean the copy
		// had been indexed first and taken the plain one.
		t.Fatalf("the real symbol did not get the unsuffixed node ID: %v", ids)
	}
}

// TestIndexAllRespectsNegatedGitignorePattern: a negation is the case a naive
// prefix matcher gets wrong, and getting it wrong drops REAL code from the
// graph — worse than the inflation being fixed. Delegating to git is what buys
// this, so it is worth asserting rather than assuming.
func TestIndexAllRespectsNegatedGitignorePattern(t *testing.T) {
	root := t.TempDir()
	gitInit(t, root)
	writeFile(t, root, ".gitignore", "gen/*\n!gen/keep.go\n")
	writeFile(t, root, "gen/keep.go", "package gen\n\nfunc Keep() {}\n")
	writeFile(t, root, "gen/drop.go", "package gen\n\nfunc Drop() {}\n")

	ids := indexedFiles(t, root)
	var keep, drop bool
	for _, id := range ids {
		switch id {
		case "go:gen.Keep":
			keep = true
		case "go:gen.Drop":
			drop = true
		}
	}
	if !keep {
		t.Fatalf("a negated (un-ignored) file was dropped from the graph: %v", ids)
	}
	if drop {
		t.Fatalf("an ignored sibling reached the graph: %v", ids)
	}
}

// TestIndexAllWithoutGitIndexesEverything: outside a git repository the matcher
// answers false for everything, so the walk behaves exactly as it did before
// this change. A non-git tree is not "everything ignored", and a missing or
// failing git must never turn into an error on a tool call.
func TestIndexAllWithoutGitIndexesEverything(t *testing.T) {
	root := t.TempDir()
	// no gitInit: deliberately not a repository
	writeFile(t, root, ".gitignore", ".venv/\n")
	writeFile(t, root, "real/saxpy.go", "package real\n\nfunc Saxpy() {}\n")
	writeFile(t, root, ".venv/lib/copy/saxpy.go", "package copy\n\nfunc Saxpy() {}\n")

	ids := indexedFiles(t, root)
	var copySeen bool
	for _, id := range ids {
		if strings.Contains(id, "copy") {
			copySeen = true
		}
	}
	if !copySeen {
		t.Fatalf("outside a git repo the walk must not honor .gitignore: %v", ids)
	}
}
