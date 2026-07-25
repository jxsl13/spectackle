package mcpserver

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jxsl13/spectackle/internal/item"
	"github.com/jxsl13/spectackle/internal/lifecycle"
	"github.com/jxsl13/spectackle/internal/workspace"
)

// loopLines mirrors the lifecycle loop taught by the `instructions` manifest
// in server.go, condensed to one dense line per step — the same loop, read
// as a checklist instead of prose.
var loopLines = []string{
	"1 find q=<topic> scope=rejection - learn why similar work failed before you draft",
	"2 find scope=code -> node IDs (go:pkg.Fn); get id=<node> depth=2 -> cross-language impact",
	"3 draft kind=proposal targets=<ids|paths> -> CONTEXT PACK (#impact #contracts #rejections)",
	"4 on explicit user approval: move to=approved|active, then draft kind=task parent=<P-id> per work item + rule op=add for new contracts",
	"5 implement code; check until ok (d records = spec/code drift, fix via rule op=edit or code fix)",
	"6 move to=done then to=archived (active->archived in one call implies done, merges spec.md); rejected REQUIRES note and is revocable; compact when check emits c",
	"7 fanout: orchestrator partitions approved tasks by disjoint scope (leases prove disjointness), spawns one fresh implementer per task in parallel, serializes only shared-file wiring",
}

// registerPrompts adds the MCP prompts (slash-command entry points): a
// standing swarm+workflow snapshot and a one-shot implementer brief for the
// next approved item. Not yet wired into New — the orchestrator calls this.
func (s *Server) registerPrompts() {
	s.mcp.AddPrompt(&mcp.Prompt{
		Name:        "workflow",
		Title:       "spectackle workflow",
		Description: "Live swarm state (agents, leases) + active items + the lifecycle loop — read before picking work. With task, prepends the full SDD lifecycle instruction for that requirement (same loop /spectackle <task> drives).",
		Arguments: []*mcp.PromptArgument{
			{Name: "task", Description: "requirement/task text; when set, prepends the research->draft->grill->decide->approve->fanout->check->archive lifecycle instruction for it"},
		},
	}, s.promptWorkflow)

	s.mcp.AddPrompt(&mcp.Prompt{
		Name:        "next",
		Title:       "next approved item",
		Description: "Full implementer brief for the next approved item (or a named one): item.Record + parent/targets/body + the 5-step implementer protocol.",
		Arguments: []*mcp.PromptArgument{
			{Name: "item", Description: "item ID to brief; default: first approved item, kind=task preferred"},
		},
	}, s.promptNext)

	s.mcp.AddPrompt(&mcp.Prompt{
		Name:        "state",
		Title:       "spectackle state",
		Description: "One read-only structured snapshot: #version #items #rules #graph #swarm #drift #health — the full spec-driven-development picture in one call; writes nothing.",
		Arguments: []*mcp.PromptArgument{
			{Name: "path", Description: "subtree to scope the snapshot to; default: whole workspace"},
		},
	}, s.promptState)
}

// lifecycleLines renders the full research->draft->grill->decide->approve->
// fanout->check->archive loop the `/spectackle <task>` repo command drives
// (.claude/commands/spectackle.md) as one dense LIFECYCLE block with the task
// text embedded — the MCP-native form of that same two-mode entry point, so
// it works identically in any MCP harness, not only Claude Code's repo
// commands.
func lifecycleLines(task string) []string {
	return []string{
		"LIFECYCLE " + task,
		"1 research q=\"" + task + "\" - impact/contracts/rejections/history/docs/gaps; draft kind=research + a fresh cheap subagent only if the pack doesn't answer it, never ad hoc exploration; token economy: find scope=code / get depth replace shell grep-find exploration, .spectackle/ never shell-edited",
		"2 draft kind=proposal targets=<ids from research> - mints the proposal, returns the context pack (#impact #contracts #rejections)",
		"3 grill id=<P-id> - close what it surfaces: unanchored targets get rule op=add, thin child-task bodies get rewritten exhaustively (files, APIs, verification commands, scope)",
		"4 decide op=ask on any decision that actually needs the user - never unstructured chat; no UI/declined/cancelled: keep working other disjoint tasks, the answer arrives later via decide op=answer",
		"5 on explicit user approval: move to=approved (or straight to=active, one call)",
		"6 fanout: partition approved tasks by disjoint scope (leases prove disjointness), spawn one fresh cheap-model implementer per task in parallel, serialize only shared-file wiring yourself",
		"7 check until ok; review diffs, run the declared gate/verify commands - never trust a done you haven't checked",
		"8 move to=done then to=archived (active->archived in one call implies done, merges spec.md); if a task escalates to blocked (rounds limit), resolve the auto-minted ADR-item via decide (rescope|reject|override-once) before continuing that task's line of work",
	}
}

// promptWorkflow does not go through gate() (prompts/get is not a tool
// call) — lock s.mu ourselves and refresh the scan so the snapshot below is
// current, exactly like preCall does for tools. With the optional task
// argument set, a LIFECYCLE block (see lifecycleLines) is prepended — the
// same instruction the `/spectackle <task>` repo command drives, so an MCP
// client with no access to Claude Code's repo commands still gets the full
// two-mode entry point natively.
func (s *Server) promptWorkflow(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(); err != nil {
		return nil, err
	}

	var b strings.Builder
	if task := strings.TrimSpace(req.Params.Arguments["task"]); task != "" {
		for _, l := range lifecycleLines(task) {
			b.WriteString(l + "\n")
		}
	}
	b.WriteString("spectackle workflow - state below is live\n")

	b.WriteString("AGENTS/LEASES\n")
	agents, err := s.cd.Agents()
	if err != nil {
		return nil, err
	}
	for _, a := range agents {
		wtLabel := "main"
		if a.WT != "" {
			wtLabel = a.WT
		}
		fmt.Fprintf(&b, "ag %s %s %s %s\n", a.Name, orDash(a.Item), hbAge(a.HB), wtLabel)
	}
	leases, err := s.cd.Leases(s.agentTTL())
	if err != nil {
		return nil, err
	}
	for _, l := range leases {
		fmt.Fprintf(&b, "l %s %s %s %s\n", l.Path, l.Agent, orDash(l.Item), leaseLeft(l.Exp))
	}

	b.WriteString("ACTIVE ITEMS\n")
	items, err := item.LoadAll(s.ws)
	if err != nil {
		return nil, err
	}
	// state != draft first: actionable items (submitted/approved/active/...)
	// surface before drafts still being written.
	sort.SliceStable(items, func(i, j int) bool {
		di := items[i].State == item.StateDraft
		dj := items[j].State == item.StateDraft
		return !di && dj
	})
	sc, err := s.idScope()
	if err != nil {
		return nil, err
	}
	for _, it := range items {
		b.WriteString(sc.record(it) + "\n")
	}

	b.WriteString("LOOP\n")
	for _, l := range loopLines {
		b.WriteString(l + "\n")
	}

	return &mcp.GetPromptResult{
		Description: "spectackle live workflow snapshot",
		Messages: []*mcp.PromptMessage{
			{Role: "user", Content: &mcp.TextContent{Text: b.String()}},
		},
	}, nil
}

// promptNext also bypasses gate() — lock and refresh manually.
func (s *Server) promptNext(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(); err != nil {
		return nil, err
	}

	sc, err := s.idScope()
	if err != nil {
		return nil, err
	}
	id := strings.TrimSpace(req.Params.Arguments["item"])
	var it item.Item
	if id != "" {
		// the brief is fetched by whatever ID form the caller saw in a state
		// or workflow prompt, which is the display form (ADR-0013).
		full, bad := sc.expand(id)
		if bad != nil {
			return textPrompt(resultText(bad)), nil
		}
		id = full
		found, ok, err := item.Get(s.ws, id)
		if err != nil {
			return nil, err
		}
		if !ok {
			return textPrompt("nf " + id), nil
		}
		it = found
	} else {
		items, err := item.LoadAll(s.ws)
		if err != nil {
			return nil, err
		}
		byID := itemsByID(items)
		picked := false
		var fallback item.Item
		haveFallback := false
		for _, cand := range items {
			// blocked items (item.StateBlocked, set by lifecycle.Escalate) and
			// items still blocked on an open need (see hasOpenNeeds) are not
			// actionable work — decide first, via the linked ADR-item.
			if cand.State == item.StateBlocked || hasOpenNeeds(s.ws, cand, byID) {
				continue
			}
			if cand.State != item.StateApproved {
				continue
			}
			if cand.Kind == "task" {
				it, picked = cand, true
				break
			}
			if !haveFallback {
				fallback, haveFallback = cand, true
			}
		}
		if !picked {
			if !haveFallback {
				return textPrompt("ok nothing approved - draft or approve first"), nil
			}
			it = fallback
		}
		id = it.ID
	}

	var b strings.Builder
	b.WriteString(sc.record(it) + "\n")
	if it.Parent != "" {
		b.WriteString("parent " + sc.short(it.Parent) + "\n")
	}
	if len(it.Targets) > 0 {
		b.WriteString("targets " + strings.Join(it.Targets, " ") + "\n")
	}
	if it.Body != "" {
		b.WriteString(it.Body + "\n")
	}

	leasePath := it.Dir
	if leasePath == "" {
		leasePath = "."
	}
	// the protocol is a script the implementer pastes back verbatim, so the
	// IDs in it are the display form the tool boundary accepts.
	short := sc.short(id)
	fmt.Fprintf(&b, "IMPLEMENTER PROTOCOL\n1 get id=%s\n2 lease op=claim paths=%s item=%s\n3 move id=%s to=active\n4 implement + test\n5 move id=%s to=done; lease op=release paths=%s\n",
		short, leasePath, short, short, short, leasePath)

	return &mcp.GetPromptResult{
		Description: "implementer brief for " + id,
		Messages: []*mcp.PromptMessage{
			{Role: "user", Content: &mcp.TextContent{Text: b.String()}},
		},
	}, nil
}

// promptState bypasses gate() too — lock and refresh manually, then reuse
// the exact same builder the `state` tool calls, so `prompts/get state` and
// `tools/call state` never drift apart in content.
func (s *Server) promptState(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(); err != nil {
		return nil, err
	}
	path := req.Params.Arguments["path"]
	txt, err := s.stateText(path)
	if err != nil {
		return nil, err
	}
	return &mcp.GetPromptResult{
		Description: "spectackle state snapshot",
		Messages: []*mcp.PromptMessage{
			{Role: "user", Content: &mcp.TextContent{Text: txt}},
		},
	}, nil
}

// itemsByID indexes a loaded item list by ID for Needs resolution below.
func itemsByID(items []item.Item) map[string]item.Item {
	m := make(map[string]item.Item, len(items))
	for _, it := range items {
		m[it.ID] = it
	}
	return m
}

// hasOpenNeeds reports whether cand is still blocked on any of its Needs
// (adr items minted by decide op=ask or lifecycle.Escalate). A need is
// resolved when the referenced item is done/archived; when it is gone from
// work.md entirely, it resolves only if lifecycle.Tombstone finds an archive
// record for it — a need that resolves nowhere at all stays open
// (conservative: unknown is never treated as done).
func hasOpenNeeds(ws workspace.Root, cand item.Item, byID map[string]item.Item) bool {
	for _, n := range cand.Needs {
		need, ok := byID[n]
		if !ok {
			if _, tombOk, _ := lifecycle.Tombstone(ws, n); tombOk {
				continue // archived = resolved
			}
			return true // resolves nowhere = still open
		}
		if need.State != item.StateDone && need.State != item.StateArchived {
			return true
		}
	}
	return false
}

func textPrompt(s string) *mcp.GetPromptResult {
	return &mcp.GetPromptResult{
		Messages: []*mcp.PromptMessage{
			{Role: "user", Content: &mcp.TextContent{Text: s}},
		},
	}
}
