package langspec

import (
	"regexp"

	"github.com/jxsl13/spectacle/internal/graph"
)

// erlangSpec covers .erl/.hrl files. Qualification is QualFileStem,
// mirroring pythonSpec: one file is treated as one module, so `run(X) ->`
// in app.erl mints "erl:app.run".
//
// KFunc only matches clause heads anchored at column 0: Erlang function
// clauses are conventionally written unindented, one function name per set
// of clauses, with clause bodies (case/if branches, nested calls) indented.
// This deliberately does not distinguish separate clauses of the same
// function (`foo(0) -> ...` and `foo(N) -> ...` both mint the same
// "erl:<stem>.foo" ID) — each clause head line mints its own Node, and the
// indexer's collision handling (internal/ids: "~2", "~3", ...) disambiguates
// them in deterministic file-path/line order. This is an accepted
// imperfection of the v0 line-oriented parser, not a bug: distinguishing
// clauses of the same function would need arity-aware parsing, deferred to a
// later langspec iteration.
var erlangSpec = Spec{
	Lang: graph.LangErl,
	Exts: []string{".erl", ".hrl"},
	Qual: QualFileStem,
	Defs: []Def{
		{
			Kind: graph.KFunc,
			// `run(X) ->` or `run(X, Y) ->`, column 0 only. Excludes
			// indented case-clause lines such as `  foo(X) ->` inside a
			// case/if/receive block.
			Re:   regexp.MustCompile(`^([a-z]\w*)\(.*\)\s*->`),
			Name: 1,
		},
		{
			Kind: graph.KType,
			// `-module(app).` — the module attribute.
			Re:   regexp.MustCompile(`^-module\((\w+)\)`),
			Name: 1,
		},
	},
}

func init() { registry = append(registry, erlangSpec) }
