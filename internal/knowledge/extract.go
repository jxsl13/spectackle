package knowledge

import (
	"strings"

	"github.com/jxsl13/spectackle/internal/drift"
	"github.com/jxsl13/spectackle/internal/item"
	"github.com/jxsl13/spectackle/internal/spec"
)

// Extract lifts one workspace's reusable knowledge into a portable artifact:
// every EARS rule and `## intent` prose section in the cascade, and every
// ADR among items (proposals/tasks/bugs/research describe transient,
// repository-local state and are never included — including them would
// drown the condensate in noise no other repository can use).
//
// source is the repository label recorded as provenance on every entry (the
// caller passes e.g. the module path); it is also the artifact's sole
// top-level Sources entry.
//
// Extraction does not paraphrase: rule sentences and intent prose travel
// verbatim. What is stripped is exactly what cannot mean anything outside
// this repository — rule-ID prefix/number, `applies` node-ID anchors,
// lifecycle state, file paths — by simply never copying those fields into
// Entry in the first place.
func Extract(c *spec.Cascade, items []item.Item, source string) (Artifact, error) {
	var entries []Entry

	for _, sf := range c.All() {
		for _, r := range sf.Rules {
			text := strings.TrimSpace(r.Text)
			if text == "" {
				continue // structurally incomplete rule (see ears E002); nothing to carry
			}
			entries = append(entries, Entry{
				Kind:      KindRule,
				Text:      text,
				Rationale: strings.TrimSpace(r.Rationale),
				Count:     1,
				Sources:   []Provenance{{Source: source, Dir: sf.Dir}},
				Key:       drift.NormHash([]byte(text)),
			})
		}
		for _, s := range sf.Sections {
			if s.Name != "intent" {
				continue // only intent prose travels; notes/design/context stay local
			}
			prose := strings.TrimSpace(s.Text)
			if prose == "" {
				continue
			}
			entries = append(entries, Entry{
				Kind:    KindIntent,
				Prose:   prose,
				Count:   1,
				Sources: []Provenance{{Source: source, Dir: sf.Dir}},
				Key:     drift.NormHash([]byte(prose)),
			})
		}
	}

	for _, it := range items {
		if it.Kind != "adr" {
			continue // proposals/tasks/bugs/research: transient, repository-local, not knowledge
		}
		question := strings.TrimSpace(it.Title)
		if question == "" {
			continue
		}
		entries = append(entries, Entry{
			Kind:         KindADR,
			Question:     question,
			Context:      strings.TrimSpace(it.Context),
			Decision:     strings.TrimSpace(it.Decision),
			Consequences: strings.TrimSpace(it.Consequences),
			Status:       strings.TrimSpace(it.Status),
			Count:        1,
			Sources:      []Provenance{{Source: source, Dir: it.Dir}},
			Key:          drift.NormHash([]byte(question)),
		})
	}

	sortEntries(entries)
	return Artifact{Sources: []string{source}, Entries: entries}, nil
}
