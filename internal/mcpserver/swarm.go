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
	return s.scan.Refresh()
}

// postCall prepends unseen sibling learnings (sw records) to a text result —
// the piggyback realtime channel.
func (s *Server) postCall(res *mcp.CallToolResult) *mcp.CallToolResult {
	if res == nil || len(res.Content) == 0 {
		return res
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		return res
	}
	cursor, err := s.cd.Cursor()
	if err != nil {
		return res
	}
	events, err := s.cd.After(cursor, 6)
	if err != nil || len(events) == 0 {
		return res
	}
	var b strings.Builder
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
	tc.Text = b.String() + tc.Text
	return res
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
	return text(fmt.Sprintf("! LEASE E %s held=%s item=%s exp=%ds — pick different scope or coordinate",
		l.Path, l.Agent, orDash(l.Item), int(time.Until(l.Exp).Seconds())))
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
			return text("! ARG E - claim requires paths")
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
			return text(fmt.Sprintf("! LEASE E %s held=%s item=%s exp=%ds",
				conflict.Path, conflict.Agent, orDash(conflict.Item), int(time.Until(conflict.Exp).Seconds())))
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
			fmt.Fprintf(&b, "l %s %s %s %ds\n", l.Path, l.Agent, orDash(l.Item), int(time.Until(l.Exp).Seconds()))
		}
		return text(b.String())
	}
	return text("! ARG E - op must be claim|release|ls")
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
		fmt.Fprintf(&b, "ag %s %s %ds %s%s\n", a.Name, orDash(a.Item), int(time.Since(a.HB).Seconds()), wtLabel, me)
	}
	leases, err := s.cd.Leases(s.agentTTL())
	if err != nil {
		return nil, nil, err
	}
	for _, l := range leases {
		fmt.Fprintf(&b, "l %s %s %s %ds\n", l.Path, l.Agent, orDash(l.Item), int(time.Until(l.Exp).Seconds()))
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

func (s *Server) work(in workIn) (*mcp.CallToolResult, any, error) {
	switch in.Op {
	case "start":
		return s.workStart(in.Item)
	case "submit":
		return s.workSubmit(in.Item)
	case "abort":
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
	return text("! ARG E - op must be start|submit|abort|status")
}

func (s *Server) workStart(id string) (*mcp.CallToolResult, any, error) {
	if s.wtItem != "" {
		return text("! WT E already in worktree for " + s.wtItem + " — submit or abort first")
	}
	if id == "" {
		return text("! ARG E - start requires item")
	}
	if !wt.IsRepo(s.main.Dir) {
		return text("! WT E workspace is not a git repository")
	}
	it, ok, err := item.Get(s.main, id)
	if err != nil {
		return nil, nil, err
	}
	if !ok {
		return text("! ARG E - unknown item " + id)
	}
	if it.State != item.StateApproved && it.State != item.StateActive {
		return text("! ARG E - item is " + it.State + "; work needs approved|active")
	}
	// orphaned worktree from a crashed sibling?
	if w, exists, _ := s.cd.GetWorktree(id); exists {
		if holderAlive(s, w.Agent) && w.Agent != s.agent {
			return text("! WT E worktree for " + id + " open by live agent " + w.Agent)
		}
		_ = wt.Remove(s.main.Dir, w.Root)
		_ = wt.DeleteBranch(s.main.Dir, w.Branch)
		_ = s.cd.DelWorktree(id)
	}
	// lease item + targets, all-or-nothing
	scopes := append([]string{id}, normalizeTargets(it.Targets)...)
	conflict, err := s.cd.Claim(scopes, id, s.leaseTTL(), s.agentTTL())
	if err != nil {
		return nil, nil, err
	}
	if conflict != nil {
		return text(fmt.Sprintf("! LEASE E %s held=%s item=%s", conflict.Path, conflict.Agent, orDash(conflict.Item)))
	}

	base, err := wt.Head(s.main.Dir)
	if err != nil {
		return nil, nil, err
	}
	branch := "spectackle/" + id
	root := filepath.Join(s.main.WtDir(), id)
	if err := wt.Add(s.main.Dir, root, branch, "HEAD"); err != nil {
		_ = s.cd.ReleaseItem(id)
		return text("! WT E " + err.Error())
	}
	// carry main's LIVE .spectackle state into the worktree (main bundles may
	// be ahead of HEAD — the server never commits them itself)
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
	return text(fmt.Sprintf("wt %s open %s\nok edit/build/bench ONLY under this root; check until ok, then work op=submit", id, root))
}

func (s *Server) workSubmit(id string) (*mcp.CallToolResult, any, error) {
	if id == "" {
		id = s.wtItem
	}
	if id == "" || id != s.wtItem {
		return text("! ARG E - no open worktree for " + orDash(id))
	}
	w, ok, err := s.cd.GetWorktree(id)
	if err != nil || !ok {
		return text("! WT E worktree record missing — work op=abort and restart")
	}
	it, _, err := item.Get(s.ws, id)
	if err != nil {
		return nil, nil, err
	}

	// a previous submit already escalated this item — decide first, the
	// gate stays unrun (it would only pile up more rounds against a budget
	// that is already exhausted)
	if it.State == item.StateBlocked {
		return text(fmt.Sprintf("i %s blocked rounds=%d/%d — decide %s", it.ID, it.Rounds, s.maxRounds(), lastNeed(it.Needs)))
	}

	// GATE 1: verify + goal on the worktree tree
	if res := s.runGate(it.Goal); res != "" {
		return s.gateFail(it, res)
	}
	_ = s.cd.PutWorktree(withState(w, "gating"))
	if _, err := wt.CommitCode(s.ws.Dir, "spectackle "+id+": "+it.Title); err != nil {
		return text("! WT E commit: " + err.Error())
	}

	// INTEGRATE under the global lock
	ok, holder, err := s.cd.LockIntegrate(2 * time.Minute)
	if err != nil {
		return nil, nil, err
	}
	if !ok {
		return text("! LOCK W integrate held by " + holder + " — retry submit shortly")
	}
	defer s.cd.UnlockIntegrate()
	_ = s.cd.PutWorktree(withState(w, "integrating"))

	conflicts, err := wt.MergeMain(s.ws.Dir)
	if err != nil {
		return text("! WT E merge: " + err.Error())
	}
	if len(conflicts) > 0 {
		_ = s.cd.PutWorktree(withState(w, "conflict"))
		return text("! WT E conflict " + strings.Join(conflicts, " ") + "\nok resolve these files in your worktree, then work op=submit again")
	}
	// GATE 2: the tree that merges is the tree that was tested
	if res := s.runGate(it.Goal); res != "" {
		return s.gateFail(it, res)
	}
	touched, _ := wt.TouchedFiles(s.main.Dir, w.Base, w.Branch)
	if overlap := wt.DirtyOverlap(s.main.Dir, touched); len(overlap) > 0 {
		return text("! WT E main-dirty " + strings.Join(overlap, " ") + " — commit/stash these in the main checkout, then submit again")
	}
	if err := wt.FFMain(s.main.Dir, w.Branch); err != nil {
		return text("! WT E ff-merge: " + err.Error())
	}

	// REPLAY .spectackle state semantically
	_ = s.cd.PutWorktree(withState(w, "replaying"))
	rep, err := replay.Run(s.main, s.ws, id, w.Base, s.cd, s.g)
	if err != nil {
		return text("! WT E replay: " + err.Error() + "\nok fix and work op=submit again (replay resumes idempotently)")
	}
	if err := journal.Append(s.main, it.Dir, journal.Event{Ev: journal.EvSubmit, ID: id, Dir: it.Dir,
		Note: fmt.Sprintf("branch %s: %d events, %d rules, %d items replayed", w.Branch, rep.Events, rep.Rules, len(rep.Items))}); err != nil {
		return nil, nil, err
	}
	_ = s.cd.Emit("submit", id, it.Title)

	// teardown + back to main (branch kept for audit)
	wtRoot := s.ws.Dir
	if err := s.reroot(s.main.Dir, ""); err != nil {
		return nil, nil, err
	}
	_ = wt.Remove(s.main.Dir, wtRoot)
	_ = s.cd.DelWorktree(id)
	_ = s.cd.ReleaseItem(id)

	var b strings.Builder
	fmt.Fprintf(&b, "ok %s merged to main (%d events, %d rules replayed)\n", id, rep.Events, rep.Rules)
	for from, to := range rep.Remap {
		fmt.Fprintf(&b, "sw 0 %s remap %s replayed as %s\n", s.agent, from, to)
	}
	b.WriteString("i " + id + " — now on main; move to=done/archived as appropriate\n")
	return text(b.String())
}

func (s *Server) workAbort(id string) (*mcp.CallToolResult, any, error) {
	if id == "" {
		id = s.wtItem
	}
	if id == "" {
		return text("! ARG E - abort requires item")
	}
	w, ok, err := s.cd.GetWorktree(id)
	if err != nil {
		return nil, nil, err
	}
	if !ok {
		return text("! ARG E - no worktree for " + id)
	}
	if w.Agent != s.agent && holderAlive(s, w.Agent) {
		return text("! WT E worktree held by live agent " + w.Agent)
	}
	mine := s.wtItem == id
	if mine {
		if err := s.reroot(s.main.Dir, ""); err != nil {
			return nil, nil, err
		}
	}
	_ = wt.Remove(s.main.Dir, w.Root)
	_ = wt.DeleteBranch(s.main.Dir, w.Branch)
	_ = s.cd.DelWorktree(id)
	_ = s.cd.ReleaseItem(id)
	// the item returns to approved on main (its worktree state is discarded)
	if it, ok, _ := item.Get(s.main, id); ok && it.State == item.StateActive {
		it.State = item.StateApproved
		if err := item.Upsert(s.main, it); err != nil {
			return nil, nil, err
		}
	}
	if err := journal.Append(s.main, w.Item, journal.Event{Ev: journal.EvAbort, ID: id,
		Note: "worktree abandoned by " + s.agent}); err != nil {
		return nil, nil, err
	}
	_ = s.cd.Emit("abort", id, "worktree abandoned")
	return text("ok " + id + " aborted; item back to approved")
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
// (lifecycle.Escalate mints the decision item that is the only way out)
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
	s.scan.MarkDirty()
	if it.Rounds < max {
		return text(gateMsg)
	}
	blocked, d, err := lifecycle.Escalate(s.ws, s.minter(), it)
	if err != nil {
		return nil, nil, err
	}
	_ = s.cd.Emit("escalate", blocked.ID, "gate rounds exhausted -> "+d.ID)
	return text(fmt.Sprintf("i %s blocked rounds=%d/%d — decide %s", blocked.ID, blocked.Rounds, max, d.ID))
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
