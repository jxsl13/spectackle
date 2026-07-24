package mcpserver

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jxsl13/spectacle/internal/item"
	"github.com/jxsl13/spectacle/internal/journal"
	"github.com/jxsl13/spectacle/internal/lifecycle"
	"github.com/jxsl13/spectacle/internal/workspace"
)

// decide — native, persistent user decisions (SDD orchestration v2, docs/tools.md
// #14). NOT YET REGISTERED: registration into s.registerTools() (tools.go) is
// the orchestrator's call, out of this task's scope — this file only defines
// the input struct and the (ctx, req, in) handler shape, exactly like `rule`
// (tools.go), since `ask` needs req.Session for elicitation.
type decideIn struct {
	Op       string   `json:"op" jsonschema:"ask|answer|ls"`
	ID       string   `json:"id,omitempty" jsonschema:"D-id (answer) — omit for ask"`
	Question string   `json:"question,omitempty" jsonschema:"ask: the decision to make"`
	Kind     string   `json:"kind,omitempty" jsonschema:"radio|confirm|text, default radio"`
	Options  []string `json:"options,omitempty" jsonschema:"radio choices, 2-5"`
	Item     string   `json:"item,omitempty" jsonschema:"lifecycle item this decision blocks"`
	Choose   string   `json:"choose,omitempty" jsonschema:"answer: option text / yes|no / free text"`
}

// decide dispatches on op. See ask/answer/ls below for the persistence
// contract of each.
func (s *Server) decide(ctx context.Context, req *mcp.CallToolRequest, in decideIn) (*mcp.CallToolResult, any, error) {
	switch in.Op {
	case "ask":
		return s.decideAsk(ctx, req, in)
	case "answer":
		return s.decideAnswer(in)
	case "ls":
		return s.decideLs()
	}
	return text("! ARG E - op must be ask|answer|ls")
}

// decideAsk mints a `decision` item (state=draft, kind=decision) recording
// the question, its kind/options and — if in.Item is set — which item it
// blocks (appended to that item's Needs, exactly like lifecycle.Escalate
// links its auto-minted decisions). It then tries MCP elicitation
// (req.Session.Elicit — the same native-UI mechanism `rule`'s slot forms use
// in production, see elicitSlots in tools.go): the host renders a
// radio/confirm/text form and, on accept, the answer resolves immediately
// (same path as `answer`). No elicitation support, decline/cancel, or any
// transport error leaves the D-item open (state=submitted) — the caller is
// NOT meant to block on it; it can keep working other disjoint tasks and the
// decision gets answered later, from anywhere, via `answer`.
func (s *Server) decideAsk(ctx context.Context, req *mcp.CallToolRequest, in decideIn) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(in.Question) == "" {
		return text("! ARG E - ask requires question")
	}
	kind := in.Kind
	if kind == "" {
		kind = "radio"
	}
	var opts []string
	switch kind {
	case "radio":
		if len(in.Options) < 2 {
			return text("! ARG E - radio requires 2-5 options")
		}
		opts = in.Options
	case "confirm":
		opts = []string{"yes", "no"}
	case "text":
		// free text: no fixed option set
	default:
		return text("! ARG E - kind must be radio|confirm|text")
	}

	dir := ""
	var blocks item.Item
	hasBlocks := false
	if in.Item != "" {
		found, ok, err := item.Get(s.ws, in.Item)
		if err != nil {
			return nil, nil, err
		}
		if !ok {
			return text("! ARG E - unknown item " + in.Item)
		}
		blocks, hasBlocks = found, true
		dir = found.Dir
	}

	var bodyLines []string
	bodyLines = append(bodyLines, "kind: "+kind)
	if len(opts) > 0 {
		bodyLines = append(bodyLines, "options: "+strings.Join(opts, ", "))
	}
	if hasBlocks {
		bodyLines = append(bodyLines, "blocks: "+blocks.ID)
	}

	d, err := lifecycle.Draft(s.ws, s.minter(), "decision", in.Question, strings.Join(bodyLines, "\n"), dir, "", nil)
	if err != nil {
		return text("! ARG E - " + err.Error())
	}
	// asked = open, not merely drafted: docs/tools.md #14 documents the
	// no-answer-yet outcome as "state=submitted" — resolveDecision (below)
	// takes it the rest of the way to done once it is actually answered.
	if _, err := lifecycle.Move(s.ws, d.ID, item.StateSubmitted, ""); err != nil {
		return nil, nil, err
	}
	if hasBlocks {
		blocks.Needs = append(blocks.Needs, d.ID)
		if err := item.Upsert(s.ws, blocks); err != nil {
			return nil, nil, err
		}
	}
	s.scan.MarkDirty()
	_ = s.cd.Emit("decide", d.ID, "ask "+in.Question)

	props := map[string]any{}
	switch kind {
	case "radio":
		props["choice"] = map[string]any{"type": "string", "enum": opts, "description": in.Question}
	case "confirm":
		props["choice"] = map[string]any{"type": "boolean", "description": in.Question}
	case "text":
		props["choice"] = map[string]any{"type": "string", "description": in.Question}
	}
	res, err := req.Session.Elicit(ctx, &mcp.ElicitParams{
		Message: "spectacle: " + in.Question,
		RequestedSchema: map[string]any{
			"type": "object", "properties": props, "required": []string{"choice"},
		},
	})
	if err == nil && res.Action == "accept" {
		return s.resolveDecision(d.ID, decideChoiceString(kind, res.Content["choice"]))
	}
	return text(fmt.Sprintf("need decision %s %s | %s", d.ID, in.Question, strings.Join(opts, ", ")))
}

// decideChoiceString normalizes an elicitation result's "choice" value to
// the string recorded on the decision item; confirm's boolean becomes
// yes/no so answer= and the stored options line agree on vocabulary.
func decideChoiceString(kind string, v any) string {
	if kind == "confirm" {
		if b, ok := v.(bool); ok {
			if b {
				return "yes"
			}
			return "no"
		}
	}
	sv, _ := v.(string)
	return sv
}

// decideAnswer resolves an open decision from anywhere, any time — the
// waiting orchestrator sees it on its next swarm/state/find call (no polling
// required). Choose is validated against the decision's stored options
// (parsed from its body — see decideOptions, which understands both the
// `options: a, b, c` form decideAsk writes and the `outcome=a|b|c` form
// lifecycle.Escalate writes for its auto-minted decisions); a decision with
// no stored options (kind=text) accepts free text.
func (s *Server) decideAnswer(in decideIn) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(in.ID) == "" {
		return text("! ARG E - answer requires id")
	}
	if strings.TrimSpace(in.Choose) == "" {
		return text("! ARG E - answer requires choose")
	}
	d, ok, err := item.Get(s.ws, in.ID)
	if err != nil {
		return nil, nil, err
	}
	if !ok || d.Kind != "decision" {
		return text("! ARG E - unknown decision " + in.ID)
	}
	if d.State == item.StateDone {
		return text("! ARG E - " + in.ID + " already decided")
	}
	choose := in.Choose
	if opts := decideOptions(d.Body); len(opts) > 0 {
		matched := ""
		for _, o := range opts {
			if strings.EqualFold(o, choose) {
				matched = o
				break
			}
		}
		if matched == "" {
			return text("! ARG E - choose must be one of: " + strings.Join(opts, ", "))
		}
		choose = matched
	}
	return s.resolveDecision(d.ID, choose)
}

// decideOptions extracts the fixed option set (if any) a decision was asked
// with. Two body shapes are understood: decideAsk's own `options: a, b, c`
// line, and lifecycle.Escalate's `... outcome=a|b|c.` sentence (T-0030
// foundation, not writable from this package). No match = free text.
var reOutcome = regexp.MustCompile(`outcome=([a-zA-Z0-9_-]+(?:\|[a-zA-Z0-9_-]+)*)`)

func decideOptions(body string) []string {
	for _, line := range strings.Split(body, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "options: "); ok {
			var out []string
			for _, o := range strings.Split(v, ",") {
				if o = strings.TrimSpace(o); o != "" {
					out = append(out, o)
				}
			}
			return out
		}
	}
	if m := reOutcome.FindStringSubmatch(body); m != nil {
		return strings.Split(m[1], "|")
	}
	return nil
}

// resolveDecision persists a decision's outcome (state=done, choice recorded
// in the body, journaled) and applies it to whatever the decision blocks, if
// anything: a blocked item (item.StateBlocked, set by lifecycle.Escalate)
// with choice in {rescope,reject,override-once} resolves via
// lifecycle.ResolveBlocked; any other blocked-on item just has this
// decision's ID cleared from its Needs (the ordinary decide-ask-on-any-item
// case — nothing to unblock, it was never in item.StateBlocked).
func (s *Server) resolveDecision(id, choice string) (*mcp.CallToolResult, any, error) {
	d, ok, err := item.Get(s.ws, id)
	if err != nil {
		return nil, nil, err
	}
	if !ok {
		return text("! ARG E - unknown decision " + id)
	}

	d.State = item.StateDone
	if strings.TrimSpace(d.Body) != "" {
		d.Body = strings.TrimRight(d.Body, "\n") + "\n"
	}
	d.Body += "choice: " + choice
	if err := item.Upsert(s.ws, d); err != nil {
		return nil, nil, err
	}
	if err := journal.Append(s.ws, d.Dir, journal.Event{
		Ev: journal.EvDecide, ID: d.ID, Dir: d.Dir, Note: choice,
	}); err != nil {
		return nil, nil, err
	}
	_ = s.cd.Emit("decide", d.ID, choice)

	it, hasBlocked, err := blockingItem(s.ws, id)
	if err != nil {
		return nil, nil, err
	}
	if hasBlocked {
		if it.State == item.StateBlocked && (choice == "rescope" || choice == "reject" || choice == "override-once") {
			if _, err := lifecycle.ResolveBlocked(s.ws, it.ID, choice, "decide "+id+": "+choice); err != nil {
				return text("! ARG E - " + err.Error())
			}
		} else {
			it.Needs = removeID(it.Needs, id)
			if err := item.Upsert(s.ws, it); err != nil {
				return nil, nil, err
			}
		}
	}
	s.scan.MarkDirty()
	return text("ok " + id + " " + choice)
}

// blockingItem finds the (at most one) item whose Needs references decisionID.
func blockingItem(ws workspace.Root, decisionID string) (item.Item, bool, error) {
	items, err := item.LoadAll(ws)
	if err != nil {
		return item.Item{}, false, err
	}
	for _, it := range items {
		for _, n := range it.Needs {
			if n == decisionID {
				return it, true, nil
			}
		}
	}
	return item.Item{}, false, nil
}

func removeID(ids []string, id string) []string {
	var out []string
	for _, x := range ids {
		if x != id {
			out = append(out, x)
		}
	}
	return out
}

// decideLs lists open (not yet done) decision items as dense i-lines.
func (s *Server) decideLs() (*mcp.CallToolResult, any, error) {
	items, err := item.LoadAll(s.ws)
	if err != nil {
		return nil, nil, err
	}
	var b strings.Builder
	for _, it := range items {
		if it.Kind != "decision" || it.State == item.StateDone {
			continue
		}
		b.WriteString(item.Record(it) + "\n")
	}
	if b.Len() == 0 {
		return text("ok no open decisions")
	}
	return text(b.String())
}
