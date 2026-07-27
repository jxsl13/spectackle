package relnotes

// Golden determinism over a fixture journal (T-01KYGCJ6).

import (
	"testing"
	"time"

	"github.com/jxsl13/spectackle/internal/journal"
)

func TestRenderGolden(t *testing.T) {
	ts := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)
	events := []journal.Event{
		{Ev: journal.EvArchive, K: "task", ID: "T-01AAAAAAAAAAAAAAAAAAAAAAAA", T: ts,
			Sum: "short display IDs on git surfaces. Forensic detail that must not leak."},
		{Ev: journal.EvArchive, K: "bug", ID: "B-01BBBBBBBBBBBBBBBBBBBBBBBB", T: ts,
			Sum: "the archive edge is atomic."},
		{Ev: journal.EvArchive, K: "task", ID: "T-00OLDOLDOLDOLDOLDOLDOLDOLD", T: ts.Add(-48 * time.Hour),
			Sum: "predates the window."},
		{Ev: journal.EvMove, K: "task", ID: "T-01CCCCCCCCCCCCCCCCCCCCCCCC", T: ts, Sum: "not an archive"},
		{Ev: journal.EvArchive, K: "task", ID: "T-01DDDDDDDDDDDDDDDDDDDDDDDD", T: ts, Sum: "  "},
		// compensated-retry dedup: the WINNING later-T event sits EARLIER
		// in the slice than the loser — position must not decide.
		{Ev: journal.EvArchive, K: "task", ID: "T-01EEEEEEEEEEEEEEEEEEEEEEEE", T: ts.Add(time.Minute), Sum: "the retry that landed."},
		{Ev: journal.EvArchive, K: "task", ID: "T-01EEEEEEEEEEEEEEEEEEEEEEEE", T: ts, Sum: "the failed first attempt."},
		// equal-T tie breaks by Eid, deterministically
		{Ev: journal.EvArchive, K: "task", ID: "T-01FFFFFFFFFFFFFFFFFFFFFFFF", T: ts, Eid: "aa", Sum: "tie loser."},
		{Ev: journal.EvArchive, K: "task", ID: "T-01FFFFFFFFFFFFFFFFFFFFFFFF", T: ts, Eid: "bb", Sum: "tie winner."},
		// N-way, not pairwise: three archives, last T wins
		{Ev: journal.EvArchive, K: "bug", ID: "B-01GGGGGGGGGGGGGGGGGGGGGGGG", T: ts, Sum: "first."},
		{Ev: journal.EvArchive, K: "bug", ID: "B-01GGGGGGGGGGGGGGGGGGGGGGGG", T: ts.Add(2 * time.Minute), Sum: "third and final."},
		{Ev: journal.EvArchive, K: "bug", ID: "B-01GGGGGGGGGGGGGGGGGGGGGGGG", T: ts.Add(time.Minute), Sum: "second."},
	}
	got := Render(events, ts.Add(-time.Hour))
	want := `## Features

- **T-01AAAAAAAAAAA** — short display IDs on git surfaces.
- **T-01EEEEEEEEEEE** — the retry that landed.
- **T-01FFFFFFFFFFF** — tie winner.

## Fixes

- **B-01BBBBBBBBBBB** — the archive edge is atomic.
- **B-01GGGGGGGGGGG** — third and final.
`
	if got != want {
		t.Fatalf("golden mismatch:\ngot:\n%s\nwant:\n%s", got, want)
	}
	if got2 := Render(events, ts.Add(-time.Hour)); got2 != got {
		t.Fatal("render is not deterministic")
	}
}
