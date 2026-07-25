package langspec

import (
	"regexp"

	"github.com/jxsl13/spectackle/internal/graph"
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
// parameter list (either closed on the same line, or left open at
// end-of-line for a multi-line signature — R-0005, cspan.Span's own
// multi-line header scan resolves the rest once CallRe is wired below), an
// optional `where` generic constraint clause, and a trailing `{`, an
// expression body ending in `=> ...;` (expression-bodied member), a bare
// `;` (interface/abstract method decl), or nothing at all — Allman style,
// R-0005: idiomatic .NET/Visual-Studio-formatted C# opens the body brace on
// its OWN line, and cspan.Span has handled that placement since T-0053; the
// gap was only ever this Def's own regex requiring `{` on the signature
// line — on the same line. Requiring a modifier keyword is what keeps
// control-flow lines (if/for/while/switch/catch/return, none of which are
// modifier keywords) and field declarations (which never reach a `(`
// before the terminating `;`) from matching, without an explicit negative
// lookahead list. The >=2-space indentation requirement (mirroring
// java.go exactly) is what keeps a top-level primary-constructor record
// declaration (`public record Coord(int X, int Y);`, which also has
// modifier + return-type-shaped token + name + parens + `;`) from
// colliding with the method heuristic: records in this fixture are
// declared unindented.
//
// Three more Defs (R-0005) cover method shapes that are valid C# without
// any modifier keyword at all — deliberately kept to the minimal two-token
// "TYPE NAME(...)" shape, with no modifier-skipping, so they can never
// double-match (nor collide with) a real modifier-bearing method line
// (which always has 3+ tokens before its parens) or a bare control-flow
// line (which only ever has one token before its parens):
//   - explicit interface implementation (`void IPrintable.Print()`): the
//     name segment is dotted, capturing only the method name after the
//     last `.`.
//   - interface member declarations (`double Area();`, ends in a bare `;`
//     with no body — interface members are implicitly public and never
//     carry a modifier).
//   - local functions (`double Square(double v) => v * v;`, expression-
//     bodied, declared inside another method's body — never carries a
//     modifier either).
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
			// `  void Risky() where T : class { ... }`, interface/abstract
			// decls ending in `;`: `  public double Area();`,
			// `  public double Area()` followed by `{` on the next line
			// (Allman), and `  public static async Task<List<int>> FetchAsync(`
			// (multi-line signature, params continue on later lines).
			// Must NOT match field decls (`  private int count;`, no `(`
			// before `;`) or control-flow lines (`if (...) {`, no modifier
			// keyword up front).
			Re:   regexp.MustCompile(`^\s{2,}(?:(?:public|private|protected|internal|static|virtual|override|abstract|sealed|async|extern|partial|new)\s+)+[\w<>\[\],.?]+\s+(\w+)(?:\s*(?:<[\w,\s]+>)?\s*\([^)]*\)\s*(?:where\s+[\w:,\s()\[\]<>.?]+?\s*)*(?:\{\s*$|=>.*;\s*$|;\s*$|$)|\s*\(\s*$)`),
			Name: 1,
		},
		{
			Kind: graph.KFunc,
			// `  public Circle(double radius) {` or Allman
			// `  public Circle(double radius)` / `  {`: a constructor has
			// exactly one identifier (the class's own name) before `(`, no
			// separate return-type token — same disjointness argument as
			// java.go's constructor Def: a real two-token method line has
			// no valid decomposition here, and this Def has no valid
			// decomposition for a two-token method line.
			Re:   regexp.MustCompile(`^\s{2,}(?:(?:public|private|protected|internal)\s+)+(\w+)\([^)]*\)\s*(?:\{\s*$|$)`),
			Name: 1,
		},
		{
			Kind: graph.KFunc,
			// `  void IPrintable.Print()` (explicit interface
			// implementation — R-0005): no modifier keyword by C# grammar
			// rules, but the dotted name (`Interface.Method`) is what
			// disambiguates this from a bare two-token method line; capture
			// group 1 is only the method name after the last `.`.
			Re:   regexp.MustCompile(`^\s{2,}[\w<>\[\],.?]+\s+(?:[\w<>\[\],.?]+\.)+(\w+)\s*\([^)]*\)\s*(?:\{\s*$|=>.*;\s*$|;\s*$|$)`),
			Name: 1,
		},
		{
			Kind: graph.KFunc,
			// `  double Area();` (interface member declaration, no body —
			// R-0005): interface members are implicitly public and never
			// carry a modifier keyword. The bare-`;`-with-no-body ending
			// alone isn't enough to keep this from matching a plain
			// statement of the same two-token shape (`return default(T);`,
			// `await SaveAsync();`) — both are lowercase keywords, so the
			// type token is restricted to csharpTypeToken (a real C# type
			// always starts uppercase or is one of the built-in lowercase
			// keyword types; `return`/`await`/`throw`/`yield`/`new` are
			// not).
			Re:   regexp.MustCompile(`^\s{2,}` + csharpTypeToken + `\s+(\w+)\s*\([^)]*\)\s*;\s*$`),
			Name: 1,
		},
		{
			Kind: graph.KFunc,
			// `    double Square(double v) => v * v;` (local function
			// declared inside another method's body, expression-bodied —
			// R-0005): modern C#'s idiom for scoped helpers, and never
			// carries a modifier keyword either. Same csharpTypeToken
			// restriction as the interface-member Def above, for the same
			// statement-collision reason.
			Re:   regexp.MustCompile(`^\s{2,}` + csharpTypeToken + `\s+(\w+)\s*\([^)]*\)\s*=>.*;\s*$`),
			Name: 1,
		},
	},

	// CallRe/Stop (LSP-001): C# method/constructor bodies are
	// brace-delimited exactly like Java, so the same brace-counted
	// cspan.Span machinery applies directly — including its Allman-brace
	// lookahead (R-0005: csharpSpec previously left CallRe nil, which
	// meant zero call edges, ever, for any C# file).
	CallRe: regexp.MustCompile(`\b([A-Za-z_]\w*)\s*\(`),
	Stop:   csharpCallStop,
}

// csharpCallStop lists C# control-flow keywords whose `name (` syntax
// structurally matches CallRe but is never a call into a repo symbol.
var csharpCallStop = []string{
	"if", "for", "foreach", "while", "switch", "catch", "using", "lock", "return",
}

// csharpTypeToken is a regex fragment matching a token shaped like a real
// C# type: one of the built-in lowercase keyword types, or anything
// starting with an uppercase letter (C#'s overwhelmingly strong PascalCase
// type-naming convention). It exists to keep the two modifier-less
// "TYPE NAME(...)" Defs above (interface member decls, local functions)
// from matching an ordinary two-token statement that happens to share the
// same shape — `return default(T);` or `await SaveAsync();` are both
// "lowercase-keyword identifier(...);", but `return`/`await`/`throw`/
// `yield`/`new` are never real return types, so excluding anything that
// isn't an uppercase-led or known-builtin token rules them out without
// needing RE2 lookahead (which Go's regexp package doesn't support).
const csharpTypeToken = `(?:void|bool|byte|sbyte|char|decimal|double|float|int|uint|long|ulong|short|ushort|object|string|dynamic|[A-Z][\w<>\[\],.?]*)`

func init() { registry = append(registry, csharpSpec) }
