package item

import (
	"regexp"
	"strings"
)

// reOutcome matches lifecycle.Escalate's auto-minted decision body sentence
// `... outcome=a|b|c.`. Mirrors internal/mcpserver/decide.go's identically
// named regex exactly.
var reOutcome = regexp.MustCompile(`outcome=([a-zA-Z0-9_-]+(?:\|[a-zA-Z0-9_-]+)*)`)

// ParseOptions extracts the fixed option set (if any) an adr item's Body
// records. Options are the rejected alternatives, not just the chosen
// Decision — this project's own record-keeping rules name them as knowledge
// worth keeping, so callers that carry an ADR onward (see
// internal/knowledge's Extract) need them, not just the Context/Decision/
// Consequences/Status fields item.Item already exposes.
//
// Three body shapes are understood, tried in this order:
//  1. one `option: <text>` line per option, verbatim (no comma-splitting —
//     an option's own text may contain commas). This is what NEW decisions
//     write.
//  2. the legacy `options: a, b, c` comma-joined line older decisions wrote —
//     kept forever so existing items/journals stay answerable without
//     migration (when an option's own text contains commas, the
//     comma-split fragments it into more pieces than were intended, but
//     that shattered form is exactly what remains answerable — it is not
//     rewritten to option: lines).
//  3. lifecycle.Escalate's `... outcome=a|b|c.` sentence.
//
// No match = free text (nil).
//
// This is a byte-for-byte copy of internal/mcpserver/decide.go's unexported
// decideOptions, exported here so internal/knowledge can carry ADR options
// onto its portable Entry without importing internal/mcpserver (which it
// must not: see T-0110's report on the resulting duplication — decide.go
// still has its own copy because internal/mcpserver was held by a sibling
// task at the time this function was extracted; a follow-up should delete
// decideOptions and call this instead).
func ParseOptions(body string) []string {
	var out []string
	for _, line := range strings.Split(body, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "option: "); ok {
			out = append(out, v)
		}
	}
	if len(out) > 0 {
		return out
	}
	for _, line := range strings.Split(body, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "options: "); ok {
			for _, o := range strings.Split(v, ",") {
				if o = strings.TrimSpace(o); o != "" {
					out = append(out, o)
				}
			}
			return out
		}
	}
	if m := reOutcome.FindStringSubmatch(body); m != nil {
		return strings.Split(m[1], "|")
	}
	return nil
}
