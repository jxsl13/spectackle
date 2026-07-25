package langspec

import (
	"regexp"

	"github.com/jxsl13/spectackle/internal/graph"
)

// luaSpec covers .lua files. Qualification is QualFileStem (a Lua module
// is its own namespace): `function run()` in app.lua mints "lua:app.run".
//
// Matched names may retain dots or colons (`M.name`, `Obj:method`) because
// Lua uses them to spell table-qualified functions and methods directly in
// the `function` statement; ids.Mint accepts any non-whitespace qualifier,
// so these pass through unqualified further (e.g. "lua:app.Obj:method").
var luaSpec = Spec{
	Lang: graph.LangLua,
	Exts: []string{".lua"},
	Qual: QualFileStem,
	Defs: []Def{
		{
			Kind: graph.KFunc,
			// `function run(x)`, `local function run(x)`, `function M.run(x)`,
			// `function Obj:method(x)`.
			Re:   regexp.MustCompile(`^\s*(?:local\s+)?function\s+([A-Za-z_][A-Za-z0-9_.:]*)\s*\(`),
			Name: 1,
		},
		{
			Kind: graph.KFunc,
			// `run = function(x)`, `local run = function(x)`.
			Re:   regexp.MustCompile(`^\s*(?:local\s+)?([A-Za-z_][A-Za-z0-9_.]*)\s*=\s*function\s*\(`),
			Name: 1,
		},
	},

	// CallRe/Stop (LSP-001): any `name(` inside a def's keyword-counted body
	// span is a call edge unless it's the def's own name or a reserved word.
	CallRe: regexp.MustCompile(`\b([A-Za-z_]\w*)\s*\(`),
	Stop:   luaCallStop,

	// EndSpan (T-0119): Lua has no braces, so the body is bounded by
	// keyword-counting instead (cspan.KeywordSpan). Open counts
	// function/if/for/while — deliberately NOT "do" on its own: for/while's
	// trailing "do" ("for i = 1, x do", "while x do") is part of the SAME
	// statement as the for/while keyword already counted, and Lua has no
	// other bare "do ... end" construct exercised by any Def here, so
	// counting "do" separately would double-count every for/while loop
	// (two opens, one "end" to close them). Lua has no false-open trap
	// analogous to Ruby's modifier-if: every `if`/`for`/`while` in Lua is
	// syntactically a block (Lua has no postfix/modifier control-flow
	// forms), and one-line forms (`if x then y end`) still require the
	// literal "end" keyword, so KeywordSpan's net same-line counting
	// (open=1, close=1 => depth 0, single-line body) handles them
	// correctly with no special-casing. `elseif`/`repeat` are deliberately
	// excluded: "elseif" doesn't match `\bif\b` (no word boundary before
	// "if" inside "elseif", since 's' and 'i' are both word characters),
	// and `repeat ... until` closes with "until", not "end", so pairing it
	// with the "end"-only Close here would either never close (ok=false)
	// or, if nested inside a function, wrongly consume the enclosing
	// function's own "end" — Lua source using `repeat` is a known,
	// documented gap (not exercised by any fixture/finding here; "repeat"
	// is not in Open, so it is simply never counted, leaving Span
	// computation for a repeat-containing function unaffected as long as
	// the repeat block's own "until" line contains no bare "end").
	EndSpan: &EndSpanSpec{
		Open:  regexp.MustCompile(`\b(function|if|for|while)\b`),
		Close: regexp.MustCompile(`\bend\b`),
	},
}

// luaCallStop is Lua's full reserved-word list: none of these can ever be a
// real callable symbol, but `function(` (anonymous function literal, e.g.
// `run2 = function(x)`) structurally matches CallRe's `name(` shape and
// must never become a phantom "function" call edge.
var luaCallStop = []string{
	"and", "break", "do", "else", "elseif", "end", "false", "for",
	"function", "goto", "if", "in", "local", "nil", "not", "or",
	"repeat", "return", "then", "true", "until", "while",
}

func init() { registry = append(registry, luaSpec) }
