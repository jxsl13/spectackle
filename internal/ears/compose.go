package ears

import (
	"fmt"
	"strings"
)

// Slots are the structured inputs a rule is composed from. End users and
// agents never hand-write EARS sentences; they fill slots (via the add_rule
// MCP tool, elicitation-guided) and the server composes and lints the
// sentence deterministically.
type Slots struct {
	System    string // "the <system>" subject; leading "the " is stripped
	Response  string // what the system SHALL do (must name something verifiable)
	Trigger   string // WHEN … (Event-driven, Complex)
	State     string // WHILE … (State-driven, Complex)
	Condition string // IF … (Unwanted behaviour, Complex)
	Feature   string // WHERE … (Optional feature, Complex)
}

// MissingSlots returns the slot names still required to compose a rule of
// pattern p. An empty result means Compose will succeed.
func MissingSlots(p Pattern, s Slots) []string {
	var missing []string
	need := func(name, v string) {
		if strings.TrimSpace(v) == "" {
			missing = append(missing, name)
		}
	}
	need("system", s.System)
	need("response", s.Response)
	switch p {
	case PEvent:
		need("trigger", s.Trigger)
	case PState:
		need("state", s.State)
	case PUnwanted:
		need("condition", s.Condition)
	case POptional:
		need("feature", s.Feature)
	case PComplex:
		n := 0
		for _, v := range []string{s.Trigger, s.State, s.Condition, s.Feature} {
			if strings.TrimSpace(v) != "" {
				n++
			}
		}
		if n < 2 {
			missing = append(missing, "two of: trigger,state,condition,feature")
		}
	}
	return missing
}

// Compose builds the canonical EARS sentence for pattern p from slots.
// Clause order for Complex follows the EARS convention:
// WHERE, WHILE, WHEN/IF, response.
func Compose(p Pattern, s Slots) (string, error) {
	if m := MissingSlots(p, s); len(m) > 0 {
		return "", fmt.Errorf("ears: missing slots for pattern %s: %s", p, strings.Join(m, ", "))
	}
	system := strings.TrimSpace(s.System)
	system = strings.TrimPrefix(system, "the ")
	system = strings.TrimPrefix(system, "The ")
	response := strings.TrimSuffix(strings.TrimSpace(s.Response), ".")
	core := "the " + system + " SHALL " + response + "."

	// A slot value that already leads with its own keyword ("WHEN x
	// happens") would compose a doubled keyword — seven live rules carried
	// WHEN WHEN before this normalization (B-01KYFSZ7). Strip the clause's
	// own keyword case-insensitively; other keywords are left alone (a
	// trigger legitimately containing "if" mid-sentence must survive).
	clause := func(kw, v string) string {
		v = strings.TrimSpace(v)
		if len(v) > len(kw) && strings.EqualFold(v[:len(kw)], kw) && v[len(kw)] == ' ' {
			v = strings.TrimSpace(v[len(kw):])
		}
		return kw + " " + v + ", "
	}
	switch p {
	case PUbiquitous:
		return "The " + system + " SHALL " + response + ".", nil
	case PEvent:
		return clause("WHEN", s.Trigger) + core, nil
	case PState:
		return clause("WHILE", s.State) + core, nil
	case PUnwanted:
		return clause("IF", s.Condition) + "THEN " + core, nil
	case POptional:
		return clause("WHERE", s.Feature) + core, nil
	case PComplex:
		var b strings.Builder
		if strings.TrimSpace(s.Feature) != "" {
			b.WriteString(clause("WHERE", s.Feature))
		}
		if strings.TrimSpace(s.State) != "" {
			b.WriteString(clause("WHILE", s.State))
		}
		switch {
		case strings.TrimSpace(s.Trigger) != "":
			b.WriteString(clause("WHEN", s.Trigger))
			b.WriteString(core)
		case strings.TrimSpace(s.Condition) != "":
			b.WriteString(clause("IF", s.Condition))
			b.WriteString("THEN " + core)
		default:
			b.WriteString(core)
		}
		return b.String(), nil
	}
	return "", fmt.Errorf("ears: cannot compose pattern %s", p)
}

// PatternFromString maps the one-letter tool encoding to a Pattern.
//
// It SCANS patternNames rather than re-listing the letters in a switch: the
// switch was a second copy of the pattern set, so a seventh pattern added to
// patternNames parsed as PInvalid while String() happily rendered it
// (T-01KYT2EHRMEAH). Index 0 is skipped because it is the "?" sentinel for
// PInvalid — accepting it would let a caller name the not-a-pattern value.
//
// EqualFold preserves the documented lowercase acceptance the switch had via
// ToUpper (compose_test.go pins "e" and "c"), and TrimSpace keeps " x " and
// other padded junk failing.
func PatternFromString(s string) Pattern {
	s = strings.TrimSpace(s)
	for i := 1; i < len(patternNames); i++ {
		if strings.EqualFold(s, patternNames[i]) {
			return Pattern(i)
		}
	}
	return PInvalid
}
