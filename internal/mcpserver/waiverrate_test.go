package mcpserver

// Waiver-rate tripwire properties (T-01KYFXEP): counts render with the
// rate, small samples and clean renders stay out of the math, sub-threshold
// stays silent, and check output never carries the line.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jxsl13/spectackle/internal/journal"
)

// mkVerdicts builds a synthetic event stream: n verdicts, each judging a
// render with `open` findings and waiving `waive` of them.
func mkVerdicts(tag string, n, open, waive int) []journal.Event {
	var events []journal.Event
	base := time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC)
	for i := 0; i < n; i++ {
		id := "T-" + tag + strings.Repeat("A", 8) + string(rune('A'+i))
		hash := "h" + tag + string(rune('a'+i))
		events = append(events,
			journal.Event{T: base.Add(time.Duration(2*i) * time.Minute),
				Ev: journal.EvGrill, ID: id, Hash: hash, Open: open},
			journal.Event{T: base.Add(time.Duration(2*i+1) * time.Minute),
				Ev: journal.EvReview, ID: id, Hash: hash, Pass: true,
				Wv: make([]string, waive)},
		)
	}
	return events
}

func TestWaiverRateRenders(t *testing.T) {
	// 10 verdicts, each render open=1; 6 waive their finding, 4 address it.
	events := append(mkVerdicts("W", 6, 1, 1), mkVerdicts("X", 4, 1, 0)...)
	got := waiverRateLine(events)
	want := "w waiver-rate 60% over last 10 verdicts (waived=6 addressed=4)"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestWaiverRateSmallSampleSilent(t *testing.T) {
	if got := waiverRateLine(mkVerdicts("W", 4, 1, 1)); got != "" {
		t.Fatalf("4 qualifying verdicts must stay silent: %q", got)
	}
}

func TestWaiverRateCleanRendersExcluded(t *testing.T) {
	// 4 waiving verdicts + 10 clean-render verdicts: the clean ones do not
	// qualify, so the sample stays below the floor and the line is silent —
	// a clean streak neither dilutes nor triggers.
	events := append(mkVerdicts("W", 4, 1, 1), mkVerdicts("C", 10, 0, 0)...)
	if got := waiverRateLine(events); got != "" {
		t.Fatalf("clean renders must not qualify: %q", got)
	}
	// and with 5 qualifying + clean noise, the rate computes over the 5.
	events = append(mkVerdicts("Q", 5, 2, 2), mkVerdicts("C", 10, 0, 0)...)
	got := waiverRateLine(events)
	if !strings.Contains(got, "over last 5 verdicts (waived=10 addressed=0)") {
		t.Fatalf("qualifying-only denominator wrong: %q", got)
	}
}

func TestWaiverRateBelowThresholdSilent(t *testing.T) {
	// 2 waived of 10 open total = 20% < 50%.
	events := append(mkVerdicts("W", 2, 1, 1), mkVerdicts("X", 8, 1, 0)...)
	if got := waiverRateLine(events); got != "" {
		t.Fatalf("sub-threshold rate must stay silent: %q", got)
	}
}

// check output is byte-identical with a tripping journal present: the line
// lives in state and the packs, never in the CI-string-matched surface.
func TestWaiverRateNeverInCheck(t *testing.T) {
	root := t.TempDir()
	sess := connectRoot(t, root)
	before := callText(t, sess, "check", map[string]any{})
	// plant a tripping journal behind the server's back (test-only write)
	var b strings.Builder
	for _, e := range append(mkVerdicts("W", 6, 1, 1), mkVerdicts("X", 4, 1, 0)...) {
		raw, _ := json.Marshal(e)
		b.Write(raw)
		b.WriteByte('\n')
	}
	if err := os.MkdirAll(filepath.Join(root, ".spectackle"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".spectackle", "journal.ndjson"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	s2, sess2 := connectRootWithServer(t, root)
	if wr := s2.waiverRate(); !strings.Contains(wr, "waiver-rate") {
		t.Fatalf("fixture journal must trip the line: %q", wr)
	}
	after := callText(t, sess2, "check", map[string]any{})
	if before != after {
		t.Fatalf("check changed with a tripping journal present:\nbefore %q\nafter  %q", before, after)
	}
	if strings.Contains(after, "waiver-rate") {
		t.Fatalf("check must never carry the tripwire: %q", after)
	}
	out := callText(t, sess2, "state", map[string]any{})
	if !strings.Contains(out, "w waiver-rate") {
		t.Fatalf("state must carry the tripwire in #health: %q", out)
	}
}
