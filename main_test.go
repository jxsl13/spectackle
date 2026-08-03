package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/jxsl13/spectackle/internal/wt"
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
	// main_test.go sits AT the module root since the main package moved
	// there (T-01KYJ7AJ7B). Do NOT depend on a prebuilt path: build here.
	moduleRoot := filepath.Dir(thisFile)

	out := filepath.Join(dir, "spectackle")
	if runtime.GOOS == "windows" {
		out += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", out, ".")
	cmd.Dir = moduleRoot
	if output, err := cmd.CombinedOutput(); err != nil {
		callBinBuildErr = "go build .: " + err.Error() + "\n" + string(output)
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
	writeSpec(t, clean, "---\nschema: v1\n---\n## TST-ARC-001\nThe tool SHALL exit with code 0 on clean specs.\n")
	if code := lint([]string{clean}); code != 0 {
		t.Fatalf("clean tree: lint = %d, want 0", code)
	}

	dirty := t.TempDir()
	writeSpec(t, dirty, "---\nschema: v1\n---\n## TST-ARC-001\nThe tool should handle things appropriately.\n")
	if code := lint([]string{dirty}); code != 1 {
		t.Fatalf("dirty tree: lint = %d, want 1", code)
	}
}

func TestReindexExitCode(t *testing.T) {
	root := t.TempDir()
	writeSpec(t, root, "---\nschema: v1\n---\n")
	if code := reindex([]string{"-root", root}); code != 0 {
		t.Fatalf("reindex = %d, want 0", code)
	}
	// cache landed inside .spectackle/cache, never elsewhere in the workspace
	if _, err := os.Stat(filepath.Join(root, ".spectackle", "cache", "index.db")); err != nil {
		t.Fatalf("cache not created where expected: %v", err)
	}
}

// TestReindexBuildsSymbolGraph is DEFECT 3's proof: `reindex` previously
// only resynced the spec/doc cache via internal/sync — the symbol graph
// drift depends on was built exclusively inside the MCP server, so this
// command's name and help text promised a rebuild it never performed. A
// workspace with at least one Go file (two functions, one calling the
// other, so a call edge exists too) must now produce non-zero node AND
// edge counts, logged in the same "files, nodes, edges" shape
// mcpserver.Server.reindex already used — the one place an operator can
// confirm reindex actually ran.
func TestReindexBuildsSymbolGraph(t *testing.T) {
	root := t.TempDir()
	writeSpec(t, root, "---\nschema: v1\n---\n")
	src := "package pkg\n\nfunc A() int {\n\treturn B()\n}\n\nfunc B() int {\n\treturn 1\n}\n"
	if err := os.WriteFile(filepath.Join(root, "f.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	var logBuf bytes.Buffer
	origOut := log.Writer()
	log.SetOutput(&logBuf)
	defer log.SetOutput(origOut)

	if code := reindex([]string{"-root", root}); code != 0 {
		t.Fatalf("reindex = %d, want 0: log=%s", code, logBuf.String())
	}

	out := logBuf.String()
	m := regexp.MustCompile(`(\d+) files, (\d+) nodes, (\d+) edges`).FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("reindex log output missing the files/nodes/edges counts: %q", out)
	}
	nodes, err := strconv.Atoi(m[2])
	if err != nil {
		t.Fatalf("parsing node count %q: %v", m[2], err)
	}
	edges, err := strconv.Atoi(m[3])
	if err != nil {
		t.Fatalf("parsing edge count %q: %v", m[3], err)
	}
	if nodes == 0 {
		t.Fatalf("reindex reported 0 nodes for a workspace with a Go file: %q", out)
	}
	if edges == 0 {
		t.Fatalf("reindex reported 0 edges for a workspace with a call between two functions: %q", out)
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
		writeSpec(t, clean, "---\nschema: v1\n---\n## TST-ARC-001\nThe tool SHALL exit with code 0 on clean specs.\n")
		code := run([]string{"lint", clean})
		if code != 0 {
			t.Errorf("run([lint, cleanDir]) = %d, want 0", code)
		}
	})

	t.Run("lint dirty", func(t *testing.T) {
		dirty := t.TempDir()
		writeSpec(t, dirty, "---\nschema: v1\n---\n## TST-ARC-001\nThe tool should handle things appropriately.\n")
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

// TestWritePIDFileAtomicAndClean pins B-01KYDR: the pidfile must carry its
// full content from the instant it is observable (the old create-then-write
// sequence left a window where the path existed with zero bytes — the exact
// 0.10s fast-fail TestServePidfileHTTPCreateAndRemove hit under suite
// load), the temp file used for the atomic publish must not survive, and
// the refuse-to-clobber failure path must leave a pre-existing file
// untouched and the directory free of leftovers.
func TestWritePIDFileAtomicAndClean(t *testing.T) {
	dir := t.TempDir()
	pidPath := filepath.Join(dir, "spectackle.pid")

	if err := writePIDFile(pidPath); err != nil {
		t.Fatalf("writePIDFile: %v", err)
	}
	data, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatalf("read pidfile: %v", err)
	}
	if want := fmt.Sprintf("%d\n", os.Getpid()); string(data) != want {
		t.Fatalf("pidfile content = %q, want %q", data, want)
	}
	assertOnlyPidfile(t, dir)

	// Second write against the same path: refused, original untouched,
	// still no temp leftovers.
	if err := writePIDFile(pidPath); err == nil {
		t.Fatal("writePIDFile over an existing pidfile: expected an error, got nil")
	}
	after, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatalf("re-read pidfile: %v", err)
	}
	if string(after) != string(data) {
		t.Fatalf("pre-existing pidfile modified by refused write: %q -> %q", data, after)
	}
	assertOnlyPidfile(t, dir)
}

// assertOnlyPidfile fails the test if dir holds anything besides
// spectackle.pid — i.e. a leaked temp file from the atomic publish.
func assertOnlyPidfile(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if e.Name() != "spectackle.pid" {
			t.Fatalf("unexpected leftover in pidfile dir: %s", e.Name())
		}
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

// TestCallExitCodesOnRefusal is B-01KYD4H: the README's headless contract says
// a refusal prints its text on stdout but exits non-zero — "script against the
// exit code, not the prose". Every refusal exited 0, so a scripted gate (or a
// batch, which is how this bit: a batched draft refusal vanished without
// failing anything) could not detect one. The server now marks refusals with
// IsError, which the call client already mapped to the exit code.
func TestCallExitCodesOnRefusal(t *testing.T) {
	bin := requireCallBinary(t)
	root := t.TempDir()

	code := func(err error) int {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return ee.ExitCode()
		}
		if err != nil {
			t.Fatalf("unexpected run error: %v", err)
		}
		return 0
	}

	// refusal families: the record prints on stdout AND the exit is non-zero
	for _, tc := range []struct{ name, args string }{
		{"unknown kind", "{\"kind\":\"epic\",\"title\":\"x\"}"},
		{"missing title", "{\"kind\":\"task\",\"title\":\"\"}"},
	} {
		stdout, _, err := runCall(t, bin, "-root", root, "draft", tc.args)
		if !strings.Contains(stdout, "! ARG E") {
			t.Fatalf("%s: refusal text missing from stdout: %q", tc.name, stdout)
		}
		if code(err) == 0 {
			t.Fatalf("%s: refusal exited 0 (B-01KYD4H): %q", tc.name, stdout)
		}
	}

	// success still exits 0 with the record on stdout
	stdout, _, err := runCall(t, bin, "-root", root, "draft", "{\"kind\":\"task\",\"title\":\"ok\"}")
	if code(err) != 0 || !strings.Contains(stdout, "i T-") {
		t.Fatalf("success path broken: code=%d out=%q", code(err), stdout)
	}

	// a lifecycle refusal follows the same contract
	id := strings.Fields(stdout)[1]
	stdout, _, err = runCall(t, bin, "-root", root, "move", "{\"id\":\""+id+"\",\"to\":\"rejected\"}")
	if code(err) == 0 || !strings.Contains(stdout, "note") {
		t.Fatalf("noteless reject: code=%d out=%q", code(err), stdout)
	}
}

// TestWatchStaleRebuildsAndTriggers pins the watcher half of T-01KYEH under
// the committed-only policy (ADR-01KYF5): a moved (or unstamped) HEAD
// rebuilds from a clean snapshot, atomically replaces the executable path,
// hands it to the exec step, and cancels the serve context; a non-git root
// idles loudly instead of dying or thrashing.
func TestWatchStaleRebuildsAndTriggers(t *testing.T) {
	if testing.Short() {
		t.Skip("runs a real go build: skipped in -short")
	}
	repoRoot, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(t.TempDir(), "spx")
	if err := os.WriteFile(exe, []byte("old image"), 0o755); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	restartTo := make(chan string, 1)
	stopped := make(chan struct{})
	go watchStale(ctx, repoRoot, exe,
		10*time.Millisecond, restartTo, func() { close(stopped) }, nil)

	select {
	case got := <-restartTo:
		if got != exe {
			t.Fatalf("restart path = %q, want %q", got, exe)
		}
	case <-time.After(120 * time.Second):
		t.Fatal("watcher never triggered")
	}
	<-stopped
	if info, err := os.Stat(exe); err != nil || info.Size() < 1<<20 {
		t.Fatalf("executable not replaced by a real build: %v size=%d", err, info.Size())
	}
	if _, err := os.Stat(exe + ".new"); !os.IsNotExist(err) {
		t.Fatal("temp build artifact left behind")
	}

	// Idle half: a non-git root has no resolvable HEAD — committed-only
	// means the watcher idles loudly instead of rebuilding, and cancel
	// still shuts it down cleanly.
	ctx2, cancel2 := context.WithCancel(context.Background())
	restartTo2 := make(chan string, 1)
	go watchStale(ctx2, t.TempDir(), filepath.Join(t.TempDir(), "x"),
		10*time.Millisecond, restartTo2, func() { t.Error("non-git root must not trigger a restart") }, nil)
	select {
	case <-restartTo2:
		t.Fatal("failing rebuild produced a restart path")
	case <-time.After(300 * time.Millisecond):
	}
	cancel2()
}

// TestServeSelfRestartExecSwap is the end-to-end (non-short): a spawned
// hand-built resident server with -self-restart converges onto the stamped
// committed-only lineage in exactly one swap — same PID (exec preserves
// it), same port, pidfile intact, the log names the swap — and afterwards
// working-tree edits must NOT trigger further swaps (the B-01KYES20V
// edit-churn tripwire).
func TestServeSelfRestartExecSwap(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a server and runs a real go build: skipped in -short")
	}
	srcRoot, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	// The served repo is a FIXTURE whose HEAD is the current working tree:
	// the watcher rebuilds from HEAD, and asserting committed-only
	// semantics against a HEAD that predates the feature would test the
	// old code (the bootstrap chicken-and-egg this fixture removes).
	repoRoot := filepath.Join(t.TempDir(), "fixture")
	if out, err := exec.Command("cp", "-R", srcRoot, repoRoot).CombinedOutput(); err != nil {
		t.Fatalf("fixture copy: %v: %s", err, out)
	}
	for _, junk := range []string{".git", ".spectackle/cache", ".spectackle/wt"} {
		_ = os.RemoveAll(filepath.Join(repoRoot, junk))
	}
	fixtureGit := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = repoRoot
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=fixture", "GIT_AUTHOR_EMAIL=f@f",
			"GIT_COMMITTER_NAME=fixture", "GIT_COMMITTER_EMAIL=f@f")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("fixture git %v: %v %s", args, err, out)
		}
	}
	fixtureGit("init", "-q", "-b", "main")
	// Between init and the commit: the commit is what spawns git's detached
	// `maintenance run --auto` child, and this fixture is a full copy of the
	// source tree living under a t.TempDir the harness unlinks the moment the
	// test returns (B-01KYQA4WXEFAT). It is also the largest fixture in the
	// suite, so it is the one most likely to give auto-gc real work to do.
	if err := wt.QuietMaintenance(repoRoot); err != nil {
		t.Fatalf("QuietMaintenance: %v", err)
	}
	fixtureGit("add", "-A")
	fixtureGit("commit", "-q", "-m", "fixture: working tree as HEAD")

	bin := filepath.Join(t.TempDir(), "spx")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Dir = srcRoot
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v: %s", err, out)
	}

	addr := freeAddr(t)
	pidPath := filepath.Join(t.TempDir(), "spx.pid")
	logPath := filepath.Join(t.TempDir(), "serve.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	srv := exec.Command(bin, "serve", "-root", repoRoot, "-http", addr,
		"-pidfile", pidPath, "-self-restart", "-self-restart-every", "500ms")
	srv.Stderr = logFile
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Process.Kill(); _, _ = srv.Process.Wait() })
	waitForFile(t, pidPath, 30*time.Second)

	// No trigger needed: the hand-built binary carries buildHead="" and the
	// committed-only watcher converges onto the stamped lineage at the
	// first tick — that swap IS the test. A working-tree mtime touch must
	// no longer matter at all.

	deadline := time.Now().Add(180 * time.Second)
	for time.Now().Before(deadline) {
		raw, _ := os.ReadFile(logPath)
		if strings.Contains(string(raw), "exec-replacing with the clean-snapshot rebuild") {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	raw, _ := os.ReadFile(logPath)
	if !strings.Contains(string(raw), "exec-replacing with the clean-snapshot rebuild") {
		t.Fatalf("swap never logged:\n%s", raw)
	}

	// The replacement re-binds and re-creates the pidfile with the SAME pid.
	waitForFile(t, pidPath, 30*time.Second)
	pidRaw, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatal(err)
	}
	if want := fmt.Sprintf("%d\n", srv.Process.Pid); string(pidRaw) != want {
		t.Fatalf("pid changed across exec: got %q want %q", pidRaw, want)
	}
	// And it serves: the second serving-over-http line proves the new image.
	if got := strings.Count(string(raw)+"", "serving over http"); got < 1 {
		t.Fatalf("no serving line after swap:\n%s", raw)
	}
	deadline = time.Now().Add(30 * time.Second)
	served2 := false
	for time.Now().Before(deadline) {
		raw, _ = os.ReadFile(logPath)
		if strings.Count(string(raw), "serving over http") >= 2 {
			served2 = true
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	if !served2 {
		t.Fatalf("replacement never announced serving:\n%s", raw)
	}
	// The edit-churn tripwire: with gen2 stamped on HEAD, a working-tree
	// mtime touch and an untracked file must trigger NO further swap —
	// committed-only means edits are structurally invisible to the watcher.
	now := time.Now().Add(2 * time.Second)
	_ = os.Chtimes(filepath.Join(repoRoot, "cmd", "spectackle", "main.go"), now, now)
	churn := filepath.Join(repoRoot, "selfrestart_churn_probe.txt")
	if err := os.WriteFile(churn, []byte("uncommitted\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(churn) })
	time.Sleep(2 * time.Second) // four 500ms ticks
	raw, _ = os.ReadFile(logPath)
	if got := strings.Count(string(raw), "serving over http"); got != 2 {
		t.Fatalf("working-tree edits changed the serving generation (%d announcements, want 2) under committed-only:\n%s", got, raw)
	}
}

// The committed-only rebuild policy (ADR-01KYF5, closing B-01KYES20V and
// B-01KYES20Y): the rebuild decision keys on commits alone, and the build
// snapshot structurally excludes the dirty working tree.
func TestNeedsRebuildCommitBased(t *testing.T) {
	if needsRebuild("abc", "abc") {
		t.Fatal("same commit must not rebuild")
	}
	if !needsRebuild("abc", "def") {
		t.Fatal("moved HEAD must rebuild")
	}
	if !needsRebuild("", "def") {
		t.Fatal("hand-built first generation must converge once")
	}
	if needsRebuild("abc", "") {
		t.Fatal("unresolvable HEAD must never rebuild")
	}
}

func TestSnapshotHeadExcludesDirtyTree(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	run("init", "-q")
	// Committed into below, in a t.TempDir (B-01KYQA4WXEFAT).
	if err := wt.QuietMaintenance(dir); err != nil {
		t.Fatalf("QuietMaintenance: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "committed.txt"), []byte("in\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "-A")
	run("commit", "-q", "-m", "seed")
	if err := os.WriteFile(filepath.Join(dir, "dirty.txt"), []byte("out\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "committed.txt"), []byte("dirty edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	snap, err := snapshotHead(context.Background(), dir, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(snap)
	if _, err := os.Stat(filepath.Join(snap, "dirty.txt")); err == nil {
		t.Fatal("untracked dirty file leaked into the build snapshot")
	}
	data, err := os.ReadFile(filepath.Join(snap, "committed.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "in\n" {
		t.Fatalf("snapshot carries the dirty edit, not HEAD: %q", data)
	}
}

// The busy guard (B-01KYF7): while a call is in flight the watcher must
// not swap — the long edge that moved HEAD completes on the old
// generation, and the swap follows once quiet.
func TestWatchStaleDefersWhileBusy(t *testing.T) {
	if testing.Short() {
		t.Skip("runs a real go build: skipped in -short")
	}
	repoRoot, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(t.TempDir(), "spx")
	if err := os.WriteFile(exe, []byte("old image"), 0o755); err != nil {
		t.Fatal(err)
	}
	var busy atomic.Bool
	busy.Store(true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	restartTo := make(chan string, 1)
	go watchStale(ctx, repoRoot, exe,
		10*time.Millisecond, restartTo, func() {}, busy.Load)
	select {
	case <-restartTo:
		t.Fatal("swap fired while busy")
	case <-time.After(2 * time.Second):
	}
	busy.Store(false)
	select {
	case <-restartTo:
	case <-time.After(120 * time.Second):
		t.Fatal("swap never followed once quiet")
	}
}
