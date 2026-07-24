// Package lifecycle implements the item state machine and its persistence
// effects. Fusion of the three lifecycle models: SpecKit (intent = proposal,
// work = tasks, linked via parent), OpenSpec (a proposal carries a delta
// spec; archiving merges it into the living spec.md), Cavekit-style tight
// loop (dense records, plain language — no encoding tricks).
//
// States: draft -> submitted -> approved -> active -> done -> archived,
// plus rejected. Transitions follow a total order over the six main states
// (draft < submitted < approved < active < done < archived): forward jumps
// are always legal — every hop is optional, so draft->active or
// approved->archived is a single move call. Guards are enforced here,
// server-side:
//   - rejected is reachable from any of the six states except archived, and
//     REQUIRES a note (the rejection corpus is a product feature);
//   - rejected is REVOCABLE (e.g. the rejection lacked information): the
//     reject journal event snapshots the full item, so `move` can restore it
//     into draft, submitted, approved or active (never done/archived) — and
//     reject events survive every compaction, so revocability is permanent;
//   - done -> active (reopen) is the one backward hop kept outside rejection;
//   - archived requires no open children; skipping straight to archived
//     (e.g. from active) implies done and runs the archive effects once;
//   - archived is terminal;
//   - rejected/archived items leave work.md — their summaries live in the
//     journal, searchable via `find scope=rejection|history`.
package lifecycle

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/jxsl13/spectacle/internal/item"
	"github.com/jxsl13/spectacle/internal/journal"
	"github.com/jxsl13/spectacle/internal/spec"
	"github.com/jxsl13/spectacle/internal/workspace"
)

// stateOrder is the total order over the six main states: any move from an
// earlier state to a later one is legal in one call (skips allowed).
// rejected sits outside the order — it is handled separately below.
var stateOrder = map[string]int{
	item.StateDraft:     0,
	item.StateSubmitted: 1,
	item.StateApproved:  2,
	item.StateActive:    3,
	item.StateDone:      4,
	item.StateArchived:  5,
}

// orderedStates lists the six main states in ascending order, for building
// Allowed() results and error messages.
var orderedStates = []string{
	item.StateDraft, item.StateSubmitted, item.StateApproved,
	item.StateActive, item.StateDone, item.StateArchived,
}

// Allowed returns the transitions available from a state, in order:
// remaining forward states, then the done->active reopen special case,
// then rejected (unless from is archived or already rejected). rejected
// itself only allows revocation into draft/submitted/approved/active.
func Allowed(from string) []string {
	if from == item.StateRejected {
		return []string{item.StateDraft, item.StateSubmitted, item.StateApproved, item.StateActive}
	}
	var out []string
	if ord, ok := stateOrder[from]; ok {
		for _, s := range orderedStates {
			if stateOrder[s] > ord {
				out = append(out, s)
			}
		}
	}
	if from == item.StateDone {
		out = append(out, item.StateActive) // reopen: the one kept backward hop
	}
	if from != item.StateArchived {
		out = append(out, item.StateRejected)
	}
	return out
}

func allowed(from, to string) bool {
	for _, t := range Allowed(from) {
		if t == to {
			return true
		}
	}
	return false
}

// Minter turns a scan-derived floor into the next unique ID number. The
// swarm coordination DB provides one that is collision-free across parallel
// worktrees; nil falls back to floor+1 (single-agent behavior).
type Minter func(kind string, floor int) (int, error)

// Draft creates a new item (state=draft) in the correct context dir:
// explicit dir > deepest common context dir of the targets > root.
func Draft(ws workspace.Root, mint Minter, kind, title, body, dir, parent string, targets []string) (item.Item, error) {
	if !item.ValidKind(kind) {
		return item.Item{}, fmt.Errorf("lifecycle: unknown kind %q", kind)
	}
	if strings.TrimSpace(title) == "" {
		return item.Item{}, fmt.Errorf("lifecycle: title required")
	}
	if parent != "" {
		if _, ok, err := item.Get(ws, parent); err != nil {
			return item.Item{}, err
		} else if !ok {
			return item.Item{}, fmt.Errorf("lifecycle: unknown parent %s", parent)
		}
	}
	ctx, err := scopeFor(ws, dir, targets)
	if err != nil {
		return item.Item{}, err
	}
	max, err := maxNum(ws, kind)
	if err != nil {
		return item.Item{}, err
	}
	if mint != nil {
		if max, err = mint("item:"+item.Letter(kind), max); err != nil {
			return item.Item{}, err
		}
		max-- // NextID below adds 1
	}
	it := item.Item{
		ID: item.NextID(kind, max), Kind: kind, State: item.StateDraft,
		Title: strings.TrimSpace(title), Dir: ctx, Parent: parent,
		Targets: targets, Body: strings.TrimSpace(body),
	}
	if err := item.Upsert(ws, it); err != nil {
		return item.Item{}, err
	}
	err = journal.Append(ws, ctx, journal.Event{
		Ev: journal.EvCreate, ID: it.ID, K: kind, Ti: it.Title, Dir: ctx,
	})
	return it, err
}

// Move transitions an item and applies the persistence effects. A rejected
// item (no longer in work.md) is restored from its reject journal snapshot.
func Move(ws workspace.Root, id, to, note string) (item.Item, error) {
	it, ok, err := item.Get(ws, id)
	if err != nil {
		return item.Item{}, err
	}
	if !ok {
		rej, found, err := lastReject(ws, id)
		if err != nil {
			return item.Item{}, err
		}
		if !found {
			return item.Item{}, fmt.Errorf("lifecycle: unknown item %s", id)
		}
		it = rej // state == rejected; falls through to the transition check
	}
	if !allowed(it.State, to) {
		return it, fmt.Errorf("lifecycle: %s: %s -> %s not allowed (allowed: %s)",
			id, it.State, to, strings.Join(Allowed(it.State), ", "))
	}
	if to == item.StateRejected && strings.TrimSpace(note) == "" {
		return it, fmt.Errorf("lifecycle: rejection requires a note — it becomes the searchable rejection corpus")
	}
	if to == item.StateArchived {
		if open := openChildren(ws, it); len(open) > 0 {
			return it, fmt.Errorf("lifecycle: %s has open children: %s", id, strings.Join(open, ", "))
		}
	}
	from := it.State
	ev := journal.Event{Ev: journal.EvMove, ID: it.ID, Fr: from, To: to, Note: note, Dir: it.Dir}
	if err := journal.Append(ws, it.Dir, ev); err != nil {
		return it, err
	}

	switch to {
	case item.StateRejected:
		// full snapshot so the rejection is revocable later
		if err := journal.Append(ws, it.Dir, journal.Event{
			Ev: journal.EvReject, ID: it.ID, K: it.Kind, Ti: it.Title,
			Sum: summary(it), Note: note, Dir: it.Dir,
			Body: it.Body, Tg: it.Targets, Par: it.Parent, Rls: it.Rules,
		}); err != nil {
			return it, err
		}
		if err := item.Remove(ws, it); err != nil {
			return it, err
		}
	case item.StateArchived:
		if err := archive(ws, it, note); err != nil {
			return it, err
		}
	default:
		it.State = to
		if err := item.Upsert(ws, it); err != nil {
			return it, err
		}
	}
	it.State = to
	return it, nil
}

// archive merges the item's outcome into the living spec (## intent line),
// journals the summary, removes it from work.md and archives its done
// children with it — the OpenSpec "delta merged on archive" moment.
func archive(ws workspace.Root, it item.Item, note string) error {
	line := "- " + it.ID + " " + it.Title
	if extra := firstOf(note, firstLine(it.Body)); extra != "" {
		line += ": " + extra
	}
	if err := spec.AppendIntent(ws, it.Dir, line); err != nil {
		return err
	}
	if err := journal.Append(ws, it.Dir, journal.Event{
		Ev: journal.EvArchive, ID: it.ID, K: it.Kind, Ti: it.Title,
		Sum: summary(it) + firstOf(" note: "+note, ""), Rls: it.Rules, Dir: it.Dir,
	}); err != nil {
		return err
	}
	if err := item.Remove(ws, it); err != nil {
		return err
	}
	// fold done children into the parent's archive
	all, err := item.LoadAll(ws)
	if err != nil {
		return err
	}
	for _, ch := range all {
		if ch.Parent == it.ID && ch.State == item.StateDone {
			if err := journal.Append(ws, ch.Dir, journal.Event{
				Ev: journal.EvArchive, ID: ch.ID, K: ch.Kind, Ti: ch.Title,
				Sum: "archived with parent " + it.ID, Dir: ch.Dir,
			}); err != nil {
				return err
			}
			if err := item.Remove(ws, ch); err != nil {
				return err
			}
		}
	}
	return nil
}

// lastReject reconstructs a rejected item from its most recent reject
// journal snapshot (state=rejected, ready for a revocation transition).
func lastReject(ws workspace.Root, id string) (item.Item, bool, error) {
	events, err := journal.ReadAll(ws)
	if err != nil {
		return item.Item{}, false, err
	}
	for i := len(events) - 1; i >= 0; i-- {
		e := events[i]
		if e.Ev == journal.EvReject && e.ID == id {
			return item.Item{
				ID: e.ID, Kind: e.K, State: item.StateRejected, Title: e.Ti,
				Dir: e.Dir, Parent: e.Par, Targets: e.Tg, Rules: e.Rls, Body: e.Body,
			}, true, nil
		}
	}
	return item.Item{}, false, nil
}

func openChildren(ws workspace.Root, it item.Item) []string {
	all, err := item.LoadAll(ws)
	if err != nil {
		return nil
	}
	var open []string
	for _, ch := range all {
		if ch.Parent == it.ID && ch.State != item.StateDone {
			open = append(open, ch.ID+"("+ch.State+")")
		}
	}
	sort.Strings(open)
	return open
}

// maxNum finds the highest ID number for a kind across journal create events
// (source of truth, includes archived/rejected) and active items.
func maxNum(ws workspace.Root, kind string) (int, error) {
	letter := item.Letter(kind)
	max := 0
	events, err := journal.ReadAll(ws)
	if err != nil {
		return 0, err
	}
	for _, e := range events {
		if e.Ev == journal.EvCreate && strings.HasPrefix(e.ID, letter+"-") {
			if n := item.Num(e.ID); n > max {
				max = n
			}
		}
	}
	items, err := item.LoadAll(ws)
	if err != nil {
		return 0, err
	}
	for _, it := range items {
		if strings.HasPrefix(it.ID, letter+"-") {
			if n := item.Num(it.ID); n > max {
				max = n
			}
		}
	}
	return max, nil
}

// scopeFor maps a draft to its context dir: explicit dir (scaffolded on
// demand) > deepest common existing context dir of the targets > root.
func scopeFor(ws workspace.Root, dir string, targets []string) (string, error) {
	if dir != "" && dir != "." {
		return strings.Trim(path.Clean(dir), "/"), nil
	}
	ctxs, err := ws.ContextDirs()
	if err != nil {
		return "", err
	}
	common := ""
	first := true
	for _, t := range targets {
		if !strings.ContainsAny(t, "/.") || strings.Contains(t, ":") {
			continue // node ID, not a path (dir mapping via graph lands in M1)
		}
		d := path.Dir(t)
		if d == "." {
			d = ""
		}
		if first {
			common, first = d, false
			continue
		}
		common = commonDir(common, d)
	}
	if first {
		return "", nil
	}
	return workspace.NearestContext(ctxs, common), nil
}

func commonDir(a, b string) string {
	as, bs := strings.Split(a, "/"), strings.Split(b, "/")
	var out []string
	for i := 0; i < len(as) && i < len(bs); i++ {
		if as[i] != bs[i] {
			break
		}
		out = append(out, as[i])
	}
	return strings.Join(out, "/")
}

func summary(it item.Item) string {
	s := it.Kind + " " + it.Title
	if b := firstLine(it.Body); b != "" {
		s += " — " + b
	}
	if len(s) > 400 {
		s = s[:400]
	}
	return s
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(s)
}

func firstOf(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" && strings.TrimSpace(v) != "note:" {
			return v
		}
	}
	return ""
}
