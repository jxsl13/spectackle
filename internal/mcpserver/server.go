// Package mcpserver exposes spectackle over the Model Context Protocol.
//
// Transport is stdio: only JSON-RPC 2.0 frames go to stdout, all logging goes
// to stderr (SPX-ARC-001). The server is the single source of truth for the
// spec lifecycle: every write to the versioned .spectackle/ folders happens
// here — the LLM never touches those files directly.
package mcpserver

import (
	"context"
	"log"
	"os"
	"path/filepath"
	stdsync "sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jxsl13/spectackle/internal/cache"
	"github.com/jxsl13/spectackle/internal/coord"
	"github.com/jxsl13/spectackle/internal/graph"
	"github.com/jxsl13/spectackle/internal/index"
	"github.com/jxsl13/spectackle/internal/langspec"
	"github.com/jxsl13/spectackle/internal/resolve"
	"github.com/jxsl13/spectackle/internal/store"
	"github.com/jxsl13/spectackle/internal/sync"
	"github.com/jxsl13/spectackle/internal/workspace"
	"github.com/jxsl13/spectackle/internal/wt"
)

// Version is stamped into the MCP handshake and the CLI. Pre-1.0: anything
// may break between versions; the schema stamp and cache generation rotate
// with it instead of migrating. A var, not a const: release builds inject
// the tag via -ldflags "-X .../internal/mcpserver.Version=v0.x.y".
var Version = "0.2.0-dev"

// instructions is the self-bootstrapping server manifest: it teaches a
// connecting LLM the full lifecycle loop and tool order with zero extra docs.
const instructions = `spectackle — spec-lifecycle server. Source of truth: versioned .spectackle/ folders (spec.md=living contracts, work.md=active items, journal.ndjson=history). NEVER edit these files yourself; all writes go through tools. Loop for any change: (1) find q=<topic> scope=rejection — learn why similar work failed before; (2) find scope=code → node IDs (go:pkg.Fn); get id=<node> depth=2 for cross-language impact; (3) draft kind=proposal targets=<ids|paths> → CONTEXT PACK (#impact radius, #contracts EARS rules that bind it, #rejections similar past failures); (4) on explicit user approval: move to=approved (or straight to=active — any forward state skip is one call), then draft kind=task parent=<P-id> per work item and rule op=add for new contracts (fill slots; server composes+lints EARS, auto-IDs); (5) implement code; (6) check until ok — d records = spec/code drift, resolve via rule op=edit or code fix; (7) move to=done then to=archived, or active→archived in one call (implies done; merges the delta into spec.md, compacts). move to=rejected REQUIRES note; a rejection made with too little information is revocable — move the rejected ID back to any previous state (never done/archived). compact when check emits c records. Results are dense line records n/e/r/i/s/j/a/d/g/c/l/ag/sw/wt/!/nf/ok; cur <token> = resume by passing cur back. Reference everything by ID — never paste file contents.
SWARM: you may be one of several agents on this repo. Your siblings, their claims and fresh learnings: swarm (zero params) — run it when unsure. sw lines prepended to any result are sibling learnings (esp. rejections): read them BEFORE forming hypotheses; find scope=rejection includes sibling rejections in realtime. To change code: pick an approved item, then work op=start item=<id> — the server leases its scope and returns wt <item> open <root>: do ALL code edits, builds and benchmarks under that root; spectackle tools keep taking repo-relative paths. Never edit code outside your worktree root while a work item is open. Done implementing + check ok: work op=submit — the server gates (verify+goal commands), merges to main and propagates spec state; on gate fail or merge conflict fix the reported files in your worktree and submit again. work op=abort to give up (leases release, item returns to approved). Scope conflicts come back as l lines naming the holder — pick different scope, never wait idle. Release explicit claims (lease op=release) the moment your item is done — a stale claim blocks siblings until TTL expiry.
ORCHESTRATION: the intended division of labor is a complex/strong-model orchestrator (drafts proposals, writes exhaustive task bodies, reviews implementer output, merges) plus fresh minimal-context implementer agents on a simpler/cheaper model — each pulls ONE approved task (get <T-id> = its full brief), claims its scope, implements, tests, moves it to done and releases. Task bodies must be exhaustive (files, APIs, commands, constraints) because the orchestrator explores so the implementer never has to: this keeps the orchestrator's own context free of implementation noise and makes token cost scale with task count, not codebase size. FANOUT: partition approved tasks by disjoint scope (leases prove disjointness), spawn one fresh implementer per task in parallel, serialize only shared-file wiring yourself. Before asking the user anything: research q=<topic> first (mint an R-item + cheap subagent only if the pack doesn't answer it) — never ad hoc exploration in your own context. Before move to=approved on a proposal: grill id=<P-id> and close its gaps — a clean grill stamps grilled: <date>, the evidence move checks for. Every actual user decision goes through decide op=ask (native UI), never unstructured chat — no UI leaves the ADR-item open without blocking you, answered later via decide op=answer from anywhere. Reopens and gate-fail rounds are server-counted; hitting the configured limit sidesteps the item to blocked and mints an ADR-item — exits only via decide: rescope→draft, reject→rejected, override-once→active (once).
TOKEN ECONOMY: prefer server records over shell text tools whenever you only need locations or structure — shell grep/find return file contents (cost O(codebase)), server tools return IDs+spans (cost O(results)). Substitutions: grep/rg <symbol> -> find q=<symbol> scope=code (ranked n records; add focus=<node> for near-my-work ranking); grep -r over specs/history -> find scope=rule|rejection|history (FTS5); find(1) by filename -> find scope=code (file paths and signatures match too); where is X defined / what calls X -> get id=<node> depth=N; before any sed-style bulk edit survey the sites via get depth and run check after — sed is for the edit, never for the search; NEVER grep or sed .spectackle/ files (server-owned: reads via find/get, writes via tools only). Read raw file bytes with shell only when you already know the one file you need.`

// Server bundles the MCP server with its workspace, cache, graph and swarm
// coordination.
type Server struct {
	main   workspace.Root // the MAIN repo root (immutable after New)
	ws     workspace.Root // the ACTIVE root — re-rooted to a worktree during `work`
	cache  *cache.Cache   // active root's index cache
	blobs  store.Store    // active root's persistent parse-blob cache
	scan   *sync.Scanner
	g      graph.Graph
	cd     *coord.DB // shared swarm coordination (main repo's cache dir)
	agent  string
	wtItem string // item of the open worktree ("" = rooted at main)
	mcp    *mcp.Server

	lastSweep time.Time

	// mu serializes tool calls: the MCP SDK dispatches them concurrently,
	// but lifecycle writes are read-modify-write over shared files (ID
	// minting, work.md rewrites) — found the hard way when two concurrent
	// draft calls minted the same task ID.
	mu stdsync.Mutex
}

// New detects the workspace starting at root, resolves the MAIN repo (a
// linked worktree resolves to its parent), scaffolds, opens the local index
// cache and the shared coordination DB, and registers all tools.
func New(root string) (*Server, error) {
	ws, err := workspace.Detect(root, root)
	if err != nil {
		return nil, err
	}
	agent := os.Getenv("SPECTACKLE_AGENT")
	if agent == "" {
		agent = coord.GenName()
	}
	ws.Agent = agent

	// coordination always lives in the MAIN repo, even when started inside
	// a linked worktree (or a non-git dir, where main == ws).
	mainWS := ws
	if wt.IsRepo(ws.Dir) {
		if mainDir, _, err := wt.CommonRoot(ws.Dir); err == nil && mainDir != ws.Dir {
			if m, err := workspace.Detect(mainDir, mainDir); err == nil {
				mainWS = m
				mainWS.Agent = agent
			}
		}
	}
	if err := mainWS.EnsureScaffold(""); err != nil {
		return nil, err
	}
	if ws.Dir != mainWS.Dir {
		if err := ws.EnsureScaffold(""); err != nil {
			return nil, err
		}
	}
	c, err := cache.Open(ws.CacheDir())
	if err != nil {
		return nil, err
	}
	cd, err := coord.Open(mainWS.CoordPath(), agent, os.Getpid())
	if err != nil {
		c.Close()
		return nil, err
	}
	s := &Server{
		main:  mainWS,
		ws:    ws,
		cache: c,
		scan:  &sync.Scanner{Root: ws, Cache: c},
		g:     graph.NewMem(),
		cd:    cd,
		agent: agent,
		blobs: openBlobs(ws),
	}
	s.reindex()
	s.mcp = mcp.NewServer(&mcp.Implementation{
		Name:    "spectackle",
		Title:   "spectackle — spec-driven cross-language code intelligence",
		Version: Version,
	}, &mcp.ServerOptions{Instructions: instructions})
	s.registerTools()
	s.registerPrompts()
	return s, nil
}

// openBlobs opens the persistent parse-blob cache of a root (warm graph
// starts across sessions — the point of the resident -http service). A
// failed open degrades to the in-memory store: caching is never load-bearing.
func openBlobs(ws workspace.Root) store.Store {
	b, err := store.Open(filepath.Join(ws.CacheDir(), "parse.db"))
	if err != nil {
		log.Printf("parse cache: %v (using in-memory store)", err)
		return store.NewMem()
	}
	return b
}

// reindex rebuilds the symbol graph from the active root (startup and after
// every reroot — worktree code diverges from main). A failed index run keeps
// the previous graph: lifecycle tools must survive unparseable trees.
func (s *Server) reindex() {
	g := graph.NewMem()
	// hand-written parsers first, then every langspec-registered language —
	// adding a language is one data file in internal/langspec, no wiring.
	parsers := append(
		[]index.LanguageParser{index.GoParser{}, index.AsmParser{}, index.CudaParser{}},
		langspec.All()...)
	ix := index.New(g, s.blobs, parsers,
		resolve.Default().All(), s.ws.Cfg.Ignore...)
	st, err := ix.IndexAll(context.Background(), s.ws.Dir)
	if err != nil {
		log.Printf("index: %v (keeping previous graph)", err)
		return
	}
	// M3 upgrade pass: go/types-resolved call edges (chained selectors,
	// cross-package methods). Best-effort — a broken module tree must not
	// take the syntactic graph down with it.
	typed, tErr := index.ResolveTypedCalls(context.Background(), g, s.ws.Dir, s.blobs)
	if tErr != nil {
		log.Printf("index: typed calls: %v (syntactic graph only)", tErr)
	}
	s.g = g
	log.Printf("index: %d files, %d nodes, %d edges (+%d typed calls, %d skipped)",
		st.Files, st.Nodes, st.Edges, typed, st.Skipped)
}

// MCP returns the underlying protocol server (for transports and tests).
func (s *Server) MCP() *mcp.Server { return s.mcp }

// Close releases the cache and coordination handles. The agent deregisters
// (leases + registry row) so short-lived sessions leave a clean swarm view;
// an open worktree's record survives in coord.db for reclaim.
func (s *Server) Close() error {
	err := s.cache.Close()
	if bErr := s.blobs.Close(); err == nil {
		err = bErr
	}
	if dErr := s.cd.Deregister(); err == nil {
		err = dErr
	}
	if cErr := s.cd.Close(); err == nil {
		err = cErr
	}
	return err
}

func (s *Server) leaseTTL() time.Duration {
	return time.Duration(s.main.Cfg.Swarm.LeaseTTL) * time.Second
}

func (s *Server) agentTTL() time.Duration {
	return time.Duration(s.main.Cfg.Swarm.AgentTTL) * time.Second
}

// reroot swaps the active workspace (main <-> worktree), swapping the local
// index cache and scanner with it. The coordination DB always stays on main.
func (s *Server) reroot(dir, item string) error {
	nws := workspace.Root{Dir: dir, Agent: s.agent, Cfg: s.main.Cfg}
	if err := nws.EnsureScaffold(""); err != nil {
		return err
	}
	nc, err := cache.Open(nws.CacheDir())
	if err != nil {
		return err
	}
	s.cache.Close()
	s.blobs.Close()
	s.ws, s.cache = nws, nc
	s.blobs = openBlobs(nws)
	s.scan = &sync.Scanner{Root: nws, Cache: nc}
	s.wtItem = item
	s.reindex() // the graph must reflect the newly active root
	return s.cd.SetActive(item, map[bool]string{true: "", false: dir}[item == ""])
}
