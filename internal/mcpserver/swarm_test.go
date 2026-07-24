package mcpserver

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jxsl13/spectackle/internal/journal"
	"github.com/jxsl13/spectackle/internal/workspace"
	"github.com/jxsl13/spectackle/internal/wt"
)

// twoAgents spins up two Server instances ("alice", "bob") on the SAME
// workspace — the N-stdio-processes topology exercised in one binary via two
// distinct SQLite connections.
func twoAgents(t *testing.T, root string) (*mcp.ClientSession, *mcp.ClientSession) {
	t.Helper()
	t.Setenv("SPECTACKLE_AGENT", "alice")
	alice := connectRoot(t, root)
	t.Setenv("SPECTACKLE_AGENT", "bob")
	bob := connectRoot(t, root)
	return alice, bob
}

// gitRoot creates a workspace that is also a git repo with an initial commit.
func gitRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".spectackle"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".spectackle", "config.yaml"),
		[]byte("schema: v0\nverify: [\"test -f ok.txt\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := wt.InitTestRepo(root); err != nil {
		t.Skipf("git unavailable: %v", err)
	}
	return root
}

func TestTwoServersMintUniqueIDs(t *testing.T) {
	alice, bob := twoAgents(t, t.TempDir())
	const n = 8
	results := make(chan string, 2*n)
	for i := 0; i < n; i++ {
		go func(i int) {
			results <- strings.Fields(callText(t, alice, "draft", map[string]any{
				"kind": "task", "title": fmt.Sprintf("alice %d", i)}))[1]
		}(i)
		go func(i int) {
			results <- strings.Fields(callText(t, bob, "draft", map[string]any{
				"kind": "task", "title": fmt.Sprintf("bob %d", i)}))[1]
		}(i)
	}
	seen := map[string]bool{}
	for i := 0; i < 2*n; i++ {
		id := <-results
		if !strings.HasPrefix(id, "T-") { // sw piggyback line may shift fields
			t.Fatalf("unexpected ID token %q", id)
		}
		if seen[id] {
			t.Fatalf("duplicate ID %s minted across two servers", id)
		}
		seen[id] = true
	}
	out := callText(t, alice, "check", map[string]any{})
	if strings.Contains(out, "E101") {
		t.Fatalf("duplicate item IDs on disk:\n%s", out)
	}
}

func TestLeaseContention(t *testing.T) {
	alice, bob := twoAgents(t, t.TempDir())

	out := callText(t, alice, "lease", map[string]any{"op": "claim", "paths": []string{"gpu/kernels"}})
	if !strings.Contains(out, "ok claimed") {
		t.Fatalf("claim: %q", out)
	}
	// overlapping foreign claim blocked, sibling scope fine
	out = callText(t, bob, "lease", map[string]any{"op": "claim", "paths": []string{"gpu/kernels/saxpy.cu"}})
	if !strings.Contains(out, "! LEASE E") || !strings.Contains(out, "alice") {
		t.Fatalf("overlap not blocked: %q", out)
	}
	out = callText(t, bob, "lease", map[string]any{"op": "claim", "paths": []string{"math"}})
	if !strings.Contains(out, "ok claimed") {
		t.Fatalf("disjoint claim: %q", out)
	}
	// enforcement on draft targets
	out = callText(t, bob, "draft", map[string]any{
		"kind": "task", "title": "sneak", "targets": []string{"gpu/kernels/saxpy.cu"}})
	if !strings.Contains(out, "! LEASE E") {
		t.Fatalf("draft ignored lease: %q", out)
	}
	// release frees the scope
	callText(t, alice, "lease", map[string]any{"op": "release", "paths": []string{"gpu/kernels"}})
	out = callText(t, bob, "lease", map[string]any{"op": "claim", "paths": []string{"gpu/kernels"}})
	if !strings.Contains(out, "ok claimed") {
		t.Fatalf("claim after release: %q", out)
	}
	// ls shows the swarm state
	out = callText(t, alice, "lease", map[string]any{"op": "ls"})
	if !strings.Contains(out, "l gpu/kernels bob") || !strings.Contains(out, "l math bob") {
		t.Fatalf("ls: %q", out)
	}
}

func TestSwarmRealtimeRejection(t *testing.T) {
	alice, bob := twoAgents(t, t.TempDir())

	callText(t, alice, "draft", map[string]any{"kind": "proposal", "title": "pin kernels in VRAM"})
	// find the ID from alice's own view (bob's piggyback may shift tokens)
	callText(t, alice, "move", map[string]any{"id": "P-0001", "to": "rejected",
		"note": "VRAM pinning starves sibling tenants"})

	// bob's very next tool result carries the learning as an sw record
	out := callText(t, bob, "swarm", map[string]any{})
	if !strings.Contains(out, "reject") || !strings.Contains(out, "VRAM pinning") {
		t.Fatalf("swarm missing rejection: %q", out)
	}
	// and find scope=rejection unions the live coord event (pre-merge)
	out = callText(t, bob, "find", map[string]any{"q": "starves tenants", "scope": "rejection"})
	if !strings.Contains(out, "P-0001") {
		t.Fatalf("find union missing sibling rejection: %q", out)
	}
}

func TestWorkLifecycleE2E(t *testing.T) {
	root := gitRoot(t)
	t.Setenv("SPECTACKLE_AGENT", "alice")
	alice := connectRoot(t, root)

	callText(t, alice, "draft", map[string]any{
		"kind": "proposal", "title": "optimize memory pool", "targets": []string{"main.go"}})
	callText(t, alice, "move", map[string]any{"id": "P-0001", "to": "submitted"})
	callText(t, alice, "move", map[string]any{"id": "P-0001", "to": "approved"})

	out := callText(t, alice, "work", map[string]any{"op": "start", "item": "P-0001"})
	if !strings.Contains(out, "wt P-0001 open ") {
		t.Fatalf("work start: %q", out)
	}
	wtRoot := ""
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "wt P-0001 open ") {
			wtRoot = strings.TrimPrefix(l, "wt P-0001 open ")
		}
	}
	if _, err := os.Stat(filepath.Join(wtRoot, "main.go")); err != nil {
		t.Fatalf("worktree checkout missing: %v", err)
	}
	// the live (uncommitted) item state traveled into the worktree
	out = callText(t, alice, "get", map[string]any{"id": "P-0001"})
	if !strings.Contains(out, "P-0001 proposal active") {
		t.Fatalf("item not active in worktree: %q", out)
	}

	// gate fails until the verify condition is met
	out = callText(t, alice, "work", map[string]any{"op": "submit"})
	if !strings.Contains(out, "! GATE E") {
		t.Fatalf("gate should fail: %q", out)
	}
	// "implement": edit code in the worktree, satisfy the gate
	if err := os.WriteFile(filepath.Join(wtRoot, "pool.go"), []byte("package main // pooled\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wtRoot, "ok.txt"), []byte("ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	callText(t, alice, "move", map[string]any{"id": "P-0001", "to": "done"})
	out = callText(t, alice, "work", map[string]any{"op": "submit"})
	if !strings.Contains(out, "merged to main") {
		t.Fatalf("submit: %q", out)
	}

	// code landed on main, worktree gone, item state replayed, journal once
	if _, err := os.Stat(filepath.Join(root, "pool.go")); err != nil {
		t.Fatalf("code did not reach main: %v", err)
	}
	if _, err := os.Stat(wtRoot); !os.IsNotExist(err) {
		t.Fatalf("worktree not torn down")
	}
	out = callText(t, alice, "get", map[string]any{"id": "P-0001"})
	if !strings.Contains(out, "P-0001 proposal done") {
		t.Fatalf("item state not replayed: %q", out)
	}
	out = callText(t, alice, "work", map[string]any{"op": "status"})
	if !strings.Contains(out, "ok no open worktrees") {
		t.Fatalf("status: %q", out)
	}
	// a second submit of the same item is impossible (worktree gone)
	out = callText(t, alice, "work", map[string]any{"op": "submit", "item": "P-0001"})
	if !strings.Contains(out, "! ARG E") {
		t.Fatalf("stale submit: %q", out)
	}
}

func TestWorkAbortAndConcurrentSubmit(t *testing.T) {
	root := gitRoot(t)
	// no verify gate for this one
	os.WriteFile(filepath.Join(root, ".spectackle", "config.yaml"), []byte("schema: v0\n"), 0o644)
	alice, bob := twoAgents(t, root)

	for i, sess := range []*mcp.ClientSession{alice, bob} {
		callText(t, sess, "draft", map[string]any{
			"kind": "proposal", "title": fmt.Sprintf("work %d", i), "targets": []string{fmt.Sprintf("f%d.go", i)}})
	}
	for _, id := range []string{"P-0001", "P-0002"} {
		callText(t, alice, "move", map[string]any{"id": id, "to": "submitted"})
		callText(t, alice, "move", map[string]any{"id": id, "to": "approved"})
	}

	outA := callText(t, alice, "work", map[string]any{"op": "start", "item": "P-0001"})
	outB := callText(t, bob, "work", map[string]any{"op": "start", "item": "P-0002"})
	rootA, rootB := wtRootOf(t, outA, "P-0001"), wtRootOf(t, outB, "P-0002")

	// bob cannot start alice's item (lease)
	out := callText(t, bob, "work", map[string]any{"op": "abort", "item": "P-0001"})
	if !strings.Contains(out, "! WT E") {
		t.Fatalf("bob aborted alice's live worktree: %q", out)
	}

	// disjoint edits, concurrent submits — the integrate lock serializes
	os.WriteFile(filepath.Join(rootA, "fa.go"), []byte("package main\n"), 0o644)
	os.WriteFile(filepath.Join(rootB, "fb.go"), []byte("package main\n"), 0o644)
	done := make(chan string, 2)
	go func() { done <- callText(t, alice, "work", map[string]any{"op": "submit"}) }()
	go func() { done <- callText(t, bob, "work", map[string]any{"op": "submit"}) }()
	merged := 0
	var retry []string
	for i := 0; i < 2; i++ {
		out := <-done
		if strings.Contains(out, "merged to main") {
			merged++
		} else if strings.Contains(out, "! LOCK W") {
			retry = append(retry, out)
		} else {
			t.Fatalf("unexpected submit result: %q", out)
		}
	}
	// a loser of the lock race retries and succeeds
	for range retry {
		out := callText(t, bob, "work", map[string]any{"op": "submit"})
		if !strings.Contains(out, "merged to main") {
			out = callText(t, alice, "work", map[string]any{"op": "submit"})
			if !strings.Contains(out, "merged to main") {
				t.Fatalf("retry submit failed: %q", out)
			}
		}
		merged++
	}
	if merged != 2 {
		t.Fatalf("merged %d of 2", merged)
	}
	for _, f := range []string{"fa.go", "fb.go"} {
		if _, err := os.Stat(filepath.Join(root, f)); err != nil {
			t.Fatalf("%s did not reach main: %v", f, err)
		}
	}
	// journal deltas exactly once: count create events for each item in the
	// main journal file itself
	raw, err := os.ReadFile(filepath.Join(root, ".spectackle", "journal.ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"P-0001", "P-0002"} {
		if c := strings.Count(string(raw), `"ev":"create","id":"`+id+`"`); c != 1 {
			t.Fatalf("item %s has %d create events on main (want exactly 1):\n%s", id, c, raw)
		}
	}
}

func wtRootOf(t *testing.T, out, item string) string {
	t.Helper()
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "wt "+item+" open ") {
			return strings.TrimPrefix(l, "wt "+item+" open ")
		}
	}
	t.Fatalf("no wt line in %q", out)
	return ""
}

func TestCompactBlockedInWorktree(t *testing.T) {
	root := gitRoot(t)
	os.WriteFile(filepath.Join(root, ".spectackle", "config.yaml"), []byte("schema: v0\n"), 0o644)
	t.Setenv("SPECTACKLE_AGENT", "alice")
	alice := connectRoot(t, root)
	callText(t, alice, "draft", map[string]any{"kind": "proposal", "title": "x"})
	callText(t, alice, "move", map[string]any{"id": "P-0001", "to": "submitted"})
	callText(t, alice, "move", map[string]any{"id": "P-0001", "to": "approved"})
	callText(t, alice, "work", map[string]any{"op": "start", "item": "P-0001"})
	out := callText(t, alice, "compact", map[string]any{})
	if !strings.Contains(out, "! WT E compact") {
		t.Fatalf("compact not blocked in worktree: %q", out)
	}
	out = callText(t, alice, "work", map[string]any{"op": "abort"})
	if !strings.Contains(out, "aborted") {
		t.Fatalf("abort: %q", out)
	}
	// item back to approved on main
	out = callText(t, alice, "get", map[string]any{"id": "P-0001"})
	if !strings.Contains(out, "proposal approved") {
		t.Fatalf("abort did not restore approved: %q", out)
	}
}

// TestCompactHintFiresOncePerCrossing proves postCall's proactive T-0093
// nudge: once the root journal crosses Compact.JournalMax (300 by default),
// ANY tool result carries a "c . journal <n> events since last compact"
// line — but only once per crossing, never on every call.
//
// The >300 events are seeded directly into the root journal.ndjson (not via
// ordinary tool calls) so the server session connected below starts with a
// cold compact-hint cache: its very first tool call always re-counts the
// journal regardless of the 30s debounce window (s.lastCompactCheck is the
// server's zero value), giving an accurate count on that first call without
// the test needing to wait out the debounce window in real time.
func TestCompactHintFiresOncePerCrossing(t *testing.T) {
	root := t.TempDir()
	ws := workspace.Root{Dir: root}
	if err := ws.EnsureScaffold(""); err != nil {
		t.Fatal(err)
	}
	const seeded = 305 // > the default compact.journal_max (300)
	for i := 0; i < seeded; i++ {
		if err := journal.Append(ws, "", journal.Event{Ev: journal.EvCreate, ID: fmt.Sprintf("T-%04d", i)}); err != nil {
			t.Fatal(err)
		}
	}

	t.Setenv("SPECTACKLE_AGENT", "alice")
	alice := connectRoot(t, root)

	want := fmt.Sprintf("c . journal %d events since last compact", seeded)
	out := callText(t, alice, "swarm", map[string]any{})
	if !strings.Contains(out, want) {
		t.Fatalf("compact hint missing on first (crossing) call: %q, want substring %q", out, want)
	}

	// the immediately following identical call must NOT repeat the hint —
	// same crossing, nothing has changed since it was surfaced.
	out2 := callText(t, alice, "get", map[string]any{"id": "does-not-matter"})
	if strings.Contains(out2, "c . journal") {
		t.Fatalf("compact hint repeated on the very next call: %q", out2)
	}
}

// ---- T-0115: stale-binary hint (postCall's second proactive nudge,
// mirroring compactHint's shape exactly — see swarm.go: staleHint,
// binaryStale) ----

// fakeBinary creates a stand-in "binary" file with an explicit ModTime, so
// the tests below can compare it against source mtimes exactly, instead of
// racing whatever moment `go test` happened to compile the real test
// binary.
func fakeBinary(t *testing.T, mtime time.Time) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "spectackle-bin")
	if err := os.WriteFile(p, []byte("bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	return p
}

// withFakeExecutable swaps osExecutable (swarm.go's os.Executable
// indirection, added so this is testable without touching the real running
// test binary) for the duration of the calling test, restoring the original
// on cleanup.
func withFakeExecutable(t *testing.T, path string, err error) {
	t.Helper()
	orig := osExecutable
	osExecutable = func() (string, error) { return path, err }
	t.Cleanup(func() { osExecutable = orig })
}

// selfModuleRoot creates a temp directory whose go.mod declares spectackle's
// own module (modulePath, server.go) — the one precondition binaryStale's
// selfModule gate requires before it will look at any file's mtime at all
// (see swarm.go: selfModule). Every test below that needs the walk to
// actually run uses this instead of a bare t.TempDir(); none of the
// package's OTHER fixtures (plain temp dirs, gitRoot's throwaway repos) ever
// declare this module, which is exactly why the gate leaves them silent —
// see TestStaleHintSilentOutsideOwnModule.
func selfModuleRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module "+modulePath+"\n\ngo 1.25\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// freshGoFile writes a .go file whose ModTime is unambiguously "now" —
// newer than any fakeBinary stamped with a past timestamp earlier in the
// same test.
func freshGoFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("package p\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := os.Chtimes(path, now, now); err != nil {
		t.Fatal(err)
	}
}

// TestStaleHintSilentWhenBinaryNewer covers case 1: a binary built after
// every source file under the root is not stale — no hint, ever.
func TestStaleHintSilentWhenBinaryNewer(t *testing.T) {
	root := selfModuleRoot(t)
	past := time.Now().Add(-time.Hour)
	if err := os.WriteFile(filepath.Join(root, "old.go"), []byte("package root\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(root, "old.go"), past, past); err != nil {
		t.Fatal(err)
	}
	withFakeExecutable(t, fakeBinary(t, time.Now()), nil) // binary freshly "built", after old.go

	s := &Server{ws: workspace.Root{Dir: root}}
	if hint := s.staleHint(); hint != "" {
		t.Fatalf("hint fired though the binary postdates every source file: %q", hint)
	}
}

// TestStaleHintFiresOnCrossing covers case 2, end-to-end through the real
// MCP path (postCall wired via the swarm tool, like
// TestCompactHintFiresOncePerCrossing above): a source file newer than the
// binary produces exactly one hint naming the rebuild command.
func TestStaleHintFiresOnCrossing(t *testing.T) {
	root := selfModuleRoot(t)
	withFakeExecutable(t, fakeBinary(t, time.Now().Add(-time.Hour)), nil)
	freshGoFile(t, filepath.Join(root, "pkg", "fresh.go"))

	t.Setenv("SPECTACKLE_AGENT", "alice")
	alice := connectRoot(t, root)

	out := callText(t, alice, "swarm", map[string]any{})
	if !strings.Contains(out, "sb stale") || !strings.Contains(out, "make dev") {
		t.Fatalf("stale hint missing or doesn't name the rebuild command: %q", out)
	}
}

// TestStaleHintOncePerCrossing covers case 3: the very next call, still
// inside the 30s debounce window and still in the same "stale" streak, must
// not repeat the hint.
func TestStaleHintOncePerCrossing(t *testing.T) {
	root := selfModuleRoot(t)
	withFakeExecutable(t, fakeBinary(t, time.Now().Add(-time.Hour)), nil)
	freshGoFile(t, filepath.Join(root, "fresh.go"))

	t.Setenv("SPECTACKLE_AGENT", "alice")
	alice := connectRoot(t, root)

	out := callText(t, alice, "swarm", map[string]any{})
	if !strings.Contains(out, "sb stale") {
		t.Fatalf("expected the hint on the crossing call: %q", out)
	}
	out2 := callText(t, alice, "get", map[string]any{"id": "does-not-matter"})
	if strings.Contains(out2, "sb stale") {
		t.Fatalf("stale hint repeated within the debounce window: %q", out2)
	}
}

// TestStaleHintHonorsSkipDir covers case 4 — the one a naive walk fails: a
// source file newer than the binary but buried under a directory every
// workspace walk prunes (node_modules is one of workspace's built-in
// defaultSkipNames, reached only through ws.SkipDir) must NOT trigger the
// hint. A walk that didn't route through ws.SkipDir would descend into it
// and report staleness that isn't real.
func TestStaleHintHonorsSkipDir(t *testing.T) {
	root := selfModuleRoot(t)
	withFakeExecutable(t, fakeBinary(t, time.Now().Add(-time.Hour)), nil)
	freshGoFile(t, filepath.Join(root, "node_modules", "pkg", "fresh.go"))

	s := &Server{ws: workspace.Root{Dir: root}}
	if hint := s.staleHint(); hint != "" {
		t.Fatalf("hint fired for a file inside a pruned directory (naive walk): %q", hint)
	}
}

// TestStaleHintDegradesToSilence covers case 5: both failure modes —
// os.Executable itself erroring, and a resolvable path that can't be
// stat'd — degrade to silence, never to an error surfaced on a tool call.
func TestStaleHintDegradesToSilence(t *testing.T) {
	root := selfModuleRoot(t)
	freshGoFile(t, filepath.Join(root, "fresh.go"))

	t.Run("os.Executable fails", func(t *testing.T) {
		withFakeExecutable(t, "", errors.New("boom"))
		s := &Server{ws: workspace.Root{Dir: root}}
		if hint := s.staleHint(); hint != "" {
			t.Fatalf("hint fired despite os.Executable failing: %q", hint)
		}
	})

	t.Run("binary path unstattable", func(t *testing.T) {
		withFakeExecutable(t, filepath.Join(t.TempDir(), "does-not-exist"), nil)
		s := &Server{ws: workspace.Root{Dir: root}}
		if hint := s.staleHint(); hint != "" {
			t.Fatalf("hint fired despite an unstattable binary path: %q", hint)
		}
	})
}

// TestStaleHintSilentOutsideOwnModule proves the selfModule gate (swarm.go)
// that makes the whole feature safe to wire in unconditionally: a workspace
// that is NOT a checkout of spectackle's own module never triggers the
// hint, no matter how stale-looking its file mtimes are relative to this
// process's own executable — comparing them would be meaningless noise for
// any spectackle deployment serving a third-party codebase. This is also
// exactly why the rest of this package's fixtures (plain temp dirs, none of
// which declare spectackle's own module in a go.mod) never see the hint.
func TestStaleHintSilentOutsideOwnModule(t *testing.T) {
	root := t.TempDir() // no go.mod at all: the ordinary case for these fixtures
	withFakeExecutable(t, fakeBinary(t, time.Now().Add(-time.Hour)), nil)
	freshGoFile(t, filepath.Join(root, "fresh.go"))

	s := &Server{ws: workspace.Root{Dir: root}}
	if hint := s.staleHint(); hint != "" {
		t.Fatalf("hint fired for a workspace outside spectackle's own module: %q", hint)
	}
}
