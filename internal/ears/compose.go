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

	clause := func(kw, v string) string { return kw + " " + strings.TrimSpace(v) + ", " }
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
func PatternFromString(s string) Pattern {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "U":
		return PUbiquitous
	case "E":
		return PEvent
	case "S":
		return PState
	case "N":
		return PUnwanted
	case "O":
		return POptional
	case "C":
		return PComplex
	}
	return PInvalid
}
