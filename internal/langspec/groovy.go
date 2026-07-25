package langspec

import (
	"regexp"

	"github.com/jxsl13/spectackle/internal/graph"
)

// groovySpec covers .groovy files. Qualification is QualFileStem, mirroring
// pythonSpec and rustSpec: one file is treated as one script/module, so `def
// run` in App.groovy mints "groovy:App.run".
//
// Groovy's typed-method Def deliberately does NOT require a modifier
// keyword (unlike java.go/csharp.go): Groovy's default (unspecified)
// visibility is `public`, so a modifier-less typed method
// (`boolean fly() { ... }`) is the idiomatic common case, not an edge case
// (R-0005) — trait/interface default-body methods and Gradle/Jenkinsfile-
// style scripts routinely omit it. What still keeps field decls
// (`private String prefix`, never reaches a `(` before end of line) and
// control-flow lines (`if (...) {`, a single token before `(`) from
// matching is the *shape*: a real typed method always has two tokens
// (return type + name) before `(`, which if/for/while/switch/catch never
// do — same disjointness argument as the constructor Def below.
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
			// `  public String greet(String name) {`, `  static void run() {}`,
			// `  boolean fly() {` (no modifier — Groovy's default visibility,
			// R-0005), `  public boolean refund(` (multi-line signature,
			// param list and brace continue on later lines — R-0005). The
			// return-type token is deliberately NOT the plain
			// `[\w<>\[\],.]+` java.go/csharp.go use: it excludes matching
			// the bare 3-letter token `def` exactly (Go's RE2 has no
			// lookahead, hence the character-by-character alternation)
			// while still allowing any real type name that merely starts
			// with "de"/"def" (double, decimal, default, ...). Without this,
			// making the modifier optional above would let this Def
			// double-match every dynamically typed `def name(...)` line
			// already covered by the def-based Def, with "def" parsed as
			// if it were the return type. A bare, body-less declaration
			// shape (`double area()`, no trailing `{` or `;`) is
			// deliberately NOT matched here: it is structurally identical
			// to a `command arg()` statement (e.g. Groovy's parenless
			// `println foo()`), which an existing negative-line guard
			// depends on staying unmatched — a [medium] finding left
			// un-chased because closing it would resurrect that false
			// positive.
			Re:   regexp.MustCompile(`^\s{2,}(?:(?:public|private|protected|static|final|synchronized)\s+)*(?:[^d\s][\w<>\[\],.]*|d[^e\s][\w<>\[\],.]*|de[^f\s][\w<>\[\],.]*|def[\w<>\[\],.]+)\s+(\w+)\s*\((?:[^)]*\)\s*\{\s*$|\s*$)`),
			Name: 1,
		},
		{
			Kind: graph.KFunc,
			// `  public PaymentService(String id) {` (constructor: same
			// token pattern as a typed method but with no explicit
			// return-type token before the name — R-0005). Exactly one
			// identifier between the optional modifiers and `(`, with NO
			// whitespace allowed before `(`, is what makes this disjoint
			// from both the two-token typed-method Def above (`boolean
			// fly()` has two tokens before `(`) and from a control-flow
			// line (`if (prefix) {` always has a space before `(`, unlike
			// every real call/constructor/method — a bare `\s*` here would
			// let `if (` slip through as if `if` were a one-token
			// constructor name). Unlike the typed-method Def, this one
			// deliberately does NOT accept a bare trailing `(` (multi-line
			// signature): with only a single captured token and no
			// modifier required, that shape is indistinguishable from an
			// ordinary multi-line function call statement
			// (`foo(\n  bar,\n  baz\n)`), so it's left unmatched rather than
			// risk false positives.
			Re:   regexp.MustCompile(`^\s{2,}(?:(?:public|private|protected|static|final|synchronized)\s+)*(\w+)\([^)]*\)\s*\{\s*$`),
			Name: 1,
		},
		{
			Kind: graph.KType,
			// `class Foo {`, `abstract class Foo {`, `interface Shape {}`,
			// `trait Flyable {}`, `enum Color {}`,
			// `static class Inner {` (static nested class — R-0005:
			// `static` was missing from the modifier alternation, so the
			// whole ^-anchored regex failed on it).
			Re:   regexp.MustCompile(`^\s*(?:(?:public|abstract|final|static)\s+)*(?:class|interface|trait|enum)\s+(\w+)`),
			Name: 1,
		},
	},

	// CallRe/Stop (LSP-001): Groovy method/constructor bodies are
	// brace-delimited exactly like Java, so the same brace-counted
	// cspan.Span machinery applies directly (R-0005: groovySpec previously
	// left CallRe nil, which meant zero call edges, ever, for any Groovy
	// file, regardless of how idiomatically the calls were written).
	CallRe: regexp.MustCompile(`\b([A-Za-z_]\w*)\s*\(`),
	Stop:   groovyCallStop,
}

// groovyCallStop lists Groovy control-flow keywords whose `name (` syntax
// structurally matches CallRe but is never a call into a repo symbol.
var groovyCallStop = []string{
	"if", "for", "while", "switch", "catch", "synchronized", "return",
}

func init() { registry = append(registry, groovySpec) }
