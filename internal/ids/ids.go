// Package ids defines the minting and validation rules for the two kinds of
// identifier the system hands to an LLM: node IDs for code symbols and record
// IDs for lifecycle records.
//
// # Node IDs
//
// A NodeID is the cheapest currency in the whole system: the LLM references
// graph nodes by ID instead of by file contents, so IDs must be short,
// deterministic and byte-identical across re-indexing runs of unchanged files.
//
// Format: "<lang>:<qualified-name>", e.g.
//
//	go:saxpy.Saxpy      Go function Saxpy in package saxpy
//	c:launch_saxpy      C symbol launch_saxpy
//	cu:saxpy_kernel     CUDA __global__ kernel
//	asm:mat.mulVec      Plan 9 asm TEXT ·mulVec in package mat
//
// When two distinct definitions mint the same ID (e.g. static C functions of
// the same name in different translation units), the indexer disambiguates by
// appending "~2", "~3", ... in deterministic file-path order.
//
// # Record IDs
//
// A RecordID identifies a lifecycle record (proposal, task, bug, ADR,
// research). Unlike node IDs it is minted, not derived, so per-clone counters
// used to collide across independently drafting checkouts. Per ADR-0013 a
// record ID is a UUIDv7 rendered in Crockford base32: globally unique without
// coordination, and time-ordered, so the canonical string sorts by mint time.
// The full 26-character form is what gets stored; ShortenRecordID and
// ResolveRecordID provide the git-style short-prefix display and lookup that
// keep ID-dense output affordable. See MintRecordID.
package ids

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// idRe validates "<lang>:<qual>" with an optional "~N" collision suffix.
var idRe = regexp.MustCompile(`^[a-z]{1,5}:[^\s~]+(~[2-9][0-9]*)?$`)

// Mint builds a NodeID string from a language tag and a qualified name.
// The qualified name must not contain whitespace; spaces are replaced by "_".
func Mint(lang, qual string) string {
	qual = strings.ReplaceAll(strings.TrimSpace(qual), " ", "_")
	return lang + ":" + qual
}

// WithCollision returns id with a "~n" disambiguation suffix (n >= 2).
func WithCollision(id string, n int) string {
	return fmt.Sprintf("%s~%d", id, n)
}

// Parse splits an ID into language tag and qualified name (collision suffix
// retained in qual). It returns an error for malformed IDs.
func Parse(id string) (lang, qual string, err error) {
	if !Valid(id) {
		return "", "", fmt.Errorf("ids: malformed node ID %q", id)
	}
	lang, qual, _ = strings.Cut(id, ":")
	return lang, qual, nil
}

// Valid reports whether id conforms to the NodeID grammar.
func Valid(id string) bool { return idRe.MatchString(id) }

// ---------------------------------------------------------------------------
// Record IDs (ADR-0013): UUIDv7 in Crockford base32
// ---------------------------------------------------------------------------

const (
	// RecordIDLen is the length of the canonical encoded form. 128 bits in a
	// 5-bit alphabet need ceil(128/5) = 26 characters; the 2 slack bits are
	// leading zeroes of the first character, which is therefore never above
	// '7'.
	RecordIDLen = 26

	// MinRecordPrefixLen is the floor ShortenRecordID never goes below.
	//
	// Reasoning, and why a floor is not a substitute for adaptivity:
	// uniqueness alone would allow a 1-character prefix in a young workspace,
	// which is neither recognizable as a record ID nor stable — the very next
	// record would collide with it. The floor is about identity and stability,
	// not about uniqueness, which ShortenRecordID computes adaptively.
	//
	// Six is chosen because:
	//   - A prefix of p characters pins 5p of the leading 48 timestamp bits.
	//     At p=6 that is 30 bits, i.e. the mint time to within 2^18 ms ≈ 4.4
	//     minutes. That is the coarsest granularity at which a prefix still
	//     states something true and stable about when the record was made;
	//     anything shorter carries no information at all, since every ID in
	//     this repository's lifetime shares the same leading characters.
	//   - It matches the width of the sequential IDs it replaces ("T-0134" is
	//     6 characters), so the output-diet cost that ADR-0013 measured stays
	//     at zero for the common case.
	// It is deliberately NOT a target length: in a same-millisecond batch the
	// adaptive answer is far longer, which is the whole point of ADR-0013's
	// caveat about the leading timestamp run.
	MinRecordPrefixLen = 6

	// crockfordAlphabet is Crockford's base32: the digits and the uppercase
	// letters minus I, L, O and U. Its symbols are in ascending ASCII order,
	// which is what makes the fixed-width encoding sort lexicographically in
	// the same order as the 128-bit value it encodes.
	crockfordAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
)

// ErrNoMatch reports that no known record ID starts with the given prefix.
// Callers test for it with errors.Is.
var ErrNoMatch = errors.New("ids: no record matches prefix")

// AmbiguousPrefixError reports that a prefix matches more than one known
// record. It carries every candidate so callers can render an error that names
// what the user has to disambiguate. Callers test for it with errors.As.
type AmbiguousPrefixError struct {
	Prefix     string
	Candidates []string // every matching record ID, ascending
}

func (e *AmbiguousPrefixError) Error() string {
	return fmt.Sprintf("ids: prefix %q matches %d records: %s",
		e.Prefix, len(e.Candidates), strings.Join(e.Candidates, ", "))
}

// RecordID is the raw 128-bit value of a lifecycle record identifier: a UUIDv7
// as laid out by RFC 9562, i.e. a 48-bit big-endian unix-millisecond timestamp
// followed by the version and variant markers and random bits. Its canonical
// text form is String.
type RecordID [16]byte

// MintRecordID mints a record ID stamped with the current time.
func MintRecordID() RecordID { return MintRecordIDAt(time.Now()) }

// MintRecordIDAt mints a record ID stamped with ts, whose millisecond
// resolution is all that survives. An explicit timestamp exists so that a
// migration can preserve the original creation order of records it rewrites,
// and so that tests can construct same-millisecond batches deterministically.
//
// Two IDs minted in the same millisecond differ in their random tail but have
// no defined order relative to each other; IDs from distinct milliseconds
// always sort by time. That is the ordering guarantee of plain UUIDv7 — this
// package deliberately adds no monotonic within-millisecond counter, since
// nothing in the lifecycle depends on sub-millisecond ordering.
func MintRecordIDAt(ts time.Time) RecordID {
	var id RecordID
	ms := uint64(ts.UnixMilli())
	// 48-bit big-endian unix_ts_ms.
	id[0] = byte(ms >> 40)
	id[1] = byte(ms >> 32)
	id[2] = byte(ms >> 24)
	id[3] = byte(ms >> 16)
	id[4] = byte(ms >> 8)
	id[5] = byte(ms)
	if _, err := rand.Read(id[6:]); err != nil {
		// Unreachable on every supported platform: since Go 1.24 crypto/rand
		// never returns an error. If the system entropy source ever did fail,
		// minting a predictable ID would be worse than stopping.
		panic("ids: crypto/rand failed: " + err.Error())
	}
	id[6] = id[6]&0x0f | 0x70 // version 7
	id[8] = id[8]&0x3f | 0x80 // variant 10
	return id
}

// MintRecordIDFrom mints a record ID stamped with ts whose 74 random bits are
// derived from seed rather than drawn from the system entropy source, so the
// same (ts, seed) pair always produces the same ID. The result is a
// well-formed UUIDv7 — same layout, same version and variant markers, same
// ordering guarantees — and is indistinguishable from a randomly minted one
// to every reader.
//
// It exists for the schema migration, which has a hard determinism
// requirement: the same input workspace must migrate to the same IDs on any
// machine, so a second clone migrating the same committed records converges
// instead of minting a divergent set that then collides on merge. Journal
// timestamps are truncated to the second, so a timestamp alone is nowhere
// near unique — the seed (the record's legacy ID) is what separates records
// created in the same second.
//
// Do NOT use it for ordinary minting. Its uniqueness is only as good as the
// seed's, whereas MintRecordID's rests on 74 bits of entropy and needs no
// coordination or care from the caller.
func MintRecordIDFrom(ts time.Time, seed string) RecordID {
	sum := sha256.Sum256([]byte(seed))
	var id RecordID
	ms := uint64(ts.UnixMilli())
	id[0] = byte(ms >> 40)
	id[1] = byte(ms >> 32)
	id[2] = byte(ms >> 24)
	id[3] = byte(ms >> 16)
	id[4] = byte(ms >> 8)
	id[5] = byte(ms)
	copy(id[6:], sum[:10])
	id[6] = id[6]&0x0f | 0x70 // version 7
	id[8] = id[8]&0x3f | 0x80 // variant 10
	return id
}

// Time returns the mint timestamp, at millisecond resolution, in UTC.
func (id RecordID) Time() time.Time {
	ms := int64(id[0])<<40 | int64(id[1])<<32 | int64(id[2])<<24 |
		int64(id[3])<<16 | int64(id[4])<<8 | int64(id[5])
	return time.UnixMilli(ms).UTC()
}

// String returns the canonical 26-character Crockford base32 form: the 128-bit
// value as a fixed-width big-endian numeral, most significant digit first, so
// that byte-wise string comparison of two IDs matches numeric comparison of
// the values, and therefore matches their timestamp order.
func (id RecordID) String() string {
	hi := binary.BigEndian.Uint64(id[0:8])
	lo := binary.BigEndian.Uint64(id[8:16])
	var out [RecordIDLen]byte
	for i := range out {
		out[i] = crockfordAlphabet[bits5(hi, lo, uint(125-5*i))]
	}
	return string(out[:])
}

// bits5 extracts the 5 bits at offset shift from the 128-bit value hi:lo.
// Offsets at or above 128 read as zero, which is what makes the first
// character of the encoding carry only the top 3 bits.
func bits5(hi, lo uint64, shift uint) uint64 {
	switch {
	case shift >= 64:
		return hi >> (shift - 64) & 31
	case shift <= 59:
		return lo >> shift & 31
	default: // straddles the hi/lo boundary
		return (hi<<(64-shift) | lo>>shift) & 31
	}
}

// ParseRecordID decodes the canonical form produced by RecordID.String and
// returns the exact bytes it was built from.
//
// It is strict on purpose: record IDs are machine-produced and machine-copied,
// so anything that is not the canonical form is a caller bug worth surfacing
// rather than guessing at. In particular the characters Crockford excludes for
// being confusable — I, L, O and U — are rejected instead of silently mapped
// to 1, 1, 0 and V. Validation covers the encoding only; it does not assert
// that the decoded value carries UUID version 7.
func ParseRecordID(s string) (RecordID, error) {
	var id RecordID
	if s == "" {
		return id, errors.New("ids: empty record ID")
	}
	if len(s) != RecordIDLen {
		return id, fmt.Errorf("ids: record ID %q has %d characters, want %d",
			s, len(s), RecordIDLen)
	}
	var hi, lo uint64
	for i := 0; i < len(s); i++ {
		v, err := decodeSymbol(s[i])
		if err != nil {
			return RecordID{}, fmt.Errorf("ids: record ID %q at position %d: %w", s, i, err)
		}
		if i == 0 && v > 7 {
			return RecordID{}, fmt.Errorf("ids: record ID %q overflows 128 bits: "+
				"the first character encodes only 3 bits and cannot exceed '7'", s)
		}
		hi = hi<<5 | lo>>59
		lo = lo<<5 | v
	}
	binary.BigEndian.PutUint64(id[0:8], hi)
	binary.BigEndian.PutUint64(id[8:16], lo)
	return id, nil
}

// decodeSymbol maps one canonical character to its 5-bit value.
func decodeSymbol(c byte) (uint64, error) {
	if i := strings.IndexByte(crockfordAlphabet, c); i >= 0 {
		return uint64(i), nil
	}
	switch c {
	case 'I', 'L', 'O', 'U', 'i', 'l', 'o', 'u':
		return 0, fmt.Errorf("character %q is excluded from the Crockford base32 "+
			"alphabet as confusable and is not remapped", c)
	}
	if c >= 'a' && c <= 'z' {
		return 0, fmt.Errorf("character %q is lowercase; record IDs are uppercase", c)
	}
	return 0, fmt.Errorf("character %q is not in the Crockford base32 alphabet %q",
		c, crockfordAlphabet)
}

// ValidRecordID reports whether s is a canonical record ID.
func ValidRecordID(s string) bool {
	_, err := ParseRecordID(s)
	return err == nil
}

// ShortenRecordID returns the shortest prefix of id that no other entry of
// known starts with, never shorter than MinRecordPrefixLen. known may contain
// id itself, duplicates, and identifiers of other shapes (legacy sequential
// IDs stay resolvable), all of which are compared as plain strings.
//
// The length is computed rather than fixed because UUIDv7 leads with the
// millisecond timestamp: roughly the first ten characters are shared by every
// record minted in the same second, so a git-style fixed 7 would be ambiguous
// almost immediately in a swarm that mints several records per second.
//
// The returned prefix resolves back to id through ResolveRecordID against the
// same known set.
func ShortenRecordID(id string, known []string) (string, error) {
	if _, err := ParseRecordID(id); err != nil {
		return "", err
	}
	n := MinRecordPrefixLen
	for _, k := range known {
		if k == id {
			continue
		}
		if l := commonPrefixLen(id, k) + 1; l > n {
			n = l
		}
	}
	if n > len(id) {
		n = len(id)
	}
	return id[:n], nil
}

// commonPrefixLen returns the number of leading bytes a and b share.
func commonPrefixLen(a, b string) int {
	n := min(len(a), len(b))
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}

// ResolveRecordID expands a prefix to the single known record ID it names.
//
// The three outcomes are distinguishable: a hit returns the full ID and a nil
// error; a miss returns an error matching ErrNoMatch; an ambiguity returns an
// *AmbiguousPrefixError carrying every candidate. An entry equal to prefix is
// an exact hit and wins over any longer entry it is a prefix of, so a fully
// spelled-out ID always resolves to itself.
func ResolveRecordID(prefix string, known []string) (string, error) {
	if prefix == "" {
		return "", errors.New("ids: empty record ID prefix")
	}
	var hits []string
	seen := make(map[string]bool, len(known))
	for _, k := range known {
		if k == prefix {
			return k, nil
		}
		if strings.HasPrefix(k, prefix) && !seen[k] {
			seen[k] = true
			hits = append(hits, k)
		}
	}
	switch len(hits) {
	case 0:
		return "", fmt.Errorf("%w: %q", ErrNoMatch, prefix)
	case 1:
		return hits[0], nil
	}
	sort.Strings(hits)
	return "", &AmbiguousPrefixError{Prefix: prefix, Candidates: hits}
}
