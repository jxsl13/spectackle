package index

import (
	"context"
	"path/filepath"
	"sync"
	"testing"

	"github.com/jxsl13/spectacle/internal/graph"
	"github.com/jxsl13/spectacle/internal/resolve"
	"github.com/jxsl13/spectacle/internal/store"
)

// countingParser wraps a LanguageParser and counts Parse invocations, so
// tests can assert the store.Store cache actually short-circuits re-parsing.
type countingParser struct {
	LanguageParser
	mu    sync.Mutex
	calls int
}

func (c *countingParser) Parse(path string, src []byte) (ParseResult, error) {
	c.mu.Lock()
	c.calls++
	c.mu.Unlock()
	return c.LanguageParser.Parse(path, src)
}

func (c *countingParser) Calls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

// TestIndexAllCacheAcceleratesSecondRun is the load-bearing proof for T-0016:
// with a persistent store shared across two Indexer instances, the second
// IndexAll of an unchanged tree parses zero files yet reproduces identical
// node/edge counts.
func TestIndexAllCacheAcceleratesSecondRun(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "pkg/a.go", `package pkg

import "fmt"

func Alpha() {
	Beta()
	fmt.Println("x")
}

func Beta() {}
`)
	writeFile(t, root, "pkg/b.go", `package pkg

func Gamma() {
	Beta()
}
`)

	dbPath := filepath.Join(t.TempDir(), "cache", "parse.db")
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer s.Close()

	// 1. cold run: every file must be parsed.
	cp1 := &countingParser{LanguageParser: GoParser{}}
	g1 := graph.NewMem()
	ix1 := New(g1, s, []LanguageParser{cp1}, resolve.Default().All())
	st1, err := ix1.IndexAll(context.Background(), root)
	if err != nil {
		t.Fatalf("IndexAll (cold): %v", err)
	}
	if cp1.Calls() != 2 {
		t.Fatalf("cold IndexAll Parse calls = %d, want 2", cp1.Calls())
	}

	// 2. IndexAll with the same store, on an unchanged tree, and a fresh
	// Indexer/graph: must do zero parses yet reproduce identical counts.
	cp2 := &countingParser{LanguageParser: GoParser{}}
	g2 := graph.NewMem()
	ix2 := New(g2, s, []LanguageParser{cp2}, resolve.Default().All())
	st2, err := ix2.IndexAll(context.Background(), root)
	if err != nil {
		t.Fatalf("IndexAll (warm): %v", err)
	}
	if got := cp2.Calls(); got != 0 {
		t.Fatalf("2. IndexAll Parse calls = %d, want 0 (cache-accelerated)", got)
	}
	if st2.Nodes != st1.Nodes || st2.Edges != st1.Edges || st2.Files != st1.Files {
		t.Fatalf("warm Stats = %+v, want identical to cold Stats = %+v", st2, st1)
	}

	// graphs built from the two runs must agree on content, not just counts.
	for _, id := range []graph.NodeID{"go:pkg.Alpha", "go:pkg.Beta", "go:pkg.Gamma"} {
		n1, ok1 := g1.Node(id)
		n2, ok2 := g2.Node(id)
		if ok1 != ok2 {
			t.Fatalf("node %s presence mismatch: cold=%v warm=%v", id, ok1, ok2)
		}
		if ok1 && (n1.Kind != n2.Kind || n1.Lang != n2.Lang || n1.File != n2.File || n1.Line != n2.Line || n1.Sig != n2.Sig) {
			t.Errorf("node %s mismatch: cold=%+v warm=%+v", id, n1, n2)
		}
	}
}

// TestParseCachedHitAndMiss exercises parseCached directly: a cache hit skips
// the parser, and changed content (different hash) forces a reparse and
// overwrites the cached blob.
func TestParseCachedHitAndMiss(t *testing.T) {
	s := store.NewMem()
	defer s.Close()

	cp := &countingParser{LanguageParser: GoParser{}}
	src1 := []byte("package p\n\nfunc F() {}\n")

	pr1, err := parseCached(s, cp, "p.go", src1)
	if err != nil {
		t.Fatalf("parseCached (miss): %v", err)
	}
	if cp.Calls() != 1 {
		t.Fatalf("Parse calls after cold parseCached = %d, want 1", cp.Calls())
	}

	pr2, err := parseCached(s, cp, "p.go", src1)
	if err != nil {
		t.Fatalf("parseCached (hit): %v", err)
	}
	if cp.Calls() != 1 {
		t.Fatalf("Parse calls after cached parseCached = %d, want still 1", cp.Calls())
	}
	if len(pr2.Nodes) != len(pr1.Nodes) || pr2.Hash != pr1.Hash {
		t.Fatalf("cached ParseResult = %+v, want it to match the fresh parse %+v", pr2, pr1)
	}

	// changed content -> different hash -> forced reparse.
	src2 := []byte("package p\n\nfunc F() {}\n\nfunc G() {}\n")
	pr3, err := parseCached(s, cp, "p.go", src2)
	if err != nil {
		t.Fatalf("parseCached (changed content): %v", err)
	}
	if cp.Calls() != 2 {
		t.Fatalf("Parse calls after content change = %d, want 2", cp.Calls())
	}
	if len(pr3.Nodes) != 2 {
		t.Fatalf("reparsed Nodes = %d, want 2 (F and G)", len(pr3.Nodes))
	}
}

// A nil store must not panic and must simply always parse.
func TestParseCachedNilStore(t *testing.T) {
	cp := &countingParser{LanguageParser: GoParser{}}
	src := []byte("package p\n\nfunc F() {}\n")
	if _, err := parseCached(nil, cp, "p.go", src); err != nil {
		t.Fatalf("parseCached(nil store): %v", err)
	}
	if _, err := parseCached(nil, cp, "p.go", src); err != nil {
		t.Fatalf("parseCached(nil store) 2nd call: %v", err)
	}
	if cp.Calls() != 2 {
		t.Fatalf("Parse calls with nil store = %d, want 2 (no caching possible)", cp.Calls())
	}
}
