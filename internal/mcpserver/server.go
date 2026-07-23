// Package mcpserver exposes spectacle over the Model Context Protocol.
//
// Transport is stdio: only JSON-RPC 2.0 frames go to stdout, all logging goes
// to stderr (SPX-ARC-001) — a single stray fmt.Println would corrupt the
// protocol stream.
//
// M0 wiring: the spec/EARS tools (contracts, lint_ears, coverage, link and
// the contract half of plan_change) are fully functional; graph-backed tools
// (sym, map, impact, reindex and the impact half of plan_change) return a
// documented stub record until M1/M2 land the parsers and resolvers.
package mcpserver

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jxsl13/spectacle/internal/graph"
)

// Version is stamped into the MCP handshake and the CLI.
const Version = "0.1.0-m0"

// Server bundles the MCP server with its repo root and (future) graph.
type Server struct {
	root string
	g    graph.Graph
	mcp  *mcp.Server
}

// New builds the MCP server for a repo root and registers all tools.
// The spec cascade is re-loaded per tool call: spec files are small, and a
// live-edited spec must be visible to the next call without a restart.
func New(root string) *Server {
	s := &Server{
		root: root,
		g:    graph.NewMem(),
	}
	s.mcp = mcp.NewServer(&mcp.Implementation{
		Name:    "spectacle",
		Title:   "spectacle — spec-driven cross-language code intelligence",
		Version: Version,
	}, nil)
	s.registerTools()
	return s
}

// MCP returns the underlying protocol server (for transports and tests).
func (s *Server) MCP() *mcp.Server { return s.mcp }
