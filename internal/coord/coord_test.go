package coord

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func open2(t *testing.T) (*DB, *DB) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "coord.db")
	a, err := Open(path, "alice", 1)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Open(path, "bob", 2)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close(); b.Close() })
	return a, b
}

const ttl = 10 * time.Minute

// TestNextIDUniqueAcrossConnections hammers the counter from two connections
// (the two-process shape) — no duplicates, no regression below the floor.
func TestNextIDUniqueAcrossConnections(t *testing.T) {
	a, b := open2(t)
	const perSide = 50
	seen := make(chan int, 2*perSide)
	var wg sync.WaitGroup
	for _, d := range []*DB{a, b} {
		wg.Add(1)
		go func(d *DB) {
			defer wg.Done()
			for i := 0; i < perSide; i++ {
				n, err := d.NextID("item:P", 3) // floor from a pretend file scan
				if err != nil {
					t.Errorf("NextID: %v", err)
					return
				}
				seen <- n
			}
		}(d)
	}
	wg.Wait()
	close(seen)
	got := map[int]bool{}
	for n := range seen {
		if n <= 3 {
			t.Fatalf("minted %d at or below the floor", n)
		}
		if got[n] {
			t.Fatalf("duplicate ID %d minted across connections", n)
		}
		got[n] = true
	}
	if len(got) != 2*perSide {
		t.Fatalf("minted %d unique IDs, want %d", len(got), 2*perSide)
	}
	// floor re-seeding after coord.db loss: higher scan floor wins
	if n, _ := a.NextID("item:P", 500); n != 501 {
		t.Fatalf("floor reseed = %d, want 501", n)
	}
}

func TestClaimOverlapMatrix(t *testing.T) {
	a, b := open2(t)
	if c, err := a.Claim([]string{"gpu/kernels", "P-0001"}, "P-0001", ttl, ttl); c != nil || err != nil {
		t.Fatalf("initial claim: %+v %v", c, err)
	}
	cases := []struct {
		path     string
		conflict bool
	}{
		{"gpu/kernels", true},          // equal
		{"gpu/kernels/saxpy.cu", true}, // child of leased dir
		{"gpu", true},                  // parent of leased dir
		{"gpu/other", false},           // sibling
		{"gpuX", false},                // prefix but not path-prefix
		{"P-0001", true},               // item ID equality
		{"P-0002", false},
	}
	for _, c := range cases {
		got, err := b.Claim([]string{c.path}, "T-0009", ttl, ttl)
		if err != nil {
			t.Fatal(err)
		}
		if (got != nil) != c.conflict {
			t.Errorf("claim %q: conflict=%v, want %v", c.path, got != nil, c.conflict)
		}
		if got == nil {
			b.Release([]string{c.path})
		}
	}
	// own re-claim refreshes, never conflicts
	if c, _ := a.Claim([]string{"gpu/kernels"}, "P-0001", ttl, ttl); c != nil {
		t.Fatalf("own re-claim conflicted: %+v", c)
	}
	// release frees for the sibling
	a.Release(nil)
	if c, _ := b.Claim([]string{"gpu/kernels"}, "T-0009", ttl, ttl); c != nil {
		t.Fatalf("claim after release conflicted: %+v", c)
	}
}

func TestStaleAgentSweep(t *testing.T) {
	a, b := open2(t)
	if c, _ := a.Claim([]string{"memory"}, "P-0001", ttl, ttl); c != nil {
		t.Fatal("claim failed")
	}
	// age alice's heartbeat directly (white-box)
	if _, err := a.db.Exec(`UPDATE agents SET hb=? WHERE name='alice'`, time.Now().Add(-2*time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	// stale holder no longer blocks
	if c, _ := b.Blocked([]string{"memory/pool.go"}, 15*time.Minute); c != nil {
		t.Fatalf("stale lease still blocks: %+v", c)
	}
	expired, err := b.Sweep(15 * time.Minute)
	if err != nil || len(expired) != 1 || expired[0].Agent != "alice" {
		t.Fatalf("Sweep = %+v, %v", expired, err)
	}
	events, _ := a.After(0, 10) // alice sees bob's expire event
	found := false
	for _, e := range events {
		if e.Ev == "expire" && e.Agent == "bob" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expire event not emitted: %+v", events)
	}
	// SPX-SWM-006: alice's registry row is gone with her lease
	agents, err := b.Agents()
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range agents {
		if a.Name == "alice" {
			t.Fatalf("stale agent row survived the sweep: %+v", agents)
		}
	}
	// alice is still alive, just idle past TTL: her next heartbeat re-registers
	if err := a.Heartbeat(); err != nil {
		t.Fatal(err)
	}
	agents, _ = b.Agents()
	found = false
	for _, ag := range agents {
		found = found || ag.Name == "alice"
	}
	if !found {
		t.Fatalf("heartbeat after sweep must re-register: %+v", agents)
	}
}

func TestSweepDeletesLeaselessStaleAgents(t *testing.T) {
	// the common case: a short-lived driver session registered, held no
	// lease, and went away — its row must not linger past agent_ttl.
	a, b := open2(t)
	if _, err := a.db.Exec(`UPDATE agents SET hb=? WHERE name='alice'`,
		time.Now().Add(-2*time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	expired, err := b.Sweep(15 * time.Minute)
	if err != nil || len(expired) != 0 {
		t.Fatalf("Sweep = %+v, %v (no leases were held)", expired, err)
	}
	agents, _ := b.Agents()
	for _, ag := range agents {
		if ag.Name == "alice" {
			t.Fatalf("lease-less stale agent survived the sweep: %+v", agents)
		}
	}
}

func TestDeregister(t *testing.T) {
	a, b := open2(t)
	if c, _ := a.Claim([]string{"gpu"}, "P-0001", ttl, ttl); c != nil {
		t.Fatal("claim failed")
	}
	if err := a.Deregister(); err != nil {
		t.Fatal(err)
	}
	agents, _ := b.Agents()
	for _, ag := range agents {
		if ag.Name == "alice" {
			t.Fatalf("deregistered agent still listed: %+v", agents)
		}
	}
	leases, _ := b.Leases(ttl)
	if len(leases) != 0 {
		t.Fatalf("deregister must drop own leases: %+v", leases)
	}
	// scope is immediately claimable by the sibling
	if c, _ := b.Claim([]string{"gpu"}, "T-0001", ttl, ttl); c != nil {
		t.Fatalf("claim after deregister conflicted: %+v", c)
	}
}

func TestEventsCursorAndSearch(t *testing.T) {
	a, b := open2(t)
	a.Emit("reject", "P-0003", "VRAM residency breaks multi-tenant scheduling")
	a.Emit("rule", "CUDA-KRN-002", "add: kernel bounds check")

	// bob sees alice's events, alice does not see her own
	ev, err := b.After(0, 10)
	if err != nil || len(ev) != 2 || ev[0].Ev != "reject" {
		t.Fatalf("After = %+v, %v", ev, err)
	}
	if ev, _ := a.After(0, 10); len(ev) != 0 {
		t.Fatalf("own events must be filtered: %+v", ev)
	}
	// cursor advances
	b.SetCursor(ev[1].Seq)
	if ev, _ := b.After(mustCursor(t, b), 10); len(ev) != 0 {
		t.Fatalf("cursor did not advance: %+v", ev)
	}
	// search
	hits, _ := b.SearchEvents("multi-tenant", []string{"reject"}, 5)
	if len(hits) != 1 || hits[0].Ref != "P-0003" {
		t.Fatalf("SearchEvents = %+v", hits)
	}
}

// TestFreshAgentNoReplay (T-0015): an agent that registers for the first
// time must not replay events emitted before it ever connected — its cursor
// starts at the current MAX(seq), not 0.
func TestFreshAgentNoReplay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "coord.db")
	a, err := Open(path, "alice", 1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })

	a.Emit("rule", "SPX-ARC-001", "add: first event")
	a.Emit("rule", "SPX-ARC-002", "add: second event")

	// charlie registers only now, after both events already exist
	charlie, err := Open(path, "charlie", 3)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { charlie.Close() })

	cur, err := charlie.Cursor()
	if err != nil {
		t.Fatal(err)
	}
	if ev, _ := charlie.After(cur, 10); len(ev) != 0 {
		t.Fatalf("fresh agent replayed pre-existing events: %+v", ev)
	}

	// an event emitted after charlie's Open must still reach it
	a.Emit("rule", "SPX-ARC-003", "add: third event")
	ev, err := charlie.After(cur, 10)
	if err != nil || len(ev) != 1 || ev[0].Ref != "SPX-ARC-003" {
		t.Fatalf("charlie missed a post-registration event: %+v, %v", ev, err)
	}
}

// TestReRegisterAfterSweepNoReplay (T-0015): a sibling's sweep deletes an
// idle agent's registry row; its next Heartbeat re-registers it, and that
// re-registration must set cursor to the current MAX(seq) too — otherwise
// the whole event history replays into a long-idle-but-alive agent.
func TestReRegisterAfterSweepNoReplay(t *testing.T) {
	a, b := open2(t)
	// age alice's heartbeat so she looks stale to a sweep
	if _, err := a.db.Exec(`UPDATE agents SET hb=? WHERE name='alice'`, time.Now().Add(-2*time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	b.Emit("rule", "SPX-ARC-001", "event before alice re-registers")
	if _, err := b.Sweep(15 * time.Minute); err != nil {
		t.Fatal(err)
	}
	// alice is still alive; her next heartbeat re-registers her row
	if err := a.Heartbeat(); err != nil {
		t.Fatal(err)
	}
	cur, err := a.Cursor()
	if err != nil {
		t.Fatal(err)
	}
	if ev, _ := a.After(cur, 10); len(ev) != 0 {
		t.Fatalf("re-registered agent replayed pre-existing history: %+v", ev)
	}
	b.Emit("rule", "SPX-ARC-002", "event after alice re-registers")
	ev, err := a.After(cur, 10)
	if err != nil || len(ev) != 1 || ev[0].Ref != "SPX-ARC-002" {
		t.Fatalf("re-registered agent missed a post-registration event: %+v, %v", ev, err)
	}
}

func mustCursor(t *testing.T, d *DB) int64 {
	t.Helper()
	c, err := d.Cursor()
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestIntegrateLock(t *testing.T) {
	a, b := open2(t)
	ok, _, err := a.LockIntegrate(time.Minute)
	if err != nil || !ok {
		t.Fatalf("lock: %v %v", ok, err)
	}
	if ok, holder, _ := b.LockIntegrate(time.Minute); ok || holder != "alice" {
		t.Fatalf("second lock: ok=%v holder=%s", ok, holder)
	}
	// re-entrant for the holder
	if ok, _, _ := a.LockIntegrate(time.Minute); !ok {
		t.Fatal("holder re-lock failed")
	}
	a.UnlockIntegrate()
	if ok, _, _ := b.LockIntegrate(time.Minute); !ok {
		t.Fatal("lock after unlock failed")
	}
	// expired lock counts as free (crashed integrator)
	b.db.Exec(`UPDATE locks SET exp=? WHERE name='integrate'`, time.Now().Add(-time.Minute).Unix())
	if ok, _, _ := a.LockIntegrate(time.Minute); !ok {
		t.Fatal("expired lock not stealable")
	}
}

func TestAppliedAndWorktrees(t *testing.T) {
	a, _ := open2(t)
	a.MarkApplied("T-0001", []string{"aa", "bb"})
	a.MarkApplied("T-0001", []string{"bb", "cc"}) // idempotent
	m, _ := a.Applied("T-0001")
	if len(m) != 3 || !m["aa"] || !m["cc"] {
		t.Fatalf("Applied = %+v", m)
	}
	w := Worktree{Item: "T-0001", Agent: "alice", Branch: "spectackle/T-0001", Root: "/x", Base: "abc", State: "open"}
	a.PutWorktree(w)
	got, ok, _ := a.GetWorktree("T-0001")
	if !ok || got.Branch != w.Branch || got.State != "open" {
		t.Fatalf("GetWorktree = %+v %v", got, ok)
	}
	a.DelWorktree("T-0001")
	if _, ok, _ := a.GetWorktree("T-0001"); ok {
		t.Fatal("DelWorktree failed")
	}
}

// TestLockGeneralized proves Lock/Unlock work for an arbitrary name, not just
// the 'integrate' special case LockIntegrate used to hardcode — the same
// three properties TestIntegrateLock already covers for that one name: a
// live foreign holder blocks, the same agent re-locking is reentrant (see
// Lock's doc for why that's deliberate, not a gap), and an expired holder's
// row is stealable (crash safety survives the generalization).
func TestLockGeneralized(t *testing.T) {
	a, b := open2(t)
	ok, _, err := a.Lock("work:gpu", time.Minute)
	if err != nil || !ok {
		t.Fatalf("lock: %v %v", ok, err)
	}
	if ok, holder, _ := b.Lock("work:gpu", time.Minute); ok || holder != "alice" {
		t.Fatalf("second lock: ok=%v holder=%s", ok, holder)
	}
	if ok, _, _ := a.Lock("work:gpu", time.Minute); !ok {
		t.Fatal("holder re-lock failed")
	}
	a.Unlock("work:gpu")
	if ok, _, _ := b.Lock("work:gpu", time.Minute); !ok {
		t.Fatal("lock after unlock failed")
	}
	b.db.Exec(`UPDATE locks SET exp=? WHERE name='work:gpu'`, time.Now().Add(-time.Minute).Unix())
	if ok, _, _ := a.Lock("work:gpu", time.Minute); !ok {
		t.Fatal("expired lock not stealable")
	}
}

// TestWithLockNoLostUpdate is the property-level analogue of
// TestNextIDUniqueAcrossConnections, aimed at WithLock instead of NextID: N
// writers split across TWO REAL coord.db connections (the two-process shape
// this whole task exists for — SQLite's file locking does not know or care
// whether two *sql.DB handles live in one OS process or two, so this
// exercises the actual cross-connection serialization path, not a Go mutex)
// perform a whole-file read-modify-write of a shared counter file with no
// synchronization of their own. Without WithLock this reproduces exactly the
// item.Upsert defect (both sides read N, both write N+1); with it, every
// increment must land.
func TestWithLockNoLostUpdate(t *testing.T) {
	a, b := open2(t)
	path := filepath.Join(t.TempDir(), "counter.txt")
	if err := os.WriteFile(path, []byte("0"), 0o644); err != nil {
		t.Fatal(err)
	}
	const perSide = 25
	var wg sync.WaitGroup
	errs := make(chan error, 2*perSide)
	for _, d := range []*DB{a, b} {
		wg.Add(1)
		go func(d *DB) {
			defer wg.Done()
			for i := 0; i < perSide; i++ {
				err := d.WithLock("counter", func() error {
					raw, err := os.ReadFile(path)
					if err != nil {
						return err
					}
					n, err := strconv.Atoi(strings.TrimSpace(string(raw)))
					if err != nil {
						return err
					}
					return os.WriteFile(path, []byte(strconv.Itoa(n+1)), 0o644)
				})
				if err != nil {
					errs <- err
				}
			}
		}(d)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("WithLock: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := strconv.Atoi(strings.TrimSpace(string(raw)))
	if got != 2*perSide {
		t.Fatalf("counter = %d, want %d — %d increments were lost", got, 2*perSide, 2*perSide-got)
	}
}

// TestWithLockReleasesOnPanic proves the scoped-execution wrapper releases
// the lock on every exit path, including a panic in fn — the bare
// Lock/Unlock pair this replaces would leave the row held (until its ttl
// lapses) if fn ever panicked between the two calls.
func TestWithLockReleasesOnPanic(t *testing.T) {
	a, b := open2(t)
	func() {
		defer func() { recover() }()
		a.WithLock("panicky", func() error { panic("boom") })
	}()
	ok, holder, err := b.Lock("panicky", time.Minute)
	if err != nil || !ok {
		t.Fatalf("lock after panic: ok=%v holder=%s err=%v", ok, holder, err)
	}
}

// TestWithLockBusyTimesOut proves a caller that cannot get the lock fails
// loudly with the current holder named, instead of blocking forever — a
// stuck sibling should surface as an error, never a hang.
func TestWithLockBusyTimesOut(t *testing.T) {
	a, b := open2(t)
	if ok, _, err := a.Lock("stuck", time.Minute); err != nil || !ok {
		t.Fatalf("setup lock: %v %v", ok, err)
	}
	saved := writeLockAcquireTimeout
	writeLockAcquireTimeout = 100 * time.Millisecond
	defer func() { writeLockAcquireTimeout = saved }()
	err := b.WithLock("stuck", func() error { return nil })
	if err == nil || !strings.Contains(err.Error(), "alice") {
		t.Fatalf("WithLock against a busy lock = %v, want a timeout naming the holder", err)
	}
}

// TestWithLockParallelAcrossNames proves two DIFFERENT lock names never
// serialize each other — the granularity property every per-context writer
// (item's "work:<ctx>", spec's "spec:<ctx>") depends on. Global-lock-in-
// disguise would make this test hang; a deterministic handshake (channels,
// not sleeps) proves B completes while A is still held, not just that B
// completes eventually.
func TestWithLockParallelAcrossNames(t *testing.T) {
	a, b := open2(t)
	aHolding := make(chan struct{})
	release := make(chan struct{})
	go func() {
		a.WithLock("ctx-a", func() error {
			close(aHolding)
			<-release
			return nil
		})
	}()
	<-aHolding // a definitely holds "ctx-a" now, and will until we close release
	bDone := make(chan struct{})
	go func() {
		b.WithLock("ctx-b", func() error { return nil })
		close(bDone)
	}()
	select {
	case <-bDone:
		// b finished while a is still holding a disjoint name — no serialization.
	case <-time.After(2 * time.Second):
		t.Fatal("WithLock on a different name blocked behind an unrelated holder")
	}
	close(release)
}

// TestLockDoesNotBlockLeaseAndViceVersa establishes the ordering this task's
// brief demands be stated explicitly: the named write-lock (table `locks`)
// and scope leases (table `leases`) are independent resources that no code
// path acquires nested inside the other, so there is no ordering to violate.
// This test is the regression guard for that claim: one agent holds the
// write-lock while a DIFFERENT agent claims a lease, and vice versa, and
// neither blocks on the other.
func TestLockDoesNotBlockLeaseAndViceVersa(t *testing.T) {
	a, b := open2(t)
	if ok, _, err := a.Lock("work:.", time.Minute); err != nil || !ok {
		t.Fatalf("lock: %v %v", ok, err)
	}
	done := make(chan struct{})
	go func() {
		if _, err := b.Claim([]string{"gpu"}, "T-0001", time.Minute, time.Hour); err != nil {
			t.Errorf("Claim while sibling holds write-lock: %v", err)
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Claim blocked behind an unrelated write-lock holder")
	}
	a.Unlock("work:.")

	if _, err := a.Claim([]string{"math"}, "T-0002", time.Minute, time.Hour); err != nil {
		t.Fatal(err)
	}
	done2 := make(chan struct{})
	go func() {
		if ok, _, err := b.Lock("work:other", time.Minute); err != nil || !ok {
			t.Errorf("Lock while sibling holds a lease: ok=%v err=%v", ok, err)
		}
		close(done2)
	}()
	select {
	case <-done2:
	case <-time.After(2 * time.Second):
		t.Fatal("Lock blocked behind an unrelated lease holder")
	}
}
