package mcpserver

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jxsl13/spectackle/internal/item"
	"github.com/jxsl13/spectackle/internal/journal"
	"github.com/jxsl13/spectackle/internal/lifecycle"
	"github.com/jxsl13/spectackle/internal/workspace"
)

// decide — native, persistent user decisions (SDD orchestration v2, docs/tools.md
// #14). NOT YET REGISTERED: registration into s.registerTools() (tools.go) is
// the orchestrator's call, out of this task's scope — this file only defines
// the input struct and the (ctx, req, in) handler shape, exactly like `rule`
// (tools.go), since `ask` needs req.Session for elicitation.
type decideIn struct {
	Op           string   `json:"op" jsonschema:"ask|answer|ls"`
	ID           string   `json:"id,omitempty" jsonschema:"ADR-id, full or prefix (answer) — omit for ask"`
	Question     string   `json:"question,omitempty" jsonschema:"ask: the decision to make"`
	Context      string   `json:"context,omitempty" jsonschema:"ADR context: the forces and constraints behind this decision"`
	Kind         string   `json:"kind,omitempty" jsonschema:"radio|confirm|text, default radio"`
	Options      []string `json:"options,omitempty" jsonschema:"radio choices, 2-5"`
	Item         string   `json:"item,omitempty" jsonschema:"lifecycle item this decision blocks, full ID or prefix"`
	Choose       string   `json:"choose,omitempty" jsonschema:"answer: option text / yes|no / free text"`
	Consequences string   `json:"consequences,omitempty" jsonschema:"answer: ADR consequences — trade-offs and follow-on effects of the decision"`
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

// decideAsk mints an `adr` item (state=draft, kind=adr) recording
// the question, its kind/options and — if in.Item is set — which item it
// blocks (appended to that item's Needs, exactly like lifecycle.Escalate
// links its auto-minted decisions). It then tries MCP elicitation
// (req.Session.Elicit — the same native-UI mechanism `rule`'s slot forms use
// in production, see elicitSlots in tools.go): the host renders a
// radio/confirm/text form and, on accept, the answer resolves immediately
// (same path as `answer`). No elicitation support, decline/cancel, or any
// transport error leaves the ADR-item open (state=submitted) — the caller is
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
	blocksID := ""
	if in.Item != "" {
		// a short prefix names the blocked item just as well as its full ID
		// (ADR-0013); what gets persisted below — the `blocks:` body line
		// and the Needs backlink — is always the resolved full ID, because a
		// short form is computed against a set that keeps growing and only
		// the full ID stays unambiguous forever.
		full, bad, err := s.expandID(in.Item)
		if bad != nil || err != nil {
			return bad, nil, err
		}
		in.Item = full
		found, ok, err := item.Get(s.ws, in.Item)
		if err != nil {
			return nil, nil, err
		}
		if ok {
			blocks, hasBlocks = found, true
			dir, blocksID = found.Dir, found.ID
		} else if tomb, tombOk, err := lifecycle.Tombstone(s.ws, in.Item); err != nil {
			return nil, nil, err
		} else if tombOk {
			// archived item: keep the block as provenance in the body, but
			// there is no work.md home to append+Upsert a Needs backlink
			// into — hasBlocks stays false.
			dir, blocksID = tomb.Dir, tomb.ID
		} else {
			return text("! ARG E - unknown item " + in.Item)
		}
	}

	var bodyLines []string
	bodyLines = append(bodyLines, "kind: "+kind)
	for _, o := range opts {
		bodyLines = append(bodyLines, "option: "+o)
	}
	if blocksID != "" {
		bodyLines = append(bodyLines, "blocks: "+blocksID)
	}

	d, err := lifecycle.Draft(s.ws, s.minter(), "adr", in.Question, strings.Join(bodyLines, "\n"), dir, "", nil)
	if err != nil {
		return text("! ARG E - " + err.Error())
	}
	// classic ADR fields: Context is whatever the caller supplied (may be
	// empty); Status starts at "proposed" per the ADR convention and only
	// moves to "accepted" once resolveDecision lands an outcome. Persist
	// before Move so Move's own item.Get/Upsert round-trip carries them.
	d.Context = in.Context
	d.Status = "proposed"
	if err := item.Upsert(s.ws, d); err != nil {
		return nil, nil, err
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
		Message: "spectackle: " + in.Question,
		RequestedSchema: map[string]any{
			"type": "object", "properties": props, "required": []string{"choice"},
		},
	})
	if err == nil && res.Action == "accept" {
		return s.resolveDecision(d.ID, decideChoiceString(kind, res.Content["choice"]), "")
	}
	// the need-decision record is the ONLY place this ID is ever published —
	// answering it later, from any session, means pasting it into
	// decide op=answer — so it is rendered short like every other item ID,
	// and prefix resolution is what makes the paste work (ADR-0013).
	sc, sErr := s.idScope()
	if sErr != nil {
		return nil, nil, sErr
	}
	return text(fmt.Sprintf("need decision %s %s | %s", sc.short(d.ID), in.Question, strings.Join(opts, ", ")))
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
// (parsed from its body — see decideOptions, which understands decideAsk's
// current `option: <text>` lines, its legacy `options: a, b, c` form, and
// the `outcome=a|b|c` form lifecycle.Escalate writes for its auto-minted
// decisions); a decision with no stored options (kind=text) accepts free
// text.
func (s *Server) decideAnswer(in decideIn) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(in.ID) == "" {
		return text("! ARG E - answer requires id")
	}
	if strings.TrimSpace(in.Choose) == "" {
		return text("! ARG E - answer requires choose")
	}
	// a short prefix answers a decision just as well as its full ID
	// (ADR-0013) — and the need-decision record the caller is answering from
	// only ever published the short form.
	sc, err := s.idScope()
	if err != nil {
		return nil, nil, err
	}
	asked := in.ID // what the caller typed, for the refusals that echo it
	full, bad := sc.expand(in.ID)
	if bad != nil {
		return bad, nil, nil
	}
	in.ID = full
	d, ok, err := item.Get(s.ws, in.ID)
	if err != nil {
		return nil, nil, err
	}
	if !ok || d.Kind != "adr" {
		return text("! ARG E - unknown decision " + asked)
	}
	if d.State == item.StateDone {
		return text("! ARG E - " + sc.short(in.ID) + " already decided")
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
	return s.resolveDecision(d.ID, choose, in.Consequences)
}

// decideOptions extracts the fixed option set (if any) a decision was asked
// with. Three body shapes are understood, tried in order:
//  1. decideAsk's current form: one `option: <text>` line per option,
//     verbatim (no comma-splitting — an option's own text may contain
//     commas). This is what NEW decisions write.
//  2. the legacy `options: a, b, c` comma-joined line decideAsk used to
//     write — kept forever so existing items/journals stay answerable
//     without migration (when an option's own text contains commas, the
//     comma-split fragments it into more pieces than were intended, but
//     that shattered form is exactly what remains answerable — it is not
//     rewritten to option: lines).
//  3. lifecycle.Escalate's `... outcome=a|b|c.` sentence (T-0030
//     foundation, not writable from this package).
//
// No match = free text.
var reOutcome = regexp.MustCompile(`outcome=([a-zA-Z0-9_-]+(?:\|[a-zA-Z0-9_-]+)*)`)

func decideOptions(body string) []string {
	var out []string
	for _, line := range strings.Split(body, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "option: "); ok {
			out = append(out, v)
		}
	}
	if len(out) > 0 {
		return out
	}
	for _, line := range strings.Split(body, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "options: "); ok {
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
//
// It also lands the classic ADR fields: Decision gets the chosen option and
// Status moves to "accepted" — both on every resolution path (op=answer, and
// op=ask's immediate-accept-via-elicitation shortcut, which resolves the same
// way in one round trip). consequences is optional free text from op=answer
// only ("" from the ask path) and is stored verbatim when non-empty.
func (s *Server) resolveDecision(id, choice, consequences string) (*mcp.CallToolResult, any, error) {
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
	d.Decision = choice
	d.Status = "accepted"
	if consequences != "" {
		d.Consequences = consequences
	}
	if err := item.Upsert(s.ws, d); err != nil {
		return nil, nil, err
	}
	if err := journal.Append(s.ws, d.Dir, journal.Event{
		Ev: journal.EvDecide, ID: d.ID, Dir: d.Dir, Note: choice,
	}); err != nil {
		return nil, nil, err
	}
	_ = s.cd.Emit("decide", d.ID, choice)

	sc, err := s.idScope()
	if err != nil {
		return nil, nil, err
	}

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
	return text("ok " + sc.short(id) + " " + choice)
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
	sc, err := s.idScope()
	if err != nil {
		return nil, nil, err
	}
	var b strings.Builder
	for _, it := range items {
		if it.Kind != "adr" || it.State == item.StateDone {
			continue
		}
		b.WriteString(sc.record(it) + "\n")
	}
	if b.Len() == 0 {
		return text("ok no open decisions")
	}
	return text(b.String())
}
