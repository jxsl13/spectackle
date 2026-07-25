package langspec

import (
	"regexp"

	"github.com/jxsl13/spectackle/internal/graph"
)

// scalaSpec covers .scala files. Qualification is QualFileStem, mirroring
// pythonSpec and javaSpec: one file is treated as one module, so `def run`
// in App.scala mints "scala:App.run".
//
// Unlike C#/Java, Scala's `def` keyword is unambiguous (control-flow
// constructs like if/for/while/match never start with `def`), so the
// method Def needs no modifier-keyword requirement to exclude non-methods
// — it only needs to skip past optional leading modifiers
// (private/protected/override/final/implicit/inline/lazy) before `def`. An
// optional `(param, list)` right after the name is captured into Sig
// (R-0005: no Def previously set Sig, so Node.Sig was always empty); a
// paramless def (`def area: Double`) simply leaves Sig empty, same as
// before, and a multi-line signature (params wrapping past the def line)
// also leaves Sig empty since only the def line itself is scanned — a
// line-scanner limitation shared with every other langspec Sig capture
// (see glsl.go).
//
// Likewise `case class`/`case object` are captured by allowing `case` as
// one of the optional leading modifiers on the type Def.
var scalaSpec = Spec{
	Lang: graph.Lang("scala"),
	Exts: []string{".scala"},
	Qual: QualFileStem,
	Defs: []Def{
		{
			Kind: graph.KFunc,
			// `def run(x: Int): Int = x`, `  private def helper() = {`,
			// `override def toString: String = "x"`.
			Re:   regexp.MustCompile(`^\s*(?:(?:private|protected|override|final|implicit|inline|lazy)\s+)*def\s+(\w+)\s*(?:\(([^)]*)\))?`),
			Name: 1,
			Sig:  2,
		},
		{
			Kind: graph.KFunc,
			// Scala 3 single-line extension method, e.g.
			// `extension (x: Int) def triple: Int = x * 3` (R-0005: the
			// plain `def` Def above is ^-anchored and requires `def` to be
			// the first token after optional modifiers, so it never matches
			// when `extension (...)` precedes `def` on the same line). The
			// multi-line form (`extension (x: Int)` on its own line,
			// `def double: Int = ...` on the next) already matches the
			// plain `def` Def above without help, since that following line
			// is itself an ordinary ^-anchored `def` line.
			Re:   regexp.MustCompile(`^\s*extension\s*\([^)]*\)\s*def\s+(\w+)`),
			Name: 1,
		},
		{
			Kind: graph.KFunc,
			// A `val` bound to a lambda literal — Scala's idiomatic
			// function-value shape, e.g. `val square = (x: Int) => x * x`
			// (R-0005). Requiring `= (` immediately (optionally
			// whitespace-separated) before `=>` is what keeps a plain value
			// binding like `val x: Int = 1` from matching: there is no
			// parenthesized-lambda shape to find.
			Re:   regexp.MustCompile(`^\s*(?:(?:private|protected|override|final|implicit|lazy)\s+)*val\s+(\w+)\s*(?::[^=]+)?=\s*\([^)]*\)\s*=>`),
			Name: 1,
		},
		{
			Kind: graph.KType,
			// `class Foo {`, `case class Point(x: Int, y: Int)`,
			// `object Foo {`, `case object Empty`, `trait Shape {`,
			// `sealed trait Shape`, `enum Color { ... }`.
			Re:   regexp.MustCompile(`^\s*(?:(?:private|protected|abstract|final|sealed|implicit|case)\s+)*(?:class|object|trait|enum)\s+(\w+)`),
			Name: 1,
		},
		{
			Kind: graph.KType,
			// Scala 3 given/typeclass-instance declaration, e.g.
			// `given intOrdering: Ordering[Int] with` (R-0005: previously
			// only the nested `def` inside the given block was captured,
			// never the given declaration itself).
			Re:   regexp.MustCompile(`^\s*given\s+(\w+)\s*:`),
			Name: 1,
		},
	},

	// CallRe/Stop (LSP-001): Scala's brace-bodied defs/classes (the
	// idiomatic majority, and 100% of this Spec's own test fixtures) are
	// brace-delimited exactly like C/C++/Java, so the same brace-counted
	// cspan.Span machinery applies directly (R-0005: scalaSpec previously
	// left CallRe nil, which zeroed both call edges and EndLine spans for
	// every Scala KFunc node). Scala 3's colon/indentation-based bodies
	// (`trait Shape:` with no braces) have nothing for cspan.Span to find —
	// Span's own Allman-lookahead simply returns ok=false for those lines
	// (no behavior change from before CallRe existed), which is the
	// documented brace-language framework behavior, not a Scala-specific
	// special case.
	CallRe: regexp.MustCompile(`\b([A-Za-z_]\w*)\s*\(`),
	Stop:   scalaCallStop,
}

// scalaCallStop lists Scala control-flow keywords whose `name (` syntax
// structurally matches CallRe but is never a call into a repo symbol.
var scalaCallStop = []string{
	"if", "for", "while", "catch", "synchronized", "return",
}

func init() { registry = append(registry, scalaSpec) }
