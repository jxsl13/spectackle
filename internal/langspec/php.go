package langspec

import (
	"regexp"

	"github.com/jxsl13/spectackle/internal/graph"
)

// phpSpec covers .php files. Qualification is QualFileStem, mirroring
// pythonSpec: one file is one module, so `function run` in app.php mints
// "php:app.run". The function Def requires a name (\w+) immediately after
// `function`, so anonymous closures such as `function () use (&$x) {` or
// `function ($x) {` never match — there is no identifier for the Name
// group to capture.
//
// R-0005: phpSpec previously left CallRe nil even though PHP functions,
// methods, classes/interfaces/traits/enums are brace-delimited exactly like
// C/C++ (c.go) — the shared brace-counted body-span + call-edge machinery
// was never wired up, so every PHP node's EndLine collapsed to Line and no
// same-file call/method-call edge was ever emitted (e.g.
// standaloneHelper -> doubleIt, create -> initialize via `$svc->initialize()`).
// Fixed by setting CallRe/Stop, mirroring c.go and this task's perl.go fix.
// Class/interface/trait/enum-level EndLine (as opposed to their
// function/method members') stays == Line regardless: span computation is
// gated on Kind == KFunc/KMethod in langspec.go's Parse, a framework-wide
// rule this Spec can't opt out of (see c_test.go's cSpec struct/enum nodes,
// which have the identical limitation) — extending it to KType would be
// shared-engine work, out of scope here.
//
// CallRe requires the callee to touch its `(` with no whitespace (same
// reasoning as perl.go's CallRe): this both matches every real call in the
// fixtures (`doubleIt($x)`, `$svc->initialize()`, `$this->validate($name)`,
// `$this->log(...)`, `trim($name)`, `strtoupper($s)`) and structurally
// excludes PHP's `new self(...)`/`new parent(...)` construct-call idiom's
// `self`/`parent` from ever being mistaken for a call to a real symbol —
// backstopped by Stop below for the case those are written with no space.
var phpSpec = Spec{
	Lang: graph.LangPHP,
	Exts: []string{".php"},
	Qual: QualFileStem,
	Defs: []Def{
		{
			Kind: graph.KFunc,
			// `function run($x) {`, `public function run($x) {`,
			// `private static function run() {`, `abstract function run();`.
			Re:   regexp.MustCompile(`^\s*(?:public\s+|private\s+|protected\s+|static\s+|abstract\s+|final\s+)*function\s+(\w+)`),
			Name: 1,
		},
		{
			Kind: graph.KType,
			// `class Foo`, `abstract class Foo`, `final class Foo extends Base`,
			// `interface Foo`, `trait Foo`, `enum Foo`.
			Re:   regexp.MustCompile(`^\s*(?:abstract\s+|final\s+)?(?:class|interface|trait|enum)\s+(\w+)`),
			Name: 1,
		},
	},

	CallRe: regexp.MustCompile(`\b([A-Za-z_]\w*)\(`),
	Stop:   phpCallStop,
}

// phpCallStop lists PHP keywords/pseudo-callables whose `name(` syntax
// structurally matches CallRe but is never a call into a repo symbol.
var phpCallStop = []string{
	"if", "elseif", "for", "foreach", "while", "switch", "match", "catch",
	"self", "parent", "static", "isset", "empty", "unset", "list", "array",
}

func init() { registry = append(registry, phpSpec) }
