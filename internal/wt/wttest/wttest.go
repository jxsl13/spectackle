// Package wttest builds throwaway git fixture repositories for tests, with
// the temp-directory removal guard B-01KYQA4WXEFAT needs baked in.
//
// It is a separate package from internal/wt only because it imports
// `testing`: importing testing from a production package registers testing's
// flags in every binary that links it, so the helper cannot live next to
// InitTestRepo.
//
// THE GUARD, and why it is mechanism-independent. The reported failure is a
// harness cleanup error, never an assertion:
//
//	TempDir RemoveAll cleanup: unlinkat /tmp/TestXxx/001/.git/objects: directory not empty
//
// os.RemoveAll only surfaces that ENOTEMPTY PathError when its recursive
// descent itself succeeded (GOROOT/src/os/removeall_at.go), so the failure
// requires something CONCURRENTLY creating entries in .git/objects while the
// harness unlinks it. wt.QuietMaintenance silences the one such writer we
// could name — git's detached auto-maintenance child — but the writer that
// actually produced the two CI failures was never identified, and a guard
// that only defends against the mechanism we happened to think of defends
// against nothing on the day a different one appears. Hence: retry the
// removal, and do it for every fixture, whatever is writing.
package wttest

import (
	"os"
	"testing"
	"time"

	"github.com/jxsl13/spectackle/internal/wt"
)

// How hard Dir tries before giving up and letting the harness report the
// failure itself. Deliberately bounded: a fixture that is still growing after
// this long is a bug worth seeing, not a race worth waiting out. The product
// is an upper bound on cleanup cost per fixture, paid only when the first
// removal actually fails.
const (
	removeAttempts = 20
	removeBackoff  = 25 * time.Millisecond
)

// Dir returns a temporary directory that is safe to run git inside: it is
// t.TempDir plus a bounded-retry removal registered as a cleanup.
//
// THE REGISTRATION ORDER IS LOAD-BEARING and is the whole trick. Verified
// against Go 1.26.5: testing registers exactly ONE cleanup, at the FIRST
// t.TempDir call of a test (testing.go: `if c.tempDirOnce.Do(...)`), and
// cleanups run LIFO. Our cleanup is therefore registered no earlier than
// testing's own, which means it runs BEFORE it — and it leaves nothing for
// testing.removeAll to trip over, because os.RemoveAll on a path that no
// longer exists returns nil.
//
// That ordering matters because testing's own removal is not retried on any
// platform we run CI on: testing.removeAll retries only errors for which
// isWindowsRetryable reports true, so on Linux an ENOTEMPTY comes straight
// back as `TempDir RemoveAll cleanup: %v` — verbatim the reported string.
// Retrying inside the harness is not available to us; retrying before it is.
//
// Use Dir directly for a fixture that needs a repository shape Repo does not
// build (a bare repo, a non-main default branch, a repo deliberately left
// without an identity). Such a fixture must still call wt.QuietMaintenance
// itself — TestEveryGitFixtureDisablesMaintenance enforces that.
func Dir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Cleanup(func() {
		n, err := removeWithRetry(dir, removeAttempts, removeBackoff, nil)
		if err != nil {
			// Not t.Error: the harness's own cleanup runs immediately after
			// this one and will report the surviving directory in its own
			// words. Failing here too would turn one flake into two, and the
			// useful new information is the attempt count — it says whether
			// something is still writing or the directory is simply
			// unremovable.
			t.Logf("wttest: %s survived %d removal attempts: %v", dir, n, err)
		}
	})
	return dir
}

// Repo returns a git repository with an initial commit on branch main, built
// under Dir so it carries both halves of B-01KYQA4WXEFAT's fix: no detached
// maintenance child (via wt.InitTestRepo -> wt.QuietMaintenance) and a
// retrying removal that does not depend on having identified every writer.
//
// It SKIPS rather than fails when git is unavailable, matching
// wt.InitTestRepo's established convention: a machine without git cannot run
// a git test, and that is not the test's fault.
func Repo(t *testing.T) string {
	t.Helper()
	dir := Dir(t)
	if err := wt.InitTestRepo(dir); err != nil {
		t.Skipf("git unavailable: %v", err)
	}
	return dir
}

// removeWithRetry deletes dir, retrying while the removal keeps failing, and
// reports how many attempts it took. afterFail, when non-nil, is called with
// the 1-based number of the attempt that just failed, before the wait — a
// seam the package's own test uses to make a transiently-unremovable
// directory removable again at a known attempt, so the retry can be pinned
// by attempt COUNT rather than by elapsed time (B-01KYQG88GZEM2 filed that
// flake class here twice).
func removeWithRetry(dir string, attempts int, wait time.Duration, afterFail func(attempt int)) (int, error) {
	var err error
	for n := 1; n <= attempts; n++ {
		if err = os.RemoveAll(dir); err == nil {
			return n, nil
		}
		if afterFail != nil {
			afterFail(n)
		}
		if n < attempts {
			time.Sleep(wait)
		}
	}
	return attempts, err
}
