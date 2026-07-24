package index

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jxsl13/spectackle/internal/graph"
	"github.com/jxsl13/spectackle/internal/resolve"
	"github.com/jxsl13/spectackle/internal/store"
)

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	abs := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newTestIndexer(g graph.Graph) Indexer {
	return New(g, store.NewMem(), []LanguageParser{GoParser{}}, resolve.Default().All())
}

func TestIndexAllEndToEnd(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "pkg/a.go", `package pkg

import "fmt"

func Alpha() {
	Beta()
	fmt.Println("x")
}

func Beta() {}
`)
	writeFile(t, root, "cgodemo/cgo.go", `package cgodemo

/*
#include "foo.h"
*/
import "C"

func Launch(n int) error {
	if rc := C.launch_foo(C.int(n)); rc != 0 {
		return nil
	}
	return nil
}
`)

	g := graph.NewMem()
	st, err := newTestIndexer(g).IndexAll(context.Background(), root)
	if err != nil {
		t.Fatalf("IndexAll: %v", err)
	}
	if st.Files != 2 || st.Skipped != 0 {
		t.Errorf("Stats = %+v, want Files=2 Skipped=0", st)
	}

	// Go nodes present.
	for _, id := range []graph.NodeID{"go:pkg.Alpha", "go:pkg.Beta", "go:cgodemo.Launch"} {
		if _, ok := g.Node(id); !ok {
			t.Errorf("node %s missing from graph", id)
		}
	}

	// ECgo edge from the Go func to the C symbol.
	cgoEdges := g.Neighbors("go:cgodemo.Launch", graph.Out, []graph.EdgeKind{graph.ECgo})
	found := false
	for _, e := range cgoEdges {
		if e.Dst == "c:launch_foo" {
			found = true
		}
	}
	if !found {
		t.Errorf("Neighbors(go:cgodemo.Launch, Out, ECgo) = %+v, want edge to c:launch_foo", cgoEdges)
	}

	// c:launch_foo materialized as a stub node.
	stub, ok := g.Node("c:launch_foo")
	if !ok {
		t.Fatal("stub node c:launch_foo not materialized")
	}
	if stub.Lang != graph.LangC || stub.Kind != graph.KFunc {
		t.Errorf("stub = %+v, want Lang=c Kind=KFunc", stub)
	}

	// connected nodes carry a positive degree rank.
	for _, id := range []graph.NodeID{"go:pkg.Alpha", "go:pkg.Beta", "go:cgodemo.Launch", "c:launch_foo"} {
		n, ok := g.Node(id)
		if !ok {
			continue // reported above
		}
		if n.Rank <= 0 {
			t.Errorf("%s: Rank = %v, want > 0", id, n.Rank)
		}
	}

	// Alpha -> Beta call edge survived, at the call site line.
	callEdges := g.Neighbors("go:pkg.Alpha", graph.Out, []graph.EdgeKind{graph.ECall})
	if len(callEdges) != 1 || callEdges[0].Dst != "go:pkg.Beta" {
		t.Errorf("Alpha call edges = %+v, want exactly one to go:pkg.Beta", callEdges)
	}
}

// Call candidates whose target was never defined (fmt.Println) must be pruned
// and must not leave a phantom node behind.
func TestIndexAllPrunesUndefinedCallees(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "p/p.go", `package p

import "fmt"

func F() {
	fmt.Println("hi")
	undefinedLocal()
}
`)
	g := graph.NewMem()
	if _, err := newTestIndexer(g).IndexAll(context.Background(), root); err != nil {
		t.Fatalf("IndexAll: %v", err)
	}
	if es := g.Neighbors("go:p.F", graph.Out, nil); len(es) != 0 {
		t.Errorf("go:p.F outgoing edges = %+v, want none (all callees undefined)", es)
	}
	for _, id := range []graph.NodeID{"go:fmt.Println", "go:p.undefinedLocal"} {
		if _, ok := g.Node(id); ok {
			t.Errorf("phantom node %s exists, want pruned", id)
		}
	}
}

// Two definitions minting the same ID: the file-path-first one keeps the base
// ID, the second gets the ~2 suffix (deterministic, SPX-GRA-001).
func TestIndexAllCollisionSuffix(t *testing.T) {
	const body = `package col

func Dup() {}
`
	root := t.TempDir()
	writeFile(t, root, "a.go", body)
	writeFile(t, root, "b.go", body)

	g := graph.NewMem()
	if _, err := newTestIndexer(g).IndexAll(context.Background(), root); err != nil {
		t.Fatalf("IndexAll: %v", err)
	}
	first, ok := g.Node("go:col.Dup")
	if !ok {
		t.Fatal("base node go:col.Dup missing")
	}
	second, ok := g.Node("go:col.Dup~2")
	if !ok {
		t.Fatal("collision node go:col.Dup~2 missing")
	}
	if first.File != "a.go" || second.File != "b.go" {
		t.Errorf("collision order not file-path deterministic: base in %q, ~2 in %q (want a.go, b.go)",
			first.File, second.File)
	}
	if _, ok := g.Node("go:col.Dup~3"); ok {
		t.Error("unexpected go:col.Dup~3 node")
	}
}

// The shipped example repo is the reference Go -> C -> CUDA chain; with only
// the Go parser active it must still yield the Saxpy node and its cgo edge.
func TestIndexAllSaxpyExample(t *testing.T) {
	root, err := filepath.Abs(filepath.FromSlash("../../examples/saxpy"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(root); err != nil {
		t.Skipf("examples/saxpy not available: %v", err)
	}

	g := graph.NewMem()
	if _, err := newTestIndexer(g).IndexAll(context.Background(), root); err != nil {
		t.Fatalf("IndexAll(%s): %v", root, err)
	}

	n, ok := g.Node("go:saxpy.Saxpy")
	if !ok {
		t.Fatal("node go:saxpy.Saxpy missing")
	}
	if want := "(n int,a float32,x,y []float32)error"; n.Sig != want {
		t.Errorf("Saxpy Sig = %q, want %q", n.Sig, want)
	}

	cgo := g.Neighbors("go:saxpy.Saxpy", graph.Out, []graph.EdgeKind{graph.ECgo})
	found := false
	for _, e := range cgo {
		if e.Dst == "c:launch_saxpy" {
			found = true
		}
	}
	if !found {
		t.Errorf("Neighbors(go:saxpy.Saxpy, Out, ECgo) = %+v, want edge to c:launch_saxpy", cgo)
	}
}

// IndexAll must honor config.yaml-style ignore globs passed to New: a file
// under an ignored glob is excluded from the graph, and the same tree indexed
// without the ignore glob includes it (T-0026).
func TestIndexAllRespectsIgnoreGlobs(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "gen/x.go", `package gen

func Generated() {}
`)
	writeFile(t, root, "keep/y.go", `package keep

func Kept() {}
`)

	// with the ignore glob: gen/x.go is excluded, keep/y.go still indexed.
	gIgnored := graph.NewMem()
	ixIgnored := New(gIgnored, store.NewMem(), []LanguageParser{GoParser{}}, resolve.Default().All(), "gen/**")
	st, err := ixIgnored.IndexAll(context.Background(), root)
	if err != nil {
		t.Fatalf("IndexAll (ignored): %v", err)
	}
	if st.Files != 1 {
		t.Errorf("Stats.Files = %d, want 1 (gen/x.go excluded)", st.Files)
	}
	if _, ok := gIgnored.Node("go:gen.Generated"); ok {
		t.Error("go:gen.Generated present, want excluded by ignore glob gen/**")
	}
	if _, ok := gIgnored.Node("go:keep.Kept"); !ok {
		t.Error("go:keep.Kept missing, want present (not covered by ignore glob)")
	}

	// without the ignore glob: both files are indexed.
	gAll := graph.NewMem()
	ixAll := newTestIndexer(gAll)
	if _, err := ixAll.IndexAll(context.Background(), root); err != nil {
		t.Fatalf("IndexAll (no ignore): %v", err)
	}
	if _, ok := gAll.Node("go:gen.Generated"); !ok {
		t.Error("go:gen.Generated missing, want present without an ignore glob")
	}
	if _, ok := gAll.Node("go:keep.Kept"); !ok {
		t.Error("go:keep.Kept missing, want present")
	}
}

func TestMatchIgnore(t *testing.T) {
	cases := []struct {
		globs []string
		rel   string
		want  bool
	}{
		{[]string{"**"}, "anything/at/all.go", true},
		{[]string{"**"}, "top.go", true},
		{[]string{"bin/**"}, "bin/tool", true},
		{[]string{"bin/**"}, "bin/sub/deep.go", true},
		{[]string{"bin/**"}, "bin", true}, // trailing /** matches the dir itself
		{[]string{"bin/**"}, "binx/tool", false},
		{[]string{"bin/**"}, "src/bin/tool", false},
		{[]string{"**/*.tmp"}, "a.tmp", true},
		{[]string{"**/*.tmp"}, "x/y/a.tmp", true},
		{[]string{"**/*.tmp"}, "a.tmpx", false},
		{[]string{"**/*.tmp"}, "a.tmp/keep.go", false},
		{[]string{"bin/**", "**/*.tmp"}, "x/a.tmp", true}, // any glob suffices
		{nil, "anything.go", false},
	}
	for _, c := range cases {
		if got := MatchIgnore(c.globs, c.rel); got != c.want {
			t.Errorf("MatchIgnore(%v, %q) = %v, want %v", c.globs, c.rel, got, c.want)
		}
	}
}
