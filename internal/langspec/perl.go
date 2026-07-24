package langspec

import (
	"regexp"

	"github.com/jxsl13/spectackle/internal/graph"
)

// perlSpec covers .pl and .pm files. Qualification is QualFileStem (a Perl
// script/module file is its own namespace): `sub run` in app.pl mints
// "pl:app.run".
var perlSpec = Spec{
	Lang: graph.LangPerl,
	Exts: []string{".pl", ".pm"},
	Qual: QualFileStem,
	Defs: []Def{
		{
			Kind: graph.KFunc,
			// `sub run {`.
			Re:   regexp.MustCompile(`^\s*sub\s+([A-Za-z_][A-Za-z0-9_]*)`),
			Name: 1,
		},
		{
			Kind: graph.KType,
			// `package MyApp;`, `package MyApp::Utils;` — `::` stays in the
			// captured name.
			Re:   regexp.MustCompile(`^\s*package\s+([A-Za-z_][A-Za-z0-9_:]*)`),
			Name: 1,
		},
	},
}

func init() { registry = append(registry, perlSpec) }
