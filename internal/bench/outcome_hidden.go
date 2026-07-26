package bench

// The five hidden acceptance tests — compiled into the HARNESS binary,
// written only into scratch copies at scoring time, never into the fixture
// during a run (the walk test pins that). One test per planted edge case;
// together they define "complete" for the limiter brief. The -v output
// contract (--- PASS/--- FAIL lines) has been stable since go1.14, which
// runHiddenTests relies on for per-test parsing.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const hiddenAcceptance = `package limiter

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Edge 1: Allow(0) is true and consumes nothing.
func TestHiddenZeroConsumesNothing(t *testing.T) {
	b := New(2, 0)
	if !b.Allow(0) {
		t.Fatal("Allow(0) must be true")
	}
	if !b.Allow(2) {
		t.Fatal("Allow(0) must not have consumed tokens")
	}
}

// Edge 2: n greater than capacity is false even on a full bucket.
func TestHiddenOverCapacity(t *testing.T) {
	b := New(3, 1)
	if b.Allow(4) {
		t.Fatal("n>capacity must be false on a full bucket")
	}
}

// Edge 3: negative n is refused, never panics, consumes nothing.
func TestHiddenNegative(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Allow(-1) panicked: %v", r)
		}
	}()
	b := New(2, 0)
	if b.Allow(-1) {
		t.Fatal("negative n must be false")
	}
	if !b.Allow(2) {
		t.Fatal("negative n must not consume")
	}
}

// Edge 4: refill accrues fractionally over time and clamps at capacity.
func TestHiddenRefillClamps(t *testing.T) {
	now := time.Unix(1000, 0)
	clock := func() time.Time { return now }
	b := NewWithClock(2, 0.5, clock)
	if !b.Allow(2) {
		t.Fatal("full bucket must allow capacity")
	}
	now = now.Add(2 * time.Second) // 1.0 token accrued
	if !b.Allow(1) {
		t.Fatal("refill must accrue over elapsed time")
	}
	if b.Allow(1) {
		t.Fatal("only one token had accrued")
	}
	now = now.Add(time.Hour) // far beyond capacity
	if b.Allow(3) {
		t.Fatal("refill must clamp at capacity")
	}
	if !b.Allow(2) {
		t.Fatal("clamped bucket holds exactly capacity")
	}
}

// Edge 5: concurrent Allow calls never oversell the bucket.
func TestHiddenConcurrentNoOversell(t *testing.T) {
	b := New(100, 0)
	var granted atomic.Int64
	var wg sync.WaitGroup
	for range 200 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if b.Allow(1) {
				granted.Add(1)
			}
		}()
	}
	wg.Wait()
	if g := granted.Load(); g > 100 {
		t.Fatalf("oversold: %d grants from a 100-token bucket", g)
	}
}
`

// hiddenTestNames maps the per-test verdict lines runHiddenTests parses.
var hiddenTestNames = []string{
	"TestHiddenZeroConsumesNothing",
	"TestHiddenOverCapacity",
	"TestHiddenNegative",
	"TestHiddenRefillClamps",
	"TestHiddenConcurrentNoOversell",
}

// runHiddenTests writes the hidden suite into scratch's limiter package and
// runs it, returning (passed, total). A tree without a limiter package or
// one that fails to build scores 0 — an implementation that does not
// compile is exactly as complete as one that does not exist.
func runHiddenTests(scratch string) (int, int) {
	total := len(hiddenTestNames)
	limDir := filepath.Join(scratch, "limiter")
	if _, err := os.Stat(limDir); err != nil {
		return 0, total
	}
	if err := os.WriteFile(filepath.Join(limDir, "hidden_acceptance_test.go"), []byte(hiddenAcceptance), 0o644); err != nil {
		return 0, total
	}
	cmd := exec.Command("go", "test", "./limiter/", "-race", "-count=1", "-v", "-run", "TestHidden")
	cmd.Dir = scratch
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod")
	done := make(chan struct{})
	var out []byte
	go func() { out, _ = cmd.CombinedOutput(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Minute):
		_ = cmd.Process.Kill()
		<-done
	}
	passed := 0
	for _, name := range hiddenTestNames {
		if strings.Contains(string(out), "--- PASS: "+name) {
			passed++
		}
	}
	return passed, total
}
