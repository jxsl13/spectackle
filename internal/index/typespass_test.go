package index

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jxsl13/spectacle/internal/graph"
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

	added, err := ResolveTypedCalls(context.Background(), g, root)
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

	first, err := ResolveTypedCalls(context.Background(), g, root)
	if err != nil {
		t.Fatalf("ResolveTypedCalls (1st): %v", err)
	}
	if first == 0 {
		t.Fatalf("first run added 0 edges, want >= 1")
	}

	second, err := ResolveTypedCalls(context.Background(), g, root)
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

// TestResolveTypedCallsRepo is the design doc's real-world regression test
// (§4/§1): the actual spectacle repo has s.cd.Sweep() in
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

	added, err := ResolveTypedCalls(context.Background(), g, root)
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
