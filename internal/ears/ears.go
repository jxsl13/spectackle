// Package ears implements the semantic pillar: classification and linting of
// requirements written in EARS (Easy Approach to Requirements Syntax).
//
// Every rule in a spectackle spec file must match exactly one of the six EARS
// patterns. Vague prose is rejected by the linter; keywords are case-sensitive
// and uppercase (WHEN, WHILE, IF, THEN, WHERE, SHALL) so that lowercase
// occurrences in code fragments (e.g. "if (i < n)") never trigger a pattern.
package ears

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

// Pattern is one of the six EARS rule patterns.
type Pattern uint8

const (
	PInvalid    Pattern = iota
	PUbiquitous         // The <system> SHALL <response>.
	PEvent              // WHEN <trigger>, the <system> SHALL <response>.
	PState              // WHILE <state>, the <system> SHALL <response>.
	PUnwanted           // IF <condition>, THEN the <system> SHALL <response>.
	POptional           // WHERE <feature>, the <system> SHALL <response>.
	PComplex            // combination, e.g. WHERE ..., WHEN ..., the <system> SHALL ...
)

var patternNames = [...]string{"?", "U", "E", "S", "N", "O", "C"}

func (p Pattern) String() string {
	if int(p) < len(patternNames) {
		return patternNames[p]
	}
	return "?"
}

var (
	reUbiquitous = regexp.MustCompile(`^The .+ SHALL .+`)
	reEvent      = regexp.MustCompile(`^WHEN .+, (?:the|an?) .+ SHALL .+`)
	reState      = regexp.MustCompile(`^WHILE .+, (?:the|an?) .+ SHALL .+`)
	reUnwanted   = regexp.MustCompile(`^IF .+, THEN (?:the|an?) .+ SHALL .+`)
	reOptional   = regexp.MustCompile(`^WHERE .+, (?:the|an?) .+ SHALL .+`)
	reComplex    = regexp.MustCompile(`^(?:WHERE .+, )?(?:WHILE .+, )?(?:WHEN|IF) .+, (?:THEN )?(?:the|an?) .+ SHALL .+`)

	// condition keywords at clause starts, used to detect Complex rules
	reClauseKeyword = regexp.MustCompile(`(?:^|, )(WHEN|WHILE|IF|WHERE) `)

	reRuleID = regexp.MustCompile(`^[A-Z][A-Z0-9]*(-[A-Z0-9]+)*-\d{3}$`)

	// W001 heuristic: the response clause is considered verifiable when it
	// names something concrete — a number, a code identifier (backticked,
	// camelCase, ALL_CAPS, dotted path or call), or a path-like token.
	reMeasurable = regexp.MustCompile("[0-9]|`|[a-z][A-Z]|[A-Za-z]+\\.[A-Za-z]|[A-Za-z_]+\\(|[A-Z]{2}[A-Z0-9_]*|/")
)

// vagueTerms are rejected outright (E004): they make a requirement untestable.
var vagueTerms = []string{
	"appropriate", "appropriately", "properly", "adequate", "user-friendly",
	"as needed", "if necessary", "etc", "and so on", "robust", "efficient",
	"efficiently", "fast", "quickly", "easily", "seamless", "seamlessly",
	"reasonable", "reasonably", "flexible", "intuitive", "handle",
}

// Classify returns the EARS pattern of a normalized rule sentence,
// or PInvalid if it matches none.
func Classify(sentence string) Pattern {
	s := normalize(sentence)
	if n := len(reClauseKeyword.FindAllString(s, -1)); n >= 2 && reComplex.MatchString(s) {
		return PComplex
	}
	switch {
	case reEvent.MatchString(s):
		return PEvent
	case reState.MatchString(s):
		return PState
	case reUnwanted.MatchString(s):
		return PUnwanted
	case reOptional.MatchString(s):
		return POptional
	case reUbiquitous.MatchString(s):
		return PUbiquitous
	}
	return PInvalid
}

// Severity of a lint finding.
type Severity uint8

const (
	Error Severity = iota
	Warn
)

func (s Severity) String() string {
	if s == Error {
		return "E"
	}
	return "W"
}

// Finding is one lint result, renderable as "! <code> <sev> <file>:<line> <msg>".
type Finding struct {
	Code     string
	Severity Severity
	File     string
	Line     int
	Msg      string
}

func (f Finding) String() string {
	return fmt.Sprintf("! %s %s %s:%d %s", f.Code, f.Severity, f.File, f.Line, f.Msg)
}

// LintSentence checks a single rule sentence (codes E001-E004, W001-W002).
func LintSentence(sentence, file string, line int) []Finding {
	s := normalize(sentence)
	var fs []Finding
	add := func(code string, sev Severity, msg string) {
		fs = append(fs, Finding{Code: code, Severity: sev, File: file, Line: line, Msg: msg})
	}

	shalls := strings.Count(s, "SHALL")
	if shalls == 0 {
		add("E001", Error, "rule has no SHALL response keyword")
		return fs // further checks are meaningless without a response clause
	}
	if shalls > 1 {
		add("E003", Error, "rule has more than one SHALL (single response clause required)")
	}
	if Classify(s) == PInvalid {
		add("E002", Error, "rule matches no EARS pattern (U/E/S/N/O/C)")
	}
	// W003 (B-01KYFSZ7): a doubled clause keyword is composer damage — the
	// pre-normalization composer prepended WHEN to triggers that already
	// led with it, and seven live rules carried WHEN WHEN undetected.
	for _, kw := range []string{"WHEN", "WHILE", "IF", "WHERE", "THEN"} {
		if strings.Contains(s, kw+" "+kw+" ") {
			add("W003", Warn, "doubled clause keyword "+kw+" "+kw+" (composer damage; re-compose via rule op=edit)")
		}
	}
	low := " " + strings.ToLower(s) + " "
	for _, t := range vagueTerms {
		if strings.Contains(low, " "+t+" ") || strings.Contains(low, " "+t+",") || strings.Contains(low, " "+t+".") {
			add("E004", Error, "vague term "+strconv(t)+" makes the rule untestable")
		}
	}
	if _, resp, ok := strings.Cut(s, "SHALL "); ok && !reMeasurable.MatchString(resp) {
		add("W001", Warn, "response clause names nothing verifiable (number, identifier, API, artifact)")
	}
	if n := len(strings.Fields(s)); n > 40 {
		add("W002", Warn, fmt.Sprintf("rule is %d words long (max 40); split it", n))
	}
	return fs
}

// Rule is one parsed spec rule (heading + sentence) inside a spec file.
type Rule struct {
	ID        string
	Pattern   Pattern
	Text      string   // the EARS sentence
	Rationale string   // optional "Rationale:" paragraph
	Applies   []string // optional node IDs from "{applies: id,id}" heading suffix
	File      string
	Line      int // line of the "## <ID>" heading
}

var reHeading = regexp.MustCompile(`^## +(\S+?)(?: +\{applies: *([^}]*)\})? *$`)

// proseSections are the whitelisted lowercase `##` headings that hold prose
// instead of rules (addressable as sec:<dir>#<name>); they are exempt from
// the E005 rule-ID check.
var proseSections = map[string]bool{"intent": true, "notes": true, "design": true, "context": true}

// IsProseSection reports whether a heading name is a whitelisted prose section.
func IsProseSection(name string) bool { return proseSections[name] }

// Section is one prose section of a spec file.
type Section struct {
	Name string
	Line int // 1-based heading line within the given text
	Text string
}

// ParseSections extracts the whitelisted prose sections from spec-file
// markdown (front matter already stripped).
func ParseSections(text string) []Section {
	lines := strings.Split(text, "\n")
	var out []Section
	for i := 0; i < len(lines); i++ {
		m := reHeading.FindStringSubmatch(lines[i])
		if m == nil || !proseSections[m[1]] {
			continue
		}
		var b []string
		j := i + 1
		for ; j < len(lines) && !strings.HasPrefix(lines[j], "## "); j++ {
			b = append(b, lines[j])
		}
		out = append(out, Section{Name: m[1], Line: i + 1, Text: strings.TrimSpace(strings.Join(b, "\n"))})
		i = j - 1
	}
	return out
}

// ParseRules extracts rules from spec-file markdown (front matter already
// stripped by the caller, or absent). startLine is the 1-based line number of
// the first line of text within the original file.
// It reports structural findings (E005) alongside the parsed rules.
func ParseRules(text, file string, startLine int) ([]Rule, []Finding) {
	lines := strings.Split(text, "\n")
	var rules []Rule
	var fs []Finding
	for i := 0; i < len(lines); i++ {
		m := reHeading.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		ln := startLine + i
		id := m[1]
		if proseSections[id] {
			continue // whitelisted prose section, not a rule
		}
		if !reRuleID.MatchString(id) {
			fs = append(fs, Finding{Code: "E005", Severity: Error, File: file, Line: ln,
				Msg: "heading " + strconv(id) + " is not a valid rule ID (PREFIX-SEG-042 form)"})
		}
		r := Rule{ID: id, File: file, Line: ln}
		if m[2] != "" {
			for _, a := range strings.Split(m[2], ",") {
				if a = strings.TrimSpace(a); a != "" {
					r.Applies = append(r.Applies, a)
				}
			}
		}
		// rule sentence: first non-empty line before the next heading;
		// "Rationale:" paragraphs are captured separately.
		for j := i + 1; j < len(lines) && !strings.HasPrefix(lines[j], "## "); j++ {
			l := strings.TrimSpace(lines[j])
			if l == "" {
				continue
			}
			if rat, ok := strings.CutPrefix(l, "Rationale:"); ok {
				r.Rationale = strings.TrimSpace(rat)
				continue
			}
			if r.Text == "" {
				r.Text = l
				fs = append(fs, LintSentence(l, file, startLine+j)...)
			}
		}
		if r.Text == "" {
			fs = append(fs, Finding{Code: "E002", Severity: Error, File: file, Line: ln,
				Msg: "rule " + id + " has no sentence"})
		}
		r.Pattern = Classify(r.Text)
		rules = append(rules, r)
	}
	return rules, fs
}

// LintFile parses and lints one spec file from disk (including front matter
// stripping identical to the cascade loader).
func LintFile(path string) ([]Finding, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	body, bodyStart := StripFrontMatter(string(raw))
	_, fs := ParseRules(body, path, bodyStart)
	return fs, nil
}

// StripFrontMatter removes a leading "---\n...\n---\n" YAML block and returns
// the remaining body plus the 1-based line number the body starts at.
func StripFrontMatter(s string) (body string, bodyStartLine int) {
	const fence = "---"
	lines := strings.Split(s, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != fence {
		return s, 1
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == fence {
			return strings.Join(lines[i+1:], "\n"), i + 2
		}
	}
	return s, 1
}

// FrontMatter returns the raw YAML between the fences, or "".
func FrontMatter(s string) string {
	const fence = "---"
	lines := strings.Split(s, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != fence {
		return ""
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == fence {
			return strings.Join(lines[1:i], "\n")
		}
	}
	return ""
}

func normalize(s string) string { return strings.Join(strings.Fields(s), " ") }

func strconv(s string) string { return "\"" + s + "\"" }
