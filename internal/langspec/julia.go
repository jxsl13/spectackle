package langspec

import (
	"regexp"

	"github.com/jxsl13/spectackle/internal/graph"
)

// juliaSpec covers .jl files. Qualification is QualFileStem, mirroring
// pythonSpec: one file is treated as one module, so `function run` in app.jl
// mints "jl:app.run".
var juliaSpec = Spec{
	Lang: graph.LangJl,
	Exts: []string{".jl"},
	Qual: QualFileStem,
	Defs: []Def{
		{
			Kind: graph.KFunc,
			// `function run(x)`, indented `    function run(x)`, dotted
			// names such as `function Base.show(io, x)`, and bang names
			// such as `function push!(v, x)`.
			Re:   regexp.MustCompile(`^\s*function\s+([\w.!]+)\s*\(`),
			Name: 1,
		},
		{
			Kind: graph.KFunc,
			// `macro mytime(ex) ... end` — no Def existed for macro
			// definitions at all before this fix; mints the same KFunc
			// kind as an ordinary function (a Julia macro is invoked like
			// one, just prefixed with `@` at call sites).
			Re:   regexp.MustCompile(`^\s*macro\s+([A-Za-z_]\w*!?)\s*\(`),
			Name: 1,
		},
		{
			Kind: graph.KFunc,
			// Short-form one-liner definitions at column 0 only:
			// `square(x) = x * x`, and now also a `where` generic/parametric
			// clause between the arg list and `=`, e.g.
			// `scale(x::T) where T = x * 2` — the optional
			// `(?:where\b.*?)?` (lazy) consumes exactly up to the `=` that
			// was already required, so the un-where'd case
			// (`square(x) = x * x`) is unaffected (the optional group
			// simply matches zero characters when there is no "where").
			// The trailing `[^=]` guard requires the character right after
			// `=` not to itself be `=`, which excludes comparison lines
			// such as `foo(x) == bar(y)`. Indented short-form lines are
			// excluded by the column-0 anchor. NOT fixed (low severity,
			// not free): a short-form def whose `)` and `=` are split
			// across separate physical lines at all (e.g. `volume(\n r,\n) = ...`)
			// — the framework scans one line at a time
			// (internal/langspec/langspec.go), so a single-line regex
			// structurally cannot see across that split; would need a
			// multi-line lookahead rewrite of the Def-matching loop itself,
			// out of this file's scope.
			Re:   regexp.MustCompile(`^([\w!]+)\s*\([^)]*\)\s*(?:where\b.*?)?\s*=\s*[^=]`),
			Name: 1,
		},
		{
			Kind: graph.KType,
			// `struct Point` or `mutable struct Point`.
			Re:   regexp.MustCompile(`^\s*(?:mutable\s+)?struct\s+(\w+)`),
			Name: 1,
		},
		{
			Kind: graph.KType,
			// `abstract type Shape end`.
			Re:   regexp.MustCompile(`^\s*abstract\s+type\s+(\w+)`),
			Name: 1,
		},
	},

	// CallRe/Stop (LSP-001): any `name(` inside a def's keyword-counted body
	// span is a call edge unless it's the def's own name or a reserved
	// word. The trailing `!?` (Julia has no `?`-suffixed identifier
	// convention, unlike Ruby/Elixir) lets a call site correctly resolve to
	// a bang-named function (e.g. `normalize!(p)`, called from `run_all` in
	// this fixture) instead of truncating to "normalize", a name that was
	// never minted.
	CallRe: regexp.MustCompile(`\b([A-Za-z_]\w*!?)\s*\(`),
	Stop:   juliaCallStop,

	// EndSpan (T-0119): Julia has no braces, so the body is bounded by
	// keyword-counting instead (cspan.KeywordSpan). Open counts
	// function/macro/if/for/while/begin/struct — struct is included even
	// though KType defs are never span-computed by the engine (only
	// KFunc/KMethod are), purely so a function/macro body that happens to
	// contain a nested struct block doesn't have its depth undercounted by
	// an uncounted-open, correctly-counted-close mismatch (not exercised
	// by this fixture, included for robustness). `elseif` is deliberately
	// NOT double-counted: "elseif" doesn't match `^\s*if\b` (the line
	// starts with "elseif", not "if" — the anchor requires "if" to be the
	// literal first token, and "elseif" fails that literal comparison at
	// the very first character). Julia has no modifier/postfix control-flow
	// form (unlike Ruby) — every `if`/`for`/`while` keyword always opens a
	// real `end`-terminated block, ternaries use `cond ? a : b` with no
	// "if" keyword at all — so there is no false-open trap to guard
	// against here.
	EndSpan: &EndSpanSpec{
		Open:  regexp.MustCompile(`^\s*function\s+|^\s*macro\s+|^\s*if\b|^\s*for\b|^\s*while\b|^\s*begin\b|^\s*(?:mutable\s+)?struct\s+`),
		Close: regexp.MustCompile(`^\s*end\b`),
	},
}

// juliaCallStop is Julia's reserved-word list plus "new" (the special
// in-constructor call that builds an instance of the enclosing struct —
// syntactically a call, but never a symbol this framework could mint a
// node for).
var juliaCallStop = []string{
	"baremodule", "begin", "break", "catch", "const", "continue", "do",
	"else", "elseif", "end", "export", "false", "finally", "for",
	"function", "global", "if", "import", "let", "local", "macro",
	"module", "quote", "return", "struct", "true", "try", "using",
	"while", "new",
}

func init() { registry = append(registry, juliaSpec) }
