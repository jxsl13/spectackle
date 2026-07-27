package mcpclient_test

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jxsl13/spectackle/internal/mcpclient"
	"github.com/jxsl13/spectackle/internal/mcpserver"
)

// binPath is the spectackle binary built once by TestMain, shared by every
// test that needs a stdio-spawned server. binBuildErr explains why binPath
// is empty, so tests can t.Skip with a clear reason instead of failing on a
// missing toolchain.
var (
	binPath     string
	binBuildErr string
)

func TestMain(m *testing.M) {
	os.Exit(runTestMain(m))
}

func runTestMain(m *testing.M) int {
	dir, err := os.MkdirTemp("", "spectackle-mcpclient-test-*")
	if err != nil {
		binBuildErr = "creating temp dir for build output: " + err.Error()
		return m.Run()
	}
	defer os.RemoveAll(dir)

	if _, err := exec.LookPath("go"); err != nil {
		binBuildErr = "go toolchain not found on PATH: " + err.Error()
		return m.Run()
	}

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		binBuildErr = "runtime.Caller: could not determine this test file's path"
		return m.Run()
	}
	// internal/mcpclient/client_test.go -> repo root is two levels up. Do
	// NOT depend on a prebuilt /home/user path: build from source here.
	moduleRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")

	out := filepath.Join(dir, "spectackle")
	if runtime.GOOS == "windows" {
		out += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", out, ".")
	cmd.Dir = moduleRoot
	if output, err := cmd.CombinedOutput(); err != nil {
		binBuildErr = "go build .: " + err.Error() + "\n" + string(output)
		return m.Run()
	}
	binPath = out

	return m.Run()
}

// requireBinary skips the test with a clear reason when the toolchain (or
// the build) wasn't available, per the brief: never depend on a prebuilt
// /home/user path.
func requireBinary(t *testing.T) string {
	t.Helper()
	if binPath == "" {
		t.Skip("spectackle binary unavailable, skipping: " + binBuildErr)
	}
	return binPath
}

// dialStdio spawns a fresh server over stdio against root and returns a
// connected session. The context lives as long as the test (via
// t.Cleanup), matching cfg's contract that ctx governs the spawned
// process's lifetime.
func dialStdio(t *testing.T, bin, root string) *mcpclient.Session {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	sess, err := mcpclient.Dial(ctx, mcpclient.Config{Command: bin, Root: root})
	if err != nil {
		t.Fatalf("Dial(stdio): %v", err)
	}
	t.Cleanup(func() { _ = sess.Close() })
	return sess
}

// TestDialStdio: stdio round trip against a server spawned from the built
// binary. `state` on a fresh workspace must start with the #version
// section and carry the ok-spectackle summary line (see
// internal/mcpserver/state.go).
func TestDialStdio(t *testing.T) {
	bin := requireBinary(t)
	sess := dialStdio(t, bin, t.TempDir())

	out, err := sess.Call(context.Background(), mcpclient.Call{Name: "state"})
	if err != nil {
		t.Fatalf("Call(state): %v", err)
	}
	if !strings.HasPrefix(out, "#version") {
		t.Fatalf("state output does not start with the #version section:\n%s", out)
	}
	if !strings.Contains(out, "ok spectackle") {
		t.Fatalf("state output missing an \"ok spectackle\" line:\n%s", out)
	}
}

// TestDialHTTPMatchesStdio: http round trip against a server started on
// 127.0.0.1 with a free port obtained by listening on :0 (never a
// hardcoded port, since the suite may run in parallel with other work),
// then the load-bearing assertion: the SAME call over the two transports
// must render byte-identical output. state.go's summary line embeds only
// the version, the agent name and "main"/"wt:<item>" (never a filesystem
// path), so pinning SPECTACKLE_AGENT makes two independent fresh
// workspaces produce identical bytes.
func TestDialHTTPMatchesStdio(t *testing.T) {
	bin := requireBinary(t)
	t.Setenv("SPECTACKLE_AGENT", "byte-equality-agent")

	stdioSess := dialStdio(t, bin, t.TempDir())
	stdioOut, err := stdioSess.Call(context.Background(), mcpclient.Call{Name: "state"})
	if err != nil {
		t.Fatalf("Call(state) over stdio: %v", err)
	}

	// http side: an in-process server (same wiring as `spectackle serve
	// -http`, see main.go) reached over a real network
	// listener on a free port.
	srv, err := mcpserver.New(t.TempDir())
	if err != nil {
		t.Fatalf("mcpserver.New: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv.MCP() }, nil)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	httpSrv := &http.Server{Handler: handler}
	go func() { _ = httpSrv.Serve(ln) }()
	t.Cleanup(func() { _ = httpSrv.Close() })

	endpoint := "http://" + ln.Addr().String()
	httpSess, err := mcpclient.Dial(context.Background(), mcpclient.Config{Endpoint: endpoint})
	if err != nil {
		t.Fatalf("Dial(http): %v", err)
	}
	t.Cleanup(func() { _ = httpSess.Close() })

	httpOut, err := httpSess.Call(context.Background(), mcpclient.Call{Name: "state"})
	if err != nil {
		t.Fatalf("Call(state) over http: %v", err)
	}

	if !strings.HasPrefix(httpOut, "#version") || !strings.Contains(httpOut, "ok spectackle") {
		t.Fatalf("http state output missing expected shape:\n%s", httpOut)
	}

	// The #version banner may legitimately differ: the stdio side execs a
	// freshly built binary whose build info carries a VCS pseudo-version,
	// while the http side serves in-process from the test binary
	// (B-01KYJ66TWW made version resolution build-truthful). Transport
	// parity is about the RENDER, not about two different builds agreeing
	// on their version — normalize that one token before comparing.
	reVer := regexp.MustCompile(`ok spectackle \S+ `)
	normStdio := reVer.ReplaceAllString(stdioOut, "ok spectackle X ")
	normHTTP := reVer.ReplaceAllString(httpOut, "ok spectackle X ")
	if normStdio != normHTTP {
		t.Fatalf("rendered output differs by transport (must be byte-identical modulo the version token):\nstdio (%d bytes): %q\nhttp  (%d bytes): %q",
			len(stdioOut), stdioOut, len(httpOut), httpOut)
	}
	t.Logf("byte-identical output across transports modulo version (%d bytes): %q", len(normStdio), normStdio)
}

// TestInstructions: the server's initialize-handshake instructions manifest
// must survive the round trip non-empty and must carry the RECORDS marker
// — dropping this field is the exact failure the brief calls out from an
// earlier hand-rolled wrapper.
func TestInstructions(t *testing.T) {
	bin := requireBinary(t)
	sess := dialStdio(t, bin, t.TempDir())

	instr := sess.Instructions()
	if instr == "" {
		t.Fatal("Instructions() returned an empty string")
	}
	if !strings.Contains(instr, "RECORDS") {
		t.Fatalf("Instructions() missing the RECORDS marker:\n%s", instr)
	}
}

// TestCallIsErrorPath: `find` with no `q` fails the server's own input
// schema validation (the `Q` field has no `omitempty` json tag, so
// jsonschema-go marks it required) and comes back as a CallToolResult with
// IsError set. Call must surface both the rendered text AND a non-nil
// error, so a caller can distinguish a refusal from success without
// parsing prose.
func TestCallIsErrorPath(t *testing.T) {
	bin := requireBinary(t)
	sess := dialStdio(t, bin, t.TempDir())

	out, err := sess.Call(context.Background(), mcpclient.Call{Name: "find", Arguments: map[string]any{}})
	if err == nil {
		t.Fatalf("Call(find, no q): expected a non-nil error for a refused call, got nil (output: %q)", out)
	}
	if out == "" {
		t.Fatal("Call(find, no q): expected non-empty rendered text alongside the error")
	}
	t.Logf("refusal text: %q, error: %v", out, err)
}

// TestCloseIdempotentReleasesProcess: Close must be safe to call twice, and
// must actually release the spawned OS process (not just the in-process
// handle) — verified via /proc, since the Session API intentionally does
// not expose the child's pid.
func TestCloseIdempotentReleasesProcess(t *testing.T) {
	bin := requireBinary(t)
	root := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	sess, err := mcpclient.Dial(ctx, mcpclient.Config{Command: bin, Root: root})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}

	// Confirm the session is actually live before closing it.
	if _, err := sess.Call(context.Background(), mcpclient.Call{Name: "state"}); err != nil {
		t.Fatalf("Call before Close: %v", err)
	}

	pid, ok := findSpectackleChild(t, root)
	if !ok {
		t.Skip("could not locate the spawned process via /proc (non-Linux or restricted /proc); skipping process-release assertion")
	}

	if err := sess.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// Idempotent: a second Close must not panic or hang.
	closeDone := make(chan error, 1)
	go func() { closeDone <- sess.Close() }()
	select {
	case err := <-closeDone:
		t.Logf("second Close returned: %v (must not panic/hang; error is acceptable)", err)
	case <-time.After(5 * time.Second):
		t.Fatal("second Close() did not return within 5s")
	}

	if !waitProcessGone(pid, 5*time.Second) {
		t.Fatalf("spawned process (pid %d) still alive after Close", pid)
	}

	// The transport is torn down: a further call must fail, not hang.
	callDone := make(chan error, 1)
	go func() {
		_, err := sess.Call(context.Background(), mcpclient.Call{Name: "state"})
		callDone <- err
	}()
	select {
	case err := <-callDone:
		if err == nil {
			t.Fatal("Call after Close: expected an error, got nil")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Call after Close did not return within 5s")
	}
}

// findSpectackleChild scans /proc for a process whose cmdline names our
// unique per-test root directory, i.e. the child spawned by Dial for this
// test. Returns ok=false (not a failure) when /proc isn't usable, so the
// caller can skip instead of falsely failing on a non-Linux box.
func findSpectackleChild(t *testing.T, root string) (int, bool) {
	t.Helper()
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0, false
	}
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		data, err := os.ReadFile(filepath.Join("/proc", e.Name(), "cmdline"))
		if err != nil || len(data) == 0 {
			continue
		}
		args := strings.Split(strings.TrimRight(string(data), "\x00"), "\x00")
		joined := strings.Join(args, " ")
		if strings.Contains(joined, "serve") && strings.Contains(joined, root) {
			return pid, true
		}
	}
	return 0, false
}

// processAlive reports whether pid still exists, via the null signal
// (POSIX-portable liveness check; stdlib only, no new dependency).
func processAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

func waitProcessGone(pid int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return !processAlive(pid)
}

// TestRefusalErrorCarriesNoTransportPaths pins T-01KYE0: a tool refusal's
// error is a marker (ErrToolRefused), never a re-narration carrying the
// spawn command's absolute paths — those cost CLI-driven agents ~430 bytes
// per refusal, 68-78% of the first live judges' whole tool-output diet.
// Transport errors keep the full description; a refusal has its text.
func TestRefusalErrorCarriesNoTransportPaths(t *testing.T) {
	bin := requireBinary(t)
	sess := dialStdio(t, bin, t.TempDir())

	out, err := sess.Call(context.Background(), mcpclient.Call{Name: "find", Arguments: map[string]any{}})
	if err == nil {
		t.Fatal("expected a refusal error")
	}
	if !errors.Is(err, mcpclient.ErrToolRefused) {
		t.Fatalf("refusal not marked ErrToolRefused: %v", err)
	}
	if strings.Contains(err.Error(), "stdio spawn") || strings.Contains(err.Error(), os.TempDir()) || strings.Contains(err.Error(), bin) {
		t.Fatalf("refusal error leaks transport detail: %v", err)
	}
	if out == "" {
		t.Fatal("refusal text must still be rendered")
	}
}

// TestShapeLineFromLiveSchema pins B-01KYE69's renderer against the real
// server schema over stdio: required props starred and first, enum-shaped
// descriptions inlined, prose descriptions omitted — the line teaches
// vocabulary (a judge burned fourteen refusals because `choose` was never
// named anywhere it could see), not the manual.
func TestShapeLineFromLiveSchema(t *testing.T) {
	bin := requireBinary(t)
	sess := dialStdio(t, bin, t.TempDir())

	line, err := sess.ShapeLine(context.Background(), "decide")
	if err != nil {
		t.Fatalf("ShapeLine: %v", err)
	}
	if !strings.HasPrefix(line, "shape: decide {op*:ask|answer|ls") {
		t.Fatalf("required-first with enum inline lost: %q", line)
	}
	if !strings.Contains(line, "choose") {
		t.Fatalf("the vocabulary word the judge could not guess is missing: %q", line)
	}
	if strings.Contains(line, "ADR context") {
		t.Fatalf("prose description leaked into the shape line: %q", line)
	}
	if _, err := sess.ShapeLine(context.Background(), "no-such-tool"); err == nil {
		t.Fatal("unknown tool must error, not fabricate a shape")
	}
}

// Issue 178: a spawned server refusing at startup died with a bare
// "initialize: EOF" — the stderr diagnostic must reach the error now.
func TestDialSurfacesServerStderrOnStartupRefusal(t *testing.T) {
	bin := requireBinary(t)
	// a root whose config schema mismatches: serve refuses before the
	// handshake with the workspace error on stderr
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".spectackle"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".spectackle", "config.yaml"),
		[]byte("schema: v99\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err := mcpclient.Dial(ctx, mcpclient.Config{Command: bin, Root: root})
	if err == nil {
		t.Fatal("dial against a refusing server must fail")
	}
	if !strings.Contains(err.Error(), "server stderr:") || !strings.Contains(err.Error(), "schema") {
		t.Fatalf("the server's stderr diagnostic must reach the caller: %v", err)
	}
}
