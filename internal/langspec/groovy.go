package langspec

import (
	"regexp"

	"github.com/jxsl13/spectackle/internal/graph"
)

// groovySpec covers .groovy files. Qualification is QualFileStem, mirroring
// pythonSpec and rustSpec: one file is treated as one script/module, so `def
// run` in App.groovy mints "groovy:App.run".
//
// Two KFunc Defs cover Groovy's two method-declaration styles: dynamically
// typed (`def name(...)`, no modifier required) and statically typed,
// modifier-led (java.go shape: `[\w<>\[\],.]+\s+(\w+)\s*\([^)]*\)\s*\{\s*$`
// after one-or-more required modifier keywords). Requiring a modifier
// keyword on the typed-method Def is what keeps a field decl (which never
// reaches a `(` before end of line) and control-flow lines (`if (...) {`,
// no modifier keyword) from matching, without an explicit negative
// lookahead list — same rationale as java.go.
var groovySpec = Spec{
	Lang: graph.LangGroovy,
	Exts: []string{".groovy"},
	Qual: QualFileStem,
	Defs: []Def{
		{
			Kind: graph.KFunc,
			// `def run() {`, `static def run(x) {}`, `private def helper(x) {}`.
			Re:   regexp.MustCompile(`^\s*(?:(?:public|private|protected|static|final|synchronized)\s+)*def\s+(\w+)\s*\(`),
			Name: 1,
		},
		{
			Kind: graph.KFunc,
			// `  public String greet(String name) {`, `  static void run() {}`.
			// Requires >=1 modifier keyword (java.go shape) to stay clear of
			// field decls and control-flow lines.
			Re:   regexp.MustCompile(`^\s{2,}(?:(?:public|private|protected|static|final|synchronized)\s+)+[\w<>\[\],.]+\s+(\w+)\s*\([^)]*\)\s*\{\s*$`),
			Name: 1,
		},
		{
			Kind: graph.KType,
			// `class Foo {`, `abstract class Foo {`, `interface Shape {}`,
			// `trait Flyable {}`, `enum Color {}`.
			Re:   regexp.MustCompile(`^\s*(?:(?:public|abstract|final)\s+)*(?:class|interface|trait|enum)\s+(\w+)`),
			Name: 1,
		},
	},
}

func init() { registry = append(registry, groovySpec) }
