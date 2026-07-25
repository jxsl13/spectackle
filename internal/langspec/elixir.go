package langspec

import (
	"regexp"

	"github.com/jxsl13/spectackle/internal/graph"
)

// elixirSpec covers .ex/.exs files. Qualification is QualFileStem, mirroring
// pythonSpec/rubySpec: one file is treated as one module, so `def run` in
// app.ex mints "ex:app.run".
var elixirSpec = Spec{
	Lang: graph.LangEx,
	Exts: []string{".ex", ".exs"},
	Qual: QualFileStem,
	Defs: []Def{
		{
			Kind: graph.KFunc,
			// `def run(x)`, `defp helper(x)`, `defmacro foo(x)`,
			// `defmacrop foo(x)`, and now `defguard`/`defguardp` (guard
			// macros, e.g. `defguard is_positive(n) when ...`) and
			// `defdelegate` (function delegation, e.g.
			// `defdelegate delegated_double(x), to: Utils, as: :double_value`
			// — the delegate's own name is captured exactly like an
			// ordinary def; the `to:`/`as:` options after it are outside
			// the capture group). A trailing `?`/`!` is allowed (Elixir
			// convention for predicate/dangerous functions, e.g.
			// `def valid?` or `def save!`); ids.Mint's idRe accepts it
			// unmodified. The mandatory `\s+` after the def keyword means
			// `defstruct [...]`/`defexception [...]`/`defprotocol`/
			// `defimpl` never match here: none of "struct", "exception",
			// "protocol", "impl" is one of this alternation's suffixes, so
			// for each of those "def" can only consume its own 3 letters,
			// leaving the rest of the keyword glued on with no whitespace
			// before it — defprotocol/defimpl get their own Def below;
			// defstruct/defexception are deliberately unhandled (absent
			// from every finding for this language).
			Re:   regexp.MustCompile(`^\s*def(?:p|macro|macrop|guard|guardp|delegate)?\s+([a-z_]\w*[?!]?)`),
			Name: 1,
		},
		{
			Kind: graph.KType,
			// `defmodule Foo do` or `defmodule Foo.Bar.Baz do` — dotted
			// module names kept verbatim in the captured name.
			Re:   regexp.MustCompile(`^\s*defmodule\s+([A-Z][\w.]*)`),
			Name: 1,
		},
		{
			Kind: graph.KType,
			// `defprotocol MyApp.Calculator.Shape do` — protocol
			// declaration; mints a type node the same way defmodule does.
			Re:   regexp.MustCompile(`^\s*defprotocol\s+([A-Z][\w.]*)`),
			Name: 1,
		},
		{
			Kind: graph.KType,
			// `defimpl MyApp.Calculator.Shape, for: Tuple do` — captures the
			// protocol name being implemented (stops at the `,` before
			// `for:`). This intentionally mints under the SAME name as the
			// defprotocol declaration it implements (that's what Elixir
			// code actually calls it); a source file with both gets the
			// framework's existing ~2/~3 collision suffixing, same as any
			// other same-named symbol pair.
			Re:   regexp.MustCompile(`^\s*defimpl\s+([A-Z][\w.]*)`),
			Name: 1,
		},
	},

	// CallRe/Stop (LSP-001): any `name(` inside a def's keyword-counted body
	// span is a call edge unless it's the def's own name or a reserved
	// word. The trailing `[!?]?` lets a call site correctly resolve to a
	// bang/predicate-named function (e.g. `valid?(name)`, `save!(name)` —
	// both real calls inside this fixture's `greet/1`) instead of
	// truncating to a name that was never minted.
	CallRe: regexp.MustCompile(`\b([A-Za-z_]\w*[!?]?)\s*\(`),
	Stop:   elixirCallStop,

	// EndSpan (T-0119): Elixir has no braces, so the body is bounded by
	// keyword-counting instead (cspan.KeywordSpan). Every Elixir block
	// construct — def/defp/defmacro/defmacrop/defguard/defdelegate's
	// non-existent body aside, defmodule/defprotocol/defimpl, if/unless,
	// case/cond, for/with, try, receive, quote, fn — opens with a trailing
	// "do" keyword and closes with "end"; there is no construct-specific
	// opener list to build the way Lua/Ruby/Julia need one, since Elixir
	// uniformly spells every block the same way. So Open is a single
	// trailing-"do" pattern, and Close is the universal "end".
	//
	// The "do:" trap (this language's version of Ruby's modifier-if): the
	// single-line `, do: expr` form (`def sum(a, b), do: a + b`) has no
	// matching "end" at all, but a naive `\bdo\b` would match the "do" in
	// "do:" (word-boundary triggers between "o" and ":"). Real block "do"
	// is always the LAST significant token on its line; anchoring at
	// end-of-line via `do\s*(?:#.*)?$` matches `def run(x) do`,
	// `if valid?(name) do`, `defimpl ..., for: Tuple do`, etc., but never
	// `do:` (always followed by more code on the same line).
	//
	// KNOWN GAP (not fixable from this file alone): a multi-clause def
	// whose guard is wrapped onto its own continuation line before "do"
	// (this fixture's `classify/1`, guarded clauses only —
	// `def classify(n)\n    when is_integer(n) and n > 0 do\n  :positive\nend`)
	// keeps EndLine == Line. cspan.KeywordSpan's contract (T-0118, frozen
	// by this task) requires the opening keyword to be on the SAME line as
	// the Def match; here the Def regex matches "def classify(n)" on line
	// 1, but "do" is on line 2, so KeywordSpan's first check
	// (open vs lines[start]) never finds it and returns ok=false — exactly
	// the same "no body found" result as before this task, i.e. no
	// regression, just not improved for this one construct shape. Every
	// other multi-line function/macro in this fixture (including
	// classify/1's third, unguarded clause, which has "do" on its def
	// line) spans correctly.
	EndSpan: &EndSpanSpec{
		Open:  regexp.MustCompile(`\bdo\s*(?:#.*)?$`),
		Close: regexp.MustCompile(`^\s*end\b`),
	},
}

// elixirCallStop is Elixir's reserved-word list plus the control-flow
// special forms (if/unless/case/cond/for/with/receive/try/raise/throw) that
// can be — though rarely are — written with an explicit `name(` call shape;
// none of them names a symbol this framework could ever mint a node for.
var elixirCallStop = []string{
	"after", "and", "catch", "do", "else", "false", "fn", "in", "nil",
	"not", "or", "rescue", "true", "when",
	"if", "unless", "case", "cond", "for", "with", "receive", "try",
	"raise", "throw",
}

func init() { registry = append(registry, elixirSpec) }
