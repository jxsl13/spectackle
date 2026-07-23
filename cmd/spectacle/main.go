// Command spectacle is the spec-driven MCP server for cross-language
// codebases. Subcommands:
//
//	spectacle serve [-root DIR]   run the MCP server on stdio
//	spectacle lint  [PATH]        lint all EARS spec files, exit 1 on errors
//	spectacle version             print the version
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jxsl13/spectacle/internal/ears"
	"github.com/jxsl13/spectacle/internal/mcpserver"
	"github.com/jxsl13/spectacle/internal/spec"
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
  spectacle serve [-root DIR]   run the MCP server on stdio
  spectacle lint  [PATH]        lint all EARS spec files, exit 1 on errors
  spectacle version             print the version`)
}

func serve(args []string) int {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	root := fs.String("root", ".", "repository root to serve")
	_ = fs.Parse(args)

	s := mcpserver.New(*root)
	log.Printf("spectacle %s serving %s over stdio", mcpserver.Version, *root)
	if err := s.MCP().Run(context.Background(), &mcp.StdioTransport{}); err != nil {
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
