package evidence

import (
	"strings"
	"testing"

	"github.com/jxsl13/spectackle/internal/graph"
)

func fixtureGraph() graph.Graph {
	g := graph.NewMem()
	// B-0009 shape: Orphan is exported under the target path with zero
	// inbound edges from outside its own file; Used has a consumer.
	nodes := []graph.Node{
		{ID: "go:tgt.Orphan", Kind: graph.KFunc, File: "tgt/a.go", Line: 10},
		{ID: "go:tgt.Used", Kind: graph.KFunc, File: "tgt/a.go", Line: 20},
		{ID: "go:other.Caller", Kind: graph.KFunc, File: "other/b.go", Line: 5},
	}
	edges := []graph.Edge{
		{Src: "go:other.Caller", Dst: "go:tgt.Used", Kind: graph.ECall, File: "other/b.go", Line: 7},
	}
	g.Upsert(nodes, edges)
	return g
}

// RED-RUN (T-01KYD88KE, written first): the B-0009 class — declared,
// exported, never consumed — is reported; a consumer clears it; an
// unconsumed-ok directive suppresses it visibly; a stale directive is
// itself flagged.
func TestUnconsumedSweep(t *testing.T) {
	g := fixtureGraph()
	recs := Unconsumed(g, []string{"tgt"}, nil)
	joined := strings.Join(recs, "\n")
	if !strings.Contains(joined, "e unconsumed go:tgt.Orphan") {
		t.Fatalf("orphan not reported: %q", joined)
	}
	if strings.Contains(joined, "tgt.Used") {
		t.Fatalf("consumed symbol reported: %q", joined)
	}
	recs = Unconsumed(g, []string{"tgt"}, map[string]string{"go:tgt.Orphan": "reflection lookup target"})
	joined = strings.Join(recs, "\n")
	if !strings.Contains(joined, "e suppressed go:tgt.Orphan reflection lookup target") {
		t.Fatalf("suppression not visible: %q", joined)
	}
	if strings.Contains(joined, "e unconsumed go:tgt.Orphan") {
		t.Fatalf("suppressed symbol still counted: %q", joined)
	}
	recs = Unconsumed(g, []string{"tgt"}, map[string]string{"go:tgt.Used": "no longer true"})
	if !strings.Contains(strings.Join(recs, "\n"), "e stale-suppress go:tgt.Used") {
		t.Fatalf("stale suppression not flagged: %v", recs)
	}
}

// B-0003 fixture: 21 call sites, one with a divergent shape — exactly that
// site reports; a 10/10 split reports nothing.
func TestDivergentCallers(t *testing.T) {
	g := graph.NewMem()
	nodes := []graph.Node{{ID: "go:tgt.Popular", Kind: graph.KFunc, File: "tgt/a.go", Line: 5}}
	var edges []graph.Edge
	src := "package callers\n\nfunc use() {\n"
	line := 4
	for i := 0; i < 20; i++ {
		src += "\tPopular(dir)\n"
		edges = append(edges, graph.Edge{Src: "go:callers.use", Dst: "go:tgt.Popular", Kind: graph.ECall, File: "callers/c.go", Line: line})
		line++
	}
	src += "\tPopular(itemID())\n"
	edges = append(edges, graph.Edge{Src: "go:callers.use", Dst: "go:tgt.Popular", Kind: graph.ECall, File: "callers/c.go", Line: line})
	src += "}\n"
	g.Upsert(nodes, edges)
	load := func(path string) []byte {
		if path == "callers/c.go" {
			return []byte(src)
		}
		return nil
	}
	recs := DivergentCallers(g, []string{"tgt"}, load)
	joined := strings.Join(recs, "\n")
	if !strings.Contains(joined, "e divergent go:tgt.Popular 1/21 sites differ") {
		t.Fatalf("B-0003 shape not reported: %q", joined)
	}
	// 10/10 split: no minority
	g2 := graph.NewMem()
	g2.Upsert(nodes, nil)
	var edges2 []graph.Edge
	src2 := "package callers\n\nfunc use() {\n"
	line = 4
	for i := 0; i < 10; i++ {
		src2 += "\tPopular(dir)\n"
		edges2 = append(edges2, graph.Edge{Src: "go:callers.use", Dst: "go:tgt.Popular", Kind: graph.ECall, File: "callers/c.go", Line: line})
		line++
	}
	for i := 0; i < 10; i++ {
		src2 += "\tPopular(itemID())\n"
		edges2 = append(edges2, graph.Edge{Src: "go:callers.use", Dst: "go:tgt.Popular", Kind: graph.ECall, File: "callers/c.go", Line: line})
		line++
	}
	src2 += "}\n"
	g2.Upsert(nodes, edges2)
	load2 := func(path string) []byte { return []byte(src2) }
	if recs := DivergentCallers(g2, []string{"tgt"}, load2); len(recs) != 0 {
		t.Fatalf("50/50 split reported: %v", recs)
	}
}

// Caps and determinism hold; non-Go loads skip cleanly.
func TestEvidenceCapsAndDeterminism(t *testing.T) {
	g := graph.NewMem()
	var nodes []graph.Node
	for i := 0; i < 30; i++ {
		nodes = append(nodes, graph.Node{
			ID:   graph.NodeID(strings.Join([]string{"go:tgt.Orphan", string(rune('A' + i%26)), string(rune('A' + i/26))}, "")),
			Kind: graph.KFunc, File: "tgt/a.go", Line: i + 1})
	}
	g.Upsert(nodes, nil)
	a := strings.Join(Unconsumed(g, []string{"tgt"}, nil), "\n")
	b := strings.Join(Unconsumed(g, []string{"tgt"}, nil), "\n")
	if a != b {
		t.Fatal("nondeterministic output")
	}
	if !strings.Contains(a, "e +20 more") {
		t.Fatalf("cap missing: %q", a)
	}
	// non-Go: loader returns bytes that fail to parse — skip, no findings
	gp := graph.NewMem()
	gp.Upsert([]graph.Node{{ID: "cu:k.Kern", Kind: graph.KFunc, File: "tgt/k.cu", Line: 1}}, []graph.Edge{
		{Src: "cu:m.a", Dst: "cu:k.Kern", Kind: graph.ECall, File: "tgt/m.cu", Line: 3},
		{Src: "cu:m.b", Dst: "cu:k.Kern", Kind: graph.ECall, File: "tgt/m.cu", Line: 4},
		{Src: "cu:m.c", Dst: "cu:k.Kern", Kind: graph.ECall, File: "tgt/m.cu", Line: 5},
		{Src: "cu:m.d", Dst: "cu:k.Kern", Kind: graph.ECall, File: "tgt/m.cu", Line: 6},
		{Src: "cu:m.e", Dst: "cu:k.Kern", Kind: graph.ECall, File: "tgt/m.cu", Line: 7},
	})
	if recs := DivergentCallers(gp, []string{"tgt"}, func(string) []byte { return []byte("__global__ void k(){}") }); len(recs) != 0 {
		t.Fatalf("non-Go did not skip cleanly: %v", recs)
	}
}
