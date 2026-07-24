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
			// `private inline fun <T> identity(x: T): T = x`. The optional
			// generic parameter list (`<T>`) sits between `fun` and the name.
			Re:   regexp.MustCompile(`^\s*(?:(?:public|private|protected|internal|open|override|suspend|inline|operator|infix|tailrec|external|abstract|final)\s+)*fun\s+(?:<[^>]*>\s+)?(\w+)`),
			Name: 1,
		},
		{
			Kind: graph.KType,
			// `class Foo {`, `data class Point(...)`, `enum class Color {`,
			// `sealed class Shape {`, `object Singleton {`,
			// `interface Shape {`, `annotation class Marker`.
			Re:   regexp.MustCompile(`^\s*(?:(?:public|private|protected|internal|open|abstract|final|sealed|data|inner|value|enum|annotation)\s+)*(?:class|object|interface)\s+(\w+)`),
			Name: 1,
		},
	},
}

func init() { registry = append(registry, kotlinSpec) }
