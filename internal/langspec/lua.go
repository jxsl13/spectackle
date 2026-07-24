package langspec

import (
	"regexp"

	"github.com/jxsl13/spectacle/internal/graph"
)

// luaSpec covers .lua files. Qualification is QualFileStem (a Lua module
// is its own namespace): `function run()` in app.lua mints "lua:app.run".
//
// Matched names may retain dots or colons (`M.name`, `Obj:method`) because
// Lua uses them to spell table-qualified functions and methods directly in
// the `function` statement; ids.Mint accepts any non-whitespace qualifier,
// so these pass through unqualified further (e.g. "lua:app.Obj:method").
var luaSpec = Spec{
	Lang: graph.LangLua,
	Exts: []string{".lua"},
	Qual: QualFileStem,
	Defs: []Def{
		{
			Kind: graph.KFunc,
			// `function run(x)`, `local function run(x)`, `function M.run(x)`,
			// `function Obj:method(x)`.
			Re:   regexp.MustCompile(`^\s*(?:local\s+)?function\s+([A-Za-z_][A-Za-z0-9_.:]*)\s*\(`),
			Name: 1,
		},
		{
			Kind: graph.KFunc,
			// `run = function(x)`, `local run = function(x)`.
			Re:   regexp.MustCompile(`^\s*(?:local\s+)?([A-Za-z_][A-Za-z0-9_.]*)\s*=\s*function\s*\(`),
			Name: 1,
		},
	},
}

func init() { registry = append(registry, luaSpec) }
