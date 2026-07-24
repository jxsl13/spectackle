// Package replay merges a worktree's .spectacle state onto main
// SEMANTICALLY: journal events are the operation log (CRDT-style), main's
// spec.md/work.md are materialized views, and replay re-applies the
// worktree's event delta through the same code paths the tools use. Git
// never textually merges .spectacle files (branches carry code only).
//
// Idempotency, twice over: the delta excludes event IDs already recorded as
// applied (coord.applied — a retried submit after a partial failure resumes
// precisely), and every individual operation is convergent (duplicate
// journal appends filtered by Eid, rule adds skip on equal content, work.md
// upserts overwrite, intent lines check containment).
package replay

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jxsl13/spectacle/internal/coord"
	"github.com/jxsl13/spectacle/internal/drift"
	"github.com/jxsl13/spectacle/internal/graph"
	"github.com/jxsl13/spectacle/internal/item"
	"github.com/jxsl13/spectacle/internal/journal"
	"github.com/jxsl13/spectacle/internal/spec"
	"github.com/jxsl13/spectacle/internal/workspace"
	"github.com/jxsl13/spectacle/internal/wt"
)

// Report summarizes one replay run (rendered as dense records by the caller).
type Report struct {
	Events  int               // delta events applied
	Rules   int               // rule ops applied
	Items   []string          // item IDs reconciled
	Remap   map[string]string // rule IDs re-minted due to collision on main
	Intents int
}

// Run replays the worktree's journal delta onto main. Caller holds the
// integrate lock. base is the branch-point commit sha; itemID keys the
// applied-set.
func Run(main, wtWS workspace.Root, itemID, base string, cd *coord.DB, g graph.Graph) (Report, error) {
	rep := Report{Remap: map[string]string{}}

	applied, err := cd.Applied(itemID)
	if err != nil {
		return rep, err
	}
	ctxs, err := wtWS.ContextDirs()
	if err != nil {
		return rep, err
	}

	mainCascade, err := spec.Load(main.Dir)
	if err != nil {
		return rep, err
	}

	type ctxDelta struct {
		ctx    string
		events []journal.Event
	}
	var deltas []ctxDelta
	for _, ctx := range ctxs {
		baseline, err := baselineEids(main.Dir, base, ctx)
		if err != nil {
			return rep, err
		}
		events, err := journal.Read(wtWS, ctx)
		if err != nil {
			return rep, err
		}
		var delta []journal.Event
		for _, e := range events {
			if e.Eid == "" {
				return rep, fmt.Errorf("replay: %s has a pre-swarm journal event without eid — compact on main first", ctx)
			}
			if baseline[e.Eid] || applied[e.Eid] {
				continue
			}
			delta = append(delta, e)
		}
		if len(delta) > 0 {
			deltas = append(deltas, ctxDelta{ctx, delta})
		}
	}

	touchedItems := map[string]bool{}
	for _, d := range deltas {
		// 1. history: append delta events verbatim (Eid-filtered against
		// main's recent events for the crash window before MarkApplied)
		recent, err := recentEids(main, d.ctx)
		if err != nil {
			return rep, err
		}
		var eids []string
		for _, e := range d.events {
			eids = append(eids, e.Eid)
			if !recent[e.Eid] {
				if err := journal.Append(main, d.ctx, e); err != nil {
					return rep, err
				}
			}
			rep.Events++

			switch e.Ev {
			case journal.EvRule:
				if err := applyRule(main, mainCascade, e, cd, g, &rep); err != nil {
					return rep, err
				}
				// cascade changed on disk; reload for subsequent ops
				if mainCascade, err = spec.Load(main.Dir); err != nil {
					return rep, err
				}
			case journal.EvCreate, journal.EvMove, journal.EvReject,
				journal.EvGrill, journal.EvDecide, journal.EvEscalate:
				// SDD orchestration v2 events (T-0030): grill/decide/escalate
				// carry no bespoke replay effect of their own — the resulting
				// item state (blocked/rounds/needs/...) is captured by the
				// worktree's work.md and reconciled below like any other
				// touched item. Recorded in history either way; tolerated
				// even for older journals that predate them.
				if e.ID != "" {
					touchedItems[e.ID] = true
				}
			case journal.EvArchive:
				if e.ID != "" {
					touchedItems[e.ID] = true
				}
				line := intentLine(e)
				ok, err := intentContains(main, d.ctx, line)
				if err != nil {
					return rep, err
				}
				if !ok {
					if err := spec.AppendIntent(main, d.ctx, line); err != nil {
						return rep, err
					}
					rep.Intents++
				}
			}
		}
		if err := cd.MarkApplied(itemID, eids); err != nil {
			return rep, err
		}
	}

	// 2. work.md reconciliation: final state wins (IDs are coord-minted and
	// item-leased, so no sibling writes the same ID)
	wtItems, err := item.LoadAll(wtWS)
	if err != nil {
		return rep, err
	}
	byID := map[string]item.Item{}
	for _, it := range wtItems {
		byID[it.ID] = it
	}
	for id := range touchedItems {
		rep.Items = append(rep.Items, id)
		if it, ok := byID[id]; ok {
			for i, r := range it.Rules {
				if to, remapped := rep.Remap[r]; remapped {
					it.Rules[i] = to
				}
			}
			if err := item.Upsert(main, it); err != nil {
				return rep, err
			}
		} else if mainIt, ok, _ := item.Get(main, id); ok {
			// rejected or archived in the worktree -> leaves main's work.md
			if err := item.Remove(main, mainIt); err != nil {
				return rep, err
			}
		}
	}
	return rep, nil
}

// applyRule replays one rule event onto main's spec bundles.
func applyRule(main workspace.Root, c *spec.Cascade, e journal.Event, cd *coord.DB, g graph.Graph, rep *Report) error {
	rep.Rules++
	switch e.Op {
	case "add":
		if existing, ok := c.Rule(e.Rule); ok {
			if drift.NormHash([]byte(existing.Text)) == drift.NormHash([]byte(e.Txt)) {
				return nil // identical rule already on main (retry or sibling)
			}
			// genuine collision (pre-coord mint): re-mint and record the remap
			stem := e.Rule[:strings.LastIndex(e.Rule, "-")]
			res, err := spec.AddRule(main, c, spec.AuthorReq{
				Dir: e.Dir, Stem: stem, Sentence: e.Txt, Applies: e.Ap,
				Mint: func(s string, floor int) (int, error) { return cd.NextID("rule:"+s, floor) },
			})
			if err != nil || !res.Written {
				return fmt.Errorf("replay: remap of %s failed: %v %v", e.Rule, res.Findings, err)
			}
			rep.Remap[e.Rule] = res.ID
			_ = cd.Emit("remap", e.Rule, "replayed as "+res.ID)
			return stampAnchors(main, g, res.ID, e.Txt, e.Ap)
		}
		res, err := spec.AddRule(main, c, spec.AuthorReq{
			Dir: e.Dir, ForceID: e.Rule, Sentence: e.Txt, Applies: e.Ap,
		})
		if err != nil {
			return err
		}
		if !res.Written {
			return fmt.Errorf("replay: rule %s failed lint on main: %v", e.Rule, res.Findings)
		}
		return stampAnchors(main, g, e.Rule, e.Txt, e.Ap)
	case "edit":
		id := e.Rule
		if to, ok := rep.Remap[id]; ok {
			id = to
		}
		if _, ok := c.Rule(id); !ok {
			return nil // retired by a sibling or already-applied retry — skip
		}
		res, err := spec.EditRule(main, c, id, e.Txt, "", e.Ap)
		if err != nil || !res.Written {
			return fmt.Errorf("replay: edit %s: %v %v", id, res.Findings, err)
		}
		return stampAnchors(main, g, id, e.Txt, e.Ap)
	case "retire":
		id := e.Rule
		if to, ok := rep.Remap[id]; ok {
			id = to
		}
		if _, ok := c.Rule(id); !ok {
			return nil // already gone — idempotent
		}
		_, err := spec.RetireRule(main, c, id)
		return err
	}
	return nil
}

func stampAnchors(main workspace.Root, g graph.Graph, rule, text string, applies []string) error {
	if len(applies) == 0 {
		return nil
	}
	anchors, err := drift.Load(main)
	if err != nil {
		return err
	}
	for _, node := range applies {
		anchors = drift.Upsert(anchors, drift.Stamp(main, g, rule, text, graph.NodeID(node)))
	}
	return drift.Save(main, anchors)
}

func intentLine(e journal.Event) string { return "- " + e.ID + " " + e.Ti }

func intentContains(main workspace.Root, ctx, line string) (bool, error) {
	c, err := spec.Load(main.Dir)
	if err != nil {
		return false, err
	}
	sf, ok := c.File(ctx)
	if !ok {
		return false, nil
	}
	for _, sec := range sf.Sections {
		if sec.Name == "intent" && strings.Contains(sec.Text, line) {
			return true, nil
		}
	}
	return false, nil
}

// baselineEids collects the event IDs present in a context's journal at the
// branch-point commit.
func baselineEids(mainDir, base, ctx string) (map[string]bool, error) {
	rel := ctx
	if rel != "" {
		rel += "/"
	}
	rel += workspace.Dot + "/journal.ndjson"
	raw, err := wt.ShowFile(mainDir, base, rel)
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, l := range strings.Split(string(raw), "\n") {
		if l == "" {
			continue
		}
		var e struct {
			Eid string `json:"eid"`
		}
		if json.Unmarshal([]byte(l), &e) == nil && e.Eid != "" {
			out[e.Eid] = true
		}
	}
	return out, nil
}

// recentEids guards the crash window between file append and MarkApplied.
func recentEids(main workspace.Root, ctx string) (map[string]bool, error) {
	events, err := journal.Read(main, ctx)
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	start := 0
	if len(events) > 200 {
		start = len(events) - 200
	}
	for _, e := range events[start:] {
		if e.Eid != "" {
			out[e.Eid] = true
		}
	}
	return out, nil
}
