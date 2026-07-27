package benchmark

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Store is the loaded bench.ndjson of one context: records grouped by key,
// each key's retained versions sorted ascending by Ver.
type Store struct {
	byKey map[string][]Record
	// Quarantined lines: content whose stored key failed verification or
	// whose JSON did not parse. Never silently dropped — they are reported
	// and preserved byte-identically on rewrite (union merges and hand
	// edits must not lose data to a strict reader).
	Quarantine []string
}

// Load reads a bench.ndjson. A missing file is an empty store. Duplicate
// IDs (union-merge artifacts) dedup to one; a (key, ver) collision between
// DIFFERENT records resolves deterministically: newer T wins, ties break
// by ID — both clones converge on the same winner.
func Load(path string) (*Store, error) {
	st := &Store{byKey: map[string][]Record{}}
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return st, nil
	}
	if err != nil {
		return nil, err
	}
	seenID := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var r Record
		if jerr := json.Unmarshal([]byte(line), &r); jerr != nil {
			st.Quarantine = append(st.Quarantine, line)
			continue
		}
		if verr := r.Validate(); verr != nil {
			st.Quarantine = append(st.Quarantine, line)
			continue
		}
		if seenID[r.ID] {
			continue // union-merge duplicate of the same record
		}
		seenID[r.ID] = true
		st.byKey[r.Key] = append(st.byKey[r.Key], r)
	}
	for key, recs := range st.byKey {
		sort.Slice(recs, func(i, j int) bool {
			if recs[i].Ver != recs[j].Ver {
				return recs[i].Ver < recs[j].Ver
			}
			if !recs[i].T.Equal(recs[j].T) {
				return recs[i].T.Before(recs[j].T)
			}
			return recs[i].ID < recs[j].ID
		})
		// (key, ver) collisions from parallel clones: keep the winner —
		// the LAST in the deterministic order above (newest T, then ID).
		kept := recs[:0]
		for i, r := range recs {
			if i+1 < len(recs) && recs[i+1].Ver == r.Ver {
				continue
			}
			kept = append(kept, r)
		}
		st.byKey[key] = kept
	}
	return st, nil
}

// Keys returns every benchmark key, sorted.
func (st *Store) Keys() []string {
	out := make([]string, 0, len(st.byKey))
	for k := range st.byKey {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Head returns the newest retained version for key.
func (st *Store) Head(key string) (Record, bool) {
	recs := st.byKey[key]
	if len(recs) == 0 {
		return Record{}, false
	}
	return recs[len(recs)-1], true
}

// Versions returns all retained versions for key, ascending.
func (st *Store) Versions(key string) []Record { return st.byKey[key] }

// ByID finds a record by its exact ID across all keys.
func (st *Store) ByID(id string) (Record, bool) {
	for _, recs := range st.byKey {
		for _, r := range recs {
			if r.ID == id {
				return r, true
			}
		}
	}
	return Record{}, false
}

// Put inserts rec: Ver is ASSIGNED here (head+1, or 1 for a new key).
// Identical content to the head is an idempotent replay — nothing is
// written and the head returns with changed=false. The superseded head
// (if any) returns so the caller can journal its raw values
// (ADR-01KYJMWEWQ). History trims to depth newest versions.
func (st *Store) Put(rec Record, depth int) (stored Record, prev *Record, changed bool, err error) {
	if depth < 1 {
		depth = 1
	}
	if rec.Key == "" {
		rec.Key, err = CanonicalKey(rec.Name, rec.Frame)
		if err != nil {
			return Record{}, nil, false, err
		}
	}
	rec.Frame = CanonicalFrame(rec.Frame)
	if head, ok := st.Head(rec.Key); ok {
		if SameContent(head, rec) {
			return head, nil, false, nil
		}
		rec.Ver = head.Ver + 1
		p := head
		prev = &p
	} else {
		rec.Ver = 1
	}
	if err := rec.Validate(); err != nil {
		return Record{}, nil, false, err
	}
	recs := append(st.byKey[rec.Key], rec)
	if len(recs) > depth {
		recs = recs[len(recs)-depth:]
	}
	st.byKey[rec.Key] = recs
	return rec, prev, true, nil
}

// Remove drops every retained version of key. Returns how many were held.
func (st *Store) Remove(key string) int {
	n := len(st.byKey[key])
	delete(st.byKey, key)
	return n
}

// Save writes the store atomically (temp + rename), keys sorted, versions
// ascending, quarantined lines preserved verbatim at the end.
func (st *Store) Save(path string) error {
	var b strings.Builder
	for _, key := range st.Keys() {
		for _, r := range st.byKey[key] {
			raw, err := json.Marshal(r)
			if err != nil {
				return err
			}
			b.Write(raw)
			b.WriteByte('\n')
		}
	}
	for _, q := range st.Quarantine {
		b.WriteString(q)
		b.WriteByte('\n')
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("benchmark: save %s: %w", path, err)
	}
	return nil
}
