// Package mcpserver exposes spectacle over the Model Context Protocol.
//
// Transport is stdio: only JSON-RPC 2.0 frames go to stdout, all logging goes
// to stderr (SPX-ARC-001). The server is the single source of truth for the
// spec lifecycle: every write to the versioned .spectacle/ folders happens
// here — the LLM never touches those files directly.
package mcpserver

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jxsl13/spectacle/internal/cache"
	"github.com/jxsl13/spectacle/internal/graph"
	"github.com/jxsl13/spectacle/internal/sync"
	"github.com/jxsl13/spectacle/internal/workspace"
)

// Version is stamped into the MCP handshake and the CLI. Pre-1.0: anything
// may break between versions; the schema stamp and cache generation rotate
// with it instead of migrating.
const Version = "0.2.0-dev"

// instructions is the self-bootstrapping server manifest: it teaches a
// connecting LLM the full lifecycle loop and tool order with zero extra docs.
const instructions = `spectacle — spec-lifecycle server. Source of truth: versioned .spectacle/ folders (spec.md=living contracts, work.md=active items, journal.ndjson=history). NEVER edit these files yourself; all writes go through tools. Loop for any change: (1) find q=<topic> scope=rejection — learn why similar work failed before; (2) find scope=code → node IDs (go:pkg.Fn); get id=<node> depth=2 for cross-language impact; (3) draft kind=proposal targets=<ids|paths> → CONTEXT PACK (#impact radius, #contracts EARS rules that bind it, #rejections similar past failures); (4) on explicit user approval: move to=approved, then draft kind=task parent=<P-id> per work item and rule op=add for new contracts (fill slots; server composes+lints EARS, auto-IDs); (5) implement code; (6) check until ok — d records = spec/code drift, resolve via rule op=edit or code fix; (7) move to=done, then to=archived (merges the delta into spec.md, compacts). move to=rejected REQUIRES note; a rejection made with too little information is revocable — move the rejected ID back to any previous state. compact when check emits c records. Results are dense line records n/e/r/i/s/j/a/d/g/c/!/nf/ok; cur <token> = resume by passing cur back. Reference everything by ID — never paste file contents.`

// Server bundles the MCP server with its workspace, cache and graph.
type Server struct {
	ws    workspace.Root
	cache *cache.Cache
	scan  *sync.Scanner
	g     graph.Graph
	mcp   *mcp.Server
}

// New detects the workspace starting at root, scaffolds the root .spectacle
// folder, opens the cache and registers all tools.
func New(root string) (*Server, error) {
	ws, err := workspace.Detect(root, root)
	if err != nil {
		return nil, err
	}
	if err := ws.EnsureScaffold(""); err != nil {
		return nil, err
	}
	c, err := cache.Open(ws.CacheDir())
	if err != nil {
		return nil, err
	}
	s := &Server{
		ws:    ws,
		cache: c,
		scan:  &sync.Scanner{Root: ws, Cache: c},
		g:     graph.NewMem(),
	}
	s.mcp = mcp.NewServer(&mcp.Implementation{
		Name:    "spectacle",
		Title:   "spectacle — spec-driven cross-language code intelligence",
		Version: Version,
	}, &mcp.ServerOptions{Instructions: instructions})
	s.registerTools()
	return s, nil
}

// MCP returns the underlying protocol server (for transports and tests).
func (s *Server) MCP() *mcp.Server { return s.mcp }

// Close releases the cache handle.
func (s *Server) Close() error { return s.cache.Close() }
