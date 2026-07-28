// Package knowledge lifts the reusable part of one repository's spec/item
// corpus into a portable artifact, and condenses several such artifacts into
// one. It does not paraphrase or generalize prose: an EARS sentence, an ADR's
// fields, or an intent paragraph travel byte-for-byte as the repository wrote
// them. Genericity is measured, not inferred — see Merge's recurrence rank.
//
// This package only produces and reads the artifact format. Wiring it to a
// CLI subcommand or MCP tool, and an "apply to a target repo" step, are
// deliberately out of scope (a later task), the same way internal/mcpclient
// landed before its subcommand.
package knowledge

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/jxsl13/spectackle/internal/drift"
	"github.com/jxsl13/spectackle/internal/ears"
	"github.com/jxsl13/spectackle/internal/workspace"
)

// EntryKind identifies which of the three portable shapes an Entry carries.
// Only one of Entry's payload groups is populated, matching Kind.
type EntryKind string

const (
	KindRule   EntryKind = "rule"
	KindADR    EntryKind = "adr"
	KindIntent EntryKind = "intent"
)

// kindDoc is the front matter's `kind:` value, distinguishing a knowledge
// artifact from spec.md/work.md at a glance (they share the same
// front-matter-fenced markdown family, per workspace.SchemaStamp).
const kindDoc = "knowledge"

// Provenance names one repository (Source) and, within it, the context dir
// (Dir; "" = root) that stands behind an Entry — either because that
// repository's own spec/item files literally assert it (Entry.Sources), or
// because an LLM drew on it while generalizing an entry that has no single
// asserting repository (Entry.DerivedFrom). Dir travels as a portability
// hint for the human curating a condensate — it is never part of merge
// identity.
type Provenance struct {
	Source string `yaml:"source"`
	Dir    string `yaml:"dir"`
}

// Entry is one portable unit of knowledge: an EARS rule sentence, an ADR, or
// a prose section from ears.IsProseSection's whitelist — stripped of
// everything that cannot mean anything outside the repository that minted
// it: rule-ID prefix/number, `applies` anchors, lifecycle state, file paths.
// Sentences and prose keep their own words verbatim; this package never
// paraphrases. An Entry may instead be LLM-authored — a generalization over
// several repositories rather than a copy of any one of them — but it still
// never rewrites a sentence itself; see NewEntry.
type Entry struct {
	// Kind and Key are carried by the section heading on the wire (see
	// Marshal/Parse), not by YAML body fields — they identify the entry, the
	// payload below is its content.
	Kind EntryKind `yaml:"-"`
	Key  string    `yaml:"-"`

	// Rule payload (Kind == KindRule).
	Text      string `yaml:"text,omitempty"`
	Rationale string `yaml:"rationale,omitempty"`

	// ADR payload (Kind == KindADR). Question is the item's Title. There is
	// no separate "options" field on internal/item.Item — options live as
	// `option:`/`options:` body lines (see item.ParseOptions) — but they are
	// the rejected alternatives an ADR exists to preserve, so they travel
	// here once parsed out of the item's Body.
	Question     string   `yaml:"question,omitempty"`
	Context      string   `yaml:"context,omitempty"`
	Decision     string   `yaml:"decision,omitempty"`
	Consequences string   `yaml:"consequences,omitempty"`
	Status       string   `yaml:"status,omitempty"`
	Options      []string `yaml:"options,omitempty"`

	// Intent payload (Kind == KindIntent): one repository's prose section —
	// any of ears.IsProseSection's whitelist (intent, notes, design,
	// context), not only `## intent` — verbatim.
	Prose string `yaml:"prose,omitempty"`

	// Count is the recurrence rank: the number of distinct repositories
	// backing this entry, pooling Sources and DerivedFrom — both are
	// evidence of how widely a convention holds, whether a repository
	// asserts the entry itself or was one of the inputs an LLM generalized
	// it from. 1 for a freshly Extracted entry; recomputed by Merge (and by
	// NewEntry) as entries combine.
	Count int `yaml:"count"`

	// Sources is the union of provenance rows that literally assert this
	// entry (asserted-by): that repository's own spec/item files contain
	// this text. Sorted by (Source, Dir) for determinism. May be empty for
	// a purely LLM-authored generalization — see DerivedFrom.
	Sources []Provenance `yaml:"sources,omitempty"`

	// DerivedFrom is the union of provenance rows an LLM drew on to
	// generalize this entry (derived-from): those repositories fed the
	// generalization, but none of them necessarily contains this exact
	// text. Disjoint in meaning from Sources — an asserted fact and a
	// generalization drawn from facts are not the same kind of evidence,
	// even though both count toward Count — and kept as a separate field
	// (never merged into Sources) so the distinction survives the wire.
	// Sorted by (Source, Dir) for determinism.
	DerivedFrom []Provenance `yaml:"derived_from,omitempty"`
}

// Artifact is a portable snapshot of one repository's reusable knowledge (as
// produced by Extract), or the condensate Merge produces from several. It
// shares spec.md's format family — YAML front matter plus `##`-headed
// markdown sections — because a condensate exists to be read and curated by
// a human before it is applied to other repositories; an opaque encoding
// would defeat that.
type Artifact struct {
	// Sources lists every repository label (as passed to Extract) reflected
	// somewhere in Entries, sorted. One element for a freshly Extracted
	// artifact, the union of inputs after Merge.
	Sources []string
	Entries []Entry

	// Resolutions records how a Merge Conflict was settled: which identity
	// was in conflict, which value won, and which values lost — see
	// Resolution's doc. Losing values are not deleted, only demoted out of
	// Entries; they remain fully present here. Empty for an artifact that
	// has never had a conflict resolved.
	Resolutions []Resolution
}

// frontMatterDoc is the artifact's global YAML header.
type frontMatterDoc struct {
	Schema  string   `yaml:"schema"`
	Kind    string   `yaml:"kind"`
	Sources []string `yaml:"sources"`
}

var reSectionHeading = regexp.MustCompile(`^## (rule|adr|intent) (\S+)$`)

// reResolutionHeading matches a resolved-conflict section, distinct from an
// ordinary entry section (reSectionHeading) by the literal "resolution"
// token — the two regexes are disjoint by construction, so a line matches
// at most one of them.
var reResolutionHeading = regexp.MustCompile(`^## resolution (rule|adr|intent) (\S+)$`)

// sortEntries canonicalizes entry order: by Kind, then by recurrence Count
// descending, then by content Key ascending. When every entry has the same
// Count (e.g. a freshly Extracted artifact, where Count is always 1) this
// reduces exactly to "by kind, then content key" — the format's baseline
// determinism guarantee. Merge's recurrence ranking is the same order with
// Count doing real work as a primary-within-kind key, so a merged
// condensate reads as a human-ranked list without fighting the format's
// general ordering rule.
func sortEntries(entries []Entry) {
	sort.SliceStable(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.Count != b.Count {
			return a.Count > b.Count
		}
		return a.Key < b.Key
	})
}

func sortProvenance(ps []Provenance) {
	sort.Slice(ps, func(i, j int) bool {
		if ps[i].Source != ps[j].Source {
			return ps[i].Source < ps[j].Source
		}
		return ps[i].Dir < ps[j].Dir
	})
}

// sortResolutions canonicalizes resolution order the same way sortEntries
// does for entries: by Kind, then by content Key ascending. There is no
// Count-based ordering here — a Resolution isn't ranked, it's a settled
// fact about one identity.
func sortResolutions(rs []Resolution) {
	sort.SliceStable(rs, func(i, j int) bool {
		if rs[i].Kind != rs[j].Kind {
			return rs[i].Kind < rs[j].Kind
		}
		return rs[i].Key < rs[j].Key
	})
}

// canonicalizeEntry returns a copy of e with Sources and DerivedFrom sorted
// (and copied, never aliasing the caller's backing array) — the same
// per-entry canonicalization Marshal already applied to Entries, reused for
// Resolution.Winner/Losers so the whole wire format has one determinism
// rule, not two.
func canonicalizeEntry(e Entry) Entry {
	e.Sources = append([]Provenance(nil), e.Sources...)
	sortProvenance(e.Sources)
	e.DerivedFrom = append([]Provenance(nil), e.DerivedFrom...)
	sortProvenance(e.DerivedFrom)
	return e
}

// Marshal renders an artifact as front-matter-fenced markdown. Output is
// canonicalized (entries sorted, provenance sorted, sources deduped and
// sorted) so that Marshal is a pure function of an artifact's content: two
// calls on equivalent input, however ordered, produce byte-identical output.
func Marshal(a Artifact) ([]byte, error) {
	sources := dedupSorted(a.Sources)

	entries := make([]Entry, len(a.Entries))
	copy(entries, a.Entries)
	for i := range entries {
		entries[i] = canonicalizeEntry(entries[i])
	}
	sortEntries(entries)

	resolutions := make([]Resolution, len(a.Resolutions))
	copy(resolutions, a.Resolutions)
	for i := range resolutions {
		resolutions[i].Winner = canonicalizeEntry(resolutions[i].Winner)
		losers := make([]Entry, len(resolutions[i].Losers))
		for j, l := range resolutions[i].Losers {
			losers[j] = canonicalizeEntry(l)
		}
		resolutions[i].Losers = losers
	}
	sortResolutions(resolutions)

	fm := frontMatterDoc{Schema: workspace.SchemaStamp, Kind: kindDoc, Sources: sources}
	fmBytes, err := yaml.Marshal(fm)
	if err != nil {
		return nil, fmt.Errorf("knowledge: marshal front matter: %w", err)
	}

	var b strings.Builder
	b.WriteString("---\n")
	b.Write(fmBytes)
	b.WriteString("---\n")
	for _, e := range entries {
		fmt.Fprintf(&b, "\n## %s %s\n", e.Kind, e.Key)
		body, err := yaml.Marshal(e)
		if err != nil {
			return nil, fmt.Errorf("knowledge: marshal entry %s %s: %w", e.Kind, e.Key, err)
		}
		b.Write(body)
	}
	for _, r := range resolutions {
		fmt.Fprintf(&b, "\n## resolution %s %s\n", r.Kind, r.Key)
		doc := resolutionDoc{Winner: r.Winner, Losers: r.Losers}
		body, err := yaml.Marshal(doc)
		if err != nil {
			return nil, fmt.Errorf("knowledge: marshal resolution %s %s: %w", r.Kind, r.Key, err)
		}
		b.Write(body)
	}
	return []byte(b.String()), nil
}

// resolutionDoc is a Resolution's body on the wire: Kind/Key are carried by
// the section heading (matching Entry's own Kind/Key convention), so only
// Winner and Losers are YAML fields.
type resolutionDoc struct {
	Winner Entry   `yaml:"winner"`
	Losers []Entry `yaml:"losers"`
}

// Parse reads an artifact rendered by Marshal. The schema stamp is checked
// exactly like spec.Load/item.LoadWork: a mismatch is a hard error, there is
// no migration.
func Parse(data []byte) (Artifact, error) {
	src := string(data)

	// An empty file is diagnosed as empty, not as a schema mismatch: the
	// version complaint sent a gap hunt looking for a regeneration
	// problem that did not exist (B-01KYMCJG8HFYW).
	if strings.TrimSpace(src) == "" {
		return Artifact{}, fmt.Errorf("knowledge: empty artifact — nothing to parse")
	}
	var fm frontMatterDoc
	if raw := ears.FrontMatter(src); raw != "" {
		if err := yaml.Unmarshal([]byte(raw), &fm); err != nil {
			return Artifact{}, fmt.Errorf("knowledge: front matter: %w", err)
		}
	}
	if fm.Schema != workspace.SchemaStamp {
		return Artifact{}, fmt.Errorf("knowledge: schema %q != %q — regenerate, there is no migration", fm.Schema, workspace.SchemaStamp)
	}
	if fm.Kind != kindDoc {
		return Artifact{}, fmt.Errorf("knowledge: front matter kind %q != %q", fm.Kind, kindDoc)
	}

	body, _ := ears.StripFrontMatter(src)
	lines := strings.Split(body, "\n")
	var entries []Entry
	var resolutions []Resolution
	for i := 0; i < len(lines); i++ {
		if m := reResolutionHeading.FindStringSubmatch(lines[i]); m != nil {
			j := i + 1
			for ; j < len(lines) && !strings.HasPrefix(lines[j], "## "); j++ {
			}
			var doc resolutionDoc
			if err := yaml.Unmarshal([]byte(strings.Join(lines[i+1:j], "\n")), &doc); err != nil {
				return Artifact{}, fmt.Errorf("knowledge: resolution %s %s: %w", m[1], m[2], err)
			}
			doc.Winner.Kind = EntryKind(m[1])
			doc.Winner.Key = m[2]
			for k := range doc.Losers {
				doc.Losers[k].Kind = EntryKind(m[1])
				doc.Losers[k].Key = m[2]
			}
			resolutions = append(resolutions, Resolution{Kind: EntryKind(m[1]), Key: m[2], Winner: doc.Winner, Losers: doc.Losers})
			i = j - 1
			continue
		}
		m := reSectionHeading.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		j := i + 1
		for ; j < len(lines) && !strings.HasPrefix(lines[j], "## "); j++ {
		}
		var e Entry
		if err := yaml.Unmarshal([]byte(strings.Join(lines[i+1:j], "\n")), &e); err != nil {
			return Artifact{}, fmt.Errorf("knowledge: entry %s %s: %w", m[1], m[2], err)
		}
		e.Kind = EntryKind(m[1])
		// The key on the wire is a transport detail, NOT an identity a
		// reader may trust. NewEntry refuses a caller-supplied key outright
		// ("a caller-chosen key would break dedup") and Extract derives it
		// from content — but Parse used to take the section heading
		// verbatim, so every downstream identity check (FoldInto's dedup,
		// Merge's bucketing, apply's duplicate-decision suppression) was
		// only as sound as whatever the producing repository happened to
		// write. A hand-authored artifact, or one from an older or newer
		// key scheme, therefore re-added the same rule on every apply and
		// re-asked a question already answered. Recompute here so identity
		// means one thing everywhere: the content.
		if k := contentKey(e); k != "" {
			e.Key = k
		} else {
			// no identifying payload to hash (a structurally incomplete
			// entry); keep the wire key rather than collapsing every
			// such entry onto one empty identity
			e.Key = m[2]
		}
		entries = append(entries, e)
		i = j - 1
	}
	return Artifact{Sources: dedupSorted(fm.Sources), Entries: entries, Resolutions: resolutions}, nil
}

// contentKey is a knowledge entry's identity, and the single definition of
// it: the normalized hash of the one field that says what the entry IS —
// a rule's text, a decision's question, an intent's prose. Everything that
// compares entries across repositories (FoldInto's dedup, Merge's
// bucketing, apply's duplicate-decision suppression) compares this, which
// is what lets the same sentence arriving from two sources, or an
// LLM-authored generalization and an extracted rule that agree, land in
// one bucket. It returns "" when the identifying field is empty, i.e. when
// the entry carries nothing to be identified by.
func contentKey(e Entry) string {
	var identity string
	switch e.Kind {
	case KindRule:
		identity = strings.TrimSpace(e.Text)
	case KindADR:
		identity = strings.TrimSpace(e.Question)
	case KindIntent:
		identity = strings.TrimSpace(e.Prose)
	}
	if identity == "" {
		return ""
	}
	return drift.NormHash([]byte(identity))
}

// NewEntry builds and validates an entry the package itself did not lift
// out of a repository's own files — chiefly an LLM-authored generalization,
// but the same entry point works for any caller-supplied entry. It gives
// that entry the same treatment Extract gives an extracted one:
//
//   - the content key is computed here, from payload, the same way Extract
//     computes it (drift.NormHash of Text/Question/Prose depending on
//     kind) — this is what makes an LLM-authored entry merge with an
//     Extracted one that says the same thing. There is no Key parameter:
//     a caller cannot supply one even by mistake, because a caller-chosen
//     key would break dedup.
//   - kind is validated against the three known EntryKinds.
//   - the payload fields required for that kind must be non-empty after
//     trimming: Text for a rule, Question and Decision for an ADR, Prose
//     for intent. A malformed entry is rejected outright, not silently
//     coerced into something plausible — this package still never
//     paraphrases or invents prose.
//
// Only the fields relevant to kind are read from payload (its Kind, Key,
// Count, Sources and DerivedFrom are ignored; pass assertedBy/derivedFrom
// instead). assertedBy is Sources (this repository's files literally
// contain this text); derivedFrom is DerivedFrom (repositories an LLM drew
// on to generalize this entry). At least one of the two must be non-empty:
// an entry backed by no repository at all is not knowledge, it is a
// hallucination.
func NewEntry(kind EntryKind, payload Entry, assertedBy, derivedFrom []Provenance) (Entry, error) {
	if len(assertedBy) == 0 && len(derivedFrom) == 0 {
		return Entry{}, fmt.Errorf("knowledge: entry requires at least one of assertedBy or derivedFrom")
	}

	var e Entry
	switch kind {
	case KindRule:
		text := strings.TrimSpace(payload.Text)
		if text == "" {
			return Entry{}, fmt.Errorf("knowledge: rule entry requires non-empty text")
		}
		e = Entry{
			Kind:      KindRule,
			Text:      text,
			Rationale: strings.TrimSpace(payload.Rationale),
		}
	case KindADR:
		question := strings.TrimSpace(payload.Question)
		if question == "" {
			return Entry{}, fmt.Errorf("knowledge: adr entry requires non-empty question")
		}
		decision := strings.TrimSpace(payload.Decision)
		if decision == "" {
			return Entry{}, fmt.Errorf("knowledge: adr entry requires non-empty decision")
		}
		var options []string
		if len(payload.Options) > 0 {
			options = append([]string(nil), payload.Options...)
		}
		e = Entry{
			Kind:         KindADR,
			Question:     question,
			Context:      strings.TrimSpace(payload.Context),
			Decision:     decision,
			Consequences: strings.TrimSpace(payload.Consequences),
			Status:       strings.TrimSpace(payload.Status),
			Options:      options,
		}
	case KindIntent:
		prose := strings.TrimSpace(payload.Prose)
		if prose == "" {
			return Entry{}, fmt.Errorf("knowledge: intent entry requires non-empty prose")
		}
		e = Entry{
			Kind:  KindIntent,
			Prose: prose,
		}
	default:
		return Entry{}, fmt.Errorf("knowledge: unknown entry kind %q", kind)
	}

	e.Key = contentKey(e)
	e.Sources = unionProvenance(nil, assertedBy)
	e.DerivedFrom = unionProvenance(nil, derivedFrom)
	e.Count = distinctSourceCount(e.Sources, e.DerivedFrom)
	return e, nil
}

// dedupSorted returns a sorted copy of ss with duplicates removed.
func dedupSorted(ss []string) []string {
	if len(ss) == 0 {
		return nil
	}
	set := make(map[string]bool, len(ss))
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		if !set[s] {
			set[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}
