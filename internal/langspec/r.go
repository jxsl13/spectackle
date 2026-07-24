package langspec

import (
	"regexp"

	"github.com/jxsl13/spectacle/internal/graph"
)

// rSpec covers .r files (including uppercase .R sources — see the
// case-sensitivity note below). Qualification is QualFileStem, mirroring
// pythonSpec and rustSpec: one file is treated as one module, so
// `run <- function(x) x` in app.R mints "r:app.run".
//
// Exts intentionally lists only the lowercase ".r", not both ".r" and ".R".
// Checked internal/langspec/langspec.go first per the brief: SpecParser
// itself does no extension matching at all (Extensions() just returns
// p.S.Exts verbatim; matching happens one layer up, in
// internal/index.New/indexer.IndexAll). There, index.New's byExt map is
// keyed by the exact strings from Extensions() (index/indexer.go:66-71), but
// every lookup against that map first lowercases the file's extension
// (index/indexer.go:104 `strings.ToLower(filepath.Ext(p))` and :140
// `ix.byExt[strings.ToLower(filepath.Ext(rel))]`) — matching index.LangOf's
// own lowercase-fallback behavior (index/langs.go:68). So a source file
// named "Foo.R" is already routed to the ".r" entry without needing a
// separate ".R" entry in Exts; adding one would be redundant. This is
// confirmed end-to-end by TestRSpecIndexAllUppercaseExtension in r_test.go,
// which runs a real .R file through index.New + IndexAll.
var rSpec = Spec{
	Lang: graph.LangR,
	Exts: []string{".r"},
	Qual: QualFileStem,
	Defs: []Def{
		{
			Kind: graph.KFunc,
			// `run <- function(x) { ... }`, `my.func <- function(x, y) ...`
			// (dot-names are idiomatic in R).
			Re:   regexp.MustCompile(`^\s*([\w.]+)\s*<-\s*function\s*\(`),
			Name: 1,
		},
		{
			Kind: graph.KFunc,
			// `run = function(x) { ... }` (the less common `=` assignment
			// form of the same top-level function definition).
			Re:   regexp.MustCompile(`^\s*([\w.]+)\s*=\s*function\s*\(`),
			Name: 1,
		},
	},
}

func init() { registry = append(registry, rSpec) }
