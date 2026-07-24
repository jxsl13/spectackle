package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

func init() {
	// Silence logging during tests
	log.SetOutput(io.Discard)
}

// callBinPath is the spectackle binary built once by TestMain, shared by
// every test in this file that needs the `call` subcommand exercised as a
// real external process (so stdout/stderr separation, and the -root-less
// self-exec stdio spawn inside internal/mcpclient, are the genuine article
// rather than an in-process approximation). callBinBuildErr explains why
// callBinPath is empty, so tests can t.Skip with a clear reason instead of
// failing on a missing toolchain — same pattern as
// internal/mcpclient/client_test.go's binPath/binBuildErr.
var (
	callBinPath     string
	callBinBuildErr string
)

func TestMain(m *testing.M) {
	os.Exit(runCallBinTestMain(m))
}

func runCallBinTestMain(m *testing.M) int {
	dir, err := os.MkdirTemp("", "spectackle-cmd-test-*")
	if err != nil {
		callBinBuildErr = "creating temp dir for build output: " + err.Error()
		return m.Run()
	}
	defer os.RemoveAll(dir)

	if _, err := exec.LookPath("go"); err != nil {
		callBinBuildErr = "go toolchain not found on PATH: " + err.Error()
		return m.Run()
	}

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		callBinBuildErr = "runtime.Caller: could not determine this test file's path"
		return m.Run()
	}
	// cmd/spectackle/main_test.go -> repo root is two levels up. Do NOT
	// depend on a prebuilt /home/user path: build from source here.
	moduleRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")

	out := filepath.Join(dir, "spectackle")
	if runtime.GOOS == "windows" {
		out += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", out, "./cmd/spectackle")
	cmd.Dir = moduleRoot
	if output, err := cmd.CombinedOutput(); err != nil {
		callBinBuildErr = "go build ./cmd/spectackle: " + err.Error() + "\n" + string(output)
		return m.Run()
	}
	callBinPath = out

	return m.Run()
}

// requireCallBinary skips the test with a clear reason when the toolchain
// (or the build) wasn't available.
func requireCallBinary(t *testing.T) string {
	t.Helper()
	if callBinPath == "" {
		t.Skip("spectackle binary unavailable, skipping: " + callBinBuildErr)
	}
	return callBinPath
}

// runCall runs `<bin> call <args...>` to completion and returns its
// stdout/stderr, without asserting on the exit code (some callers expect
// non-zero, e.g. the refusal test).
func runCall(t *testing.T, bin string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	full := append([]string{"call"}, args...)
	cmd := exec.Command(bin, full...)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err = cmd.Run()
	return outBuf.String(), errBuf.String(), err
}

// waitForHTTPReady polls addr until a TCP connection succeeds, failing the
// test if timeout elapses first. Mirrors the readiness loop already used by
// TestServeNoPidfileFlag.
func waitForHTTPReady(t *testing.T, addr string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		conn, dialErr := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if dialErr == nil {
			_ = conn.Close()
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("server never became reachable at %s: %v", addr, dialErr)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestCallStdioSingle: `call state` against a temp workspace, over the
// default stdio transport (spawns a fresh server). stdout must start with
// the #version section (see internal/mcpserver/state.go); stderr must
// carry no tool text — only connection diagnostics belong there (CLI-001
// binds `serve` on stdio, not `call`, but a spawned child's stderr must
// still never land on our stdout, and our own diagnostics must stay off of
// it in the other direction too).
func TestCallStdioSingle(t *testing.T) {
	bin := requireCallBinary(t)
	root := t.TempDir()

	stdout, stderr, err := runCall(t, bin, "-root", root, "state")
	if err != nil {
		t.Fatalf("call state (stdio): %v\nstderr: %s", err, stderr)
	}
	if !strings.HasPrefix(stdout, "#version") {
		t.Fatalf("stdout does not start with the #version section:\n%s", stdout)
	}
	if strings.Contains(stderr, "#version") {
		t.Fatalf("stderr unexpectedly carries tool text:\n%s", stderr)
	}
}

// TestCallHTTPMatchesStdio: the SAME call (`call state`) issued once over
// stdio and once over -http against a resident server must render
// byte-identical stdout. SPECTACKLE_AGENT is pinned via t.Setenv (inherited
// by both child processes through cmd.Env defaulting to os.Environ()) so
// both fresh workspaces render the same agent name in the summary line —
// the same reason internal/mcpclient/client_test.go's
// TestDialHTTPMatchesStdio pins it.
func TestCallHTTPMatchesStdio(t *testing.T) {
	bin := requireCallBinary(t)
	t.Setenv("SPECTACKLE_AGENT", "call-cmd-byte-equality")

	stdout, stderr, err := runCall(t, bin, "-root", t.TempDir(), "state")
	if err != nil {
		t.Fatalf("call state (stdio): %v\nstderr: %s", err, stderr)
	}

	httpRoot := t.TempDir()
	addr := freeAddr(t)
	srv := exec.Command(bin, "serve", "-root", httpRoot, "-http", addr)
	var srvErr bytes.Buffer
	srv.Stderr = &srvErr
	if err := srv.Start(); err != nil {
		t.Fatalf("start resident server: %v", err)
	}
	t.Cleanup(func() {
		_ = srv.Process.Signal(syscall.SIGTERM)
		_ = srv.Wait()
	})
	waitForHTTPReady(t, addr, 2*time.Second)

	httpOut, httpErrOut, err := runCall(t, bin, "-http", addr, "state")
	if err != nil {
		t.Fatalf("call state (http): %v\nstderr: %s\nserver stderr: %s", err, httpErrOut, srvErr.String())
	}

	if !strings.HasPrefix(httpOut, "#version") {
		t.Fatalf("http stdout does not start with the #version section:\n%s", httpOut)
	}
	if stdout != httpOut {
		t.Fatalf("rendered output differs by transport (must be byte-identical):\nstdio (%d bytes): %q\nhttp  (%d bytes): %q",
			len(stdout), stdout, len(httpOut), httpOut)
	}
	t.Logf("byte-identical output across transports (%d bytes): %q", len(stdout), stdout)
}

// TestCallStdinMultiLine: two stdin lines, issued over ONE session (the
// whole point of the stdin batch mode — reconnecting per line would
// re-index the workspace on every call). Both tools' output must appear in
// stdout, in the order the lines were given.
func TestCallStdinMultiLine(t *testing.T) {
	bin := requireCallBinary(t)
	root := t.TempDir()

	cmd := exec.Command(bin, "call", "-root", root)
	cmd.Stdin = strings.NewReader("{\"name\": \"state\", \"arguments\": {}}\n{\"name\": \"swarm\", \"arguments\": {}}\n")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("call (stdin batch): %v\nstderr: %s", err, stderr.String())
	}

	out := stdout.String()
	stateIdx := strings.Index(out, "#version")
	swarmIdx := strings.Index(out, "ag ")
	if stateIdx != 0 {
		t.Fatalf("expected state's #version section at the very start of stdout:\n%s", out)
	}
	if swarmIdx < 0 {
		t.Fatalf("swarm's output (an \"ag \" line) missing from stdout:\n%s", out)
	}
	if swarmIdx < stateIdx {
		t.Fatalf("swarm's output came before state's, want line order preserved:\n%s", out)
	}
}

// TestCallRefusalExitsNonZero: `find` with no `q` fails the server's own
// input-schema validation and comes back flagged IsError. `call` must
// print the refusal text to stdout (a shell caller has nothing else to
// show) AND exit non-zero (so it can branch on the gate refusal without
// parsing prose).
func TestCallRefusalExitsNonZero(t *testing.T) {
	bin := requireCallBinary(t)
	root := t.TempDir()

	stdout, stderr, err := runCall(t, bin, "-root", root, "find", "{}")
	if err == nil {
		t.Fatalf("call find {} : expected a non-zero exit, got success\nstdout: %s\nstderr: %s", stdout, stderr)
	}
	if strings.TrimSpace(stdout) == "" {
		t.Fatalf("call find {} : expected the refusal text on stdout, got nothing\nstderr: %s", stderr)
	}
	t.Logf("refusal text on stdout: %q", stdout)
}

// TestCallInstructions: -instructions must print the server's instructions
// manifest (non-empty, carrying the RECORDS marker — the exact field an
// earlier hand-rolled wrapper in this project silently dropped) and exit 0.
func TestCallInstructions(t *testing.T) {
	bin := requireCallBinary(t)
	root := t.TempDir()

	stdout, stderr, err := runCall(t, bin, "-root", root, "-instructions")
	if err != nil {
		t.Fatalf("call -instructions: %v\nstderr: %s", err, stderr)
	}
	if strings.TrimSpace(stdout) == "" {
		t.Fatal("call -instructions: expected non-empty output")
	}
	if !strings.Contains(stdout, "RECORDS") {
		t.Fatalf("call -instructions: missing the RECORDS marker:\n%s", stdout)
	}
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
