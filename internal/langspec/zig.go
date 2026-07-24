package langspec

import (
	"regexp"

	"github.com/jxsl13/spectacle/internal/graph"
)

// zigSpec covers .zig files. Qualification is QualFileStem (a Zig source
// file is a struct/namespace of its own): `fn run` in app.zig mints
// "zig:app.run".
var zigSpec = Spec{
	Lang: graph.LangZig,
	Exts: []string{".zig"},
	Qual: QualFileStem,
	Defs: []Def{
		{
			Kind: graph.KFunc,
			// `fn run() void {`, `pub fn run(...)`, `export fn run(...)`,
			// `extern "c" fn run(...)`, `pub inline fn run(...)`. Anchored at
			// line start, so a `fn` type appearing mid-line (e.g. a
			// `comptime f: fn () void` parameter) is never matched.
			Re:   regexp.MustCompile(`^\s*(?:pub\s+)?(?:export\s+)?(?:extern\s+(?:"\w+"\s+)?)?(?:inline\s+)?fn\s+(\w+)`),
			Name: 1,
		},
		{
			Kind: graph.KType,
			// `const Point = struct {`, `pub const Color = enum {`,
			// `const Handle = opaque {`, `const Shape = union {`, optionally
			// `packed`/`extern` qualified.
			Re:   regexp.MustCompile(`^\s*(?:pub\s+)?const\s+(\w+)\s*=\s*(?:packed\s+|extern\s+)?(?:struct|enum|union|opaque)\b`),
			Name: 1,
		},
	},
}

func init() { registry = append(registry, zigSpec) }
