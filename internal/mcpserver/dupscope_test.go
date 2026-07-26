package mcpserver

// Dup-detector added-line scoping (T-01KYFPNCX): a diff that only inserts
// code ADJACENT to one twin of a pre-existing dup pair stays silent; a diff
// that ADDS a twin fires. The waiver-rate tripwire attributed the bulk of
// this session's waivers to exactly this false-positive class.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jxsl13/spectackle/internal/journal"
)

func writeFileT(t *testing.T, root, rel, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, rel), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDiffAddedLines(t *testing.T) {
	diff := `diff --git a/x.go b/x.go
index 111..222 100644
--- a/x.go
+++ b/x.go
@@ -10,6 +10,8 @@ func ctx() {
 context1
 context2
+added1
+added2
 context3
-removed
+added3
 context4
`
	got := diffAddedLines(diff)
	runs := got["x.go"]
	if len(runs) != 2 {
		t.Fatalf("want 2 added runs, got %v", runs)
	}
	// new-side numbering: context1=10 context2=11 added1=12 added2=13
	// context3=14 added3=15 context4=16
	if runs[0] != [2]int{12, 13} || runs[1] != [2]int{15, 15} {
		t.Fatalf("added runs wrong: %v", runs)
	}
}

// The incident regression, both directions, at the validateDups level with
// a real workspace: two identical twins pre-exist; a diff whose added lines
// touch neither stays silent even though its hunk CONTEXT overlaps one; a
// diff that adds a third twin fires.
func TestValidateDupsAddedOnlyScoping(t *testing.T) {
	root := t.TempDir()
	twin := "package demo\n\n// A returns one.\nfunc A() int {\n\tx := 1\n\ty := x + 1\n\tz := y * 2\n\treturn z - y + x\n}\n\n// B returns one.\nfunc B() int {\n\tx := 1\n\ty := x + 1\n\tz := y * 2\n\treturn z - y + x\n}\n\nfunc pad() {}\n"
	writeFileT(t, root, "demo.go", twin)
	s, _ := connectRootWithServer(t, root)

	// (1) adjacent insertion: added lines land on pad(), context covers B.
	adjacent := `diff --git a/demo.go b/demo.go
index 111..222 100644
--- a/demo.go
+++ b/demo.go
@@ -17,3 +17,5 @@ func B() int {
 }

 func pad() {}
+
+func newcomer() int { return 42 }
`
	if got := s.validateDups(adjacent); len(got) != 0 {
		t.Fatalf("context-only overlap must stay silent: %v", got)
	}

	// (2) the diff ADDS a twin of A: fires.
	added := `diff --git a/demo.go b/demo.go
index 111..222 100644
--- a/demo.go
+++ b/demo.go
@@ -1,5 +1,14 @@
 package demo

+// C returns one.
+func C() int {
+	x := 1
+	y := x + 1
+	z := y * 2
+	return z - y + x
+}
+
 // A returns one.
`
	// materialize the post-diff tree so the graph sees C
	withC := strings.Replace(twin, "// A returns one.",
		"// C returns one.\nfunc C() int {\n\tx := 1\n\ty := x + 1\n\tz := y * 2\n\treturn z - y + x\n}\n\n// A returns one.", 1)
	writeFileT(t, root, "demo.go", withC)
	s.markDirty()
	s.reindex()
	got := s.validateDups(added)
	fired := false
	for _, l := range got {
		if strings.Contains(l, "demo.C") {
			fired = true
		}
	}
	if !fired {
		t.Fatalf("an added twin must fire: %v", got)
	}
}

func TestHashPrefixUnified(t *testing.T) {
	h := "0123456789abcdef0123"
	if short8(h) != "01234567" || shortHash(h) != "0123456789ab" {
		t.Fatalf("truncation forms wrong: %s %s", short8(h), shortHash(h))
	}
	if short8("abc") != "abc" || hashPrefix("abc", 8) != "abc" {
		t.Fatal("short input must pass through")
	}
}

// Rider: a terminal note citing an orphan's full ID closes it.
func TestOrphanClosedByCitation(t *testing.T) {
	root := t.TempDir()
	s, _ := connectRootWithServer(t, root)
	orphan := "T-01KYZZQRPHANC1TE0000000000"
	if err := journal.Append(s.ws, "", journal.Event{Ev: journal.EvCreate, ID: orphan}); err != nil {
		t.Fatal(err)
	}
	if got := s.orphanedItems(); len(got) != 1 {
		t.Fatalf("setup: want the orphan visible, got %v", got)
	}
	if err := journal.Append(s.ws, "", journal.Event{
		Ev: journal.EvReject, ID: "B-01KYZZRECVRY00000000000000",
		Note: "orphaned create " + orphan + " closed for the record",
	}); err != nil {
		t.Fatal(err)
	}
	if got := s.orphanedItems(); len(got) != 0 {
		t.Fatalf("citation must close the orphan: %v", got)
	}
}
