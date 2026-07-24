// Package item persists lifecycle items (proposals, tasks, bugs, research)
// as `##`-anchored blocks in the per-context work.md — ACTIVE items only.
// Rejected and archived items leave work.md; their summaries live in the
// journal. One item = one block keeps merge conflicts block-local and
// work.md bounded (anti file-sprawl: no per-item files, ever).
package item

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jxsl13/spectackle/internal/ears"
	"github.com/jxsl13/spectackle/internal/workspace"
)

// Kinds and their ID letters. adr items (architecture decision records,
// formerly "decision") are minted by lifecycle.Escalate (see
// internal/lifecycle) to record the way out of a rounds-exhausted feedback
// loop — never drafted directly by an agent.
var kindLetter = map[string]string{
	"proposal": "P", "task": "T", "bug": "B", "research": "R", "adr": "ADR",
}

// States of the lifecycle state machine (see internal/lifecycle).
const (
	StateDraft     = "draft"
	StateSubmitted = "submitted"
	StateApproved  = "approved"
	StateActive    = "active"
	StateDone      = "done"
	StateArchived  = "archived"
	StateRejected  = "rejected"

	// StateBlocked is a side state, not part of the six-state main order
	// (like rejected): an item lands here only via lifecycle.Escalate, when
	// a done->active reopen would exceed the configured feedback round
	// limit. It is deliberately excluded from lifecycle's stateOrder/
	// orderedStates — Move() refuses every transition into or out of it;
	// only lifecycle.ResolveBlocked (driven by the linked decision item's
	// outcome) can move an item out of blocked.
	StateBlocked = "blocked"
)

// Item is one lifecycle item.
type Item struct {
	ID     string
	Kind   string
	State  string
	Title  string
	Dir    string // context dir (repo-relative, "" = root)
	Parent string

	// Refs is a general citation set: item IDs this item cites, of any kind
	// in either direction — research citing research, a proposal citing the
	// research that produced it, an ADR naming the research that fed it.
	// Order-preserving and duplicate-free (duplicates collapse on write).
	// It differs from both other cross-item fields: Parent is a single
	// structural owner (one task belongs to one proposal), Needs means
	// blocked-on and drives the escalation exits, while Refs carries no
	// lifecycle meaning at all — a plain citation the state machine never
	// interprets. Shape and existence are validated at the write path (see
	// UnknownRefs), never by Parse: a ref may legitimately point at an item
	// archived out of work.md.
	Refs []string

	Created string // YYYY-MM-DD
	Goal    string // optional shell command gating work-submit (benchmark/verify target)
	Targets []string
	Rules   []string
	Body    string

	// Feedback-loop / escalation fields (SDD orchestration v2).
	Rounds   int      // done->active reopen count since the last reset (rescope/override-once)
	Grilled  string   // most recent grill feedback (freeform, survives rescope)
	Needs    []string // IDs this item is blocked on (decision items minted by Escalate)
	Override bool     // override-once already spent — cannot be spent again

	// ADR template fields (architecture decision records; kind=="adr").
	// Always empty and omitted for every other kind — these are structured
	// replacements for what used to be prose in Body, not general-purpose
	// fields. Status follows the classic ADR convention proposed|accepted|
	// superseded|deprecated; an empty Status on an adr item is conventionally
	// read as "proposed".
	Context      string // the forces and constraints behind the decision
	Decision     string // the chosen option, verbatim
	Consequences string // trade-offs and follow-on effects of the decision
	Status       string // proposed|accepted|superseded|deprecated
}

// IDRe matches item IDs like P-0007 or ADR-0007 (adr). D-0007 is also
// accepted: the legacy ID letter for adr items before the decision->adr
// rename — existing D-xxxx items in .spectackle files are not migrated by
// this change, so the regex must keep reading them. New adr items are
// always minted as ADR-NNNN (see kindLetter above); D is legacy-only.
//
// It also accepts the ULID form <KIND>-<26-char Crockford base32> (see
// ulid.go), so records written before and after the switch to ULID ids
// both resolve (T-0125 / ITM-001). The character class is built from the
// same crockford alphabet constant NewULID encodes with, so the accepted
// grammar and the generator can never drift apart. Nothing mints a ULID
// id yet — internal/lifecycle's minter still calls NextID and produces
// only the legacy \d{4} form — this regex just widens what resolves.
var IDRe = regexp.MustCompile(`^(?:ADR|[PTBRD])-(?:\d{4}|[` + crockford + `]{26})$`)

// ValidKind reports whether k is a known item kind.
func ValidKind(k string) bool { _, ok := kindLetter[k]; return ok }

// NextID mints the next ID for a kind given the highest number seen so far
// across journal create events and active items (journal is source of truth).
func NextID(kind string, maxSeen int) string {
	return fmt.Sprintf("%s-%04d", kindLetter[kind], maxSeen+1)
}

// Num extracts the numeric part of an item ID (0 if malformed, and 0 for a
// ULID-form id — see below). Handles both single-letter (P-0007) and
// multi-letter (ADR-0007) prefixes.
//
// For a ULID-form id (T-0125), Num deliberately returns 0 rather than
// erroring or trying to derive a number from the ULID's timestamp bits.
// Verified against both existing callers before choosing this: maxNum in
// internal/lifecycle/lifecycle.go calls Num twice, once over journal
// events and once over active items, purely to find the highest number
// already used for a kind so NextID can mint one past it — i.e. Num's
// result is only ever compared against a running max as a floor for the
// legacy numeric sequence. A ULID id was never part of that sequence, so
// it must contribute neither a false floor nor a false ceiling; 0 is a
// correct no-op there in both call sites. A future caller that needs to
// order or compare ULID ids should sort the raw ID string instead — a
// Crockford ULID's lexicographic string order equals its creation order by
// construction (see ulid.go) — rather than reading a number out of Num.
//
// The suffix must be entirely digits: fmt.Sscanf's "%d" verb stops at the
// first non-digit rather than requiring the whole string to match, so a
// naive Sscanf-based parse would silently return a truncated number for a
// ULID suffix that happens to start with digits (most do, since the
// timestamp half sorts through the low end of the alphabet early on).
// Rejecting any non-digit byte up front avoids that trap.
func Num(id string) int {
	i := strings.IndexByte(id, '-')
	if i < 0 {
		return 0
	}
	suffix := id[i+1:]
	if suffix == "" {
		return 0
	}
	for j := 0; j < len(suffix); j++ {
		if suffix[j] < '0' || suffix[j] > '9' {
			return 0
		}
	}
	n, err := strconv.Atoi(suffix)
	if err != nil {
		return 0
	}
	return n
}

// Letter returns the ID letter for a kind ("" if unknown).
func Letter(kind string) string { return kindLetter[kind] }

// reItemHeading accepts the same two ID forms IDRe does (ITM-001), and for
// the same reason: work.md is where items round-trip, so a heading grammar
// narrower than the ID grammar would let a ULID-form item be written and
// then never read back. It shares the crockford alphabet constant with the
// generator so the two can never drift apart.
var reItemHeading = regexp.MustCompile(
	`^## +((?:ADR|[PTBRD])-(?:\d{4}|[` + crockford + `]{26})) +(.+?) *$`)

// LoadWork parses a work.md file (missing file = no items).
func LoadWork(path, ctx string) ([]Item, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	body, _ := ears.StripFrontMatter(string(raw))
	lines := strings.Split(body, "\n")
	var items []Item
	for i := 0; i < len(lines); i++ {
		m := reItemHeading.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		it := Item{ID: m[1], Title: m[2], Dir: ctx}
		j := i + 1
		// machine header: contiguous key: value lines
		for ; j < len(lines); j++ {
			k, v, ok := strings.Cut(lines[j], ": ")
			if !ok || strings.ContainsAny(k, " \t") || strings.HasPrefix(lines[j], "## ") {
				break
			}
			switch k {
			case "kind":
				it.Kind = v
			case "state":
				it.State = v
			case "created":
				it.Created = v
			case "parent":
				it.Parent = v
			case "refs":
				it.Refs = splitList(v)
			case "goal":
				it.Goal = v
			case "targets":
				it.Targets = splitList(v)
			case "rules":
				it.Rules = splitList(v)
			case "rounds":
				n, _ := strconv.Atoi(v)
				it.Rounds = n
			case "grilled":
				it.Grilled = v
			case "needs":
				it.Needs = splitList(v)
			case "override":
				it.Override = v == "true"
			case "context":
				it.Context = v
			case "decision":
				it.Decision = v
			case "consequences":
				it.Consequences = v
			case "status":
				it.Status = v
			}
		}
		// body: until next item heading
		var b []string
		for ; j < len(lines); j++ {
			if reItemHeading.MatchString(lines[j]) {
				break
			}
			b = append(b, lines[j])
		}
		it.Body = strings.TrimSpace(strings.Join(b, "\n"))
		items = append(items, it)
		i = j - 1
	}
	return items, nil
}

// LoadAll returns all active items of the workspace.
func LoadAll(root workspace.Root) ([]Item, error) {
	ctxs, err := root.ContextDirs()
	if err != nil {
		return nil, err
	}
	var out []Item
	for _, c := range ctxs {
		its, err := LoadWork(root.WorkPath(c), c)
		if err != nil {
			return nil, err
		}
		out = append(out, its...)
	}
	return out, nil
}

// Get finds one item by ID.
func Get(root workspace.Root, id string) (Item, bool, error) {
	its, err := LoadAll(root)
	if err != nil {
		return Item{}, false, err
	}
	for _, it := range its {
		if it.ID == id {
			return it, true, nil
		}
	}
	return Item{}, false, nil
}

// Upsert writes an item block into its context's work.md (replacing an
// existing block with the same ID) and injects the schema frontmatter.
func Upsert(root workspace.Root, it Item) error {
	if it.Created == "" {
		it.Created = time.Now().UTC().Format("2006-01-02")
	}
	items, err := LoadWork(root.WorkPath(it.Dir), it.Dir)
	if err != nil {
		return err
	}
	replaced := false
	for i := range items {
		if items[i].ID == it.ID {
			items[i] = it
			replaced = true
		}
	}
	if !replaced {
		items = append(items, it)
	}
	return writeWork(root, it.Dir, items)
}

// Remove deletes an item block from its context's work.md.
func Remove(root workspace.Root, it Item) error {
	items, err := LoadWork(root.WorkPath(it.Dir), it.Dir)
	if err != nil {
		return err
	}
	var out []Item
	for _, x := range items {
		if x.ID != it.ID {
			out = append(out, x)
		}
	}
	return writeWork(root, it.Dir, out)
}

func writeWork(root workspace.Root, ctx string, items []Item) error {
	if err := root.EnsureScaffold(ctx); err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("---\nschema: " + workspace.SchemaStamp + "\n---\n")
	for _, it := range items {
		b.WriteString("\n## " + it.ID + " " + it.Title + "\n")
		b.WriteString("kind: " + it.Kind + "\n")
		b.WriteString("state: " + it.State + "\n")
		b.WriteString("created: " + it.Created + "\n")
		if it.Parent != "" {
			b.WriteString("parent: " + it.Parent + "\n")
		}
		if refs := dedupeStrings(it.Refs); len(refs) > 0 {
			b.WriteString("refs: " + strings.Join(refs, ", ") + "\n")
		}
		if it.Goal != "" {
			b.WriteString("goal: " + it.Goal + "\n")
		}
		if it.Rounds != 0 {
			b.WriteString("rounds: " + strconv.Itoa(it.Rounds) + "\n")
		}
		if it.Grilled != "" {
			b.WriteString("grilled: " + it.Grilled + "\n")
		}
		if len(it.Needs) > 0 {
			b.WriteString("needs: " + strings.Join(it.Needs, ", ") + "\n")
		}
		if it.Override {
			b.WriteString("override: true\n")
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
		if len(it.Targets) > 0 {
			b.WriteString("targets: " + strings.Join(it.Targets, ", ") + "\n")
		}
		if len(it.Rules) > 0 {
			b.WriteString("rules: " + strings.Join(it.Rules, ", ") + "\n")
		}
		if it.Body != "" {
			b.WriteString("\n" + it.Body + "\n")
		}
	}
	return os.WriteFile(root.WorkPath(ctx), []byte(b.String()), 0o644)
}

func splitList(v string) []string {
	var out []string
	for _, s := range strings.Split(v, ",") {
		if s = strings.TrimSpace(s); s != "" && s != "-" {
			out = append(out, s)
		}
	}
	return out
}

// dedupeStrings returns ss with duplicates removed, keeping the order of
// first appearance. Used when rendering Refs so accidental repeats in a
// proposed reference set don't get written twice.
func dedupeStrings(ss []string) []string {
	if len(ss) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(ss))
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// UnknownRefs validates a proposed reference set for item selfID against
// known, the set of currently loadable item IDs, and reports — in refs'
// input order — every entry that is unusable as a citation: malformed (does
// not match IDRe), self-referential (equal to selfID, always a mistake), or
// absent from known. An entry failing more than one check is reported once.
//
// UnknownRefs is deliberately not called from Parse: a work.md may
// legitimately cite an item that has since been archived out of work.md,
// and a parser that refused to load such a file would make a dangling
// citation unrecoverable. Validation belongs at the write path, which
// should call UnknownRefs before persisting and reject or warn on a
// non-empty result.
func UnknownRefs(selfID string, refs []string, known map[string]bool) []string {
	var out []string
	for _, r := range refs {
		if !IDRe.MatchString(r) || r == selfID || !known[r] {
			out = append(out, r)
		}
	}
	return out
}

// Record renders the dense `i` output line.
func Record(it Item) string {
	dir := it.Dir
	if dir == "" {
		dir = "."
	}
	return fmt.Sprintf("i %s %s %s %s %s", it.ID, it.Kind, it.State, dir, it.Title)
}
