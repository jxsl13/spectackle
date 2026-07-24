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
//
// CallRe stays nil: Fortran bodies are keyword-delimited (`end ...`), not
// brace-delimited, and the framework's call capture is bounded by
// brace-counted body spans (see Spec.CallRe doc) — nothing to bound here.
var fortranSpec = Spec{
	Lang: graph.LangF90,
	Exts: []string{".f90", ".f95", ".f03", ".f08"},
	Qual: QualFileStem,
	Defs: []Def{
		{
			Kind: graph.KFunc,
			// `real function det(m)`, `pure elemental real(kind=8) function f(x)`,
			// `recursive function fib(n) result(r)`
			Re:   regexp.MustCompile(`(?i)^\s*(?:(?:pure|impure|elemental|recursive|module)\s+)*(?:(?:integer|real|logical|character|complex|double\s+precision|type)\s*(?:\([^)]*\))?\s+)?function\s+(\w+)\s*(\([^)]*\))`),
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
			Kind: graph.KType,
			// `module linalg` — but never `module procedure ...` or the
			// `module function/subroutine` submodule forms (those carry more
			// words after the name; `\s*$` pins the bare-module form).
			Re:   regexp.MustCompile(`(?i)^\s*module\s+(\w+)\s*$`),
			Name: 1,
		},
	},
}

func init() { registry = append(registry, fortranSpec) }
