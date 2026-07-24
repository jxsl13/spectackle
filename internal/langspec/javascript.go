package langspec

import (
	"regexp"

	"github.com/jxsl13/spectackle/internal/graph"
)

// javascriptSpec covers .js/.mjs files. Qualification is QualFileStem,
// mirroring pythonSpec: one file is one module, so `function run` in app.js
// mints "js:app.run".
var javascriptSpec = Spec{
	Lang: graph.LangJS,
	Exts: []string{".js", ".mjs"},
	Qual: QualFileStem,
	Defs: []Def{
		{
			Kind: graph.KFunc,
			// `function run(x) {`, `export function run(x) {`,
			// `async function run(x) {`, `function* gen() {`.
			Re:   regexp.MustCompile(`^(?:export\s+)?(?:async\s+)?function\s*\*?\s+(\w+)`),
			Name: 1,
		},
		{
			Kind: graph.KType,
			// `class Foo {` or `export class Foo extends Base {`
			Re:   regexp.MustCompile(`^(?:export\s+)?class\s+(\w+)`),
			Name: 1,
		},
		{
			Kind: graph.KFunc,
			// `const run = (x) => {`, `export const run = async (x) => {`,
			// `let run = x => x * 2`.
			Re:   regexp.MustCompile(`^(?:export\s+)?(?:const|let|var)\s+(\w+)\s*=\s*(?:async\s*)?(?:\(|\w+\s*=>)`),
			Name: 1,
		},
	},
}
