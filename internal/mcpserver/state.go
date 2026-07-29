package mcpserver

import (
	"fmt"
	"sort"
	"strings"

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

// TypedPassState is the go/types call-edge upgrade pass's outcome on the
// last successful reindex — whether it ran, and if not, why and how many
// packages were affected (issue 28). BuildGraph (server.go) returns this
// instead of a bare edge count precisely so the failure survives past the
// log line reindex's stderr always got: a resident server driven over
// stdio/HTTP, or the `call` subcommand, never sees stderr, so `state`
// answering "ok graph nodes=N edges=M" with no mention of the degradation
// left an agent trusting a `get depth` impact radius that had silently lost
// every cross-package typed call edge. Both stateGraphSection (state.go) and
// check() (tools.go) read it via typedPassFinding below, so the record's
// wording and gate live in exactly one place.
type TypedPassState struct {
	Added int // edges added; meaningful only when Cause == ""

	// Cause is "" when the pass is healthy: it either completed over real
	// packages, or found no Go module at root to check at all
	// (index.ResolveTypedCalls treats that as nothing to do, not a
	// failure) — either way there is nothing to warn about. Cause is
	// non-empty exactly when the pass failed, and Cause == "" is
	// deliberately the ONLY signal typedPassFinding trusts (not a separate
	// "did it run" bool): the zero-value TypedPassState — before the very
	// first reindex completes, or after a reindex whose IndexAll itself
	// failed and left the previous state untouched — must read as healthy,
	// not as a phantom degradation with an empty cause.
	Cause string

	// Packages is the number of packages that individually failed to
	// load/type-check; 0 when Cause == "", or when the whole-module load
	// failed before any per-package breakdown existed.
	Packages int
}

// typedPassFinding renders s.typedPass as a `!` finding when the pass did
// not complete, or "" when it did. A healthy index must add NOTHING here —
// the output-diet contract (SPX-ARC-002, omit-if-empty) — so both `state`
// (stateGraphSection below) and `check` (tools.go) call this one function
// rather than composing their own wording, keeping the gate and the message
// identical on both surfaces.
//
// Shape: reuses the existing `!` finding grammar (docs/tools.md) rather than
// inventing a new record letter — TYPED joins LEASE/WT/GATE/LOCK/GRILL/NEEDS
// as a non-lint code under the same `! <code> <sev> <ref> <msg>` line. `h`
// (the harness-detection / stale-binary hint shape) was the other
// candidate, but `!` fits better: this is a structured, severity-carrying
// finding an agent should branch on (skip/avoid extra logging, temporary
// distrust of impact-radius answers), not a passive nudge, and it is a
// WARNING (W) — the graph still answers, just from syntactic edges only —
// not an error, since nothing here blocks the call.
func (s *Server) typedPassFinding() string {
	tp := s.typedPass
	if tp.Cause == "" {
		return ""
	}
	return fmt.Sprintf(
		"! TYPED W - typed-call pass disabled packages=%d: %s (graph has syntactic call edges only — get depth/impact answers under-report cross-package blast radius until this is fixed)",
		tp.Packages, tp.Cause,
	)
}

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
	fmt.Fprintf(&b, "ok spectackle %s agent %s root %s\n", ResolvedVersion(), s.agent, rootLabel)

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

	if sec := s.stateRulesSection(c, path); sec != "" {
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

	health := s.stateHealthSection(c, path)
	// The waiver-rate tripwire (T-01KYFXEP) rides #health: computed,
	// visible, never vetoing — and never in check (CI string-matches ok).
	if wr := s.waiverRate(); wr != "" {
		health += wr + "\n"
	}
	for _, o := range s.orphanedItems() {
		health += o + "\n"
	}
	if h := s.hookHint(); h != "" {
		health += h + "\n"
	}
	if health != "" {
		b.WriteString("#health\n")
		b.WriteString(health)
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
	// Only non-zero buckets, side states included (T-01KYE8): the fixed
	// six-bucket line spent bytes on zeros while blocked and rejected — the
	// most actionable states in the machine — were invisible in the one
	// line agents read first; a blocked item awaiting its decide could only
	// be found by scanning the listing.
	fmt.Fprintf(&b, "ok items total=%d", len(scoped))
	for _, st := range []string{
		item.StateDraft, item.StateSubmitted, item.StateApproved,
		item.StateActive, item.StateDone,
		item.StateBlocked, item.StateRejected,
	} {
		if counts[st] > 0 {
			fmt.Fprintf(&b, " %s=%d", st, counts[st])
		}
	}
	b.WriteString("\n")
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
func (s *Server) stateRulesSection(c *spec.Cascade, path string) string {
	var files []spec.SpecFile
	for _, sf := range c.All() {
		if within(path, sf.Dir) {
			files = append(files, sf)
		}
	}
	findings := c.Findings()
	// Coverage visibility must survive the zero-rules workspace — that is
	// the workspace MOST in need of the signal (T-01KYD87ZN).
	uncovered := s.uncoveredPackages(c, path)
	if len(files) == 0 && len(findings) == 0 && len(uncovered) == 0 {
		return ""
	}
	total := 0
	for _, sf := range files {
		total += len(sf.Rules)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "ok rules total=%d dirs=%d findings=%d\n", total, len(files), len(findings))
	// Per-dir lines only for dirs that carry FINDINGS — the exceptional case
	// an agent must act on. The healthy inventory used to be listed in full
	// (eighteen lines, about five hundred bytes on this repository, on EVERY
	// state call) and is exactly what `get <dir>` answers on demand; the
	// benchmark showed state to be the most expensive read on the surface,
	// and this inventory was its least actionable part. A/B-proven under
	// T-01KYDQ: fewer bytes at equal validity.
	dirty := map[string]bool{}
	for _, f := range findings {
		// a finding's File is <dir>/.spectackle/spec.md; the context dir is
		// what precedes the bundle path (root when nothing does).
		d := strings.TrimSuffix(f.File, "spec.md")
		d = strings.TrimSuffix(d, ".spectackle/")
		d = strings.TrimSuffix(d, "/")
		dirty[d] = true
	}
	for _, sf := range files {
		if dirty[sf.Dir] {
			fmt.Fprintf(&b, "ok dir %s rules=%d\n", orDot(sf.Dir), len(sf.Rules))
		}
	}
	// Default coverage visibility (T-01KYD87ZN): one line per uncovered
	// package dir — state is not string-matched by CI (only check's output
	// is compared to ok), so visibility lives here and gating is check's
	// explicit opt-in.
	for _, d := range uncovered {
		fmt.Fprintf(&b, "ok dir %s rules=0 uncovered\n", d)
	}
	for _, f := range findings {
		b.WriteString(f.String() + "\n")
	}
	return b.String()
}

// stateGraphSection: whole-graph size, not path-filtered (the graph has no
// per-node "within path" notion cheaper than a full scan; nodes=0 omits the
// section entirely rather than printing a vacuous zero record), plus a
// typed-call-pass degradation finding (see typedPassFinding) when the last
// reindex's go/types upgrade pass did not complete — omitted on a healthy
// pass, same as the rest of this section on an empty graph.
func (s *Server) stateGraphSection() string {
	nodes, edges := s.g.Stats()
	if nodes == 0 {
		return ""
	}
	line := fmt.Sprintf("ok graph nodes=%d edges=%d\n", nodes, edges)
	if f := s.typedPassFinding(); f != "" {
		line += f + "\n"
	}
	return line
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
		fmt.Fprintf(&b, "ag %s %s %s %s\n", a.Name, orDash(a.Item), hbAge(a.HB), wtLabel)
	}
	leases, err := s.cd.Leases(s.agentTTL())
	if err != nil {
		return "", err
	}
	for _, l := range leases {
		fmt.Fprintf(&b, "l %s %s %s %s\n", l.Path, l.Agent, orDash(l.Item), leaseLeft(l.Exp))
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
