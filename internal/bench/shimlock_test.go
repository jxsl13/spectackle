package bench

// Shim atomicity + order-tolerant sequence completeness (B-01KYGZNT):
// parallel shim invocations yield a complete 1..N log; an out-of-order but
// complete log (the pre-lock warn-2 incident shape) scores as intact; a
// genuine hole or duplicate still disqualifies.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// mkFakeShimWorkspace builds a minimal shim wrapping /bin/echo so the lock
// path is exercised without a real server binary.
func mkFakeShimWorkspace(t *testing.T) (dir, shim string) {
	t.Helper()
	dir = t.TempDir()
	shim = filepath.Join(dir, "meter.sh")
	if err := os.WriteFile(shim, fmt.Appendf(nil, meterShim,
		"/bin/echo", filepath.Join(dir, "meter.log"), "cafe0123cafe0123", filepath.Join(dir, "transcript.log")), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir, shim
}

// The actual warn-2 mechanism: an argument containing REAL newlines must
// still produce exactly one meter line per call, holding the seq chain.
func TestShimMultilineArgvSingleEntry(t *testing.T) {
	dir, shim := mkFakeShimWorkspace(t)
	multiline := "{\"kind\":\"task\",\"body\":\"line one\nline two\nline three\"}"
	for i := 0; i < 3; i++ {
		if err := exec.Command(shim, "call", "-root", dir, "draft", multiline).Run(); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile(filepath.Join(dir, "meter.log"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 3 {
		t.Fatalf("3 calls must be exactly 3 meter lines, got %d:\n%s", len(lines), raw)
	}
	for i, l := range lines {
		if got := strings.Fields(l)[0]; got != fmt.Sprint(i+1) {
			t.Fatalf("line %d carries seq %s", i+1, got)
		}
	}
}

func TestShimConcurrentSequenceComplete(t *testing.T) {
	dir, shim := mkFakeShimWorkspace(t)
	const n = 24
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = exec.Command(shim, "call", "-root", dir, "state", "{}").Run()
		}()
	}
	wg.Wait()
	raw, err := os.ReadFile(filepath.Join(dir, "meter.log"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != n {
		t.Fatalf("want %d meter lines, got %d", n, len(lines))
	}
	seen := map[string]bool{}
	for _, l := range lines {
		seq := strings.Fields(l)[0]
		if seen[seq] {
			t.Fatalf("duplicate seq %s under concurrency", seq)
		}
		seen[seq] = true
	}
	for i := 1; i <= n; i++ {
		if !seen[fmt.Sprint(i)] {
			t.Fatalf("hole at seq %d under concurrency", i)
		}
	}
}

// scoreSeqsFixture writes a meter.log with the given seq order and scores
// it (no scenario sidecar: the basic path runs against /usr/bin/false-free
// echo shim workspace — only the sequence verdict is asserted).
func seqVerdict(t *testing.T, seqs []int) (disq bool, reason string) {
	t.Helper()
	dir := t.TempDir()
	var b strings.Builder
	for _, q := range seqs {
		fmt.Fprintf(&b, "%d cafe0123cafe0123 10 0 call -root x state {}\n", q)
	}
	if err := os.WriteFile(filepath.Join(dir, "meter.log"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	sc, err := ScoreAgentRun("/bin/echo", dir)
	if err != nil {
		t.Fatal(err)
	}
	return sc.Disqualified, sc.DisqualifyReason
}

func TestSequenceOrderTolerantButComplete(t *testing.T) {
	// the warn-2 incident shape: out of order, hole-free — intact
	if disq, why := seqVerdict(t, []int{1, 2, 5, 3, 4, 6}); disq {
		t.Fatalf("out-of-order complete log must not disqualify: %s", why)
	}
	// a genuine hole still disqualifies
	if disq, _ := seqVerdict(t, []int{1, 2, 4, 5}); !disq {
		t.Fatal("a hole must disqualify")
	}
	// a duplicate still disqualifies
	if disq, _ := seqVerdict(t, []int{1, 2, 2, 3}); !disq {
		t.Fatal("a duplicate must disqualify")
	}
}
