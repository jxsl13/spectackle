package mcpserver

import (
	"strings"
	"testing"
	"time"

	"github.com/jxsl13/spectackle/internal/ids"
	"github.com/jxsl13/spectackle/internal/item"
	"github.com/jxsl13/spectackle/internal/lifecycle"
)

// The short-ID prefix contract at the tool boundary (T-0136, ADR-0013). The
// rest of the suite exercises it implicitly — every helper that reads an ID
// off a result reads the DISPLAY form and feeds it straight back — so these
// tests cover the parts that implicit use cannot reach: the refusal shapes,
// the archived-tombstone half of the resolution set, and the round trip stated
// as an assertion rather than relied upon as a side effect.

// TestPrefixResolvesThroughSeveralTools: one record, addressed by its full ID
// and by a short prefix, through more than one tool, is the same record.
func TestPrefixResolvesThroughSeveralTools(t *testing.T) {
	root := t.TempDir()
	s, sess := connectRootWithServer(t, root)

	display := draftID(t, sess, map[string]any{
		"kind": "proposal", "title": "cache kernels in VRAM",
		"body": "Keep compiled kernels resident.",
	})
	full := fullID(t, s, display)
	if full == display {
		t.Fatalf("draft emitted the full ID %q — nothing was shortened", full)
	}
	if !strings.HasPrefix(full, display) {
		t.Fatalf("displayed %q is not a prefix of stored %q", display, full)
	}

	// get, by both forms
	for _, id := range []string{full, display} {
		out := callText(t, sess, "get", map[string]any{"id": id})
		if !strings.Contains(out, "Keep compiled kernels resident.") {
			t.Fatalf("get %q did not reach the record: %q", id, out)
		}
	}

	// grill, by the short form, and move, by the full one: both land on the
	// same record, which move then proves by reporting its new state.
	if out := callText(t, sess, "grill", map[string]any{"id": display}); strings.Contains(out, "nf ") {
		t.Fatalf("grill did not resolve the prefix: %q", out)
	}
	out := callText(t, sess, "move", map[string]any{"id": display, "to": "submitted"})
	wantItemRecord(t, out, full, "proposal submitted")

	it, ok, err := item.Get(s.ws, full)
	if err != nil || !ok || it.State != item.StateSubmitted {
		t.Fatalf("the two forms addressed different records: %+v %v %v", it, ok, err)
	}
}

// TestAmbiguousPrefixRefusesAndNamesEveryCandidate: ambiguity is an error that
// hands back the whole candidate set, never a guess. Two records minted in the
// same millisecond are the honest fixture — their tails agree far past the
// six-character floor, which is exactly the case ADR-0013's caveat is about.
func TestAmbiguousPrefixRefusesAndNamesEveryCandidate(t *testing.T) {
	root := t.TempDir()
	s, sess := connectRootWithServer(t, root)

	// same-millisecond mint, seeded directly so the collision is deterministic
	// rather than dependent on how fast the machine runs.
	ts := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	var full []string
	for _, title := range []string{"first", "second"} {
		id := item.MintIDAt("task", ts)
		if err := item.Upsert(s.ws, item.Item{
			ID: id, Kind: "task", State: item.StateDraft, Title: title,
		}); err != nil {
			t.Fatal(err)
		}
		full = append(full, id)
	}
	// At the raised floor (ADR-01KYEP) same-millisecond mints no longer
	// share MinRecordPrefixLen characters — the floor pins into the random
	// tail. Ambiguity needs a crafted sibling diverging only past the
	// shared prefix; the second minted item stays as an unrelated peer.
	sibling := full[0][:len(full[0])-1] + map[bool]string{true: "0", false: "1"}[!strings.HasSuffix(full[0], "0")]
	if err := item.Upsert(s.ws, item.Item{
		ID: sibling, Kind: "task", State: item.StateDraft, Title: "crafted sibling",
	}); err != nil {
		t.Fatal(err)
	}
	full = append(full, sibling)
	shared := full[0][:2+ids.MinRecordPrefixLen] // "T-" + the floor
	if !strings.HasPrefix(sibling, shared) {
		t.Fatalf("fixture broken: %q and %q do not share the floor", full[0], sibling)
	}

	out := callText(t, sess, "get", map[string]any{"id": shared})
	if !strings.Contains(out, "ambiguous prefix") {
		t.Fatalf("ambiguous prefix was not refused: %q", out)
	}
	// only the prefix-sharers are candidates at the raised floor: the
	// crafted sibling and its origin — the unrelated same-ms mint diverges
	// inside its random tail and rightly stays unnamed.
	candidates := []string{full[0], sibling}
	for _, id := range candidates {
		short := shortID(t, s, id)
		if !strings.Contains(out, short) {
			t.Fatalf("refusal does not name candidate %s (as %s): %q", id, short, out)
		}
	}
	// and the named candidates are usable as-is: one more call, no guessing.
	for _, id := range candidates {
		out := callText(t, sess, "get", map[string]any{"id": shortID(t, s, id)})
		if strings.Contains(out, "ambiguous") || strings.Contains(out, "nf ") {
			t.Fatalf("a candidate named by the refusal did not resolve: %q", out)
		}
	}
}

// TestUnknownPrefixIsNotFoundNotAmbiguity: the two failure modes stay
// distinguishable. A prefix matching nothing keeps get's nf-with-nearest
// behavior (SPX-ARC-003) — reporting it as an ambiguity would send the caller
// looking for candidates that do not exist.
func TestUnknownPrefixIsNotFoundNotAmbiguity(t *testing.T) {
	root := t.TempDir()
	_, sess := connectRootWithServer(t, root)

	draftID(t, sess, map[string]any{"kind": "task", "title": "the only record"})

	out := callText(t, sess, "get", map[string]any{"id": "T-ZZZZZZ"})
	if strings.Contains(out, "ambiguous") {
		t.Fatalf("unknown prefix reported as ambiguous: %q", out)
	}
	if !strings.HasPrefix(out, "nf ") {
		t.Fatalf("unknown prefix did not answer nf: %q", out)
	}
}

// TestTombstonedArchivedIDResolvesByPrefix: archived records leave work.md and
// survive only as journal tombstones. If the resolution set were the live
// items alone, the entire archived history would stop being addressable by the
// short form the tools themselves emit.
func TestTombstonedArchivedIDResolvesByPrefix(t *testing.T) {
	root := t.TempDir()
	s, sess := connectRootWithServer(t, root)

	archived, err := lifecycle.Draft(s.ws, nil, "proposal", "long shipped", "", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lifecycle.Move(s.ws, archived.ID, item.StateArchived, "shipped"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := item.Get(s.ws, archived.ID); ok {
		t.Fatal("fixture broken: the archived item is still in work.md")
	}

	out := callText(t, sess, "get", map[string]any{"id": shortID(t, s, archived.ID)})
	if !strings.Contains(out, "journal tombstone") {
		t.Fatalf("archived record not reachable by prefix: %q", out)
	}
	if !strings.Contains(out, "long shipped") {
		t.Fatalf("prefix resolved to the wrong record: %q", out)
	}
}

// TestRenderedIDFedBackResolves is the round trip ADR-0013 exists for, stated
// once as an assertion: whatever a result prints is a legal argument. It walks
// several tools because each renders through its own path, and a single one of
// them forgetting to shorten (or shortening against a stale record set) is
// precisely the bug this catches.
func TestRenderedIDFedBackResolves(t *testing.T) {
	root := t.TempDir()
	s, sess := connectRootWithServer(t, root)

	full := draftFullID(t, s, sess, map[string]any{"kind": "task", "title": "round trip"})
	// a second record so the floor is genuinely contested and the emitted
	// length has to be computed rather than defaulted.
	draftID(t, sess, map[string]any{"kind": "task", "title": "contender"})

	for _, tool := range []string{"get", "state", "find"} {
		args := map[string]any{"id": full}
		switch tool {
		case "state":
			args = map[string]any{}
		case "find":
			args = map[string]any{"q": "round trip", "scope": "task"}
		}
		out := callText(t, sess, tool, args)
		emitted := ""
		for _, l := range strings.Split(out, "\n") {
			f := strings.Fields(l)
			if len(f) >= 2 && f[0] == "i" && strings.HasPrefix(full, f[1]) {
				emitted = f[1]
			}
		}
		if emitted == "" {
			t.Fatalf("%s emitted no record for %s: %q", tool, full, out)
		}
		if emitted == full {
			t.Fatalf("%s emitted the full ID, not the display form: %q", tool, out)
		}
		back := callText(t, sess, "get", map[string]any{"id": emitted})
		if strings.Contains(back, "ambiguous") || strings.HasPrefix(back, "nf ") {
			t.Fatalf("%s emitted %q, which get then refused: %q", tool, emitted, back)
		}
		if !strings.Contains(back, "round trip") {
			t.Fatalf("%s emitted %q, which resolved elsewhere: %q", tool, emitted, back)
		}
	}
}

// TestPastedIDIsNormalized: a human or an LLM re-typing an ID out of a chat log
// may lowercase it or hit one of the four characters Crockford excludes as
// confusable. internal/ids refuses those (it validates machine-produced
// values); the boundary folds them, which is the split the alphabet was
// designed for. The fold can only turn a guaranteed miss into a hit.
func TestPastedIDIsNormalized(t *testing.T) {
	root := t.TempDir()
	s, sess := connectRootWithServer(t, root)

	full := draftFullID(t, s, sess, map[string]any{
		"kind": "task", "title": "pasted", "body": "Body of the pasted record.",
	})
	display := shortID(t, s, full)

	// lowercase is always safe to fold: no minted ID contains one.
	out := callText(t, sess, "get", map[string]any{"id": strings.ToLower(display)})
	if !strings.Contains(out, "Body of the pasted record.") {
		t.Fatalf("lowercased ID did not resolve: %q", out)
	}

	// and a confusable substitution, when the record's own tail contains the
	// character it gets confused with.
	for from, to := range map[string]string{"1": "l", "0": "O", "V": "U"} {
		if !strings.Contains(display[2:], from) {
			continue
		}
		typo := display[:2] + strings.Replace(display[2:], from, to, 1)
		out := callText(t, sess, "get", map[string]any{"id": typo})
		if !strings.Contains(out, "Body of the pasted record.") {
			t.Fatalf("confusable %q->%q (%q) did not resolve: %q", from, to, typo, out)
		}
	}
}
