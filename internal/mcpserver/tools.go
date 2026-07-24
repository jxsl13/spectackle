package mcpserver

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jxsl13/spectacle/internal/budget"
	"github.com/jxsl13/spectacle/internal/drift"
	"github.com/jxsl13/spectacle/internal/ears"
	"github.com/jxsl13/spectacle/internal/graph"
	"github.com/jxsl13/spectacle/internal/index"
	"github.com/jxsl13/spectacle/internal/item"
	"github.com/jxsl13/spectacle/internal/journal"
	"github.com/jxsl13/spectacle/internal/lifecycle"
	"github.com/jxsl13/spectacle/internal/spec"
)

// The 7-tool surface is documented in docs/tools.md; the structs below are
// the normative schemas (SPX-REPO-001). Results use the dense line grammar,
// not JSON — that is where the token savings come from.

var reRuleID = regexp.MustCompile(`^[A-Z][A-Z0-9]*(-[A-Z0-9]+)*-\d{3}$`)

// ---- input structs (JSON Schemas are inferred from these) ----

type findIn struct {
	Q     string `json:"q" jsonschema:"text or ID fragment"`
	Scope string `json:"scope,omitempty" jsonschema:"code|rule|spec|proposal|task|bug|research|rejection|history|all, default all"`
	K     int    `json:"k,omitempty" jsonschema:"max results, default 8"`
}

type getIn struct {
	ID     string `json:"id" jsonschema:"node ID, rule ID, item ID, sec:<dir>#<name>, or path"`
	Depth  int    `json:"depth,omitempty" jsonschema:"impact BFS depth for node IDs, default 0"`
	Budget int    `json:"budget,omitempty" jsonschema:"token budget, default 2000"`
	Cur    string `json:"cur,omitempty" jsonschema:"resume cursor"`
}

type draftIn struct {
	Kind    string   `json:"kind" jsonschema:"proposal|task|research|bug"`
	Title   string   `json:"title" jsonschema:"one-line title"`
	Body    string   `json:"body,omitempty" jsonschema:"intent/delta-spec prose"`
	Targets []string `json:"targets,omitempty" jsonschema:"node IDs or paths the change touches"`
	Parent  string   `json:"parent,omitempty" jsonschema:"parent item ID (tasks under a proposal)"`
	Dir     string   `json:"dir,omitempty" jsonschema:"force context dir; default derived from targets"`
}

type ruleIn struct {
	Op        string   `json:"op" jsonschema:"add|edit|retire"`
	ID        string   `json:"id,omitempty" jsonschema:"rule ID (edit/retire)"`
	Dir       string   `json:"dir,omitempty" jsonschema:"context dir (add), default root"`
	Pattern   string   `json:"pattern,omitempty" jsonschema:"U|E|S|N|O|C; elicited if missing"`
	System    string   `json:"system,omitempty" jsonschema:"the acting system"`
	Response  string   `json:"response,omitempty" jsonschema:"what it SHALL do; name something verifiable"`
	Trigger   string   `json:"trigger,omitempty" jsonschema:"WHEN clause (E/C)"`
	State     string   `json:"state,omitempty" jsonschema:"WHILE clause (S/C)"`
	Condition string   `json:"condition,omitempty" jsonschema:"IF clause (N/C)"`
	Feature   string   `json:"feature,omitempty" jsonschema:"WHERE clause (O/C)"`
	Stem      string   `json:"stem,omitempty" jsonschema:"ID stem e.g. CUDA-KRN; default: stem of last rule in target"`
	Rationale string   `json:"rationale,omitempty" jsonschema:"optional rationale"`
	Applies   []string `json:"applies,omitempty" jsonschema:"node IDs the rule is pinned to (anchored for drift)"`
	Item      string   `json:"item,omitempty" jsonschema:"lifecycle item this change belongs to"`
}

type moveIn struct {
	ID   string `json:"id" jsonschema:"item ID, e.g. P-0007"`
	To   string `json:"to" jsonschema:"submitted|approved|rejected|active|done|archived"`
	Note string `json:"note,omitempty" jsonschema:"REQUIRED for rejected (rejection corpus); recommended for archived"`
}

type checkIn struct {
	Path   string `json:"path,omitempty" jsonschema:"subtree, default workspace"`
	Fix    bool   `json:"fix,omitempty" jsonschema:"auto-draft backprop proposals for drift, default false"`
	Budget int    `json:"budget,omitempty" jsonschema:"token budget, default 1500"`
}

type compactIn struct {
	Path  string `json:"path,omitempty" jsonschema:"context dir, default all"`
	Apply bool   `json:"apply,omitempty" jsonschema:"execute (default: dry-run listing candidates)"`
}

func text(s string) (*mcp.CallToolResult, any, error) {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: s}}}, nil, nil
}

// gate wraps a handler with the debounced cache sync that runs before every
// tool call (the .spectacle files on disk are the source of truth).
func gate[T any](s *Server, h func(T) (*mcp.CallToolResult, any, error)) func(context.Context, *mcp.CallToolRequest, T) (*mcp.CallToolResult, any, error) {
	return func(_ context.Context, _ *mcp.CallToolRequest, in T) (*mcp.CallToolResult, any, error) {
		if err := s.scan.Refresh(); err != nil {
			return nil, nil, err
		}
		return h(in)
	}
}

func (s *Server) registerTools() {
	mcp.AddTool(s.mcp, &mcp.Tool{Name: "find",
		Description: "Unified search. scope: code→nodes(n), rule→EARS(r), spec→prose(s), proposal|task|bug|research→items(i), rejection→past rejections(j), history→journal(j), all→mixed. ALWAYS search rejection+history before drafting."},
		gate(s, s.find))

	mcp.AddTool(s.mcp, &mcp.Tool{Name: "get",
		Description: "Read one thing by ID. Item→header+body. Rule→text+rationale+anchors(a). Node with depth>0→cross-language impact radius (n/e). Dir→its rules+items. File→resolved contracts. sec:<dir>#<name>→prose. Unknown→nf with nearest."},
		gate(s, s.get))

	mcp.AddTool(s.mcp, &mcp.Tool{Name: "draft",
		Description: "Create a lifecycle item (state=draft) in the correct .spectacle/work.md — server assigns ID+scope. With targets the response is a CONTEXT PACK: #impact, #contracts, #rejections — read it before writing code. Never edit .spectacle files yourself."},
		gate(s, s.draft))

	mcp.AddTool(s.mcp, &mcp.Tool{Name: "rule",
		Description: "Author EARS contracts — the ONLY write path for rules. add: fill slots (missing ones elicited or returned as need records); server composes+lints (errors reject, nothing written), auto-IDs, anchors applies. edit: recompose/relink by id. retire: remove; text survives in journal."},
		func(ctx context.Context, req *mcp.CallToolRequest, in ruleIn) (*mcp.CallToolResult, any, error) {
			if err := s.scan.Refresh(); err != nil {
				return nil, nil, err
			}
			return s.rule(ctx, req, in)
		})

	mcp.AddTool(s.mcp, &mcp.Tool{Name: "move",
		Description: "Transition a lifecycle item. rejected REQUIRES note (item leaves work.md; summary stays searchable via find scope=rejection) and is revocable: move the rejected ID back to any previous state. archived requires done + no open children; merges the delta into spec.md. approve/reject only on explicit user instruction."},
		gate(s, s.move))

	mcp.AddTool(s.mcp, &mcp.Tool{Name: "check",
		Description: "Verify: spec lint (!), coverage gaps (g), drift via anchors (d gone|changed|stale), duplicate item IDs, compact-due (c). fix=true drafts one backprop proposal per drifted rule and re-stamps anchors. Run until ok before move to=done."},
		gate(s, s.check))

	mcp.AddTool(s.mcp, &mcp.Tool{Name: "compact",
		Description: "Housekeeping. Dry-run lists candidates (c): done-unarchived items, journal folds. apply=true executes — reject events are NEVER dropped. Trigger when check emits c records."},
		gate(s, s.compact))
}

// ---- find ----

var scopeKinds = map[string][]string{
	"all":       nil,
	"rule":      {"rule"},
	"spec":      {"section"},
	"proposal":  {"proposal"},
	"task":      {"task"},
	"bug":       {"bug"},
	"research":  {"research"},
	"rejection": {"rejection"},
	"history":   {"journal", "rejection"},
}

func (s *Server) find(in findIn) (*mcp.CallToolResult, any, error) {
	if in.K <= 0 {
		in.K = 8
	}
	if in.Scope == "" {
		in.Scope = "all"
	}
	if in.Scope == "code" {
		nodes := s.g.Find(in.Q, in.K, graph.KUnknown)
		if len(nodes) == 0 {
			return text("ok no code matches (graph indexing lands in M1)")
		}
		var lines []string
		for _, n := range nodes {
			lines = append(lines, nodeLine(n))
		}
		return text(budget.Render(lines, ""))
	}
	kinds, ok := scopeKinds[in.Scope]
	if !ok {
		return text("! ARG E - unknown scope " + in.Scope)
	}
	docs, err := s.cache.Search(in.Q, kinds, in.K)
	if err != nil {
		return nil, nil, err
	}
	if len(docs) == 0 {
		return text("ok no matches")
	}
	var lines []string
	for _, d := range docs {
		dir := d.Dir
		if dir == "" {
			dir = "."
		}
		switch d.Kind {
		case "rule":
			lines = append(lines, fmt.Sprintf("r %s %s %s", d.ID, dir, d.Title))
		case "section":
			lines = append(lines, fmt.Sprintf("s %s %s", d.ID, d.Body))
		case "journal", "rejection":
			lines = append(lines, fmt.Sprintf("j %s %s :: %s", d.ID, d.Title, d.Body))
		default: // items
			lines = append(lines, fmt.Sprintf("i %s %s %s %s", d.ID, d.Kind, dir, d.Title))
		}
	}
	return text(budget.Render(lines, ""))
}

// ---- get ----

func (s *Server) get(in getIn) (*mcp.CallToolResult, any, error) {
	if in.Budget <= 0 {
		in.Budget = 2000
	}
	id := strings.TrimSpace(in.ID)
	switch {
	case item.IDRe.MatchString(id):
		return s.getItem(id)
	case reRuleID.MatchString(id):
		return s.getRule(id)
	case strings.HasPrefix(id, "sec:"):
		return s.getSection(id)
	case strings.Contains(id, ":") && !strings.Contains(id, "/"):
		return s.getNode(id, in.Depth, in.Budget)
	default:
		return s.getPath(id, in.Budget)
	}
}

func (s *Server) getItem(id string) (*mcp.CallToolResult, any, error) {
	it, ok, err := item.Get(s.ws, id)
	if err != nil {
		return nil, nil, err
	}
	if !ok {
		return s.nearest(id)
	}
	var b strings.Builder
	b.WriteString(item.Record(it) + "\n")
	if it.Parent != "" {
		b.WriteString("parent " + it.Parent + "\n")
	}
	if len(it.Targets) > 0 {
		b.WriteString("targets " + strings.Join(it.Targets, " ") + "\n")
	}
	if len(it.Rules) > 0 {
		b.WriteString("rules " + strings.Join(it.Rules, " ") + "\n")
	}
	if it.Body != "" {
		b.WriteString(it.Body + "\n")
	}
	return text(b.String())
}

func (s *Server) getRule(id string) (*mcp.CallToolResult, any, error) {
	c, err := spec.Load(s.ws.Dir)
	if err != nil {
		return nil, nil, err
	}
	r, ok := c.Rule(id)
	if !ok {
		return s.nearest(id)
	}
	var b strings.Builder
	dir := filepath.ToSlash(filepath.Dir(filepath.Dir(r.File)))
	if dir == "." {
		dir = "."
	}
	fmt.Fprintf(&b, "r %s %s %s %s\n", r.ID, r.Pattern, dir, r.Text)
	if r.Rationale != "" {
		b.WriteString("rationale " + r.Rationale + "\n")
	}
	anchors, _ := drift.Load(s.ws)
	for _, a := range anchors {
		if a.Rule == id {
			fmt.Fprintf(&b, "a %s %s %s:%d-%d %s\n", a.Rule, a.Node, a.File, a.Start, a.End, a.CHash)
		}
	}
	return text(b.String())
}

func (s *Server) getSection(id string) (*mcp.CallToolResult, any, error) {
	rest := strings.TrimPrefix(id, "sec:")
	dir, name, _ := strings.Cut(rest, "#")
	if dir == "." {
		dir = ""
	}
	c, err := spec.Load(s.ws.Dir)
	if err != nil {
		return nil, nil, err
	}
	sf, ok := c.File(dir)
	if !ok {
		return s.nearest(id)
	}
	for _, sec := range sf.Sections {
		if sec.Name == name {
			return text("s " + id + "\n" + sec.Text + "\n")
		}
	}
	return s.nearest(id)
}

func (s *Server) getNode(id string, depth, tokBudget int) (*mcp.CallToolResult, any, error) {
	n, ok := s.g.Node(graph.NodeID(id))
	if !ok {
		return s.nearest(id)
	}
	var lines []string
	if depth <= 0 {
		lines = []string{nodeLine(n)}
	} else {
		nodes, edges := s.g.Impact([]graph.NodeID{n.ID}, depth, graph.Both, nil)
		for _, x := range nodes {
			lines = append(lines, nodeLine(x))
		}
		for _, e := range edges {
			lines = append(lines, fmt.Sprintf("e %s %s %s via=%s:%d", e.Src, e.Kind, e.Dst, e.File, e.Line))
		}
	}
	kept, cur := budget.TruncateRecords(lines, 0, tokBudget)
	return text(budget.Render(kept, cur))
}

func (s *Server) getPath(p string, tokBudget int) (*mcp.CallToolResult, any, error) {
	p = strings.Trim(filepath.ToSlash(p), "/")
	if p == "." {
		p = ""
	}
	c, err := spec.Load(s.ws.Dir)
	if err != nil {
		return nil, nil, err
	}
	var lines []string
	if strings.Contains(filepath.Base(p), ".") && p != "" {
		// file: resolved contracts (graph nodes join in M1)
		for _, r := range c.ForPath(p) {
			lines = append(lines, ruleLine(r))
		}
	} else {
		// dir: its scoped rules + active items
		probe := p + "/_"
		if p == "" {
			probe = "_"
		}
		for _, r := range c.ForPath(probe) {
			lines = append(lines, ruleLine(r))
		}
		items, err := item.LoadAll(s.ws)
		if err != nil {
			return nil, nil, err
		}
		for _, it := range items {
			if p == "" || it.Dir == p || strings.HasPrefix(it.Dir, p+"/") {
				lines = append(lines, item.Record(it))
			}
		}
	}
	if len(lines) == 0 {
		return text("ok nothing scoped to " + orDot(p))
	}
	kept, cur := budget.TruncateRecords(lines, 0, tokBudget)
	return text(budget.Render(kept, cur))
}

// nearest returns nf corrections instead of an error (SPX-ARC-003).
func (s *Server) nearest(id string) (*mcp.CallToolResult, any, error) {
	docs, _ := s.cache.Search(id, nil, 3)
	var ids []string
	for _, d := range docs {
		ids = append(ids, d.ID)
	}
	for _, n := range s.g.Find(id, 3-len(ids), graph.KUnknown) {
		ids = append(ids, string(n.ID))
	}
	if len(ids) == 0 {
		return text("nf - - -")
	}
	for len(ids) < 3 {
		ids = append(ids, "-")
	}
	return text("nf " + strings.Join(ids[:3], " "))
}

// ---- draft ----

func (s *Server) draft(in draftIn) (*mcp.CallToolResult, any, error) {
	targets := normalizeTargets(in.Targets)
	it, err := lifecycle.Draft(s.ws, in.Kind, in.Title, in.Body, in.Dir, in.Parent, targets)
	if err != nil {
		return text("! ARG E - " + err.Error())
	}
	s.scan.MarkDirty()
	var b strings.Builder
	b.WriteString(item.Record(it) + "\n")
	if len(targets) == 0 {
		return text(b.String())
	}

	// context pack
	c, err := spec.Load(s.ws.Dir)
	if err != nil {
		return nil, nil, err
	}
	b.WriteString("#impact\n")
	var seeds []graph.NodeID
	for _, t := range targets {
		if !strings.ContainsAny(t, "/") && strings.Contains(t, ":") {
			seeds = append(seeds, graph.NodeID(t))
		}
	}
	nodes, edges := s.g.Impact(seeds, 2, graph.Both, nil)
	if len(nodes) == 0 {
		b.WriteString("ok radius empty (graph indexing lands in M1; path targets still resolve contracts)\n")
	}
	for _, n := range nodes {
		b.WriteString(nodeLine(n) + "\n")
	}
	for _, e := range edges {
		fmt.Fprintf(&b, "e %s %s %s via=%s:%d\n", e.Src, e.Kind, e.Dst, e.File, e.Line)
	}

	b.WriteString("#contracts\n")
	seen := map[string]bool{}
	var rl []string
	for _, t := range targets {
		if p, ok := targetPath(t); ok {
			for _, r := range c.ForPath(p) {
				if !seen[r.ID] {
					seen[r.ID] = true
					rl = append(rl, ruleLine(r))
				}
			}
		}
	}
	if len(rl) == 0 {
		probe := it.Dir + "/_"
		if it.Dir == "" {
			probe = "_"
		}
		for _, r := range c.ForPath(probe) {
			rl = append(rl, ruleLine(r))
		}
	}
	if len(rl) == 0 {
		b.WriteString("ok no applicable rules\n")
	}
	for _, l := range rl {
		b.WriteString(l + "\n")
	}

	b.WriteString("#rejections\n")
	docs, err := s.cache.Search(in.Title+" "+strings.Join(targets, " "), []string{"rejection"}, 5)
	if err != nil {
		return nil, nil, err
	}
	if len(docs) == 0 {
		b.WriteString("ok none similar\n")
	}
	for _, d := range docs {
		fmt.Fprintf(&b, "j %s %s :: %s\n", d.ID, d.Title, d.Body)
	}
	return text(b.String())
}

// ---- rule ----

var slotQuestions = map[string]string{
	"pattern":   "EARS pattern: U ubiquitous, E event (WHEN), S state (WHILE), N unwanted (IF/THEN), O optional (WHERE), C complex",
	"system":    "Which system/component does the rule bind (e.g. 'host wrapper')?",
	"response":  "What SHALL it do? Name something verifiable (number, API, error code, artifact).",
	"trigger":   "WHEN — the triggering event",
	"state":     "WHILE — the state during which the rule holds",
	"condition": "IF — the unwanted condition",
	"feature":   "WHERE — the optional feature gating the rule",
}

func missingSlots(in ruleIn) []string {
	if ears.PatternFromString(in.Pattern) == ears.PInvalid {
		return []string{"pattern"}
	}
	return ears.MissingSlots(ears.PatternFromString(in.Pattern), slotsOf(in))
}

func slotsOf(in ruleIn) ears.Slots {
	return ears.Slots{
		System: in.System, Response: in.Response, Trigger: in.Trigger,
		State: in.State, Condition: in.Condition, Feature: in.Feature,
	}
}

func elicitSlots(ctx context.Context, req *mcp.CallToolRequest, in *ruleIn, missing []string) bool {
	props := map[string]any{}
	for _, m := range missing {
		p := map[string]any{"type": "string", "description": slotQuestions[m]}
		if m == "pattern" {
			p["enum"] = []string{"U", "E", "S", "N", "O", "C"}
		}
		props[m] = p
	}
	res, err := req.Session.Elicit(ctx, &mcp.ElicitParams{
		Message: "spectacle: complete the EARS contract",
		RequestedSchema: map[string]any{
			"type": "object", "properties": props, "required": missing,
		},
	})
	if err != nil || res.Action != "accept" {
		return false
	}
	get := func(k string) string { v, _ := res.Content[k].(string); return v }
	for _, m := range missing {
		switch m {
		case "pattern":
			in.Pattern = get(m)
		case "system":
			in.System = get(m)
		case "response":
			in.Response = get(m)
		case "trigger":
			in.Trigger = get(m)
		case "state":
			in.State = get(m)
		case "condition":
			in.Condition = get(m)
		case "feature":
			in.Feature = get(m)
		}
	}
	return true
}

func (s *Server) rule(ctx context.Context, req *mcp.CallToolRequest, in ruleIn) (*mcp.CallToolResult, any, error) {
	defer s.scan.MarkDirty()
	c, err := spec.Load(s.ws.Dir)
	if err != nil {
		return nil, nil, err
	}
	switch in.Op {
	case "add":
		return s.ruleAdd(ctx, req, in, c)
	case "edit":
		return s.ruleEdit(in, c)
	case "retire":
		return s.ruleRetire(in, c)
	}
	return text("! ARG E - op must be add|edit|retire")
}

func (s *Server) ruleAdd(ctx context.Context, req *mcp.CallToolRequest, in ruleIn, c *spec.Cascade) (*mcp.CallToolResult, any, error) {
	for range 3 {
		missing := missingSlots(in)
		if len(missing) == 0 {
			break
		}
		if elicitSlots(ctx, req, &in, missing) {
			continue
		}
		var b strings.Builder
		for _, m := range missing {
			b.WriteString("need " + m + " " + slotQuestions[m] + "\n")
		}
		return text(b.String())
	}
	if missing := missingSlots(in); len(missing) > 0 {
		return text("! ARG E - still missing: " + strings.Join(missing, ", "))
	}
	p := ears.PatternFromString(in.Pattern)
	sentence, err := ears.Compose(p, slotsOf(in))
	if err != nil {
		return text("! ARG E - " + err.Error())
	}
	res, err := spec.AddRule(s.ws, c, spec.AuthorReq{
		Dir: in.Dir, Stem: in.Stem,
		Sentence: sentence, Rationale: in.Rationale, Applies: in.Applies,
	})
	if err != nil {
		return text("! ARG E - " + err.Error())
	}
	var b strings.Builder
	for _, f := range res.Findings {
		b.WriteString(f.String() + "\n")
	}
	if !res.Written {
		b.WriteString("! REJECTED E - fix the slots and retry; nothing was written\n")
		return text(b.String())
	}
	dir := strings.Trim(in.Dir, "/")
	if dir == "" {
		dir = "."
	}
	fmt.Fprintf(&b, "ok %s %s\nr %s %s %s %s\n", res.ID, res.Path, res.ID, p, dir, sentence)
	s.journalRule("add", res.ID, sentence, in.Applies, in.Item, dirOf(in.Dir))
	b.WriteString(s.stampAnchors(res.ID, sentence, in.Applies))
	return text(b.String())
}

func (s *Server) ruleEdit(in ruleIn, c *spec.Cascade) (*mcp.CallToolResult, any, error) {
	if in.ID == "" {
		return text("! ARG E - edit requires id")
	}
	sentence := ""
	if in.Pattern != "" {
		p := ears.PatternFromString(in.Pattern)
		var err error
		sentence, err = ears.Compose(p, slotsOf(in))
		if err != nil {
			return text("! ARG E - " + err.Error())
		}
	}
	res, err := spec.EditRule(s.ws, c, in.ID, sentence, in.Rationale, in.Applies)
	if err != nil {
		return text("! ARG E - " + err.Error())
	}
	var b strings.Builder
	for _, f := range res.Findings {
		b.WriteString(f.String() + "\n")
	}
	if !res.Written {
		b.WriteString("! REJECTED E - nothing was written\n")
		return text(b.String())
	}
	final, _ := c.Rule(in.ID)
	if sentence == "" {
		sentence = final.Text
	}
	fmt.Fprintf(&b, "ok %s %s\n", in.ID, res.Path)
	s.journalRule("edit", in.ID, sentence, in.Applies, in.Item, ruleCtx(res.Path))
	if in.Applies != nil {
		b.WriteString(s.stampAnchors(in.ID, sentence, in.Applies))
	}
	return text(b.String())
}

func (s *Server) ruleRetire(in ruleIn, c *spec.Cascade) (*mcp.CallToolResult, any, error) {
	if in.ID == "" {
		return text("! ARG E - retire requires id")
	}
	old, _ := c.Rule(in.ID)
	file, err := spec.RetireRule(s.ws, c, in.ID)
	if err != nil {
		return text("! ARG E - " + err.Error())
	}
	s.journalRule("retire", in.ID, old.Text, old.Applies, in.Item, ruleCtx(file))
	// drop anchors of the retired rule
	anchors, _ := drift.Load(s.ws)
	var keep []drift.Anchor
	for _, a := range anchors {
		if a.Rule != in.ID {
			keep = append(keep, a)
		}
	}
	if len(keep) != len(anchors) {
		if err := drift.Save(s.ws, keep); err != nil {
			return nil, nil, err
		}
	}
	return text(fmt.Sprintf("ok %s retired from %s (text preserved in journal)", in.ID, file))
}

func (s *Server) journalRule(op, id, txt string, applies []string, itemID, ctx string) {
	_ = journal.Append(s.ws, ctx, journal.Event{
		Ev: journal.EvRule, Op: op, Rule: id, Txt: txt, Ap: applies, Item: itemID, Dir: ctx,
	})
}

// stampAnchors writes/refreshes anchors for a rule's applies list.
func (s *Server) stampAnchors(ruleID, sentence string, applies []string) string {
	if len(applies) == 0 {
		return ""
	}
	anchors, _ := drift.Load(s.ws)
	var b strings.Builder
	for _, node := range applies {
		a := drift.Stamp(s.ws, s.g, ruleID, sentence, graph.NodeID(node))
		anchors = drift.Upsert(anchors, a)
		if a.CHash == "-" {
			fmt.Fprintf(&b, "a %s %s pending (node not indexed yet)\n", ruleID, node)
		} else {
			fmt.Fprintf(&b, "a %s %s %s:%d-%d %s\n", ruleID, node, a.File, a.Start, a.End, a.CHash)
		}
	}
	if err := drift.Save(s.ws, anchors); err != nil {
		return "! IO E - anchors: " + err.Error() + "\n"
	}
	return b.String()
}

// ---- move ----

func (s *Server) move(in moveIn) (*mcp.CallToolResult, any, error) {
	it, err := lifecycle.Move(s.ws, in.ID, in.To, in.Note)
	if err != nil {
		return text("! ARG E - " + err.Error())
	}
	s.scan.MarkDirty()
	return text(item.Record(it) + "\n")
}

// ---- check ----

func (s *Server) check(in checkIn) (*mcp.CallToolResult, any, error) {
	if in.Budget <= 0 {
		in.Budget = 1500
	}
	c, err := spec.Load(s.ws.Dir)
	if err != nil {
		return nil, nil, err
	}
	var lines []string

	// spec lint
	for _, f := range c.Findings() {
		lines = append(lines, f.String())
	}

	// coverage: source dirs with zero applicable rules
	lines = append(lines, s.coverageGaps(c, in.Path)...)

	// duplicate item IDs (branch-merge backstop)
	items, err := item.LoadAll(s.ws)
	if err != nil {
		return nil, nil, err
	}
	seen := map[string]string{}
	for _, it := range items {
		if prev, dup := seen[it.ID]; dup {
			lines = append(lines, fmt.Sprintf("! E101 E %s duplicate item ID %s (also in %s)", orDot(it.Dir), it.ID, orDot(prev)))
		}
		seen[it.ID] = it.Dir
	}

	// drift
	anchors, err := drift.Load(s.ws)
	if err != nil {
		return nil, nil, err
	}
	results := drift.Classify(s.ws, s.g, anchors, func(id string) bool {
		_, ok := c.Rule(id)
		return ok
	})
	changed := false
	pending := 0
	for _, r := range results {
		switch r.Class {
		case drift.OK:
		case drift.Pending:
			pending++
		case drift.Moved:
			n, _ := s.g.Node(r.Anchor.Node)
			end := n.EndLine
			if end == 0 {
				end = n.Line
			}
			a := r.Anchor
			a.File, a.Start, a.End = n.File, n.Line, end
			anchors = drift.Upsert(anchors, a)
			changed = true
		default:
			d := fmt.Sprintf("d %s %s %s %s:%d-%d", r.Class, r.Anchor.Rule, r.Anchor.Node, r.Anchor.File, r.Anchor.Start, r.Anchor.End)
			if in.Fix && (r.Class == drift.Changed || r.Class == drift.Gone) {
				bp, err := s.backprop(c, r)
				if err != nil {
					return nil, nil, err
				}
				d += " item=" + bp
				if r.Class == drift.Changed {
					a := r.Anchor
					a.CHash = r.NewHash
					anchors = drift.Upsert(anchors, a)
					changed = true
				}
			}
			lines = append(lines, d)
		}
	}
	if changed {
		if err := drift.Save(s.ws, anchors); err != nil {
			return nil, nil, err
		}
	}
	if pending > 0 {
		lines = append(lines, fmt.Sprintf("ok %d anchors pending (graph indexing lands in M1)", pending))
	}

	// compact-due signals
	lines = append(lines, s.compactCandidates(in.Path)...)

	if len(lines) == 0 {
		return text("ok")
	}
	kept, cur := budget.TruncateRecords(lines, 0, in.Budget)
	return text(budget.Render(kept, cur))
}

func (s *Server) coverageGaps(c *spec.Cascade, sub string) []string {
	root := filepath.Join(s.ws.Dir, filepath.FromSlash(sub))
	uncovered := map[string]bool{}
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "testdata", "bin", ".spectacle":
				return filepath.SkipDir
			}
			return nil
		}
		if index.LangOf(p) == "" {
			return nil
		}
		rel, _ := filepath.Rel(s.ws.Dir, p)
		rel = filepath.ToSlash(rel)
		if len(c.ForPath(rel)) == 0 {
			uncovered[filepath.ToSlash(filepath.Dir(rel))] = true
		}
		return nil
	})
	var out []string
	for d := range uncovered {
		out = append(out, "g uncovered "+d+" source files with zero applicable rules")
	}
	sort.Strings(out)
	return out
}

// backprop drafts a proposal for one drifted anchor.
func (s *Server) backprop(c *spec.Cascade, r drift.Result) (string, error) {
	rule, _ := c.Rule(r.Anchor.Rule)
	ctx := ruleCtx(rule.File)
	body := fmt.Sprintf("Backprop: code under %s drifted (%s).\nrule: %s\nnode: %s at %s:%d-%d\nold hash %s, new hash %s.\nResolve: rule op=edit id=%s (spec follows code) or revert the code (code follows spec).",
		r.Anchor.Rule, r.Class, rule.Text, r.Anchor.Node, r.Anchor.File, r.Anchor.Start, r.Anchor.End, r.Anchor.CHash, orDash(r.NewHash), r.Anchor.Rule)
	it, err := lifecycle.Draft(s.ws, "proposal", fmt.Sprintf("backprop %s %s", r.Anchor.Rule, r.Class), body, ctx, "", []string{string(r.Anchor.Node)})
	if err != nil {
		return "", err
	}
	_ = journal.Append(s.ws, ctx, journal.Event{
		Ev: journal.EvDrift, Rule: r.Anchor.Rule, Node: string(r.Anchor.Node),
		Cls: string(r.Class), Oh: r.Anchor.CHash, Nh: r.NewHash, Item: it.ID, Dir: ctx,
	})
	return it.ID, nil
}

// ---- compact ----

func (s *Server) compactCandidates(sub string) []string {
	var out []string
	ctxs, err := s.ws.ContextDirs()
	if err != nil {
		return out
	}
	items, _ := item.LoadAll(s.ws)
	done := 0
	for _, it := range items {
		if it.State == item.StateDone && within(sub, it.Dir) {
			done++
		}
	}
	if done >= s.ws.Cfg.Compact.DoneMax {
		out = append(out, fmt.Sprintf("c . done %d done items awaiting archive", done))
	}
	for _, ctx := range ctxs {
		if !within(sub, ctx) {
			continue
		}
		events, err := journal.Read(s.ws, ctx)
		if err != nil {
			continue
		}
		since := 0
		for _, e := range events {
			if e.Ev == journal.EvCompact {
				since = 0
				continue
			}
			since++
		}
		if since >= s.ws.Cfg.Compact.JournalMax {
			out = append(out, fmt.Sprintf("c %s journal %d events since last compact", orDot(ctx), since))
		}
	}
	return out
}

func (s *Server) compact(in compactIn) (*mcp.CallToolResult, any, error) {
	defer s.scan.MarkDirty()
	var b strings.Builder
	cands := s.compactCandidates(in.Path)
	items, err := item.LoadAll(s.ws)
	if err != nil {
		return nil, nil, err
	}
	var doneItems []item.Item
	for _, it := range items {
		if it.State == item.StateDone && within(in.Path, it.Dir) {
			doneItems = append(doneItems, it)
		}
	}
	if len(cands) == 0 && len(doneItems) == 0 {
		return text("ok nothing to compact")
	}
	for _, c := range cands {
		b.WriteString(c + "\n")
	}
	for _, it := range doneItems {
		fmt.Fprintf(&b, "c %s done-item %s %s\n", orDot(it.Dir), it.ID, it.Title)
	}
	if !in.Apply {
		b.WriteString("ok dry-run — pass apply=true to execute\n")
		return text(b.String())
	}

	// archive done items (skipping ones with open children)
	for _, it := range doneItems {
		if _, err := lifecycle.Move(s.ws, it.ID, item.StateArchived, "compact"); err != nil {
			fmt.Fprintf(&b, "! SKIP W %s %s\n", it.ID, err.Error())
		} else {
			fmt.Fprintf(&b, "ok archived %s\n", it.ID)
		}
	}
	// fold journals over threshold: drop create/move/rule/drift events,
	// keep reject/archive/compact verbatim, append a compact event
	ctxs, err := s.ws.ContextDirs()
	if err != nil {
		return nil, nil, err
	}
	for _, ctx := range ctxs {
		if !within(in.Path, ctx) {
			continue
		}
		events, err := journal.Read(s.ws, ctx)
		if err != nil {
			return nil, nil, err
		}
		if len(events) < s.ws.Cfg.Compact.JournalMax {
			continue
		}
		var keep []journal.Event
		folded := 0
		for _, e := range events {
			switch e.Ev {
			case journal.EvReject, journal.EvArchive, journal.EvCompact:
				keep = append(keep, e)
			default:
				folded++
			}
		}
		if folded == 0 {
			continue
		}
		if err := journal.Rewrite(s.ws, ctx, keep); err != nil {
			return nil, nil, err
		}
		if err := journal.Append(s.ws, ctx, journal.Event{
			Ev: journal.EvCompact, N: folded, Note: "journal fold", Dir: ctx,
		}); err != nil {
			return nil, nil, err
		}
		fmt.Fprintf(&b, "ok folded %d events in %s\n", folded, orDot(ctx))
	}
	return text(b.String())
}

// ---- shared helpers ----

func nodeLine(n graph.Node) string {
	l := fmt.Sprintf("n %s %s %s:%d", n.ID, n.Kind, n.File, n.Line)
	if n.Sig != "" {
		l += " sig=" + n.Sig
	}
	return l
}

func ruleLine(r spec.ResolvedRule) string {
	return fmt.Sprintf("r %s %s %s %s", r.ID, r.Pattern, r.ScopeDir, r.Text)
}

// targetPath decides whether a target is a file path (as opposed to a node
// ID) and strips an optional ":line" suffix.
func targetPath(t string) (string, bool) {
	if i := strings.IndexByte(t, ':'); i > 0 {
		if !strings.ContainsAny(t[:i], "./") {
			return "", false
		}
		return t[:i], true
	}
	return t, strings.ContainsAny(t, "./")
}

func normalizeTargets(ts []string) []string {
	var out []string
	for _, t := range ts {
		if p, ok := targetPath(t); ok {
			out = append(out, p)
		} else {
			out = append(out, t)
		}
	}
	return out
}

func ruleCtx(specRel string) string {
	dir := filepath.ToSlash(filepath.Dir(filepath.Dir(specRel)))
	if dir == "." {
		return ""
	}
	return dir
}

func dirOf(d string) string {
	d = strings.Trim(filepath.ToSlash(d), "/")
	if d == "." {
		return ""
	}
	return d
}

func within(sub, dir string) bool {
	sub = dirOf(sub)
	if sub == "" {
		return true
	}
	return dir == sub || strings.HasPrefix(dir, sub+"/")
}

func orDot(s string) string {
	if s == "" {
		return "."
	}
	return s
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
