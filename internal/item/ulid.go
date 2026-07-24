package item

import (
	"crypto/rand"
	"fmt"
	"io"
	"time"
)

// crockford is the 32-symbol Crockford base32 alphabet used to encode
// ULIDs: ascending byte order, with I, L, O and U omitted (I/L/O to avoid
// confusion with 1/1/0, U to avoid accidental profanity). Because the
// alphabet ascends, lexicographic order over the encoded string equals
// numeric order over the underlying bits equals creation order for ids
// minted by the same process — that sortability is the entire point of
// using ULIDs here, see NewULID.
const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// crockfordValue maps every byte Crockford's spec accepts on decode to its
// 5-bit value, or false if b is outside the alphabet. Decoding is
// case-insensitive and treats the visually-ambiguous letters as digits (I,
// L -> 1; O -> 0), per the Crockford spec; U decodes to nothing; it is
// excluded from the alphabet entirely. NewULID's own output never uses
// this table on the encode side — generation always emits the canonical
// uppercase alphabet above, so a byte-identical round trip only depends on
// the straightforward path (upper-case digits/letters), not the ambiguous
// aliases.
var crockfordValue = buildCrockfordValue()

func buildCrockfordValue() map[byte]byte {
	m := make(map[byte]byte, 32+8)
	for i := 0; i < len(crockford); i++ {
		c := crockford[i]
		m[c] = byte(i)
		if c >= 'A' && c <= 'Z' {
			m[c-'A'+'a'] = byte(i)
		}
	}
	// Ambiguous-character aliases, decode-only.
	m['I'], m['i'] = 1, 1
	m['L'], m['l'] = 1, 1
	m['O'], m['o'] = 0, 0
	return m
}

// Generator mints ULIDs from an injectable time source and randomness
// source, so callers (and tests) never depend on an untestable global
// clock or the process-wide crypto/rand.Reader. The zero value is safe to
// use directly: Now defaults to time.Now and Random defaults to
// crypto/rand.Reader.
type Generator struct {
	// Now returns the current time; nil means time.Now.
	Now func() time.Time
	// Random supplies the 80 bits (10 bytes) of ULID randomness; nil means
	// crypto/rand.Reader.
	Random io.Reader
}

// NewULID mints a 26-character, uppercase Crockford base32 ULID: a 48-bit
// big-endian millisecond Unix timestamp (10 characters) followed by 80
// bits of randomness from g.Random (16 characters). It returns an error
// only if reading randomness fails.
func (g Generator) NewULID() (string, error) {
	now := g.Now
	if now == nil {
		now = time.Now
	}
	rnd := g.Random
	if rnd == nil {
		rnd = rand.Reader
	}

	ms := uint64(now().UnixMilli())
	ms &= (1 << 48) - 1 // clamp to the 48 bits ULID's timestamp field holds

	var entropy [10]byte
	if _, err := io.ReadFull(rnd, entropy[:]); err != nil {
		return "", fmt.Errorf("item: read ulid randomness: %w", err)
	}

	return encodeTimestamp(ms) + encodeEntropy(entropy), nil
}

// NewULIDID mints a full item id "<KIND>-<ULID>" for kind, using the
// existing kindLetter map so the prefix stays the single source of truth
// for kind letters (see kindLetter in item.go). It does not check that
// kind is valid; callers that need that should call ValidKind first, same
// as NextID.
//
// Nothing in this task wires NewULIDID into the minter: internal/lifecycle
// keeps calling NextID and producing legacy <KIND>-NNNN ids. This function
// exists so the generation side is ready and independently testable before
// that switch happens.
func (g Generator) NewULIDID(kind string) (string, error) {
	u, err := g.NewULID()
	if err != nil {
		return "", err
	}
	return kindLetter[kind] + "-" + u, nil
}

// encodeTimestamp encodes a 48-bit millisecond Unix timestamp as 10
// Crockford base32 characters: treat ms as a big-endian 50-bit number
// (its top 2 bits always 0, since 48 < 50) and emit one base32 digit per
// 5-bit group, most significant first. This is a plain fixed-width
// base-32 integer encoding, so it is trivially invertible by
// decodeTimestamp.
func encodeTimestamp(ms uint64) string {
	var out [10]byte
	for i := 9; i >= 0; i-- {
		out[i] = crockford[ms&0x1F]
		ms >>= 5
	}
	return string(out[:])
}

// decodeTimestamp is the inverse of encodeTimestamp: it reads the first 10
// characters of s as a base-32 integer and returns the millisecond Unix
// timestamp they encode.
func decodeTimestamp(s string) (uint64, error) {
	if len(s) < 10 {
		return 0, fmt.Errorf("item: ulid timestamp too short: %q", s)
	}
	var ms uint64
	for i := 0; i < 10; i++ {
		v, ok := crockfordValue[s[i]]
		if !ok {
			return 0, fmt.Errorf("item: invalid crockford base32 byte %q in %q", s[i], s)
		}
		ms = ms<<5 | uint64(v)
	}
	return ms, nil
}

// ULIDTimestamp decodes the millisecond Unix timestamp encoded in the
// first 10 characters of a ULID string (the timestamp component minted by
// NewULID) and returns it as a time.Time in UTC. It is the inverse of the
// timestamp half of NewULID.
func ULIDTimestamp(ulid string) (time.Time, error) {
	ms, err := decodeTimestamp(ulid)
	if err != nil {
		return time.Time{}, err
	}
	return time.UnixMilli(int64(ms)).UTC(), nil
}

// encodeEntropy encodes 10 bytes (80 bits) of randomness as 16 Crockford
// base32 characters, 5 bits at a time, most-significant-bit first. 80 is
// exactly divisible by 5, so unlike encodeTimestamp this needs no padding
// bits and no leading-zero framing.
func encodeEntropy(e [10]byte) string {
	var out [16]byte
	for i := 0; i < 16; i++ {
		out[i] = crockford[bitsAt(e[:], i*5)]
	}
	return string(out[:])
}

// bitsAt reads a 5-bit big-endian field out of data starting at bit offset
// off (bit 0 is the most significant bit of data[0]). The caller must
// guarantee off+5 <= len(data)*8; this is only used by encodeEntropy,
// where 16*5 == len(data)*8 exactly, so every read is in bounds.
func bitsAt(data []byte, off int) byte {
	var v byte
	for i := 0; i < 5; i++ {
		bitIdx := off + i
		byteIdx := bitIdx / 8
		shift := 7 - uint(bitIdx%8)
		bit := (data[byteIdx] >> shift) & 1
		v = v<<1 | bit
	}
	return v
}
