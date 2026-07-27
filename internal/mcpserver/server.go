// Package mcpserver exposes spectackle over the Model Context Protocol.
//
// Transport is stdio: only JSON-RPC 2.0 frames go to stdout, all logging goes
// to stderr (SPX-ARC-001). The server is the single source of truth for the
// spec lifecycle: every write to the versioned .spectackle/ folders happens
// here — the LLM never touches those files directly.
package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime/debug"
	stdsync "sync"
	"sync/atomic"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jxsl13/spectackle/internal/cache"
	"github.com/jxsl13/spectackle/internal/coord"
	"github.com/jxsl13/spectackle/internal/forge"
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
//
// It carries ONLY what every session needs (T-01KYE3): the lifecycle loop,
// the token-economy substitutions, and the record-writing rules. The
// situational sections — SWARM (multi-agent sessions), ORCHESTRATION (the
// orchestrator role), BROWNFIELD (one-time onboarding) — live in the `guide`
// prompt and are fetched when their mode applies: they were 54% of a
// manifest that every single-agent session paid in full at connect time.
const instructions = `spectackle — spec-lifecycle server. Source of truth: versioned .spectackle/ folders (spec.md=living contracts, work.md=active items, journal.ndjson=history). NEVER edit these files yourself; all writes go through tools. Loop for any change: (1) find q=<topic> scope=rejection — learn why similar work failed before; (2) find scope=code → node IDs (go:pkg.Fn); get id=<node> depth=2 for cross-language impact; (3) draft kind=proposal targets=<ids|paths> → CONTEXT PACK (#impact radius, #contracts EARS rules that bind it, #rejections similar past failures); (4) on explicit user approval: move to=approved (or straight to=active — any forward state skip is one call), then draft kind=task parent=<P-id> per work item and rule op=add for new contracts (fill slots; server composes+lints EARS, auto-IDs); (5) implement code; (6) check until ok — d records = spec/code drift, resolve via rule op=edit or code fix; (7) move to=done then to=archived, or active→archived in one call (implies done; merges the delta into spec.md, compacts). move to=rejected REQUIRES note; a rejection made with too little information is revocable — move the rejected ID back to any previous state (never done/archived). compact when check emits c records. Results are dense line records n/e/r/i/s/j/a/d/g/c/l/ag/sw/wt/!/nf/ok; cur <token> = resume by passing cur back. Reference everything by ID — never paste file contents.
MODES: working alongside other agents, orchestrating implementer agents, or onboarding an existing repo? Fetch the matching playbook on demand: prompt guide topic=swarm|orchestration|brownfield.
TOKEN ECONOMY: prefer server records over shell text tools whenever you only need locations or structure — shell grep/find return file contents (cost O(codebase)), server tools return IDs+spans (cost O(results)). Substitutions: grep/rg <symbol> -> find q=<symbol> scope=code (ranked n records; add focus=<node> for near-my-work ranking); grep -r over specs/history -> find scope=rule|rejection|history (FTS5); find(1) by filename -> find scope=code (file paths and signatures match too); where is X defined / what calls X -> get id=<node> depth=N; before any sed-style bulk edit survey the sites via get depth and run check after — sed is for the edit, never for the search; NEVER grep or sed .spectackle/ files (server-owned: reads via find/get, writes via tools only). Read raw file bytes with shell only when you already know the one file you need.
RECORDS: write item bodies as compacted substance — constraints, decisions, measurements, rejected alternatives and why. Never paste verbatim user quotes or transcript excerpts: they bloat every later read of the record without adding information. Write every item body in American English: spelling variants (behavior/behaviour, initialize/initialise) fragment full-text matches exactly like a language mix does. Same token-economy family as the output diet. Standing decisions and durable learnings are cast into the spec as they emerge — rule op=add or decide op=ask — never into agent-private memory nothing else reads or versions.
BACKPROP: every completed or rejected item leaves its learning in one of three durable stores — spec.md rules (standing contracts), journal tombstone notes (archive/reject summaries), knowledge artifacts (docs, ADRs). The archive/reject note is what later sessions search first: what changed, what was measured, what was deliberately not done, where the learning landed — the note IS the training signal. Research items end consumed (rules/ADRs/tasks citing them) or explicitly closed no-action. Resident-server repo? the watcher self-restarts it post-merge; without one, make dev is operator work — stale binaries serve yesterday's logic.`

// guideTopics are the situational playbooks served by the `guide` prompt,
// verbatim the sections that used to sit in the manifest (T-01KYE3). Their
// content is unchanged by design: this move re-prices when they are read,
// not what they say.
var guideTopics = map[string]string{
	"swarm":         `SWARM: you may be one of several agents on this repo. Your siblings, their claims and fresh learnings: swarm (zero params) — run it when unsure. sw lines prepended to any result are sibling learnings (esp. rejections): read them BEFORE forming hypotheses; find scope=rejection includes sibling rejections in realtime. To change code: pick an approved item, then work op=start item=<id> — the server leases its scope and returns wt <item> open <root>: do ALL code edits, builds and benchmarks under that root; spectackle tools keep taking repo-relative paths. Never edit code outside your worktree root while a work item is open. Done implementing + check ok: work op=submit — the server gates (verify+goal commands), merges to main and propagates spec state; on gate fail or merge conflict fix the reported files in your worktree and submit again. work op=abort to give up (leases release, item returns to approved). Scope conflicts come back as l lines naming the holder — pick different scope, never wait idle. Release explicit claims (lease op=release) the moment your item is done — a stale claim blocks siblings until TTL expiry.`,
	"orchestration": `ORCHESTRATION: the intended division of labor is a complex/strong-model orchestrator (drafts proposals, writes exhaustive task bodies, reviews implementer output, merges) plus fresh minimal-context implementer agents on a simpler/cheaper model — each pulls ONE approved task (get <T-id> = its full brief), claims its scope, implements, tests, moves it to done and releases. Task bodies must be exhaustive (files, APIs, commands, constraints) because the orchestrator explores so the implementer never has to: this keeps the orchestrator's own context free of implementation noise and makes token cost scale with task count, not codebase size. FANOUT: partition approved tasks by disjoint scope (leases prove disjointness), spawn one fresh implementer per task in parallel, serialize only shared-file wiring yourself. Before asking the user anything: research q=<topic> first (mint an R-item + cheap subagent only if the pack doesn't answer it) — never ad hoc exploration in your own context. Before move to=approved on a proposal: grill id=<P-id> and close its gaps — a clean grill stamps grilled: <date>, the evidence move checks for. Every actual user decision goes through decide op=ask (native UI), never unstructured chat — no UI leaves the ADR-item open without blocking you, answered later via decide op=answer from anywhere. Reopens and gate-fail rounds are server-counted; hitting the configured limit sidesteps the item to blocked and mints an ADR-item — exits only via decide: rescope→draft, reject→rejected, override-once→active (once). Review mode: one reviewer, sequential lenses, never one agent per lens (panel only per item, on risk).`,
	"brownfield":    `BROWNFIELD IMPORT: onboarding an existing repo, in order. (1) Index first: state/reindex yields the graph immediately and costs no decisions — you get the real node IDs everything anchors to. (2) Survey in parallel: fan out READ-ONLY subagents over disjoint subtrees (one per top-level package/module); each reports the subtree's purpose, the invariants its code/tests/docs already assert, and candidate contracts. Read-only means no leases and no work.md contention, so fan out as wide as the tree. (3) Mint centrally: the orchestrator turns that survey into rule op=add contracts, scoped per context dir and anchored via applies to node IDs from step 1 — implementers never hand-write spec files, the server composes and lints. (4) Capture decisions: existing design/ADR documents become adr items via decide (context, decision, consequences, status); pure reference docs stay docs. (5) Baseline: run check until clean — the stamped anchors are the point from which drift detection means anything. (6) Then the normal loop: find scope=rejection, draft, grill, implement. Encode only invariants the code/tests/docs actually assert, never invented ones; start with the load-bearing few and let check's coverage gaps show what is still unowned.`,
}

// modulePath mirrors this repo's go.mod module directive. It backstops
// moduleRepoURL when debug.ReadBuildInfo cannot report a usable main
// module path — ReadBuildInfo returns ok=false in some build modes, and
// bi.Main.Path is empty in test binaries — so the fallback is load-bearing,
// not defensive decoration: without it the defect-reporting paragraph
// below could ship with a broken or missing URL.
const modulePath = "github.com/jxsl13/spectackle"

// moduleRepoURLFrom applies the derivation rule in isolation, given an
// already-read main module path: forges that host Go modules expose the
// repository at exactly "https://" + the module path, no path suffix, so
// the derivation stays host-agnostic. Split out from moduleRepoURL so the
// fallback branch is unit-testable without depending on how the calling
// binary itself was built.
func moduleRepoURLFrom(mainPath string) string {
	if mainPath == "" {
		mainPath = modulePath
	}
	return "https://" + mainPath
}

// moduleRepoURL derives the running binary's main module repository URL
// from its embedded build info (bi.Main.Path), falling back to modulePath.
func moduleRepoURL() string {
	mainPath := ""
	if bi, ok := debug.ReadBuildInfo(); ok && bi != nil {
		mainPath = bi.Main.Path
	}
	return moduleRepoURLFrom(mainPath)
}

// defectReportParagraph composes the DEFECT REPORTS paragraph (MCP-008):
// where to report a defect found in the server itself, what makes the
// report worth filing, and why a fix PR is not wanted. Kept out of the
// instructions const because its URL is resolved from runtime build info,
// not known at compile time.
func defectReportParagraph() string {
	return fmt.Sprintf("DEFECT REPORTS: notice a defect in this server itself, not in the repo you're operating on? File it as an issue at %s with your analysis — the reproduction, what you observed versus what you expected, and the isolated cause when you have one; that analysis is the expensive part and the only reason a report is worth filing. Do not send a fix PR: you lack the design context behind the code as written, and reviewing an unsolicited patch costs the maintainer more than the analysis would have saved them.", moduleRepoURL())
}

// manifest composes the full server-instructions manifest handed to the
// MCP initialize handshake: the static lifecycle instructions plus the
// defect-reporting paragraph, whose URL is only known at runtime.
// Manifest exposes the composed manifest for surfaces outside the MCP
// handshake: the CLI `manifest` subcommand prints it for humans and for the
// bench harness, which meters it as its own once-per-session line — its
// cost multiplier is sessions, not calls, and 7238 bytes stayed invisible
// through eleven benchmarked landings for lack of exactly this metering
// (T-01KYE3).
func Manifest() string { return manifest() }

func manifest() string {
	return instructions + "\n" + defectReportParagraph()
}

// Server bundles the MCP server with its workspace, cache, graph and swarm
// coordination.
type Server struct {
	main workspace.Root // the MAIN repo root (immutable after New)
	ws   workspace.Root // the ACTIVE root — re-rooted to a worktree during `work`
	// inFlight counts tool calls between gate entry and return — the
	// self-restart watcher defers its exec swap while it is nonzero
	// (B-01KYF7: a swap severed the archive edge that had itself moved
	// HEAD, twice, live).
	inFlight atomic.Int64

	// edgeFlush, when set, flushes the current call's edge commit early —
	// the move handler calls it before gitflow so the structured edge
	// commit lands (and rides the branch/push) ahead of the transition
	// sweep, which then finds the records clean (T-01KYD94MG coexistence).
	edgeFlush func(failed bool)

	// selfRestartOn records that the committed-only self-restart watcher is
	// active for this process: the mtime-based make-dev stale hint is then
	// suppressed — its advice would be a manual dirty-tree rebuild, the
	// exact hazard ADR-01KYF5 closes. Set once at serve start.
	selfRestartOn bool

	// forgeOverride, when set, replaces forgeFor's construction — the
	// TEST seam that lets the online branch/PR shape run hermetically
	// (injected offline forge + a local bare remote) now that mode:
	// offline no longer constructs a forge at all (T-01KYHAH1GJ P1).
	// Never set in production.
	forgeOverride func() (forge.Forge, error)

	// home is the root this process was STARTED with (-root), immutable
	// after New. It is where submit/abort re-root back to: a server homed
	// in a linked worktree (the resident orchestrator topology) must return
	// to ITS root, not to s.main — re-rooting to main would silently
	// migrate the serving workspace to the primary checkout, the third
	// member of the s.main/s.ws family after BinaryStale and ServedDir
	// (B-01KYEPC9SJE23V67A9YS9XBZFH).
	home   string
	cache  *cache.Cache // active root's index cache
	blobs  store.Store  // active root's persistent parse-blob cache
	scan   *sync.Scanner
	g      graph.Graph
	cd     *coord.DB // shared swarm coordination (main repo's cache dir)
	agent  string
	wtItem string // item of the open worktree ("" = rooted at main)
	mcp    *mcp.Server

	lastSweep time.Time

	// indexedAt is when reindex last successfully rebuilt s.g (captured
	// before the walk starts, not after — see reindex). check (DRF-003)
	// compares a node's file ModTime against this to detect a graph that is
	// older than the file it describes, so it can degrade to Pending
	// instead of hashing a stale line range against fresh content. Zero
	// value (never indexed) is handled the same as any other timestamp: any
	// file's ModTime compares after it, so every node classifies Pending
	// until the first successful reindex — correct, since there is no graph
	// yet to trust either way.
	indexedAt time.Time

	// typedPass is the go/types call-edge upgrade pass's outcome on the last
	// successful reindex (issue 28) — set alongside s.g and s.indexedAt in
	// reindex, from BuildGraph's return, so state (stateGraphSection) and
	// check can tell "the graph has typed call edges" from "it doesn't, and
	// here is why" instead of only seeing a node/edge count that reads the
	// same either way. The zero value has Cause == "", which
	// typedPassFinding treats as healthy — see TypedPassState's doc comment
	// for why that is the field reindex must leave untouched on failure.
	typedPass TypedPassState

	// compact-hint cache (postCall's proactive nudge, T-0093): a debounced
	// count of root journal events since the last compact, refreshed at most
	// once every 30s (the same cadence as the stale-agent sweep above) so the
	// hint never re-reads the journal file on every single tool call.
	// hintedAt is the count the hint was last surfaced at — 0 means armed
	// (never emitted yet, or re-armed after the count dropped back below the
	// threshold, i.e. a compact ran). See swarm.go: compactHint.
	lastCompactCheck time.Time
	compactCount     int
	hintedAt         int

	// stale-binary hint cache (postCall's proactive nudge, MCP-010): a
	// debounced verdict for whether the running executable is older than the
	// newest .go file under s.ws.Dir, refreshed at most once every
	// staleCheckInterval so the hint never walks the workspace tree on every
	// single tool call. staleHinted mirrors hintedAt's role above: false
	// means armed (never surfaced for the current crossing, or re-armed
	// after a rebuild made staleVerdict false again). See swarm.go:
	// staleHint, binaryStale.
	lastStaleCheck time.Time
	staleVerdict   bool
	staleHinted    bool

	// scCache memoizes the known-record-ID set for the duration of ONE tool
	// call (see idScope). Assembling it costs an item.LoadAll plus a full
	// journal pass, and a single handler routinely needs it several times —
	// once to expand its ID arguments and again for every record it renders
	// — so recomputing per use would multiply the cost of the two most
	// expensive reads in the server by the number of IDs in the output.
	//
	// Per call and not longer: any write in the call itself (draft mints,
	// move archives) changes the set, and a scope cached across calls would
	// shorten an ID against a stale peer list and hand back a prefix that no
	// longer resolves. preCall clears it; nothing else may.
	scCache *idScope

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
	// Canonicalize the root BEFORE detection: every identity decision below
	// (home == removed worktree, serving root == main, the B-0002 rebind)
	// compares paths, and git hands back symlink-resolved paths while the
	// caller's -root spelling may go through an alias — stock macOS /tmp
	// and TMPDIR are symlinks into /private, no crafted setup needed.
	// Spelling equality instead of directory identity made rerootBack miss
	// its rehome and left a session serving a deleted directory
	// (adversarial round 2 on B-01KYEPC9SJE23V67A9YS9XBZFH). Abs FIRST:
	// EvalSymlinks(".") is a no-op, and the CLI default IS "." — without
	// Abs the alias re-enters after this point via Detect's own Abs, whose
	// Getwd prefers the shell's $PWD spelling (adversarial round 3,
	// reproduced with default flags). Failures fall back to the given
	// spelling: a root that does not resolve fails Detect right after.
	if a, err := filepath.Abs(root); err == nil {
		root = a
	}
	if r, err := filepath.EvalSymlinks(root); err == nil {
		root = r
	}
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
	// Wire the swarm's shared lock table into every workspace.Root this
	// server writes through, BEFORE any of them reach item/spec/drift: those
	// packages serialize their read-modify-writes through Root.Lock (see
	// coord.DB.WithLock), and a Root built without it would silently write
	// unlocked — exactly the defect this task exists to close
	// (B-01KYD57FN3ERHBM5EQ3534YJXP). mainWS and ws are distinct values (a
	// linked worktree's ws differs from the main repo's mainWS), so both
	// need it independently; reroot below carries it forward the same way.
	mainWS.Lock, ws.Lock = cd, cd
	s := &Server{
		main:  mainWS,
		ws:    ws,
		home:  ws.Dir,
		cache: c,
		scan:  &sync.Scanner{Root: ws, Cache: c},
		g:     graph.NewMem(),
		cd:    cd,
		agent: agent,
		blobs: openBlobs(ws),
	}
	// A fresh process started inside an existing worktree (per-call stdio
	// client, or a restart after the resident server died) must rebind the
	// open-worktree state that work op=start established in another
	// process — without this, submit/abort answer "no open worktree" from
	// any process but the starter, orphaning the worktree (B-0002).
	// Identity must match: adopting a dead sibling's worktree stays an
	// explicit work op=abort decision, never an implicit takeover.
	if ws.Dir != mainWS.Dir {
		if wts, err := cd.Worktrees(); err == nil {
			for _, w := range wts {
				if w.Agent == agent && filepath.Clean(w.Root) == filepath.Clean(ws.Dir) {
					s.wtItem = w.Item
					break
				}
			}
		}
	}
	s.reindex()
	s.mcp = mcp.NewServer(&mcp.Implementation{
		Name:    "spectackle",
		Title:   "spectackle — spec-driven cross-language code intelligence",
		Version: Version,
	}, &mcp.ServerOptions{Instructions: manifest()})
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

// BuildGraph runs the full index pipeline — hand-written parsers first,
// then every langspec-registered language (adding a language is one data
// file in internal/langspec, no wiring), a syntactic pass over root, then
// the M3 go/types-resolved call-edge upgrade pass — and returns the
// resulting graph, the syntactic Stats, and the typed-call pass's outcome.
//
// Exported and factored out of (*Server).reindex so the `spectackle
// reindex` CLI subcommand (cmd/spectackle/main.go) can rebuild the exact
// same symbol graph drift classification depends on, instead of silently
// only resyncing the spec/doc cache while claiming to "reindex" (DEFECT 3 /
// contract-pending T-0107): the two call sites sharing this one function is
// what keeps them from drifting onto two different parser/resolver lists.
func BuildGraph(ctx context.Context, ws workspace.Root, blobs store.Store) (graph.Graph, index.Stats, TypedPassState, error) {
	g := graph.NewMem()
	parsers := append(
		[]index.LanguageParser{index.GoParser{}, index.AsmParser{}, index.CudaParser{}},
		langspec.All()...)
	ix := index.New(g, blobs, parsers,
		resolve.Default().All(), ws.Cfg.Ignore...)
	st, err := ix.IndexAll(ctx, ws.Dir)
	if err != nil {
		return g, st, TypedPassState{}, err
	}
	// M3 upgrade pass: go/types-resolved call edges (chained selectors,
	// cross-package methods). Best-effort — a broken module tree must not
	// take the syntactic graph down with it. Before issue 28 this failure
	// stopped at the log line below: a resident server driven over
	// stdio/HTTP (or the `call` subcommand) never sees stderr, so state/check
	// answered as if the graph were complete. tp carries the same
	// information out to both tools (see TypedPassState's doc comment,
	// state.go) instead of just this process's log.
	typed, tErr := index.ResolveTypedCalls(ctx, g, ws.Dir, blobs)
	tp := TypedPassState{Added: typed}
	if tErr != nil {
		log.Printf("index: typed calls: %v (syntactic graph only)", tErr)
		tp.Cause = tErr.Error()
		var tpErr *index.TypedPassError
		if errors.As(tErr, &tpErr) {
			tp.Packages = tpErr.Packages
		}
	}
	return g, st, tp, nil
}

// reindex rebuilds the symbol graph from the active root (startup and after
// every reroot — worktree code diverges from main). A failed index run keeps
// the previous graph: lifecycle tools must survive unparseable trees.
//
// indexedAt is captured BEFORE the walk starts, not after it finishes: a
// file edited while (or immediately after) the walk is in flight must still
// be treated as newer than this graph by check's staleness guard (DRF-003)
// — using the start time is the conservative bound, using the finish time
// would create a race window where such an edit looks indexed when it isn't.
func (s *Server) reindex() {
	t0 := time.Now()
	g, st, tp, err := BuildGraph(context.Background(), s.ws, s.blobs)
	if err != nil {
		log.Printf("index: %v (keeping previous graph)", err)
		return
	}
	s.g = g
	s.indexedAt = t0
	s.typedPass = tp
	log.Printf("index: %d files, %d nodes, %d edges (+%d typed calls, %d skipped)",
		st.Files, st.Nodes, st.Edges, tp.Added, st.Skipped)
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

// SetSelfRestart marks the committed-only watcher active (see selfRestartOn).
func (s *Server) SetSelfRestart() { s.selfRestartOn = true }

// Busy reports whether any tool call is currently in flight — the
// self-restart watcher defers swaps until quiet (B-01KYF7).
func (s *Server) Busy() bool { return s.inFlight.Load() > 0 }

// rerootBack answers where submit/abort return the active workspace after
// tearing down the task worktree at removedRoot: the process's home root —
// UNLESS home IS the worktree being removed (a per-call CLI process started
// with -root inside the task worktree, the B-0002 rebind case), where the
// only surviving root is main (B-01KYEPC9SJE23V67A9YS9XBZFH).
func (s *Server) rerootBack(removedRoot string) string {
	if filepath.Clean(s.home) == filepath.Clean(removedRoot) {
		// The home directory is about to be deleted; main is the only
		// surviving root and it becomes the new home PERMANENTLY. Returning
		// main for just this one call left s.home naming a dead path, and
		// the next cycle's teardown handed that path to reroot, whose
		// EnsureScaffold silently resurrected it as an empty non-git ghost
		// workspace the server then served (adversarial round 1 on this
		// fix; TestWorktreeSubmitEndToEndOffPrimaryBranch caught it).
		s.home = s.main.Dir
	}
	return s.home
}

// reroot swaps the active workspace (main <-> worktree), swapping the local
// index cache and scanner with it. The coordination DB always stays on main.
func (s *Server) reroot(dir, item string) error {
	// Lock: s.cd, same as New — a re-rooted Root writes item/spec files too
	// (see s.ws call sites throughout mcpserver) and must stay serialized
	// against siblings exactly like the root it replaces.
	nws := workspace.Root{Dir: dir, Agent: s.agent, Cfg: s.main.Cfg, Lock: s.cd}
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
