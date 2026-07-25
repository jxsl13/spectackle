package mcpserver

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jxsl13/spectackle/internal/item"
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
		[]byte("schema: v1\nverify: [\"test -f ok.txt\"]\n"), 0o644); err != nil {
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

// TestTwoServersMintUniqueIDs asserts uniqueness over the IDs ON DISK, never
// over the IDs the two servers PRINTED. Those are different things, and the
// distinction is the whole point: a rendered ID is the shortest prefix that
// was unambiguous against the printing server's own known set at that
// instant. Alice and Bob shorten independently, so two genuinely distinct
// full IDs minted in the same millisecond — UUIDv7 leads with a 48-bit
// millisecond timestamp, so near-simultaneous mints share a long run — can
// and do render to the same short token. Comparing the printed tokens made
// this test fail on a timing coincidence while the invariant it names held
// perfectly.
func TestTwoServersMintUniqueIDs(t *testing.T) {
	root := t.TempDir()
	alice, bob := twoAgents(t, root)
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
	for i := 0; i < 2*n; i++ {
		if id := <-results; !strings.HasPrefix(id, "T-") { // sw piggyback line may shift fields
			t.Fatalf("unexpected ID token %q", id)
		}
	}

	// Every draft acknowledged; now read what was actually stored. Stored
	// values are always full IDs, so this is the durable comparison.
	items, err := item.LoadAll(workspace.Root{Dir: root})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, it := range items {
		if !strings.HasPrefix(it.ID, "T-") {
			continue
		}
		if seen[it.ID] {
			t.Fatalf("duplicate full ID %s stored across two servers", it.ID)
		}
		seen[it.ID] = true
	}
	out := callText(t, alice, "check", map[string]any{})
	if strings.Contains(out, "E101") {
		t.Fatalf("duplicate item IDs on disk:\n%s", out)
	}
}

// TestConcurrentDraftsPersistEveryItem is the acceptance test for
// B-01KYD57FN3ERHBM5EQ3534YJXP, fixed by item.Upsert taking coord.db's named
// "work:<ctx>" lock around its whole read-modify-write (internal/item/
// item.go's withWorkLock) instead of just the final write. Server.mu does
// not help on its own — it is per process, and the shipped topology is N
// stdio processes — but it does mean each server (alice, bob) only ever
// issues one coord.db call at a time, so the cross-process case this proves
// is exactly the one withWorkLock exists for.
//
// Unique IDs do not close this. P-0088 removed the chance of two records
// sharing an ID; it did nothing about a record being overwritten by a
// neighbor whose ID was never in doubt.
func TestConcurrentDraftsPersistEveryItem(t *testing.T) {
	root := t.TempDir()
	alice, bob := twoAgents(t, root)
	const n = 8
	done := make(chan struct{}, 2*n)
	for i := 0; i < n; i++ {
		go func(i int) {
			callText(t, alice, "draft", map[string]any{"kind": "task", "title": fmt.Sprintf("alice %d", i)})
			done <- struct{}{}
		}(i)
		go func(i int) {
			callText(t, bob, "draft", map[string]any{"kind": "task", "title": fmt.Sprintf("bob %d", i)})
			done <- struct{}{}
		}(i)
	}
	for i := 0; i < 2*n; i++ {
		<-done
	}

	items, err := item.LoadAll(workspace.Root{Dir: root})
	if err != nil {
		t.Fatal(err)
	}
	stored := 0
	for _, it := range items {
		if strings.HasPrefix(it.ID, "T-") {
			stored++
		}
	}
	if stored != 2*n {
		t.Fatalf("%d of %d acknowledged drafts reached disk — the rest were clobbered", stored, 2*n)
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
	os.WriteFile(filepath.Join(root, ".spectackle", "config.yaml"), []byte("schema: v1\n"), 0o644)
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
	os.WriteFile(filepath.Join(root, ".spectackle", "config.yaml"), []byte("schema: v1\n"), 0o644)
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
// version control. Its go.mod declares spectackle's OWN module: every test
// using this fixture runs the test binary itself (Version keeps its "-dev"
// default, see server.go), so this fixture is the "dev build serving
// spectackle's own tree" half of staleEligible — the half that must still
// produce today's hint behavior (T-01KYD8H / issue #29). The unrelated-
// module half is foreignModuleWorkspace below.
func staleWorkspace(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	ws := workspace.Root{Dir: root}
	if err := ws.EnsureScaffold(""); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"),
		[]byte("module "+modulePath+"\n\ngo 1.25.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// foreignModuleWorkspace is staleWorkspace with its go.mod overwritten to
// declare a module OTHER than spectackle's own — a dev build pointed at
// someone else's repository, the second way issue #29's advice could be
// unfollowable even once the release-vs-dev half is fixed.
func foreignModuleWorkspace(t *testing.T) string {
	t.Helper()
	root := staleWorkspace(t)
	if err := os.WriteFile(filepath.Join(root, "go.mod"),
		[]byte("module example.com/someone/else\n\ngo 1.25.0\n"), 0o644); err != nil {
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

// ---- stale-binary eligibility gate (issue #29 / T-01KYD8H) ----
//
// staleHint used to fire from ANY binary serving ANY tree, including a
// released, installed binary whose own repository's sources are (almost
// always) newer than the binary — unfollowable advice on every single
// tool call, since the install has no Makefile to run "make dev" from.
// The two tests below cover the eligibility gate's failure half; the
// existing tests above (staleWorkspace, a dev-Version test binary serving
// a workspace whose go.mod matches spectackle's own module) are the
// success half and are deliberately left as they were.

// TestStaleHintSilentOnReleaseBuild: a stamped release Version never
// hints, on any tree, and — the point of gating BEFORE the walk — never
// even resolves the executable to check. execPath is swapped for a spy
// so a walk that ran despite the gate would be caught here, not just
// coincidentally produce the right answer.
func TestStaleHintSilentOnReleaseBuild(t *testing.T) {
	root := staleWorkspace(t) // spectackle's own module — proves (a) alone gates
	bin := currentExecutableModTime(t)
	// newer than the binary: would fire the hint were the walk ever reached.
	writeGoFileAt(t, root, "pkg/newer.go", bin.Add(time.Hour))

	origVersion := Version
	Version = "v0.3.0" // stamped release: no "dev" marker
	t.Cleanup(func() { Version = origVersion })

	walked := false
	origExec := execPath
	execPath = func() (string, error) { walked = true; return origExec() }
	t.Cleanup(func() { execPath = origExec })

	t.Setenv("SPECTACKLE_AGENT", "alice")
	alice := connectRoot(t, root)

	out := callText(t, alice, "swarm", map[string]any{})
	if strings.Contains(out, staleHintText) {
		t.Fatalf("stale hint fired on a released Version: %q", out)
	}
	if walked {
		t.Fatalf("binaryStale's walk ran despite a released Version — staleEligible should short-circuit before execPath is ever called")
	}

	// same released Version, a second call: still nothing, still no walk.
	out2 := callText(t, alice, "get", map[string]any{"id": "does-not-matter"})
	if strings.Contains(out2, staleHintText) {
		t.Fatalf("stale hint fired on a released Version (second call): %q", out2)
	}
	if walked {
		t.Fatalf("binaryStale's walk ran on a later call despite a released Version")
	}
}

// TestStaleHintSilentOnUnrelatedModule: a dev-Version test binary (the
// same binary every test in this file runs as) serving a tree whose
// go.mod declares a DIFFERENT module must not hint either — the advice
// names spectackle's own Makefile target, unfollowable against a
// repository that isn't spectackle at all.
func TestStaleHintSilentOnUnrelatedModule(t *testing.T) {
	root := foreignModuleWorkspace(t)
	bin := currentExecutableModTime(t)
	writeGoFileAt(t, root, "pkg/newer.go", bin.Add(time.Hour))

	t.Setenv("SPECTACKLE_AGENT", "alice")
	alice := connectRoot(t, root)

	out := callText(t, alice, "swarm", map[string]any{})
	if strings.Contains(out, staleHintText) {
		t.Fatalf("stale hint fired though the served workspace is not spectackle's own module: %q", out)
	}
}

// TestReadModulePath exercises the go.mod line scan directly: the exact
// shapes it must tolerate (surrounding whitespace, a go.mod with more than
// just the module line, one with none at all) without pulling in
// golang.org/x/mod/modfile for a single directive.
func TestReadModulePath(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
		wantOK  bool
	}{
		{"simple", "module github.com/example/foo\n\ngo 1.25.0\n", "github.com/example/foo", true},
		{"leading blank lines", "\n\nmodule github.com/example/foo\n", "github.com/example/foo", true},
		{"indented", "   module github.com/example/foo  \n", "github.com/example/foo", true},
		{"no module line", "go 1.25.0\n", "", false},
		{"empty file", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(tt.content), 0o644); err != nil {
				t.Fatal(err)
			}
			got, ok := readModulePath(dir)
			if got != tt.want || ok != tt.wantOK {
				t.Errorf("readModulePath(%q) = (%q, %v), want (%q, %v)", tt.content, got, ok, tt.want, tt.wantOK)
			}
		})
	}

	t.Run("missing go.mod", func(t *testing.T) {
		got, ok := readModulePath(t.TempDir())
		if ok || got != "" {
			t.Errorf("readModulePath on a dir with no go.mod = (%q, %v), want (\"\", false)", got, ok)
		}
	})
}

// TestIsDevBuild pins the substring rule against both shapes Version
// actually takes: the compiled-in default (server.go) and a -ldflags
// stamped release tag.
func TestIsDevBuild(t *testing.T) {
	orig := Version
	t.Cleanup(func() { Version = orig })

	Version = "0.2.0-dev"
	if !isDevBuild() {
		t.Errorf("isDevBuild() = false for the compiled-in dev default %q", Version)
	}
	Version = "v0.3.0"
	if isDevBuild() {
		t.Errorf("isDevBuild() = true for a stamped release tag %q", Version)
	}
}
