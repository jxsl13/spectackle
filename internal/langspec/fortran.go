package langspec

import (
	"regexp"

	"github.com/jxsl13/spectackle/internal/graph"
)

// fortranSpec covers free-form Fortran (.f90/.f95/.f03/.f08). Qualification
// is QualFileStem: solver.f90's `subroutine step` mints "f90:solver.step".
// Fortran is case-insensitive, so every Def is (?i).
//
// The end-keyword trap (see docs/cookbook-new-language.md): `end function
// foo` / `end subroutine bar` would match a naive `function\s+(\w+)` regex.
// Go regexp has no lookahead, so instead of excluding "end" explicitly the
// Defs anchor the match at line start through an alternation of the ONLY
// legal prefix words (attributes and type declarations) — "end" is not in
// that set, so an end-line can never reach the `function`/`subroutine`
// keyword literal.
var fortranSpec = Spec{
	Lang: graph.LangF90,
	Exts: []string{".f90", ".f95", ".f03", ".f08"},
	Qual: QualFileStem,
	Defs: []Def{
		{
			Kind: graph.KFunc,
			// `real function det(m)`, `pure elemental real(kind=8) function f(x)`,
			// `recursive function fib(n) result(r)`. The Sig group
			// `(\([^)]*\))?` is now OPTIONAL (was mandatory): a
			// continuation-line argument list wrapped with `&`
			// (`function total_energy(mass, velocity, &` / next line
			// `height) result(te)`) has no closing `)` on the def line at
			// all, so a mandatory group made the WHOLE regex fail to
			// match — the function was invisible, not just missing its
			// Sig. With the group optional, the name still mints (Sig
			// simply comes back empty for a wrapped signature); the
			// framework scans one line at a time
			// (internal/langspec/langspec.go), so genuinely recovering the
			// wrapped Sig text itself would need a multi-line rewrite of
			// the Def-matching loop, out of this file's scope.
			Re:   regexp.MustCompile(`(?i)^\s*(?:(?:pure|impure|elemental|recursive|module)\s+)*(?:(?:integer|real|logical|character|complex|double\s+precision|type)\s*(?:\([^)]*\))?\s+)?function\s+(\w+)\s*(\([^)]*\))?`),
			Name: 1,
			Sig:  2,
		},
		{
			Kind: graph.KFunc,
			// `subroutine step(dt)`, `pure subroutine reset` (parens optional)
			Re:   regexp.MustCompile(`(?i)^\s*(?:(?:pure|impure|elemental|recursive|module)\s+)*subroutine\s+(\w+)\s*(\([^)]*\))?`),
			Name: 1,
			Sig:  2,
		},
		{
			Kind: graph.KFunc,
			// `program main ... end program main` — the top-level
			// executable unit; deliberately KFunc (not KType) so its body
			// gets EndSpan-computed and scanned for call edges the same
			// way a subroutine's does (a program calls other
			// functions/subroutines directly in its body, exactly like
			// this fixture's `program main` calling `total_energy`/`step`).
			// The end-keyword trap doesn't apply here the way it does to
			// function/subroutine: "program" never appears as a legal
			// prefix word before another keyword the way "module" does
			// (there is no "program procedure ..." form), so a plain
			// anchored match is enough.
			Re:   regexp.MustCompile(`(?i)^\s*program\s+(\w+)`),
			Name: 1,
		},
		{
			Kind: graph.KType,
			// `module linalg` — but never `module procedure ...` or the
			// `module function/subroutine` submodule forms (those carry more
			// words after the name; `\s*$` pins the bare-module form).
			Re:   regexp.MustCompile(`(?i)^\s*module\s+(\w+)\s*$`),
			Name: 1,
		},
		{
			Kind: graph.KType,
			// Derived type declarations: `type :: particle` /
			// `type, extends(base) :: particle`. Requires the `::`
			// separator, which is exactly what distinguishes a TYPE
			// DEFINITION from a variable declared to be of a derived type
			// (`type(particle) :: p`, which has `(` glued directly onto
			// "type" with no `::` reachable before it — the alternation of
			// optional `, attr` groups here can only ever be followed by
			// `::`, never by `(`, so that declaration form can never match
			// this Def at all).
			Re:   regexp.MustCompile(`(?i)^\s*type\b(?:\s*,\s*\w+(?:\([^)]*\))?)*\s*::\s*(\w+)`),
			Name: 1,
		},
		{
			Kind: graph.KType,
			// Named interface blocks (generic procedure dispatch), e.g.
			// `interface norm ... end interface norm`. An anonymous
			// `interface` (no name) never matches: `\s+(\w+)` requires at
			// least one name character, and a bare "interface" line has
			// nothing after it for `\s+` to consume.
			Re:   regexp.MustCompile(`(?i)^\s*interface\s+(\w+)`),
			Name: 1,
		},
	},

	// CallRe/Stop (LSP-001): any `name(` inside a def's keyword-counted body
	// span is a call edge unless it's the def's own name or a Stop-listed
	// keyword/type-spec/attribute word. Fortran syntactically reuses `(...)`
	// for three unrelated things — real calls (`kinetic_energy(mass, v)`),
	// type-spec/attribute parameters (`real(kind=8)`, `intent(in)`), and
	// derived-type component/array access (`self%velocity(1)`) — a
	// line-oriented regex cannot tell these apart the way a real compiler's
	// symbol table can. Stop below removes every type-spec/attribute/
	// control keyword actually seen adjacent to `(` in this fixture's real
	// bodies (`real(`, `intent(`, `class(`, `type(`, `if(`); the
	// component/array-access case (`self%velocity(1)` producing a dangling
	// "velocity" edge) is a known, structural limitation shared with every
	// other langspec Spec's syntactic call detection (cf. C's tolerance of
	// dangling printf/setmetatable-style edges) and is not chased further.
	CallRe: regexp.MustCompile(`\b([A-Za-z_]\w*)\s*\(`),
	Stop:   fortranCallStop,

	// EndSpan (T-0119): Fortran has no braces, so the body is bounded by
	// keyword-counting instead (cspan.KeywordSpan). Open counts
	// function/subroutine/program (the three KFunc-kinded constructs whose
	// bodies actually get spanned) plus the control-flow constructs that
	// can nest inside their bodies (block-if/do/select case); Close is a
	// single generic `^\s*end\b`, matching every Fortran end-form this
	// fixture uses ("end if", "end do", "end function NAME",
	// "end subroutine NAME", "end program NAME") — Fortran requires the
	// prior open's keyword to reappear after "end", but for pure
	// depth-counting purposes only the COUNT of opens vs. closes matters,
	// not which keyword closes which; a generic "end" is sufficient and
	// keeps this Close identical to Ruby/Julia's.
	//
	// The end-keyword trap and the modifier-if analogue, together: the
	// function/subroutine Open patterns mirror their Def regexes'
	// attribute-prefix skipping exactly, so "end function foo" can never
	// reach the "function" literal the same way it can't in the Defs
	// above. The if-Open requires a trailing "then" (`^\s*if\s*\(.*\)\s*then\b`):
	// Fortran's other legal if-form, the one-line unblocked statement
	// (`if (n < 2) r = n`, no "then", no matching "end if"), would be a
	// false open without that anchor — this is Fortran's equivalent of
	// Ruby's modifier-if trap, a single-line conditional with no block to
	// close. `else if (...) then` is correctly NOT double-counted: that
	// line starts with "else", not "if", so the `^\s*if` anchor fails on
	// it (the original if's "end if" closes the whole cascade, exactly as
	// Fortran's grammar intends).
	EndSpan: &EndSpanSpec{
		Open: regexp.MustCompile(`(?i)^\s*(?:(?:pure|impure|elemental|recursive|module)\s+)*(?:(?:integer|real|logical|character|complex|double\s+precision|type)\s*(?:\([^)]*\))?\s+)?function\b` +
			`|^\s*(?:(?:pure|impure|elemental|recursive|module)\s+)*subroutine\b` +
			`|^\s*program\s+\w+` +
			`|^\s*if\s*\(.*\)\s*then\b` +
			`|^\s*do\b` +
			`|^\s*select\s+case\b`),
		Close: regexp.MustCompile(`(?i)^\s*end\b`),
	},
}

// fortranCallStop is the set of type-spec, attribute, and control-flow
// keywords whose `word(`/`word (` syntax structurally matches CallRe but is
// never a call into a repo symbol — not an exhaustive list of every Fortran
// keyword, just every one confirmed adjacent to `(` in this language's real
// fixture bodies (declarations like `real(kind=8), intent(in) :: mass`,
// `class(particle), intent(in) :: self`, `type(particle) :: p`, and
// block-if's `if (n < 2) then`), plus the most common others in the same
// families for defensive coverage.
var fortranCallStop = []string{
	"if", "then", "else", "elseif", "end", "do", "while", "select", "case",
	"where", "forall", "associate", "block",
	"function", "subroutine", "program", "module", "type", "interface",
	"result", "contains", "implicit", "use", "call", "print",
	"real", "integer", "logical", "character", "complex", "class",
	"kind", "len", "dimension", "allocatable", "pointer", "parameter",
	"public", "private", "intent", "optional", "save", "target", "value",
	"in", "out", "inout", "common", "external", "intrinsic",
}

func init() { registry = append(registry, fortranSpec) }
