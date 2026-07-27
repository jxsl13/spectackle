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
	}
	got := Render(events, ts.Add(-time.Hour))
	want := `## Features

- **T-01AAAAAAAAAAA** — short display IDs on git surfaces.

## Fixes

- **B-01BBBBBBBBBBB** — the archive edge is atomic.
`
	if got != want {
		t.Fatalf("golden mismatch:\ngot:\n%s\nwant:\n%s", got, want)
	}
	if got2 := Render(events, ts.Add(-time.Hour)); got2 != got {
		t.Fatal("render is not deterministic")
	}
}
