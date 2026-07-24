package langspec

import (
	"regexp"

	"github.com/jxsl13/spectacle/internal/graph"
)

// phpSpec covers .php files. Qualification is QualFileStem, mirroring
// pythonSpec: one file is one module, so `function run` in app.php mints
// "php:app.run". The function Def requires a name (\w+) immediately after
// `function`, so anonymous closures such as `function () use (&$x) {` or
// `function ($x) {` never match — there is no identifier for the Name
// group to capture.
var phpSpec = Spec{
	Lang: graph.Lang("php"),
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
}

func init() { registry = append(registry, phpSpec) }
