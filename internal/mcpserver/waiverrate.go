package mcpserver

// Waiver-rate tripwire (T-01KYFXEP, P-01KYES / ADR-01KYES0TR): every
// finding is waivable with a reason, and the counterweight is VISIBILITY,
// never a veto — a gate that vetoed on waiver rate would teach padding the
// findings; a visible rate teaches judgment. The line renders in state's
// #health section and after the verdict line of the grill/validate packs,
// and NEVER in check: check's single ok path is CI-string-matched
// (the coverage-gate lesson, T-01KYD87ZN).

import (
	"fmt"
	"sort"

	"github.com/jxsl13/spectackle/internal/journal"
)

const (
	// waiverRateWindow is how many qualifying verdicts (newest first,
	// grill and validate pooled) the rate is computed over.
	waiverRateWindow = 20
	// waiverRateMinSample: below this many qualifying verdicts the line
	// stays silent — a rate over three verdicts is noise wearing a
	// percent sign, not a signal.
	waiverRateMinSample = 5
	// waiverRateThreshold: below this the line stays silent.
	waiverRateThreshold = 0.5
)

// waiverRateLine computes the tripwire over the workspace's journals.
// Qualifying verdicts are those whose bound render had open findings —
// clean-streak verdicts (open=0) are excluded from the denominator
// entirely, so a run of clean grills cannot dilute the signal. Returns ""
// when silent (small sample or below threshold).
func waiverRateLine(events []journal.Event) string {
	// Renders indexed by ID+hash so each verdict finds the open count it
	// judged; last render wins for a given pair, matching reviewState.
	type renderKey struct{ id, hash string }
	renderOpen := map[renderKey]int{}
	for _, e := range events {
		if e.Ev == journal.EvGrill || (e.Ev == journal.EvValidate && e.Op == "render") {
			renderOpen[renderKey{e.ID, e.Hash}] = e.Open
		}
	}
	type verdict struct {
		t      int64
		open   int
		waived int
	}
	var vs []verdict
	for _, e := range events {
		isVerdict := e.Ev == journal.EvReview || (e.Ev == journal.EvValidate && e.Op == "verdict")
		if !isVerdict {
			continue
		}
		open, ok := renderOpen[renderKey{e.ID, e.Hash}]
		if !ok || open == 0 {
			continue // unqualifying: no bound render, or a clean render
		}
		vs = append(vs, verdict{t: e.T.UnixNano(), open: open, waived: len(e.Wv)})
	}
	sort.Slice(vs, func(i, j int) bool { return vs[i].t > vs[j].t })
	if len(vs) > waiverRateWindow {
		vs = vs[:waiverRateWindow]
	}
	if len(vs) < waiverRateMinSample {
		return ""
	}
	waived, addressed := 0, 0
	for _, v := range vs {
		waived += v.waived
		a := v.open - v.waived
		if a < 0 {
			a = 0 // waivers for keys beyond the render are ignored upstream
		}
		addressed += a
	}
	if waived+addressed == 0 {
		return ""
	}
	rate := float64(waived) / float64(waived+addressed)
	if rate < waiverRateThreshold {
		return ""
	}
	return fmt.Sprintf("w waiver-rate %d%% over last %d verdicts (waived=%d addressed=%d)",
		int(rate*100+0.5), len(vs), waived, addressed)
}

// waiverRate reads the workspace journals and renders the line ("" when
// silent) — the one shared entry the three surfaces call.
func (s *Server) waiverRate() string {
	events, err := journal.ReadAll(s.ws)
	if err != nil {
		return ""
	}
	return waiverRateLine(events)
}
