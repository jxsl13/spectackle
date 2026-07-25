package langspec

import (
	"regexp"

	"github.com/jxsl13/spectackle/internal/graph"
)

// cSpec covers .c/.h files. Qualification is QualFlat (C's unit of
// namespacing is the translation unit's global symbol table, not the file):
// `int launch_saxpy(...)` mints "c:launch_saxpy" regardless of which .c/.h
// file it's declared or defined in.
//
// This is the langspec that completes the go -> c -> cu chain
// (index.CudaParser only mints extern "C" *definitions* found inside .cu
// files): a prototype declared in a plain .h header, e.g.
// examples/saxpy/saxpy/kernels/saxpy.h's `int launch_saxpy(...)`, now also
// mints a real, located "c:launch_saxpy" node — upgrading what would
// otherwise be a stub materialized only from the cgo resolver's edge
// endpoint (see internal/index.stubNodes).
//
// The function Def's regex is deliberately strict about anchoring at column
// zero (^, no leading \s): every call site and every statement inside a
// function body is indented, so this alone rules out matching calls and
// (indented) control-flow statements. Unindented control keywords such as
// `if`/`for`/`while`/`switch`/`return`/`sizeof` also can't produce a false
// match structurally: the regex requires its non-capturing "type" prefix to
// end in a whitespace or `*` character immediately followed by the captured
// name and then `(`, and C control keywords are always followed directly by
// `(` (with at most the single space already consumed into the type
// prefix) with no further identifier between the keyword and `(` — so there
// is never a word left over to capture as Name. `#define`/`#include` lines
// are excluded because the anchor requires the first character to be
// [A-Za-z_], never `#`.
var cSpec = Spec{
	Lang: graph.LangC,
	Exts: []string{".c", ".h"},
	Qual: QualFlat,
	Defs: []Def{
		{
			Kind: graph.KFunc,
			// Function prototype or definition where the return type AND name
			// both live on this physical line, e.g.
			// `int launch_saxpy(int n, float a, const float *x, float *y);`,
			// `static void helper(void) {`, a same-line one-liner body
			// (`int add(int a, int b) { return a + b; }`), or a bare Allman
			// signature with nothing after the closing paren (brace deferred
			// to a following line, depth-counted by cspan.Span — see T-0053).
			// The trailing alternation is: balanced params + optional
			// `;`/`{...}` tail (prototype, K&R, or Allman-with-nothing-after),
			// OR params ending in `,` (an unclosed multi-line parameter list
			// that continues on the next physical line). Non-empty params
			// content may never *start* with `*`: that shape is
			// `NAME(*rest...`, which is what a function-pointer typedef's
			// `(*Alias)(...)` looks like once `Alias` has been mistaken for
			// the "name" (e.g. `typedef int (*BinOp)(int, int);` would
			// otherwise mint a bogus `int` node) — real parameter lists never
			// open with a bare `*` before any type.
			Re:   regexp.MustCompile(`^(?:[A-Za-z_][\w\s\*]*?[\s\*])(\w+)\s*\((?:[^*;{}][^;{}]*\)\s*(?:[;{].*)?|\)\s*(?:[;{].*)?|[^*;{}][^;{}]*,\s*)$`),
			Name: 1,
		},
		{
			Kind: graph.KFunc,
			// Same shapes as the Def above, but for the physical line where
			// the return type was left on a *previous* line entirely (Allman
			// with the type on its own line, e.g. `int\nsubtract(int a, int
			// b)\n{`, or a multi-line parameter list whose first line is
			// `compute_average(int values[], int count,` with `double
			// weight)` on the type's own earlier line too) — this line has no
			// type prefix at all, just `name(...`. Requiring a mandatory
			// prefix (as the Def above does) is what keeps single-token
			// control keywords (`if (`, `for (`, `while (`, `switch (`) from
			// ever being captured as Name there; here, with no prefix at all,
			// the same risk is closed a different way: the balanced-close
			// alternative only accepts params that are empty, exactly `void`,
			// or contain a `,`/space/`*`/`[`/`]` — real parameter lists
			// almost always look like `TYPE name[, ...]`, while a bare
			// control-flow condition (`x`, `n <= 1`, `i`) never does, so
			// `if (n <= 1)`-shaped lines never match this Def even though
			// they'd otherwise satisfy the "no prefix, ends after `)`" shape.
			Re:   regexp.MustCompile(`^(\w+)\s*\((?:(?:|void|[\w\s\*\[\],]*[\s,][\w\s\*\[\],]*)\)\s*(?:[;{].*)?|[\w\s\*\[\],]*,\s*)$`),
			Name: 1,
		},
		{
			Kind: graph.KType,
			// `struct Point {`, `typedef struct Point {`, `enum Color {`,
			// `union U {`.
			Re:   regexp.MustCompile(`^(?:typedef\s+)?(?:struct|union|enum)\s+(\w+)`),
			Name: 1,
		},
		{
			Kind: graph.KType,
			// Function-pointer typedef, e.g. `typedef int (*BinOp)(int,
			// int);` — the aliased name is wrapped as `(*Name)`, so it can
			// never satisfy the KFunc Defs above (Name isn't immediately
			// followed by `(`) nor the struct/union/enum Def above (there's
			// no struct/union/enum keyword at all).
			Re:   regexp.MustCompile(`^typedef\s+[\w\s\*]+?\(\s*\*\s*(\w+)\s*\)\s*\(`),
			Name: 1,
		},
		{
			Kind: graph.KFunc,
			// Function-like macro, e.g. `#define MAX(a, b) ((a) > (b) ? (a) : (b))`.
			Re:   regexp.MustCompile(`^#define\s+(\w+)\(`),
			Name: 1,
		},
	},

	// CallRe/Stop (LSP-001): any `name(` inside a KFunc def's brace-counted
	// body span is a call edge unless name is the def's own name (recursion,
	// including the def line's own self-match) or one of Stop's control-flow
	// keywords/operators that structurally look like a call.
	CallRe: regexp.MustCompile(`\b([A-Za-z_]\w*)\s*\(`),
	Stop:   cFamilyCallStop,
}

// cFamilyCallStop is the Stop list shared by cSpec and cppSpec's CallRe: C
// keywords and standard-library macros whose `name (` syntax structurally
// matches CallRe but is never a call into a repo symbol.
var cFamilyCallStop = []string{
	"if", "for", "while", "switch", "return",
	"sizeof", "defined", "static_assert", "alignof", "typeof",
}

func init() {
	registry = append(registry, cSpec)
}
