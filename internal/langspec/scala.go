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
// (private/protected/override/final/implicit/inline/lazy) before `def`.
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
			Re:   regexp.MustCompile(`^\s*(?:(?:private|protected|override|final|implicit|inline|lazy)\s+)*def\s+(\w+)`),
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
	},
}

func init() { registry = append(registry, scalaSpec) }
