package item

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// fixedRandom is an io.Reader that always yields the same byte, for
// deterministic tests.
type fixedRandom struct{ b byte }

func (f fixedRandom) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = f.b
	}
	return len(p), nil
}

func at(t time.Time) func() time.Time {
	return func() time.Time { return t }
}

// TestNewULIDShape checks the encoded form: exactly 26 characters, every
// one drawn from the Crockford alphabet (no I, L, O, U).
func TestNewULIDShape(t *testing.T) {
	g := Generator{Now: at(time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC))}
	id, err := g.NewULID()
	if err != nil {
		t.Fatal(err)
	}
	if len(id) != 26 {
		t.Fatalf("len(%q) = %d, want 26", id, len(id))
	}
	for _, c := range id {
		if !strings.ContainsRune(crockford, c) {
			t.Fatalf("id %q contains %q, not in Crockford alphabet %q", id, c, crockford)
		}
	}
	for _, bad := range []rune{'I', 'L', 'O', 'U'} {
		if strings.ContainsRune(id, bad) {
			t.Fatalf("id %q contains forbidden character %q", id, bad)
		}
	}
}

// TestNewULIDMonotonicSortability proves — not assumes — that ids minted
// at increasing timestamps sort in generation order as plain strings, at
// three separate timestamps each with independent randomness.
func TestNewULIDMonotonicSortability(t *testing.T) {
	times := []time.Time{
		time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 6, 15, 8, 30, 0, 0, time.UTC),
		time.Date(2027, 12, 31, 23, 59, 59, 999_000_000, time.UTC),
	}
	var ids []string
	for i, tm := range times {
		g := Generator{Now: at(tm), Random: fixedRandom{byte(0x10 + i)}}
		id, err := g.NewULID()
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	sorted := append([]string(nil), ids...)
	// Selection sort by plain string comparison — independent of any
	// library sort, so this really exercises string ordering.
	for i := 0; i < len(sorted); i++ {
		min := i
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j] < sorted[min] {
				min = j
			}
		}
		sorted[i], sorted[min] = sorted[min], sorted[i]
	}
	for i := range ids {
		if ids[i] != sorted[i] {
			t.Fatalf("ids not in sorted (generation) order:\n generated=%v\n sorted=%v", ids, sorted)
		}
	}
	// And explicitly: id[0] < id[1] < id[2] as plain strings.
	if !(ids[0] < ids[1] && ids[1] < ids[2]) {
		t.Fatalf("ids not strictly increasing: %v", ids)
	}
}

// TestNewULIDSameMillisecondDiffers checks that two ids minted at the same
// millisecond, with independent randomness, differ from each other but
// both still decode to that same millisecond.
func TestNewULIDSameMillisecondDiffers(t *testing.T) {
	tm := time.Date(2026, 3, 14, 15, 9, 26, 535_000_000, time.UTC)
	g1 := Generator{Now: at(tm), Random: fixedRandom{0xAA}}
	g2 := Generator{Now: at(tm), Random: fixedRandom{0x55}}
	id1, err := g1.NewULID()
	if err != nil {
		t.Fatal(err)
	}
	id2, err := g2.NewULID()
	if err != nil {
		t.Fatal(err)
	}
	if id1 == id2 {
		t.Fatalf("same-millisecond ids with different randomness collided: %q", id1)
	}
	if id1[:10] != id2[:10] {
		t.Fatalf("same-millisecond ids have different timestamp halves: %q vs %q", id1[:10], id2[:10])
	}
	wantMS := tm.UnixMilli()
	for _, id := range []string{id1, id2} {
		got, err := ULIDTimestamp(id)
		if err != nil {
			t.Fatalf("ULIDTimestamp(%q): %v", id, err)
		}
		if got.UnixMilli() != wantMS {
			t.Fatalf("ULIDTimestamp(%q) = %v (%d ms), want %d ms", id, got, got.UnixMilli(), wantMS)
		}
	}
}

// TestULIDTimestampRoundTrip decodes the first 10 characters of a minted
// id back to the exact millisecond it was minted with.
func TestULIDTimestampRoundTrip(t *testing.T) {
	cases := []time.Time{
		time.UnixMilli(0).UTC(),
		time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC),
		time.Date(2099, 1, 1, 0, 0, 0, 123_000_000, time.UTC),
	}
	for _, tm := range cases {
		g := Generator{Now: at(tm), Random: fixedRandom{0x7F}}
		id, err := g.NewULID()
		if err != nil {
			t.Fatal(err)
		}
		got, err := ULIDTimestamp(id[:10])
		if err != nil {
			t.Fatalf("ULIDTimestamp(%q): %v", id[:10], err)
		}
		if got.UnixMilli() != tm.UnixMilli() {
			t.Fatalf("round-trip for %v: got %d ms, want %d ms (id=%q)", tm, got.UnixMilli(), tm.UnixMilli(), id)
		}
	}
}

// TestNewULIDDeterministic checks that a fixed time source and fixed
// randomness produce a byte-identical id across two independent calls.
func TestNewULIDDeterministic(t *testing.T) {
	tm := time.Date(2026, 7, 24, 9, 41, 2, 7_000_000, time.UTC)
	mk := func() (string, error) {
		g := Generator{Now: at(tm), Random: bytes.NewReader([]byte{
			0x01, 0x23, 0x45, 0x67, 0x89, 0xAB, 0xCD, 0xEF, 0x01, 0x23,
		})}
		return g.NewULID()
	}
	id1, err := mk()
	if err != nil {
		t.Fatal(err)
	}
	id2, err := mk()
	if err != nil {
		t.Fatal(err)
	}
	if id1 != id2 {
		t.Fatalf("determinism broken: %q != %q", id1, id2)
	}
}

// TestNewULIDID mints a full item id and checks the kind prefix comes from
// the same kindLetter map NextID uses.
func TestNewULIDID(t *testing.T) {
	g := Generator{Now: at(time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)), Random: fixedRandom{0x01}}
	id, err := g.NewULIDID("proposal")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(id, "P-") || len(id) != len("P-")+26 {
		t.Fatalf("NewULIDID(proposal) = %q, want P-<26 chars>", id)
	}
	if !IDRe.MatchString(id) {
		t.Fatalf("NewULIDID(proposal) = %q does not match IDRe", id)
	}
}
