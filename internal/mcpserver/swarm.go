package mcpserver

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jxsl13/spectackle/internal/coord"
	"github.com/jxsl13/spectackle/internal/item"
	"github.com/jxsl13/spectackle/internal/journal"
	"github.com/jxsl13/spectackle/internal/lifecycle"
	"github.com/jxsl13/spectackle/internal/replay"
	"github.com/jxsl13/spectackle/internal/workspace"
	"github.com/jxsl13/spectackle/internal/wt"
)

// ---- swarm tool inputs ----

type leaseIn struct {
	Op    string   `json:"op" jsonschema:"claim|release|ls"`
	Paths []string `json:"paths,omitempty" jsonschema:"repo-relative dirs/files or item IDs (claim/release)"`
	Item  string   `json:"item,omitempty" jsonschema:"item the claim belongs to"`
	TTL   int      `json:"ttl,omitempty" jsonschema:"lease seconds, default from config (600)"`
}

type workIn struct {
	Op   string `json:"op" jsonschema:"start|submit|abort|status"`
	Item string `json:"item,omitempty" jsonschema:"item ID; required for start, defaults to own active item"`
}

type swarmIn struct{}

// ---- gate plumbing ----

// preCall runs under the mutex before every handler: heartbeat, lease
// refresh, throttled stale-agent sweep, cache sync.
func (s *Server) preCall() error {
	if err := s.cd.Heartbeat(); err != nil {
		return err
	}
	if err := s.cd.RefreshLeases(s.leaseTTL()); err != nil {
		return err
	}
	if time.Since(s.lastSweep) > 30*time.Second {
		s.lastSweep = time.Now()
		if _, err := s.cd.Sweep(s.agentTTL()); err != nil {
			return err
		}
	}
	return s.refreshLocked()
}

// refreshLocked is the part of preCall that every entry point needs, tool or
// not: drop the memoized ID scope and bring the index up to date. Prompt
// handlers bypass gate() and take s.mu themselves (see prompts.go), so they
// call this instead of s.scan.Refresh directly.
//
// Dropping the scope here covers the case markDirty cannot see: a SIBLING
// process writing work.md between two of our calls. Our own writes invalidate
// it at the write itself.
func (s *Server) refreshLocked() error {
	s.scCache = nil
	return s.scan.Refresh()
}

// markDirty records that this call changed the workspace: the index needs a
// rescan, and the memoized ID scope is now stale.
//
// Invalidating at the write rather than at the call boundary is what makes
// the memo safe for the handlers that call each other directly instead of
// going through gate() — the grill/draft tests do, and so does every future
// composite handler. A scope value already pulled into a local variable is a
// copy and survives this untouched, which is what draft and move rely on to
// render a just-minted ID against the peer set as it was before the mint.
func (s *Server) markDirty() {
	s.scCache = nil
	s.scan.MarkDirty()
}

// postCall prepends unseen sibling learnings (sw records) and a proactive
// compact-due hint (c record, T-0093) to a text result — the piggyback
// realtime channel. Either block may be empty; the result is only touched
// when at least one of them has something to say.
func (s *Server) postCall(res *mcp.CallToolResult) *mcp.CallToolResult {
	if res == nil || len(res.Content) == 0 {
		return res
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		return res
	}
	var b strings.Builder
	if cursor, err := s.cd.Cursor(); err == nil {
		if events, err := s.cd.After(cursor, 6); err == nil && len(events) > 0 {
			shown := events
			overflow := 0
			if len(shown) > 5 {
				shown, overflow = shown[:5], len(events)-5
			}
			last := cursor
			for _, e := range shown {
				b.WriteString(swLine(e) + "\n")
				last = e.Seq
			}
			if overflow > 0 {
				fmt.Fprintf(&b, "sw more n=%d (see swarm)\n", overflow)
			}
			_ = s.cd.SetCursor(last)
		}
	}
	if hint := s.compactHint(); hint != "" {
		b.WriteString(hint + "\n")
	}
	if hint := s.staleHint(); hint != "" {
		b.WriteString(hint + "\n")
	}
	if b.Len() == 0 {
		return res
	}
	tc.Text = b.String() + tc.Text
	return res
}

// compactHint returns a "c . journal <n> events since last compact" record
// — the exact shape compactCandidates produces for the root context — once
// the root journal crosses Compact.JournalMax, so the nudge surfaces on ANY
// tool result instead of waiting for an explicit `check` call. Empty string
// = nothing to say right now.
//
// CHEAP: the journal is only re-counted at most once per 30s (postCall runs
// under s.mu, same as preCall, so no extra locking is needed for the cached
// fields on Server) — every other call within the window reuses the cached
// count instead of re-reading the journal file.
//
// QUIET: once surfaced at a given count, the hint stays silent on every
// following call until the count either drops back below the threshold (a
// compact ran — s.hintedAt resets to 0, "armed") or grows by another full
// threshold since the count it was last surfaced at (still noisy growth
// worth re-flagging even without a compact in between).
func (s *Server) compactHint() string {
	if time.Since(s.lastCompactCheck) > 30*time.Second {
		s.lastCompactCheck = time.Now()
		s.compactCount = rootJournalSince(s.ws)
	}
	max := s.ws.Cfg.Compact.JournalMax
	n := s.compactCount
	if max <= 0 || n < max {
		s.hintedAt = 0 // below threshold (or freshly compacted): re-armed
		return ""
	}
	if s.hintedAt != 0 && n < s.hintedAt+max {
		return "" // already surfaced this crossing, no further growth yet
	}
	s.hintedAt = n
	return fmt.Sprintf("c . journal %d events since last compact", n)
}

// rootJournalSince mirrors compactCandidates' per-context journal count
// (tools.go), narrowed to the root context (".") only — the cheapest read
// that still serves a proactive, on-every-call hint.
func rootJournalSince(ws workspace.Root) int {
	events, err := journal.Read(ws, "")
	if err != nil {
		return 0
	}
	since := 0
	for _, e := range events {
		if e.Ev == journal.EvCompact {
			since = 0
			continue
		}
		since++
	}
	return since
}

// staleCheckInterval bounds how often staleHint re-walks the workspace tree
// to refresh its verdict. A full walk per tool call is unacceptable (this
// runs on every call, same as the compact hint); the verdict only needs to
// be as fresh as the time it takes an operator to notice and run `make
// dev`, so the same 30s cadence as the compact-hint cache and the
// stale-agent sweep is more than tight enough — this is a debounce, not a
// correctness requirement.
const staleCheckInterval = 30 * time.Second

// staleHint returns a "h . binary stale ..." record naming the rebuild
// command (MCP-010) once the running executable is older than the newest
// .go file under s.ws.Dir — the resident server is answering from code the
// tree has moved past. Debounced like compactHint: the walk itself is
// cached and refreshed at most once per staleCheckInterval, and once
// surfaced for a given crossing the hint stays silent (staleHinted) until
// the verdict drops back to "not stale" (a rebuild ran — re-armed) and
// crosses again. Empty string = nothing to say right now.
//
// Gated by staleEligible BEFORE any of the above: the advice is only
// followable on a dev build of spectackle serving spectackle's own tree
// (issue #29 — a released binary answering ANY tree, which is what every
// installed user runs, fired this on every call, telling them to run a
// Makefile target their tree does not have). Checking eligibility first
// means an ineligible binary never pays for binaryStale's walk either.
// BinaryStale exposes the staleness verdict to the serve loop (T-01KYEH):
// the self-restart watcher polls it to decide when a rebuild-and-exec is
// due. It deliberately mirrors staleHint BYTE FOR BYTE — same s.ws root,
// under the same lock — because a first draft judged s.main instead and
// never read stale while the hint on the identical process did; whatever
// the two Root values diverge in, the hint path is the one proven live.
// Undebounced by design: the caller owns its own cadence.
func (s *Server) BinaryStale() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return staleEligible(s.ws) && binaryStale(s.ws)
}

// AgentName is this server's swarm identity. The self-restart exec must
// carry it explicitly in the environment: defers never run across
// syscall.Exec, so Deregister is skipped, and a replacement that
// re-generates a random name (coord.GenName when SPECTACKLE_AGENT is
// unset) turns its own pre-swap leases into a dead foreign holder that
// blocks it and every sibling until the TTL sweep — found by adversarial
// review of the first self-restart draft (T-01KYEH). With the identity
// carried over, the skipped Deregister is harmless: the same row upserts
// and the leases are legitimately retained.
func (s *Server) AgentName() string { return s.agent }

// ServedDir is the root of the tree currently being served — the tree
// BinaryStale judges, and therefore the tree the self-restart rebuild must
// compile. The distinction from s.main.Dir is load-bearing: a server
// started inside a linked git worktree resolves main to the PRIMARY
// checkout, whose sources may lack exactly the changes that made the
// binary stale — the first live swap rebuilt there and exec'd a
// replacement that did not know its own flags.
func (s *Server) ServedDir() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ws.Dir
}

func (s *Server) staleHint() string {
	if !staleEligible(s.ws) {
		return ""
	}
	if time.Since(s.lastStaleCheck) > staleCheckInterval {
		s.lastStaleCheck = time.Now()
		s.staleVerdict = binaryStale(s.ws)
	}
	if !s.staleVerdict {
		s.staleHinted = false // not stale (or freshly rebuilt): re-armed
		return ""
	}
	if s.staleHinted {
		return "" // already surfaced this crossing
	}
	s.staleHinted = true
	return "h . binary stale — rebuild+restart: make dev"
}

// staleEligible reports whether staleHint's "make dev" advice could
// possibly be actionable. BOTH must hold:
//   - isDevBuild: this binary is itself an unreleased development build,
//     not a stamped release — a released binary can't act on "make dev"
//     regardless of whose tree it is serving.
//   - servesOwnModule: the tree being served is spectackle's OWN module —
//     a dev build pointed at some other repository can't run spectackle's
//     Makefile against it either.
//
// Neither alone suffices (see the doc comments below); this is exactly
// the pair of conditions issue #29 identified as missing, checked once,
// cheaply, before binaryStale's directory walk runs at all.
func staleEligible(ws workspace.Root) bool {
	return isDevBuild() && servesOwnModule(ws)
}

// isDevBuild reports whether Version still reads as an unstamped
// development build rather than a released tag. A release build injects a
// bare semver tag via -ldflags (see server.go's Version comment); the
// compiled-in default, and every dev or test binary that never runs that
// ldflags step, keeps the "-dev" suffix — so a substring check is the
// same distinction that comment already documents, just made queryable.
func isDevBuild() bool {
	return strings.Contains(Version, "dev")
}

// servesOwnModule reports whether ws is spectackle's own checkout, judged
// by its go.mod module directive against modulePath. This deliberately
// reads the SERVED workspace's go.mod from disk rather than reusing
// moduleRepoURL/debug.ReadBuildInfo: build info describes what this
// binary was compiled FROM, which is fixed at compile time, while s.ws
// can be re-rooted onto any repository on disk (work op=start moves it to
// a worktree, and nothing stops that worktree from belonging to a
// different module than the running binary) — the only fact that can
// answer "whose tree is this" is the tree itself.
//
// A go.mod that can't be read, or that doesn't declare a module (no Go
// project at ws.Dir, or a monorepo subdir whose go.mod lives elsewhere),
// degrades to false, not true: ownership defaults to unconfirmed, and an
// unconfirmed tree gets silence, not a hint — the same direction every
// other failure branch around staleness already degrades (see
// binaryStale's doc comment).
func servesOwnModule(ws workspace.Root) bool {
	path, ok := readModulePath(ws.Dir)
	return ok && path == modulePath
}

// readModulePath extracts the module directive from dir/go.mod with a raw
// line scan rather than golang.org/x/mod/modfile: that package is today
// only an indirect dependency (pulled in transitively, not by this repo's
// own code), and reading one directive line does not earn promoting it to
// a direct one — typespass.go already reads go.mod's raw bytes for its
// cache key for the identical reason.
func readModulePath(dir string) (string, bool) {
	raw, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return "", false
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
			return strings.TrimSpace(rest), true
		}
	}
	return "", false
}

// execPath resolves the running executable's path. A package var (not a
// direct os.Executable call) purely so tests can simulate os.Executable
// failing without needing a genuinely unresolvable binary in-process;
// production wiring is exactly os.Executable, nothing more.
var execPath = os.Executable

// binaryStale reports whether the running executable (os.Executable,
// symlinks resolved — a dev binary invoked via a symlink is common) is
// older than the newest .go file under ws.Dir. It walks with ws.SkipDir,
// the same skip logic every other workspace walk uses, so vendored trees,
// nested git boundaries and ignored directories are pruned exactly as
// elsewhere — a naive walk would descend into worktrees and cache
// directories and report nonsense. It stops at the first newer file found
// (filepath.SkipAll): the caller needs a boolean, not a maximum age.
//
// Any failure along the way — os.Executable, an unresolvable symlink, an
// unstattable binary — degrades to false (not stale). Staleness is
// informational; it must never turn into an error on a tool call.
func binaryStale(ws workspace.Root) bool {
	exe, err := execPath()
	if err != nil {
		return false
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return false
	}
	bin, err := os.Stat(exe)
	if err != nil {
		return false
	}
	binTime := bin.ModTime()
	stale := false
	_ = filepath.WalkDir(ws.Dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable entry: skip it, don't fail the walk
		}
		if d.IsDir() {
			rel, _ := filepath.Rel(ws.Dir, p)
			rel = filepath.ToSlash(rel)
			if rel == "." {
				rel = ""
			}
			if ws.SkipDir(rel, d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil // unstattable file: skip it, don't fail the walk
		}
		if info.ModTime().After(binTime) {
			stale = true
			return filepath.SkipAll
		}
		return nil
	})
	return stale
}

func swLine(e coord.Event) string {
	ref := e.Ref
	if ref == "" {
		ref = "-"
	}
	return fmt.Sprintf("sw %d %s %s %s %s", e.Seq, e.Agent, e.Ev, ref, e.Msg)
}

// blockedByLease enforces foreign leases on server-mediated writes.
func (s *Server) blockedByLease(paths []string) (*mcp.CallToolResult, any, error) {
	var check []string
	for _, p := range paths {
		if p != "" && p != "." {
			check = append(check, p)
		}
	}
	if len(check) == 0 {
		return nil, nil, nil
	}
	l, err := s.cd.Blocked(check, s.agentTTL())
	if err != nil {
		return nil, nil, err
	}
	if l == nil {
		return nil, nil, nil
	}
	return refuse(fmt.Sprintf("! LEASE E %s held=%s item=%s exp=%s — pick different scope or coordinate",
		l.Path, l.Agent, orDash(l.Item), leaseLeft(l.Exp)))
}

// minter routes ID minting through the swarm counters (collision-free
// across parallel worktrees).
func (s *Server) minter() lifecycle.Minter {
	return func(kind string, floor int) (int, error) { return s.cd.NextID(kind, floor) }
}

func (s *Server) ruleMinter() func(stem string, floor int) (int, error) {
	return func(stem string, floor int) (int, error) { return s.cd.NextID("rule:"+stem, floor) }
}

// ---- lease tool ----

func (s *Server) lease(in leaseIn) (*mcp.CallToolResult, any, error) {
	switch in.Op {
	case "claim":
		if len(in.Paths) == 0 {
			return refuse("! ARG E - claim requires paths")
		}
		ttl := s.leaseTTL()
		if in.TTL > 0 {
			ttl = time.Duration(in.TTL) * time.Second
		}
		conflict, err := s.cd.Claim(normalizeTargets(in.Paths), in.Item, ttl, s.agentTTL())
		if err != nil {
			return nil, nil, err
		}
		if conflict != nil {
			return refuse(fmt.Sprintf("! LEASE E %s held=%s item=%s exp=%s",
				conflict.Path, conflict.Agent, orDash(conflict.Item), leaseLeft(conflict.Exp)))
		}
		_ = s.cd.Emit("claim", in.Item, strings.Join(in.Paths, " "))
		return text("ok claimed " + strings.Join(in.Paths, " "))
	case "release":
		if err := s.cd.Release(in.Paths); err != nil {
			return nil, nil, err
		}
		_ = s.cd.Emit("release", in.Item, strings.Join(in.Paths, " "))
		return text("ok released")
	case "ls":
		leases, err := s.cd.Leases(s.agentTTL())
		if err != nil {
			return nil, nil, err
		}
		if len(leases) == 0 {
			return text("ok no live leases")
		}
		var b strings.Builder
		for _, l := range leases {
			fmt.Fprintf(&b, "l %s %s %s %s\n", l.Path, l.Agent, orDash(l.Item), leaseLeft(l.Exp))
		}
		return text(b.String())
	}
	return refuse("! ARG E - op must be claim|release|ls")
}

// ---- swarm tool ----

func (s *Server) swarm(swarmIn) (*mcp.CallToolResult, any, error) {
	var b strings.Builder
	agents, err := s.cd.Agents()
	if err != nil {
		return nil, nil, err
	}
	for _, a := range agents {
		wtLabel := "main"
		if a.WT != "" {
			wtLabel = a.WT
		}
		me := ""
		if a.Name == s.agent {
			me = " (you)"
		}
		fmt.Fprintf(&b, "ag %s %s %s %s%s\n", a.Name, orDash(a.Item), hbAge(a.HB), wtLabel, me)
	}
	leases, err := s.cd.Leases(s.agentTTL())
	if err != nil {
		return nil, nil, err
	}
	for _, l := range leases {
		fmt.Fprintf(&b, "l %s %s %s %s\n", l.Path, l.Agent, orDash(l.Item), leaseLeft(l.Exp))
	}
	wts, err := s.cd.Worktrees()
	if err != nil {
		return nil, nil, err
	}
	for _, w := range wts {
		fmt.Fprintf(&b, "wt %s %s %s %s\n", w.Item, w.State, w.Agent, w.Root)
	}
	events, err := s.cd.SearchEvents("", nil, 10)
	if err != nil {
		return nil, nil, err
	}
	for _, e := range events {
		b.WriteString(swLine(e) + "\n")
	}
	if b.Len() == 0 {
		return text("ok swarm empty")
	}
	return text(b.String())
}

// ---- work tool ----

// reattachOwnWorktree re-roots a fresh process onto its OWN open worktree
// before submit or abort (B-01KYE8): the open-worktree state lives in
// process memory, every CLI call is its own process, and the B-0002 startup
// rebind only fires when the process was started with -root inside the
// worktree — which nothing taught headless callers to do. Identity gates
// the reattach exactly like B-0002: only a worktree recorded for THIS agent
// qualifies; adopting a dead sibling's stays an explicit abort decision.
func (s *Server) reattachOwnWorktree(id string) {
	if s.wtItem != "" || id == "" {
		return
	}
	// The caller may hold a display prefix; the worktree ledger is keyed by
	// the full ID — resolve with the same two legs workStart uses: serving
	// root first, then main. Serving-only misses main by design; a MAIN
	// item held by a restarted worktree-homed server misses the serving
	// root, and a ws-only read here dead-ended exactly that reattach — the
	// restarted server's status listed the worktree submit then refused
	// (adversarial round 3). Every worktree-flow item is main-resolvable
	// by the workStart gate, so the fallback loses nothing.
	it, ok, err := item.Get(s.ws, id)
	if err != nil {
		return
	}
	if !ok {
		if it, ok, err = item.Get(s.main, id); err != nil || !ok {
			return
		}
	}
	if w, ok, err := s.cd.GetWorktree(it.ID); err == nil && ok && w.Agent == s.agent {
		// Ledger rows written by pre-canonicalization binaries may carry an
		// alias spelling; trusting it verbatim defeats rerootBack's rehome
		// during the migration window (round 4). Canonicalize defensively.
		root := w.Root
		if r, rerr := filepath.EvalSymlinks(root); rerr == nil {
			root = r
		}
		if err := s.reroot(root, w.Item); err == nil {
			s.wtItem = w.Item
		}
	}
}

func (s *Server) work(in workIn) (*mcp.CallToolResult, any, error) {
	switch in.Op {
	case "start":
		return s.workStart(in.Item)
	case "submit":
		s.reattachOwnWorktree(in.Item)
		return s.workSubmit(in.Item)
	case "abort":
		s.reattachOwnWorktree(in.Item)
		return s.workAbort(in.Item)
	case "status":
		wts, err := s.cd.Worktrees()
		if err != nil {
			return nil, nil, err
		}
		if len(wts) == 0 {
			return text("ok no open worktrees")
		}
		var b strings.Builder
		for _, w := range wts {
			fmt.Fprintf(&b, "wt %s %s %s %s\n", w.Item, w.State, w.Agent, w.Root)
		}
		return text(b.String())
	}
	return refuse("! ARG E - op must be start|submit|abort|status")
}

func (s *Server) workStart(id string) (*mcp.CallToolResult, any, error) {
	if s.wtItem != "" {
		return refuse("! WT E already in worktree for " + s.wtItem + " — submit or abort first")
	}
	if id == "" {
		return refuse("! ARG E - start requires item")
	}
	if !wt.IsRepo(s.main.Dir) {
		return refuse("! WT E workspace is not a git repository")
	}
	// Resolve against the SERVING workspace — where state answers from. A
	// server homed in a linked worktree carries live records s.main has
	// never seen; resolving there made an item state showed as active
	// refuse here as unknown (B-01KYEPC9SJE23V67A9YS9XBZFH, reproduced
	// live). Resolution is the ONLY serving-root concern in this flow: the
	// worktree flow forks from, seeds from, and replays into main, so an
	// item main has never seen is refused TRUTHFULLY below instead of
	// being started — forking a main-integrated worktree for a
	// serving-branch-only item corrupts main's records at submit (replay
	// baselines assume main seeding; adversarial round 1 on this fix).
	it, ok, err := item.Get(s.ws, id)
	if err != nil {
		return nil, nil, err
	}
	if ok && s.ws.Dir != s.main.Dir {
		mainIt, mainOK, err := item.Get(s.main, it.ID)
		if err != nil {
			return nil, nil, err
		}
		if !mainOK {
			return refuse("! WT E " + it.ID + " lives only on this serving root — the worktree flow integrates into main, which has never seen it; drive it with move from here, or land the serving branch first")
		}
		it = mainIt
	} else if !ok {
		// The serving root does not know it; main may (a worktree-homed
		// server starting a main item — the flow's own record space).
		it, ok, err = item.Get(s.main, id)
		if err != nil {
			return nil, nil, err
		}
		if !ok {
			return refuse("! ARG E - unknown item " + id)
		}
	}
	if it.State != item.StateApproved && it.State != item.StateActive {
		// Name WHICH root's state this is: for a worktree-homed server the
		// gate adopts main's copy, and main can lag the serving branch (the
		// item approved there but still draft on main) — a bare "item is
		// draft" would contradict this session's own get (round 2 finding).
		where := ""
		if s.ws.Dir != s.main.Dir {
			where = " on main"
		}
		return refuse("! ARG E - item is " + it.State + where + "; work needs approved|active")
	}
	// orphaned worktree from a crashed sibling?
	adoptWarn := ""
	if w, exists, _ := s.cd.GetWorktree(id); exists {
		if holderAlive(s, w.Agent) && w.Agent != s.agent {
			return refuse("! WT E worktree for " + id + " open by live agent " + w.Agent)
		}
		_ = wt.Remove(s.main.Dir, w.Root)
		// A discard failure must be SAID (B-01KYEEJKE): the surviving
		// branch is what the attach below will resume, silently changing
		// what "a fresh start" means.
		if err := wt.DiscardBranch(s.main.Dir, w.Branch, s.gitBase()); err != nil {
			adoptWarn = "! WT W branch " + w.Branch + " not deleted (" + err.Error() + ") — the start below resumes its surviving commits\n"
		}
		_ = s.cd.DelWorktree(id)
	}
	// lease item + targets, all-or-nothing
	scopes := append([]string{id}, normalizeTargets(it.Targets)...)
	conflict, err := s.cd.Claim(scopes, id, s.leaseTTL(), s.agentTTL())
	if err != nil {
		return nil, nil, err
	}
	if conflict != nil {
		return refuse(fmt.Sprintf("! LEASE E %s held=%s item=%s", conflict.Path, conflict.Agent, orDash(conflict.Item)))
	}

	base, err := wt.Head(s.main.Dir)
	if err != nil {
		return nil, nil, err
	}
	branch := "spectackle/" + id
	root := filepath.Join(s.main.WtDir(), id)
	if err := wt.Add(s.main.Dir, root, branch, "HEAD", s.gitBase()); err != nil {
		_ = s.cd.ReleaseItem(id)
		return refuse("! WT E " + err.Error())
	}
	// The ledger spelling must be directory identity, not the config's: a
	// worktrees_dir override that traverses a symlink would store a w.Root
	// nothing canonical ever matches — the rebind and rerootBack compare
	// against it (adversarial round 3). Resolvable only now that Add
	// created the directory.
	if r, err := filepath.EvalSymlinks(root); err == nil {
		root = r
	}
	// carry main's LIVE .spectackle state into the worktree (main bundles may
	// be ahead of HEAD — the server never commits them itself). Main, not
	// the serving root: the submit replay baselines against main's committed
	// journal at the fork point, and seeding from a serving root's live
	// bundles makes already-integrated sibling events reappear in the next
	// item's delta (adversarial round 1 on B-01KYEPC9SJE23V67A9YS9XBZFH).
	if err := copyBundles(s.main, root); err != nil {
		return nil, nil, err
	}
	if err := s.cd.PutWorktree(coord.Worktree{
		Item: id, Agent: s.agent, Branch: branch, Root: root, Base: base, State: "open",
	}); err != nil {
		return nil, nil, err
	}
	if err := journal.Append(s.main, it.Dir, journal.Event{Ev: journal.EvStart, ID: id, Dir: it.Dir,
		Note: "branch " + branch + " base " + base[:12]}); err != nil {
		return nil, nil, err
	}
	_ = s.cd.Emit("start", id, it.Title)
	if err := s.reroot(root, id); err != nil {
		return nil, nil, err
	}
	// activate inside the worktree so the state travels with the replay
	if it.State == item.StateApproved {
		if _, err := lifecycle.Move(s.ws, id, item.StateActive, ""); err != nil {
			return nil, nil, err
		}
	}
	// The ID is emitted in display form like every other record ID; the root
	// path beside it necessarily still carries the full one, since that is
	// the directory actually on disk.
	sc, err := s.idScope()
	if err != nil {
		return nil, nil, err
	}
	return text(adoptWarn + fmt.Sprintf("wt %s open %s\nok edit/build/bench ONLY under this root; check until ok, then work op=submit item=%s (any process)", sc.short(id), root, sc.short(id)))
}

func (s *Server) workSubmit(id string) (*mcp.CallToolResult, any, error) {
	if id == "" {
		id = s.wtItem
	} else if it, ok, err := item.Get(s.ws, id); err == nil && ok {
		// The caller may hold a display prefix; s.wtItem is the full ID.
		// The serving workspace resolves (B-01KYEPC9SJE23V67A9YS9XBZFH):
		// after reattach it is the task worktree, whose bundles carry the
		// item; s.main may never have seen it.
		id = it.ID
	}
	if id == "" || id != s.wtItem {
		return refuse("! ARG E - no open worktree for " + orDash(id))
	}
	w, ok, err := s.cd.GetWorktree(id)
	if err != nil || !ok {
		return refuse("! WT E worktree record missing — work op=abort and restart")
	}
	it, _, err := item.Get(s.ws, id)
	if err != nil {
		return nil, nil, err
	}

	// a previous submit already escalated this item — decide first, the
	// gate stays unrun (it would only pile up more rounds against a budget
	// that is already exhausted)
	if it.State == item.StateBlocked {
		sc, err := s.idScope()
		if err != nil {
			return nil, nil, err
		}
		return text(fmt.Sprintf("i %s blocked rounds=%d/%d — decide %s",
			sc.short(it.ID), it.Rounds, s.maxRounds(), sc.short(lastNeed(it.Needs))))
	}

	// GATE 1: verify + goal on the worktree tree
	if res := s.runGate(it.Goal); res != "" {
		return s.gateFail(it, res)
	}
	_ = s.cd.PutWorktree(withState(w, "gating"))
	if _, err := wt.CommitCode(s.ws.Dir, "spectackle "+id+": "+it.Title); err != nil {
		return refuse("! WT E commit: " + err.Error())
	}

	// INTEGRATE under the global lock
	ok, holder, err := s.cd.LockIntegrate(2 * time.Minute)
	if err != nil {
		return nil, nil, err
	}
	if !ok {
		return refuse("! LOCK W integrate held by " + holder + " — retry submit shortly")
	}
	defer s.cd.UnlockIntegrate()
	_ = s.cd.PutWorktree(withState(w, "integrating"))

	conflicts, err := wt.MergeMain(s.ws.Dir)
	if err != nil {
		return refuse("! WT E merge: " + err.Error())
	}
	if len(conflicts) > 0 {
		_ = s.cd.PutWorktree(withState(w, "conflict"))
		return refuse("! WT E conflict " + strings.Join(conflicts, " ") + "\nok resolve these files in your worktree, then work op=submit again")
	}
	// GATE 2: the tree that merges is the tree that was tested
	if res := s.runGate(it.Goal); res != "" {
		return s.gateFail(it, res)
	}
	touched, _ := wt.TouchedFiles(s.main.Dir, w.Base, w.Branch)
	if overlap := wt.DirtyOverlap(s.main.Dir, touched); len(overlap) > 0 {
		return refuse("! WT E main-dirty " + strings.Join(overlap, " ") + " — commit/stash these in the main checkout, then submit again")
	}
	if err := wt.FFMainPreservingRecords(s.main.Dir, w.Branch); err != nil {
		return refuse("! WT E ff-merge: " + err.Error())
	}

	// REPLAY .spectackle state semantically
	_ = s.cd.PutWorktree(withState(w, "replaying"))
	rep, err := replay.Run(s.main, s.ws, id, w.Base, s.cd, s.g)
	if err != nil {
		return refuse("! WT E replay: " + err.Error() + "\nok fix and work op=submit again (replay resumes idempotently)")
	}
	if err := journal.Append(s.main, it.Dir, journal.Event{Ev: journal.EvSubmit, ID: id, Dir: it.Dir,
		Note: fmt.Sprintf("branch %s: %d events, %d rules, %d items replayed", w.Branch, rep.Events, rep.Rules, len(rep.Items))}); err != nil {
		return nil, nil, err
	}
	_ = s.cd.Emit("submit", id, it.Title)

	// teardown + back to the process's home root (branch kept for audit) —
	// main only when home IS the dying worktree (B-01KYEPC9SJE23V67A9YS9XBZFH)
	wtRoot := s.ws.Dir
	if err := s.reroot(s.rerootBack(wtRoot), ""); err != nil {
		return nil, nil, err
	}
	_ = wt.Remove(s.main.Dir, wtRoot)
	_ = s.cd.DelWorktree(id)
	_ = s.cd.ReleaseItem(id)

	var b strings.Builder
	// A serving root that is not the primary checkout received none of the
	// replay: the integration landed in main's records, and this root only
	// sees it after its own fetch/checkout. Saying so beats a state call
	// that suddenly disagrees with the submit result (nothing is silent).
	if s.ws.Dir != s.main.Dir {
		fmt.Fprintf(&b, "! WT W serving root %s shows pre-submit state until it syncs with main\n", s.ws.Dir)
	}
	// A detached main checkout means the fast-forward advanced no named
	// branch (B-01KYEEJKQ): reachable only when vacateBranch found no base
	// at all, and self-healing at archive (the branch merges into the real
	// base via the PR or the offline double) — but claiming "merged to
	// main" while it lasts is a lie, and the deferral must be said.
	if cur, cerr := wt.CurrentBranch(s.main.Dir); cerr == nil && cur == "HEAD" {
		fmt.Fprintf(&b, "ok %s integrated on a detached head (%d events, %d rules replayed) — no named branch advanced; the archive merge lands it on the base\n", id, rep.Events, rep.Rules)
	} else {
		fmt.Fprintf(&b, "ok %s merged to main (%d events, %d rules replayed)\n", id, rep.Events, rep.Rules)
	}
	for from, to := range rep.Remap {
		fmt.Fprintf(&b, "sw 0 %s remap %s replayed as %s\n", s.agent, from, to)
	}
	// The follow-up instruction must be one THIS session can execute: a
	// worktree-homed server's move runs against its serving root, which
	// does not carry the just-replayed item — "move to=done" here answers
	// unknown item, the exact tool-disagreement class this fix closes
	// (adversarial round 2). Point at a root that can see it instead.
	if s.ws.Dir != s.main.Dir {
		b.WriteString("i " + id + " — now on main; move to=done/archived from a main-rooted session (this serving root cannot resolve it until it syncs)\n")
	} else {
		b.WriteString("i " + id + " — now on main; move to=done/archived as appropriate\n")
	}
	return text(b.String())
}

func (s *Server) workAbort(id string) (*mcp.CallToolResult, any, error) {
	if id == "" {
		id = s.wtItem
	} else if it, ok, err := item.Get(s.ws, id); err == nil && ok {
		// serving-workspace resolution, same as start and submit
		// (B-01KYEPC9SJE23V67A9YS9XBZFH)
		id = it.ID
	}
	if id == "" {
		return refuse("! ARG E - abort requires item")
	}
	w, ok, err := s.cd.GetWorktree(id)
	if err != nil {
		return nil, nil, err
	}
	if !ok {
		return refuse("! ARG E - no worktree for " + id)
	}
	if w.Agent != s.agent && holderAlive(s, w.Agent) {
		return refuse("! WT E worktree held by live agent " + w.Agent)
	}
	// A server homed in a linked worktree returns to ITS root after the
	// teardown, not to main — rerootBack also rehomes to main permanently
	// when home is the very worktree being torn down (the B-0002 per-call
	// topology), so no later cycle can resolve back to a deleted path.
	mine := s.wtItem == id
	if mine {
		if err := s.reroot(s.rerootBack(w.Root), ""); err != nil {
			return nil, nil, err
		}
	}
	_ = wt.Remove(s.main.Dir, w.Root)
	// A discard failure must be SAID (B-01KYEEJKE): abort's contract is
	// that the worktree state is thrown away, and a surviving branch is
	// exactly what a later start would silently resume through the attach
	// path. DiscardBranch vacates the main checkout first, so the common
	// checked-out-branch failure now succeeds instead of being swallowed.
	abortWarn := ""
	if err := wt.DiscardBranch(s.main.Dir, w.Branch, s.gitBase()); err != nil {
		abortWarn = "! WT W branch " + w.Branch + " not deleted (" + err.Error() + ") — a later start on this item resumes its surviving commits\n"
	}
	_ = s.cd.DelWorktree(id)
	_ = s.cd.ReleaseItem(id)
	// the item returns to approved on main (its worktree state is discarded;
	// worktree-flow items are main-resolvable by the workStart gate, so main
	// is where the next start looks); its context dir is also where the
	// abort event belongs — passing the item ID here scaffolded a bogus
	// <item-id>/.spectackle dir at the repo root (B-0003; coord.Worktree
	// carries no Dir, so w.Item was reached for)
	dir := ""
	if it, ok, _ := item.Get(s.main, id); ok {
		dir = it.Dir
		if it.State == item.StateActive {
			it.State = item.StateApproved
			if err := item.Upsert(s.main, it); err != nil {
				return nil, nil, err
			}
		}
	}
	if err := journal.Append(s.main, dir, journal.Event{Ev: journal.EvAbort, ID: id,
		Note: "worktree abandoned by " + s.agent}); err != nil {
		return nil, nil, err
	}
	_ = s.cd.Emit("abort", id, "worktree abandoned")
	sc, err := s.idScope()
	if err != nil {
		return nil, nil, err
	}
	// Name the root the rollback landed on when it is not the serving one:
	// a worktree-homed session's own get/state cannot see the item, and a
	// bare "back to approved" reads as the abort having lost it (round 3).
	onMain := ""
	if s.ws.Dir != s.main.Dir {
		onMain = " on main"
	}
	return text(abortWarn + "ok " + sc.short(id) + " aborted; item back to approved" + onMain)
}

// runGate executes the configured verify commands plus the item goal in the
// active root; non-empty return = dense failure record.
func (s *Server) runGate(goal string) string {
	cmds := append([]string{}, s.main.Cfg.Verify...)
	if strings.TrimSpace(goal) != "" {
		cmds = append(cmds, goal)
	}
	for _, c := range cmds {
		cmd := exec.Command("sh", "-c", c)
		cmd.Dir = s.ws.Dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			tail := string(out)
			if len(tail) > 400 {
				tail = tail[len(tail)-400:]
			}
			return fmt.Sprintf("! GATE E %q %v\n%s", c, err, strings.TrimSpace(tail))
		}
	}
	return ""
}

// maxRounds resolves the configured feedback round limit the same way
// lifecycle's unexported maxRounds does (that helper isn't reachable from
// here — package-private), defaulting to 3 for a zero-value Cfg.
func (s *Server) maxRounds() int {
	if s.ws.Cfg.Feedback.MaxRounds > 0 {
		return s.ws.Cfg.Feedback.MaxRounds
	}
	return 3
}

// gateFail records a GATE 1/GATE 2 failure against the item's feedback-round
// budget — the same server-counted mechanic as lifecycle.Move's done->active
// reopen (see lifecycle.ErrRoundsExhausted), just triggered by a gate
// failure instead of a reopen; lifecycle.Move itself is not involved (the
// item's state does not change on a gate fail, only its Rounds counter).
// At the configured limit the item is escalated into item.StateBlocked
// (lifecycle.Escalate mints the adr item that is the only way out)
// instead of just returning the gate output. Either way the worktree is
// left open — work op=abort remains available, and once the block clears
// via decide, submit can be retried.
func (s *Server) gateFail(it item.Item, gateMsg string) (*mcp.CallToolResult, any, error) {
	max := s.maxRounds()
	it.Rounds++
	if err := item.Upsert(s.ws, it); err != nil {
		return nil, nil, err
	}
	if err := journal.Append(s.ws, it.Dir, journal.Event{
		Ev: journal.EvMove, ID: it.ID, Fr: it.State, To: it.State, Dir: it.Dir,
		Note: "gate fail", Rnd: it.Rounds,
	}); err != nil {
		return nil, nil, err
	}
	s.markDirty()
	if it.Rounds < max {
		return text(gateMsg)
	}
	blocked, d, err := lifecycle.Escalate(s.ws, s.minter(), it)
	if err != nil {
		return nil, nil, err
	}
	_ = s.cd.Emit("escalate", blocked.ID, "gate rounds exhausted -> "+d.ID)
	sc, err := s.idScope()
	if err != nil {
		return nil, nil, err
	}
	return text(fmt.Sprintf("i %s blocked rounds=%d/%d — decide %s",
		sc.short(blocked.ID), blocked.Rounds, max, sc.short(d.ID)))
}

// lastNeed mirrors lifecycle's unexported lastNeed (package-private there
// too): the most recently added ID in Needs, "-" if empty.
func lastNeed(needs []string) string {
	if len(needs) == 0 {
		return "-"
	}
	return needs[len(needs)-1]
}

func withState(w coord.Worktree, state string) coord.Worktree { w.State = state; return w }

func holderAlive(s *Server, name string) bool {
	agents, err := s.cd.Agents()
	if err != nil {
		return true // fail safe: assume alive
	}
	for _, a := range agents {
		if a.Name == name {
			return time.Since(a.HB) < s.agentTTL()
		}
	}
	return false
}

// copyBundles mirrors main's live .spectackle bundle files into a fresh
// worktree (whose checkout only has the last committed state).
func copyBundles(main workspace.Root, wtRoot string) error {
	ctxs, err := main.ContextDirs()
	if err != nil {
		return err
	}
	for _, ctx := range ctxs {
		for _, f := range []string{"spec.md", "work.md", "journal.ndjson"} {
			src := filepath.Join(main.SpectackleDir(ctx), f)
			raw, err := os.ReadFile(src)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return err
			}
			dst := filepath.Join(wtRoot, filepath.FromSlash(ctx), workspace.Dot, f)
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(dst, raw, 0o644); err != nil {
				return err
			}
		}
	}
	// anchors are root-only and needed for drift checks in the worktree
	if raw, err := os.ReadFile(main.AnchorsPath()); err == nil {
		dst := filepath.Join(wtRoot, workspace.Dot, "anchors.tsv")
		if err := os.WriteFile(dst, raw, 0o644); err != nil {
			return err
		}
	}
	return nil
}
