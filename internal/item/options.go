package item

import (
	"regexp"
	"strings"
)

// reOutcome matches lifecycle.Escalate's auto-minted decision body sentence in
// BOTH spellings it has used: the original `... outcome=a|b|c.` and the
// `... choose=a|b|c.` it was deliberately changed to (following the old text
// failed twice, see lifecycle.Escalate). That change updated no parser, and
// because this regex was duplicated byte-for-byte in internal/mcpserver, a
// value that had to change in two places changed in neither — so every
// escalation ADR silently accepted free text and a typo in `choose` stranded
// its item in blocked forever, reporting success (B-01KYS7111XFHZ). Both
// spellings are matched permanently: records carrying either are already in
// journals and work trees, and they must stay answerable without migration —
// the same reasoning the legacy comma-joined shape below is kept for.
//
// The duplicate in internal/mcpserver is now deleted; this is the only parser.
var reOutcome = regexp.MustCompile(`(?:outcome|choose)=([a-zA-Z0-9_-]+(?:\|[a-zA-Z0-9_-]+)*)`)

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
//  3. lifecycle.Escalate's `... outcome=a|b|c.` / `... choose=a|b|c.` sentence.
//
// No match = free text (nil).
//
// This is the SINGLE parser. It began as a copy of internal/mcpserver's
// unexported decideOptions, extracted so internal/knowledge could carry ADR
// options onto its portable Entry without importing internal/mcpserver; the
// follow-up this comment used to ask for — delete decideOptions and call this
// instead — is done, and it was not cosmetic. While both copies existed, a
// change to the body text they both parse landed in neither.
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
