package langspec

import (
	"regexp"

	"github.com/jxsl13/spectacle/internal/graph"
)

// dartSpec covers .dart files. Qualification is QualFileStem, mirroring
// pythonSpec and rustSpec: one file is treated as one library, so `void
// main()` in app.dart mints "dart:app.main".
//
// The two KFunc Defs mirror java.go's method heuristic: a top-level function
// must start at column 0 with a recognized return-type token (void, a
// generic Future<...>/Stream<...>, a capitalized type name, or one of the
// built-in scalar types), while a method requires >=2 spaces of indentation
// (java.go rationale: this is what stops top-level type/record collisions
// and keeps control-flow lines like `if (x) {` — which start with a
// lowercase keyword not present in the return-type alternation — from
// matching either Def).
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
			// Top-level function, column 0: `void main() {`,
			// `Future<int> load() async {`, `Stream<int> ticks() sync* {}`.
			Re:   regexp.MustCompile(`^(?:void|Future<[^>]*>|Stream<[^>]*>|[A-Z]\w*(?:<[^>]*>)?|int|double|bool|String|num|dynamic)\??\s+(\w+)\s*\([^)]*\)\s*(?:async\*?|sync\*)?\s*\{`),
			Name: 1,
		},
		{
			Kind: graph.KFunc,
			// Method, >=2-space indent: `  void speak() {`,
			// `  static void reset() {}`, `  @override String toString() {}`.
			Re:   regexp.MustCompile(`^\s{2,}(?:static\s+|@override\s+)*(?:void|Future<[^>]*>|Stream<[^>]*>|[A-Z]\w*(?:<[^>]*>)?|int|double|bool|String|num|dynamic)\??\s+(\w+)\s*\([^)]*\)\s*(?:async\*?|sync\*)?\s*\{`),
			Name: 1,
		},
	},
}

func init() { registry = append(registry, dartSpec) }
