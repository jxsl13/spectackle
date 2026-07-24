package langspec

import (
	"regexp"

	"github.com/jxsl13/spectacle/internal/graph"
)

// csharpSpec covers .cs files. Qualification is QualFileStem, mirroring
// pythonSpec and javaSpec: one file is treated as one module, so
// `class Foo` in App.cs mints "cs:App.Foo".
//
// The method Def is a pragmatic single-line heuristic, java.go style: it
// requires >=2 leading whitespace (methods are always nested inside a
// type) then at least one leading modifier keyword (public/private/
// protected/internal/static/virtual/override/abstract/sealed/async/extern/
// partial/new), then a return-type token, the method name, a parenthesized
// parameter list, an optional `where` generic constraint clause, and a
// trailing `{`, an expression body ending in `=> ...;` (expression-bodied
// member), or a bare `;` (interface/abstract method decl) on the same
// line. Requiring a modifier keyword is what keeps control-flow lines
// (if/for/while/switch/catch/return, none of which are modifier keywords)
// and field declarations (which never reach a `(` before the terminating
// `;`) from matching, without an explicit negative lookahead list. The
// >=2-space indentation requirement (mirroring java.go exactly) is what
// keeps a top-level primary-constructor record declaration
// (`public record Coord(int X, int Y);`, which also has modifier +
// return-type-shaped token + name + parens + `;`) from colliding with the
// method heuristic: records in this fixture are declared unindented.
var csharpSpec = Spec{
	Lang: graph.Lang("cs"),
	Exts: []string{".cs"},
	Qual: QualFileStem,
	Defs: []Def{
		{
			Kind: graph.KType,
			// `public class Foo {`, `internal sealed class Bar {`,
			// `interface IShape {`, `struct Point {`, `record Point(int x, int y);`,
			// `enum Color {`.
			Re:   regexp.MustCompile(`^\s*(?:(?:public|private|protected|internal|abstract|sealed|static|partial|readonly|ref)\s+)*(?:class|interface|struct|record|enum)\s+(\w+)`),
			Name: 1,
		},
		{
			Kind: graph.KFunc,
			// `  public static void Main(string[] args) {`,
			// `  private int Count() { ... }`, `  public T Get<T>(int id) { ... }`,
			// `  public int Square(int x) => x * x;`,
			// `  void Risky() where T : class { ... }`, and interface/abstract
			// decls ending in `;`: `  public double Area();`.
			// Must NOT match field decls (`  private int count;`, no `(`
			// before `;`) or control-flow lines (`if (...) {`, no modifier
			// keyword up front).
			Re:   regexp.MustCompile(`^\s{2,}(?:(?:public|private|protected|internal|static|virtual|override|abstract|sealed|async|extern|partial|new)\s+)+[\w<>\[\],.?]+\s+(\w+)\s*(?:<[\w,\s]+>)?\s*\([^)]*\)\s*(?:where\s+[\w:,\s()\[\]<>.?]+?\s*)*(?:\{\s*$|=>.*;\s*$|;\s*$)`),
			Name: 1,
		},
	},
}

func init() { registry = append(registry, csharpSpec) }
