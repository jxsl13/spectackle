package ears

import (
	"strings"
	"testing"
)

// Fuzz targets (SPX-EARS-004): the linter surface eats untrusted spec text —
// the invariant under fuzzing is crash-freedom plus a few cheap structural
// guarantees, never grammar semantics (those live in ears_test.go).

func FuzzLintSentence(f *testing.F) {
	f.Add("The system SHALL log to stderr only.", "spec.md", 1)
	f.Add("WHEN a kernel is launched, the wrapper SHALL check cudaGetLastError.", "a.md", 3)
	f.Add("WHILE a worktree is open, the server SHALL block compact.", "b.md", 0)
	f.Add("IF a status is non-zero, THEN the binding SHALL wrap it.", "c.md", -1)
	f.Add("vague prose without any keyword", "d.md", 2)
	f.Add("", "", 0)
	f.Add("SHALL SHALL SHALL", "e.md", 99)
	f.Fuzz(func(t *testing.T, sentence, file string, line int) {
		LintSentence(sentence, file, line) // must not panic
	})
}

func FuzzParseRules(f *testing.F) {
	f.Add("## ABC-001\nThe system SHALL log to stderr only.\n")
	f.Add("## ABC-001 {applies: go:pkg.A,go:pkg.B}\nWHEN x, the y SHALL z.\n\nRationale: because.\n")
	f.Add("## intent\nprose only\n")
	f.Add("## \n\n##\n## X {applies: }\n")
	f.Add("no heading at all\r\nwith CRLF\r\n")
	f.Add("")
	f.Fuzz(func(t *testing.T, text string) {
		rules, _ := ParseRules(text, "fuzz.md", 1)
		for _, r := range rules {
			if r.Line < 0 {
				t.Fatalf("negative rule line %d for %q", r.Line, r.ID)
			}
		}
	})
}

func FuzzStripFrontMatter(f *testing.F) {
	f.Add("---\nschema: v0\n---\nbody\n")
	f.Add("---\nunclosed frontmatter\n")
	f.Add("body without frontmatter\n")
	f.Add("---\n---\n")
	f.Add("")
	f.Add("\r\n---\r\nschema: v0\r\n---\r\nbody\r\n")
	f.Fuzz(func(t *testing.T, text string) {
		body, start := StripFrontMatter(text)
		if start < 1 {
			t.Fatalf("bodyStartLine must be >= 1, got %d", start)
		}
		if len(body) > len(text) {
			t.Fatalf("body longer than input: %d > %d", len(body), len(text))
		}
		if body != "" && !strings.Contains(text, body[:1]) {
			t.Fatalf("body head %q not from input", body[:1])
		}
	})
}
