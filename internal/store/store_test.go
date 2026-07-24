package store

import (
	"path/filepath"
	"testing"
)

func TestMemStore(t *testing.T) {
	s := NewMem()
	defer s.Close()

	h1 := [32]byte{1}
	h2 := [32]byte{2}
	if _, ok := s.Get("a.go", h1); ok {
		t.Fatal("empty store reported a hit")
	}
	if err := s.Put(Entry{Path: "a.go", Hash: h1, Blob: []byte("parsed")}); err != nil {
		t.Fatal(err)
	}
	e, ok := s.Get("a.go", h1)
	if !ok || string(e.Blob) != "parsed" {
		t.Fatalf("Get = %+v %v", e, ok)
	}
	// hash mismatch is a miss (content changed on disk)
	if _, ok := s.Get("a.go", h2); ok {
		t.Fatal("stale hash reported a hit")
	}
	// overwrite with the new hash
	if err := s.Put(Entry{Path: "a.go", Hash: h2, Blob: []byte("reparsed")}); err != nil {
		t.Fatal(err)
	}
	if e, ok := s.Get("a.go", h2); !ok || string(e.Blob) != "reparsed" {
		t.Fatalf("overwrite failed: %+v %v", e, ok)
	}
}

func TestSQLiteStoreRoundtrip(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "sub", "parse.db")
	s, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	h1 := [32]byte{1}
	h2 := [32]byte{2}

	if _, ok := s.Get("a.go", h1); ok {
		t.Fatal("empty store reported a hit")
	}
	if err := s.Put(Entry{Path: "a.go", Hash: h1, Blob: []byte("parsed")}); err != nil {
		t.Fatal(err)
	}
	e, ok := s.Get("a.go", h1)
	if !ok || string(e.Blob) != "parsed" || e.Hash != h1 {
		t.Fatalf("Get = %+v %v", e, ok)
	}

	// hash mismatch is a miss (content changed on disk)
	if _, ok := s.Get("a.go", h2); ok {
		t.Fatal("stale hash reported a hit")
	}

	// unknown path is a miss
	if _, ok := s.Get("b.go", h1); ok {
		t.Fatal("unknown path reported a hit")
	}

	// upsert with the new hash overwrites in place
	if err := s.Put(Entry{Path: "a.go", Hash: h2, Blob: []byte("reparsed")}); err != nil {
		t.Fatal(err)
	}
	if e, ok := s.Get("a.go", h2); !ok || string(e.Blob) != "reparsed" {
		t.Fatalf("overwrite failed: %+v %v", e, ok)
	}
	if _, ok := s.Get("a.go", h1); ok {
		t.Fatal("stale hash still reported a hit after overwrite")
	}
}

func TestSQLiteStoreReopenPersists(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "parse.db")
	h := [32]byte{7}

	s1, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := s1.Put(Entry{Path: "a.go", Hash: h, Blob: []byte("parsed")}); err != nil {
		t.Fatal(err)
	}
	if err := s1.Close(); err != nil {
		t.Fatal(err)
	}

	s2, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	e, ok := s2.Get("a.go", h)
	if !ok || string(e.Blob) != "parsed" {
		t.Fatalf("reopened store lost entry: %+v %v", e, ok)
	}
}
