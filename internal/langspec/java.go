package langspec

import (
	"regexp"

	"github.com/jxsl13/spectackle/internal/graph"
)

// javaSpec covers .java files. Qualification is QualFileStem, mirroring
// pythonSpec and javascriptSpec: one file is treated as one module, so
// `class Foo` in App.java mints "java:App.Foo".
//
// The method Def is a pragmatic single-line heuristic, not a Java grammar:
// it requires at least one leading access/other modifier keyword
// (public/private/protected/static/final/synchronized/abstract/default)
// immediately after >=2 spaces of indentation, then an optional generic
// type-parameter token (`<T>`), a return-type token, the method name, a
// parenthesized parameter list (either closed on the same line, or left
// open at end-of-line for a multi-line signature — cspan.Span's own
// multi-line header scan resolves the rest once CallRe is wired below), an
// optional throws clause, and a trailing `{` on the same line (when the
// parameter list closes there). Requiring a modifier keyword is what keeps
// control-flow lines (if/for/while/switch/catch/return, none of which are
// modifier keywords) and field declarations (which never reach a `(` before
// the terminating `;`) from matching, without an explicit negative
// lookahead list. `default` is included so interface default methods
// (R-0005: `default String hello() { ... }`) are recognized (Java's
// `default` in a switch-case is a bare label with no method shape after
// it, so it can't false-positive here).
//
// The constructor Def is a second, narrower heuristic: same modifier/
// indentation requirements, but exactly one identifier (the constructor's
// name, conventionally the class name) before the parameter list — a
// regular method always has a return-type token *and* a name, so the two
// Defs are structurally disjoint and never double-match the same line
// (R-0005: `public Calculator(int base) { ... }`).
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
			// `  private int count() {`, `  public void risky() throws Exception {`,
			// `  public <T> T identity(T value) {` (generic method),
			// `  default String hello() { ... }` (interface default method),
			// `  public int longSum(` (multi-line signature, param list and
			// brace continue on later lines).
			// Must NOT match field decls (`  private int count;`, no `(`
			// before `;`) or control-flow lines (`if (...) {`, no modifier
			// keyword up front).
			Re:   regexp.MustCompile(`^\s{2,}(?:public\s+|private\s+|protected\s+|static\s+|final\s+|synchronized\s+|abstract\s+|default\s+)+(?:<[^>]*>\s+)?[\w<>\[\],.]+\s+(\w+)\s*\((?:[^)]*\)\s*(?:throws\s+[\w.,\s]+)?\{\s*$|\s*$)`),
			Name: 1,
		},
		{
			Kind: graph.KFunc,
			// `  public Calculator(int base) {`, `  public Node(int value) {`
			// (nested class constructor). Exactly one identifier (no
			// separate return-type token) between the modifiers and `(`.
			Re:   regexp.MustCompile(`^\s{2,}(?:public\s+|private\s+|protected\s+)+(\w+)\s*\((?:[^)]*\)\s*\{\s*$|\s*$)`),
			Name: 1,
		},
	},

	// CallRe/Stop (LSP-001): Java is brace-delimited exactly like C/C++, so
	// the same brace-counted cspan.Span body-bounding and call-edge
	// machinery applies directly (R-0005: javaSpec previously left CallRe
	// nil, which silently zeroed both call edges and EndLine spans for
	// every Java KFunc node — the same single missing field broke both
	// halves of the impact-BFS use case).
	CallRe: regexp.MustCompile(`\b([A-Za-z_]\w*)\s*\(`),
	Stop:   javaCallStop,
}

// javaCallStop lists Java control-flow keywords whose `name (` syntax
// structurally matches CallRe but is never a call into a repo symbol.
var javaCallStop = []string{
	"if", "for", "while", "switch", "catch", "synchronized", "return",
}

func init() { registry = append(registry, javaSpec) }
