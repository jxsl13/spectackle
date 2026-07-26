package ids

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestMintParseRoundtrip(t *testing.T) {
	id := Mint("go", "saxpy.Saxpy")
	if id != "go:saxpy.Saxpy" || !Valid(id) {
		t.Fatalf("Mint = %q (valid=%v)", id, Valid(id))
	}
	lang, qual, err := Parse(id)
	if err != nil || lang != "go" || qual != "saxpy.Saxpy" {
		t.Fatalf("Parse = %q %q %v", lang, qual, err)
	}
	// whitespace is normalized away
	if got := Mint("cu", " saxpy kernel "); got != "cu:saxpy_kernel" {
		t.Fatalf("Mint with spaces = %q", got)
	}
}

func TestCollisionSuffix(t *testing.T) {
	id := WithCollision("c:init", 2)
	if id != "c:init~2" || !Valid(id) {
		t.Fatalf("WithCollision = %q (valid=%v)", id, Valid(id))
	}
	if _, _, err := Parse(id); err != nil {
		t.Fatalf("Parse collision ID: %v", err)
	}
}

func TestValid(t *testing.T) {
	for id, want := range map[string]bool{
		"go:pkg.Fn":      true,
		"asm:mat.mul":    true,
		"sec:gpu#intent": true,
		"go:pkg.Fn~2":    true,
		"go:":            false,
		"pkg.Fn":         false,
		"GO:pkg.Fn":      false, // lang tag is lowercase
		"go:a b":         false, // no whitespace
		"go:x~1":         false, // collision suffixes start at 2
	} {
		if got := Valid(id); got != want {
			t.Errorf("Valid(%q) = %v, want %v", id, got, want)
		}
	}
}

// ---------------------------------------------------------------------------
// Record IDs (ADR-0013)
// ---------------------------------------------------------------------------

// mintBatchAt mints n record IDs all stamped with the same millisecond, which
// is the adversarial case for prefix shortening: they share their entire
// 48-bit timestamp, i.e. the first ten encoded characters.
func mintBatchAt(ts time.Time, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = MintRecordIDAt(ts).String()
	}
	return out
}

func TestMintRecordIDLayout(t *testing.T) {
	ts := time.UnixMilli(1_750_000_000_123).UTC()
	id := MintRecordIDAt(ts)
	if got := id[6] & 0xf0; got != 0x70 {
		t.Errorf("version nibble = %#x, want 0x70", got)
	}
	if got := id[8] & 0xc0; got != 0x80 {
		t.Errorf("variant bits = %#x, want 0x80", got)
	}
	if got := id.Time(); !got.Equal(ts) {
		t.Errorf("Time() = %v, want %v", got, ts)
	}
	if got := id.String(); len(got) != RecordIDLen {
		t.Errorf("String() = %q (%d chars), want %d", got, len(got), RecordIDLen)
	}
	// The current clock must land inside the representable window and survive
	// the round trip through the encoding.
	now := MintRecordID()
	if d := time.Since(now.Time()); d < 0 || d > time.Minute {
		t.Errorf("MintRecordID timestamp %v is %v away from now", now.Time(), d)
	}
}

func TestRecordIDRoundTrip(t *testing.T) {
	for i := 0; i < 10000; i++ {
		want := MintRecordID()
		s := want.String()
		got, err := ParseRecordID(s)
		if err != nil {
			t.Fatalf("ParseRecordID(%q): %v", s, err)
		}
		if got != want {
			t.Fatalf("round trip of %q: got % x, want % x", s, got[:], want[:])
		}
		if got.String() != s {
			t.Fatalf("re-encode = %q, want %q", got.String(), s)
		}
	}
	// Boundary values: the encoding must cover the whole 128-bit range.
	for _, tc := range []struct {
		id   RecordID
		want string
	}{
		{RecordID{}, "00000000000000000000000000"},
		{RecordID{15: 1}, "00000000000000000000000001"},
		{RecordID{
			0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
			0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
		}, "7ZZZZZZZZZZZZZZZZZZZZZZZZZ"},
	} {
		if got := tc.id.String(); got != tc.want {
			t.Errorf("String(% x) = %q, want %q", tc.id[:], got, tc.want)
		}
		back, err := ParseRecordID(tc.want)
		if err != nil || back != tc.id {
			t.Errorf("ParseRecordID(%q) = % x, %v; want % x", tc.want, back[:], err, tc.id[:])
		}
	}
}

func TestRecordIDOrderingIsLexicographic(t *testing.T) {
	// Property: over many minted IDs, string order and timestamp order agree
	// whenever the timestamps differ. Random timestamps across a wide window
	// exercise carries through every character position.
	const n = 4000
	ids := make([]RecordID, n)
	for i := range ids {
		ms := int64(1_600_000_000_000) + rand.Int64N(400_000_000_000)
		ids[i] = MintRecordIDAt(time.UnixMilli(ms))
	}
	for i := 0; i < n; i++ {
		for j := 0; j < 8; j++ { // sample pairs; n^2 would be pointless work
			b := ids[rand.IntN(n)]
			a := ids[i]
			at, bt := a.Time(), b.Time()
			if at.Equal(bt) {
				continue
			}
			if (at.Before(bt)) != (a.String() < b.String()) {
				t.Fatalf("order mismatch: %s (%v) vs %s (%v)", a, at, b, bt)
			}
		}
	}

	// Sorting the encoded forms must reproduce non-decreasing mint times, and
	// same-millisecond ties must not disturb that.
	var mixed []RecordID
	base := time.UnixMilli(1_700_000_000_000)
	for k := 0; k < 50; k++ {
		ts := base.Add(time.Duration(k) * time.Millisecond)
		for r := 0; r < 5; r++ { // five IDs share each millisecond
			mixed = append(mixed, MintRecordIDAt(ts))
		}
	}
	rand.Shuffle(len(mixed), func(i, j int) { mixed[i], mixed[j] = mixed[j], mixed[i] })
	strs := make([]string, len(mixed))
	for i, id := range mixed {
		strs[i] = id.String()
	}
	sort.Strings(strs)
	var prev time.Time
	for i, s := range strs {
		id, err := ParseRecordID(s)
		if err != nil {
			t.Fatalf("ParseRecordID(%q): %v", s, err)
		}
		if i > 0 && id.Time().Before(prev) {
			t.Fatalf("sorted position %d has timestamp %v before previous %v", i, id.Time(), prev)
		}
		prev = id.Time()
	}
}

func TestRecordIDUniqueness(t *testing.T) {
	const n = 50000
	seen := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		s := MintRecordID().String()
		if _, dup := seen[s]; dup {
			t.Fatalf("duplicate record ID %q after %d mints", s, i)
		}
		seen[s] = struct{}{}
	}
	// Same-millisecond batches are the case per-clone counters got wrong.
	batch := mintBatchAt(time.UnixMilli(1_700_000_000_000), 20000)
	uniq := make(map[string]struct{}, len(batch))
	for _, s := range batch {
		uniq[s] = struct{}{}
	}
	if len(uniq) != len(batch) {
		t.Fatalf("same-millisecond batch: %d unique of %d", len(uniq), len(batch))
	}
}

func TestParseRecordIDRejects(t *testing.T) {
	valid := MintRecordID().String()
	for name, s := range map[string]string{
		"empty":            "",
		"too short":        valid[:25],
		"too long":         valid + "0",
		"node ID":          "go:saxpy.Saxpy",
		"legacy item ID":   "T-0134",
		"letter I":         "0" + strings.Repeat("1", 24) + "I",
		"letter L":         "0" + strings.Repeat("1", 24) + "L",
		"letter O":         "0O" + strings.Repeat("1", 24),
		"letter U":         "0U" + strings.Repeat("1", 24),
		"lowercase":        strings.ToLower(valid),
		"punctuation":      "0-" + strings.Repeat("1", 24),
		"non-ASCII":        "0é" + strings.Repeat("1", 23), // 'é' is two bytes
		"overflows 128bit": "8" + strings.Repeat("0", 25),
		"all Z":            strings.Repeat("Z", 26),
	} {
		if _, err := ParseRecordID(s); err == nil {
			t.Errorf("%s: ParseRecordID(%q) accepted, want error", name, s)
		} else if !strings.HasPrefix(err.Error(), "ids: ") || len(err.Error()) < 20 {
			t.Errorf("%s: unhelpful error %q", name, err)
		}
		if ValidRecordID(s) {
			t.Errorf("%s: ValidRecordID(%q) = true, want false", name, s)
		}
	}
	// The confusable characters name themselves rather than being remapped.
	_, err := ParseRecordID("0" + strings.Repeat("1", 24) + "O")
	if err == nil || !strings.Contains(err.Error(), "excluded") {
		t.Errorf("error for 'O' = %v, want an explicit exclusion message", err)
	}
	if !ValidRecordID(valid) {
		t.Errorf("ValidRecordID(%q) = false", valid)
	}
}

func TestShortenRecordIDHonorsFloor(t *testing.T) {
	id := MintRecordID().String()
	got, err := ShortenRecordID(id, nil)
	if err != nil {
		t.Fatalf("ShortenRecordID: %v", err)
	}
	if len(got) != MinRecordPrefixLen || !strings.HasPrefix(id, got) {
		t.Fatalf("ShortenRecordID with no peers = %q, want the first %d chars of %q",
			got, MinRecordPrefixLen, id)
	}
	// Unrelated ID shapes in the known set never force a longer prefix.
	got, err = ShortenRecordID(id, []string{id, "T-0134", "P-0088", "go:pkg.Fn"})
	if err != nil || len(got) != MinRecordPrefixLen {
		t.Fatalf("ShortenRecordID against legacy IDs = %q, %v", got, err)
	}
	if _, err := ShortenRecordID("T-0134", []string{id}); err == nil {
		t.Error("ShortenRecordID accepted a non-canonical ID")
	}
}

// TestShortenRecordIDIsAdaptive is the regression test for ADR-0013's caveat:
// UUIDv7 leads with the millisecond timestamp, so records minted together
// share a long run of characters and any fixed short length is ambiguous.
func TestShortenRecordIDIsAdaptive(t *testing.T) {
	const n = 500
	batch := mintBatchAt(time.UnixMilli(1_700_000_000_123), n)

	// Establish the trap: a git-style fixed 7-character prefix collides.
	fixed := make(map[string]int, n)
	for _, id := range batch {
		fixed[id[:7]]++
	}
	if len(fixed) != 1 {
		t.Fatalf("expected all %d same-millisecond IDs to share a 7-char prefix, got %d distinct",
			n, len(fixed))
	}

	// Adaptive shortening still yields unique, resolvable prefixes.
	seen := make(map[string]string, n)
	longest := 0
	for _, id := range batch {
		p, err := ShortenRecordID(id, batch)
		if err != nil {
			t.Fatalf("ShortenRecordID(%q): %v", id, err)
		}
		if len(p) < MinRecordPrefixLen {
			t.Fatalf("prefix %q is below the floor %d", p, MinRecordPrefixLen)
		}
		if !strings.HasPrefix(id, p) {
			t.Fatalf("prefix %q is not a prefix of %q", p, id)
		}
		if other, dup := seen[p]; dup {
			t.Fatalf("prefix %q shared by %q and %q", p, other, id)
		}
		seen[p] = id
		longest = max(longest, len(p))
		back, err := ResolveRecordID(p, batch)
		if err != nil || back != id {
			t.Fatalf("ResolveRecordID(%q) = %q, %v; want %q", p, back, err, id)
		}
	}
	if longest <= MinRecordPrefixLen {
		t.Fatalf("longest adaptive prefix is %d; a same-millisecond batch must "+
			"push past the floor of %d", longest, MinRecordPrefixLen)
	}
	t.Logf("same-millisecond batch of %d: adaptive prefixes ran to %d characters "+
		"(fixed 7 would have been %d-way ambiguous)", n, longest, n)

	// A batch minted in a real tight loop behaves the same way.
	live := make([]string, n)
	for i := range live {
		live[i] = MintRecordID().String()
	}
	got := make(map[string]struct{}, n)
	for _, id := range live {
		p, err := ShortenRecordID(id, live)
		if err != nil {
			t.Fatalf("ShortenRecordID(%q): %v", id, err)
		}
		if _, dup := got[p]; dup {
			t.Fatalf("live batch: duplicate prefix %q", p)
		}
		got[p] = struct{}{}
	}
}

func TestResolveRecordID(t *testing.T) {
	ts := time.UnixMilli(1_700_000_000_000)
	known := mintBatchAt(ts, 8)
	sort.Strings(known)

	// Hit: the full ID and a unique prefix both resolve.
	want := known[3]
	for _, p := range []string{want, want[:25]} {
		got, err := ResolveRecordID(p, known)
		if err != nil || got != want {
			t.Fatalf("ResolveRecordID(%q) = %q, %v; want %q", p, got, err, want)
		}
	}

	// Miss.
	missing := MintRecordIDAt(ts.Add(-time.Hour)).String()
	if _, err := ResolveRecordID(missing[:12], known); !errors.Is(err, ErrNoMatch) {
		t.Fatalf("miss returned %v, want ErrNoMatch", err)
	}

	// Ambiguity: at the raised floor (ADR-01KYEP) same-millisecond mints
	// no longer share MinRecordPrefixLen characters — the floor pins into
	// the random tail by design — so ambiguity needs a crafted sibling
	// diverging only past the shared prefix.
	sibling := want[:25] + map[bool]string{true: "0", false: "1"}[want[25:] != "0"]
	known = append(known, sibling)
	sort.Strings(known)
	shared := want[:MinRecordPrefixLen]
	_, err := ResolveRecordID(shared, known)
	var amb *AmbiguousPrefixError
	if !errors.As(err, &amb) {
		t.Fatalf("ambiguous prefix returned %v, want *AmbiguousPrefixError", err)
	}
	if amb.Prefix != shared {
		t.Errorf("Prefix = %q, want %q", amb.Prefix, shared)
	}
	wantCand := []string{}
	for _, k := range known {
		if strings.HasPrefix(k, shared) {
			wantCand = append(wantCand, k)
		}
	}
	if !slices.Equal(amb.Candidates, wantCand) {
		t.Errorf("Candidates = %v, want all %d known IDs", amb.Candidates, len(known))
	}
	for _, k := range wantCand {
		if !strings.Contains(amb.Error(), k) {
			t.Errorf("error message omits candidate %q: %s", k, amb.Error())
		}
	}
	if errors.Is(err, ErrNoMatch) {
		t.Error("ambiguity must be distinguishable from a miss")
	}

	// Empty prefix is a caller bug, not an ambiguity.
	if _, err := ResolveRecordID("", known); err == nil {
		t.Error("empty prefix accepted")
	}
	// Legacy sequential IDs stay resolvable through the same entry point.
	legacy := append(slices.Clone(known), "T-0134")
	if got, err := ResolveRecordID("T-0134", legacy); err != nil || got != "T-0134" {
		t.Fatalf("legacy resolve = %q, %v", got, err)
	}
	// An exactly spelled ID wins over a longer entry it prefixes.
	odd := []string{want, want + "-suffix"}
	if got, err := ResolveRecordID(want, odd); err != nil || got != want {
		t.Fatalf("exact match = %q, %v; want %q", got, err, want)
	}
}

func ExampleMintRecordID() {
	id := MintRecordIDAt(time.UnixMilli(1_700_000_000_123))
	fmt.Println(len(id.String()), id.Time().UTC().Format(time.RFC3339Nano))
	// Output: 26 2023-11-14T22:13:20.123Z
}

// TestPrefixPinsFivePMinusTwoTimestampBits pins the arithmetic the
// MinRecordPrefixLen comment argues from (B-01KYD4J): a p-character prefix
// of the fixed-width base32 encoding pins 5p-2 leading bits — the first
// character carries only 3 payload bits — so two IDs minted 2^(48-(5p-2))
// milliseconds apart share the p-character prefix, and two minted just
// past that boundary do not. The comment once claimed 5p, four times
// tighter than reality; this test keeps the two from drifting apart again.
func TestPrefixPinsFivePMinusTwoTimestampBits(t *testing.T) {
	const p = MinRecordPrefixLen
	pinned := 5*p - 2
	// At the raised floor the pinned bits exceed the 48 timestamp bits
	// (ADR-01KYEP: p=13 pins 63 = all 48 plus 15 random) — the mint-window
	// concept collapses and the guarantee becomes: IDs from DIFFERENT
	// milliseconds can never share a p-char prefix. Keep the arithmetic
	// honest for any future floor on either side of the 48-bit line.
	if pinned >= 48 {
		enc := func(ms int64) string { return MintRecordIDAt(time.UnixMilli(ms)).String()[:p] }
		base := int64(0x0123_4567_8900)
		for _, delta := range []int64{1, 1000, 1 << 20, 1 << 40} {
			if enc(base) == enc(base+delta) {
				t.Fatalf("IDs %d ms apart share the %d-char prefix — the floor must pin every timestamp bit", delta, p)
			}
		}
		// Same-millisecond mints agree on the full 48 timestamp bits: the
		// first 9 characters (45 bits) are pure timestamp and must match.
		a, b := MintRecordIDAt(time.UnixMilli(base)).String(), MintRecordIDAt(time.UnixMilli(base)).String()
		if a[:9] != b[:9] {
			t.Fatalf("same-ms mints diverge inside the timestamp run: %s vs %s", a, b)
		}
		return
	}
	windowMs := int64(1) << (48 - pinned)
	base := (int64(0x0123_4567_8900) >> (48 - pinned)) << (48 - pinned)
	enc := func(ms int64) string { return MintRecordIDAt(time.UnixMilli(ms)).String()[:p] }
	a := enc(base)
	inside := enc(base + windowMs - 1)
	outside := enc(base + windowMs)
	if a != inside {
		t.Fatalf("IDs %d ms apart (inside the 2^%d ms window) differ at %d chars: %s vs %s",
			windowMs-1, 48-pinned, p, a, inside)
	}
	if a == outside {
		t.Fatalf("IDs %d ms apart (outside the window) still share %d chars: %s vs %s",
			windowMs, p, a, outside)
	}
}
