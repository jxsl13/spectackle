// Package relnotes derives release notes from the journal's archive
// tombstones (T-01KYGCJ6): the archive note is the training signal, and
// here it doubles as the change log — no hand-written drift by
// construction. Only the summary HEAD line (first sentence, capped) of
// each tombstone is rendered; the human-written intro fronts the file.
package relnotes

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jxsl13/spectackle/internal/journal"
)

// headLine returns the first sentence of a tombstone summary, capped —
// public notes carry the claim, the tombstone carries the forensics.
func headLine(sum string) string {
	s := strings.Join(strings.Fields(sum), " ")
	if i := strings.Index(s, ". "); i > 0 && i < 220 {
		s = s[:i+1]
	}
	if len(s) > 220 {
		s = s[:220] + "…"
	}
	return s
}

// shortID renders the display form (kind + 13 body chars).
func shortID(id string) string {
	kind, body, ok := strings.Cut(id, "-")
	if !ok || len(body) <= 13 {
		return id
	}
	return kind + "-" + body[:13]
}

var kindHeadings = []struct{ kind, heading string }{
	{"proposal", "## Delivered proposals"},
	{"task", "## Features"},
	{"bug", "## Fixes"},
	{"adr", "## Decisions"},
	{"research", "## Research"},
}

// Render generates the notes body from archive events at or after since
// (zero time = everything). Deterministic: grouped by kind in a fixed
// order, sorted by ID (mint order) within each group.
func Render(events []journal.Event, since time.Time) string {
	// One line per item: a compensated archive retry appends a second
	// EvArchive for the same ID (tools.go archive compensation), and the
	// LATEST tombstone is the final one. Compare by T value — ReadAll
	// concatenates context files in directory order and replay preserves
	// original timestamps, so slice position proves nothing. Equal-T ties
	// (whole-second truncation) break by Eid for determinism.
	latest := map[string]journal.Event{}
	for _, e := range events {
		if e.Ev != journal.EvArchive || strings.TrimSpace(e.Sum) == "" {
			continue
		}
		if !since.IsZero() && e.T.Before(since) {
			continue
		}
		if p, ok := latest[e.ID]; ok && (e.T.Before(p.T) || (e.T.Equal(p.T) && e.Eid < p.Eid)) {
			continue
		}
		latest[e.ID] = e
	}
	byKind := map[string][]journal.Event{}
	for _, e := range latest {
		byKind[e.K] = append(byKind[e.K], e)
	}
	var b strings.Builder
	for _, kh := range kindHeadings {
		es := byKind[kh.kind]
		if len(es) == 0 {
			continue
		}
		sort.Slice(es, func(i, j int) bool { return es[i].ID < es[j].ID })
		b.WriteString(kh.heading + "\n\n")
		for _, e := range es {
			fmt.Fprintf(&b, "- **%s** — %s\n", shortID(e.ID), headLine(e.Sum))
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}
