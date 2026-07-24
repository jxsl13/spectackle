package langspec

import (
	"regexp"

	"github.com/jxsl13/spectackle/internal/graph"
)

// elixirSpec covers .ex/.exs files. Qualification is QualFileStem, mirroring
// pythonSpec/rubySpec: one file is treated as one module, so `def run` in
// app.ex mints "ex:app.run".
var elixirSpec = Spec{
	Lang: graph.LangEx,
	Exts: []string{".ex", ".exs"},
	Qual: QualFileStem,
	Defs: []Def{
		{
			Kind: graph.KFunc,
			// `def run(x)`, `defp helper(x)`, `defmacro foo(x)`,
			// `defmacrop foo(x)`. A trailing `?`/`!` is allowed (Elixir
			// convention for predicate/dangerous functions, e.g.
			// `def valid?` or `def save!`); ids.Mint's idRe accepts it
			// unmodified. The mandatory `\s+` after the def keyword means
			// `defstruct [...]` never matches: "defstruct" has no keyword
			// boundary before "struct", so `def(?:p|macro|macrop)?` can only
			// consume "def" here, leaving "struct" glued on with no
			// whitespace before it.
			Re:   regexp.MustCompile(`^\s*def(?:p|macro|macrop)?\s+([a-z_]\w*[?!]?)`),
			Name: 1,
		},
		{
			Kind: graph.KType,
			// `defmodule Foo do` or `defmodule Foo.Bar.Baz do` — dotted
			// module names kept verbatim in the captured name.
			Re:   regexp.MustCompile(`^\s*defmodule\s+([A-Z][\w.]*)`),
			Name: 1,
		},
	},
}

func init() { registry = append(registry, elixirSpec) }
