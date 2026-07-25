package langspec

import (
	"regexp"

	"github.com/jxsl13/spectackle/internal/graph"
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
			// `run(X) ->`, `run(X, Y) ->`, `classify(X) when X > 0 ->`
			// (a guarded clause head), and `combine(A,` (the first physical
			// line of a head whose args and `->` wrap to a later line) —
			// column 0 only. R-0005 (two former [high] misses, fixed by one
			// simplification): the old regex required `\(.*\)\s*->` to all
			// land on the SAME physical line, which broke on any text
			// between `)` and `->` (a guard) and on any head split across
			// lines. Dropping that requirement down to just `name(` is
			// sound specifically because of this Def's own column-0
			// convention above: idiomatic Erlang has nothing else that is
			// both column-0 and shaped like `lowercase_atom(` — module
			// attributes always start with `-`, and clause bodies are
			// always indented — so seeing `name(` at column 0 is already
			// sufficient to know this is a clause head, without needing to
			// also see how its arguments or arrow are laid out. Excludes
			// indented case-clause lines such as `  foo(X) ->` inside a
			// case/if/receive block.
			Re:   regexp.MustCompile(`^([a-z]\w*)\(`),
			Name: 1,
		},
		{
			Kind: graph.KType,
			// `-module(app).` — the module attribute.
			Re:   regexp.MustCompile(`^-module\((\w+)\)`),
			Name: 1,
		},
		{
			Kind: graph.KType,
			// `-record(person, {name, age}).` (R-0005, headline miss): the
			// closest Erlang analog to a struct/type.
			Re:   regexp.MustCompile(`^-record\((\w+)`),
			Name: 1,
		},
		{
			Kind: graph.KType,
			// `-type option() :: {ok, term()} | {error, term()}.` (R-0005,
			// headline miss): Erlang's other core type-definition form.
			Re:   regexp.MustCompile(`^-type\s+(\w+)`),
			Name: 1,
		},
	},
}

func init() { registry = append(registry, erlangSpec) }
