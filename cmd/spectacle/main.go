// Command spectacle is the spec-driven MCP server for cross-language
// codebases. Subcommands:
//
//	spectacle serve [-root DIR] [-http ADDR] run the MCP server on stdio, or over
//	                                         Streamable HTTP when -http is set
//	                                         (workspace auto-detected)
//	spectacle lint  [PATH]        lint all EARS spec bundles, exit 1 on errors
//	spectacle reindex [-root DIR] force a cache resync (debugging aid)
//	spectacle version             print the version
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jxsl13/spectacle/internal/cache"
	"github.com/jxsl13/spectacle/internal/ears"
	"github.com/jxsl13/spectacle/internal/mcpserver"
	"github.com/jxsl13/spectacle/internal/spec"
	syncpkg "github.com/jxsl13/spectacle/internal/sync"
	"github.com/jxsl13/spectacle/internal/workspace"
)

func main() {
	log.SetFlags(0)
	log.SetOutput(os.Stderr) // stdout is reserved for JSON-RPC (SPX-ARC-001)

	args := os.Args[1:]
	if len(args) == 0 {
		usage()
		os.Exit(2)
	}
	switch args[0] {
	case "serve":
		os.Exit(serve(args[1:]))
	case "lint":
		os.Exit(lint(args[1:]))
	case "reindex":
		os.Exit(reindex(args[1:]))
	case "version":
		fmt.Println("spectacle " + mcpserver.Version)
	case "-h", "--help", "help":
		usage()
	default:
		log.Printf("unknown subcommand %q", args[0])
		usage()
		os.Exit(2)
	}
}

func usage() {
	log.Print(`usage:
  spectacle serve [-root DIR] [-http ADDR] run the MCP server on stdio, or over
                                            Streamable HTTP when -http is set
                                            (workspace auto-detected)
  spectacle lint  [PATH]        lint all EARS spec bundles, exit 1 on errors
  spectacle reindex [-root DIR] force a cache resync
  spectacle version             print the version`)
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
	_ = fs.Parse(args)

	s, err := mcpserver.New(*root)
	if err != nil {
		log.Printf("serve: %v", err)
		return 1
	}
	defer s.Close()

	if *httpAddr == "" {
		log.Printf("spectacle %s serving over stdio", mcpserver.Version)
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
	log.Printf("spectacle %s serving over http on %s", mcpserver.Version, *httpAddr)
	if err := http.ListenAndServe(*httpAddr, handler); err != nil {
		log.Printf("serve: %v", err)
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
