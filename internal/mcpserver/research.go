package mcpserver

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jxsl13/spectacle/internal/budget"
	"github.com/jxsl13/spectacle/internal/drift"
	"github.com/jxsl13/spectacle/internal/graph"
	"github.com/jxsl13/spectacle/internal/spec"
	"github.com/jxsl13/spectacle/internal/workspace"
)

// research is the second strictly read-only tool in the surface (state is
// the first): a condensed problem-space pack for a question and/or a set of
// targets, so an orchestrator can front-load research before minting a
// proposal instead of ad hoc exploration in its own context (see the
// ORCHESTRATION paragraph in server.go's instructions). Writes nothing — no
// draft, no journal.Append, no cache write, no anchor mutation.

type researchIn struct {
	Q       string   `json:"q" jsonschema:"topic or question"`
	Targets []string `json:"targets,omitempty" jsonschema:"node IDs or paths the question touches"`
	Depth   int      `json:"depth,omitempty" jsonschema:"impact BFS depth, default 2"`
	Budget  int      `json:"budget,omitempty" jsonschema:"token budget, default 2500"`
}

// packHits is the top-K used for every graph.Find / cache.Search call inside
// research and grill's condensed packs — small enough to stay dense, plenty
// for a "have we seen this before" signal.
const packHits = 5

func (s *Server) research(in researchIn) (*mcp.CallToolResult, any, error) {
	if in.Depth <= 0 {
		in.Depth = 2
	}
	if in.Budget <= 0 {
		in.Budget = 2500
	}
	txt, err := s.researchText(in)
	if err != nil {
		return nil, nil, err
	}
	var lines []string
	for _, l := range strings.Split(strings.TrimRight(txt, "\n"), "\n") {
		if l != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) == 0 {
		return text("ok research empty — provide q and/or targets")
	}
	kept, cur := budget.TruncateRecords(lines, 0, in.Budget)
	return text(budget.Render(kept, cur))
}

// researchText builds the condensed problem-space pack. Sections are omitted
// entirely when empty (SPX-MCP output-diet spirit, mirrored from state.go /
// draft's context pack) — no "ok none" filler lines. Strictly read-only:
// every helper below is Load/Find/Search/WalkDir only.
func (s *Server) researchText(in researchIn) (string, error) {
	targets := normalizeTargets(in.Targets)
	c, err := spec.Load(s.ws.Dir)
	if err != nil {
		return "", err
	}
	anchors, err := drift.Load(s.ws)
	if err != nil {
		return "", err
	}

	var b strings.Builder

	if sec := s.researchImpact(in.Q, targets, in.Depth); sec != "" {
		b.WriteString("#impact\n")
		b.WriteString(sec)
	}

	if sec := s.researchContracts(c, targets); sec != "" {
		b.WriteString("#contracts\n")
		b.WriteString(sec)
	}

	rejSec, hasRej, err := s.researchDocsKind(in.Q, "rejection", func(d string, t string, body string) string {
		return fmt.Sprintf("j %s %s :: %s", d, t, body)
	})
	if err != nil {
		return "", err
	}
	if rejSec != "" {
		b.WriteString("#rejections\n")
		b.WriteString(rejSec)
	}

	histSec, _, err := s.researchDocsKind(in.Q, "journal", func(d string, t string, body string) string {
		return fmt.Sprintf("j %s %s :: %s", d, t, body)
	})
	if err != nil {
		return "", err
	}
	if histSec != "" {
		b.WriteString("#history\n")
		b.WriteString(histSec)
	}

	docsSec, _, err := s.researchDocsKind(in.Q, "section", func(d string, _ string, body string) string {
		return fmt.Sprintf("s %s %s", d, body)
	})
	if err != nil {
		return "", err
	}
	if docsSec != "" {
		b.WriteString("#docs\n")
		b.WriteString(docsSec)
	}

	if sec := s.researchGaps(c, anchors, targets); sec != "" {
		b.WriteString("#gaps\n")
		b.WriteString(sec)
	}

	if sec := researchOpen(c, anchors, targets, in.Q, hasRej); sec != "" {
		b.WriteString("#open\n")
		b.WriteString(sec)
	}

	return b.String(), nil
}

// researchImpact seeds a bounded BFS from any node-ID-shaped targets plus the
// top graph.Find hits for the question text, then renders the reachable
// subgraph as n/e lines — the same shape as draft's #impact and get's
// depth>0 node lookup.
func (s *Server) researchImpact(q string, targets []string, depth int) string {
	seen := map[graph.NodeID]bool{}
	var seeds []graph.NodeID
	for _, t := range targets {
		if !strings.ContainsAny(t, "/") && strings.Contains(t, ":") {
			id := graph.NodeID(t)
			if !seen[id] {
				seen[id] = true
				seeds = append(seeds, id)
			}
		}
	}
	if q != "" {
		for _, n := range s.g.Find(q, packHits, graph.KUnknown) {
			if !seen[n.ID] {
				seen[n.ID] = true
				seeds = append(seeds, n.ID)
			}
		}
	}
	if len(seeds) == 0 {
		return ""
	}
	nodes, edges := s.g.Impact(seeds, depth, graph.Both, nil)
	var b strings.Builder
	for _, n := range nodes {
		b.WriteString(nodeLine(n) + "\n")
	}
	for _, e := range edges {
		fmt.Fprintf(&b, "e %s %s %s via=%s:%d\n", e.Src, e.Kind, e.Dst, e.File, e.Line)
	}
	return b.String()
}

// researchContracts resolves the EARS contracts bound to the target paths
// (reusing tools.go's splitContractRules, same r-root collapsing as draft's
// #contracts), falling back to a directory-level probe of the nearest
// context dir when none of the targets resolve any rule directly — the
// "target paths + item dir" fallback draft's context pack uses via its freshly
// drafted item's Dir; research has no item, so the dir is derived straight
// from the targets via workspace.NearestContext instead.
func (s *Server) researchContracts(c *spec.Cascade, targets []string) string {
	if len(targets) == 0 {
		return ""
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
		dir := s.scopeDirFor(targets)
		probe := dir + "/_"
		if dir == "" {
			probe = "_"
		}
		splitContractRules(c.ForPath(probe), nil, rootSeen, &rl, &rootIDs)
	}
	if len(rl) == 0 && len(rootIDs) == 0 {
		return ""
	}
	var b strings.Builder
	if len(rootIDs) > 0 {
		b.WriteString("r-root " + strings.Join(rootIDs, " ") + "\n")
	}
	for _, l := range rl {
		b.WriteString(l + "\n")
	}
	return b.String()
}

// scopeDirFor finds the nearest existing context dir for the first path-like
// target — the research-side analogue of lifecycle.scopeFor (unexported,
// draft-only), used only as the #contracts fallback probe's scope.
func (s *Server) scopeDirFor(targets []string) string {
	ctxs, err := s.ws.ContextDirs()
	if err != nil {
		return ""
	}
	for _, t := range targets {
		p, ok := targetPath(t)
		if !ok {
			continue
		}
		dir := p
		if strings.Contains(filepath.Base(p), ".") {
			dir = filepath.Dir(p)
		}
		if d := workspace.NearestContext(ctxs, filepath.ToSlash(dir)); d != "" {
			return d
		}
	}
	return ""
}

// researchDocsKind wraps one cache.Search call for a single doc kind,
// rendering each hit with render. Returns ("", false, nil) when q is empty
// (an unscoped FTS query would be meaningless) or nothing matched.
func (s *Server) researchDocsKind(q, kind string, render func(id, title, body string) string) (string, bool, error) {
	if q == "" {
		return "", false, nil
	}
	docs, err := s.cache.Search(q, []string{kind}, packHits)
	if err != nil {
		return "", false, err
	}
	if len(docs) == 0 {
		return "", false, nil
	}
	var b strings.Builder
	for _, d := range docs {
		b.WriteString(render(d.ID, d.Title, d.Body) + "\n")
	}
	return b.String(), true, nil
}

// researchGaps flags targets missing from the graph (g unknown), node
// targets present but never bound by a rule (g unanchored — via drift.Load),
// and reuses check()'s coverage-gap walk (s.coverageGaps) scoped to each
// path target's directory.
func (s *Server) researchGaps(c *spec.Cascade, anchors []drift.Anchor, targets []string) string {
	if len(targets) == 0 {
		return ""
	}
	anchored := map[string]bool{}
	for _, a := range anchors {
		anchored[string(a.Node)] = true
	}
	var lines []string
	dirsDone := map[string]bool{}
	for _, t := range targets {
		if !strings.ContainsAny(t, "/") && strings.Contains(t, ":") {
			if _, ok := s.g.Node(graph.NodeID(t)); !ok {
				lines = append(lines, "g unknown "+t)
				continue
			}
			if !anchored[t] {
				lines = append(lines, "g unanchored "+t)
			}
			continue
		}
		p, ok := targetPath(t)
		if !ok {
			continue
		}
		dir := p
		if strings.Contains(filepath.Base(p), ".") {
			dir = filepath.Dir(p)
		}
		dir = filepath.ToSlash(dir)
		if dirsDone[dir] {
			continue
		}
		dirsDone[dir] = true
		lines = append(lines, s.coverageGaps(c, dir)...)
	}
	if len(lines) == 0 {
		return ""
	}
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(l + "\n")
	}
	return b.String()
}

// researchOpen generates the checklist of open questions: one per target
// lacking any binding rule (path targets via c.ForPath, node targets via
// anchors), plus a single generic risk flag when a non-empty question
// produced zero similar rejections (nothing learned from past failures).
func researchOpen(c *spec.Cascade, anchors []drift.Anchor, targets []string, q string, hasRej bool) string {
	anchored := map[string]bool{}
	for _, a := range anchors {
		anchored[string(a.Node)] = true
	}
	var lines []string
	for _, t := range targets {
		hasRule := false
		if p, ok := targetPath(t); ok {
			hasRule = len(c.ForPath(p)) > 0
		} else if !strings.ContainsAny(t, "/") && strings.Contains(t, ":") {
			hasRule = anchored[t]
		} else {
			continue
		}
		if !hasRule {
			lines = append(lines, "q "+t+" has no binding rule")
		}
	}
	if q != "" && !hasRej {
		lines = append(lines, "q no similar rejection found - risk unknown")
	}
	if len(lines) == 0 {
		return ""
	}
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(l + "\n")
	}
	return b.String()
}
