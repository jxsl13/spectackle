package mcpserver

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jxsl13/spectackle/internal/budget"
	"github.com/jxsl13/spectackle/internal/drift"
	"github.com/jxsl13/spectackle/internal/graph"
	"github.com/jxsl13/spectackle/internal/item"
	"github.com/jxsl13/spectackle/internal/journal"
	"github.com/jxsl13/spectackle/internal/spec"
)

// grill is the critique pack: close an item's gaps before promoting it (the
// ORCHESTRATION paragraph in server.go's instructions: "Before move
// to=approved on a proposal: grill id=<P-id> and close its gaps — a clean
// grill stamps grilled: <date>, the evidence move checks for"). Unlike
// research and state, grill is NOT read-only: after rendering the pack it
// performs exactly one write — it stamps it.Grilled with today's date
// (item.Upsert), journals an EvGrill event, and emits a swarm learning — so
// siblings and a later move op=approved can see the item was just reviewed.

type grillIn struct {
	ID     string `json:"id" jsonschema:"item ID, e.g. P-0007"`
	Budget int    `json:"budget,omitempty" jsonschema:"token budget, default 1500"`
	Cur    string `json:"cur,omitempty" jsonschema:"resume cursor"`
}

func (s *Server) grill(in grillIn) (*mcp.CallToolResult, any, error) {
	if in.Budget <= 0 {
		in.Budget = 1500
	}
	it, ok, err := item.Get(s.ws, in.ID)
	if err != nil {
		return nil, nil, err
	}
	if !ok {
		return s.nearest(in.ID)
	}

	c, err := spec.Load(s.ws.Dir)
	if err != nil {
		return nil, nil, err
	}
	anchors, err := drift.Load(s.ws)
	if err != nil {
		return nil, nil, err
	}
	anchored := map[string]bool{}
	for _, a := range anchors {
		anchored[string(a.Node)] = true
	}
	all, err := item.LoadAll(s.ws)
	if err != nil {
		return nil, nil, err
	}

	var lines []string
	lines = append(lines, item.Record(it))

	addSection := func(name string, recs []string) {
		if len(recs) == 0 {
			return
		}
		lines = append(lines, name)
		lines = append(lines, recs...)
	}

	addSection("#targets", grillTargets(s.g, it.Targets, anchored))
	addSection("#contracts", grillContracts(c, it.Targets))
	addSection("#briefs", grillBriefs(all, it.ID))
	addSection("#tests", s.grillTests(it.Targets))

	rej, err := s.grillRejections(it)
	if err != nil {
		return nil, nil, err
	}
	addSection("#rejections", rej)
	addSection("#questions", grillQuestions(it))

	// evidence — the ONE write this tool performs: stamp the item as
	// grilled today, journal it, and let siblings piggyback the news.
	it.Grilled = time.Now().UTC().Format("2006-01-02")
	if err := item.Upsert(s.ws, it); err != nil {
		return nil, nil, err
	}
	if err := journal.Append(s.ws, it.Dir, journal.Event{
		Ev: journal.EvGrill, ID: it.ID, Dir: it.Dir, Gr: it.Grilled,
	}); err != nil {
		return nil, nil, err
	}
	_ = s.cd.Emit("grill", it.ID, "grilled "+it.Grilled)
	s.scan.MarkDirty()

	lines = append(lines, fmt.Sprintf("ok grilled %s %s", it.ID, it.Grilled))

	kept, cur := budget.TruncateRecords(lines, budget.Resume(in.Cur), in.Budget)
	return text(budget.Render(kept, cur))
}

// grillTargets checks node-ID-shaped targets against the graph and the
// anchor set: missing from the graph entirely -> nf, present but never bound
// by any rule's applies list -> g unanchored. Path-shaped targets are the
// #contracts section's concern instead (paths aren't graph nodes).
func grillTargets(g graph.Graph, targets []string, anchored map[string]bool) []string {
	var out []string
	for _, t := range targets {
		if strings.ContainsAny(t, "/") || !strings.Contains(t, ":") {
			continue
		}
		if _, ok := g.Node(graph.NodeID(t)); !ok {
			out = append(out, "nf "+t)
			continue
		}
		if !anchored[t] {
			out = append(out, "g unanchored "+t)
		}
	}
	return out
}

// grillContracts flags path-shaped targets with zero resolved EARS rules —
// code the item touches with no contract governing it at all.
func grillContracts(c *spec.Cascade, targets []string) []string {
	var out []string
	for _, t := range targets {
		p, ok := targetPath(t)
		if !ok {
			continue
		}
		if len(c.ForPath(p)) == 0 {
			out = append(out, "g nocontract "+t)
		}
	}
	return out
}

// grillBriefs applies the task-brief quality heuristics from the
// ORCHESTRATION paragraph ("Task bodies must be exhaustive") to every child
// task, one b-line per failing heuristic — a task can fail more than one.
func grillBriefs(all []item.Item, parentID string) []string {
	var out []string
	for _, ch := range all {
		if ch.Parent != parentID || ch.Kind != "task" {
			continue
		}
		for _, h := range briefHeuristics(ch.Body) {
			out = append(out, "b "+ch.ID+" "+h)
		}
	}
	sort.Strings(out)
	return out
}

// briefHeuristics is the cheap, mechanical half of "is this task brief
// exhaustive enough for a fresh minimal-context implementer": long enough to
// plausibly carry files/APIs/constraints, names at least one path, and names
// a verification command the implementer can run before calling it done.
func briefHeuristics(body string) []string {
	var fails []string
	if len(body) < 300 {
		fails = append(fails, "short-body")
	}
	if !strings.Contains(body, "/") {
		fails = append(fails, "no-path")
	}
	if !strings.Contains(body, "go test") && !strings.Contains(body, "make") {
		fails = append(fails, "no-verify")
	}
	return fails
}

// grillTests flags target packages under internal/ or cmd/ that have no
// *_test.go file at all — a coarse, package-level test-coverage smell check
// (not a coverage percentage), scoped to the item's own targets only.
func (s *Server) grillTests(targets []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, t := range targets {
		p, ok := targetPath(t)
		if !ok {
			continue
		}
		if !strings.HasPrefix(p, "internal/") && !strings.HasPrefix(p, "cmd/") {
			continue
		}
		dir := p
		if strings.Contains(filepath.Base(p), ".") {
			dir = filepath.Dir(p)
		}
		dir = filepath.ToSlash(dir)
		if seen[dir] {
			continue
		}
		seen[dir] = true
		matches, _ := filepath.Glob(filepath.Join(s.ws.Dir, filepath.FromSlash(dir), "*_test.go"))
		if len(matches) == 0 {
			out = append(out, "g notest "+dir)
		}
	}
	sort.Strings(out)
	return out
}

// grillRejections looks for similar past failures via the item's own
// title+body — the same signal draft's #rejections section searches for
// before the item existed.
func (s *Server) grillRejections(it item.Item) ([]string, error) {
	q := it.Title
	if it.Body != "" {
		q += " " + it.Body
	}
	docs, err := s.cache.Search(q, []string{"rejection"}, packHits)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, d := range docs {
		out = append(out, fmt.Sprintf("j %s %s :: %s", d.ID, d.Title, d.Body))
	}
	return out, nil
}

// grillQuestions is the standing review checklist: scope disjointness,
// rollback and an exit criterion should each be addressed somewhere in the
// item's own body; whichever the body never mentions comes back as an open
// question (omit-if-present, not a fixed always-on list). Proposals get one
// more: whether the item shows any sign of having weighed an alternative at
// all (see recordsDeliberation).
func grillQuestions(it item.Item) []string {
	body := strings.ToLower(it.Body)
	var out []string
	if !strings.Contains(body, "scope") && !strings.Contains(body, "disjoint") {
		out = append(out, "q scope disjointness not addressed")
	}
	if !strings.Contains(body, "rollback") && !strings.Contains(body, "revert") {
		out = append(out, "q rollback not addressed")
	}
	if !strings.Contains(body, "exit criterion") && !strings.Contains(body, "done when") && !strings.Contains(body, "verif") {
		out = append(out, "q exit criterion not addressed")
	}
	if it.Kind == "proposal" && !recordsDeliberation(it, body) {
		out = append(out, "q deliberation not addressed")
	}
	return out
}

// recordsDeliberation is a heuristic, not a gate (grill only asks — move
// to=approved is never blocked on this): it reports whether a proposal
// shows ANY sign of having weighed more than one approach, either
// structurally (a ref citing an adr or research item — the ID letters
// minted for those kinds, see item.go's kindLetter, plus the legacy D-
// adr letter) or in prose (a body line using this repo's own vocabulary for
// naming a rejected/considered alternative, e.g. "Synthesis of the two
// rejected extremes..." in this repo's own ADR records). Both checks are
// deliberately loose substring matches: a false positive here costs one
// dismissed question, a false negative costs nothing that was not already
// lost by the item never having recorded its reasoning.
func recordsDeliberation(it item.Item, lowerBody string) bool {
	for _, r := range it.Refs {
		if strings.HasPrefix(r, "ADR-") || strings.HasPrefix(r, "R-") || strings.HasPrefix(r, "D-") {
			return true
		}
	}
	return strings.Contains(lowerBody, "alternative") &&
		(strings.Contains(lowerBody, "reject") || strings.Contains(lowerBody, "consider"))
}
