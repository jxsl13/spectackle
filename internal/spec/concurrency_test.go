package spec

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/jxsl13/spectackle/internal/coord"
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
// process issues tool calls one at a time. Sharing one connection across
// multiple goroutines here would test a scenario that guarantee rules out.
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

// TestConcurrentAddRuleNoLostUpdate: n agents each AddRule a distinct
// ForceID into the SAME bundle file concurrently, through n independent
// coord.db connections. Before withSpecLock existed this had the same shape
// as item.Upsert's B-01KYD5 race: AddRule reads the file's current bytes,
// appends its block, and rewrites the whole file with no lock around the
// read+write, so two concurrent adds both reading the pre-add bytes would
// have the later write erase the earlier one's block.
func TestConcurrentAddRuleNoLostUpdate(t *testing.T) {
	dir := t.TempDir()
	const n = 20
	roots := swarmAgents(t, dir, n)
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i, root := range roots {
		wg.Add(1)
		go func(i int, root workspace.Root) {
			defer wg.Done()
			req := AuthorReq{
				Dir:      "pkg",
				ForceID:  fmt.Sprintf("CUC-%03d", i+1),
				Sentence: "The system SHALL log to `stderr` only.",
			}
			res, err := AddRule(root, c, req)
			if err != nil {
				errs <- err
				return
			}
			if !res.Written {
				errs <- fmt.Errorf("agent%02d: AddRule not written, findings=%+v", i, res.Findings)
			}
		}(i, root)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("AddRule: %v", err)
	}

	c2, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("CUC-%03d", i+1)
		if _, ok := c2.Rule(id); !ok {
			t.Errorf("rule %s missing after concurrent AddRule — clobbered by a sibling's write", id)
		}
	}
}

// panicLocker fails the test the instant anything calls WithLock on it —
// used to prove a path that never reaches the write (AddRule's lint-error
// early return) never acquires ws.Lock either. spec.Load itself takes a
// bare root string, not a workspace.Root, so the read path proper has no
// Root to carry a Lock at all — this covers the one spec-package call that
// DOES take a workspace.Root, per this task's "no lock in the read path"
// requirement.
type panicLocker struct{ t *testing.T }

func (l panicLocker) WithLock(name string, fn func() error) error {
	l.t.Fatalf("code path acquired lock %q that should never have reached a write", name)
	return nil
}

func TestAddRuleLintErrorNeverLocks(t *testing.T) {
	root := t.TempDir()
	ws := workspace.Root{Dir: root, Lock: panicLocker{t: t}}
	res, err := AddRule(ws, &Cascade{}, AuthorReq{
		Dir: "pkg", Stem: "PKG-API", Sentence: "do stuff", // invalid EARS
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Written {
		t.Fatal("lint-error AddRule reported Written")
	}
}
