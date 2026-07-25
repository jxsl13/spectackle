package mcpserver

import (
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

	// read the ID off alice's own draft record (bob's piggyback may shift tokens)
	prop := draftID(t, alice, map[string]any{"kind": "proposal", "title": "pin kernels in VRAM"})
	callText(t, alice, "move", map[string]any{"id": prop, "to": "rejected",
		"note": "VRAM pinning starves sibling tenants"})

	// bob's very next tool result carries the learning as an sw record
	out := callText(t, bob, "swarm", map[string]any{})
	if !strings.Contains(out, "reject") || !strings.Contains(out, "VRAM pinning") {
		t.Fatalf("swarm missing rejection: %q", out)
	}
	// and find scope=rejection unions the live coord event (pre-merge)
	out = callText(t, bob, "find", map[string]any{"q": "starves tenants", "scope": "rejection"})
	if !strings.Contains(out, prop) {
		t.Fatalf("find union missing sibling rejection: %q", out)
	}
}

func TestWorkLifecycleE2E(t *testing.T) {
	root := gitRoot(t)
	t.Setenv("SPECTACKLE_AGENT", "alice")
	alice := connectRoot(t, root)

	prop := draftID(t, alice, map[string]any{
		"kind": "proposal", "title": "optimize memory pool", "targets": []string{"main.go"}})
	callText(t, alice, "move", map[string]any{"id": prop, "to": "submitted"})
	callText(t, alice, "move", map[string]any{"id": prop, "to": "approved"})

	out := callText(t, alice, "work", map[string]any{"op": "start", "item": prop})
	if !strings.Contains(out, "wt "+prop+" open ") {
		t.Fatalf("work start: %q", out)
	}
	wtRoot := ""
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "wt "+prop+" open ") {
			wtRoot = strings.TrimPrefix(l, "wt "+prop+" open ")
		}
	}
	if _, err := os.Stat(filepath.Join(wtRoot, "main.go")); err != nil {
		t.Fatalf("worktree checkout missing: %v", err)
	}
	// the live (uncommitted) item state traveled into the worktree
	out = callText(t, alice, "get", map[string]any{"id": prop})
	if !strings.Contains(out, prop+" proposal active") {
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
	callText(t, alice, "move", map[string]any{"id": prop, "to": "done"})
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
	out = callText(t, alice, "get", map[string]any{"id": prop})
	if !strings.Contains(out, prop+" proposal done") {
		t.Fatalf("item state not replayed: %q", out)
	}
	out = callText(t, alice, "work", map[string]any{"op": "status"})
	if !strings.Contains(out, "ok no open worktrees") {
		t.Fatalf("status: %q", out)
	}
	// a second submit of the same item is impossible (worktree gone)
	out = callText(t, alice, "work", map[string]any{"op": "submit", "item": prop})
	if !strings.Contains(out, "! ARG E") {
		t.Fatalf("stale submit: %q", out)
	}
}

func TestWorkAbortAndConcurrentSubmit(t *testing.T) {
	root := gitRoot(t)
	// no verify gate for this one
	os.WriteFile(filepath.Join(root, ".spectackle", "config.yaml"), []byte("schema: v0\n"), 0o644)
	alice, bob := twoAgents(t, root)

	// stored IDs, resolved at draft time: two proposals minted milliseconds
	// apart share their displayed prefix once both exist (see draftFullID).
	var ids []string
	for i, sess := range []*mcp.ClientSession{alice, bob} {
		ids = append(ids, storedID(t, root, draftID(t, sess, map[string]any{
			"kind": "proposal", "title": fmt.Sprintf("work %d", i), "targets": []string{fmt.Sprintf("f%d.go", i)}})))
	}
	for _, id := range ids {
		callText(t, alice, "move", map[string]any{"id": id, "to": "submitted"})
		callText(t, alice, "move", map[string]any{"id": id, "to": "approved"})
	}

	outA := callText(t, alice, "work", map[string]any{"op": "start", "item": ids[0]})
	outB := callText(t, bob, "work", map[string]any{"op": "start", "item": ids[1]})
	rootA, rootB := wtRootOf(t, outA, ids[0]), wtRootOf(t, outB, ids[1])

	// bob cannot start alice's item (lease)
	out := callText(t, bob, "work", map[string]any{"op": "abort", "item": ids[0]})
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
	for _, id := range ids {
		if c := strings.Count(string(raw), `"ev":"create","id":"`+id+`"`); c != 1 {
			t.Fatalf("item %s has %d create events on main (want exactly 1):\n%s", id, c, raw)
		}
	}
}

// wtRootOf pulls the worktree root out of a `wt <id> open <root>` line.
//
// item may be either the stored ID or the displayed one: the line carries the
// display form, which is a prefix of the stored ID, so the ID field is matched
// by prefix rather than compared as a literal.
func wtRootOf(t *testing.T, out, item string) string {
	t.Helper()
	for _, l := range strings.Split(out, "\n") {
		f := strings.Fields(l)
		if len(f) == 4 && f[0] == "wt" && f[2] == "open" && strings.HasPrefix(item, f[1]) {
			return f[3]
		}
	}
	t.Fatalf("no wt line for %s in %q", item, out)
	return ""
}

func TestCompactBlockedInWorktree(t *testing.T) {
	root := gitRoot(t)
	os.WriteFile(filepath.Join(root, ".spectackle", "config.yaml"), []byte("schema: v0\n"), 0o644)
	t.Setenv("SPECTACKLE_AGENT", "alice")
	alice := connectRoot(t, root)
	prop := draftID(t, alice, map[string]any{"kind": "proposal", "title": "x"})
	callText(t, alice, "move", map[string]any{"id": prop, "to": "submitted"})
	callText(t, alice, "move", map[string]any{"id": prop, "to": "approved"})
	callText(t, alice, "work", map[string]any{"op": "start", "item": prop})
	out := callText(t, alice, "compact", map[string]any{})
	if !strings.Contains(out, "! WT E compact") {
		t.Fatalf("compact not blocked in worktree: %q", out)
	}
	out = callText(t, alice, "work", map[string]any{"op": "abort"})
	if !strings.Contains(out, "aborted") {
		t.Fatalf("abort: %q", out)
	}
	// item back to approved on main
	out = callText(t, alice, "get", map[string]any{"id": prop})
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

// ---- stale-binary hint (MCP-010) ----

const staleHintText = "h . binary stale — rebuild+restart: make dev"

// currentExecutableModTime resolves the running test binary's mtime the same
// way binaryStale does (os.Executable, symlinks resolved, Stat) — the
// reference point every staleness fixture below is built relative to.
func currentExecutableModTime(t *testing.T) time.Time {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	fi, err := os.Stat(exe)
	if err != nil {
		t.Fatalf("stat executable: %v", err)
	}
	return fi.ModTime()
}

// writeGoFileAt writes a trivial .go file at root-relative rel and stamps its
// mtime explicitly, so staleness fixtures don't depend on wall-clock timing
// between "build the test binary" and "write the fixture".
func writeGoFileAt(t *testing.T, root, rel string, mtime time.Time) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("package pkg\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, mtime, mtime); err != nil {
		t.Fatal(err)
	}
}

// staleWorkspace builds a fresh scaffolded workspace root for the staleness
// tests below — no git needed, the swarm/get tools it exercises don't touch
// version control.
func staleWorkspace(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	ws := workspace.Root{Dir: root}
	if err := ws.EnsureScaffold(""); err != nil {
		t.Fatal(err)
	}
	return root
}

// TestStaleHintSilentWhenBinaryNewest: 1. binary newer than all sources ->
// no hint anywhere in the result.
func TestStaleHintSilentWhenBinaryNewest(t *testing.T) {
	root := staleWorkspace(t)
	bin := currentExecutableModTime(t)
	writeGoFileAt(t, root, "pkg/older.go", bin.Add(-time.Hour))

	t.Setenv("SPECTACKLE_AGENT", "alice")
	alice := connectRoot(t, root)

	out := callText(t, alice, "swarm", map[string]any{})
	if strings.Contains(out, staleHintText) {
		t.Fatalf("stale hint fired though binary is newest: %q", out)
	}
}

// TestStaleHintFiresOnceOnCrossing: 2. a source file newer than the binary
// -> exactly one hint, naming the rebuild command, on the call that first
// observes the crossing.
func TestStaleHintFiresOnceOnCrossing(t *testing.T) {
	root := staleWorkspace(t)
	bin := currentExecutableModTime(t)
	writeGoFileAt(t, root, "pkg/newer.go", bin.Add(time.Hour))

	t.Setenv("SPECTACKLE_AGENT", "alice")
	alice := connectRoot(t, root)

	out := callText(t, alice, "swarm", map[string]any{})
	if n := strings.Count(out, staleHintText); n != 1 {
		t.Fatalf("want exactly one stale hint, got %d: %q", n, out)
	}
}

// TestStaleHintDebounced: 3. once per crossing — a second call inside the
// debounce window (the two calls below run back-to-back, well under
// staleCheckInterval) must not repeat the hint.
func TestStaleHintDebounced(t *testing.T) {
	root := staleWorkspace(t)
	bin := currentExecutableModTime(t)
	writeGoFileAt(t, root, "pkg/newer.go", bin.Add(time.Hour))

	t.Setenv("SPECTACKLE_AGENT", "alice")
	alice := connectRoot(t, root)

	out := callText(t, alice, "swarm", map[string]any{})
	if !strings.Contains(out, staleHintText) {
		t.Fatalf("stale hint missing on first (crossing) call: %q", out)
	}
	out2 := callText(t, alice, "get", map[string]any{"id": "does-not-matter"})
	if strings.Contains(out2, staleHintText) {
		t.Fatalf("stale hint repeated inside the debounce window: %q", out2)
	}
}

// TestStaleHintHonorsSkipDir: 4. a newer file inside a directory the
// workspace's skip rules prune (vendor/, a defaultSkipNames entry) must NOT
// trigger the hint — a naive walk that ignores ws.SkipDir fails this test.
func TestStaleHintHonorsSkipDir(t *testing.T) {
	root := staleWorkspace(t)
	bin := currentExecutableModTime(t)
	writeGoFileAt(t, root, "vendor/pruned/newer.go", bin.Add(time.Hour))

	t.Setenv("SPECTACKLE_AGENT", "alice")
	alice := connectRoot(t, root)

	out := callText(t, alice, "swarm", map[string]any{})
	if strings.Contains(out, staleHintText) {
		t.Fatalf("stale hint fired from a SkipDir-pruned directory: %q", out)
	}
}

// TestStaleHintSilentOnExecutableFailure: 5. os.Executable failing, or the
// resolved binary being unstattable, degrades to silence — never to an
// error on the tool call. execPath is swapped out (production is always
// os.Executable; see swarm.go) to simulate both failure shapes without a
// genuinely unresolvable binary in-process.
func TestStaleHintSilentOnExecutableFailure(t *testing.T) {
	root := staleWorkspace(t)
	bin := currentExecutableModTime(t)
	// a source newer than the binary would fire the hint were the walk ever
	// reached — proves the degrade happens before the walk, not because
	// there was nothing to report.
	writeGoFileAt(t, root, "pkg/newer.go", bin.Add(time.Hour))

	t.Run("ExecutableErrors", func(t *testing.T) {
		orig := execPath
		execPath = func() (string, error) { return "", fmt.Errorf("boom") }
		t.Cleanup(func() { execPath = orig })

		t.Setenv("SPECTACKLE_AGENT", "bob")
		bob := connectRoot(t, root)
		out := callText(t, bob, "swarm", map[string]any{})
		if strings.Contains(out, staleHintText) {
			t.Fatalf("stale hint fired despite os.Executable failing: %q", out)
		}
		if strings.Contains(out, "! ") {
			t.Fatalf("os.Executable failure degraded to a tool error, not silence: %q", out)
		}
	})

	t.Run("BinaryUnstattable", func(t *testing.T) {
		orig := execPath
		execPath = func() (string, error) { return filepath.Join(root, "no-such-binary"), nil }
		t.Cleanup(func() { execPath = orig })

		t.Setenv("SPECTACKLE_AGENT", "carol")
		carol := connectRoot(t, root)
		out := callText(t, carol, "swarm", map[string]any{})
		if strings.Contains(out, staleHintText) {
			t.Fatalf("stale hint fired despite an unstattable binary: %q", out)
		}
		if strings.Contains(out, "! ") {
			t.Fatalf("unstattable binary degraded to a tool error, not silence: %q", out)
		}
	})
}
