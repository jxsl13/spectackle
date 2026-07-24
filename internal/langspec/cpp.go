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
			Kind: graph.KFunc,
			// Out-of-line method definition, e.g.
			// `void Foo::run(int x) {` or `int Foo::Bar::get() const {`.
			// SpecParser's Def has a single Name submatch group, so there is
			// no way to carry the enclosing class qualifier (group 1) into
			// the minted NodeID here; only the flat method name (group 2) is
			// captured. This means e.g. `Foo::run` and `Bar::run` both mint
			// the same "cpp:run" node (a documented v0 limitation, mirroring
			// pythonSpec's method/function conflation).
			Re:   regexp.MustCompile(`^(?:[A-Za-z_][\w:&<>,\s\*]*?[\s\*])?(\w+)::(\w+)\s*\(`),
			Name: 2,
		},
		{
			Kind: graph.KFunc,
			// Plain (non-method) function prototype or definition, same
			// shape as cSpec's function Def, e.g.
			// `int launch_saxpy(int n, float a, const float *x, float *y);`
			// or `static void helper(void) {`.
			Re:   regexp.MustCompile(`^(?:[A-Za-z_][\w\s\*]*?[\s\*])(\w+)\s*\([^;{}]*\)\s*[;{]\s*$`),
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
