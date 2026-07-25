package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jxsl13/spectackle/internal/budget"
	"github.com/jxsl13/spectackle/internal/drift"
	"github.com/jxsl13/spectackle/internal/ears"
	"github.com/jxsl13/spectackle/internal/graph"
	"github.com/jxsl13/spectackle/internal/index"
	"github.com/jxsl13/spectackle/internal/item"
	"github.com/jxsl13/spectackle/internal/journal"
	"github.com/jxsl13/spectackle/internal/lifecycle"
	"github.com/jxsl13/spectackle/internal/spec"
)

// The 7-tool surface is documented in docs/tools.md; the structs below are
// the normative schemas (SPX-REPO-001). Results use the dense line grammar,
// not JSON — that is where the token savings come from. (state's stateIn
// lives in state.go — the read-only overview tool, 11th on the full surface.)

var reRuleID = regexp.MustCompile(`^[A-Z][A-Z0-9]*(-[A-Z0-9]+)*-\d{3}$`)

// ---- input structs (JSON Schemas are inferred from these) ----

type findIn struct {
	Q      string `json:"q" jsonschema:"text or ID fragment"`
	Scope  string `json:"scope,omitempty" jsonschema:"code|rule|spec|proposal|task|bug|research|adr|rejection|history|all, default all"`
	K      int    `json:"k,omitempty" jsonschema:"max results, default 8"`
	Focus  string `json:"focus,omitempty" jsonschema:"node ID; scope=code only: rank matches by personalized PageRank around this node, default empty = global rank"`
	Budget int    `json:"budget,omitempty" jsonschema:"token budget, default 2000"`
	Cur    string `json:"cur,omitempty" jsonschema:"resume cursor"`
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
	Cur    string `json:"cur,omitempty" jsonschema:"resume cursor"`
}

type compactIn struct {
	Path  string `json:"path,omitempty" jsonschema:"context dir, default all"`
	Apply bool   `json:"apply,omitempty" jsonschema:"execute (default: dry-run listing candidates)"`
}

// knowledgeEntryIn is one brownfield-authored entry for knowledge
// op=export's mode (b): a repository with no .spectackle bundle at all,
// where the caller (an LLM that surveyed code/tests/docs) authors entries
// directly instead of them being lifted by knowledge.Extract. Every field
// here maps straight onto knowledge.Entry's payload; there is deliberately
// no key/id field — knowledge.NewEntry computes the content key itself, so
// a caller cannot supply (or corrupt) one even by mistake.
type knowledgeEntryIn struct {
	Kind         string   `json:"kind" jsonschema:"rule|adr|intent"`
	Dir          string   `json:"dir,omitempty" jsonschema:"context dir this entry was drawn from, default root"`
	Text         string   `json:"text,omitempty" jsonschema:"rule: the composed EARS sentence"`
	Rationale    string   `json:"rationale,omitempty" jsonschema:"rule: optional rationale"`
	Question     string   `json:"question,omitempty" jsonschema:"adr: the decision question"`
	Context      string   `json:"context,omitempty" jsonschema:"adr: forces and constraints behind the decision"`
	Decision     string   `json:"decision,omitempty" jsonschema:"adr: the chosen option"`
	Consequences string   `json:"consequences,omitempty" jsonschema:"adr: trade-offs and follow-on effects"`
	Status       string   `json:"status,omitempty" jsonschema:"adr: proposed|accepted|superseded|deprecated"`
	Options      []string `json:"options,omitempty" jsonschema:"adr: rejected alternatives"`
	Prose        string   `json:"prose,omitempty" jsonschema:"intent: prose section text, verbatim"`
}

// knowledgeIn is the one input struct for the knowledge tool (op=export|
// merge|apply — see internal/mcpserver/knowledge.go). Fields are shared
// across ops rather than nested per-op, matching this file's flat-params
// convention (SPX-ARC-004): Path/Body/Paths are read tool-agnostically —
// which ones apply depends on Op, documented per-field below.
type knowledgeIn struct {
	Op string `json:"op" jsonschema:"export|merge|apply"`

	// export: write the marshaled artifact here too (still returned
	// inline) — a fleet workflow needs a file to move between
	// repositories. apply: read the artifact to fold in from this path
	// (alternative to Body).
	Path string `json:"path,omitempty" jsonschema:"export: also write the artifact here; apply: read the artifact from this path"`

	// apply: inline artifact text, alternative to Path. merge: one more
	// artifact to merge, alongside Paths.
	Body string `json:"body,omitempty" jsonschema:"inline artifact text — apply: the artifact to fold in; merge: one more artifact to merge, alongside paths"`

	// merge: artifact file paths to parse and merge (2 or more, typically;
	// combine with Body for one more inline artifact).
	Paths []string `json:"paths,omitempty" jsonschema:"merge: artifact file paths to parse and merge"`

	// export mode (b), brownfield: caller-authored entries for a repo with
	// no .spectackle bundle. Omitted/empty selects mode (a): walk this
	// workspace's own cascade+items via knowledge.Extract.
	Entries []knowledgeEntryIn `json:"entries,omitempty" jsonschema:"export brownfield mode: caller-authored entries (no .spectackle bundle here) — each is validated+keyed via knowledge.NewEntry, a supplied key is impossible"`
}

func text(s string) (*mcp.CallToolResult, any, error) {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: s}}}, nil, nil
}

// gate serializes the handler (the SDK dispatches tool calls concurrently,
// but lifecycle writes are read-modify-write over shared files), runs the
// swarm bookkeeping + debounced cache sync before it, and piggybacks unseen
// sibling learnings (sw records) onto the result.
func gate[T any](s *Server, h func(T) (*mcp.CallToolResult, any, error)) func(context.Context, *mcp.CallToolRequest, T) (*mcp.CallToolResult, any, error) {
	return func(_ context.Context, _ *mcp.CallToolRequest, in T) (*mcp.CallToolResult, any, error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		if err := s.preCall(); err != nil {
			return nil, nil, err
		}
		res, out, err := h(in)
		return s.postCall(res), out, err
	}
}

func (s *Server) registerTools() {
	mcp.AddTool(s.mcp, &mcp.Tool{Name: "find",
		Description: "Unified search. scope: code→nodes(n), rule→EARS(r), spec→prose(s), proposal|task|bug|research|adr→items(i), rejection→past rejections(j), history→journal(j), all→mixed. ALWAYS search rejection+history before drafting."},
		gate(s, s.find))

	mcp.AddTool(s.mcp, &mcp.Tool{Name: "get",
		Description: "Read one thing by ID. Item→header+body. Rule→text+rationale+anchors(a). Node with depth>0→cross-language impact radius (n/e). Dir→its rules+items. File→resolved contracts. sec:<dir>#<name>→prose. Unknown→nf with nearest."},
		gate(s, s.get))

	mcp.AddTool(s.mcp, &mcp.Tool{Name: "draft",
		Description: "Create a lifecycle item (state=draft) in the correct .spectackle/work.md — server assigns ID+scope. With targets the response is a CONTEXT PACK: #impact, #contracts, #rejections — read it before writing code. Never edit .spectackle files yourself."},
		gate(s, s.draft))

	mcp.AddTool(s.mcp, &mcp.Tool{Name: "rule",
		Description: "Author EARS contracts — the ONLY write path for rules. add: fill slots (missing ones elicited or returned as need records); server composes+lints (errors reject, nothing written), auto-IDs, anchors applies. edit: recompose/relink by id. retire: remove; text survives in journal."},
		func(ctx context.Context, req *mcp.CallToolRequest, in ruleIn) (*mcp.CallToolResult, any, error) {
			s.mu.Lock()
			defer s.mu.Unlock()
			if err := s.preCall(); err != nil {
				return nil, nil, err
			}
			res, out, err := s.rule(ctx, req, in)
			return s.postCall(res), out, err
		})

	mcp.AddTool(s.mcp, &mcp.Tool{Name: "move",
		Description: "Transition a lifecycle item. States are totally ordered (draft<submitted<approved<active<done<archived): ANY forward skip is one call (draft→active, active→archived implies done). rejected REQUIRES note (item leaves work.md; summary stays searchable via find scope=rejection) and is revocable back to draft/submitted/approved/active. archived needs no open children; merges the delta into spec.md. approve/reject only on explicit user instruction."},
		gate(s, s.move))

	mcp.AddTool(s.mcp, &mcp.Tool{Name: "check",
		Description: "Verify: spec lint (!), coverage gaps (g), drift via anchors (d gone|changed|stale), duplicate item IDs, compact-due (c). fix=true drafts one backprop proposal per drifted rule and re-stamps anchors. Run until ok before move to=done."},
		gate(s, s.check))

	mcp.AddTool(s.mcp, &mcp.Tool{Name: "compact",
		Description: "Housekeeping. Dry-run lists candidates (c): done-unarchived items, journal folds. apply=true executes — reject events are NEVER dropped. Trigger when check emits c records."},
		gate(s, s.compact))

	mcp.AddTool(s.mcp, &mcp.Tool{Name: "lease",
		Description: "Scope leases stop agent collisions. claim: reserve dirs/files/item IDs (auto-refreshed each call; conflict → l line naming the holder). release: drop — do this the moment your item is done, a stale claim blocks siblings until TTL expiry. ls: all live leases. work op=start auto-claims its item+targets — explicit claims only for extra scope."},
		gate(s, s.lease))

	mcp.AddTool(s.mcp, &mcp.Tool{Name: "work",
		Description: "Worktree lifecycle for an approved item. start: lease item+targets, create git worktree+branch, re-root this session (wt line = YOUR edit/build root; spectackle paths stay repo-relative). submit: gate (config verify + item goal) → commit code → integrate main → re-gate → ff-merge → replay .spectackle state → teardown. abort: teardown, item back to approved. status: wt lines. Fix gate/merge failures in the worktree, then submit again."},
		gate(s, s.work))

	mcp.AddTool(s.mcp, &mcp.Tool{Name: "swarm",
		Description: "Sibling awareness, zero params: ag agents (item, freshness), l leases, wt open worktrees, sw recent learnings (rejections first). Check before claiming scope or hypothesizing — a sibling may have failed at it already."},
		gate(s, s.swarm))

	mcp.AddTool(s.mcp, &mcp.Tool{Name: "research",
		Description: "Condensed problem-space pack, STRICTLY read-only: #impact #contracts #rejections #history #docs #gaps #open (q = server-generated open questions). Run BEFORE asking the user or drafting; mint an R-item for a cheap subagent only if this pack cannot answer it. Orchestrator-only — implementers never research."},
		gate(s, s.research))

	mcp.AddTool(s.mcp, &mcp.Tool{Name: "grill",
		Description: "Critique pack before delegation: #targets #contracts #briefs (b = thin child-task bodies) #tests #rejections #questions. Stamps grilled: <date> on the item — the evidence move checks before approve. Close every gap it surfaces, then approve."},
		gate(s, s.grill))

	mcp.AddTool(s.mcp, &mcp.Tool{Name: "decide",
		Description: "Structured user decisions — never unstructured chat. ask: native UI form (radio|confirm|text) via elicitation; without UI the ADR-item stays open (need decision …) and is answered later from ANY session via op=answer. Decisions on blocked items drive the exits rescope|reject|override-once. ls: open decisions."},
		func(ctx context.Context, req *mcp.CallToolRequest, in decideIn) (*mcp.CallToolResult, any, error) {
			s.mu.Lock()
			defer s.mu.Unlock()
			if err := s.preCall(); err != nil {
				return nil, nil, err
			}
			res, out, err := s.decide(ctx, req, in)
			return s.postCall(res), out, err
		})

	mcp.AddTool(s.mcp, &mcp.Tool{Name: "state",
		Description: "One read-only structured snapshot: #version #items #rules #graph #swarm #drift #health — the full spec-driven-development picture in one call; writes nothing."},
		gate(s, s.state))

	mcp.AddTool(s.mcp, &mcp.Tool{Name: "knowledge",
		Description: "Portable knowledge: export/merge/apply rules+ADRs+intent across repositories. export: this workspace's cascade+items -> artifact (no entries), or brownfield caller-authored entries (entries=..., validated+keyed via NewEntry) for a repo with no .spectackle bundle; path also writes the file. merge: N artifacts (path/body) -> one condensate; conflicts (same question, different decision) are reported as x records, NEVER auto-resolved. apply: fold ONE artifact into this workspace — additive only, idempotent, dedups on content key not rule ID; rules go through rule op=add's composer, ADRs through the decide path, no new write path; reports added= and gaps= in one call."},
		gate(s, s.knowledge))

	mcp.AddTool(s.mcp, &mcp.Tool{Name: "commands",
		Description: "Generate harness-native slash-command/prompt files from the spectackle templates. detect: sniff which harnesses (claude|copilot|codex|kimi) are wired into the repo from root markers (h lines). gen: (re)write their command files — harness list is arg > detection > elicitation (native checkbox form); no UI/declined leaves an adr item open (need decision …) instead of blocking."},
		func(ctx context.Context, req *mcp.CallToolRequest, in commandsIn) (*mcp.CallToolResult, any, error) {
			s.mu.Lock()
			defer s.mu.Unlock()
			if err := s.preCall(); err != nil {
				return nil, nil, err
			}
			res, out, err := s.commands(ctx, req, in)
			return s.postCall(res), out, err
		})
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
	"adr":       {"adr"},
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
	if in.Budget <= 0 {
		in.Budget = 2000
	}
	if in.Scope == "code" {
		k := in.K
		if in.Focus != "" {
			k *= 4 // over-fetch, PPR decides the final K (SPX-GRA-004)
		}
		nodes := s.g.Find(in.Q, k, graph.KUnknown)
		if len(nodes) == 0 {
			return text("ok no code matches")
		}
		if in.Focus != "" {
			if _, ok := s.g.Node(graph.NodeID(in.Focus)); !ok {
				return s.nearest(in.Focus)
			}
			score := graph.PersonalizedRank(s.g, []graph.NodeID{graph.NodeID(in.Focus)}, 4, 20, 0.85)
			sort.SliceStable(nodes, func(i, j int) bool {
				return score[nodes[i].ID] > score[nodes[j].ID]
			})
			if len(nodes) > in.K {
				nodes = nodes[:in.K]
			}
		}
		var lines []string
		for _, n := range nodes {
			lines = append(lines, nodeLine(n))
		}
		kept, cur := budget.TruncateRecords(lines, budget.Resume(in.Cur), in.Budget)
		return text(budget.Render(kept, cur))
	}
	kinds, ok := scopeKinds[in.Scope]
	if !ok {
		return text("! ARG E - unknown scope " + in.Scope)
	}
	docs, err := s.cache.Search(in.Q, kinds, in.K)
	if err != nil {
		return nil, nil, err
	}
	var lines []string
	// union in live sibling learnings: a rejection in another agent's
	// worktree is visible here BEFORE it ever merges to main
	if in.Scope == "rejection" || in.Scope == "history" || in.Scope == "all" {
		evKinds := []string{"reject"}
		if in.Scope == "history" || in.Scope == "all" {
			evKinds = nil
		}
		if events, err := s.cd.SearchEvents(in.Q, evKinds, in.K); err == nil {
			for _, e := range events {
				lines = append(lines, swLine(e))
			}
		}
	}
	if len(docs) == 0 && len(lines) == 0 {
		return text("ok no matches")
	}
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
	kept, cur := budget.TruncateRecords(lines, budget.Resume(in.Cur), in.Budget)
	return text(budget.Render(kept, cur))
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
		return s.getNode(id, in.Depth, in.Budget, budget.Resume(in.Cur))
	default:
		return s.getPath(id, in.Budget, budget.Resume(in.Cur))
	}
}

func (s *Server) getItem(id string) (*mcp.CallToolResult, any, error) {
	it, ok, err := item.Get(s.ws, id)
	if err != nil {
		return nil, nil, err
	}
	if !ok {
		tomb, tombOk, err := lifecycle.Tombstone(s.ws, id)
		if err != nil {
			return nil, nil, err
		}
		if !tombOk {
			return s.nearest(id)
		}
		return text(item.Record(tomb) + " (archived; journal tombstone)\n")
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
	if it.Context != "" {
		b.WriteString("context: " + it.Context + "\n")
	}
	if it.Decision != "" {
		b.WriteString("decision: " + it.Decision + "\n")
	}
	if it.Consequences != "" {
		b.WriteString("consequences: " + it.Consequences + "\n")
	}
	if it.Status != "" {
		b.WriteString("status: " + it.Status + "\n")
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

func (s *Server) getNode(id string, depth, tokBudget, offset int) (*mcp.CallToolResult, any, error) {
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
	// binding contracts of the requested node only (SPX-SPC-007) — impact
	// neighbors stay bare; root-scoped rules collapse to one r-root record.
	if c, err := spec.Load(s.ws.Dir); err == nil {
		var rl, rootIDs []string
		splitContractRules(c.ForNode(id, n.File), nil, map[string]bool{}, &rl, &rootIDs)
		lines = append(lines, rl...)
		if len(rootIDs) > 0 {
			lines = append(lines, "r-root "+strings.Join(rootIDs, " "))
		}
	}
	kept, cur := budget.TruncateRecords(lines, offset, tokBudget)
	return text(budget.Render(kept, cur))
}

func (s *Server) getPath(p string, tokBudget, offset int) (*mcp.CallToolResult, any, error) {
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
	kept, cur := budget.TruncateRecords(lines, offset, tokBudget)
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
	if s.wtItem == "" { // inside a worktree the scope is already leased
		if res, out, err := s.blockedByLease(targets); res != nil || err != nil {
			return res, out, err
		}
	}
	it, err := lifecycle.Draft(s.ws, s.minter(), in.Kind, in.Title, in.Body, in.Dir, in.Parent, targets)
	if err != nil {
		return text("! ARG E - " + err.Error())
	}
	s.scan.MarkDirty()
	var b strings.Builder
	b.WriteString(item.Record(it) + "\n")
	if len(targets) == 0 {
		return text(b.String())
	}

	// context pack — sections are omitted entirely when empty (no filler
	// lines); root-scoped rules collapse to one ID-only r-root record since
	// their full text is stable knowledge available via get.
	c, err := spec.Load(s.ws.Dir)
	if err != nil {
		return nil, nil, err
	}

	var impact strings.Builder
	var seeds []graph.NodeID
	for _, t := range targets {
		if !strings.ContainsAny(t, "/") && strings.Contains(t, ":") {
			seeds = append(seeds, graph.NodeID(t))
		}
	}
	nodes, edges := s.g.Impact(seeds, 2, graph.Both, nil)
	for _, n := range nodes {
		impact.WriteString(nodeLine(n) + "\n")
	}
	for _, e := range edges {
		fmt.Fprintf(&impact, "e %s %s %s via=%s:%d\n", e.Src, e.Kind, e.Dst, e.File, e.Line)
	}
	if impact.Len() > 0 {
		b.WriteString("#impact\n")
		// typed call edges can blow the radius up — keep the pack bounded
		// (SPX-ARC-002); a cur record marks the cut.
		lines := strings.Split(strings.TrimRight(impact.String(), "\n"), "\n")
		kept, cur := budget.TruncateRecords(lines, 0, 1200)
		b.WriteString(budget.Render(kept, cur))
		if !strings.HasSuffix(b.String(), "\n") {
			b.WriteString("\n")
		}
	}

	seen := map[string]bool{}
	rootSeen := map[string]bool{}
	var rl []string
	var rootIDs []string
	for _, t := range targets {
		if p, ok := targetPath(t); ok {
			splitContractRules(c.ForPath(p), seen, rootSeen, &rl, &rootIDs)
		}
	}
	if len(rl) == 0 && len(rootIDs) == 0 {
		probe := it.Dir + "/_"
		if it.Dir == "" {
			probe = "_"
		}
		splitContractRules(c.ForPath(probe), nil, rootSeen, &rl, &rootIDs)
	}
	var contracts strings.Builder
	if len(rootIDs) > 0 {
		contracts.WriteString("r-root " + strings.Join(rootIDs, " ") + "\n")
	}
	for _, l := range rl {
		contracts.WriteString(l + "\n")
	}
	if contracts.Len() > 0 {
		b.WriteString("#contracts\n")
		b.WriteString(contracts.String())
	}

	docs, err := s.cache.Search(in.Title+" "+strings.Join(targets, " "), []string{"rejection"}, 5)
	if err != nil {
		return nil, nil, err
	}
	if len(docs) > 0 {
		b.WriteString("#rejections\n")
		for _, d := range docs {
			fmt.Fprintf(&b, "j %s %s :: %s\n", d.ID, d.Title, d.Body)
		}
	}
	return text(b.String())
}

// splitContractRules buckets resolved rules into full r-lines (rl) and
// root-scoped IDs (rootIDs), each deduped by rule ID. seen may be nil to
// skip non-root dedup (fresh probe list); rootSeen must not be nil.
func splitContractRules(rules []spec.ResolvedRule, seen, rootSeen map[string]bool, rl, rootIDs *[]string) {
	for _, r := range rules {
		if r.ScopeDir == "." {
			if !rootSeen[r.ID] {
				rootSeen[r.ID] = true
				*rootIDs = append(*rootIDs, r.ID)
			}
			continue
		}
		if seen != nil {
			if seen[r.ID] {
				continue
			}
			seen[r.ID] = true
		}
		*rl = append(*rl, ruleLine(r))
	}
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
		Message: "spectackle: complete the EARS contract",
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
	if s.wtItem == "" {
		if res, out, err := s.blockedByLease([]string{dirOf(in.Dir)}); res != nil || err != nil {
			return res, out, err
		}
	}
	res, err := spec.AddRule(s.ws, c, spec.AuthorReq{
		Dir: in.Dir, Stem: in.Stem, Mint: s.ruleMinter(),
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
	// a retired rule keeps no anchors at all: reconcile against an empty
	// keep set so every row for in.ID is dropped, not just the ones that
	// happen to match the rule's last-known applies.
	anchors, _ := drift.Load(s.ws)
	reconciled := drift.Reconcile(anchors, in.ID, nil)
	if len(reconciled) != len(anchors) {
		if err := drift.Save(s.ws, reconciled); err != nil {
			return nil, nil, err
		}
	}
	return text(fmt.Sprintf("ok %s retired from %s (text preserved in journal)", in.ID, file))
}

func (s *Server) journalRule(op, id, txt string, applies []string, itemID, ctx string) {
	_ = journal.Append(s.ws, ctx, journal.Event{
		Ev: journal.EvRule, Op: op, Rule: id, Txt: txt, Ap: applies, Item: itemID, Dir: ctx,
	})
	// dual-write: siblings learn about contract changes before any merge
	_ = s.cd.Emit("rule", id, op+": "+txt)
}

// stampAnchors writes/refreshes anchors for a rule's applies list. It first
// reconciles anchors.tsv down to exactly this applies set for ruleID, so a
// node dropped from applies (or replaced after a mistyped one) loses its
// anchor row here instead of lingering as a permanently-stale one — this
// makes add idempotent too: adding the same rule again reconciles to
// exactly the applies passed this time.
func (s *Server) stampAnchors(ruleID, sentence string, applies []string) string {
	if len(applies) == 0 {
		return ""
	}
	anchors, _ := drift.Load(s.ws)
	keep := make([]graph.NodeID, len(applies))
	for i, node := range applies {
		keep[i] = graph.NodeID(node)
	}
	anchors = drift.Reconcile(anchors, ruleID, keep)
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
	if s.wtItem == "" {
		if res, out, err := s.blockedByLease([]string{in.ID}); res != nil || err != nil {
			return res, out, err
		}
	}
	// grill/needs gates on forward moves (soft warnings by default;
	// feedback.grill=require hard-blocks ungrilled proposals).
	warns := ""
	if forwardState(in.To) {
		if pre, ok, _ := item.Get(s.ws, in.ID); ok {
			if pre.Kind == "proposal" && pre.Grilled == "" {
				if s.ws.Cfg.Feedback.Grill == "require" {
					return text("! GRILL E " + in.ID + " ungrilled — grill first (feedback.grill=require)")
				}
				warns += "! GRILL W " + in.ID + " ungrilled — grill first or proceed deliberately\n"
			}
			if open := s.openNeeds(pre); len(open) > 0 {
				warns += "! NEEDS W " + in.ID + " open needs: " + strings.Join(open, " ") + "\n"
			}
		}
	}
	it, err := lifecycle.Move(s.ws, in.ID, in.To, in.Note, s.auditGateOpts()...)
	if err != nil {
		var rex lifecycle.ErrRoundsExhausted
		if errors.As(err, &rex) {
			// anti-ping-pong: the reopen budget is spent — server-side
			// escalation to the blocked side state + auto-minted adr item.
			blocked, dec, eErr := lifecycle.Escalate(s.ws, s.minter(), rex.Item)
			if eErr != nil {
				return nil, nil, eErr
			}
			_ = s.cd.Emit("escalate", blocked.ID, "rounds limit — decide "+dec.ID)
			s.scan.MarkDirty()
			return text(fmt.Sprintf("i %s %s blocked %s %s\n! ROUNDS E %s rounds exhausted — decide %s (rescope|reject|override-once)\n",
				blocked.ID, blocked.Kind, orDot(blocked.Dir), blocked.Title, blocked.ID, dec.ID))
		}
		if strings.HasPrefix(err.Error(), "! GATE E") {
			// auditGate's refusal is already a dense record (one line per
			// offending anchor) — return it verbatim as the tool result,
			// not wrapped in another "! ARG E -" prefix that would corrupt
			// the record grammar callers parse.
			return text(err.Error() + "\n")
		}
		return text("! ARG E - " + err.Error())
	}
	if in.To == item.StateRejected {
		// dual-write: the rejection reaches siblings before any merge
		_ = s.cd.Emit("reject", in.ID, it.Title+" :: "+in.Note)
	}
	s.scan.MarkDirty()
	return text(warns + item.Record(it) + "\n")
}

// auditGateOpts loads the spec cascade and, if that succeeds, returns a
// lifecycle.WithAuditGate option wired exactly like check builds its
// rule-text resolver (s.g plus a closure over c.Rule) — this is what arms
// the done-state audit gate (see lifecycle.Move's doc comment) at the
// server's move call sites. spec.Load failing is not a move failure: the
// cascade is simply unavailable, so the returned slice is empty and Move
// runs with the gate skipped, exactly as it did before this option existed.
func (s *Server) auditGateOpts() []lifecycle.MoveOption {
	c, err := spec.Load(s.ws.Dir)
	if err != nil {
		return nil
	}
	return []lifecycle.MoveOption{lifecycle.WithAuditGate(s.g, func(id string) (string, bool) {
		r, ok := c.Rule(id)
		return r.Text, ok
	})}
}

// forwardState reports whether a move target is a forward position in the
// total order (the grill/needs gates only guard forward progress).
func forwardState(to string) bool {
	switch to {
	case item.StateSubmitted, item.StateApproved, item.StateActive, item.StateDone, item.StateArchived:
		return true
	}
	return false
}

// openNeeds returns the subset of an item's needs that are still unresolved:
//   - referenced item exists in work.md and is not done -> open;
//   - referenced item exists in work.md and is done -> resolved;
//   - referenced item is gone from work.md but resolves via
//     lifecycle.Tombstone (i.e. it was archived) -> resolved — archiving is
//     itself a completion;
//   - referenced item resolves nowhere at all (bad ID, or gone for any
//     other reason) -> open, conservatively: unknown is never treated as
//     done.
func (s *Server) openNeeds(it item.Item) []string {
	var open []string
	for _, n := range it.Needs {
		if dep, ok, _ := item.Get(s.ws, n); ok {
			if dep.State != item.StateDone {
				open = append(open, n)
			}
			continue
		}
		if _, tombOk, _ := lifecycle.Tombstone(s.ws, n); tombOk {
			continue
		}
		open = append(open, n)
	}
	return open
}

// ---- check ----

// staleFile reports whether file (repo-relative, as stored on graph.Node)
// was modified after the active root's graph was last successfully
// rebuilt (DRF-003). Under the resident -http server, s.reindex only runs
// at startup and on reroot (SPX-MCP-003 refreshes the .spectackle files,
// never the graph), so a code edit made through any other channel leaves
// s.g's node Line/EndLine pointing at positions that no longer mean what
// they did when they were indexed; hashing that stale range against the
// new file content silently produces a hash for a span that isn't the
// node. check wires this into drift.Classify (as its stale predicate) so
// such an anchor degrades to Pending instead of a hash-based verdict —
// see drift.Classify's doc comment for the false-heal this closes
// (go:main.main hashed twice at stale positions in this repo, both
// auto-healed as Evolved, both wrong).
//
// A stat failure (file deleted, permission error) reports not-stale:
// Classify already has a dedicated Gone path once SpanHash itself fails to
// read the file, so staleFile's job is narrowly "is the on-disk file newer
// than the graph", not "does the file still exist".
func (s *Server) staleFile(file string) bool {
	fi, err := os.Stat(filepath.Join(s.ws.Dir, file))
	if err != nil {
		return false
	}
	return fi.ModTime().After(s.indexedAt)
}

// anchorsNeedRefresh reports whether any anchor's recorded file has changed
// on disk since the graph was last built — the gate for check's conditional
// reindex (DRF-003). Anchor.File is used directly rather than resolving
// through s.g.Node first: it is what SpanHash will read regardless of
// whether the node itself has since moved files, it is already populated
// on every non-pending anchor, and checking it costs no graph lookup.
// Pending anchors (File == "-": the node was never indexed) are skipped —
// there is nothing on disk yet to compare a graph timestamp against.
func (s *Server) anchorsNeedRefresh(anchors []drift.Anchor) bool {
	for _, a := range anchors {
		if a.File == "-" {
			continue
		}
		if s.staleFile(a.File) {
			return true
		}
	}
	return false
}

func (s *Server) check(in checkIn) (*mcp.CallToolResult, any, error) {
	// T-0086 live proof: default budget is 1500 tokens; this comment only
	// shifts the function's code hash — the MCP-004 rule sentence above is
	// untouched, so re-running check classifies this anchor as Evolved and
	// heals it mechanically (no fix=true needed).
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

	// orphan applies targets: a live rule declares {applies: node} but the
	// (rule,node) anchor row is missing — binding intent without a binding
	// (MCP-004). One dense record per missing pair.
	anchored := map[string]bool{}
	for _, a := range anchors {
		anchored[a.Rule+"\x00"+string(a.Node)] = true
	}
	var orphans []string
	for _, f := range c.All() {
		for _, r := range f.Rules {
			for _, node := range r.Applies {
				if !anchored[r.ID+"\x00"+node] {
					orphans = append(orphans, fmt.Sprintf("g orphan %s %s", r.ID, node))
				}
			}
		}
	}
	sort.Strings(orphans)
	lines = append(lines, orphans...)

	// DRF-003 conditional refresh: Pending alone is an incomplete cure. A
	// resident -http server's graph is only rebuilt at startup/reroot
	// (SPX-MCP-003 refreshes .spectackle files only), so an out-of-band
	// code edit leaves anchors classifying Pending forever once staleFile
	// is wired — a wrong "evolved" answer traded for no answer at all,
	// which makes drift detection inert exactly under the mode this task
	// exists to make trustworthy. So: if any anchor's file changed since
	// the graph was last built, reindex ONCE for this call before
	// classifying — conditional on real staleness, not per-call. This does
	// NOT reintroduce the unconditional per-call reindexing P-0077
	// explicitly rejected ("Indexing this repository costs a full file
	// walk; paying it per tool call would undo the reason the resident
	// service exists"): the walk only runs when anchorsNeedRefresh finds a
	// genuinely stale file, so an unchanged workspace pays nothing extra.
	// staleFile stays wired into Classify below regardless — if reindex
	// itself fails, it logs and keeps the previous graph, s.indexedAt is
	// untouched, staleFile still reports stale, and Classify still falls
	// back to Pending: a refusal to judge, never a false heal.
	if s.anchorsNeedRefresh(anchors) {
		s.reindex()
	}

	results := drift.Classify(s.ws, s.g, anchors, func(id string) (string, bool) {
		r, ok := c.Rule(id)
		return r.Text, ok
	}, s.staleFile)
	changed := false
	pending := 0
	healed, audited := 0, 0
	ruleSeen := map[string]bool{}
	var ruleLines []string
	// remember dedupes the trailing `r <id> ...` block: at most one line per
	// distinct rule that appeared in a healed or audited `d` record, in the
	// same grammar the get/find paths use (ruleLine).
	remember := func(id string) {
		if ruleSeen[id] {
			return
		}
		ruleSeen[id] = true
		rule, ok := c.Rule(id)
		if !ok {
			return
		}
		dir := ruleCtx(rule.File)
		if dir == "" {
			dir = "."
		}
		ruleLines = append(ruleLines, ruleLine(spec.ResolvedRule{Rule: rule, ScopeDir: dir}))
	}
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
		case drift.Evolved:
			// code changed, rule sentence identical — the only mechanically
			// healable case: re-stamp the anchor's code hash and move on, no
			// in.Fix gate needed (this never touches the spec, it just
			// catches the anchor's hash up to reality).
			old := r.Anchor.CHash
			a := r.Anchor
			a.CHash = r.NewHash
			anchors = drift.Upsert(anchors, a)
			changed = true
			healed++
			lines = append(lines, fmt.Sprintf("d healed %s %s %s:%d-%d was=%s now=%s",
				r.Anchor.Rule, r.Anchor.Node, r.Anchor.File, r.Anchor.Start, r.Anchor.End,
				short8(old), short8(r.NewHash)))
			remember(r.Anchor.Rule)
			rule, _ := c.Rule(r.Anchor.Rule)
			ctx := ruleCtx(rule.File)
			_ = journal.Append(s.ws, ctx, journal.Event{
				Ev: journal.EvDrift, Rule: r.Anchor.Rule, Node: string(r.Anchor.Node),
				Cls: "healed", Oh: old, Nh: r.NewHash, Dir: ctx,
			})
		case drift.Tightened, drift.Diverged:
			// rule sentence changed (with or without the code) — never
			// auto-healed, a human has to look. Only draft a backprop
			// proposal when explicitly asked via in.Fix.
			audited++
			lines = append(lines, fmt.Sprintf("d audit %s %s %s:%d-%d %s",
				r.Anchor.Rule, r.Anchor.Node, r.Anchor.File, r.Anchor.Start, r.Anchor.End, r.Class))
			remember(r.Anchor.Rule)
			if in.Fix {
				if _, err := s.backprop(c, r); err != nil {
					return nil, nil, err
				}
			}
		default: // Gone, Stale
			d := fmt.Sprintf("d %s %s %s %s:%d-%d", r.Class, r.Anchor.Rule, r.Anchor.Node, r.Anchor.File, r.Anchor.Start, r.Anchor.End)
			if in.Fix && r.Class == drift.Gone {
				bp, err := s.backprop(c, r)
				if err != nil {
					return nil, nil, err
				}
				d += " item=" + bp
			}
			lines = append(lines, d)
		}
	}
	lines = append(lines, ruleLines...)
	if changed {
		if err := drift.Save(s.ws, anchors); err != nil {
			return nil, nil, err
		}
	}
	if pending > 0 {
		lines = append(lines, fmt.Sprintf("ok %d anchors pending (nodes not in the graph yet)", pending))
	}

	// compact-due signals
	lines = append(lines, s.compactCandidates(in.Path)...)

	if healed > 0 || audited > 0 {
		lines = append(lines, fmt.Sprintf("ok healed=%d audit=%d", healed, audited))
	}

	if len(lines) == 0 {
		return text("ok")
	}
	kept, cur := budget.TruncateRecords(lines, budget.Resume(in.Cur), in.Budget)
	return text(budget.Render(kept, cur))
}

// short8 truncates a hex hash to its first 8 characters for compact display
// in `d healed` records; hashes shorter than that pass through unchanged.
func short8(h string) string {
	if len(h) > 8 {
		return h[:8]
	}
	return h
}

func (s *Server) coverageGaps(c *spec.Cascade, sub string) []string {
	root := filepath.Join(s.ws.Dir, filepath.FromSlash(sub))
	uncovered := map[string]bool{}
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(s.ws.Dir, p)
		rel = filepath.ToSlash(rel)
		if rel == "." {
			rel = ""
		}
		if d.IsDir() {
			if s.ws.SkipDir(rel, d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if index.LangOf(p) == "" {
			return nil
		}
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
	it, err := lifecycle.Draft(s.ws, s.minter(), "proposal", fmt.Sprintf("backprop %s %s", r.Anchor.Rule, r.Class), body, ctx, "", []string{string(r.Anchor.Node)})
	if err != nil {
		return "", err
	}
	_ = journal.Append(s.ws, ctx, journal.Event{
		Ev: journal.EvDrift, Rule: r.Anchor.Rule, Node: string(r.Anchor.Node),
		Cls: string(r.Class), Oh: r.Anchor.CHash, Nh: r.NewHash, Item: it.ID, Dir: ctx,
	})
	_ = s.cd.Emit("drift", r.Anchor.Rule, string(r.Class)+" at "+string(r.Anchor.Node)+" backprop="+it.ID)
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

// mergeCandidates surfaces near-duplicate rule pairs as merge suggestions
// (MCP-005): same spec file, same EARS pattern, and either sentence-token
// Jaccard >= 0.6 or identical non-empty applies sets. Suggestion only —
// merging changes contract semantics and stays a rule op=edit+retire.
func (s *Server) mergeCandidates(sub string) []string {
	c, err := spec.Load(s.ws.Dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, f := range c.All() {
		if !within(sub, f.Dir) {
			continue
		}
		for i := 0; i < len(f.Rules); i++ {
			for k := i + 1; k < len(f.Rules); k++ {
				a, b := f.Rules[i], f.Rules[k]
				if a.Pattern != b.Pattern {
					continue
				}
				j := jaccard(ruleTokens(a.Text), ruleTokens(b.Text))
				if j >= 0.6 || sameApplies(a.Applies, b.Applies) {
					out = append(out, fmt.Sprintf("c %s mergeable %s+%s j=%.2f", orDot(f.Dir), a.ID, b.ID, j))
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

// ruleTokens normalizes an EARS sentence for similarity: lowercase, alnum
// runs only, EARS scaffolding and glue words dropped.
func ruleTokens(text string) map[string]bool {
	stop := map[string]bool{
		"when": true, "while": true, "if": true, "then": true, "where": true,
		"shall": true, "the": true, "a": true, "an": true, "to": true,
		"of": true, "and": true, "or": true, "as": true, "is": true,
	}
	toks := map[string]bool{}
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			if t := cur.String(); !stop[t] {
				toks[t] = true
			}
			cur.Reset()
		}
	}
	for _, r := range strings.ToLower(text) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			cur.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	return toks
}

func jaccard(a, b map[string]bool) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	inter := 0
	for t := range a {
		if b[t] {
			inter++
		}
	}
	return float64(inter) / float64(len(a)+len(b)-inter)
}

func sameApplies(a, b []string) bool {
	if len(a) == 0 || len(a) != len(b) {
		return false
	}
	set := make(map[string]bool, len(a))
	for _, n := range a {
		set[n] = true
	}
	for _, n := range b {
		if !set[n] {
			return false
		}
	}
	return true
}

func (s *Server) compact(in compactIn) (*mcp.CallToolResult, any, error) {
	if s.wtItem != "" {
		return text("! WT E compact is blocked inside a worktree — journal folds would corrupt the submit replay")
	}
	defer s.scan.MarkDirty()
	var b strings.Builder
	cands := s.compactCandidates(in.Path)
	cands = append(cands, s.mergeCandidates(in.Path)...)
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

	// archive done items (skipping ones with open children) — same audit
	// gate as the move tool; the cascade is loaded once for the whole batch.
	gateOpts := s.auditGateOpts()
	for _, it := range doneItems {
		if _, err := lifecycle.Move(s.ws, it.ID, item.StateArchived, "compact", gateOpts...); err != nil {
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
			case journal.EvReject, journal.EvArchive, journal.EvCompact,
				journal.EvEscalate, journal.EvDecide:
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
	var l string
	if n.EndLine > n.Line {
		l = fmt.Sprintf("n %s %s %s:%d-%d", n.ID, n.Kind, n.File, n.Line, n.EndLine)
	} else {
		l = fmt.Sprintf("n %s %s %s:%d", n.ID, n.Kind, n.File, n.Line)
	}
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
