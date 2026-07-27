// Command spectackle is the spec-driven MCP server for cross-language
// codebases. Subcommands:
//
//	spectackle serve [-root DIR] [-http ADDR] [-pidfile PATH]
//	                                  run the MCP server on stdio, or over
//	                                  Streamable HTTP when -http is set
//	                                  (workspace auto-detected); with
//	                                  -pidfile, write the PID once the
//	                                  server is ready and remove it on
//	                                  shutdown
//	spectackle call [-root DIR] [-http ADDR] [-instructions] [NAME [JSON]]
//	                                  make a headless tool call: spawns a
//	                                  server over stdio, or reaches a
//	                                  resident one via -http; with NAME,
//	                                  issues that one call (JSON is its
//	                                  arguments object); with none, reads
//	                                  one {"name":...,"arguments":...} JSON
//	                                  object per non-empty stdin line and
//	                                  issues them over a single session;
//	                                  -instructions prints the server's
//	                                  instructions manifest and exits
//	spectackle lint  [PATH]        lint all EARS spec bundles, exit 1 on errors
//	spectackle reindex [-root DIR] rebuild the symbol graph and resync the
//	                                  spec/doc cache; prints files/nodes/edges
//	                                  so an operator can confirm it ran
//	spectackle version             print the version
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jxsl13/spectackle/internal/bench"
	"github.com/jxsl13/spectackle/internal/cache"
	"github.com/jxsl13/spectackle/internal/ears"
	"github.com/jxsl13/spectackle/internal/journal"
	"github.com/jxsl13/spectackle/internal/mcpclient"
	"github.com/jxsl13/spectackle/internal/mcpserver"
	"github.com/jxsl13/spectackle/internal/relnotes"
	"github.com/jxsl13/spectackle/internal/spec"
	"github.com/jxsl13/spectackle/internal/store"
	syncpkg "github.com/jxsl13/spectackle/internal/sync"
	"github.com/jxsl13/spectackle/internal/workspace"
	"github.com/jxsl13/spectackle/internal/wt"
)

// httpShutdownTimeout bounds how long runHTTPListener waits for in-flight
// requests to drain once shutdown is requested (via ctx.Done()).
const httpShutdownTimeout = 5 * time.Second

func main() {
	log.SetFlags(0)
	log.SetOutput(os.Stderr) // stdout is reserved for JSON-RPC (SPX-ARC-001)
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		usage()
		return 2
	}
	switch args[0] {
	case "serve":
		return serve(args[1:])
	case "call":
		return call(args[1:])
	case "lint":
		return lint(args[1:])
	case "verify":
		return verifyCmd(args[1:])
	case "hook":
		return hookCmd(args[1:])
	case "releasenotes":
		return releaseNotesCmd(args[1:])
	case "reindex":
		return reindex(args[1:])
	case "bench":
		return benchCmd(args[1:])
	case "manifest":
		fmt.Println(mcpserver.Manifest())
		return 0
	case "version":
		fmt.Println("spectackle " + mcpserver.ResolvedVersion())
		return 0
	case "-h", "--help", "help":
		usage()
		return 0
	default:
		log.Printf("unknown subcommand %q", args[0])
		usage()
		return 2
	}
}

func usage() {
	log.Print(`usage:
  spectackle serve [-root DIR] [-http ADDR] [-pidfile PATH]
                                  run the MCP server on stdio, or over
                                  Streamable HTTP when -http is set
                                  (workspace auto-detected); with -pidfile,
                                  write the PID once the server is ready
                                  and remove it on shutdown
  spectackle call [-root DIR] [-http ADDR] [-instructions] [NAME [JSON]]
                                  make a headless tool call: spawns a server
                                  over stdio, or reaches a resident one via
                                  -http; with NAME, issues that one call
                                  (JSON is its arguments object); with none,
                                  reads one {"name":...,"arguments":...}
                                  JSON object per non-empty stdin line and
                                  issues them over a single session;
                                  -instructions prints the server's
                                  instructions manifest and exits
  spectackle lint  [PATH]        lint all EARS spec bundles, exit 1 on errors
  spectackle reindex [-root DIR] rebuild the symbol graph and resync the
                                  spec/doc cache; prints files/nodes/edges
                                  so an operator can confirm it ran
  spectackle version             print the version`)
}

func rootFlag(name string, args []string) string {
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	root := fs.String("root", ".", "workspace detection start / fallback root")
	_ = fs.Parse(args)
	return *root
}

func serve(args []string) int {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	root := fs.String("root", ".", "workspace detection start / fallback root")
	httpAddr := fs.String("http", "", "serve over Streamable HTTP on this address (e.g. 127.0.0.1:7331) instead of stdio")
	pidfile := fs.String("pidfile", "", "write the process PID here once the server is ready to accept requests; removed on shutdown (fails if the file already exists)")
	selfRestart := fs.Bool("self-restart", false, "HTTP mode only: when git HEAD of the served tree moves past the commit this binary was built from, rebuild from a clean snapshot of HEAD and exec-replace (committed-only, ADR-01KYF5; the dirty working tree never serves)")
	selfRestartEvery := fs.Duration("self-restart-every", 30*time.Second, "staleness poll cadence for -self-restart")
	_ = fs.Parse(args)

	s, err := mcpserver.New(*root)
	if err != nil {
		log.Printf("serve: %v", err)
		return 1
	}
	defer s.Close()

	if *httpAddr == "" {
		if *selfRestart {
			// A stdio peer holds the pipe; an exec swap mid-session would
			// sever a live JSON-RPC conversation. The staleness HINT
			// (MCP-010) still fires on stdio — the restart stays manual
			// there by design.
			log.Printf("serve: -self-restart is HTTP-only; stdio keeps the stale hint and a manual restart")
		}
		if *pidfile != "" {
			if err := writePIDFile(*pidfile); err != nil {
				log.Printf("serve: %v", err)
				return 1
			}
			defer removePIDFile(*pidfile)
		}
		log.Printf("spectackle %s serving over stdio", mcpserver.ResolvedVersion())
		if err := s.MCP().Run(context.Background(), &mcp.StdioTransport{}); err != nil {
			log.Printf("serve: %v", err)
			return 1
		}
		return 0
	}

	// v0 limitation: a single shared *mcp.Server instance backs every HTTP
	// session, so graph/cache/worktree lifecycle state is shared across all
	// connected clients (see docs/architecture.md §8).
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return s.MCP()
	}, nil)
	log.Printf("spectackle %s serving over http on %s", mcpserver.ResolvedVersion(), *httpAddr)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	restartTo := make(chan string, 1)
	if *selfRestart {
		exe, err := os.Executable()
		if err != nil {
			log.Printf("serve: -self-restart disabled, cannot resolve own executable: %v", err)
		} else if s.SetSelfRestart(); !s.SelfRestartEligible() {
			log.Printf("serve: -self-restart idle: this binary is not a dev build serving its own module (the eligibility guard — foreign trees must never be built or exec'd)")
		} else {
			go watchStale(ctx, s.ServedDir(), exe, *selfRestartEvery, restartTo, stop, s.Busy)
		}
	}
	httpErr := runHTTP(ctx, *httpAddr, handler, *pidfile)
	select {
	case exe := <-restartTo:
		// The exec proceeds even when the drain timed out (httpErr from a
		// shutdown deadline — a hanging streamable-HTTP GET holds its
		// session stream for as long as the session lives): the on-disk
		// binary is already replaced and the pidfile is gone, so bailing
		// out here would leave NO resident server at all, the one outcome
		// strictly worse than severing the laggard streams. runHTTP's
		// deferred removePIDFile has run either way; the replacement
		// re-creates the pidfile through the atomic create-remove contract
		// (B-01KYDR). Exec keeps the PID and argv, and carries the swarm
		// identity explicitly — defers never run across exec, so Deregister
		// is skipped, and a replacement minting a fresh random agent would
		// ghost-block its own pre-swap leases and orphan its worktrees.
		if httpErr != nil {
			log.Printf("serve: drain incomplete (%v) — exec-replacing anyway, laggard streams are severed", httpErr)
		}
		log.Printf("serve: exec-replacing into the rebuilt image %s", exe)
		// Replace, never append: a pre-existing SPECTACKLE_AGENT entry —
		// even an empty one — precedes the appended value in the environ
		// list, and os.Getenv returns the FIRST match, so the replacement
		// would re-generate a random identity despite the carry-over.
		env := make([]string, 0, len(os.Environ())+1)
		for _, kv := range os.Environ() {
			if !strings.HasPrefix(kv, "SPECTACKLE_AGENT=") {
				env = append(env, kv)
			}
		}
		env = append(env, "SPECTACKLE_AGENT="+s.AgentName())
		if err := syscall.Exec(exe, os.Args, env); err != nil {
			log.Printf("serve: exec replacement failed: %v", err)
			return 1
		}
		return 0 // unreachable: exec does not return on success
	default:
	}
	if httpErr != nil {
		log.Printf("serve: %v", httpErr)
		return 1
	}
	log.Printf("serve: shutdown complete")
	return 0
}

// watchStale polls stale() and, once it reports true, rebuilds the binary
// beside exe and triggers the graceful drain: the new image is renamed
// over the executable path (atomic; the running image keeps its inode),
// the path is handed to serve's exec step, and stop() cancels the serve
// context. A failing rebuild is LOUD and non-fatal — the old binary keeps
// serving and the watcher keeps trying, because a silently dead
// auto-restart is exactly the stale-binary trap this exists to close
// (T-01KYEH). buildRoot is the SERVED tree (Server.ServedDir), never the
// resolved main root: for a server inside a linked git worktree the main
// root is the primary checkout, whose sources may lack exactly the changes
// that made the binary stale — the first live swap rebuilt there and
// exec'd a replacement that did not know its own flags.
// buildHead is the commit this binary was built from, stamped by
// watchStale's own rebuilds via -ldflags. The hand-built first generation
// carries "" and swaps exactly once at the first tick, converging onto the
// stamped lineage.
var buildHead string

// gitHead answers the current HEAD commit of dir, "" when unresolvable.
func gitHead(ctx context.Context, dir string) string {
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// needsRebuild is the whole rebuild decision: HEAD resolvable and different
// from the commit the serving binary carries. Working-tree edits can NEVER
// trigger a swap — that is the committed-only policy (ADR-01KYF5): the 103
// edit-churn swaps in one session (B-01KYES20V) and the dirty-tree serving
// hazard (B-01KYES20Y) both close on this one predicate.
func needsRebuild(built, head string) bool {
	return head != "" && head != built
}

// snapshotHead extracts a CLEAN checkout of HEAD into a temp dir via git
// archive — the dirty working tree is structurally excluded from the build.
func snapshotHead(ctx context.Context, repoDir, head string) (string, error) {
	snap, err := os.MkdirTemp("", "spx-selfbuild-")
	if err != nil {
		return "", err
	}
	tarPath := filepath.Join(snap, "head.tar")
	// The RESOLVED hash, not the literal ref: a commit landing between the
	// rev-parse and the archive would stamp the binary with one commit and
	// fill it with another — self-healing but a wasted extra swap.
	if out, err := exec.CommandContext(ctx, "git", "-C", repoDir, "archive", "--format=tar", "-o", tarPath, head).CombinedOutput(); err != nil {
		os.RemoveAll(snap)
		return "", fmt.Errorf("git archive: %v: %s", err, strings.TrimSpace(string(out)))
	}
	if out, err := exec.CommandContext(ctx, "tar", "-x", "-f", tarPath, "-C", snap).CombinedOutput(); err != nil {
		os.RemoveAll(snap)
		return "", fmt.Errorf("untar: %v: %s", err, strings.TrimSpace(string(out)))
	}
	_ = os.Remove(tarPath)
	return snap, nil
}

// watchStale polls git HEAD of the served tree and, when the serving binary
// was built from an older commit, rebuilds from a CLEAN snapshot of HEAD
// and exec-replaces the process (T-01KYEH, amended by ADR-01KYF5:
// committed-only). It never reads working-tree mtimes and never builds the
// working tree: uncommitted code cannot reach the serving binary, and edit
// churn cannot thrash the swap loop.
func watchStale(ctx context.Context, repoDir, exe string, every time.Duration, restartTo chan<- string, stop func(), busy func() bool) {
	t := time.NewTicker(every)
	defer t.Stop()
	warnedNoGit := false
	loggedFailHead := ""
	loggedBusyHead := ""
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		head := gitHead(ctx, repoDir)
		if head == "" {
			if !warnedNoGit {
				log.Printf("serve: self-restart idle: %s has no resolvable HEAD (committed-only policy needs a git repo)", repoDir)
				warnedNoGit = true
			}
			continue
		}
		if !needsRebuild(buildHead, head) {
			continue
		}
		// Defer the swap while a call is in flight: HEAD staleness is never
		// urgent, and an exec mid-edge severed the archive that had itself
		// moved HEAD — twice, live (B-01KYF7). Retry next tick; say so once
		// per stale head, so an hour of deferral under load is
		// distinguishable from a dead watcher.
		if busy != nil && busy() {
			if loggedBusyHead != head {
				loggedBusyHead = head
				log.Printf("serve: self-restart deferring the swap to %.12s while calls are in flight (retrying each tick)", head)
			}
			continue
		}
		snap, err := snapshotHead(ctx, repoDir, head)
		if err != nil {
			if loggedFailHead != head {
				loggedFailHead = head
				log.Printf("serve: self-restart snapshot FAILED, still serving the old binary (retrying quietly until HEAD moves): %v", err)
			}
			continue
		}
		tmp := exe + ".new"
		// Context-bound: a stop signal racing an in-flight build must WIN —
		// an unbounded build that completes during the drain would fill the
		// channel and resurrect the process a supervisor just tried to stop.
		cmd := exec.CommandContext(ctx, "go", "build",
			"-ldflags", "-X main.buildHead="+head, "-o", tmp, ".")
		cmd.Dir = snap
		out, err := cmd.CombinedOutput()
		os.RemoveAll(snap)
		if err != nil {
			if loggedFailHead != head {
				loggedFailHead = head
				log.Printf("serve: self-restart rebuild FAILED, still serving the old binary (retrying quietly until HEAD moves): %v: %s", err, strings.TrimSpace(string(out)))
			}
			continue
		}
		if ctx.Err() != nil {
			return // shutdown won the race; leave the .new artifact behind, loudly absent from restartTo
		}
		// Re-check at the swap point: a call may have started during the
		// build. The built image is discarded, not renamed — next tick
		// rebuilds cheaply on the warm cache. STATED RESIDUAL: a call whose
		// counter increment lands in the sub-millisecond window between
		// this check and the exec is not deferred; the 5s drain covers
		// every short call in that window, so only a >5s call starting
		// inside it can still be severed — narrowed by orders of
		// magnitude, not eliminated.
		if busy != nil && busy() {
			_ = os.Remove(tmp)
			continue
		}
		if err := os.Rename(tmp, exe); err != nil {
			log.Printf("serve: self-restart: replace %s: %v", exe, err)
			continue
		}
		log.Printf("serve: HEAD moved to %.12s — exec-replacing with the clean-snapshot rebuild", head)
		restartTo <- exe
		stop()
		return
	}
}

// runHTTP listens on addr and serves handler until ctx is cancelled, then
// gracefully shuts the server down. If pidfile is non-empty, the PID is
// written only after the listener is successfully bound (never before — a
// pidfile that exists while the port is still coming up invites a stop
// command that races startup) and removed once the server has shut down.
// See runHTTPListener for the shutdown semantics.
func runHTTP(ctx context.Context, addr string, handler http.Handler, pidfile string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	if pidfile != "" {
		if err := writePIDFile(pidfile); err != nil {
			_ = ln.Close()
			return err
		}
		defer removePIDFile(pidfile)
	}
	return runHTTPListener(ctx, ln, handler)
}

// writePIDFile creates path containing the current process's PID (decimal,
// newline-terminated) with mode 0o644. It refuses to overwrite an existing
// file: a pre-existing pidfile usually means a live server already claims
// it, and clobbering it would strand that process with no stoppable handle.
//
// The write is atomic with respect to appearance (B-01KYDR): the PID goes
// into a temp file in the same directory first, then os.Link publishes it
// under path. A create-then-write sequence leaves a window where path
// exists with zero bytes, and any watcher acting on file appearance — a
// shell `cat`, or waitForFile in the tests — reads an empty pidfile. Link
// also keeps the refuse-to-clobber contract: it fails with EEXIST and
// leaves the pre-existing file untouched.
func writePIDFile(path string) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp*")
	if err != nil {
		return fmt.Errorf("pidfile %s: %w", path, err)
	}
	defer os.Remove(tmp.Name())
	_, writeErr := fmt.Fprintf(tmp, "%d\n", os.Getpid())
	closeErr := tmp.Close()
	if writeErr != nil {
		return fmt.Errorf("pidfile %s: %w", path, writeErr)
	}
	if closeErr != nil {
		return fmt.Errorf("pidfile %s: %w", path, closeErr)
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return fmt.Errorf("pidfile %s: %w", path, err)
	}
	if err := os.Link(tmp.Name(), path); err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("pidfile %s already exists (a server may already be running)", path)
		}
		return fmt.Errorf("pidfile %s: %w", path, err)
	}
	return nil
}

// removePIDFile removes a pidfile written by writePIDFile. It is a no-op if
// the file is already gone, and only logs (to stderr, per CLI-001) on any
// other removal error rather than changing the caller's exit code — pidfile
// cleanup failing should not mask an otherwise-successful shutdown.
func removePIDFile(path string) {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Printf("serve: failed to remove pidfile %s: %v", path, err)
	}
}

// runHTTPListener serves handler on ln until ctx is cancelled (e.g. by a
// SIGINT/SIGTERM-derived context from signal.NotifyContext), at which point
// it calls http.Server.Shutdown with a bounded timeout so in-flight requests
// can drain before returning. It blocks until the server has fully stopped.
//
// http.ErrServerClosed - the expected error once Shutdown/Close has been
// called - is mapped to nil; any other error from serving or shutting down
// is returned.
func runHTTPListener(ctx context.Context, ln net.Listener, handler http.Handler) error {
	srv := &http.Server{Handler: handler}

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- srv.Serve(ln)
	}()

	select {
	case err := <-serveErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), httpShutdownTimeout)
		defer cancel()
		shutdownErr := srv.Shutdown(shutdownCtx)

		// Wait for the Serve goroutine to actually return so callers can
		// rely on runHTTPListener returning only once the listener/socket
		// is fully released.
		err := <-serveErr
		if shutdownErr != nil {
			return shutdownErr
		}
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}

// call implements the `call` subcommand (CLI-002): a headless tool call
// against a spectackle server, spawned over stdio or reached over -http,
// using internal/mcpclient so the handshake and rendering are exactly what
// every other spectackle surface does — no hand-rolled JSON-RPC framing,
// no dropped instructions field.
//
// Exit codes: 2 for a usage problem (bad arguments/JSON, unreadable stdin
// line), 1 when the session could not be established or any tool call came
// back flagged IsError, 0 when every call succeeded.
// verifyCmd runs the workspace verify commands — the ONE local gate
// definition (config.yaml 'verify'), shared by the done transition and the
// pre-push hook (T-01KYDNN). Exit mirrors the worst command.
func verifyCmd(args []string) int {
	fs := flag.NewFlagSet("verify", flag.ExitOnError)
	root := fs.String("root", ".", "workspace root")
	_ = fs.Parse(args)
	ws, err := workspace.Detect(*root, *root)
	if err != nil {
		log.Printf("verify: %v", err)
		return 1
	}
	if len(ws.Cfg.Verify) == 0 {
		fmt.Println("ok verify: no commands configured")
		return 0
	}
	for _, cmdline := range ws.Cfg.Verify {
		fmt.Println("verify:", cmdline)
		c := exec.Command("sh", "-c", cmdline)
		c.Dir = ws.Dir
		c.Stdout, c.Stderr = os.Stdout, os.Stderr
		if err := c.Run(); err != nil {
			log.Printf("verify: %s: %v", cmdline, err)
			return 1
		}
	}
	fmt.Println("ok verify")
	return 0
}

// hookCmd installs the pre-push hook on explicit operator request — the
// consent model is recommendation over imposition; nothing here runs
// without this command (T-01KYDNN).
// releaseNotesCmd renders release notes from the journal archive
// tombstones (T-01KYGCJ6) — derived, never hand-written. -since bounds the
// window (YYYY-MM-DD); output goes to stdout for the human to front with
// an intro and commit.
func releaseNotesCmd(args []string) int {
	fs := flag.NewFlagSet("releasenotes", flag.ExitOnError)
	root := fs.String("root", ".", "workspace root")
	since := fs.String("since", "", "include archives on/after this instant (YYYY-MM-DD or RFC3339); typically the previous release tag's date; empty = all")
	_ = fs.Parse(args)
	ws, err := workspace.Detect(*root, *root)
	if err != nil {
		log.Printf("releasenotes: %v", err)
		return 1
	}
	var cut time.Time
	if *since != "" {
		if cut, err = time.Parse("2006-01-02", *since); err != nil {
			if cut, err = time.Parse(time.RFC3339, *since); err != nil {
				log.Printf("releasenotes: -since: want YYYY-MM-DD or RFC3339: %v", err)
				return 2
			}
		}
	}
	events, err := journal.ReadAll(ws)
	if err != nil {
		log.Printf("releasenotes: %v", err)
		return 1
	}
	fmt.Print(relnotes.Render(events, cut))
	return 0
}

func hookCmd(args []string) int {
	if len(args) < 1 || args[0] != "install" {
		log.Printf("hook: usage: spectackle hook install [-root DIR]")
		return 2
	}
	fs := flag.NewFlagSet("hook install", flag.ExitOnError)
	root := fs.String("root", ".", "repository root")
	_ = fs.Parse(args[1:])
	ws, err := workspace.Detect(*root, *root)
	if err != nil {
		log.Printf("hook install: %v", err)
		return 1
	}
	if err := wt.InstallPrePushHook(ws.Dir); err != nil {
		if errors.Is(err, os.ErrExist) {
			log.Printf("hook install: a foreign pre-push hook exists — chain it to 'spectackle verify' yourself, it will not be clobbered")
			return 1
		}
		log.Printf("hook install: %v", err)
		return 1
	}
	fmt.Println("ok hook installed:", wt.HookPath(ws.Dir))
	return 0
}

func call(args []string) int {
	fs := flag.NewFlagSet("call", flag.ExitOnError)
	root := fs.String("root", ".", "workspace detection start / fallback root (stdio transport only)")
	httpAddr := fs.String("http", "", "reach a resident server at this address (e.g. 127.0.0.1:7331) instead of spawning one over stdio")
	showInstructions := fs.Bool("instructions", false, "print the server's instructions manifest and exit")
	_ = fs.Parse(args)

	cfg := mcpclient.Config{Root: *root}
	if *httpAddr != "" {
		cfg.Endpoint = httpEndpoint(*httpAddr)
	}

	ctx := context.Background()
	sess, err := mcpclient.Dial(ctx, cfg)
	if err != nil {
		log.Printf("call: %v", err)
		return 1
	}
	defer sess.Close()

	if *showInstructions {
		fmt.Println(sess.Instructions())
		return 0
	}

	rest := fs.Args()
	if len(rest) > 2 {
		log.Printf("call: too many arguments (want NAME [JSON]): %v", rest)
		return 2
	}
	if len(rest) > 0 {
		c := mcpclient.Call{Name: rest[0]}
		if len(rest) == 2 {
			if err := json.Unmarshal([]byte(rest[1]), &c.Arguments); err != nil {
				log.Printf("call: parse JSON arguments: %v", err)
				return 2
			}
		}
		// B-01KYFPNCK: over -http the RESIDENT's identity would stamp every
		// verdict, silently fabricating independence — SPECTACKLE_AGENT set
		// on the CLI died at the process boundary. Forward it as the
		// verdict-scoped agent field (never overriding an explicit one);
		// the server applies every identity refusal to the forwarded name.
		if *httpAddr != "" && (c.Name == "grill" || c.Name == "validate") {
			if env := os.Getenv("SPECTACKLE_AGENT"); env != "" {
				if op, _ := c.Arguments["op"].(string); op == "verdict" {
					if _, has := c.Arguments["agent"]; !has {
						if c.Arguments == nil {
							c.Arguments = map[string]any{}
						}
						c.Arguments["agent"] = env
					}
				}
			}
		}
		out, callErr := sess.Call(ctx, c)
		fmt.Println(out)
		if callErr != nil {
			// A refusal already printed its whole reason as the result text;
			// the non-zero exit is the machine signal, and a second stderr
			// rendition would only re-tax every reader (T-01KYE0). Transport
			// errors have no result text, so they keep the full error.
			if !errors.Is(callErr, mcpclient.ErrToolRefused) {
				log.Printf("call: %s: %v", c.Name, callErr)
			}
			printShapeOnSchemaRefusal(ctx, sess, c.Name, out, callErr)
			return 1
		}
		return 0
	}

	return callStdin(ctx, sess)
}

// printShapeOnSchemaRefusal appends one shape line when a refusal came
// from the SDK's input-schema validation (B-01KYE69): that message names
// only the REJECTED property, and a live judge burned fourteen refusals
// cycling invented decide arguments because nothing ever named the
// accepted vocabulary. Only schema refusals qualify — server-composed
// refusals (`! ... E`) already carry their own guidance, and taxing every
// refusal with a schema dump would undo the T-01KYE0 diet.
func printShapeOnSchemaRefusal(ctx context.Context, sess *mcpclient.Session, tool, out string, callErr error) {
	if !errors.Is(callErr, mcpclient.ErrToolRefused) || !strings.HasPrefix(out, `validating "arguments"`) {
		return
	}
	if line, err := sess.ShapeLine(ctx, tool); err == nil {
		fmt.Println(line)
	}
}

// httpEndpoint normalizes the -http flag's address (e.g. "127.0.0.1:7331",
// matching serve's own -http flag) into the http(s) URL mcpclient.Config
// expects. A value that already names a scheme is passed through as-is.
func httpEndpoint(addr string) string {
	if strings.Contains(addr, "://") {
		return addr
	}
	return "http://" + addr
}

// callStdin reads one JSON object per non-empty stdin line, each shaped
// {"name": ..., "arguments": {...}}, and issues them in order over the
// single already-dialed session — reconnecting per line would re-index the
// workspace on every call, the exact cost CLI-002 exists to remove. It
// keeps going after a refusal (IsError) or a malformed line, reporting
// which line failed on stderr, and returns non-zero if any line failed.
func callStdin(ctx context.Context, sess *mcpclient.Session) int {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)

	failed := false
	line := 0
	for scanner.Scan() {
		line++
		text := strings.TrimSpace(scanner.Text())
		if text == "" {
			continue
		}
		var req struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal([]byte(text), &req); err != nil {
			log.Printf("call: line %d: parse JSON: %v", line, err)
			failed = true
			continue
		}
		out, err := sess.Call(ctx, mcpclient.Call{Name: req.Name, Arguments: req.Arguments})
		fmt.Println(out)
		if err != nil {
			// Same refusal/transport split as the single-call path: the
			// printed text is the refusal, stderr re-narration is noise.
			if !errors.Is(err, mcpclient.ErrToolRefused) {
				log.Printf("call: line %d (%s): %v", line, req.Name, err)
			}
			printShapeOnSchemaRefusal(ctx, sess, req.Name, out, err)
			failed = true
		}
	}
	if err := scanner.Err(); err != nil {
		log.Printf("call: reading stdin: %v", err)
		return 1
	}
	if failed {
		return 1
	}
	return 0
}

// benchCmd runs the text-surface benchmark: a scripted full lifecycle over a
// generated fixture, metering every result byte (see internal/bench). With
// -against it A/Bs two binaries; alone it reports the running binary's own
// baseline. Exit non-zero when validity fails, so the harness is scriptable.
func benchCmd(args []string) int {
	fs := flag.NewFlagSet("bench", flag.ContinueOnError)
	against := fs.String("against", "", "CANDIDATE binary to A/B; this binary (or -baseline) is the baseline")
	baseline := fs.String("baseline", "", "override the baseline binary (default: this one), so two foreign builds can be A/B'd with the current fixture and script")
	agentPrep := fs.String("agent-prep", "", "prepare a metered judge workspace in DIR: seeded fixture, fixed brief, metering shim (P-01KYDP stage 4)")
	withManifest := fs.Bool("with-manifest", false, "agent-prep only: prepend the connect-time manifest to the brief (simulates an MCP session) and record its size for the session-cost line")
	scenario := fs.String("scenario", "basic", "agent-prep only: judge scenario — basic (draft/archive/reject), tricky (rule slots, reopen loop into blocked, decide exit), worktree (swarm flow), or outcome (hidden acceptance tests score first-iteration completeness per token, T-01KYFSQQ)")
	agentScore := fs.String("agent-score", "", "score a completed judge run in DIR: goal states plus metered bytes; exit non-zero when goals were not reached")
	nonces := fs.String("nonces", "", "agent-score only: comma-separated prep nonces matched positionally to the score dirs — the out-of-band anchor a workspace re-prep cannot forge")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	self, err := os.Executable()
	if err != nil {
		log.Printf("bench: resolve self: %v", err)
		return 1
	}
	if *agentPrep != "" {
		brief, shim, nonce, err := bench.AgentPrep(self, *agentPrep, *withManifest, *scenario)
		if err != nil {
			log.Printf("bench: agent-prep: %v", err)
			return 1
		}
		// The nonce line is the orchestrator's out-of-band anchor: record it
		// at prep time and hand it to -agent-score via -nonces — no
		// in-workspace rewrite can forge a value held outside the workspace.
		fmt.Printf("agent brief %s\nagent shim %s\nagent nonce %s\n", brief, shim, nonce)
		return 0
	}
	if *agentScore != "" {
		dirs := strings.Split(*agentScore, ",")
		// Positional nonce anchors (optional): unmatched positions score
		// unanchored rather than failing, so a partially recorded batch
		// still scores — an absent anchor weakens evidence, it does not
		// invent a violation.
		anchor := func(i int) string {
			if *nonces == "" {
				return ""
			}
			ns := strings.Split(*nonces, ",")
			if i < len(ns) {
				return strings.TrimSpace(ns[i])
			}
			return ""
		}
		if len(dirs) == 1 {
			sc, err := bench.ScoreAgentRunAnchored(self, dirs[0], anchor(0))
			if err != nil {
				log.Printf("bench: agent-score: %v", err)
				return 1
			}
			fmt.Print(bench.AgentReport(sc))
			if !sc.Valid {
				return 1
			}
			return 0
		}
		var labels []string
		var scores []bench.AgentScore
		for i, d := range dirs {
			sc, err := bench.ScoreAgentRunAnchored(self, d, anchor(i))
			if err != nil {
				log.Printf("bench: agent-score %s: %v", d, err)
				return 1
			}
			labels = append(labels, filepath.Base(d))
			scores = append(scores, sc)
		}
		out, allValid := bench.AggregateReport(labels, scores)
		fmt.Print(out)
		if !allValid {
			return 1
		}
		return 0
	}
	if *against != "" || *baseline != "" {
		base, cand := self, self
		if *baseline != "" {
			base = *baseline
		}
		if *against != "" {
			cand = *against
		}
		out, err := bench.AB(base, cand)
		if err != nil {
			log.Printf("bench: %v", err)
			return 1
		}
		fmt.Print(out)
		if strings.Contains(out, "CANDIDATE LOSES") {
			return 1
		}
		return 0
	}
	res, err := bench.Run(self)
	if err != nil {
		log.Printf("bench: %v", err)
		return 1
	}
	fmt.Print(bench.Report(res))
	if !res.Valid {
		return 1
	}
	return 0
}

func lint(args []string) int {
	root := "."
	if len(args) > 0 {
		root = args[0]
	}
	c, err := spec.Load(root)
	if err != nil {
		log.Printf("lint: %v", err)
		return 1
	}
	nf, errors := 0, 0
	for _, f := range c.Findings() {
		fmt.Println(f.String())
		nf++
		if f.Severity == ears.Error {
			errors++
		}
	}
	nrules := 0
	for _, sf := range c.All() {
		nrules += len(sf.Rules)
	}
	log.Printf("%d spec files, %d rules, %d findings (%d errors)", len(c.All()), nrules, nf, errors)
	if errors > 0 {
		return 1
	}
	return 0
}

// reindex is DEFECT 3's fix: previously this subcommand only resynced the
// spec/doc cache (internal/sync) — the symbol graph drift classification
// actually depends on is built exclusively inside the MCP server
// (mcpserver.Server.reindex), so this command's name and help text
// promised a rebuild it never performed. The obvious operator response to
// a stale graph under a resident -http server (DEFECT 1/DRF-003) was to
// run `reindex`, and it did nothing. Now it rebuilds the graph too, via
// mcpserver.BuildGraph — the exact same pipeline (parser list, resolvers,
// typed-call pass) the server uses, so the two can never drift onto
// different parser sets — and opens the same persistent parse-blob cache
// the server would (ws.CacheDir()/parse.db), so a reindex also warms the
// cache a subsequent `serve` picks up.
func reindex(args []string) int {
	root := rootFlag("reindex", args)
	ws, err := workspace.Detect(root, root)
	if err != nil {
		log.Printf("reindex: %v", err)
		return 1
	}
	if err := ws.EnsureScaffold(""); err != nil {
		log.Printf("reindex: %v", err)
		return 1
	}
	c, err := cache.Open(ws.CacheDir())
	if err != nil {
		log.Printf("reindex: %v", err)
		return 1
	}
	defer c.Close()
	if err := (&syncpkg.Scanner{Root: ws, Cache: c}).Refresh(); err != nil {
		log.Printf("reindex: %v", err)
		return 1
	}

	blobs, err := store.Open(filepath.Join(ws.CacheDir(), "parse.db"))
	if err != nil {
		log.Printf("reindex: parse cache: %v (using in-memory store)", err)
		blobs = store.NewMem()
	}
	defer blobs.Close()

	_, st, tp, err := mcpserver.BuildGraph(context.Background(), ws, blobs)
	if err != nil {
		log.Printf("reindex: index: %v", err)
		return 1
	}
	log.Printf("reindex: %d files, %d nodes, %d edges (+%d typed calls, %d skipped) (%s)",
		st.Files, st.Nodes, st.Edges, tp.Added, st.Skipped, ws.Dir)
	// issue 28: this used to be BuildGraph's own log line and nothing more —
	// an operator watching reindex's stderr saw it, but the same failure was
	// invisible through the server's tool surface (state/check). Now that
	// `state`/`check` surface it too (mcpserver.TypedPassState /
	// typedPassFinding), give the operator watching THIS stderr the one
	// thing they can act on immediately: rebuilding spectackle itself with a
	// current Go toolchain restores the pass (the cause text already names
	// both the required and the running Go version when that's why).
	if tp.Cause != "" {
		log.Printf("reindex: typed-call pass disabled, %d package(s) affected: %s — rebuild spectackle with a newer Go toolchain to restore typed cross-package call edges", tp.Packages, tp.Cause)
	}
	return 0
}
