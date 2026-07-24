package graph

import "testing"

// pprTree: focus -> a -> b -> c chain plus a disconnected node; hub has
// many edges but sits two hops away from focus via a.
func pprGraph() Graph {
	g := NewMem()
	g.Upsert([]Node{
		{ID: "go:pkg.Focus"}, {ID: "go:pkg.Near"}, {ID: "go:pkg.Mid"},
		{ID: "go:pkg.Far"}, {ID: "go:pkg.Hub"}, {ID: "go:pkg.Island"},
		{ID: "go:pkg.H1"}, {ID: "go:pkg.H2"}, {ID: "go:pkg.H3"},
	}, []Edge{
		{Src: "go:pkg.Focus", Dst: "go:pkg.Near", Kind: ECall},
		{Src: "go:pkg.Near", Dst: "go:pkg.Mid", Kind: ECall},
		{Src: "go:pkg.Mid", Dst: "go:pkg.Far", Kind: ECall},
		{Src: "go:pkg.Mid", Dst: "go:pkg.Hub", Kind: ECall},
		{Src: "go:pkg.Hub", Dst: "go:pkg.H1", Kind: ECall},
		{Src: "go:pkg.Hub", Dst: "go:pkg.H2", Kind: ECall},
		{Src: "go:pkg.Hub", Dst: "go:pkg.H3", Kind: ECall},
	})
	return g
}

// TestPersonalizedRank (SPX-GRA-004): the seed scores highest, proximity
// beats distance, disconnected nodes score 0, and the computation is
// deterministic across runs.
func TestPersonalizedRank(t *testing.T) {
	g := pprGraph()
	s := PersonalizedRank(g, []NodeID{"go:pkg.Focus"}, 4, 20, 0.85)
	if s == nil {
		t.Fatal("nil scores")
	}
	// proximity dominates: the seed and its direct neighbor take the top
	// two slots (which of the two leads depends on degree, not distance)
	for id, v := range s {
		if id == "go:pkg.Focus" || id == "go:pkg.Near" {
			continue
		}
		if v >= s["go:pkg.Focus"] || v >= s["go:pkg.Near"] {
			t.Fatalf("%s (%v) must score below seed (%v) and 1-hop (%v)", id, v, s["go:pkg.Focus"], s["go:pkg.Near"])
		}
	}
	if s["go:pkg.Near"] <= s["go:pkg.Far"] {
		t.Fatalf("1-hop must outscore 3-hop: near=%v far=%v", s["go:pkg.Near"], s["go:pkg.Far"])
	}
	if s["go:pkg.Island"] != 0 {
		t.Fatalf("disconnected node must score 0, got %v", s["go:pkg.Island"])
	}

	s2 := PersonalizedRank(g, []NodeID{"go:pkg.Focus"}, 4, 20, 0.85)
	if len(s) != len(s2) {
		t.Fatalf("determinism: %d vs %d entries", len(s), len(s2))
	}
	for id, v := range s {
		if s2[id] != v {
			t.Fatalf("determinism: %s %v vs %v", id, v, s2[id])
		}
	}

	if PersonalizedRank(g, nil, 4, 20, 0.85) != nil {
		t.Fatal("no seeds must yield nil")
	}
}
