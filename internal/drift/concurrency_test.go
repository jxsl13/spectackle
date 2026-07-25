package drift

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/jxsl13/spectackle/internal/coord"
	"github.com/jxsl13/spectackle/internal/graph"
	"github.com/jxsl13/spectackle/internal/workspace"
)

// swarmAgents opens n REAL coord.db connections against the same file path —
// n distinct agent identities sharing one coord.db, the actual production
// shape (mcpserver.New opens exactly one coord.DB connection per agent
// PROCESS). SQLite's file locking does not know or care whether those
// connections live in one OS process or n, so writers racing through the
// returned roots exercise the real cross-connection serialization path, not
// a Go mutex standing in for it.
//
// One goroutine per returned Root, never more — see the identical helper and
// full rationale in internal/item/concurrency_test.go: coord.DB.Lock treats
// a name this agent already holds as reentrant rather than blocking, which
// is safe in production only because Server.mu already guarantees one agent
// process issues tool calls one at a time.
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

// TestConcurrentAnchorsRewriteNoLostUpdate: n agents each add their own
// distinct anchor row concurrently, each running the full Load-mutate-save
// cycle inside one WithAnchorsLock call — the shape WithAnchorsLock's doc
// comment requires: locking Save alone would still lose updates here,
// because each agent's Load already happened before Save's own lock (if it
// were the only one held) was even requested. Every row must survive.
func TestConcurrentAnchorsRewriteNoLostUpdate(t *testing.T) {
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
			a := Anchor{
				Rule: fmt.Sprintf("CUC-%03d", i+1), Node: graph.NodeID(fmt.Sprintf("go:pkg.F%d", i)),
				File: "-", CHash: "-", RHash: "-",
			}
			err := WithAnchorsLock(root, func() error {
				anchors, err := Load(root)
				if err != nil {
					return err
				}
				return save(root, Upsert(anchors, a))
			})
			if err != nil {
				errs <- err
			}
		}(i, root)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("locked anchors update: %v", err)
	}

	anchors, err := Load(roots[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(anchors) != n {
		t.Fatalf("%d of %d concurrent anchor writes reached disk — the rest were clobbered", len(anchors), n)
	}
	seen := map[string]bool{}
	for _, a := range anchors {
		key := a.Rule + "\x00" + string(a.Node)
		if seen[key] {
			t.Fatalf("duplicate anchor %s/%s on disk", a.Rule, a.Node)
		}
		seen[key] = true
	}
}

// panicLocker fails the test the instant anything calls WithLock on it —
// used to prove Load never acquires ws.Lock, per this task's "no lock in
// the read path" requirement.
type panicLocker struct{ t *testing.T }

func (l panicLocker) WithLock(name string, fn func() error) error {
	l.t.Fatalf("read path acquired lock %q — get/find/state must never lock", name)
	return nil
}

func TestLoadNeverLocks(t *testing.T) {
	root := t.TempDir()
	ws := workspace.Root{Dir: root}
	if err := Save(ws, []Anchor{{Rule: "R-001", Node: "go:pkg.F", File: "-", CHash: "-", RHash: "-"}}); err != nil {
		t.Fatal(err)
	}
	ws.Lock = panicLocker{t: t}
	if _, err := Load(ws); err != nil {
		t.Fatal(err)
	}
}
