package langspec

import (
	"regexp"

	"github.com/jxsl13/spectackle/internal/graph"
)

// juliaSpec covers .jl files. Qualification is QualFileStem, mirroring
// pythonSpec: one file is treated as one module, so `function run` in app.jl
// mints "jl:app.run".
var juliaSpec = Spec{
	Lang: graph.LangJl,
	Exts: []string{".jl"},
	Qual: QualFileStem,
	Defs: []Def{
		{
			Kind: graph.KFunc,
			// `function run(x)`, indented `    function run(x)`, dotted
			// names such as `function Base.show(io, x)`, and bang names
			// such as `function push!(v, x)`.
			Re:   regexp.MustCompile(`^\s*function\s+([\w.!]+)\s*\(`),
			Name: 1,
		},
		{
			Kind: graph.KFunc,
			// Short-form one-liner definitions at column 0 only:
			// `square(x) = x * x`. The trailing `[^=]` guard requires the
			// character right after `=` not to itself be `=`, which
			// excludes comparison lines such as `foo(x) == bar(y)` (there
			// the char after the first `=` is another `=`). Indented
			// short-form lines are excluded by the column-0 anchor.
			Re:   regexp.MustCompile(`^([\w!]+)\s*\([^)]*\)\s*=\s*[^=]`),
			Name: 1,
		},
		{
			Kind: graph.KType,
			// `struct Point` or `mutable struct Point`.
			Re:   regexp.MustCompile(`^\s*(?:mutable\s+)?struct\s+(\w+)`),
			Name: 1,
		},
		{
			Kind: graph.KType,
			// `abstract type Shape end`.
			Re:   regexp.MustCompile(`^\s*abstract\s+type\s+(\w+)`),
			Name: 1,
		},
	},
}

func init() { registry = append(registry, juliaSpec) }
