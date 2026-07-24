package store

import "testing"

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
