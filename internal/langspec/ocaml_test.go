package langspec

import (
	"reflect"
	"testing"

	"github.com/jxsl13/spectackle/internal/graph"
)

// ocamlSrc exercises every ocamlSpec Def (let/let rec, type incl. a
// single-type-variable param case (`type 'a forest`) and a parenthesized
// multi-param case (`type ('a, 'b) pair`), module, module type) plus
// negative lines that must NOT mint nodes: an indented nested let, a
// commented-out let, and `let () = ...` (unit pattern, no capturable name).
var ocamlSrc = []byte(`let run x = x + 1

let rec fact n =
  if n = 0 then 1 else n * fact (n - 1)

type tree = Leaf | Node of tree * tree

type 'a forest = Empty | Trees of 'a tree list

type ('a, 'b) pair = { fst : 'a; snd : 'b }

module Foo = struct
  let inner = 1
end

module type SIG = sig
  val run : int -> int
end

(* let commented = 1 *)
let () = print_string "hi"
`)

func TestOcamlSpecLangExtensions(t *testing.T) {
	p := SpecParser{S: ocamlSpec}
	if p.Lang() != graph.LangMl {
		t.Errorf("Lang() = %v, want %v", p.Lang(), graph.LangMl)
	}
	if got, want := p.Extensions(), []string{".ml", ".mli"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Extensions() = %v, want %v", got, want)
	}
}

func TestOcamlSpecNodes(t *testing.T) {
	p := SpecParser{S: ocamlSpec}
	pr, err := p.Parse("pkg/app.ml", ocamlSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)

	want := map[graph.NodeID]struct {
		Kind graph.NodeKind
		Line int
	}{
		"ml:app.run":    {graph.KFunc, 1},
		"ml:app.fact":   {graph.KFunc, 3},
		"ml:app.tree":   {graph.KType, 6},
		"ml:app.forest": {graph.KType, 8},
		"ml:app.pair":   {graph.KType, 10},
		"ml:app.Foo":    {graph.KType, 12},
		// R-0005: module-body `let inner = 1` now mints a node (findings/
		// ocaml.md's [high] miss: module members are idiomatic OCaml and
		// should be visible to `find scope=code`; see the module-body-let
		// Def's doc comment for the "not ending in in" heuristic that
		// distinguishes this from a truly local `let ... in` binding).
		"ml:app.inner": {graph.KFunc, 13},
		"ml:app.SIG":   {graph.KType, 16},
	}
	if len(pr.Nodes) != len(want) {
		t.Fatalf("got %d nodes, want %d: %+v", len(pr.Nodes), len(want), pr.Nodes)
	}
	for id, w := range want {
		n, ok := byID[id]
		if !ok {
			t.Fatalf("node %s missing, got %+v", id, pr.Nodes)
		}
		if n.Kind != w.Kind {
			t.Errorf("%s Kind = %v, want %v", id, n.Kind, w.Kind)
		}
		if n.Line != w.Line || n.EndLine != w.Line {
			t.Errorf("%s Line/EndLine = %d/%d, want %d", id, n.Line, n.EndLine, w.Line)
		}
		if n.Lang != graph.LangMl {
			t.Errorf("%s Lang = %v, want ml", id, n.Lang)
		}
		if n.File != "pkg/app.ml" {
			t.Errorf("%s File = %q, want pkg/app.ml", id, n.File)
		}
	}
}

// TestOcamlSpecNegativeLines pins down that a commented-out let, the
// unit-pattern `let () = ...`, and a genuinely local `let X = ... in`
// binding inside a function body never mint nodes.
//
// R-0005: this test previously asserted that an indented `let inner = 1`
// directly inside a `module Foo = struct ... end` body must NOT mint a
// node — that was the exact behavior findings/ocaml.md flags as a [high]
// miss (module-body members are idiomatic OCaml and should be visible to
// `find scope=code`), so that expectation was wrong and is now flipped in
// TestOcamlSpecModuleBodyLet below instead of pinned down here as a
// negative case.
func TestOcamlSpecNegativeLines(t *testing.T) {
	p := SpecParser{S: ocamlSpec}
	pr, err := p.Parse("neg.ml", []byte(`let main () =
  let s = compute () in
  print_int s
(* let commented = 1 *)
let () = print_string "hi"
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)
	if _, ok := byID["ml:neg.s"]; ok {
		t.Error("local `let s = compute () in` binding must not mint a node")
	}
	if len(pr.Nodes) != 1 {
		t.Fatalf("got %d nodes, want 1 (only main): %+v", len(pr.Nodes), pr.Nodes)
	}
	if _, ok := byID["ml:neg.main"]; !ok {
		t.Errorf("main missing, got %+v", pr.Nodes)
	}
}

// TestOcamlSpecAndChain pins down R-0005's headline miss: an `and`-chained
// mutually-recursive binding continuing a `let rec` group.
func TestOcamlSpecAndChain(t *testing.T) {
	p := SpecParser{S: ocamlSpec}
	pr, err := p.Parse("app.ml", []byte(`let rec is_even n =
  if n = 0 then true else is_odd (n - 1)
and is_odd n =
  if n = 0 then false else is_even (n - 1)
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)
	if _, ok := byID["ml:app.is_even"]; !ok {
		t.Errorf("ml:app.is_even missing, got %+v", pr.Nodes)
	}
	n, ok := byID["ml:app.is_odd"]
	if !ok {
		t.Fatalf("ml:app.is_odd missing, got %+v", pr.Nodes)
	}
	if n.Kind != graph.KFunc || n.Line != 3 {
		t.Errorf("is_odd = %+v, want KFunc Line=3", n)
	}
}

// TestOcamlSpecModuleBodyLet pins down R-0005's headline miss: functions
// bound directly inside a module (struct) body, idiomatic indented style —
// and that a genuinely local `let ... in` binding inside a function body
// (same indentation) is still correctly excluded.
func TestOcamlSpecModuleBodyLet(t *testing.T) {
	p := SpecParser{S: ocamlSpec}
	pr, err := p.Parse("app.ml", []byte(`module Stack = struct
  let create () : 'a t = ref []

  let push x s =
    s := x :: !s
end

let main () =
  let s = Stack.create () in
  Stack.push (add 1 2) s
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)
	for _, want := range []graph.NodeID{"ml:app.create", "ml:app.push"} {
		if _, ok := byID[want]; !ok {
			t.Errorf("%s missing, got %+v", want, pr.Nodes)
		}
	}
	if _, ok := byID["ml:app.s"]; ok {
		t.Error("local `let s = Stack.create () in` binding inside main must not mint a node")
	}
}

// TestOcamlSpecOperatorDef pins down R-0005's medium miss: a custom
// operator function definition.
func TestOcamlSpecOperatorDef(t *testing.T) {
	p := SpecParser{S: ocamlSpec}
	pr, err := p.Parse("app.ml", []byte(`let ( +^ ) a b = add a b
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)
	if _, ok := byID["ml:app.+^"]; !ok {
		t.Errorf("ml:app.+^ missing, got %+v", pr.Nodes)
	}
}

// TestOcamlSpecExternalVal pins down R-0005's two headline .mli-adjacent
// misses: an `external` FFI binding declaration and a `val` signature
// declaration.
func TestOcamlSpecExternalVal(t *testing.T) {
	p := SpecParser{S: ocamlSpec}
	pr, err := p.Parse("lib.ml", []byte(`external sqrt_ff : float -> float = "caml_sqrt_float"
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)
	if _, ok := byID["ml:lib.sqrt_ff"]; !ok {
		t.Errorf("ml:lib.sqrt_ff missing, got %+v", pr.Nodes)
	}

	pr2, err := p.Parse("lib.mli", []byte(`val add : int -> int -> int
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID2 := nodesByID(pr2)
	if _, ok := byID2["ml:lib.add"]; !ok {
		t.Errorf("ml:lib.add missing, got %+v", pr2.Nodes)
	}
}

// TestOcamlSpecClassMethod pins down R-0005's two headline misses: a class
// definition and an object method.
func TestOcamlSpecClassMethod(t *testing.T) {
	p := SpecParser{S: ocamlSpec}
	pr, err := p.Parse("app.ml", []byte(`class counter =
  object (self)
    val mutable n = 0
    method incr =
      n <- n + 1
    method get = n
  end
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)
	if n, ok := byID["ml:app.counter"]; !ok {
		t.Errorf("ml:app.counter missing, got %+v", pr.Nodes)
	} else if n.Kind != graph.KType {
		t.Errorf("counter Kind = %v, want KType", n.Kind)
	}
	for _, want := range []graph.NodeID{"ml:app.incr", "ml:app.get"} {
		if _, ok := byID[want]; !ok {
			t.Errorf("%s missing, got %+v", want, pr.Nodes)
		}
	}
}

// TestOcamlSpecNoCallEdges pins down that ocamlSpec deliberately leaves
// CallRe nil (R-0005 scope: OCaml has no braces for cspan.Span to count) —
// zero edges are emitted even when a body plainly calls other top-level
// functions.
func TestOcamlSpecNoCallEdges(t *testing.T) {
	p := SpecParser{S: ocamlSpec}
	pr, err := p.Parse("app.ml", []byte(`let add a b = a + b
let main () = add 1 2
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(pr.Edges) != 0 {
		t.Errorf("got %d edges, want 0 (CallRe deliberately unset for OCaml): %+v", len(pr.Edges), pr.Edges)
	}
}

func TestOcamlSpecRegisteredInAll(t *testing.T) {
	found := false
	for _, p := range All() {
		if p.Lang() == graph.LangMl {
			found = true
		}
	}
	if !found {
		t.Error("All() does not contain a parser for graph.LangMl — ocamlSpec not registered via init()")
	}
}

func TestOcamlSpecDeterministic(t *testing.T) {
	p := SpecParser{S: ocamlSpec}
	pr1, err := p.Parse("pkg/app.ml", ocamlSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	pr2, err := p.Parse("pkg/app.ml", ocamlSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !reflect.DeepEqual(pr1, pr2) {
		t.Errorf("Parse is not deterministic across identical runs:\n%+v\nvs\n%+v", pr1, pr2)
	}
}
