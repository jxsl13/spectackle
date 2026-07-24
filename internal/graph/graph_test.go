package graph

import "testing"

// diamond builds: a -> b -> d, a -> c -> d (calls), plus x isolated.
func diamond() Graph {
	g := NewMem()
	g.Upsert([]Node{
		{ID: "go:a", Kind: KFunc, File: "a.go", Line: 1},
		{ID: "go:b", Kind: KFunc, File: "b.go", Line: 1},
		{ID: "c:c", Kind: KFunc, File: "c.c", Line: 1},
		{ID: "cu:d", Kind: KKernel, File: "d.cu", Line: 1},
		{ID: "go:x", Kind: KType, File: "x.go", Line: 1},
	}, []Edge{
		{Src: "go:a", Dst: "go:b", Kind: ECall, File: "a.go", Line: 2},
		{Src: "go:a", Dst: "c:c", Kind: ECgo, File: "a.go", Line: 3},
		{Src: "go:b", Dst: "cu:d", Kind: ELaunch, File: "b.go", Line: 2},
		{Src: "c:c", Dst: "cu:d", Kind: ELaunch, File: "c.c", Line: 2},
	})
	return g
}

func TestNodeAndFind(t *testing.T) {
	g := diamond()
	if _, ok := g.Node("go:a"); !ok {
		t.Fatal("Node lookup failed")
	}
	if _, ok := g.Node("go:missing"); ok {
		t.Fatal("missing node reported present")
	}
	// kind filter + k limit
	hits := g.Find("go", 10, KFunc)
	if len(hits) != 2 {
		t.Fatalf("Find kind filter: %d hits", len(hits))
	}
	if hits := g.Find("a", 1, KUnknown); len(hits) != 1 {
		t.Fatalf("Find k limit: %d hits", len(hits))
	}
	// exact suffix match ranks first
	hits = g.Find("d", 5, KUnknown)
	if len(hits) == 0 || hits[0].ID != "cu:d" {
		t.Fatalf("suffix ranking: %+v", hits)
	}
}

func TestFindFileAndSig(t *testing.T) {
	// SPX-GRA-003: Find matches ID, file path and signature; ID hits rank
	// above file- or signature-only hits.
	g := diamond()
	g.Upsert([]Node{
		{ID: "go:pkg.Launch", Kind: KFunc, File: "pkg/launch.go", Line: 3,
			Sig: "(n int,a float32)error"},
	}, nil)

	// file-path query: nothing in the ID says "launch.go"
	hits := g.Find("launch.go", 5, KUnknown)
	if len(hits) != 1 || hits[0].ID != "go:pkg.Launch" {
		t.Fatalf("file-path match: %+v", hits)
	}
	// signature fragment
	hits = g.Find("float32", 5, KUnknown)
	if len(hits) != 1 || hits[0].ID != "go:pkg.Launch" {
		t.Fatalf("signature match: %+v", hits)
	}
	// ranking: query "a" hits go:a by ID (suffix) and pkg/launch.go by sig
	// ("a float32") — the ID hit must come first
	hits = g.Find("a", 10, KUnknown)
	if len(hits) < 2 || hits[0].ID != "go:a" {
		t.Fatalf("ID hit must rank above sig-only hit: %+v", hits)
	}
	// no match anywhere stays empty
	if hits := g.Find("zzz-nowhere", 5, KUnknown); len(hits) != 0 {
		t.Fatalf("unexpected hits: %+v", hits)
	}
}

func TestNeighbors(t *testing.T) {
	g := diamond()
	if es := g.Neighbors("go:a", Out, nil); len(es) != 2 {
		t.Fatalf("Out neighbors of a: %d", len(es))
	}
	if es := g.Neighbors("cu:d", In, nil); len(es) != 2 {
		t.Fatalf("In neighbors of d: %d", len(es))
	}
	if es := g.Neighbors("go:a", Out, []EdgeKind{ECgo}); len(es) != 1 || es[0].Dst != "c:c" {
		t.Fatalf("kind-filtered neighbors: %+v", es)
	}
}

func TestImpactBFS(t *testing.T) {
	g := diamond()
	// depth 1: only direct callees
	nodes, _ := g.Impact([]NodeID{"go:a"}, 1, Out, nil)
	if len(nodes) != 3 { // a, b, c
		t.Fatalf("depth-1 radius: %d nodes", len(nodes))
	}
	// depth 2 diamond: d reachable via two paths but appears exactly once (SPX-GRA-002)
	nodes, edges := g.Impact([]NodeID{"go:a"}, 2, Out, nil)
	if len(nodes) != 4 {
		t.Fatalf("depth-2 radius: %d nodes", len(nodes))
	}
	seen := map[NodeID]int{}
	for _, n := range nodes {
		seen[n.ID]++
	}
	if seen["cu:d"] != 1 {
		t.Fatalf("diamond node counted %d times", seen["cu:d"])
	}
	if len(edges) != 4 {
		t.Fatalf("depth-2 edges: %d", len(edges))
	}
	// edge-kind filter cuts the cgo branch
	nodes, _ = g.Impact([]NodeID{"go:a"}, 2, Out, []EdgeKind{ECall, ELaunch})
	for _, n := range nodes {
		if n.ID == "c:c" {
			t.Fatal("kind filter leaked the cgo branch")
		}
	}
	// In direction walks callers
	nodes, _ = g.Impact([]NodeID{"cu:d"}, 2, In, nil)
	if len(nodes) != 4 {
		t.Fatalf("reverse radius: %d nodes", len(nodes))
	}
	// unknown seeds are ignored, not fatal
	if nodes, _ := g.Impact([]NodeID{"go:missing"}, 2, Both, nil); len(nodes) != 0 {
		t.Fatalf("unknown seed produced %d nodes", len(nodes))
	}
}

func TestKindStrings(t *testing.T) {
	if ECgo.String() != "cgo" || EdgeKindFromString("cgo") != ECgo {
		t.Fatal("edge kind string roundtrip broken")
	}
	if EdgeKindFromString("nope") != EUnknown {
		t.Fatal("unknown edge kind must map to EUnknown")
	}
	if KKernel.String() != "kernel" {
		t.Fatalf("node kind string: %q", KKernel.String())
	}
}
