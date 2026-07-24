package index

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/jxsl13/spectackle/internal/graph"
	"github.com/jxsl13/spectackle/internal/store"
	"github.com/jxsl13/spectackle/internal/workspace"
)

// TestResolveTypedCallsChainedSelector reproduces the design doc's exit
// criterion in miniature: a call through a chained selector (s.d.Sweep(),
// s.d being a *b.D field) is invisible to GoParser's syntactic callEdges
// (only a plain x.Sel() call resolves there) but must be found by the
// go/types upgrade pass, which flattens the field hop to the concrete
// method regardless of how it is spelled in source.
func TestResolveTypedCallsChainedSelector(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/m\n\ngo 1.25\n")
	writeFile(t, root, "a/a.go", `package a

import "example.com/m/b"

type S struct {
	d *b.D
}

func (s *S) Run() {
	s.d.Sweep()
}
`)
	writeFile(t, root, "b/b.go", `package b

type D struct{}

func (d *D) Sweep() {}
`)

	g := graph.NewMem()
	if _, err := newTestIndexer(g).IndexAll(context.Background(), root); err != nil {
		t.Fatalf("IndexAll: %v", err)
	}

	// Sanity: the syntactic pass alone must NOT have found this edge — that
	// is exactly the gap this pass exists to close (design doc §1).
	before := g.Neighbors("go:a.S.Run", graph.Out, []graph.EdgeKind{graph.ECall})
	for _, e := range before {
		if e.Dst == "go:b.D.Sweep" {
			t.Fatalf("go:a.S.Run -> go:b.D.Sweep already present before ResolveTypedCalls; test no longer exercises the gap")
		}
	}

	added, err := ResolveTypedCalls(context.Background(), g, root, nil)
	if err != nil {
		t.Fatalf("ResolveTypedCalls: %v", err)
	}
	if added == 0 {
		t.Fatalf("ResolveTypedCalls added 0 edges, want >= 1")
	}

	edges := g.Neighbors("go:a.S.Run", graph.Out, []graph.EdgeKind{graph.ECall})
	found := false
	for _, e := range edges {
		if e.Dst == "go:b.D.Sweep" {
			found = true
			if e.File != "a/a.go" {
				t.Errorf("edge File = %q, want a/a.go", e.File)
			}
			if e.Line != 10 {
				t.Errorf("edge Line = %d, want 10", e.Line)
			}
		}
	}
	if !found {
		t.Fatalf("Neighbors(go:a.S.Run, Out, ECall) = %+v, missing go:b.D.Sweep", edges)
	}
}

// TestResolveTypedCallsDeterministic runs the pass twice over the same
// graph: the second run must add zero new edges. memGraph.Upsert appends
// rather than replaces, so ResolveTypedCalls itself is responsible for
// deduping against edges a prior run already added.
func TestResolveTypedCallsDeterministic(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/m\n\ngo 1.25\n")
	writeFile(t, root, "a/a.go", `package a

import "example.com/m/b"

type S struct {
	d *b.D
}

func (s *S) Run() {
	s.d.Sweep()
	s.d.Sweep() // duplicate call site: still one edge
}
`)
	writeFile(t, root, "b/b.go", `package b

type D struct{}

func (d *D) Sweep() {}
`)

	g := graph.NewMem()
	if _, err := newTestIndexer(g).IndexAll(context.Background(), root); err != nil {
		t.Fatalf("IndexAll: %v", err)
	}

	first, err := ResolveTypedCalls(context.Background(), g, root, nil)
	if err != nil {
		t.Fatalf("ResolveTypedCalls (1st): %v", err)
	}
	if first == 0 {
		t.Fatalf("first run added 0 edges, want >= 1")
	}

	second, err := ResolveTypedCalls(context.Background(), g, root, nil)
	if err != nil {
		t.Fatalf("ResolveTypedCalls (2nd): %v", err)
	}
	if second != 0 {
		t.Errorf("second run added %d edges, want 0 (dedupe against existing graph state)", second)
	}

	edges := g.Neighbors("go:a.S.Run", graph.Out, []graph.EdgeKind{graph.ECall})
	n := 0
	for _, e := range edges {
		if e.Dst == "go:b.D.Sweep" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("go:a.S.Run -> go:b.D.Sweep appears %d times, want exactly 1 (in-pass + cross-run dedupe)", n)
	}
}

// TestResolveTypedCallsCacheHitSkipsLoad is the module-hash cache's proof:
// with a store and an unchanged tree, a second ResolveTypedCalls call must
// reproduce the first call's result WITHOUT ever invoking packages.Load
// again. Comparing timings would be flaky and comparing "still succeeds
// after go.mod gets corrupted" wouldn't isolate the cache (the cache key
// itself is derived from go.mod's contents, so corrupting it changes the key
// and would force a real reload). Instead this stubs the package-level
// loadPackages var — swapped in only for the second call — to unconditionally
// fail; the second call succeeding proves loadPackages was never called.
func TestResolveTypedCallsCacheHitSkipsLoad(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/m\n\ngo 1.25\n")
	writeFile(t, root, "a/a.go", `package a

import "example.com/m/b"

type S struct {
	d *b.D
}

func (s *S) Run() {
	s.d.Sweep()
}
`)
	writeFile(t, root, "b/b.go", `package b

type D struct{}

func (d *D) Sweep() {}
`)

	s := store.NewMem()
	ctx := context.Background()

	// First call: real loadPackages, populates the cache on success.
	g1 := graph.NewMem()
	if _, err := newTestIndexer(g1).IndexAll(ctx, root); err != nil {
		t.Fatalf("IndexAll (1st): %v", err)
	}
	first, err := ResolveTypedCalls(ctx, g1, root, s)
	if err != nil {
		t.Fatalf("ResolveTypedCalls (1st, cold cache): %v", err)
	}
	if first == 0 {
		t.Fatalf("first run added 0 edges, want >= 1")
	}

	// Sentinel: swap loadPackages with a stub that always fails. If the
	// second call reaches it at all, the test fails with this exact error --
	// making a cache miss (i.e. "the cache didn't work") unmistakable in the
	// failure message rather than merely inferred from a mismatched count.
	loadFailed := errors.New("sentinel: loadPackages must not be called on a cache hit")
	prev := loadPackages
	loadPackages = func(cfg *packages.Config, patterns ...string) ([]*packages.Package, error) {
		return nil, loadFailed
	}
	defer func() { loadPackages = prev }()

	// Second call: same tree (same module hash key), fresh graph, same store.
	// A cache hit decodes the cached edge list and applies it directly.
	g2 := graph.NewMem()
	if _, err := newTestIndexer(g2).IndexAll(ctx, root); err != nil {
		t.Fatalf("IndexAll (2nd): %v", err)
	}
	second, err := ResolveTypedCalls(ctx, g2, root, s)
	if err != nil {
		t.Fatalf("ResolveTypedCalls (2nd, should be a cache hit): %v", err)
	}
	if second != first {
		t.Errorf("second run (cache hit) added %d edges, want %d (same as cold run)", second, first)
	}

	edges := g2.Neighbors("go:a.S.Run", graph.Out, []graph.EdgeKind{graph.ECall})
	found := false
	for _, e := range edges {
		if e.Dst == "go:b.D.Sweep" {
			found = true
		}
	}
	if !found {
		t.Fatalf("Neighbors(go:a.S.Run, Out, ECall) on the cache-hit graph = %+v, missing go:b.D.Sweep", edges)
	}
}

// TestModuleHashKeyNestedGitBoundary proves that a .go file under a nested
// git boundary (a linked worktree, submodule, or nested clone) does not
// affect the computed module hash key. The hash key must mirror IndexAll's
// walk, which skips nested git boundaries entirely so they never contribute
// to the cache key — this ensures a cache hit remains valid even if such
// foreign subdirectories change.
func TestModuleHashKeyNestedGitBoundary(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/m\n\ngo 1.25\n")
	writeFile(t, root, "main.go", `package main

func main() {}
`)

	// First hash: base tree with just main.go
	hash1, err := moduleHashKey(root)
	if err != nil {
		t.Fatalf("moduleHashKey (before nested boundary): %v", err)
	}

	// Create a nested git boundary: a directory with a .git file
	// (simulating a linked worktree or submodule, which have a .git FILE
	// with "gitdir: ..." content, not a .git DIRECTORY).
	nestedDir := filepath.Join(root, "nested")
	if err := os.Mkdir(nestedDir, 0755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}
	writeFile(t, nestedDir, ".git", "gitdir: /elsewhere/git\n")
	writeFile(t, nestedDir, "nested.go", `package nested

func Nested() {}
`)

	// Second hash: tree with the nested boundary and its .go file
	hash2, err := moduleHashKey(root)
	if err != nil {
		t.Fatalf("moduleHashKey (after nested boundary): %v", err)
	}

	// The hashes must be identical: the walk must have skipped the nested
	// boundary entirely, so nested.go never contributed to the key.
	if hash1 != hash2 {
		t.Errorf("module hash key changed after adding nested git boundary: %x -> %x", hash1, hash2)
	}

	// Sanity: verify that the nested boundary is actually detected as such.
	if !workspace.IsNestedGitBoundary(nestedDir) {
		t.Errorf("IsNestedGitBoundary(%s) = false, want true", nestedDir)
	}
}

// TestResolveTypedCallsRepo is the design doc's real-world regression test
// (§4/§1): the actual spectackle repo has s.cd.Sweep() in
// internal/mcpserver/swarm.go, a chained selector through a *coord.DB field,
// which the syntactic pass silently drops. Skipped under -short because it
// type-checks the whole repo module.
func TestResolveTypedCallsRepo(t *testing.T) {
	if testing.Short() {
		t.Skip("type-checks the whole repo module; skipped under -short")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "go.mod")); statErr != nil {
		t.Skipf("repo root not found at %s: %v", root, statErr)
	}

	g := graph.NewMem()
	if _, err := newTestIndexer(g).IndexAll(context.Background(), root); err != nil {
		t.Fatalf("IndexAll: %v", err)
	}

	added, err := ResolveTypedCalls(context.Background(), g, root, nil)
	if err != nil {
		t.Fatalf("ResolveTypedCalls: %v", err)
	}
	t.Logf("ResolveTypedCalls added %d edges over the repo", added)

	edges := g.Neighbors("go:coord.DB.Sweep", graph.In, []graph.EdgeKind{graph.ECall})
	found := false
	for _, e := range edges {
		if e.Src == "go:mcpserver.Server.preCall" {
			found = true
		}
	}
	if !found {
		t.Fatalf("Neighbors(go:coord.DB.Sweep, In, ECall) = %+v, missing go:mcpserver.Server.preCall", edges)
	}
}
