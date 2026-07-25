package langspec

import (
	"regexp"

	"github.com/jxsl13/spectackle/internal/graph"
)

// kotlinSpec covers .kt and .kts files. Qualification is QualFileStem,
// mirroring pythonSpec and rustSpec: one file is treated as one module, so
// `fun run` in App.kt mints "kt:App.run".
var kotlinSpec = Spec{
	Lang: graph.Lang("kt"),
	Exts: []string{".kt", ".kts"},
	Qual: QualFileStem,
	Defs: []Def{
		{
			Kind: graph.KFunc,
			// `fun run(x: Int): Int {`, `public suspend fun fetch() {}`,
			// `private inline fun <T> identity(x: T): T = x`, and extension
			// functions with a receiver type before the name
			// (`fun String.shout(): String {`, `fun <T> List<T>.second(): T
			// {`) — the optional non-capturing receiver group consumes the
			// `Type.`/`Type<T>.` prefix so group 1 captures the real
			// function name, not the receiver (R-0005: previously
			// `fun\s+(\w+)` greedily captured "String"/"List", the receiver
			// type, since `.` isn't a word character). The optional generic
			// parameter list (`<T>`) sits between `fun` and the receiver/
			// name. Requiring `(` right after the captured name (present in
			// every Kotlin function, even a parameterless one) is what
			// disambiguates the name from the receiver: a plain `fun
			// run(...)` has no `.`, so the receiver group naturally matches
			// zero-width and group 1 falls through to "run".
			Re:   regexp.MustCompile(`^\s*(?:(?:public|private|protected|internal|open|override|suspend|inline|operator|infix|tailrec|external|abstract|final)\s+)*fun\s+(?:<[^>]*>\s+)?(?:[\w?]+(?:<[^>]*>)?\.)?(\w+)\s*\(`),
			Name: 1,
		},
		{
			Kind: graph.KType,
			// `class Foo {`, `data class Point(...)`, `enum class Color {`,
			// `sealed class Shape {`, `object Singleton {`,
			// `interface Shape {`, `annotation class Marker`,
			// `companion object Creator {` (R-0005: `companion` was missing
			// from the modifier alternation, so the whole ^-anchored regex
			// failed on named companion objects).
			Re:   regexp.MustCompile(`^\s*(?:(?:public|private|protected|internal|open|abstract|final|sealed|data|inner|value|enum|annotation|companion)\s+)*(?:class|object|interface)\s+(\w+)`),
			Name: 1,
		},
	},

	// CallRe/Stop (LSP-001): Kotlin function bodies are brace-delimited, so
	// the same brace-counted cspan.Span machinery c.go/cpp.go use applies
	// directly (R-0005: kotlinSpec previously left CallRe nil, which zeroed
	// both call edges and EndLine spans — even multi-line signatures — for
	// every Kotlin KFunc node).
	CallRe: regexp.MustCompile(`\b([A-Za-z_]\w*)\s*\(`),
	Stop:   kotlinCallStop,
}

// kotlinCallStop lists Kotlin control-flow keywords whose `name (` syntax
// structurally matches CallRe but is never a call into a repo symbol.
var kotlinCallStop = []string{
	"if", "for", "while", "when", "catch", "synchronized", "return",
}

func init() { registry = append(registry, kotlinSpec) }
