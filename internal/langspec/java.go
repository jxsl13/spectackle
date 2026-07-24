package langspec

import (
	"regexp"

	"github.com/jxsl13/spectacle/internal/graph"
)

// javaSpec covers .java files. Qualification is QualFileStem, mirroring
// pythonSpec and javascriptSpec: one file is treated as one module, so
// `class Foo` in App.java mints "java:App.Foo".
//
// The method Def is a pragmatic single-line heuristic, not a Java grammar:
// it requires at least one leading access/other modifier keyword
// (public/private/protected/static/final/synchronized/abstract) immediately
// after >=2 spaces of indentation, then a return-type token, the method
// name, a parenthesized parameter list, an optional throws clause, and a
// trailing `{` on the same line. Requiring a modifier keyword is what keeps
// control-flow lines (if/for/while/switch/catch/return, none of which are
// modifier keywords) and field declarations (which never reach a `(` before
// the terminating `;`) from matching, without an explicit negative
// lookahead list.
var javaSpec = Spec{
	Lang: graph.LangJava,
	Exts: []string{".java"},
	Qual: QualFileStem,
	Defs: []Def{
		{
			Kind: graph.KType,
			// `public class Foo {`, `interface Shape {`, `enum Color {`,
			// `record Point(int x, int y) {`.
			Re:   regexp.MustCompile(`^\s*(?:public\s+|private\s+|protected\s+|abstract\s+|final\s+|static\s+)*(?:class|interface|enum|record)\s+(\w+)`),
			Name: 1,
		},
		{
			Kind: graph.KFunc,
			// `  public static void main(String[] args) {`,
			// `  private int count() {`, `  public void risky() throws Exception {`.
			// Must NOT match field decls (`  private int count;`, no `(`
			// before `;`) or control-flow lines (`if (...) {`, no modifier
			// keyword up front).
			Re:   regexp.MustCompile(`^\s{2,}(?:public\s+|private\s+|protected\s+|static\s+|final\s+|synchronized\s+|abstract\s+)+[\w<>\[\],.]+\s+(\w+)\s*\([^)]*\)\s*(?:throws\s+[\w.,\s]+)?\{\s*$`),
			Name: 1,
		},
	},
}

func init() { registry = append(registry, javaSpec) }
