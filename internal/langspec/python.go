package langspec

import (
	"regexp"

	"github.com/jxsl13/spectacle/internal/graph"
)

// pythonSpec covers .py files. Qualification is QualFileStem (Python's unit
// of namespacing is the module == the file): `def run` in app.py mints
// "py:app.run". Indented `def` lines (methods) mint the same KFunc kind as
// top-level functions in this v0 — distinguishing KMethod would need
// tracking the enclosing `class` block, deferred to a later langspec
// iteration.
var pythonSpec = Spec{
	Lang: graph.LangPy,
	Exts: []string{".py"},
	Qual: QualFileStem,
	Defs: []Def{
		{
			Kind: graph.KFunc,
			// `def run(x):`, or indented `    def run(self, x):` (method, v0: same kind).
			Re:   regexp.MustCompile(`^\s*def\s+(\w+)\s*\(`),
			Name: 1,
		},
		{
			Kind: graph.KType,
			// `class Foo:` or `class Foo(Base):`
			Re:   regexp.MustCompile(`^class\s+(\w+)`),
			Name: 1,
		},
	},
}
