package langspec

import (
	"regexp"

	"github.com/jxsl13/spectackle/internal/graph"
)

// rubySpec covers .rb files. Qualification is QualFileStem, mirroring
// pythonSpec: one file is one module, so `def run` in app.rb mints
// "rb:app.run". Method names may carry a trailing `?` or `!` (Ruby
// convention for predicate/dangerous methods, e.g. `def valid?` or
// `def save!`); the Name capture group allows that suffix, and
// ids.Mint's idRe ([^\s~]+ for the qualifier) accepts it unmodified.
var rubySpec = Spec{
	Lang: graph.LangRb,
	Exts: []string{".rb"},
	Qual: QualFileStem,
	Defs: []Def{
		{
			Kind: graph.KFunc,
			// `def run(x)`, `def self.run(x)` (class/module method),
			// `def valid?`, `def save!`, and now `def radius=(value)`
			// (setter methods: the trailing `=?` lets the Name capture
			// include the setter's own `=`, distinct from the `(` that
			// follows it — `def foo(x = 1)`'s default-value `=` is inside
			// the parens, never adjacent to the captured identifier, so it
			// can never be swept into the name).
			Re:   regexp.MustCompile(`^\s*def\s+(?:self\.)?(\w+[?!]?=?)`),
			Name: 1,
		},
		{
			Kind: graph.KFunc,
			// Operator method definitions: `def <=>(other)`, `def [](i)`,
			// `def []=(i, v)`, `def +(other)`, `def ==(other)`, etc. — none
			// of these match the word-based Def above (`\w+` requires word
			// characters, and none of these names are words). Longer
			// operators are ordered before their prefixes (`<=>` before
			// `<=`/`<`, `[]=` before `[]`, `===`/`==` before `=`-adjacent
			// forms) so the leftmost-first alternation picks the full
			// operator, not a truncated prefix of it.
			Re:   regexp.MustCompile(`^\s*def\s+(?:self\.)?(<=>|===|==|!=|<=|>=|<<|>>|\*\*|=~|\[\]=|\[\]|\+@|-@|[+\-*/%<>&|^~!])\s*\(?`),
			Name: 1,
		},
		{
			Kind: graph.KType,
			// `class Foo`, `class Foo < Base`, `module Foo`, and now
			// `class Outer::Inner` (namespaced/reopened class: the
			// trailing `(?:::\w+)*` keeps consuming `::`-joined segments
			// instead of stopping at the first one, so the class mints
			// under its full "Outer::Inner" name — ids.Mint accepts `::`
			// unmodified in the qualifier, same as Lua's dotted/colon
			// names).
			Re:   regexp.MustCompile(`^\s*(?:class|module)\s+(\w+(?:::\w+)*)`),
			Name: 1,
		},
	},

	// CallRe/Stop (LSP-001): any `name(` inside a def's keyword-counted body
	// span is a call edge unless it's the def's own name or a reserved
	// word. The trailing `[!?]?` lets a call site correctly resolve to a
	// bang/predicate-named method (e.g. `valid?(x)`, `save!(x)`) instead of
	// truncating to a name that was never minted.
	CallRe: regexp.MustCompile(`\b([A-Za-z_]\w*[!?]?)\s*\(`),
	Stop:   rubyCallStop,

	// EndSpan (T-0119): Ruby has no braces, so the body is bounded by
	// keyword-counting instead (cspan.KeywordSpan). Open counts
	// def/class/module/begin/case and the block forms of if/unless, plus
	// "do" as a trailing block-invocation keyword (`arr.each do |x|`,
	// `loop do`) — see each sub-pattern's comment for its false-open
	// handling. Close ("end") is the only way Ruby ever closes a block, so
	// a single `^\s*end\b` suffices for every opener above.
	//
	// The modifier-if/unless/while/until trap (explicitly called out in
	// this task's brief): Ruby lets any statement end in
	// `<statement> if <condition>` (a postfix guard, no matching `end`
	// ever follows) as well as the ordinary block form
	// `if <condition> ... end`. Both spellings put the literal word "if"
	// in the line, so a naive `\bif\b` would treat every modifier-if as a
	// false open, undercounting depth and closing the real enclosing
	// def/class too early. The fix: anchor if/unless at the START of the
	// (whitespace-trimmed) line via `^\s*(if|unless)\b`. A modifier form
	// always has some other statement before the keyword
	// (`raise X if y`, `return x unless y`), so "if"/"unless" is never the
	// first token there and the anchor never matches; the block form
	// always opens with "if"/"unless" as its first token, so the anchor
	// always matches it. (Not handled: `x = if cond ... end`, an if used
	// as an expression's value on an assignment's right-hand side — rare,
	// not exercised by any finding or fixture here, and left as a
	// documented gap rather than chased.)
	//
	// The "do:" trap (analogous to the modifier-if trap, for a different
	// keyword): Ruby's single-line "do:" keyword-argument/colon form
	// (`def sum(a, b), do: a + b`) has no matching "end" at all, but a
	// naive `\bdo\b` would match the "do" in "do:" (the boundary is
	// between "o" and ":", both regexp word-boundary-triggering). The real
	// block-invocation "do" is always the LAST significant token on its
	// line (optionally followed by `|block, params|`), so anchoring "do"
	// at end-of-line via `do\s*(?:\|[^|]*\|)?\s*$` matches
	// `arr.each do |x|` and `loop do` but never `do:` (which is always
	// followed by more code on the same line, not by end-of-line).
	EndSpan: &EndSpanSpec{
		Open:  regexp.MustCompile(`^\s*(?:def|class|module|begin|case)\b|^\s*(?:if|unless)\b|\bdo\s*(?:\|[^|]*\|)?\s*$`),
		Close: regexp.MustCompile(`^\s*end\b`),
	},
}

// rubyCallStop is Ruby's reserved-word list plus "super"/"yield": both look
// exactly like calls (`super(x)`, `yield(x)`) but name a control-flow
// construct, not a symbol this framework could ever mint a node for.
var rubyCallStop = []string{
	"__ENCODING__", "__LINE__", "__FILE__", "BEGIN", "END",
	"alias", "and", "begin", "break", "case", "class", "def", "defined?",
	"do", "else", "elsif", "end", "ensure", "false", "for", "if", "in",
	"module", "next", "nil", "not", "or", "redo", "rescue", "retry",
	"return", "self", "super", "then", "true", "undef", "unless", "until",
	"when", "while", "yield",
}

func init() { registry = append(registry, rubySpec) }
