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
// (Dir; "" = root) that asserted an Entry. Dir travels as a portability hint
// for the human curating a condensate — it is never part of merge identity.
type Provenance struct {
	Source string `yaml:"source"`
	Dir    string `yaml:"dir"`
}

// Entry is one portable unit of knowledge: an EARS rule sentence, an ADR, or
// an intent prose section — stripped of everything that cannot mean anything
// outside the repository that minted it: rule-ID prefix/number, `applies`
// anchors, lifecycle state, file paths. Sentences and prose keep their own
// words verbatim; extraction never paraphrases.
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
	// no separate "options" field: internal/item.Item's ADR template has no
	// such field, so Context (the forces/constraints behind the decision)
	// is the closest existing analog and is used as-is — see the package's
	// extract_test.go / the T-0108 report for why no field was invented.
	Question     string `yaml:"question,omitempty"`
	Context      string `yaml:"context,omitempty"`
	Decision     string `yaml:"decision,omitempty"`
	Consequences string `yaml:"consequences,omitempty"`
	Status       string `yaml:"status,omitempty"`

	// Intent payload (Kind == KindIntent): one repository's `## intent`
	// prose section, verbatim.
	Prose string `yaml:"prose,omitempty"`

	// Count is the recurrence rank: the number of distinct Sources. 1 for a
	// freshly Extracted entry; recomputed by Merge as entries combine.
	Count int `yaml:"count"`

	// Sources is the union of provenance rows asserting this entry, sorted
	// by (Source, Dir) for determinism.
	Sources []Provenance `yaml:"sources"`
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
}

// frontMatterDoc is the artifact's global YAML header.
type frontMatterDoc struct {
	Schema  string   `yaml:"schema"`
	Kind    string   `yaml:"kind"`
	Sources []string `yaml:"sources"`
}

var reSectionHeading = regexp.MustCompile(`^## (rule|adr|intent) (\S+)$`)

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

// Marshal renders an artifact as front-matter-fenced markdown. Output is
// canonicalized (entries sorted, provenance sorted, sources deduped and
// sorted) so that Marshal is a pure function of an artifact's content: two
// calls on equivalent input, however ordered, produce byte-identical output.
func Marshal(a Artifact) ([]byte, error) {
	sources := dedupSorted(a.Sources)

	entries := make([]Entry, len(a.Entries))
	copy(entries, a.Entries)
	for i := range entries {
		entries[i].Sources = append([]Provenance(nil), entries[i].Sources...)
		sortProvenance(entries[i].Sources)
	}
	sortEntries(entries)

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
	return []byte(b.String()), nil
}

// Parse reads an artifact rendered by Marshal. The schema stamp is checked
// exactly like spec.Load/item.LoadWork: a mismatch is a hard error, there is
// no migration.
func Parse(data []byte) (Artifact, error) {
	src := string(data)

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
	for i := 0; i < len(lines); i++ {
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
		e.Key = m[2]
		entries = append(entries, e)
		i = j - 1
	}
	return Artifact{Sources: dedupSorted(fm.Sources), Entries: entries}, nil
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
