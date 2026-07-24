package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func init() {
	// Silence logging during tests
	log.SetOutput(io.Discard)
}

func writeSpec(t *testing.T, root, content string) {
	t.Helper()
	p := filepath.Join(root, ".spectackle", "spec.md")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLintExitCodes(t *testing.T) {
	clean := t.TempDir()
	writeSpec(t, clean, "---\nschema: v0\n---\n## TST-ARC-001\nThe tool SHALL exit with code 0 on clean specs.\n")
	if code := lint([]string{clean}); code != 0 {
		t.Fatalf("clean tree: lint = %d, want 0", code)
	}

	dirty := t.TempDir()
	writeSpec(t, dirty, "---\nschema: v0\n---\n## TST-ARC-001\nThe tool should handle things appropriately.\n")
	if code := lint([]string{dirty}); code != 1 {
		t.Fatalf("dirty tree: lint = %d, want 1", code)
	}
}

func TestReindexExitCode(t *testing.T) {
	root := t.TempDir()
	writeSpec(t, root, "---\nschema: v0\n---\n")
	if code := reindex([]string{"-root", root}); code != 0 {
		t.Fatalf("reindex = %d, want 0", code)
	}
	// cache landed inside .spectackle/cache, never elsewhere in the workspace
	if _, err := os.Stat(filepath.Join(root, ".spectackle", "cache", "index.db")); err != nil {
		t.Fatalf("cache not created where expected: %v", err)
	}
}

func TestRunDispatch(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want int
	}{
		{name: "no args", args: nil, want: 2},
		{name: "version", args: []string{"version"}, want: 0},
		{name: "help", args: []string{"help"}, want: 0},
		{name: "-h", args: []string{"-h"}, want: 0},
		{name: "--help", args: []string{"--help"}, want: 0},
		{name: "bogus", args: []string{"bogus"}, want: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code := run(tt.args)
			if code != tt.want {
				t.Errorf("run(%v) = %d, want %d", tt.args, code, tt.want)
			}
		})
	}

	// Test lint with clean and dirty specs
	t.Run("lint clean", func(t *testing.T) {
		clean := t.TempDir()
		writeSpec(t, clean, "---\nschema: v0\n---\n## TST-ARC-001\nThe tool SHALL exit with code 0 on clean specs.\n")
		code := run([]string{"lint", clean})
		if code != 0 {
			t.Errorf("run([lint, cleanDir]) = %d, want 0", code)
		}
	})

	t.Run("lint dirty", func(t *testing.T) {
		dirty := t.TempDir()
		writeSpec(t, dirty, "---\nschema: v0\n---\n## TST-ARC-001\nThe tool should handle things appropriately.\n")
		code := run([]string{"lint", dirty})
		if code != 1 {
			t.Errorf("run([lint, dirtyDir]) = %d, want 1", code)
		}
	})
}

// freeAddr probes for a free localhost port by binding to :0 and releasing
// it immediately, then hands the address back to the caller. Racy in
// principle (something else could grab the port in the gap) but this is
// the same pattern already used by TestRunHTTPListenerGracefulShutdown.
func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatalf("close probe listener: %v", err)
	}
	return addr
}

// waitForFile polls for path to exist, failing the test if timeout elapses
// first.
func waitForFile(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s to appear", path)
}

// shutdownServe sends SIGTERM to the current process (the same signal
// serve()'s signal.NotifyContext(os.Interrupt, syscall.SIGTERM) listens
// for) and waits for the in-flight serve() call — running in a goroutine
// via codeCh — to return.
func shutdownServe(t *testing.T, codeCh <-chan int) int {
	t.Helper()
	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatalf("signal self with SIGTERM: %v", err)
	}
	select {
	case code := <-codeCh:
		return code
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not return within 5s of SIGTERM")
		return -1
	}
}

// TestServePidfileHTTPCreateAndRemove: -pidfile with -http writes the PID
// only once the listener is bound, and removes it on graceful shutdown.
func TestServePidfileHTTPCreateAndRemove(t *testing.T) {
	root := t.TempDir()
	pidPath := filepath.Join(t.TempDir(), "spectackle.pid")
	addr := freeAddr(t)

	codeCh := make(chan int, 1)
	go func() {
		codeCh <- serve([]string{"-root", root, "-http", addr, "-pidfile", pidPath})
	}()

	waitForFile(t, pidPath, 2*time.Second)

	data, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatalf("read pidfile: %v", err)
	}
	want := fmt.Sprintf("%d\n", os.Getpid())
	if string(data) != want {
		t.Fatalf("pidfile content = %q, want %q", data, want)
	}

	if code := shutdownServe(t, codeCh); code != 0 {
		t.Fatalf("serve() = %d, want 0", code)
	}

	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Fatalf("pidfile still present after shutdown (err=%v)", err)
	}
}

// TestServePidfilePreExisting: serve must refuse to clobber a pidfile that
// already exists, and must leave it untouched.
func TestServePidfilePreExisting(t *testing.T) {
	root := t.TempDir()
	pidPath := filepath.Join(t.TempDir(), "spectackle.pid")
	original := "not-a-real-pid\n"
	if err := os.WriteFile(pidPath, []byte(original), 0o644); err != nil {
		t.Fatalf("seed pidfile: %v", err)
	}

	addr := freeAddr(t)
	code := serve([]string{"-root", root, "-http", addr, "-pidfile", pidPath})
	if code == 0 {
		t.Fatalf("serve() = 0, want non-zero when pidfile already exists")
	}

	data, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatalf("read pidfile after failed serve: %v", err)
	}
	if string(data) != original {
		t.Fatalf("pre-existing pidfile was modified: got %q, want %q", data, original)
	}
}

// TestServePidfileUnwritablePath: a pidfile path whose parent directory is
// missing must fail the command cleanly, not panic.
func TestServePidfileUnwritablePath(t *testing.T) {
	root := t.TempDir()
	pidPath := filepath.Join(t.TempDir(), "no-such-parent-dir", "spectackle.pid")
	addr := freeAddr(t)

	code := serve([]string{"-root", root, "-http", addr, "-pidfile", pidPath})
	if code == 0 {
		t.Fatalf("serve() = 0, want non-zero for an unwritable pidfile path")
	}
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Fatalf("pidfile unexpectedly created at %s", pidPath)
	}
}

// TestServeNoPidfileFlag: omitting -pidfile must not create any file, and
// serve behaves exactly as it did before this flag existed.
func TestServeNoPidfileFlag(t *testing.T) {
	root := t.TempDir()
	// Nothing in the -pidfile code path has any reason to touch this
	// directory; if it gains a file, pidfile handling leaked despite the
	// flag being unset.
	watchDir := t.TempDir()
	addr := freeAddr(t)

	codeCh := make(chan int, 1)
	go func() {
		codeCh <- serve([]string{"-root", root, "-http", addr})
	}()

	// Wait for the server to actually accept connections (there is no
	// pidfile to poll on in this scenario).
	deadline := time.Now().Add(2 * time.Second)
	for {
		conn, dialErr := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if dialErr == nil {
			_ = conn.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("server never became reachable at %s: %v", addr, dialErr)
		}
		time.Sleep(20 * time.Millisecond)
	}

	entries, err := os.ReadDir(watchDir)
	if err != nil {
		t.Fatalf("ReadDir(%s): %v", watchDir, err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected no files created without -pidfile, found %v", entries)
	}

	if code := shutdownServe(t, codeCh); code != 0 {
		t.Fatalf("serve() = %d, want 0", code)
	}
}
