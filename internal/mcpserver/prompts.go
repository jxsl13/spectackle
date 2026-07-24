package mcpserver

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jxsl13/spectacle/internal/item"
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
		Title:       "spectacle workflow",
		Description: "Live swarm state (agents, leases) + active items + the lifecycle loop — read before picking work.",
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
		Title:       "spectacle state",
		Description: "One read-only structured snapshot: #version #items #rules #graph #swarm #drift #health — the full spec-driven-development picture in one call; writes nothing.",
		Arguments: []*mcp.PromptArgument{
			{Name: "path", Description: "subtree to scope the snapshot to; default: whole workspace"},
		},
	}, s.promptState)
}

// promptWorkflow does not go through gate() (prompts/get is not a tool
// call) — lock s.mu ourselves and refresh the scan so the snapshot below is
// current, exactly like preCall does for tools.
func (s *Server) promptWorkflow(_ context.Context, _ *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.scan.Refresh(); err != nil {
		return nil, err
	}

	var b strings.Builder
	b.WriteString("spectacle workflow - state below is live\n")

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
		fmt.Fprintf(&b, "ag %s %s %ds %s\n", a.Name, orDash(a.Item), int(time.Since(a.HB).Seconds()), wtLabel)
	}
	leases, err := s.cd.Leases(s.agentTTL())
	if err != nil {
		return nil, err
	}
	for _, l := range leases {
		fmt.Fprintf(&b, "l %s %s %s %ds\n", l.Path, l.Agent, orDash(l.Item), int(time.Until(l.Exp).Seconds()))
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
	for _, it := range items {
		b.WriteString(item.Record(it) + "\n")
	}

	b.WriteString("LOOP\n")
	for _, l := range loopLines {
		b.WriteString(l + "\n")
	}

	return &mcp.GetPromptResult{
		Description: "spectacle live workflow snapshot",
		Messages: []*mcp.PromptMessage{
			{Role: "user", Content: &mcp.TextContent{Text: b.String()}},
		},
	}, nil
}

// promptNext also bypasses gate() — lock and refresh manually.
func (s *Server) promptNext(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.scan.Refresh(); err != nil {
		return nil, err
	}

	id := strings.TrimSpace(req.Params.Arguments["item"])
	var it item.Item
	if id != "" {
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
		picked := false
		var fallback item.Item
		haveFallback := false
		for _, cand := range items {
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
	b.WriteString(item.Record(it) + "\n")
	if it.Parent != "" {
		b.WriteString("parent " + it.Parent + "\n")
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
	fmt.Fprintf(&b, "IMPLEMENTER PROTOCOL\n1 get id=%s\n2 lease op=claim paths=%s item=%s\n3 move id=%s to=active\n4 implement + test\n5 move id=%s to=done; lease op=release paths=%s\n",
		id, leasePath, id, id, id, leasePath)

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
	if err := s.scan.Refresh(); err != nil {
		return nil, err
	}
	path := req.Params.Arguments["path"]
	txt, err := s.stateText(path)
	if err != nil {
		return nil, err
	}
	return &mcp.GetPromptResult{
		Description: "spectacle state snapshot",
		Messages: []*mcp.PromptMessage{
			{Role: "user", Content: &mcp.TextContent{Text: txt}},
		},
	}, nil
}

func textPrompt(s string) *mcp.GetPromptResult {
	return &mcp.GetPromptResult{
		Messages: []*mcp.PromptMessage{
			{Role: "user", Content: &mcp.TextContent{Text: s}},
		},
	}
}
