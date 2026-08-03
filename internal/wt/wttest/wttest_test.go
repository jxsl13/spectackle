package wttest

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// The retry is the half of B-01KYQA4WXEFAT that does not depend on having
// identified the concurrent writer, so it is the half that has to be proven
// rather than argued. Proven by ACTION — the directory is gone — and by
// ATTEMPT COUNT, never by elapsed time (B-01KYQG88GZEM2 filed the
// wall-clock-assertion flake class in this repository twice).
//
// The stand-in for "something else is writing in here" is a directory whose
// children cannot be unlinked because the directory itself is not writable;
// afterFail restores the permission bit at a known attempt, so the loop is
// forced through exactly two failures and one success. A single unretried
// os.RemoveAll — which is what the harness does, and what this package
// exists to get in front of — returns the error and leaves the tree.
func TestRemoveWithRetryOutlastsATransientlyUnremovableDir(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: the permission bit this fixture relies on is not enforced")
	}
	base := t.TempDir()
	dir := filepath.Join(base, "repo", "objects")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pack"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	lock := func(mode os.FileMode) {
		if err := os.Chmod(dir, mode); err != nil {
			t.Fatal(err)
		}
	}
	lock(0o555)
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	// sanity: without the retry this removal fails, which is the condition
	// the retry has to survive. If it ever stops failing the rest of this
	// test proves nothing, so assert it rather than assume it.
	if err := os.RemoveAll(filepath.Join(base, "repo")); err == nil {
		t.Skip("this filesystem removes children of a non-writable directory; the fixture cannot be made transiently unremovable")
	}

	var failed []int
	n, err := removeWithRetry(filepath.Join(base, "repo"), 8, time.Millisecond, func(attempt int) {
		failed = append(failed, attempt)
		if attempt == 2 {
			lock(0o755)
		}
	})
	if err != nil {
		t.Fatalf("removeWithRetry = %v after %d attempts; the guard must outlast a transient failure", err, n)
	}
	if n != 3 || len(failed) != 2 {
		t.Errorf("attempts = %d, failures = %v; want the third attempt to succeed after exactly two failures", n, failed)
	}
	if _, statErr := os.Stat(filepath.Join(base, "repo")); !os.IsNotExist(statErr) {
		t.Fatalf("directory survived a successful removeWithRetry: %v", statErr)
	}
}

// removeWithRetry must stay BOUNDED: a directory that is genuinely
// unremovable has to give up and hand the failure back, not spin. An
// unbounded guard would convert the reported flake into a hang, and a hang is
// strictly worse than the flake it replaces — CI reports it as a timeout with
// no attribution at all.
func TestRemoveWithRetryIsBounded(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: the permission bit this fixture relies on is not enforced")
	}
	base := t.TempDir()
	dir := filepath.Join(base, "repo", "objects")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pack"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	n, err := removeWithRetry(filepath.Join(base, "repo"), 3, time.Millisecond, nil)
	if err == nil {
		t.Skip("this filesystem removes children of a non-writable directory; nothing here is unremovable")
	}
	if n != 3 {
		t.Errorf("attempts = %d, want exactly the 3 it was budgeted", n)
	}
}

// Repo must hand back a usable repository, not just a directory: an initial
// commit on main, and the maintenance keys already off — the guard is
// worthless if the fixture it builds is not the fixture callers need.
func TestRepoIsAQuietRepositoryOnMain(t *testing.T) {
	root := Repo(t)
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"rev-parse", "--abbrev-ref", "HEAD"}, "main"},
		{[]string{"config", "--get", "maintenance.auto"}, "false"},
	} {
		out, err := exec.Command("git", append([]string{"-C", root}, tc.args...)...).Output()
		if err != nil {
			t.Fatalf("git %v: %v", tc.args, err)
		}
		if got := string(out); got != tc.want+"\n" {
			t.Errorf("git %v = %q, want %q", tc.args, got, tc.want)
		}
	}
}
