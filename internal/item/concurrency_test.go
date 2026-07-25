package item

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jxsl13/spectackle/internal/coord"
	"github.com/jxsl13/spectackle/internal/workspace"
)

// swarmAgents opens n REAL coord.db connections against the same file path —
// n distinct agent identities sharing one coord.db, which is the actual
// production shape (mcpserver.New opens exactly one coord.DB connection per
// agent PROCESS). SQLite's file locking does not know or care whether those
// connections live in one OS process or n, so writers racing through the
// returned roots exercise the real cross-connection serialization path, not
// a Go mutex standing in for it.
//
// One goroutine per returned Root, never more: coord.DB.Lock deliberately
// treats a name this agent already holds as reentrant rather than blocking
// (see its doc comment), because in production Server.mu already guarantees
// a single agent process issues tool calls one at a time — this lock only
// ever has to repel a DIFFERENT agent. Driving two goroutines through the
// SAME connection would exercise a scenario that guarantee already rules
// out and would fail for reasons that have nothing to do with this task's
// fix (an earlier draft of this test did exactly that and intermittently
// lost updates within one side — a faithful reproduction of "no external
// serialization" for a case that, in the real topology, is never reached
// unsynchronized).
func swarmAgents(t *testing.T, dir string, n int) []workspace.Root {
	t.Helper()
	path := filepath.Join(t.TempDir(), "coord.db")
	roots := make([]workspace.Root, n)
	for i := 0; i < n; i++ {
		d, err := coord.Open(path, fmt.Sprintf("agent%02d", i), i+1)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { d.Close() })
		roots[i] = workspace.Root{Dir: dir, Lock: d}
	}
	return roots
}

// TestConcurrentUpsertNoLostUpdate is the item-package-level shape of
// mcpserver's TestConcurrentDraftsPersistEveryItem acceptance test, isolated
// from the MCP tool plumbing: n independent agents Upsert distinct items
// into the SAME work.md concurrently, each through its own coord.db
// connection, with no synchronization of their own. Before withWorkLock
// existed this reproduced B-01KYD5 exactly — every side read the same
// N-item slice and the last write to finish erased the others' records;
// measured in production as 16 acknowledged drafts, as few as 10 surviving
// to disk.
func TestConcurrentUpsertNoLostUpdate(t *testing.T) {
	dir := t.TempDir()
	const n = 20
	roots := swarmAgents(t, dir, n)
	if err := roots[0].EnsureScaffold(""); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i, root := range roots {
		wg.Add(1)
		go func(i int, root workspace.Root) {
			defer wg.Done()
			it := Item{ID: fmt.Sprintf("T-%04d", i+1), Kind: "task", State: StateDraft, Title: fmt.Sprintf("item %d", i)}
			if err := Upsert(root, it); err != nil {
				errs <- err
			}
		}(i, root)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("Upsert: %v", err)
	}

	items, err := LoadAll(roots[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != n {
		t.Fatalf("%d of %d concurrent upserts reached disk — the rest were clobbered", len(items), n)
	}
	seen := map[string]bool{}
	for _, it := range items {
		if seen[it.ID] {
			t.Fatalf("duplicate item %s on disk", it.ID)
		}
		seen[it.ID] = true
	}
}

// TestConcurrentRemoveNoLostUpdate is the same shape as
// TestConcurrentUpsertNoLostUpdate for Remove: n items are seeded, then all
// n are removed concurrently, each by its own agent connection. Every
// removal must land — a stale reader that started before a sibling's delete
// landed would otherwise resurrect the sibling's target in its own rewrite.
func TestConcurrentRemoveNoLostUpdate(t *testing.T) {
	dir := t.TempDir()
	const n = 20
	roots := swarmAgents(t, dir, n)
	if err := roots[0].EnsureScaffold(""); err != nil {
		t.Fatal(err)
	}
	seed := make([]Item, n)
	for i := range seed {
		seed[i] = Item{ID: fmt.Sprintf("T-%04d", i+1), Kind: "task", State: StateDraft, Title: fmt.Sprintf("item %d", i)}
		if err := Upsert(roots[0], seed[i]); err != nil {
			t.Fatal(err)
		}
	}

	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i, root := range roots {
		wg.Add(1)
		go func(root workspace.Root, it Item) {
			defer wg.Done()
			if err := Remove(root, it); err != nil {
				errs <- err
			}
		}(root, seed[i])
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("Remove: %v", err)
	}

	items, err := LoadAll(roots[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("%d items survived a full concurrent removal: %+v", len(items), items)
	}
}

// blockingLocker is a deterministic stand-in for coord.DB used only to prove
// lock GRANULARITY without timing-based flakiness: WithLock blocks on the
// exact target name until released and runs every other name immediately —
// a real coord.DB behaves identically per name (see coord's
// TestWithLockParallelAcrossNames, which proves that against two actual
// connections), so this only needs to show withWorkLock computes a distinct
// name per context dir, not re-prove the underlying primitive.
type blockingLocker struct {
	target  string
	holding chan struct{}
	release chan struct{}
}

func (l *blockingLocker) WithLock(name string, fn func() error) error {
	if name == l.target {
		close(l.holding)
		<-l.release
	}
	return fn()
}

// TestConcurrentUpsertDifferentContextDirsNotSerialized proves the "parallel
// across contexts" requirement: a writer to dir "a" blocked mid-lock must
// not hold up a writer to dir "b" — a global lock (or a lock keyed without
// the context) would make this test hang until the timeout fires.
func TestConcurrentUpsertDifferentContextDirsNotSerialized(t *testing.T) {
	root := ws(t)
	bl := &blockingLocker{target: "work:a", holding: make(chan struct{}), release: make(chan struct{})}
	root.Lock = bl

	aDone := make(chan struct{})
	go func() {
		if err := Upsert(root, Item{ID: "T-0001", Kind: "task", State: StateDraft, Title: "a", Dir: "a"}); err != nil {
			t.Errorf("Upsert dir=a: %v", err)
		}
		close(aDone)
	}()
	<-bl.holding // dir "a"'s writer is now blocked inside its own lock

	bDone := make(chan struct{})
	go func() {
		if err := Upsert(root, Item{ID: "T-0002", Kind: "task", State: StateDraft, Title: "b", Dir: "b"}); err != nil {
			t.Errorf("Upsert dir=b: %v", err)
		}
		close(bDone)
	}()
	select {
	case <-bDone:
		// dir "b" completed while dir "a" is still held — not serialized.
	case <-time.After(2 * time.Second):
		t.Fatal("Upsert to a different context dir serialized behind an unrelated lock")
	}
	close(bl.release)
	<-aDone
}

// panicLocker fails the test the moment anything calls WithLock on it —
// used to prove the read path (LoadAll, Get) never acquires root.Lock, per
// this task's "no lock in the read path" requirement.
type panicLocker struct{ t *testing.T }

func (l panicLocker) WithLock(name string, fn func() error) error {
	l.t.Fatalf("read path acquired lock %q — get/find/state must never lock", name)
	return nil
}

func TestReadPathsNeverLock(t *testing.T) {
	root := ws(t)
	it := Item{ID: "T-0001", Kind: "task", State: StateDraft, Title: "x", Created: "2026-07-24"}
	if err := Upsert(root, it); err != nil {
		t.Fatal(err)
	}
	root.Lock = panicLocker{t: t}
	if _, err := LoadAll(root); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := Get(root, "T-0001"); err != nil || !ok {
		t.Fatalf("Get = %v, %v", ok, err)
	}
	if _, err := LoadWork(root.WorkPath(""), ""); err != nil {
		t.Fatal(err)
	}
}
