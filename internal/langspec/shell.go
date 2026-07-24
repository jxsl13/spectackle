package langspec

import (
	"regexp"

	"github.com/jxsl13/spectacle/internal/graph"
)

// shellSpec covers .sh and .bash files. Qualification is QualFileStem
// (a shell script is its own namespace): `foo()` in deploy.sh mints
// "sh:deploy.foo". Two Defs cover the two mutually-exclusive function
// declaration shapes shell supports — POSIX `name() {` (with an optional
// `function` keyword prefix) and bash's parenthesis-less `function name {`
// — so a given line matches at most one of them.
var shellSpec = Spec{
	Lang: graph.LangSh,
	Exts: []string{".sh", ".bash"},
	Qual: QualFileStem,
	Defs: []Def{
		{
			Kind: graph.KFunc,
			// POSIX `foo() {`, optionally bash-prefixed `function foo() {`.
			Re:   regexp.MustCompile(`^\s*(?:function\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*\(\)\s*\{?`),
			Name: 1,
		},
		{
			Kind: graph.KFunc,
			// bash-only `function foo {` (no parens).
			Re:   regexp.MustCompile(`^\s*function\s+([A-Za-z_][A-Za-z0-9_]*)\s*\{`),
			Name: 1,
		},
	},
}

func init() { registry = append(registry, shellSpec) }
