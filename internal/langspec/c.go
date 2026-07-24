package langspec

import (
	"regexp"

	"github.com/jxsl13/spectacle/internal/graph"
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
			// Function prototype or definition, e.g.
			// `int launch_saxpy(int n, float a, const float *x, float *y);`
			// or `static void helper(void) {`. The trailing `[^;{}]*` keeps
			// multi-statement control-flow lines (which contain `;` inside
			// the parens, e.g. a for-loop header) from matching.
			Re:   regexp.MustCompile(`^(?:[A-Za-z_][\w\s\*]*?[\s\*])(\w+)\s*\([^;{}]*\)\s*[;{]\s*$`),
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
