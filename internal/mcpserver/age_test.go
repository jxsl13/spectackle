package mcpserver

import (
	"regexp"
	"testing"
	"time"
)

// TestHBAgeMinuteFloor pins the exact boundary the old seconds rendering
// flaked on (B-01KYDQTEM): any sub-minute age must render identical bytes.
// With %ds, an age of 0.9s vs 1.1s rendered "0s" vs "1s" and broke
// byte-identity across back-to-back state calls; with the minute floor both
// render "0m".
func TestHBAgeMinuteFloor(t *testing.T) {
	now := time.Now()
	cases := []struct {
		hb   time.Time
		want string
	}{
		{now, "0m"},
		{now.Add(-900 * time.Millisecond), "0m"},
		{now.Add(-1100 * time.Millisecond), "0m"}, // the exact old flake boundary
		{now.Add(-59 * time.Second), "0m"},
		{now.Add(-61 * time.Second), "1m"},
		{now.Add(-10 * time.Minute), "10m"},
	}
	for _, c := range cases {
		if got := hbAge(c.hb); got != c.want {
			t.Errorf("hbAge(now%+v) = %q, want %q", c.hb.Sub(now), got, c.want)
		}
	}
}

// TestLeaseLeftMinuteFloorAndClamp: remaining lifetime floors to minutes
// (59s left renders "0m" — about to expire, the safe reading for a waiting
// sibling) and never goes negative for a just-expired lease still listed.
func TestLeaseLeftMinuteFloorAndClamp(t *testing.T) {
	now := time.Now()
	cases := []struct {
		exp  time.Time
		want string
	}{
		{now.Add(59 * time.Second), "0m"},
		{now.Add(61 * time.Second), "1m"},
		{now.Add(5 * time.Minute), "4m"}, // scheduling jitter eats the boundary: floor of ~4m59.9s
		{now.Add(-3 * time.Second), "0m"},
	}
	for _, c := range cases {
		if got := leaseLeft(c.exp); got != c.want {
			t.Errorf("leaseLeft(now%+v) = %q, want %q", c.exp.Sub(now), got, c.want)
		}
	}
}

// TestStateAgLineMinuteGrammar: a real state render must carry the ag line
// in the documented grammar (docs/tools.md): `ag <name> <item|-> <age>m
// <wt|main>` — the unit suffix is the load-bearing byte here, seconds must
// not reappear.
func TestStateAgLineMinuteGrammar(t *testing.T) {
	s := newTestServer(t, t.TempDir())
	res, _, err := s.state(stateIn{})
	out := resText(t, res, err)

	agLine := regexp.MustCompile(`(?m)^ag \S+ \S+ (\d+)([sm]) (\S+)$`)
	m := agLine.FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("state output has no parsable ag line:\n%s", out)
	}
	if m[2] != "m" {
		t.Fatalf("ag line age unit = %q, want \"m\" (seconds rendering regressed): %q", m[2], m[0])
	}
	if m[1] != "0" {
		t.Fatalf("fresh self-heartbeat age = %sm, want 0m: %q", m[1], m[0])
	}
}
