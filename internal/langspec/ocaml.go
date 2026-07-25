package langspec

import (
	"regexp"

	"github.com/jxsl13/spectackle/internal/graph"
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
			Kind: graph.KFunc,
			// `and`-chained mutually-recursive binding (R-0005, headline
			// miss): `let rec is_even n = ... and is_odd n = ...` — `and`
			// continues the SAME `let rec` group, idiomatically left at
			// column 0 just like the `let` that started it, so this Def
			// mirrors the primary one but anchors on `and` instead.
			Re:   regexp.MustCompile(`^and\s+([a-z_]\w*'?)`),
			Name: 1,
		},
		{
			Kind: graph.KFunc,
			// `let` bound directly inside a module (struct) body (R-0005,
			// headline miss) — idiomatic indented style:
			//   module Stack = struct
			//     let push x s = s := x :: !s
			//   end
			// A pure line scanner cannot track "am I inside a module
			// struct...end block" vs. "am I inside a function body", so
			// this Def leans on a narrower, single-line-checkable signal
			// instead: a genuinely local `let NAME = EXPR in ...` binding
			// (the only other common source of an indented top-of-line
			// `let`) almost always closes with a trailing ` in` on that
			// same physical line (`let s = Stack.create () in`), whereas a
			// module member's `let` never does (it's a standalone
			// declaration, not an expression to be sequenced into). The
			// tail alternation below excludes exactly that trailing- `in`
			// shape (RE2 has no negative lookahead, hence the two-way
			// split on "second-to-last char isn't 'i'" OR "last char isn't
			// 'n'", plus a short-tail escape hatch); known imprecision:
			// a local let-in whose "in" appears mid-line rather than as
			// the line's last token (e.g. `let x = 5 in x + 1`) is not
			// caught by this heuristic and will still be captured here.
			Re:   regexp.MustCompile(`^\s{2,}let\s+(?:rec\s+)?([a-z_]\w*'?)\b(?:.*[^i].|.*.[^n]|.{0,1})$`),
			Name: 1,
		},
		{
			Kind: graph.KFunc,
			// Custom operator function definition (R-0005): `let ( +^ ) a b
			// = add a b` — the primary Def's name group cannot match a
			// parenthesized operator token. Known imprecision: this also
			// matches a parenthesized tuple-pattern binding, e.g.
			// `let (a, b) = compute ()`, capturing "a, b" as a name; a real
			// OCaml grammar would disambiguate via the AST, which this
			// line scanner has no way to do.
			Re:   regexp.MustCompile(`^let\s+(?:rec\s+)?\(\s*([^)]+?)\s*\)`),
			Name: 1,
		},
		{
			Kind: graph.KFunc,
			// `external sqrt_ff : float -> float = "caml_sqrt_float"`
			// (R-0005, headline miss): OCaml's FFI binding declaration,
			// explicitly in-scope per the FFI-edge use case.
			Re:   regexp.MustCompile(`^external\s+([a-z_]\w*'?)`),
			Name: 1,
		},
		{
			Kind: graph.KFunc,
			// `val add : int -> int -> int` (R-0005, headline miss): the
			// primary content of .mli interface files, entirely uncaptured
			// before since the func Def only matched `let`. Column-0
			// anchored, so an indented `val` inside a `module type ... sig
			// ... end` block (a member signature, not a top-level .mli
			// declaration) is deliberately excluded, mirroring how indented
			// `let`s are excluded from the primary Def.
			Re:   regexp.MustCompile(`^val\s+([a-z_]\w*'?)`),
			Name: 1,
		},
		{
			Kind: graph.KType,
			// `class counter = object (self) ... end` (R-0005, headline
			// miss): OCaml's class definition — no Def targeted `class` at
			// all before. Kind KType, matching how other langspecs (e.g.
			// rustSpec's trait, swiftSpec's protocol) treat a class/
			// interface-shaped construct as a type.
			Re:   regexp.MustCompile(`^class\s+([a-z_]\w*'?)`),
			Name: 1,
		},
		{
			Kind: graph.KFunc,
			// `method incr = n <- n + 1` (R-0005, headline miss): OCaml's
			// idiomatic method/receiver form inside an `object ... end`
			// body. Unlike the module-body `let` Def above, `method` is a
			// dedicated keyword with no other statement-shape it could be
			// confused with, so no "not ending in in" tail restriction is
			// needed here.
			Re:   regexp.MustCompile(`^\s+method\s+([a-z_]\w*'?)`),
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
