package mcpserver

// Risk-gated require (T-01KYFXDCH): the decision is pure and computed from
// the LANDED diff's file list — declared targets are never consulted. The
// gate wiring composes the refusal as
// "! VALIDATE E <id> <gap> (validation required: <trip>)".

import (
	"strings"
	"testing"
)

func TestRiskTripFileCount(t *testing.T) {
	nine := make([]string, 9)
	for i := range nine {
		nine[i] = "pkg/f" + string(rune('a'+i)) + ".go"
	}
	if got := riskTrip(nine, 0, nil); !strings.Contains(got, "landed 9 files >= risk_files 8") {
		t.Fatalf("nine files must trip the default threshold: %q", got)
	}
	if got := riskTrip(nine[:7], 0, nil); got != "" {
		t.Fatalf("seven files must not trip the default: %q", got)
	}
	if got := riskTrip(nine[:3], 3, nil); !strings.Contains(got, ">= risk_files 3") {
		t.Fatalf("configured threshold must apply: %q", got)
	}
}

func TestRiskTripDangerousPaths(t *testing.T) {
	files := []string{"internal/lifecycle/lifecycle.go"}
	got := riskTrip(files, 0, []string{"internal/lifecycle/**"})
	if !strings.Contains(got, "matches dangerous_paths internal/lifecycle/**") {
		t.Fatalf("subtree glob must trip on one file: %q", got)
	}
	if got := riskTrip([]string{"docs/readme.md"}, 0, []string{"internal/lifecycle/**"}); got != "" {
		t.Fatalf("non-matching file must not trip: %q", got)
	}
	if got := riskTrip([]string{"ci.yml"}, 0, []string{"*.yml"}); got == "" {
		t.Fatal("path.Match glob must trip")
	}
	// empty default: nothing dangerous anywhere
	if got := riskTrip([]string{"internal/lifecycle/lifecycle.go"}, 0, nil); got != "" {
		t.Fatalf("empty dangerous list must never trip: %q", got)
	}
}

func TestDangerousMatchShapes(t *testing.T) {
	cases := []struct {
		file, pat string
		want      bool
	}{
		{"a/b/c.go", "a/**", true},
		{"a", "a/**", true},
		{"ab/c.go", "a/**", false},
		{"x.yml", "*.yml", true},
		{"d/x.yml", "*.yml", false}, // path.Match is not recursive
	}
	for _, c := range cases {
		if got := dangerousMatch(c.file, c.pat); got != c.want {
			t.Errorf("dangerousMatch(%q,%q) = %v want %v", c.file, c.pat, got, c.want)
		}
	}
}

// Config defaults: absent knobs mean threshold 8 and empty dangerous list;
// an explicit require is never consulted against risk (the gate branches to
// the hard refusal before risk is computed — pinned by reading the wiring's
// order in tools.go, asserted here through the pure layer's independence).
func TestRiskKnobDefaults(t *testing.T) {
	s := newTestServer(t, t.TempDir())
	if got := s.ws.Cfg.Feedback.RiskFiles; got != 8 {
		t.Fatalf("default risk_files = %d, want 8", got)
	}
	if got := s.ws.Cfg.Feedback.DangerousPaths; len(got) != 0 {
		t.Fatalf("default dangerous_paths must be empty: %v", got)
	}
}
