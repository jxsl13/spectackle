// Package cache is the local, NON-versioned index at .spectackle/cache/index.db
// (pure-Go SQLite via modernc.org/sqlite, FTS5). The versioned .spectackle
// files are the source of truth; the cache is rebuildable at any time. There
// are no migrations: a generation-stamp mismatch drops everything and
// rebuilds (the sync engine re-feeds it from disk).
//
// v0 scope: meta, file stats and the FTS5 docs table. Graph persistence
// (nodes/edges/parse blobs) joins when the M1 indexer lands.
package cache

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// gen stamps the cache format; any change to the DDL, to the doc feeding
// logic, or to what the files table records for freshness — the columns the
// sync engine compares to decide a bundle is unchanged — must bump it.
// Mismatch => full rebuild.
//
// v0-6 wired files.sha: the column was declared but never written, so caches
// stamped v0-5 carry rows with a NULL hash that would read as "content
// unknown" forever. Dropping them repopulates the column in one rebuild.
const gen = "v0-6"

const ddl = `
CREATE TABLE IF NOT EXISTS meta(k TEXT PRIMARY KEY, v TEXT);
CREATE TABLE IF NOT EXISTS files(path TEXT PRIMARY KEY, mtime INTEGER, size INTEGER, sha TEXT);
CREATE VIRTUAL TABLE IF NOT EXISTS docs USING fts5(kind, id, dir, title, body);
`

// Doc is one searchable record. Kinds: rule, section, proposal, task, bug,
// research, adr, journal, rejection.
type Doc struct {
	Kind, ID, Dir, Title, Body string
}

// Cache wraps the SQLite handle.
type Cache struct{ db *sql.DB }

// Open opens (or rebuilds) the cache under the given cache directory.
// WAL + busy_timeout + immediate transactions: two agent processes rooted at
// the same workspace share this file and must not trip over each other.
func Open(cacheDir string) (*Cache, error) {
	dsn := "file:" + filepath.Join(cacheDir, "index.db") + "?_txlock=immediate" +
		"&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	c := &Cache{db: db}
	if err := c.init(); err != nil {
		db.Close()
		return nil, err
	}
	return c, nil
}

func (c *Cache) init() error {
	var have string
	err := c.db.QueryRow(`SELECT v FROM meta WHERE k='gen'`).Scan(&have)
	if err == nil && have == gen {
		return nil
	}
	// unknown/old generation: drop and rebuild (no migrations by design)
	for _, stmt := range []string{
		`DROP TABLE IF EXISTS meta`, `DROP TABLE IF EXISTS files`, `DROP TABLE IF EXISTS docs`,
	} {
		if _, err := c.db.Exec(stmt); err != nil {
			return err
		}
	}
	if _, err := c.db.Exec(ddl); err != nil {
		return err
	}
	_, err = c.db.Exec(`INSERT INTO meta(k,v) VALUES('gen',?)`, gen)
	return err
}

// Close closes the handle.
func (c *Cache) Close() error { return c.db.Close() }

// FileStat returns what the cache recorded for a path the last time it was
// indexed: the cheap metadata gate (mtime, size) plus the hash of the content
// those docs were actually built from. An empty sha means the content is
// unknown and the metadata alone must not be trusted.
func (c *Cache) FileStat(path string) (mtime, size int64, sha string, ok bool) {
	err := c.db.QueryRow(`SELECT mtime,size,COALESCE(sha,'') FROM files WHERE path=?`,
		path).Scan(&mtime, &size, &sha)
	return mtime, size, sha, err == nil
}

// PutFileStat records a path's stat together with the content hash that was
// indexed from it. Pass the hash the docs were derived from, never a hash of
// some later read: the pair is what makes freshness follow content instead of
// metadata a same-size, timestamp-preserving write leaves untouched.
func (c *Cache) PutFileStat(path string, mtime, size int64, sha string) error {
	_, err := c.db.Exec(`INSERT INTO files(path,mtime,size,sha) VALUES(?,?,?,?)
		ON CONFLICT(path) DO UPDATE SET
			mtime=excluded.mtime, size=excluded.size, sha=excluded.sha`, path, mtime, size, sha)
	return err
}

// ReplaceDocs atomically replaces all docs of the given kinds within one
// context dir — the FTS5-has-no-PK maintenance strategy: one changed file
// re-feeds all its record kinds.
func (c *Cache) ReplaceDocs(dir string, kinds []string, docs []Doc) error {
	tx, err := c.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	ph := strings.Repeat("?,", len(kinds))
	args := make([]any, 0, len(kinds)+1)
	args = append(args, dir)
	for _, k := range kinds {
		args = append(args, k)
	}
	if _, err := tx.Exec(`DELETE FROM docs WHERE dir=? AND kind IN (`+ph[:len(ph)-1]+`)`, args...); err != nil {
		return err
	}
	for _, d := range docs {
		if _, err := tx.Exec(`INSERT INTO docs(kind,id,dir,title,body) VALUES(?,?,?,?,?)`,
			d.Kind, d.ID, d.Dir, d.Title, d.Body); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Search runs a sanitized FTS query. kinds empty = all kinds. Results are
// rank-ordered (bm25), at most k.
func (c *Cache) Search(q string, kinds []string, k int) ([]Doc, error) {
	match := sanitize(q)
	if match == "" {
		return nil, nil
	}
	query := `SELECT kind,id,dir,title,snippet(docs,4,'','','…',12) FROM docs WHERE docs MATCH ?`
	args := []any{match}
	if len(kinds) > 0 {
		query += ` AND kind IN (` + strings.Repeat("?,", len(kinds)-1) + `?)`
		for _, kd := range kinds {
			args = append(args, kd)
		}
	}
	query += ` ORDER BY rank LIMIT ?`
	args = append(args, k)
	rows, err := c.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Doc
	for rows.Next() {
		var d Doc
		if err := rows.Scan(&d.Kind, &d.ID, &d.Dir, &d.Title, &d.Body); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// List enumerates docs of the given kinds with no query at all, in a stable
// descending-ID order — chronological for the time-ordered ID kinds, see the
// ORDER BY comment below. Search returns (nil, nil) for an empty query, which the find tool
// rendered as `ok no matches` — a successful call carrying a false answer on
// a workspace that had 96 rules (B-01KYR01E2VFEF). Enumeration is a separate
// question from matching, so it gets a separate query rather than a magic
// query string: there is no FTS MATCH here, only a kind filter.
func (c *Cache) List(kinds []string, k int) ([]Doc, error) {
	query := `SELECT kind,id,dir,title,substr(body,1,200) FROM docs`
	var args []any
	if len(kinds) > 0 {
		query += ` WHERE kind IN (` + strings.Repeat("?,", len(kinds)-1) + `?)`
		for _, kd := range kinds {
			args = append(args, kd)
		}
	}
	// DESC, chosen for the scopes where a bounded enumeration carries the most
	// information: item and journal IDs are time-ordered, so newest-first
	// surfaces the recent rejections and decisions the loop opens with, and
	// ascending meant a default k=8 always returned the same OLDEST eight and
	// never the newest however the corpus grew.
	//
	// It is NOT chronological for every kind: a rule ID is stem-based
	// (SXP-API-002), so DESC is reverse-alphabetical there and the order is
	// arbitrary either way — stable and pageable, which is what enumeration
	// owes a caller, but not meaningfully "newest". Per-kind ordering is a
	// real question and is deliberately not guessed at here.
	query += ` ORDER BY id DESC LIMIT ?`
	args = append(args, k)
	rows, err := c.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Doc
	for rows.Next() {
		var d Doc
		if err := rows.Scan(&d.Kind, &d.ID, &d.Dir, &d.Title, &d.Body); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// sanitize turns free text into a safe FTS5 MATCH expression: quoted tokens
// joined by OR (the LLM never passes raw MATCH syntax).
func sanitize(q string) string {
	fields := strings.FieldsFunc(q, func(r rune) bool {
		return !(r == '_' || r == '-' || r == '.' || r == ':' ||
			(r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z'))
	})
	var toks []string
	for _, f := range fields {
		toks = append(toks, fmt.Sprintf("%q", f))
	}
	return strings.Join(toks, " OR ")
}
