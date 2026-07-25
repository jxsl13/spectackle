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
	"github.com/jxsl13/spectackle/internal/ids"
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

// kindOfLetter is kindLetter reversed: the kind an ID prefix names. D is the
// legacy ID letter for adr items (see kindLetter above) and maps to adr like
// ADR does, so KindOf answers for records minted before the decision->adr
// rename as well.
var kindOfLetter = map[string]string{
	"P": "proposal", "T": "task", "B": "bug", "R": "research",
	"ADR": "adr", "D": "adr",
}

// The item ID grammar, in two halves that IDRe accepts equally.
//
// legacyIDPat is the sequential scheme every record minted before ADR-0013
// carries: a kind prefix and a four-digit counter, e.g. P-0007 or ADR-0007.
// It is NOT a deprecation window that closes later — it must keep parsing for
// as long as this program exists. Archived records leave work.md entirely and
// survive only as journal tombstones that lifecycle.Tombstone finds by exact
// ID (see internal/lifecycle), and the tool boundary refuses an ID that does
// not match IDRe before it ever reaches the tombstone lookup. The day a
// legacy ID stops matching is the day the archived half of this repository's
// history becomes unreachable.
//
// recordIDPat is the ADR-0013 scheme minted from here on: the same kind
// prefix followed by internal/ids' canonical record ID — a UUIDv7 in
// Crockford base32. The character class is that alphabet (digits plus the
// uppercase letters minus the confusable I, L, O and U); the first character
// encodes only the top 3 bits of a 128-bit value and so never exceeds '7',
// exactly as ids.ParseRecordID enforces. The length comes from
// ids.RecordIDLen rather than a literal so the two cannot drift apart.
var (
	legacyIDPat = `(?:ADR|[PTBRD])-[0-9]{4}`
	recordIDPat = `(?:ADR|[PTBRD])-[0-7][0-9A-HJKMNP-TV-Z]{` + strconv.Itoa(ids.RecordIDLen-1) + `}`
	idPat       = `(?:` + legacyIDPat + `|` + recordIDPat + `)`
)

// IDRe matches an item ID of either scheme: the legacy sequential form
// (P-0007, ADR-0007, and the legacy adr letter D-0007) and the ADR-0013
// kind-prefixed record ID (T-01K9XJ...). Both are accepted permanently — see
// the pattern doc above for why legacy IDs can never be dropped.
var IDRe = regexp.MustCompile(`^` + idPat + `$`)

// LegacyIDRe matches ONLY the pre-ADR-0013 sequential form. It exists to tell
// the two schemes apart — a migration deciding what still needs rewriting, a
// caller reporting which scheme a record predates. Nothing may use it to
// reject an ID: IDRe is the acceptance test, and it accepts both.
var LegacyIDRe = regexp.MustCompile(`^` + legacyIDPat + `$`)

// ValidKind reports whether k is a known item kind.
func ValidKind(k string) bool { _, ok := kindLetter[k]; return ok }

// MintID mints a fresh ID for a kind, stamped with the current time.
//
// Per ADR-0013 the number no longer comes from a counter. coord.NextID is
// serialized per machine but its database is gitignored, so two independent
// clones drafting concurrently minted the same ID and replay silently
// collapsed the two records into one (reproduced in R-0006). A UUIDv7 needs
// no coordination at all to be unique, and because it leads with a
// millisecond timestamp the canonical encoding also sorts by mint time.
// Returns "" for an unknown kind, matching Letter.
func MintID(kind string) string { return MintIDAt(kind, time.Now()) }

// MintIDAt mints an ID for a kind stamped with ts instead of the clock. A
// migration rewriting historical records mints from each record's own
// creation time so the archive keeps its chronology instead of collapsing
// into the migration moment; tests use it for deterministic same-millisecond
// batches.
func MintIDAt(kind string, ts time.Time) string {
	l := kindLetter[kind]
	if l == "" {
		return ""
	}
	return l + "-" + ids.MintRecordIDAt(ts).String()
}

// Num extracts the counter of a LEGACY sequential item ID (P-0007 -> 7),
// handling both single-letter and multi-letter (ADR-0007) prefixes. It
// returns 0 for anything else, record IDs included: an ADR-0013 ID has no
// counter, and a record ID whose base32 tail happens to open with digits
// would otherwise yield a meaningless number that a caller could mistake for
// a real one.
func Num(id string) int {
	if !LegacyIDRe.MatchString(id) {
		return 0
	}
	i := strings.IndexByte(id, '-')
	var n int
	if _, err := fmt.Sscanf(id[i+1:], "%d", &n); err != nil {
		return 0
	}
	return n
}

// Letter returns the ID letter for a kind ("" if unknown).
func Letter(kind string) string { return kindLetter[kind] }

// KindOf returns the kind an item ID names ("" if the ID is malformed).
// It reads the prefix, which both schemes share, so a legacy P-0007 and a
// record-ID P-01K9XJ... both answer "proposal" — callers deriving a kind from
// an ID must go through here rather than testing prefixes by hand, which is
// how the four-digit shape leaked into helpers in the first place.
func KindOf(id string) string {
	if !IDRe.MatchString(id) {
		return ""
	}
	prefix, _, _ := strings.Cut(id, "-")
	return kindOfLetter[prefix]
}

// reItemHeading matches a work.md item block heading. It shares idPat with
// IDRe so a shape one accepts is never a shape the other silently drops on
// the floor — that divergence would make a whole work.md unreadable.
var reItemHeading = regexp.MustCompile(`^## +(` + idPat + `) +(.+?) *$`)

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
