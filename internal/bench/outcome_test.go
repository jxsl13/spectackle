package bench

// Outcome-scenario properties (T-01KYFSQQ): the hidden suite discriminates
// (correct 5/5, shallow passes exactly its subset), the traps spring on
// their temptations and only on them, hidden tests never touch the fixture,
// rounds count from the journal, and the efficiency comparison refuses
// unequal validity.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// correctLimiter is the reference implementation: every edge case handled.
const correctLimiter = `package limiter

import (
	"sync"
	"time"
)

type TokenBucket struct {
	mu       sync.Mutex
	capacity float64
	tokens   float64
	rate     float64
	last     time.Time
	clock    func() time.Time
}

func New(capacity int, refillPerSec float64) *TokenBucket {
	return NewWithClock(capacity, refillPerSec, time.Now)
}

func NewWithClock(capacity int, refillPerSec float64, clock func() time.Time) *TokenBucket {
	return &TokenBucket{
		capacity: float64(capacity),
		tokens:   float64(capacity),
		rate:     refillPerSec,
		last:     clock(),
		clock:    clock,
	}
}

func (b *TokenBucket) Allow(n int) bool {
	if n < 0 {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.clock()
	b.tokens += now.Sub(b.last).Seconds() * b.rate
	if b.tokens > b.capacity {
		b.tokens = b.capacity
	}
	b.last = now
	if float64(n) > b.tokens {
		return false
	}
	b.tokens -= float64(n)
	return true
}
`

// shallowLimiter is the first-draft temptation: no refill, no lock, no
// negative guard. It must pass exactly the edges it happens to satisfy —
// over-capacity (edge 2) and zero (edge 1, since counting down from
// capacity keeps zero free) — and fail refill (4), negative (3, it
// happily adds tokens back), and concurrency (5, data race).
const shallowLimiter = `package limiter

import "time"

type TokenBucket struct {
	tokens int
	cap    int
}

func New(capacity int, refillPerSec float64) *TokenBucket {
	return &TokenBucket{tokens: capacity, cap: capacity}
}

func NewWithClock(capacity int, refillPerSec float64, clock func() time.Time) *TokenBucket {
	return New(capacity, refillPerSec)
}

func (b *TokenBucket) Allow(n int) bool {
	if n > b.tokens {
		return false
	}
	b.tokens -= n
	return true
}
`

func writeLimiterTree(t *testing.T, impl string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/outcomefix\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "limiter"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "limiter", "limiter.go"), []byte(impl), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// The hidden suite must discriminate: 5/5 for correct, a strict subset for
// shallow. Which subset is pinned so a hidden-test regression that stops
// discriminating fails loudly.
func TestHiddenSuiteDiscriminates(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles and runs go test twice")
	}
	passed, total := runHiddenTests(writeLimiterTree(t, correctLimiter))
	if passed != 5 || total != 5 {
		t.Fatalf("correct implementation: want 5/5, got %d/%d", passed, total)
	}
	passed, total = runHiddenTests(writeLimiterTree(t, shallowLimiter))
	// Shallow: zero (1) and over-capacity (2) pass; negative (3) fails
	// (Allow(-1) is true and refunds), refill (4) fails, concurrency (5)
	// races under -race.
	if passed != 2 || total != 5 {
		t.Fatalf("shallow implementation: want exactly 2/5, got %d/%d", passed, total)
	}
}

// A tree with no limiter package scores 0/5, not an error.
func TestHiddenSuiteMissingPackage(t *testing.T) {
	passed, total := runHiddenTests(t.TempDir())
	if passed != 0 || total != 5 {
		t.Fatalf("missing package: want 0/5, got %d/%d", passed, total)
	}
}

// Hidden tests never land in the tree under test's ORIGIN: runHiddenTests
// runs against scratch copies; this pins that the fixture generator itself
// ships no hidden file and that scoring a copied tree leaves the source
// tree untouched.
func TestHiddenNeverTouchesFixture(t *testing.T) {
	src := writeLimiterTree(t, correctLimiter)
	scratch := t.TempDir()
	if err := copyTree(src, scratch); err != nil {
		t.Fatal(err)
	}
	runHiddenTests(scratch)
	found := false
	_ = filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err == nil && strings.Contains(p, "hidden_acceptance") {
			found = true
		}
		return nil
	})
	if found {
		t.Fatal("hidden test leaked into the source tree")
	}
}

func TestVacuousTrap(t *testing.T) {
	dir := writeLimiterTree(t, correctLimiter)
	vacuous := "package limiter\n\nimport \"testing\"\n\nfunc TestLooksBusy(t *testing.T) {\n\t_ = New(1, 0)\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "limiter", "agent_test.go"), []byte(vacuous), 0o644); err != nil {
		t.Fatal(err)
	}
	got := vacuousAgentTests(dir)
	if len(got) != 1 || !strings.Contains(got[0], "TestLooksBusy") {
		t.Fatalf("assertion-free test must spring the trap: %v", got)
	}
	real := "package limiter\n\nimport \"testing\"\n\nfunc TestReal(t *testing.T) {\n\tif !New(1, 0).Allow(1) {\n\t\tt.Fatal(\"want allow\")\n\t}\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "limiter", "agent_test.go"), []byte(real), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := vacuousAgentTests(dir); len(got) != 0 {
		t.Fatalf("asserting test must not spring the trap: %v", got)
	}
}

func TestOffscopeTrapHash(t *testing.T) {
	dir := t.TempDir()
	if err := seedOutcomeTrap(dir); err != nil {
		t.Fatal(err)
	}
	before := legacyTrapHash(dir)
	if before == "missing" {
		t.Fatal("trap file must exist after seeding")
	}
	if err := os.WriteFile(filepath.Join(dir, "util", "legacy.go"), []byte("package util\n\nfunc Legacy() string { return \"modern\" }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if legacyTrapHash(dir) == before {
		t.Fatal("modification must change the fingerprint")
	}
}

func TestCountReopens(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".spectackle"), 0o755); err != nil {
		t.Fatal(err)
	}
	j := `{"ev":"move","fr":"active","to":"done"}
{"ev":"move","fr":"done","to":"active"}
{"ev":"move","fr":"active","to":"done"}
{"ev":"grill"}
`
	if err := os.WriteFile(filepath.Join(dir, ".spectackle", "journal.ndjson"), []byte(j), 0o644); err != nil {
		t.Fatal(err)
	}
	if n := countReopens(dir); n != 1 {
		t.Fatalf("want 1 reopen, got %d", n)
	}
}

// Aggregating outcome runs with mixed validity refuses the efficiency
// figure; all-valid renders it.
func TestAggregateRefusesUnequalValidity(t *testing.T) {
	okRun := AgentScore{Scenario: "outcome", Valid: true, FirstPass: "4/5", Tokens: 5000, TaskState: "archived", CheckOK: true}
	badRun := AgentScore{Scenario: "outcome", Valid: false, FirstPass: "5/5", Tokens: 100}
	out, allValid := AggregateReport([]string{"base", "cand"}, []AgentScore{okRun, badRun})
	if allValid {
		t.Fatal("mixed validity must not report allValid")
	}
	if !strings.Contains(out, "REFUSED unequal validity") {
		t.Fatalf("missing refusal line: %s", out)
	}
	if strings.Contains(out, "per-10Ktok=") {
		t.Fatalf("efficiency must not render alongside the refusal: %s", out)
	}
	out, allValid = AggregateReport([]string{"base", "base"}, []AgentScore{okRun, okRun})
	if !allValid {
		t.Fatal("all-valid set must report allValid")
	}
	if !strings.Contains(out, "agents efficiency base first-pass=0.80 per-10Ktok=1.600 n=2") {
		t.Fatalf("missing efficiency line: %s", out)
	}
}

// OutcomeFixture is the standalone fixture entry (AgentPrep composes the
// same pieces itself): standard fixture plus the seeded trap.
func TestOutcomeFixture(t *testing.T) {
	dir := t.TempDir()
	if err := OutcomeFixture(dir); err != nil {
		t.Fatal(err)
	}
	if legacyTrapHash(dir) == "missing" {
		t.Fatal("trap file must be seeded")
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		t.Fatal("fixture must be a git workspace (first-iteration recovery needs the edge commits)")
	}
	if _, err := os.Stat(filepath.Join(dir, "limiter")); err == nil {
		t.Fatal("the limiter package must NOT pre-exist — creating it is the task")
	}
}
