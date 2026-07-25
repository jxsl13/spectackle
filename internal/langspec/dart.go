package langspec

import (
	"regexp"

	"github.com/jxsl13/spectackle/internal/graph"
)

// dartSpec covers .dart files. Qualification is QualFileStem, mirroring
// pythonSpec and rustSpec: one file is treated as one library, so `void
// main()` in app.dart mints "dart:app.main".
//
// The KFunc Defs mirror java.go's method heuristic: a top-level function
// must start at column 0 with a recognized return-type token (void, a
// generic Future<...>/Stream<...>, a capitalized type name, or one of the
// built-in scalar types), while a method requires >=2 spaces of indentation
// (java.go rationale: this is what stops top-level type/record collisions
// and keeps control-flow lines like `if (x) {` — which start with a
// lowercase keyword not present in the return-type alternation — from
// matching either Def).
//
// R-0005 hardening: dartReturnType-prefixed function/method Defs only ever
// matched a signature closed on one physical line with a trailing `{`
// (K&R). That missed, structurally: arrow-bodied (`=>`) functions/methods/
// getters (no trailing `{` at all — dartfmt's idiomatic getter/one-liner
// style); multi-line parameter lists (dartfmt's idiomatic trailing-comma
// style, where `(...)  {` never appears together on one physical line);
// abstract/interface method declarations (semicolon-terminated, no body);
// generic methods (`<T>` between the name and its `(`); constructors
// (unnamed and named — no return-type token precedes a constructor's
// callable name at all, so neither KFunc Def's return-type-token
// requirement could ever fire); and operator overload declarations
// (`operator ==` is captured into the name group, then `==` itself sits
// where `(` is required). All are fixed below, alongside CallRe/Stop so
// call edges and real brace-counted body spans exist for Dart for the
// first time (LSP-001) — arrow bodies have no braces to span, so their
// EndLine stays equal to Line by cspan.Span's own documented "no body"
// bail-out for a `;`-terminated def line, same as a C prototype.
var dartSpec = Spec{
	Lang: graph.LangDart,
	Exts: []string{".dart"},
	Qual: QualFileStem,
	Defs: []Def{
		{
			Kind: graph.KType,
			// `class Foo {`, `abstract class Foo {`, `mixin Bar {`,
			// `enum Color {`, `extension StringExt on String {`.
			Re:   regexp.MustCompile(`^\s*(?:(?:abstract|final|sealed|base|interface|mixin)\s+)*(?:class|mixin|enum|extension)\s+(\w+)`),
			Name: 1,
		},
		{
			Kind: graph.KFunc,
			// Top-level function, column 0, braced or semicolon/arrow
			// body, single- or multi-line signature: `void main() {`,
			// `Future<int> load() async {`, `int computeSum(int a, int b)
			// => a + b;` (arrow, R-0005), `Future<void> fetchData(` with
			// the parameter list and closing `)  async {` continuing on
			// later lines (multi-line signature, R-0005: cspan.Span's own
			// multi-line-header scan resolves the rest once CallRe below
			// is wired), and a generic name (`List<T> firstOf<T>(...)`,
			// R-0005: optional `(?:<[^>]*>)?` between the name and `(`).
			Re:   regexp.MustCompile(dartTopLevelFunc),
			Name: 1,
		},
		{
			Kind: graph.KFunc,
			// Method, >=2-space indent — same shapes as the top-level
			// function Def above, plus an abstract/interface method
			// declaration with no body at all (`double area();`,
			// R-0005): `  void speak() {`, `  static void reset() {}`,
			// `  @override String toString() {}`, `  List<T>
			// mapItems<T>(List<T> input) {` (generic method, R-0005).
			Re:   regexp.MustCompile(`^\s{2,}(?:static\s+|@override\s+)*` + dartMethodTail),
			Name: 1,
		},
		{
			Kind: graph.KFunc,
			// Getter accessor, braced or arrow body, no parameter list at
			// all (R-0005 — the two Defs above require a `(...)` param
			// list, which a getter structurally never has): `  String get
			// description => '$name ($age)';`, `  int get hashCode =>
			// name.hashCode;`, `  int get value { return _value; }`.
			// Top-level getters (rare but legal Dart) match too, since
			// `\s{0,}` here is `\s*`, unlike the indented-only method Def.
			Re:   regexp.MustCompile(`^\s*(?:static\s+)?` + dartReturnType + `\s+get\s+(\w+)\s*(?:\{.*$|=>.*;\s*$)`),
			Name: 1,
		},
		{
			Kind: graph.KFunc,
			// Setter accessor (R-0005 — Dart setters carry no return-type
			// token at all, so neither the method nor the getter Def
			// above can match): `  set apiRoot(String url) { ... }`,
			// `  set apiRoot(String url) => ...;`.
			Re:   regexp.MustCompile(`^\s{2,}(?:static\s+)?set\s+(\w+)\(([^)]*)\)?\s*(?:\{.*$|=>.*;\s*$|;\s*$|$)`),
			Name: 1,
		},
		{
			Kind: graph.KFunc,
			// Constructor, unnamed or named (R-0005): `Animal(this.name,
			// this.age);` (unnamed/default — captures the class name
			// itself), `Animal.baby(String name) : this(name, 0);` (named
			// — the optional `(?:\.\w+)?` captures the whole
			// `Class.name` form as one Name group), `Box();` (unnamed, no
			// params). No return-type token precedes a constructor's
			// name — that absence is exactly what stops this Def from
			// ever double-matching a real "TYPE name(...)" method line
			// (which always has *two* identifiers before `(`, not one).
			// Requires the class-like identifier to start with a capital
			// letter (Dart's near-universal type-naming convention) so a
			// local variable assignment on the same line shape,
			// `final a = Animal('Rex', 3);`, is excluded structurally:
			// the line starts with lowercase `final`, not a capital
			// letter, at the very position this Def anchors to.
			Re:   regexp.MustCompile(`^\s{2,}([A-Z]\w*(?:\.\w+)?)\(([^)]*)\)?\s*(?:\{.*$|:.*;\s*$|;\s*$|$)`),
			Name: 1,
		},
		{
			Kind: graph.KFunc,
			// Operator overload declaration (R-0005): `bool operator
			// ==(Object other) { ... }`. Previously `operator` itself was
			// captured into the name group by the plain method Def, then
			// the operator symbol (`==`) sat where `(` was required, so
			// the whole match failed. Name captures the bare symbol
			// (`==`), mirroring ruby.go's operator Defs, not `operator`
			// itself.
			Re:   regexp.MustCompile(`^\s{2,}` + dartReturnType + `\s+operator\s*(==|!=|<=|>=|~/|<<|>>|\[\]=|\[\]|[-+*/%<>~&|^])\s*\(([^)]*)\)\s*\{.*$`),
			Name: 1,
		},
	},

	// CallRe/Stop (LSP-001): Dart function/method bodies are
	// brace-delimited exactly like C/Java, so the same brace-counted
	// cspan.Span machinery applies directly. Previously dartSpec left
	// CallRe nil entirely, which per langspec.go's documented default
	// meant zero call/launch edges, ever, for any Dart file, and every
	// node's EndLine collapsed to its Line (no real body span) since the
	// brace-counting path is only reached when CallRe is set.
	CallRe: regexp.MustCompile(`\b([A-Za-z_]\w*)\s*\(`),
	Stop:   dartCallStop,
}

// dartReturnType is a regex fragment matching a token shaped like a real
// Dart return type: `void`, a generic `Future<...>`/`Stream<...>`, a
// built-in scalar type, or anything starting with an uppercase letter
// (Dart's own strong PascalCase type-naming convention, same rationale as
// csharp.go's csharpTypeToken). The trailing `\??` allows a nullable type
// (`String?`).
const dartReturnType = `(?:void|Future<[^>]*>|Stream<[^>]*>|[A-Z]\w*(?:<[^>]*>)?|int|double|bool|String|num|dynamic)\??`

// dartMethodTail is the shared "name, optional generics, params, optional
// async/sync modifier, body-or-declaration ending" tail glued onto
// dartReturnType for both the top-level function Def and the (differently
// indented/modifier-prefixed) method Def. The params group tolerates a
// multi-line signature left open at end-of-line (`\(([^)]*)\)?`, no closing
// `)` required on this line — cspan.Span's own multi-line header scan
// resolves the rest once CallRe is set), and the final alternation accepts
// a same-line brace body (`{...`), an arrow body (`=> expr;`), a bare
// semicolon (abstract/interface declaration, no body), or nothing further
// on the line at all (open multi-line signature).
const dartMethodTail = dartReturnType + `\s+(\w+)(?:<[^>]*>)?\(([^)]*)\)?\s*(?:async\*?|sync\*)?\s*(?:\{.*$|=>.*;\s*$|;\s*$|$)`

// dartTopLevelFunc anchors dartMethodTail at column 0 (no leading
// whitespace) for top-level function declarations, mirroring the pre-R-0005
// Def's own anchoring.
const dartTopLevelFunc = `^` + dartMethodTail

// dartCallStop lists Dart control-flow keywords whose `name (` syntax
// structurally matches CallRe but is never a call into a repo symbol.
var dartCallStop = []string{
	"if", "for", "while", "switch", "catch", "return",
}

func init() { registry = append(registry, dartSpec) }
