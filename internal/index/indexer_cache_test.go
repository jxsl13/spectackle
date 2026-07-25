package index

import (
	"context"
	"crypto/sha256"
	"path/filepath"
	"sync"
	"testing"

	"github.com/jxsl13/spectackle/internal/graph"
	"github.com/jxsl13/spectackle/internal/resolve"
	"github.com/jxsl13/spectackle/internal/store"
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

// versionedParser reports a caller-chosen CacheVersion, standing in for the
// same parser before and after an upgrade that changes its output.
type versionedParser struct {
	LanguageParser
	version string
	nodeID  graph.NodeID
}

func (v versionedParser) CacheVersion() string { return v.version }

func (v versionedParser) Parse(path string, src []byte) (ParseResult, error) {
	return ParseResult{Nodes: []graph.Node{{
		ID: v.nodeID, Kind: graph.KFunc, Lang: graph.LangGo, File: path, Line: 1,
	}}}, nil
}

// TestParseCachedInvalidatesOnParserVersion is B-0007's regression proof: an
// upgraded parser must not be served its predecessor's cached blob for
// unchanged bytes. Before the fix the key was the content alone, so every
// parser fix stayed invisible in an already-indexed workspace until the
// cache directory was deleted by hand.
func TestParseCachedInvalidatesOnParserVersion(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "parse.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	src := []byte("package p\n\nfunc A() {}\n")
	old := versionedParser{version: "v1", nodeID: "go:p.old"}
	if pr, pErr := parseCached(s, old, "p.go", src); pErr != nil || pr.Nodes[0].ID != "go:p.old" {
		t.Fatalf("cold parse with v1: %+v %v", pr, pErr)
	}
	if err := s.Flush(); err != nil {
		t.Fatal(err)
	}

	// same bytes, upgraded parser: the NEW result must win, with no cache
	// deletion anywhere.
	upgraded := versionedParser{version: "v2", nodeID: "go:p.new"}
	pr, err := parseCached(s, upgraded, "p.go", src)
	if err != nil {
		t.Fatal(err)
	}
	if pr.Nodes[0].ID != "go:p.new" {
		t.Fatalf("upgraded parser served a stale cached blob: got %s, want go:p.new", pr.Nodes[0].ID)
	}
}

// TestParseCachedStillHitsOnUnchangedVersion guards the other direction: the
// discriminator must not disable the optimization it protects.
func TestParseCachedStillHitsOnUnchangedVersion(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "parse.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// counted directly on the versioned type: countingParser embeds the
	// LanguageParser interface, which does not promote CacheVersion, so
	// wrapping would silently drop the very component under test.
	cp := &countingVersionedParser{version: "v1"}
	src := []byte("package p\n\nfunc A() {}\n")
	if _, err := parseCached(s, cp, "p.go", src); err != nil {
		t.Fatal(err)
	}
	if err := s.Flush(); err != nil {
		t.Fatal(err)
	}
	if _, err := parseCached(s, cp, "p.go", src); err != nil {
		t.Fatal(err)
	}
	if n := cp.Calls(); n != 1 {
		t.Fatalf("Parse calls with unchanged content and version = %d, want 1 (cache hit)", n)
	}
}

// TestParseCacheKeyWithoutVersionerIsContentHash pins the compatibility
// promise: a parser that does not implement CacheVersioner keys exactly as
// it did before B-0007, so pre-existing cache entries and any third-party
// parser keep working untouched.
func TestParseCacheKeyWithoutVersionerIsContentHash(t *testing.T) {
	src := []byte("package p\n\nfunc A() {}\n")
	var plain LanguageParser = bareParser{}
	if _, ok := plain.(CacheVersioner); ok {
		t.Fatal("bareParser must not implement CacheVersioner — the test proves the fallback")
	}
	if got, want := parseCacheKey(plain, src), sha256.Sum256(src); got != want {
		t.Fatalf("key for a version-less parser = %x, want the plain content hash %x", got, want)
	}
}

// bareParser implements LanguageParser and nothing else — the shape every
// third-party or future parser has until it opts into CacheVersioner.
type bareParser struct{}

func (bareParser) Lang() graph.Lang     { return graph.LangGo }
func (bareParser) Extensions() []string { return []string{".bare"} }
func (bareParser) Parse(string, []byte) (ParseResult, error) {
	return ParseResult{}, nil
}

// countingVersionedParser is a CacheVersioner that counts its own Parse
// calls — countingParser cannot be reused here because it embeds the
// LanguageParser interface, which does not promote CacheVersion.
type countingVersionedParser struct {
	version string
	mu      sync.Mutex
	calls   int
}

func (c *countingVersionedParser) Lang() graph.Lang     { return graph.LangGo }
func (c *countingVersionedParser) Extensions() []string { return []string{".go"} }
func (c *countingVersionedParser) CacheVersion() string { return c.version }

func (c *countingVersionedParser) Parse(path string, src []byte) (ParseResult, error) {
	c.mu.Lock()
	c.calls++
	c.mu.Unlock()
	return ParseResult{Nodes: []graph.Node{{
		ID: "go:p.a", Kind: graph.KFunc, Lang: graph.LangGo, File: path, Line: 1,
	}}}, nil
}

func (c *countingVersionedParser) Calls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}
