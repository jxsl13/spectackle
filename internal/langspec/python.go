package langspec

import (
	"regexp"

	"github.com/jxsl13/spectackle/internal/graph"
)

// pythonSpec covers .py files. Qualification is QualFileStem (Python's unit
// of namespacing is the module == the file): `def run` in app.py mints
// "py:app.run". Indented `def` lines (methods, including methods nested
// inside nested classes) mint the same KFunc kind as top-level functions in
// this v0 — distinguishing KMethod would need tracking the enclosing `class`
// block, deferred to a later langspec iteration.
//
// R-0005: `async def` (both top-level and, at any indentation, methods —
// idiomatic in any modern async-Python codebase) previously minted no node
// at all, since the function Def required the line to start with literal
// `def`. Fixed by making an `async\s+` prefix optional before `def`. The
// class Def was previously anchored at column zero (`^class`), so a nested
// inner class (`class Outer:` containing an indented `class Inner:`) never
// matched; relaxing the anchor to `^\s*class` (mirroring the function Def's
// existing indentation-tolerance) fixes that.
//
// R-0005 also flagged that no Def set Sig, so Node.Sig was always empty for
// Python (see glsl.go/scala.go for the same fix pattern in other langs): an
// optional `(param, list)` capture group right after the name now feeds Sig.
// A multi-line signature (params wrapping past the `def`/`async def` line,
// e.g. multi_line_sig in gap-python/sample.py) leaves Sig empty, same as
// scala.go's documented limitation — only the def line itself is scanned.
//
// CallRe stays nil, deliberately (R-0005 EDGES finding + this task's brief):
// Python bodies are delimited by indentation, not braces, so there is
// nothing for cspan.Span's brace-counting to bound a call scan to — setting
// CallRe here would need a wholly different (indentation-aware) body-span
// mechanism that plainly does not exist in this framework and is out of
// scope for a single Spec file to invent (it would mean either the shared
// engine gains an indentation-counting span alongside cspan.Span/EndSpan, or
// every call on every line of a .py file becomes fair game with no bound —
// neither is a same-file fix). This means Python's call/launch edges and
// definition-span (EndLine) gaps from R-0005 remain: EndLine==Line for every
// Python node, and Parse never emits ECall edges for .py files, exactly as
// before this task (see TestPythonSpecCallReRemainsNilAndWhy in
// python_test.go, and TestSpecParserNoEdgesWithoutCallRe in
// langspec_test.go, which continues to cover pythonSpec generically since
// CallRe is still nil).
//
// Likewise, R-0005's "no class/receiver qualification for methods" finding
// (same-named methods in different classes collide, e.g. Vector.__init__ vs
// Matrix.__init__ both mint the flat "py:sample.__init__" id) is not fixed
// here: Spec.Qual only offers three fixed, non-contextual modes
// (QualFileStem/QualDirPkg/QualFlat, see langspec.go) with no hook for a
// per-Def, class-scope-aware qualifier — building one would mean the shared
// engine tracking an "enclosing class" as it walks lines, which is
// out-of-scope engine work, not a python.go regex change.
var pythonSpec = Spec{
	Lang: graph.LangPy,
	Exts: []string{".py"},
	Qual: QualFileStem,
	Defs: []Def{
		{
			Kind: graph.KFunc,
			// `def run(x):`, indented `    def run(self, x):` (method, v0:
			// same kind), `async def fetch(url):` at any indentation
			// (top-level or nested method, e.g. `Outer.Inner.async_worker`).
			// The optional `([^)]*)` after the opening paren feeds Sig with
			// the parameter list when it closes on the same line.
			Re:   regexp.MustCompile(`^\s*(?:async\s+)?def\s+(\w+)\s*\(([^)]*)\)?`),
			Name: 1,
			Sig:  2,
		},
		{
			Kind: graph.KType,
			// `class Foo:` or `class Foo(Base):`, at any indentation — a
			// nested `class Inner:` inside an outer class body must mint a
			// node too (R-0005), so this is no longer column-zero-anchored.
			Re:   regexp.MustCompile(`^\s*class\s+(\w+)`),
			Name: 1,
		},
	},
}
