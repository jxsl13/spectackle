package langspec

import (
	"regexp"

	"github.com/jxsl13/spectackle/internal/graph"
)

// cppSpec covers .cc/.cpp/.cxx/.hpp files. Qualification is QualFlat,
// mirroring cSpec: C++ symbols are minted flat, e.g. `void Foo::run(...)`
// mints "cpp:run" (see the Name=2 note below — the class qualifier is
// deliberately dropped, a known v0 limitation of SpecParser's single Name
// submatch group).
var cppSpec = Spec{
	Lang: graph.LangCpp,
	Exts: []string{".cc", ".cpp", ".cxx", ".hpp"},
	Qual: QualFlat,
	Defs: []Def{
		{
			Kind: graph.KType,
			// `class Foo {` or `class Foo : public Base {`, `struct Foo {`.
			Re:   regexp.MustCompile(`^(?:class|struct)\s+(\w+)`),
			Name: 1,
		},
		{
			Kind: graph.KType,
			// `enum Color { ... };` or `enum class Color { ... };` — enum(
			// class) was entirely absent from cppSpec's Defs before T-0122
			// even though cSpec already handles plain `enum`.
			Re:   regexp.MustCompile(`^\s*enum(?:\s+class)?\s+(\w+)`),
			Name: 1,
		},
		{
			Kind: graph.KType,
			// Nested type declaration, e.g. a `struct Inner { ... };` or
			// `class Inner { ... };` defined *inside* another class/struct
			// body (indented, unlike the column-zero Def above). Trailing
			// `{` is required (unlike the column-zero Def) specifically to
			// avoid minting a bogus type node from an ordinary indented
			// C-style variable declaration of an existing type, e.g.
			// `struct Foo instance;` inside a method body — that has no
			// `{` on the line, so it never matches this Def.
			Re:   regexp.MustCompile(`^\s+(?:class|struct)\s+(\w+)[^;{}]*\{`),
			Name: 1,
		},
		{
			Kind: graph.KFunc,
			// Out-of-line method definition, e.g.
			// `void Foo::run(int x) {` or `int Foo::Bar::get() const {`, now
			// also tolerating a template-argument list between the class
			// qualifier and `::` (`Stack<T>::push(...)`, previously a total
			// miss because the class-qualifier group was a bare `(\w+)`).
			// SpecParser's Def has a single Name submatch group, so there is
			// no way to carry the enclosing class qualifier (group 1) into
			// the minted NodeID here; only the flat method name (group 2) is
			// captured. This means e.g. `Foo::run` and `Bar::run` both mint
			// the same "cpp:run" node (a documented v0 limitation, mirroring
			// pythonSpec's method/function conflation).
			Re:   regexp.MustCompile(`^(?:[A-Za-z_][\w:&<>,\s\*]*?[\s\*])?(\w+(?:<[^<>]*>)?)::(\w+)\s*\(`),
			Name: 2,
		},
		{
			Kind: graph.KFunc,
			// Operator overload definition, in-class or free/out-of-line,
			// e.g. `std::ostream& operator<<(std::ostream& os, const
			// Circle& c) {` — the plain-function Def below can never match
			// these because its Name group is `\w+`, and an operator symbol
			// (`<<`, `+`, `()`, `[]`, ...) is not a word character.
			Re:   regexp.MustCompile(`^\s*(?:[A-Za-z_][\w:&<>,\s\*]*?[\s\*])?(operator\s*(?:[<>=!+\-*/%^&|~]+|\(\)|\[\]))\s*\((?:[^;{}]*\)\s*(?:[;{].*)?|[^;{}]*,\s*)$`),
			Name: 1,
		},
		{
			Kind: graph.KMethod,
			// Destructor definition, in-class, e.g. `virtual ~Shape() {}` —
			// the leading `~` is not a word character, so no other Def can
			// capture it (destructors always take an empty parameter list).
			Re:   regexp.MustCompile(`^\s+(?:virtual\s+)?(~\w+)\s*\(\s*\)\s*[^;{}]*\{`),
			Name: 1,
		},
		{
			Kind: graph.KMethod,
			// Inline (in-class) method definition, e.g. a getter, a
			// virtual override, or any other one-liner body defined right
			// inside the class: `double radius() const { return radius_;
			// }`. Indented (unlike the free-function Def below), so a
			// mandatory return-type prefix plus a required trailing `{` are
			// both essential here to avoid matching an ordinary indented
			// call statement (`foo(x);` has no `{`) or a bare control-flow
			// line (`if (x) {` can't supply two tokens before `(` the way a
			// `TYPE name(` declarator can).
			Re:   regexp.MustCompile(`^\s+(?:[A-Za-z_][\w:&<>,\s\*]*?[\s\*])(\w+)\s*\(([^;{}]*)\)\s*[^;{}]*\{`),
			Name: 1,
			Sig:  2,
		},
		{
			Kind: graph.KFunc,
			// Plain (non-method) function prototype or definition, same
			// shape as cSpec's function Def, e.g.
			// `int launch_saxpy(int n, float a, const float *x, float *y);`
			// or `static void helper(void) {` — now also tolerating a
			// same-line body after the opening `{` (a one-liner), a bare
			// Allman signature with nothing after the closing paren (brace
			// deferred to the next line), and a multi-line parameter list
			// (params wrapped onto a following line, e.g. `static int
			// helper(int n,\n int m) {`).
			Re:   regexp.MustCompile(`^(?:[A-Za-z_][\w\s\*]*?[\s\*])(\w+)\s*\((?:[^;{}]*\)\s*(?:[;{].*)?|[^;{}]*,\s*)$`),
			Name: 1,
		},
	},

	// CallRe/Stop (LSP-001): same shape as cSpec — see its comment. Shared
	// Stop list: cFamilyCallStop (defined in c.go).
	CallRe: regexp.MustCompile(`\b([A-Za-z_]\w*)\s*\(`),
	Stop:   cFamilyCallStop,
}

func init() {
	registry = append(registry, cppSpec)
}
