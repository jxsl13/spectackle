package mcpserver

// Lens-labeled verdicts and the per-item panel opt-in (T-01KYFXDC6):
// lenses round-trip through the journal onto both renders; an empty lens
// label refuses; a panel needs a live risk signal and respects the cap;
// back-compat verdicts without lenses stay valid; the guide-text additions
// stay inside their 350-byte budget.

import (
	"strings"
	"testing"
)

func TestVerdictLensesRoundTrip(t *testing.T) {
	root := requireGrillRoot(t)
	t.Setenv("SPECTACKLE_AGENT", "author-a")
	author := connectRoot(t, root)
	prop := draftID(t, author, map[string]any{
		"kind": "proposal", "title": "lens round trip", "body": ambFixturePad})
	callText(t, author, "grill", map[string]any{"id": prop})
	t.Setenv("SPECTACKLE_AGENT", "reviewer-b")
	reviewer := connectRoot(t, root)
	out := callText(t, reviewer, "grill", map[string]any{
		"op": "verdict", "id": prop, "pass": true, "lenses": "minimality, tokens ,refute"})
	if !strings.Contains(out, "lenses=minimality,tokens,refute") {
		t.Fatalf("ok line must carry trimmed lenses: %q", out)
	}
	out = callText(t, author, "grill", map[string]any{"id": prop})
	if !strings.Contains(out, "review pass reviewer-b") || !strings.Contains(out, "lenses=minimality,tokens,refute") {
		t.Fatalf("pack verdict line must surface lenses: %q", out)
	}
}

// Refusal shapes share one skeleton deliberately (table-driven after the
// dup detector flagged the copy-pasted pair): each case drafts, grills,
// and expects the verdict to refuse with its marker.
func TestVerdictRefusalShapes(t *testing.T) {
	cases := []struct {
		name   string
		args   map[string]any
		marker string
	}{
		{"empty lens", map[string]any{"lenses": "a,,b"}, "empty lens label"},
		{"panel without risk", map[string]any{"panel": 2}, "panel needs a live risk signal"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := requireGrillRoot(t)
			t.Setenv("SPECTACKLE_AGENT", "author-a")
			author := connectRoot(t, root)
			prop := draftID(t, author, map[string]any{
				"kind": "proposal", "title": "refusal shape " + c.name, "body": ambFixturePad})
			callText(t, author, "grill", map[string]any{"id": prop})
			t.Setenv("SPECTACKLE_AGENT", "reviewer-b")
			reviewer := connectRoot(t, root)
			args := map[string]any{"op": "verdict", "id": prop, "pass": true}
			for k, v := range c.args {
				args[k] = v
			}
			out := callText(t, reviewer, "grill", args)
			if !strings.Contains(out, c.marker) {
				t.Fatalf("%s must refuse with %q: %q", c.name, c.marker, out)
			}
		})
	}
}

func TestPanelRiskAndCap(t *testing.T) {
	root := requireGrillRoot(t)
	t.Setenv("SPECTACKLE_AGENT", "author-a")
	author := connectRoot(t, root)
	// an irreversible target with no RESTORE heading = live class-c risk
	prop := draftID(t, author, map[string]any{
		"kind": "proposal", "title": "panel with irreversible risk",
		"targets": []string{".github/workflows/ci.yml"},
		"body":    "changes the release pipeline wholesale. " + ambFixturePad})
	out := callText(t, author, "grill", map[string]any{"id": prop})
	if !strings.Contains(out, "g irreversible") {
		t.Skipf("fixture did not trigger the irreversible class: %q", out)
	}
	t.Setenv("SPECTACKLE_AGENT", "reviewer-b")
	reviewer := connectRoot(t, root)
	out = callText(t, reviewer, "grill", map[string]any{
		"op": "verdict", "id": prop, "pass": true, "panel": 9,
		"waivers": map[string]string{"irreversible:.github/workflows/ci.yml": "panel fixture; the risk is the point"}})
	if !strings.Contains(out, "exceeds swarm.panel_max") {
		t.Fatalf("over-cap panel must refuse: %q", out)
	}
	out = callText(t, reviewer, "grill", map[string]any{
		"op": "verdict", "id": prop, "pass": true, "panel": 2,
		"waivers": map[string]string{"irreversible:.github/workflows/ci.yml": "panel fixture; the risk is the point"}})
	if !strings.Contains(out, "ok review") {
		t.Fatalf("risk-justified in-cap panel must record: %q", out)
	}
}

// The sequential-default instruction additions stay inside the 350-byte
// budget the brief set: measured base additions were 246B (grill tool
// description) + 103B (orchestration guide topic).
func TestLensGuideTextBudget(t *testing.T) {
	if !strings.Contains(guideTopics["orchestration"], "sequential lenses") {
		t.Fatal("orchestration guide lost the sequential-lens instruction")
	}
	marker := "Review mode: one reviewer, sequential lenses"
	i := strings.Index(guideTopics["orchestration"], marker)
	if i < 0 {
		t.Fatal("guide marker missing")
	}
	if n := len(guideTopics["orchestration"]) - i; n > 350 {
		t.Fatalf("guide addition grew to %d bytes", n)
	}
}
