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
//	spectackle lint  [PATH]        lint all EARS spec bundles, exit 1 on errors
//	spectackle reindex [-root DIR] force a cache resync (debugging aid)
//	spectackle version             print the version
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jxsl13/spectackle/internal/cache"
	"github.com/jxsl13/spectackle/internal/ears"
	"github.com/jxsl13/spectackle/internal/mcpserver"
	"github.com/jxsl13/spectackle/internal/spec"
	syncpkg "github.com/jxsl13/spectackle/internal/sync"
	"github.com/jxsl13/spectackle/internal/workspace"
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
	case "lint":
		return lint(args[1:])
	case "reindex":
		return reindex(args[1:])
	case "version":
		fmt.Println("spectackle " + mcpserver.Version)
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
  spectackle lint  [PATH]        lint all EARS spec bundles, exit 1 on errors
  spectackle reindex [-root DIR] force a cache resync
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
	_ = fs.Parse(args)

	s, err := mcpserver.New(*root)
	if err != nil {
		log.Printf("serve: %v", err)
		return 1
	}
	defer s.Close()

	if *httpAddr == "" {
		if *pidfile != "" {
			if err := writePIDFile(*pidfile); err != nil {
				log.Printf("serve: %v", err)
				return 1
			}
			defer removePIDFile(*pidfile)
		}
		log.Printf("spectackle %s serving over stdio", mcpserver.Version)
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
	log.Printf("spectackle %s serving over http on %s", mcpserver.Version, *httpAddr)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := runHTTP(ctx, *httpAddr, handler, *pidfile); err != nil {
		log.Printf("serve: %v", err)
		return 1
	}
	log.Printf("serve: shutdown complete")
	return 0
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
func writePIDFile(path string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("pidfile %s already exists (a server may already be running)", path)
		}
		return fmt.Errorf("pidfile %s: %w", path, err)
	}
	_, writeErr := fmt.Fprintf(f, "%d\n", os.Getpid())
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		_ = os.Remove(path)
		if writeErr != nil {
			return fmt.Errorf("pidfile %s: %w", path, writeErr)
		}
		return fmt.Errorf("pidfile %s: %w", path, closeErr)
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
	log.Printf("reindex: ok (%s)", ws.Dir)
	return 0
}
