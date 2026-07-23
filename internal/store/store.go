// Package store defines the content-hash-keyed persistent cache used by the
// indexer to skip re-parsing unchanged files. M0 ships the interface and an
// in-memory implementation; a bbolt-backed implementation lands in M2
// (single-file ACID KV at .spectacle/cache.db — the graph is fully loaded to
// memory anyway, so no relational queries are needed).
package store

import "sync"

// Entry caches the parse result of one file, keyed by its content hash.
type Entry struct {
	Path string
	Hash [32]byte // sha256 of file contents
	Blob []byte   // gob-encoded index.ParseResult
}

// Store is a minimal KV cache. Get returns ok=false on miss or hash mismatch.
type Store interface {
	Get(path string, hash [32]byte) (Entry, bool)
	Put(e Entry) error
	Close() error
}

// mem is the M0 in-memory implementation (cache lives only per process).
type mem struct {
	mu sync.RWMutex
	m  map[string]Entry
}

// NewMem returns a process-local Store.
func NewMem() Store { return &mem{m: map[string]Entry{}} }

func (s *mem) Get(path string, hash [32]byte) (Entry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.m[path]
	if !ok || e.Hash != hash {
		return Entry{}, false
	}
	return e, true
}

func (s *mem) Put(e Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[e.Path] = e
	return nil
}

func (s *mem) Close() error { return nil }
