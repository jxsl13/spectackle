package langspec

import (
	"regexp"

	"github.com/jxsl13/spectacle/internal/graph"
)

// ocamlSpec covers .ml and .mli files. Qualification is QualFileStem,
// mirroring pythonSpec and rustSpec: one file is treated as one module, so
// `let run x = x` in app.ml mints "ml:app.run".
var ocamlSpec = Spec{
	Lang: graph.LangMl,
	Exts: []string{".ml", ".mli"},
	Qual: QualFileStem,
	Defs: []Def{
		{
			Kind: graph.KFunc,
			// Column-0 `let run x = x` or `let rec fact n = ...`. Nested,
			// indented `let`s (local bindings inside a function body) are
			// excluded by the `^` anchor with no leading `\s*`.
			Re:   regexp.MustCompile(`^let\s+(?:rec\s+)?([a-z_]\w*'?)`),
			Name: 1,
		},
		{
			Kind: graph.KType,
			// `type tree = Leaf | Node of tree * tree`, `type 'a tree = ...`
			// (single type-variable param), `type ('a, 'b) pair = ...`
			// (parenthesized param list). OCaml puts the type parameters
			// BEFORE the type name, so the type-name capture is group 2
			// here, not group 1 — Def.Name is an arbitrary submatch index
			// (confirmed against langspec.go's Def.Name/Sig fields, which
			// select `m[def.Name]` off the full FindStringSubmatch result
			// with no restriction to group 1; cpp.go's method Def already
			// uses Name: 2 for the same reason, dropping an earlier
			// qualifier group). Group 1 here is the optional parameter list
			// itself, deliberately left uncaptured-into-Name since the def
			// only needs the name.
			Re:   regexp.MustCompile(`^type\s+(?:('\w+|\([^)]*\))\s+)?([a-z_]\w*)`),
			Name: 2,
		},
		{
			Kind: graph.KType,
			// `module Foo = struct ... end` or `module type SIG = sig ... end`.
			Re:   regexp.MustCompile(`^module\s+(?:type\s+)?([A-Z]\w*)`),
			Name: 1,
		},
	},
}

func init() { registry = append(registry, ocamlSpec) }
