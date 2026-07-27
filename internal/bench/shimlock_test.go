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
	"regexp"
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

// Harness artifacts are git-invisible after prep (B-01KYH3SP), and an
// interleaved drive — shim calls dirtying the logs between every
// transition — completes the offline archive merge.
func TestPrepIgnoresHarnessArtifacts(t *testing.T) {
	dir := t.TempDir()
	for _, cmd := range [][]string{
		{"git", "-C", dir, "init", "-q", "-b", "main"},
		{"git", "-C", dir, "config", "user.name", "b"},
		{"git", "-C", dir, "config", "user.email", "b@l"},
		{"git", "-C", dir, "commit", "-q", "--allow-empty", "-m", "init"},
	} {
		if out, err := exec.Command(cmd[0], cmd[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("%v: %v %s", cmd, err, out)
		}
	}
	self := benchSelfBinary(t)
	if _, _, _, err := AgentPrep(self, dir, false, "outcome"); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("git", "-C", dir, "status", "--porcelain").Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"meter.log", "meter.sh", "brief.md", "journal.baseline", "trap.hash", "scenario", "transcript.log"} {
		if strings.Contains(string(out), f) {
			t.Fatalf("harness artifact %s is git-visible:\n%s", f, out)
		}
	}
}

// benchSelfBinary builds the spectackle binary once for prep-dependent
// tests; skipped under -short.
func benchSelfBinary(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("builds the binary")
	}
	bin := filepath.Join(t.TempDir(), "spectackle")
	if out, err := exec.Command("go", "build", "-o", bin, "github.com/jxsl13/spectackle").CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	return bin
}

// The judge's exact blocker shape (B-01KYH3SP): drive a task to archived
// THROUGH THE SHIM — every call dirties meter/transcript — and the offline
// merge must complete anyway now that the artifacts are ignored.
func TestInterleavedDriveArchivesThroughShim(t *testing.T) {
	self := benchSelfBinary(t)
	dir := t.TempDir()
	for _, cmd := range [][]string{
		{"git", "-C", dir, "init", "-q", "-b", "main"},
		{"git", "-C", dir, "config", "user.name", "b"},
		{"git", "-C", dir, "config", "user.email", "b@l"},
		{"git", "-C", dir, "commit", "-q", "--allow-empty", "-m", "init"},
	} {
		if out, err := exec.Command(cmd[0], cmd[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("%v: %v %s", cmd, err, out)
		}
	}
	_, shim, _, err := AgentPrep(self, dir, false, "outcome")
	if err != nil {
		t.Fatal(err)
	}
	call := func(tool, args string) string {
		t.Helper()
		out, _ := exec.Command(shim, "call", "-root", dir, tool, args).CombinedOutput()
		return string(out)
	}
	out := call("draft", `{"kind":"task","title":"interleaved drive fixture","targets":["limiter"],"body":"`+strings.Repeat("a real sentence for the ambiguity floor. ", 12)+`"}`)
	m := regexp.MustCompile(`\bi (T-[0-9A-HJKMNP-TV-Z]+)`).FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("draft failed: %s", out)
	}
	id := m[1]
	call("move", `{"id":"`+id+`","to":"active"}`)
	if err := os.WriteFile(filepath.Join(dir, "limiter", "limiter.go"),
		[]byte("package limiter\n\n// Placeholder implements nothing yet.\nfunc Placeholder() {}\n"), 0o644); err != nil {
		if mkErr := os.MkdirAll(filepath.Join(dir, "limiter"), 0o755); mkErr != nil {
			t.Fatal(mkErr)
		}
		if wErr := os.WriteFile(filepath.Join(dir, "limiter", "limiter.go"),
			[]byte("package limiter\n\n// Placeholder implements nothing yet.\nfunc Placeholder() {}\n"), 0o644); wErr != nil {
			t.Fatal(wErr)
		}
	}
	call("move", `{"id":"`+id+`","to":"done"}`)
	out = call("move", `{"id":"`+id+`","to":"archived","note":"interleaved drive closure"}`)
	// offline archive is commit-only (T-01KYHAH1GJ): the closure completes
	// on the current branch — archived state, no git error, no PR theater.
	if !strings.Contains(out, "archived") || strings.Contains(out, "! GIT E") {
		t.Fatalf("archive must close cleanly with dirty-but-ignored artifacts:\n%s", out)
	}
}

// Brief tool lists are honest maps (T-01KYH54H): every listed name exists
// on the real tool surface, and the outcome brief names the tools its
// require-configured variant legitimately needs — two judges paid a
// refusal-driven discovery loop for validate before this.
func TestBriefToolListsAreHonest(t *testing.T) {
	registered := map[string]bool{
		"state": true, "draft": true, "move": true, "get": true, "find": true,
		"grill": true, "validate": true, "check": true, "rule": true,
		"decide": true, "research": true, "work": true, "lease": true,
		"swarm": true, "compact": true, "commands": true, "reindex": true,
	}
	briefs := map[string]string{
		"basic": agentBrief, "tricky": trickyBrief,
		"worktree": worktreeBrief, "outcome": outcomeBrief,
		"outcome-ask": outcomeAskBrief,
	}
	re := regexp.MustCompile(`Available tool names: ([a-z, ]+)\.`)
	for name, b := range briefs {
		m := re.FindStringSubmatch(b)
		if m == nil {
			t.Fatalf("%s brief has no tool list", name)
		}
		for _, tool := range strings.Split(m[1], ", ") {
			if !registered[tool] {
				t.Errorf("%s brief lists nonexistent tool %q", name, tool)
			}
		}
	}
	for _, want := range []string{"validate", "work"} {
		if m := re.FindStringSubmatch(outcomeBrief); !strings.Contains(m[1], want) {
			t.Errorf("outcome brief missing %s", want)
		}
	}
}

// AskCount meters exit-0 decide-ask calls (T-01KYH6H9).
func TestAskCountFromMeter(t *testing.T) {
	dir := t.TempDir()
	log := `1 cafe 10 0 call -root x decide {"op":"ask","question":"q1"}
2 cafe 10 0 call -root x decide {"op":"answer","id":"A-1","choose":"x"}
3 cafe 10 1 call -root x decide {"op":"ask","question":"failed"}
4 cafe 10 0 call -root x state {}
5 cafe 10 0 call -root x decide {"op":"ask","question":"q2"}
`
	if err := os.WriteFile(filepath.Join(dir, "meter.log"), []byte(log), 0o644); err != nil {
		t.Fatal(err)
	}
	sc, err := ScoreAgentRun("/bin/echo", dir)
	if err != nil {
		t.Fatal(err)
	}
	if sc.AskCount != 2 {
		t.Fatalf("want 2 asks (exit-0 op=ask only), got %d", sc.AskCount)
	}
}
