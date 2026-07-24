package langspec

import (
	"regexp"

	"github.com/jxsl13/spectacle/internal/graph"
)

// rubySpec covers .rb files. Qualification is QualFileStem, mirroring
// pythonSpec: one file is one module, so `def run` in app.rb mints
// "rb:app.run". Method names may carry a trailing `?` or `!` (Ruby
// convention for predicate/dangerous methods, e.g. `def valid?` or
// `def save!`); the Name capture group allows that suffix, and
// ids.Mint's idRe ([^\s~]+ for the qualifier) accepts it unmodified.
var rubySpec = Spec{
	Lang: graph.LangRb,
	Exts: []string{".rb"},
	Qual: QualFileStem,
	Defs: []Def{
		{
			Kind: graph.KFunc,
			// `def run(x)`, `def self.run(x)` (class/module method),
			// `def valid?`, `def save!`.
			Re:   regexp.MustCompile(`^\s*def\s+(?:self\.)?(\w+[?!]?)`),
			Name: 1,
		},
		{
			Kind: graph.KType,
			// `class Foo`, `class Foo < Base`, `module Foo`.
			Re:   regexp.MustCompile(`^\s*(?:class|module)\s+(\w+)`),
			Name: 1,
		},
	},
}

func init() { registry = append(registry, rubySpec) }
