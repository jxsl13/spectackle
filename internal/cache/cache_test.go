package cache

import (
	"testing"
)

func open(t *testing.T, dir string) *Cache {
	t.Helper()
	c, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestReplaceDocsAndSearch(t *testing.T) {
	c := open(t, t.TempDir())
	if err := c.ReplaceDocs("gpu", []string{"rule"}, []Doc{
		{Kind: "rule", ID: "GPU-001", Dir: "gpu", Title: "launch check", Body: "check cudaGetLastError after launch"},
		{Kind: "rule", ID: "GPU-002", Dir: "gpu", Title: "bounds", Body: "guard index bounds"},
	}); err != nil {
		t.Fatal(err)
	}
	docs, err := c.Search("cudaGetLastError", []string{"rule"}, 5)
	if err != nil || len(docs) != 1 || docs[0].ID != "GPU-001" {
		t.Fatalf("Search = %+v, %v", docs, err)
	}
	// replace semantics: re-feeding the (dir, kinds) drops the old set
	if err := c.ReplaceDocs("gpu", []string{"rule"}, []Doc{
		{Kind: "rule", ID: "GPU-003", Dir: "gpu", Title: "new", Body: "streams must be synchronized"},
	}); err != nil {
		t.Fatal(err)
	}
	if docs, _ := c.Search("cudaGetLastError", nil, 5); len(docs) != 0 {
		t.Fatalf("stale doc survived replace: %+v", docs)
	}
	// kind filter excludes
	if docs, _ := c.Search("synchronized", []string{"journal"}, 5); len(docs) != 0 {
		t.Fatalf("kind filter leaked: %+v", docs)
	}
}

func TestSearchSanitizing(t *testing.T) {
	c := open(t, t.TempDir())
	c.ReplaceDocs(".", []string{"rule"}, []Doc{{Kind: "rule", ID: "X-001", Dir: ".", Title: "t", Body: "alpha beta"}})
	// raw FTS syntax / injection attempts must not error
	for _, q := range []string{`"alpha`, `alpha OR`, `NEAR(`, `alpha)`, `- NOT *`, ``} {
		if _, err := c.Search(q, nil, 3); err != nil {
			t.Errorf("Search(%q) errored: %v", q, err)
		}
	}
	// tokens are OR-joined
	docs, err := c.Search("beta unrelatedterm", nil, 3)
	if err != nil || len(docs) != 1 {
		t.Fatalf("OR search = %+v, %v", docs, err)
	}
}

func TestFileStat(t *testing.T) {
	c := open(t, t.TempDir())
	if _, _, _, ok := c.FileStat("x"); ok {
		t.Fatal("unknown path reported present")
	}
	if err := c.PutFileStat("x", 42, 7, "aabb"); err != nil {
		t.Fatal(err)
	}
	if mt, sz, sha, ok := c.FileStat("x"); !ok || mt != 42 || sz != 7 || sha != "aabb" {
		t.Fatalf("FileStat = %d %d %q %v", mt, sz, sha, ok)
	}
	// upsert
	c.PutFileStat("x", 43, 8, "ccdd")
	if mt, _, sha, _ := c.FileStat("x"); mt != 43 || sha != "ccdd" {
		t.Fatalf("upsert failed: %d %q", mt, sha)
	}
}

// The hash is the whole point of the row: metadata can repeat across a
// content change, so a stat write that keeps mtime and size but carries a new
// sha must still land, and a row must never read back with a stale hash.
func TestFileStatHashTracksContentNotMetadata(t *testing.T) {
	c := open(t, t.TempDir())
	if err := c.PutFileStat("bundle", 100, 64, "old"); err != nil {
		t.Fatal(err)
	}
	if err := c.PutFileStat("bundle", 100, 64, "new"); err != nil {
		t.Fatal(err)
	}
	mt, sz, sha, ok := c.FileStat("bundle")
	if !ok || mt != 100 || sz != 64 || sha != "new" {
		t.Fatalf("same-metadata rehash not recorded: %d %d %q %v", mt, sz, sha, ok)
	}
	// A row written without a hash must read back as "content unknown"
	// (empty), never as a hash that happens to match anything.
	if err := c.PutFileStat("nohash", 1, 1, ""); err != nil {
		t.Fatal(err)
	}
	if _, _, sha, ok := c.FileStat("nohash"); !ok || sha != "" {
		t.Fatalf("empty hash must round-trip empty: %q %v", sha, ok)
	}
}

func TestGenerationMismatchRebuilds(t *testing.T) {
	dir := t.TempDir()
	c := open(t, dir)
	c.ReplaceDocs(".", []string{"rule"}, []Doc{{Kind: "rule", ID: "X-001", Dir: ".", Title: "t", Body: "persist me"}})
	c.PutFileStat("f", 1, 1, "deadbeef")

	// same generation: reopen keeps data
	c.Close()
	c2 := open(t, dir)
	if docs, _ := c2.Search("persist", nil, 3); len(docs) != 1 {
		t.Fatalf("same-gen reopen lost data: %+v", docs)
	}

	// stamp mismatch: everything is dropped and rebuilt (no migrations)
	if _, err := c2.db.Exec(`UPDATE meta SET v='ancient' WHERE k='gen'`); err != nil {
		t.Fatal(err)
	}
	c2.Close()
	c3 := open(t, dir)
	if docs, _ := c3.Search("persist", nil, 3); len(docs) != 0 {
		t.Fatalf("gen mismatch must drop docs: %+v", docs)
	}
	if _, _, _, ok := c3.FileStat("f"); ok {
		t.Fatal("gen mismatch must drop file stats")
	}
}
