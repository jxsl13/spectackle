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

// Kinds and their ID letters. decision items are minted by
// lifecycle.Escalate (see internal/lifecycle) to record the way out of a
// rounds-exhausted feedback loop — never drafted directly by an agent.
var kindLetter = map[string]string{
	"proposal": "P", "task": "T", "bug": "B", "research": "R", "decision": "D",
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
	ID      string
	Kind    string
	State   string
	Title   string
	Dir     string // context dir (repo-relative, "" = root)
	Parent  string
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
}

// IDRe matches item IDs like P-0007 or D-0007 (decision).
var IDRe = regexp.MustCompile(`^[PTBRD]-\d{4}$`)

// ValidKind reports whether k is a known item kind.
func ValidKind(k string) bool { _, ok := kindLetter[k]; return ok }

// NextID mints the next ID for a kind given the highest number seen so far
// across journal create events and active items (journal is source of truth).
func NextID(kind string, maxSeen int) string {
	return fmt.Sprintf("%s-%04d", kindLetter[kind], maxSeen+1)
}

// Num extracts the numeric part of an item ID (0 if malformed).
func Num(id string) int {
	var n int
	if _, err := fmt.Sscanf(id[2:], "%d", &n); err != nil {
		return 0
	}
	return n
}

// Letter returns the ID letter for a kind ("" if unknown).
func Letter(kind string) string { return kindLetter[kind] }

var reItemHeading = regexp.MustCompile(`^## +([PTBRD]-\d{4}) +(.+?) *$`)

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

// Record renders the dense `i` output line.
func Record(it Item) string {
	dir := it.Dir
	if dir == "" {
		dir = "."
	}
	return fmt.Sprintf("i %s %s %s %s %s", it.ID, it.Kind, it.State, dir, it.Title)
}
