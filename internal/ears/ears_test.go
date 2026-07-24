package ears

import (
	"strings"
	"testing"
)

func TestClassify(t *testing.T) {
	cases := []struct {
		in   string
		want Pattern
	}{
		{"The graph SHALL identify every node by a stable ID.", PUbiquitous},
		{"WHEN a tool result exceeds the budget, the server SHALL truncate it.", PEvent},
		{"WHILE serving a tool call, the server SHALL keep file reads under 1 MiB.", PState},
		{"IF a rule has no response keyword, THEN the linter SHALL report E001.", PUnwanted},
		{"WHERE CUDA support is enabled, the indexer SHALL parse .cu files.", POptional},
		{"WHERE CUDA support is enabled, WHEN a kernel launch is found, the resolver SHALL add a launch edge.", PComplex},
		{"The kernel SHALL guard access with a check of the form if (i < n).", PUbiquitous}, // lowercase "if" is not a keyword
		{"The server should truncate results.", PInvalid},                                   // no SHALL
		{"Make it fast.", PInvalid},
	}
	for _, c := range cases {
		if got := Classify(c.in); got != c.want {
			t.Errorf("Classify(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestLintSentenceCodes(t *testing.T) {
	cases := []struct {
		in   string
		want string // expected finding code, "" for clean
	}{
		{"The server SHALL write only JSON-RPC 2.0 frames to stdout.", ""},
		{"The server should write frames to stdout.", "E001"},
		{"Whenever stuff happens, the server SHALL react.", "E002"},
		{"IF the input is bad, THEN the server SHALL reject it and the client SHALL retry.", "E003"},
		{"The server SHALL handle errors appropriately.", "E004"},
		{"The server SHALL respond.", "W001"},
		{"The server SHALL respond to every request with a well formed reply that is documented in the manual and reviewed by the team and checked against the style guide and approved by the maintainers and archived forever in the repository history log 1.", "W002"},
	}
	for _, c := range cases {
		fs := LintSentence(c.in, "t.md", 1)
		if c.want == "" {
			if len(fs) != 0 {
				t.Errorf("LintSentence(%q) = %v, want clean", c.in, fs)
			}
			continue
		}
		found := false
		for _, f := range fs {
			if f.Code == c.want {
				found = true
			}
		}
		if !found {
			t.Errorf("LintSentence(%q) = %v, want code %s", c.in, fs, c.want)
		}
	}
}

func TestParseRules(t *testing.T) {
	src := `# Title

## SPX-TST-001
The server SHALL emit JSON-RPC 2.0 frames.

Rationale: stdio framing.

## SPX-TST-002 {applies: go:pkg.Fn, cu:kern}
WHEN a request arrives, the server SHALL respond within 2 seconds.

## bad-id
The server SHALL do 1 thing.
`
	rules, fs := ParseRules(src, "t.md", 1)
	if len(rules) != 3 {
		t.Fatalf("got %d rules, want 3", len(rules))
	}
	if rules[0].ID != "SPX-TST-001" || rules[0].Pattern != PUbiquitous || rules[0].Rationale != "stdio framing." {
		t.Errorf("rule 0 parsed wrong: %+v", rules[0])
	}
	if len(rules[1].Applies) != 2 || rules[1].Applies[0] != "go:pkg.Fn" {
		t.Errorf("applies parsed wrong: %+v", rules[1].Applies)
	}
	hasE005 := false
	for _, f := range fs {
		if f.Code == "E005" {
			hasE005 = true
		}
	}
	if !hasE005 {
		t.Errorf("expected E005 for bad-id heading, got %v", fs)
	}
}

func TestStripFrontMatter(t *testing.T) {
	body, start := StripFrontMatter("---\nprefix: X\n---\nbody line\n")
	if !strings.HasPrefix(body, "body line") || start != 4 {
		t.Errorf("StripFrontMatter = %q, %d", body, start)
	}
	fm := FrontMatter("---\nprefix: X\n---\nbody\n")
	if fm != "prefix: X" {
		t.Errorf("FrontMatter = %q", fm)
	}
}
