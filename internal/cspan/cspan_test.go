package cspan

import (
	"bufio"
	"bytes"
	"regexp"
	"testing"
)

// scanLines mirrors internal/langspec's scanLines line-splitting (this
// package has zero non-stdlib imports, so it cannot import langspec's
// helper — the split logic itself is trivial enough to duplicate here for
// test setup only, unlike the span-scanning logic this package exists to
// deduplicate).
func scanLines(src []byte) ([]string, error) {
	sc := bufio.NewScanner(bytes.NewReader(src))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var lines []string
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return lines, nil
}

// --- T-0053: Allman-style brace spans; T-0054: relocated verbatim --------
//
// Span originally (as internal/langspec's unexported braceSpan) only found
// a body when the def line itself opened '{' (K&R). ddnet — and huge parts
// of the C/C++/C# world — put the brace on the next line (Allman), which
// meant zero bodies scanned, zero call edges, in an Allman codebase (see
// docs/validation-ddnet.md, T-0051/T-0053). T-0054 extracted braceSpan out
// of internal/langspec into this leaf package (so internal/resolve's FFI
// bridging pass — which cannot import langspec — can share the same fixed
// logic instead of carrying its own unfixed K&R-only copy) with no
// semantic change; this table is the same regression suite, moved
// verbatim from langspec_test.go's TestBraceSpanAllman, now exercising the
// exported Span directly. See internal/langspec's c_test.go/cpp_test.go
// for the end-to-end, real-regex-driven fixtures that remain there.
func TestSpanAllman(t *testing.T) {
	tests := []struct {
		name    string
		src     string
		start   int // 0-indexed line of the Def hit
		wantEnd int // 0-indexed line of the closing brace, only checked if wantOK
		wantOK  bool
	}{
		{
			name:    "K&R same-line (regression)",
			src:     "void f() {\n    g();\n}\n",
			start:   0,
			wantEnd: 2,
			wantOK:  true,
		},
		{
			name:    "Allman next-line",
			src:     "void f()\n{\n    g();\n}\n",
			start:   0,
			wantEnd: 3,
			wantOK:  true,
		},
		{
			name:    "Allman with blank line between",
			src:     "void f()\n\n{\n    g();\n}\n",
			start:   0,
			wantEnd: 4,
			wantOK:  true,
		},
		{
			name:   "prototype 'void f(int);' has no span",
			src:    "void f(int);\n",
			start:  0,
			wantOK: false,
		},
		{
			name:    "multi-line param header ending in Allman brace",
			src:     "void f(int a,\n       int b)\n{\n    g();\n}\n",
			start:   0,
			wantEnd: 4,
			wantOK:  true,
		},
		{
			name:    "multi-line param header ending in K&R brace",
			src:     "void f(int a,\n       int b) {\n    g();\n}\n",
			start:   0,
			wantEnd: 3,
			wantOK:  true,
		},
		{
			name:   "multi-line param header that is actually a prototype",
			src:    "void f(int a,\n       int b);\n",
			start:  0,
			wantOK: false,
		},
		{
			name:   "'#define F(x) (x)' unaffected",
			src:    "#define F(x) (x)\n",
			start:  0,
			wantOK: false,
		},
		{
			name:   "brace never arrives within the lookahead window",
			src:    "void f()\n\n\n\n{\n    g();\n}\n",
			start:  0,
			wantOK: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lines, err := scanLines([]byte(tt.src))
			if err != nil {
				t.Fatalf("scanLines: %v", err)
			}
			end, ok := Span(lines, tt.start)
			if ok != tt.wantOK {
				t.Fatalf("Span ok = %v, want %v (end=%d)", ok, tt.wantOK, end)
			}
			if ok && end != tt.wantEnd {
				t.Errorf("Span end = %d, want %d (0-indexed line of closing brace)", end, tt.wantEnd)
			}
		})
	}
}

// --- T-0118: KeywordSpan, the end-terminated-language counterpart to Span --
//
// Lua-style: "function"/"if"/"do" are the block openers ("do" alone stands
// in for both "for ... do" and "while ... do" — the loop keyword itself
// isn't separately counted, else a "for i=1,10 do" line would open twice
// for one "end"); "end" is the sole closer.
var luaOpen = regexp.MustCompile(`\b(function|if|do)\b`)
var luaClose = regexp.MustCompile(`\bend\b`)

// Ruby-style: "def"/"class"/"module" are the block openers, "end" is the
// sole closer. (Real Ruby also opens on "do", "if", "unless", etc. and has
// to dodge modifier-if/unless/while suffixes that look like a block open
// but aren't ("x += 1 if y") — that per-language regex tuning is T-0119's
// job; this fixture only needs enough of the language to exercise
// KeywordSpan's generic depth-counting.)
var rubyOpen = regexp.MustCompile(`\b(def|class|module)\b`)
var rubyClose = regexp.MustCompile(`\bend\b`)

func TestKeywordSpan(t *testing.T) {
	tests := []struct {
		name        string
		src         string
		start       int
		open, close *regexp.Regexp
		wantEnd     int
		wantOK      bool
	}{
		{
			name:    "lua function..end",
			src:     "function run(x)\n  print(x)\nend\n",
			start:   0,
			open:    luaOpen,
			close:   luaClose,
			wantEnd: 2,
			wantOK:  true,
		},
		{
			name:    "lua if..end",
			src:     "if x then\n  print(x)\nend\n",
			start:   0,
			open:    luaOpen,
			close:   luaClose,
			wantEnd: 2,
			wantOK:  true,
		},
		{
			name:    "lua for..do..end (do is the counted opener)",
			src:     "for i=1,10 do\n  print(i)\nend\n",
			start:   0,
			open:    luaOpen,
			close:   luaClose,
			wantEnd: 2,
			wantOK:  true,
		},
		{
			name:    "lua nested function+if",
			src:     "function run(x)\n  if x then\n    return x\n  end\nend\n",
			start:   0,
			open:    luaOpen,
			close:   luaClose,
			wantEnd: 4,
			wantOK:  true,
		},
		{
			name:    "lua one-line body (open and close on the def line)",
			src:     "function noop() end\n",
			start:   0,
			open:    luaOpen,
			close:   luaClose,
			wantEnd: 0,
			wantOK:  true,
		},
		{
			name:   "lua unterminated body has no span",
			src:    "function run(x)\n  print(x)\n",
			start:  0,
			open:   luaOpen,
			close:  luaClose,
			wantOK: false,
		},
		{
			name:   "def line that doesn't match open has no span",
			src:    "local x = 1\nend\n",
			start:  0,
			open:   luaOpen,
			close:  luaClose,
			wantOK: false,
		},
		{
			name:    "ruby def..end",
			src:     "def run(x)\n  puts x\nend\n",
			start:   0,
			open:    rubyOpen,
			close:   rubyClose,
			wantEnd: 2,
			wantOK:  true,
		},
		{
			name:    "ruby nested class+def",
			src:     "class Foo\n  def bar\n    1\n  end\nend\n",
			start:   0,
			open:    rubyOpen,
			close:   rubyClose,
			wantEnd: 4,
			wantOK:  true,
		},
		{
			name:    "ruby one-line body (open and close on the def line)",
			src:     "def noop; end\n",
			start:   0,
			open:    rubyOpen,
			close:   rubyClose,
			wantEnd: 0,
			wantOK:  true,
		},
		{
			name:   "ruby unterminated body has no span",
			src:    "def run(x)\n  puts x\n",
			start:  0,
			open:   rubyOpen,
			close:  rubyClose,
			wantOK: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lines, err := scanLines([]byte(tt.src))
			if err != nil {
				t.Fatalf("scanLines: %v", err)
			}
			end, ok := KeywordSpan(lines, tt.start, tt.open, tt.close)
			if ok != tt.wantOK {
				t.Fatalf("KeywordSpan ok = %v, want %v (end=%d)", ok, tt.wantOK, end)
			}
			if ok && end != tt.wantEnd {
				t.Errorf("KeywordSpan end = %d, want %d (0-indexed line of closing keyword)", end, tt.wantEnd)
			}
		})
	}
}
