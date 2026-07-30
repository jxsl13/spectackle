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
	"unicode/utf8"

	"github.com/jxsl13/spectackle/internal/drift"
	"github.com/jxsl13/spectackle/internal/graph"
	"github.com/jxsl13/spectackle/internal/ids"
	"github.com/jxsl13/spectackle/internal/item"
	"github.com/jxsl13/spectackle/internal/journal"
	"github.com/jxsl13/spectackle/internal/spec"
	"github.com/jxsl13/spectackle/internal/workspace"
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
// blocked (see item.StateBlocked) is a side state like rejected but stricter
// still: it allows NO move-driven transitions at all — only
// lifecycle.ResolveBlocked, acting on the linked decision item's outcome,
// can move an item out of it. blocked is never a legal Move destination
// either (it never appears in any from's result here) — only Escalate sets
// it.
func Allowed(from string) []string {
	if from == item.StateBlocked {
		return nil
	}
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
//
// Item IDs no longer come from a Minter: ADR-0013 moved them to
// item.MintID, which needs no coordination because a UUIDv7 is unique
// without it. Draft and Escalate still TAKE a Minter — every caller passes
// one and rules are still minted through coord.NextID, which is a separate
// counter and a separate task's concern — but the value is now unused by the
// item path. The parameter stays so that no call site outside this package
// has to change for an ID-scheme swap; retiring it belongs with retiring the
// item counter itself.
type Minter func(kind string, floor int) (int, error)

// Draft creates a new item (state=draft) in the correct context dir:
// explicit dir > deepest common context dir of the targets > root.
//
// refs is variadic so every pre-existing call site (none of which cite
// anything) keeps compiling unchanged — pass zero or more item IDs to
// stitch a citation set into the same single item.Upsert this function
// already performs. Draft does not validate refs itself (it has no
// journal-tombstone-aware "known ID" set to check against, and a
// self-reference cannot happen here since the ID does not exist yet
// anyway); callers that need validation (see item.UnknownRefs) must do it
// before calling Draft and refuse on a non-empty result.
// The mint parameter is accepted and ignored — see Minter. IDs come from
// item.MintID, which is collision-free across clones as well as across
// worktrees, which the counter never was.
func Draft(ws workspace.Root, _ Minter, kind, title, body, dir, parent string, targets []string, refs ...string) (item.Item, error) {
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
			// an archived parent (Tombstone hit) is legitimate provenance —
			// only a parent that resolves nowhere is rejected.
			if _, tombOk, err := Tombstone(ws, parent); err != nil {
				return item.Item{}, err
			} else if !tombOk {
				return item.Item{}, fmt.Errorf("lifecycle: unknown parent %s", parent)
			}
		}
	}
	ctx, err := scopeFor(ws, dir, targets)
	if err != nil {
		return item.Item{}, err
	}
	it := item.Item{
		ID: item.MintID(kind), Kind: kind, State: item.StateDraft,
		Title: strings.TrimSpace(title), Dir: ctx, Parent: parent,
		Targets: targets, Body: strings.TrimSpace(body), Refs: refs,
	}
	if err := item.Upsert(ws, it); err != nil {
		return item.Item{}, err
	}
	err = journal.Append(ws, ctx, journal.Event{
		Ev: journal.EvCreate, ID: it.ID, K: kind, Ti: it.Title, Dir: ctx,
	})
	return it, err
}

// ErrRoundsExhausted is returned by Move when a done->active reopen would
// push an item's Rounds counter to (or past) the configured feedback round
// limit (ws.Cfg.Feedback.MaxRounds). Move cannot escalate the item itself —
// escalation mints a new decision item, which needs a Minter, and Move's
// signature carries none (changing it would break every existing caller).
// So Move only persists the incremented Rounds counter (+ journal event) and
// leaves the item on done, then returns this error carrying the item as it
// stands. The caller — mcpserver's move handler in a later task — is
// expected to catch it (errors.As) and call Escalate(ws, mint, it) to
// actually transition the item into item.StateBlocked and mint the
// decision item.
type ErrRoundsExhausted struct {
	Item item.Item
}

func (e ErrRoundsExhausted) Error() string {
	return fmt.Sprintf("lifecycle: %s: feedback rounds exhausted (%d) — resolve via Escalate", e.Item.ID, e.Item.Rounds)
}

// maxRounds resolves the configured feedback round limit, defaulting to 3
// for zero-value workspace.Root (e.g. constructed directly in tests without
// going through workspace.Detect/load, which apply the same default).
func maxRounds(ws workspace.Root) int {
	if ws.Cfg.Feedback.MaxRounds > 0 {
		return ws.Cfg.Feedback.MaxRounds
	}
	return 3
}

// moveOpts carries Move's optional, additive parameters. Zero value = every
// option off, which is exactly Move's behavior before the audit gate below
// existed — so the field set here grows without ever touching an existing
// call site (see MoveOption).
type moveOpts struct {
	g        graph.Graph
	ruleText func(string) (string, bool)
}

// MoveOption customizes a Move call. Options are the least invasive way to
// grow Move's parameters: every existing call site (positional ws, id, to,
// note) keeps compiling and keeps its old behavior untouched, since omitting
// opts... leaves moveOpts at its zero value.
type MoveOption func(*moveOpts)

// WithAuditGate supplies the symbol graph and a rule-text resolver so Move
// can enforce the done-state audit gate (see the auditGate doc comment
// below). The ruleText closure mirrors the one internal/mcpserver's check
// tool builds from the loaded spec.Cascade: given a rule ID, return its
// current sentence and whether the rule still exists in the cascade
// (false routes the anchor to drift.Stale, which never blocks). Without this
// option Move performs no drift check at all — unchanged, pre-gate behavior.
func WithAuditGate(g graph.Graph, ruleText func(string) (string, bool)) MoveOption {
	return func(o *moveOpts) {
		o.g = g
		o.ruleText = ruleText
	}
}

// Move transitions an item and applies the persistence effects. A rejected
// item (no longer in work.md) is restored from its reject journal snapshot.
//
// When to is done, or a forward skip that implies done (archived — see the
// package doc comment), and the caller supplied WithAuditGate, Move refuses
// the transition if any anchor bound to the item's targets carries
// unresolved audit-class drift (drift.Tightened or drift.Diverged — a human
// has to look, per package drift's doc comment). drift.Evolved anchors never
// block: they are mechanically healable by check and re-stamp themselves the
// next time it runs, so gating on them would just make done unreachable
// without giving the caller anything to fix here. See auditGate below.
func Move(ws workspace.Root, id, to, note string, opts ...MoveOption) (item.Item, error) {
	var o moveOpts
	for _, opt := range opts {
		opt(&o)
	}
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
	if it.State == item.StateBlocked {
		return it, fmt.Errorf("lifecycle: %s: blocked — resolve via decide %s", id, lastNeed(it.Needs))
	}
	if !allowed(it.State, to) {
		return it, fmt.Errorf("lifecycle: %s: %s -> %s not allowed (allowed: %s)",
			id, it.State, to, strings.Join(Allowed(it.State), ", "))
	}
	if to == item.StateRejected && strings.TrimSpace(note) == "" {
		return it, fmt.Errorf("lifecycle: rejection requires a note — it becomes the searchable rejection corpus")
	}
	if to == item.StateDone || to == item.StateArchived {
		if err := auditGate(ws, o.g, o.ruleText, it); err != nil {
			return it, err
		}
	}
	if to == item.StateArchived {
		if open := openChildren(ws, it); len(open) > 0 {
			return it, fmt.Errorf("lifecycle: %s has open children: %s", id, strings.Join(open, ", "))
		}
	}

	// done->active is the reopen special case: it counts against the
	// configured feedback round budget before the move is allowed to land.
	reopen := it.State == item.StateDone && to == item.StateActive
	newRounds := it.Rounds
	if reopen {
		newRounds++
	}

	from := it.State
	ev := journal.Event{Ev: journal.EvMove, ID: it.ID, Fr: from, To: to, Note: note, Dir: it.Dir}
	if reopen {
		ev.Rnd = newRounds
	}
	if err := journal.Append(ws, it.Dir, ev); err != nil {
		return it, err
	}

	if reopen && newRounds >= maxRounds(ws) {
		// rounds exhausted: persist the counter bump but do NOT move the
		// item — it stays on done until the caller escalates it.
		it.Rounds = newRounds
		if err := item.Upsert(ws, it); err != nil {
			return it, err
		}
		return it, ErrRoundsExhausted{Item: it}
	}

	switch to {
	case item.StateRejected:
		// full snapshot so the rejection is revocable later
		rej := journal.Event{
			Ev: journal.EvReject, ID: it.ID, K: it.Kind, Ti: it.Title,
			Sum: summary(it), Note: note, Dir: it.Dir, Body: it.Body,
		}
		carryRecord(&rej, it)
		if err := journal.Append(ws, it.Dir, rej); err != nil {
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
		if reopen {
			it.Rounds = newRounds
		}
		if err := item.Upsert(ws, it); err != nil {
			return it, err
		}
	}
	it.State = to
	return it, nil
}

// lastNeed returns the most recently added ID in needs (the decision the
// item is currently blocked on), or "" if empty.
func lastNeed(needs []string) string {
	if len(needs) == 0 {
		return ""
	}
	return needs[len(needs)-1]
}

// shortID renders the display form of a record ID: kind prefix plus the
// first ids.MinRecordPrefixLen body characters — the same convention as
// the server's shortDisplayID, duplicated locally because lifecycle cannot
// import the server package (B-01KYKEWMHEFW1; promote to a shared home if
// a third caller appears). Legacy or short IDs pass through unchanged.
func shortID(id string) string {
	kind, body, ok := strings.Cut(id, "-")
	if !ok || len(body) <= ids.MinRecordPrefixLen {
		return id
	}
	return kind + "-" + body[:ids.MinRecordPrefixLen]
}

// Escalate transitions a done item that has exhausted its feedback rounds
// (see ErrRoundsExhausted) into the item.StateBlocked side state and mints an
// adr item (kind=adr) recording the ways out: rescope, reject, or
// override-once (omitted once it.Override has already been spent once).
// Move cannot do this itself since it has no Minter; callers that catch
// ErrRoundsExhausted from Move are expected to call this with one. Returns
// the updated (now blocked) item and the newly minted decision item.
func Escalate(ws workspace.Root, mint Minter, it item.Item) (item.Item, item.Item, error) {
	options := []string{"rescope", "reject"}
	if !it.Override {
		options = append(options, "override-once")
	}
	optStr := strings.Join(options, "|")
	// The hint is the EXACT callable invocation (B-01KYKEWMHEFW1: a judge
	// following the old "decide <task-id> outcome=..." text failed twice —
	// decide has no outcome field, and the target is the ADR, not the
	// task). The ADR's own ID exists only after Draft mints it, so the
	// body is completed in a second write.
	body := fmt.Sprintf("%s exhausted its feedback rounds (%d).", shortID(it.ID), it.Rounds)
	// shortID, matching the body one line above and every display path —
	// the full ID here made `state` render the same record at two different
	// lengths one line apart, which invites pasting a long ID where the
	// short one is expected.
	d, err := Draft(ws, mint, "adr", "escalate "+shortID(it.ID)+": "+optStr, body, it.Dir, it.ID, nil)
	if err != nil {
		return it, item.Item{}, err
	}
	d.Body = fmt.Sprintf("%s exhausted its feedback rounds (%d). Resolve via decide op=answer id=%s choose=%s.",
		shortID(it.ID), it.Rounds, shortID(d.ID), optStr)
	if err := item.Upsert(ws, d); err != nil {
		return it, d, err
	}
	it.State = item.StateBlocked
	it.Needs = append(it.Needs, d.ID)
	if err := item.Upsert(ws, it); err != nil {
		return it, d, err
	}
	if err := journal.Append(ws, it.Dir, journal.Event{
		Ev: journal.EvEscalate, ID: it.ID, Dir: it.Dir, Note: d.ID, Rnd: it.Rounds, Nd: it.Needs,
	}); err != nil {
		return it, d, err
	}
	return it, d, nil
}

// ResolveBlocked resolves an item stuck in item.StateBlocked (see Escalate)
// according to outcome, recorded on its linked decision item:
//   - "rescope": item goes back to draft; Rounds resets to 0 (a fresh
//     feedback budget), Grilled is kept (still-valid context for the
//     rescoped work).
//   - "reject": the item is rejected outright (note required — it becomes
//     the searchable rejection corpus); the reject snapshot includes
//     Rounds/Grilled/Needs/Override so it stays revocable like any other
//     rejection.
//   - "override-once": forces the item back to active for one more round
//     without counting against the limit again (Rounds resets to 0); usable
//     only once per item — Override is set and a second override-once
//     errors.
func ResolveBlocked(ws workspace.Root, id, outcome, note string) (item.Item, error) {
	it, ok, err := item.Get(ws, id)
	if err != nil {
		return item.Item{}, err
	}
	if !ok {
		return item.Item{}, fmt.Errorf("lifecycle: unknown item %s", id)
	}
	if it.State != item.StateBlocked {
		return it, fmt.Errorf("lifecycle: %s: not blocked (state=%s)", id, it.State)
	}

	switch outcome {
	case "rescope":
		it.Rounds = 0
		it.State = item.StateDraft
		if err := journal.Append(ws, it.Dir, journal.Event{
			Ev: journal.EvDecide, ID: it.ID, Dir: it.Dir, To: item.StateDraft, Note: note,
		}); err != nil {
			return it, err
		}
		if err := item.Upsert(ws, it); err != nil {
			return it, err
		}
		return it, nil
	case "reject":
		if strings.TrimSpace(note) == "" {
			return it, fmt.Errorf("lifecycle: rejection requires a note — it becomes the searchable rejection corpus")
		}
		rej := journal.Event{
			Ev: journal.EvReject, ID: it.ID, K: it.Kind, Ti: it.Title,
			Sum: summary(it), Note: note, Dir: it.Dir, Body: it.Body,
		}
		carryRecord(&rej, it)
		if err := journal.Append(ws, it.Dir, rej); err != nil {
			return it, err
		}
		if err := item.Remove(ws, it); err != nil {
			return it, err
		}
		if err := journal.Append(ws, it.Dir, journal.Event{
			Ev: journal.EvDecide, ID: it.ID, Dir: it.Dir, To: item.StateRejected, Note: note,
		}); err != nil {
			return it, err
		}
		it.State = item.StateRejected
		return it, nil
	case "override-once":
		if it.Override {
			return it, fmt.Errorf("lifecycle: %s: override-once already used", id)
		}
		it.Rounds = 0
		it.Override = true
		it.State = item.StateActive
		if err := journal.Append(ws, it.Dir, journal.Event{
			Ev: journal.EvDecide, ID: it.ID, Dir: it.Dir, To: item.StateActive, Note: note, Ov: true,
		}); err != nil {
			return it, err
		}
		if err := item.Upsert(ws, it); err != nil {
			return it, err
		}
		return it, nil
	default:
		return it, fmt.Errorf("lifecycle: unknown outcome %q (want rescope|reject|override-once)", outcome)
	}
}

// archive merges the item's outcome into the living spec (## intent line),
// journals the summary, removes it from work.md and archives its done
// children with it — the OpenSpec "delta merged on archive" moment.
func archive(ws workspace.Root, it item.Item, note string) error {
	gist := capGist(firstOf(note, gistLine(it)))
	if err := spec.AppendIntent(ws, it.Dir, IntentLine(it.ID, it.Title, gist)); err != nil {
		return err
	}
	ev := journal.Event{
		Ev: journal.EvArchive, ID: it.ID, K: it.Kind, Ti: it.Title,
		Sum: summary(it) + firstOf(" note: "+capGist(note), ""), Dir: it.Dir,
		Note: note, Gist: gist,
	}
	carryRecord(&ev, it)
	ev.Body = retainedBody(it)
	if err := journal.Append(ws, it.Dir, ev); err != nil {
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
			chEv := journal.Event{
				Ev: journal.EvArchive, ID: ch.ID, K: ch.Kind, Ti: ch.Title,
				Sum: "archived with parent " + it.ID, Dir: ch.Dir,
			}
			carryRecord(&chEv, ch)
			// the same invariant through the SECOND archive path: a child
			// folded into its parent's closure keeps its outcome
			// (cross-val-research finding 5 reproduced the citation loss
			// here for research; the adr arm closes it for decisions)
			chEv.Body = retainedBody(ch)
			if err := journal.Append(ws, ch.Dir, chEv); err != nil {
				return err
			}
			if err := item.Remove(ws, ch); err != nil {
				return err
			}
		}
	}
	return nil
}

// Tombstone reconstructs an archived item from its most recent archive
// journal event — the read-only afterlife for an id reference once
// work.md no longer has it (see archive above: an archived item leaves
// work.md and its outcome lives in the journal from then on). Returns
// ok=false if no archive event exists for id. Read-only: callers MUST NOT
// item.Upsert the result — a tombstone has no work.md home. compact's fold
// retention keeps EvArchive events forever, so tombstones survive
// compaction.
// retainedBodyMax caps the body a research tombstone carries: findings are
// compact by convention, and the journal replays on every read.
const retainedBodyMax = 8192

// retainedBody is the tombstone copy of an item's substance, for the kinds
// whose body IS the outcome. The distinction is not about kind names but
// about the delta: a proposal, task or bug compacts fairly because its
// change merged into spec.md and the code, so a summary suffices. research
// and adr have no such delta — the record is the whole artifact — so
// compressing them deleted the only copy. research was the first
// (issue 178 defect 3: 268 findings lost every citation); adr is the
// second, found by the same probe one class later, and worse: an ADR minted
// by decide carries a STRUCTURED body whose first line is the machine field
// "kind: radio", so every such record archived to the byte-identical, empty
// summary "adr <question> — kind: radio".
//
// An ADR also keeps its outcome in item fields journal.Event has no channel
// for, so retaining Body alone would still lose which option won. They are
// folded in as text, in work.md's own field order and spelling (see
// item.Item's Marshal), which is also the form item.Parse reads back.
//
// The compaction keep-list already preserves EvArchive verbatim, so folds
// keep whatever is retained here.
func retainedBody(it item.Item) string {
	if !RetainsBody(it.Kind) {
		return ""
	}
	return capRetainedBody(it.Body)
}

// carryRecord copies the item fields an event must preserve so the record
// can be reconstructed from it. Both boundaries that remove a record from
// work.md call this — which is the point: the two used to populate
// overlapping-but-different subsets, so reject preserved Targets/Parent
// that archive dropped while archive preserved Refs that reject dropped,
// and the FAILURE path was more careful with structural data than the
// SUCCESS path. One writer means they cannot drift apart again.
//
// The corresponding reader is restoreRecord. TestItemEventRoundTrip walks
// every item.Item field and fails when one is neither carried here nor
// named in its explicit drop list, so a field added to Item cannot silently
// fall through this boundary the way five of them already did.
func carryRecord(ev *journal.Event, it item.Item) {
	ev.Par, ev.Tg, ev.Refs = it.Parent, it.Targets, it.Refs
	ev.Rnd, ev.Gr, ev.Nd, ev.Ov = it.Rounds, it.Grilled, it.Needs, it.Override
	// Capped individually. They ride the event as their own fields now, so
	// they no longer compete with the body for a shared budget — which is
	// what made a large context able to amputate `decision:` three separate
	// times. A value past this cap is prose that belongs in the body.
	ev.Ctx = capRetainedBodyTo(it.Context, outcomeFieldMax)
	ev.Dec = capRetainedBodyTo(it.Decision, outcomeFieldMax)
	ev.Cons = capRetainedBodyTo(it.Consequences, outcomeFieldMax)
	ev.St = capRetainedBodyTo(it.Status, outcomeFieldMax)
	ev.Crt = carriedCreated(it)
}

// restoreRecord is carryRecord's inverse: it fills a reconstructed item
// from the event. Used by both lastReject (revocation) and Tombstone —
// Tombstone previously rebuilt only ID/Kind/Title/Dir/State/Body, so an
// archived record could not answer what it belonged to or touched even
// though the event had carried Rls and Refs all along. The loss was in the
// reader, not the writer, which is why probing the raw journal missed it.
func restoreRecord(it *item.Item, e journal.Event) {
	it.Parent, it.Targets, it.Refs = e.Par, e.Tg, e.Refs
	it.Rounds, it.Grilled, it.Needs, it.Override = e.Rnd, e.Gr, e.Nd, e.Ov
	// Normalize rather than trust. carryRecord no longer manufactures a blank
	// line at the truncation cut, but this is the path with no caller to refuse
	// to: an event written by an older binary, or any future value the header
	// cannot represent, must not make a record unrestorable. A closed-up blank
	// line is a lesser loss than a rejected item that can never be revoked.
	it.Context = item.NormalizeHeaderValue(e.Ctx)
	it.Decision = item.NormalizeHeaderValue(e.Dec)
	it.Consequences = item.NormalizeHeaderValue(e.Cons)
	it.Status = e.St
	it.Created = restoredCreated(e.ID, e.Crt)
}

// carriedCreated implements the storage half of ADR-01KYNA70PQFTB: a
// modern record ID is a UUIDv7 whose mint time IS the created date, so
// nothing is written; only a legacy sequential ID (P-0007), which carries
// no timestamp and which this program must keep parsing forever, needs the
// date on the event.
func carriedCreated(it item.Item) string {
	if item.LegacyIDRe.MatchString(it.ID) {
		return it.Created
	}
	return ""
}

// restoredCreated is the retrieval half: derive from the ID when it can be,
// else use what was carried. It must never return "" for a resolvable ID —
// item.Upsert defaults an empty Created to time.Now(), which is how a
// revoke came to stamp a fresh date over a real one, silently and
// indistinguishably from a true value. That was the only CORRUPTION in
// P-01KYN5YCXGENM rather than a loss.
func restoredCreated(id, carried string) string {
	// An item ID is "<kind letter>-<record ID>" (see item.MintIDAt), so the
	// prefix comes off before parsing. A legacy ID's tail is four digits and
	// fails to parse, which is exactly the case that falls back to carried.
	if _, tail, ok := strings.Cut(id, "-"); ok {
		if rid, err := ids.ParseRecordID(tail); err == nil {
			return rid.Time().UTC().Format("2006-01-02")
		}
	}
	return carried
}

// RetainsBody reports whether a kind's archive tombstone keeps its body
// rather than compressing to a summary — the one place that answers it, so
// a renderer never has to restate the list. It matters to readers because
// Tombstone falls back to the summary when no body was retained: without
// this predicate, "the tombstone has a body" is true for every kind and
// says nothing.
func RetainsBody(kind string) bool {
	return kind == "research" || kind == "adr"
}

// outcomeFieldMax bounds each must-keep ADR field on its own, so the
// must-keep blob has a size the rest of the budget can always be computed
// against. A decision or a status longer than this is already prose that
// belongs in context; truncating it is a real loss but a bounded one, and
// far smaller than dropping the field entirely.
const outcomeFieldMax = 2048

// intentGistMax bounds the one-line trace, matching summary()'s own 400-byte
// cap: both are single-line digests, not the record. It is deliberately far
// smaller than retainedBodyMax because this one is written into spec.md,
// which is permanent, human-read, and reviewed in diffs — a 60KB "line"
// there is not a large record, it is a broken file.
const intentGistMax = 400

// capGist bounds a one-line digest, visibly. The full text is never lost by
// this: an archive event carries the whole note in Note, which the journal
// index searches, so capping the digest costs nothing but noise. Every other
// retained value here is capped (retainedBodyMax, outcomeFieldMax); this one
// was not, and an uncapped note reached both spec.md and the journal — twice
// over, since Sum appended it again past summary()'s own cap.
func capGist(s string) string {
	s = strings.ReplaceAll(strings.TrimSpace(s), "\n", " ")
	return capRetainedBodyTo(s, intentGistMax)
}

// IntentLine composes the `## intent` line archive appends to spec.md, and
// is the ONLY place that shape is written. internal/replay used to compose
// its own, shorter version — ID and title, no gist — and because git never
// merges .spectackle text, replay is main's sole writer: every record ever
// archived through the worktree flow, the documented primary workflow,
// landed on main with the note stripped. Two functions obliged to agree
// about a permanent artifact is the shape that produced that; one is the
// fix. The gist travels on the event (journal.Event.Gist) precisely so both
// callers pass the same value rather than each deriving one.
func IntentLine(id, title, gist string) string {
	line := "- " + id + " " + title
	if gist != "" {
		line += ": " + gist
	}
	return line
}

// gistLine is the one-line substance of an item — the first body line for
// most kinds, but an adr's Decision when it has one. A decide-minted ADR's
// first body line is "kind: radio", which made both the spec.md intent line
// and the journal summary contentless for exactly the records whose entire
// value is the decision they carry.
func gistLine(it item.Item) string {
	if it.Kind != "adr" {
		return firstLine(it.Body)
	}
	if d := strings.TrimSpace(it.Decision); d != "" {
		return d
	}
	// Unanswered. Nothing here may name a side: the first `option:` line is
	// a candidate nobody chose, and rendering it as the gist made the
	// spec.md intent line and the journal summary read as a confident
	// decision FOR that option, with no trace that the others existed —
	// worse than the `kind: radio` it replaced, which was at least visibly
	// meaningless. Report the truth instead, with the size of the choice
	// that was on the table.
	if n := len(item.ParseOptions(it.Body)); n > 0 {
		return fmt.Sprintf("undecided (%d options)", n)
	}
	for _, line := range strings.Split(it.Body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || isDecideScaffold(line) {
			continue
		}
		return line
	}
	return "undecided"
}

// isDecideScaffold reports whether a body line is machine scaffolding a
// decide-minted ADR writes for itself rather than content a reader wants.
// `option:`/`choice:` are in the list because they name SIDES: quoting one
// as an item's gist asserts an outcome the record does not have.
func isDecideScaffold(line string) bool {
	for _, p := range []string{"kind:", "knowledge-conflict:", "status:", "item:", "option:", "options:", "choice:"} {
		if strings.HasPrefix(line, p) {
			return true
		}
	}
	return false
}

func capRetainedBody(b string) string {
	return capRetainedBodyTo(b, retainedBodyMax)
}

// capRetainedBodyTo is capRetainedBody against a caller-chosen budget, so a
// caller that must fit something else into the same cap (adrOutcome's
// structured fields) can reserve it rather than lose it off the end.
func capRetainedBodyTo(b string, max int) string {
	if len(b) <= max {
		return b
	}
	// never cut mid-rune: a multi-byte character straddling the cap left a
	// dangling lead byte in the journal (cross-val-research finding 1)
	cut := max
	for cut > 0 && !utf8.RuneStart(b[cut]) {
		cut--
	}
	// Trim what the cut ends on before appending the marker. The marker leads
	// with a newline, so a cut landing right after one produced a BLANK line —
	// and a blank line is what ends a machine header, so the value came back
	// unwritable and a rejected record could no longer be revoked at all
	// (B-01KYN3E973F20, found by independent verification). The cap is already
	// a deliberate, marked loss; trailing whitespace at the cut is not the part
	// worth keeping.
	return strings.TrimRight(b[:cut], "\n\r\t ") + truncationMarker
}

// truncationMarker is appended AFTER the cut, so a truncated body is
// retainedBodyMax bytes of content plus this marker — the cap bounds what
// is kept, not the exact field width.
//
// It is INLINE, and must stay inline. It used to lead with a newline, which
// made one composer emit a line break into consumers that have opposite line
// contracts: the work.md prose fields tolerate an extra line, but capGist
// flattens newlines to spaces precisely because its consumer is a single
// spec.md bullet — and then got a newline back from here, one call later. That
// put a bare marker on its own line in `## intent`, where it carried no record
// ID, so AppendIntent's dedupe could not key it and it survived every heal
// (B-01KYRQXJ99F48). A shared truncator cannot satisfy both contracts by
// picking one; it satisfies both by adding no line break at all and letting
// the caller supply a separator if it wants one.
const truncationMarker = " [body truncated at tombstone retention cap]"

func Tombstone(ws workspace.Root, id string) (item.Item, bool, error) {
	events, err := journal.ReadAll(ws)
	if err != nil {
		return item.Item{}, false, err
	}
	for i := len(events) - 1; i >= 0; i-- {
		e := events[i]
		if e.Ev == journal.EvArchive && e.ID == id {
			body := e.Sum
			if e.Body != "" {
				// research tombstones carry the retained finding; the
				// summary stays available in the journal event itself
				body = e.Body
			}
			out := item.Item{
				ID: e.ID, Kind: e.K, Title: e.Ti, Dir: e.Dir,
				State: item.StateArchived, Body: body,
			}
			restoreRecord(&out, e)
			return out, true, nil
		}
	}
	return item.Item{}, false, nil
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
			out := item.Item{
				ID: e.ID, Kind: e.K, State: item.StateRejected, Title: e.Ti,
				Dir: e.Dir, Body: e.Body,
			}
			restoreRecord(&out, e)
			return out, true, nil
		}
	}
	return item.Item{}, false, nil
}

// auditGate is the "an item cannot reach done while its bound contracts
// carry unresolved audit-class drift" check: it loads anchors.tsv, narrows
// it to the anchors bound to the item's targets (see boundAnchors), classifies
// them with drift.Classify — the same call internal/mcpserver/tools.go's
// check tool builds (graph + a rule-text closure over the loaded spec
// cascade) — and refuses when any of them is drift.Tightened or
// drift.Diverged. drift.Evolved never blocks: it is the mechanically
// healable class, re-stamped by check without a human needing to look.
//
// g == nil or ruleText == nil means Move was called without WithAuditGate:
// the check is skipped entirely (existing callers, and any test that
// doesn't care about drift). No bound anchors, or none in a blocking class,
// is also a no-op — items with no bound anchors are unaffected.
func auditGate(ws workspace.Root, g graph.Graph, ruleText func(string) (string, bool), it item.Item) error {
	if g == nil || ruleText == nil || len(it.Targets) == 0 {
		return nil
	}
	anchors, err := drift.Load(ws)
	if err != nil {
		return err
	}
	bound := boundAnchors(anchors, it.Targets)
	if len(bound) == 0 {
		return nil
	}
	var offenders []string
	// nil staleness predicate: the audit gate has no signal for "graph older
	// than file" (DRF-003 is check-only, wired in internal/mcpserver/tools.go
	// where the server actually knows its last reindex time) — nil preserves
	// the pre-DRF-003 hash-based behavior here unchanged.
	for _, r := range drift.Classify(ws, g, bound, ruleText, nil) {
		if r.Class == drift.Tightened || r.Class == drift.Diverged {
			offenders = append(offenders, fmt.Sprintf("! GATE E %s audit %s %s %s",
				it.ID, r.Anchor.Rule, r.Anchor.Node, r.Class))
		}
	}
	if len(offenders) == 0 {
		return nil
	}
	sort.Strings(offenders)
	return fmt.Errorf("%s", strings.Join(offenders, "\n"))
}

// boundAnchors narrows anchors to the ones bound to one of targets:
// node-ID-shaped targets ("go:pkg.Func", per targetPath below) match
// Anchor.Node directly; path-shaped targets match any anchor whose code span
// lives at that path or under it (a directory target covers every anchor
// beneath it) — mirroring the node-ID/path split internal/mcpserver's
// grillTargets/researchGaps use against the same anchor set.
func boundAnchors(anchors []drift.Anchor, targets []string) []drift.Anchor {
	var out []drift.Anchor
	seen := make([]bool, len(anchors))
	for _, t := range targets {
		if p, ok := targetPath(t); ok {
			for i, a := range anchors {
				if !seen[i] && (a.File == p || strings.HasPrefix(a.File, p+"/")) {
					seen[i] = true
					out = append(out, a)
				}
			}
			continue
		}
		for i, a := range anchors {
			if !seen[i] && string(a.Node) == t {
				seen[i] = true
				out = append(out, a)
			}
		}
	}
	return out
}

// targetPath decides whether a target is a file path (as opposed to a graph
// node ID) and strips an optional ":line" suffix — a duplicate, not an
// import, of internal/mcpserver/tools.go's targetPath: mcpserver imports
// lifecycle, not the other way around, and the split is three lines.
func targetPath(t string) (string, bool) {
	if i := strings.IndexByte(t, ':'); i > 0 {
		if !strings.ContainsAny(t[:i], "./") {
			return "", false
		}
		return t[:i], true
	}
	return t, strings.ContainsAny(t, "./")
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

// maxNum is gone with ADR-0013. It existed to find the highest counter a kind
// had ever used — across journal events as well as active items, because a
// compact can fold a create event away and an id whose only witness was that
// event would otherwise be minted a second time (seen live: ADR-0001..0004
// and P-0067 re-minted after a compact). Every one of those hazards is a
// property of counters. item.MintID derives nothing from what already exists,
// so there is no floor to scan for and no way to re-mint a used id.

// scopeFor maps a draft to its context dir: explicit dir (scaffolded on
// demand) > deepest common existing context dir of the targets > root.
func scopeFor(ws workspace.Root, dir string, targets []string) (string, error) {
	return ScopeFor(ws, dir, targets, nil)
}

// ScopeFor derives an item's context dir: an explicit dir wins, otherwise
// the deepest context enclosing all path targets.
//
// resolve maps a NODE-ID target to its file and is how node IDs reach
// their subsystem — lifecycle has no graph, so a caller that does (the
// MCP server) injects one. Without it, node IDs contribute nothing and
// an item scoped purely by nodes lands at root, which is what shipped
// until an independent gap hunt measured the consequence
// (B-01KYMCJFC3EMN): node IDs are the advertised currency, so the
// primary path was the broken one, and such items merged their archive
// delta into the WRONG intent section.
func ScopeFor(ws workspace.Root, dir string, targets []string, resolve func(string) (string, bool)) (string, error) {
	// "." is an EXPLICIT root, not an absent dir. The distinction is
	// load-bearing for a caller that already derived the scope with a
	// resolver: without it, a correctly-derived root is indistinguishable
	// from "undecided" and gets silently re-derived here WITHOUT the
	// resolver, so a mixed node+path target set lands on the path's dir
	// instead of root (cross-val-node finding 1). Everywhere else in the
	// codebase "." already means root (see dirOf).
	if dir == "." {
		return "", nil
	}
	if dir != "" {
		return strings.Trim(path.Clean(dir), "/"), nil
	}
	ctxs, err := ws.ContextDirs()
	if err != nil {
		return "", err
	}
	common := ""
	first := true
	for _, t := range targets {
		p := t
		if !strings.ContainsAny(t, "/.") || strings.Contains(t, ":") {
			// a node ID: usable only if the caller can resolve it
			if resolve == nil {
				continue
			}
			f, ok := resolve(t)
			if !ok || f == "" {
				continue
			}
			p = f
		}
		d := path.Dir(p)
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
	if b := gistLine(it); b != "" {
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
