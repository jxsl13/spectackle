package mcpserver

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jxsl13/spectackle/internal/budget"
	"github.com/jxsl13/spectackle/internal/drift"
	"github.com/jxsl13/spectackle/internal/item"
	"github.com/jxsl13/spectackle/internal/spec"
)

// state is the ONE strictly read-only tool in the surface: a sectioned
// overview of the whole spec-driven-development picture (items, rules,
// graph, swarm, drift, health) in a single call, so an orchestrator or a
// human can orient without walking find/get/check/swarm separately. It
// writes nothing — no drift.Save, no backprop drafts, no journal entries.

type stateIn struct {
	Path   string `json:"path,omitempty" jsonschema:"subtree, default all"`
	Budget int    `json:"budget,omitempty" jsonschema:"token budget, default 2000"`
	Cur    string `json:"cur,omitempty" jsonschema:"resume cursor"`
}

func (s *Server) state(in stateIn) (*mcp.CallToolResult, any, error) {
	if in.Budget <= 0 {
		in.Budget = 2000
	}
	txt, err := s.stateText(in.Path)
	if err != nil {
		return nil, nil, err
	}
	var lines []string
	for _, l := range strings.Split(strings.TrimRight(txt, "\n"), "\n") {
		if l != "" {
			lines = append(lines, l)
		}
	}
	kept, cur := budget.TruncateRecords(lines, budget.Resume(in.Cur), in.Budget)
	return text(budget.Render(kept, cur))
}

// stateText builds the full sectioned snapshot for a subtree. It is shared
// by the `state` tool handler (via gate, which already refreshed the scan
// and holds s.mu) and the `state` prompt handler (which locks s.mu and
// refreshes the scan itself, exactly like promptWorkflow/promptNext).
// Strictly read-only: every section below is built from Load/query calls
// only — no Save, no drafting, no journal writes, no anchor mutation.
func (s *Server) stateText(path string) (string, error) {
	var b strings.Builder

	rootLabel := "main"
	if s.wtItem != "" {
		rootLabel = "wt:" + s.wtItem
	}
	b.WriteString("#version\n")
	fmt.Fprintf(&b, "ok spectackle %s agent %s root %s\n", Version, s.agent, rootLabel)

	c, err := spec.Load(s.ws.Dir)
	if err != nil {
		return "", err
	}

	if sec, err := s.stateItemsSection(path); err != nil {
		return "", err
	} else if sec != "" {
		b.WriteString("#items\n")
		b.WriteString(sec)
	}

	if sec := stateRulesSection(c, path); sec != "" {
		b.WriteString("#rules\n")
		b.WriteString(sec)
	}

	if sec := s.stateGraphSection(); sec != "" {
		b.WriteString("#graph\n")
		b.WriteString(sec)
	}

	if sec, err := s.stateSwarmSection(); err != nil {
		return "", err
	} else if sec != "" {
		b.WriteString("#swarm\n")
		b.WriteString(sec)
	}

	if sec, err := s.stateDriftSection(c); err != nil {
		return "", err
	} else if sec != "" {
		b.WriteString("#drift\n")
		b.WriteString(sec)
	}

	if sec := s.stateHealthSection(c, path); sec != "" {
		b.WriteString("#health\n")
		b.WriteString(sec)
	}

	return b.String(), nil
}

// stateItemsSection: totals by state + i lines, scoped to path, non-draft
// items surfaced first (same ordering as promptWorkflow's ACTIVE ITEMS).
func (s *Server) stateItemsSection(path string) (string, error) {
	items, err := item.LoadAll(s.ws)
	if err != nil {
		return "", err
	}
	var scoped []item.Item
	for _, it := range items {
		if within(path, it.Dir) {
			scoped = append(scoped, it)
		}
	}
	if len(scoped) == 0 {
		return "", nil
	}
	counts := map[string]int{}
	for _, it := range scoped {
		counts[it.State]++
	}
	sort.SliceStable(scoped, func(i, j int) bool {
		di := scoped[i].State == item.StateDraft
		dj := scoped[j].State == item.StateDraft
		return !di && dj
	})
	var b strings.Builder
	fmt.Fprintf(&b, "ok items total=%d active=%d approved=%d draft=%d submitted=%d done=%d\n",
		len(scoped), counts[item.StateActive], counts[item.StateApproved],
		counts[item.StateDraft], counts[item.StateSubmitted], counts[item.StateDone])
	sc, err := s.idScope()
	if err != nil {
		return "", err
	}
	for _, it := range scoped {
		b.WriteString(sc.record(it) + "\n")
	}
	return b.String(), nil
}

// stateRulesSection: per-dir rule counts + a global findings count, scoped
// to path. Findings themselves are not path-filtered, mirroring check()'s
// spec-lint pass (SPX-MCP consistency: same lint surface everywhere).
func stateRulesSection(c *spec.Cascade, path string) string {
	var files []spec.SpecFile
	for _, sf := range c.All() {
		if within(path, sf.Dir) {
			files = append(files, sf)
		}
	}
	findings := c.Findings()
	if len(files) == 0 && len(findings) == 0 {
		return ""
	}
	total := 0
	for _, sf := range files {
		total += len(sf.Rules)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "ok rules total=%d dirs=%d findings=%d\n", total, len(files), len(findings))
	for _, sf := range files {
		fmt.Fprintf(&b, "ok dir %s rules=%d\n", orDot(sf.Dir), len(sf.Rules))
	}
	for _, f := range findings {
		b.WriteString(f.String() + "\n")
	}
	return b.String()
}

// stateGraphSection: whole-graph size, not path-filtered (the graph has no
// per-node "within path" notion cheaper than a full scan; nodes=0 omits the
// section entirely rather than printing a vacuous zero record).
func (s *Server) stateGraphSection() string {
	nodes, edges := s.g.Stats()
	if nodes == 0 {
		return ""
	}
	return fmt.Sprintf("ok graph nodes=%d edges=%d\n", nodes, edges)
}

// stateSwarmSection mirrors the swarm tool's ag/l/wt lines (no sw learnings
// here — those are the swarm tool's realtime piggyback job, not a snapshot
// concern).
func (s *Server) stateSwarmSection() (string, error) {
	var b strings.Builder
	agents, err := s.cd.Agents()
	if err != nil {
		return "", err
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
		return "", err
	}
	for _, l := range leases {
		fmt.Fprintf(&b, "l %s %s %s %ds\n", l.Path, l.Agent, orDash(l.Item), int(time.Until(l.Exp).Seconds()))
	}
	wts, err := s.cd.Worktrees()
	if err != nil {
		return "", err
	}
	for _, w := range wts {
		fmt.Fprintf(&b, "wt %s %s %s %s\n", w.Item, w.State, w.Agent, w.Root)
	}
	return b.String(), nil
}

// stateDriftSection classifies every anchor exactly like check() does, but
// performs NONE of check()'s write side effects: no drift.Save (Moved
// anchors are just counted, never re-stamped), no backprop drafts, no
// journal.Append, no cd.Emit. A pure read.
func (s *Server) stateDriftSection(c *spec.Cascade) (string, error) {
	anchors, err := drift.Load(s.ws)
	if err != nil {
		return "", err
	}
	if len(anchors) == 0 {
		return "", nil
	}
	// nil staleness predicate: state's drift section is a pure read with no
	// write side effects (see doc comment above) — it mirrors check()'s
	// classification but intentionally skips the staleness-aware healing
	// behavior check() gets, so this stays nil rather than wiring s.indexedAt.
	results := drift.Classify(s.ws, s.g, anchors, func(id string) (string, bool) {
		r, ok := c.Rule(id)
		return r.Text, ok
	}, nil)
	ok, pending, moved := 0, 0, 0
	var d []string
	for _, r := range results {
		switch r.Class {
		case drift.OK:
			ok++
		case drift.Pending:
			pending++
		case drift.Moved:
			moved++
		default: // evolved, tightened, diverged, gone, stale — check() is where these heal/audit
			d = append(d, fmt.Sprintf("d %s %s %s %s:%d-%d",
				r.Class, r.Anchor.Rule, r.Anchor.Node, r.Anchor.File, r.Anchor.Start, r.Anchor.End))
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "ok anchors total=%d ok=%d pending=%d moved=%d\n", len(results), ok, pending, moved)
	for _, l := range d {
		b.WriteString(l + "\n")
	}
	return b.String(), nil
}

// stateHealthSection: compact-due candidates plus a count-only coverage-gap
// summary (the full `g` lines belong to check(); here we just want a
// number, not the per-dir listing).
func (s *Server) stateHealthSection(c *spec.Cascade, path string) string {
	cands := s.compactCandidates(path)
	gaps := len(s.coverageGaps(c, path))
	if len(cands) == 0 && gaps == 0 {
		return ""
	}
	var b strings.Builder
	for _, cnd := range cands {
		b.WriteString(cnd + "\n")
	}
	fmt.Fprintf(&b, "ok coverage gaps=%d\n", gaps)
	return b.String()
}
