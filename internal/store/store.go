// Package store defines the content-hash-keyed persistent cache used by the
// indexer to skip re-parsing unchanged files. NewMem gives a process-local
// cache; Open backs the same interface with a single-file SQLite database
// (modernc.org/sqlite, cgo-free) so the cache survives across runs. The
// graph itself is always fully loaded to memory, so no relational queries
// are needed here — store stays a dumb KV blob cache; gob-encoding
// index.ParseResult is the caller's job (see internal/index.parseCached).
package store

import (
	"bytes"
	"database/sql"
	"os"
	"path/filepath"
	"sync"

	_ "modernc.org/sqlite"
)

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

// mem is the in-memory implementation (cache lives only per process).
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

// sqliteStore is a single-file, single-process SQLite-backed Store.
type sqliteStore struct {
	mu sync.Mutex
	db *sql.DB
}

// Open opens (creating as needed) a SQLite-backed Store at path. It targets a
// single process at a time — no WAL/multi-writer tuning, unlike
// internal/coord's shared coordination DB.
func Open(path string) (Store, error) {
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS blobs (
		path TEXT PRIMARY KEY,
		hash BLOB NOT NULL,
		blob BLOB NOT NULL
	)`); err != nil {
		db.Close()
		return nil, err
	}
	return &sqliteStore{db: db}, nil
}

func (s *sqliteStore) Get(path string, hash [32]byte) (Entry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var storedHash, blob []byte
	err := s.db.QueryRow(`SELECT hash, blob FROM blobs WHERE path = ?`, path).Scan(&storedHash, &blob)
	if err != nil {
		return Entry{}, false
	}
	if !bytes.Equal(storedHash, hash[:]) {
		return Entry{}, false
	}
	return Entry{Path: path, Hash: hash, Blob: blob}, true
}

func (s *sqliteStore) Put(e Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`INSERT INTO blobs(path, hash, blob) VALUES(?, ?, ?)
		ON CONFLICT(path) DO UPDATE SET hash=excluded.hash, blob=excluded.blob`,
		e.Path, e.Hash[:], e.Blob)
	return err
}

func (s *sqliteStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.db.Close()
}
