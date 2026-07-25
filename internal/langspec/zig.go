package langspec

import (
	"regexp"

	"github.com/jxsl13/spectackle/internal/graph"
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
		{
			Kind: graph.KType,
			// `const MyError = error{ ... };` (R-0005: Zig's error-set type
			// declaration — an extremely common idiomatic type — wasn't
			// recognized because the type Def above only accepts
			// struct/enum/union/opaque after `=`, and `error{` is a
			// different keyword entirely).
			Re:   regexp.MustCompile(`^\s*(?:pub\s+)?const\s+(\w+)\s*=\s*error\s*\{`),
			Name: 1,
		},
		{
			Kind: graph.KFunc,
			// `test "add works" { ... }` (R-0005, low severity but free:
			// Zig's unit-test block, present in virtually every idiomatic
			// Zig file). The test name string itself isn't a valid Go
			// identifier for qualification purposes, so the literal `test`
			// keyword is captured as the node name, mirroring how swift.go
			// captures the literal `init`/`deinit` keywords for those Defs.
			Re:   regexp.MustCompile(`^\s*(test)\s+"`),
			Name: 1,
		},
	},

	// CallRe/Stop (LSP-001): Zig function/struct/error-set bodies are
	// brace-delimited exactly like C/C++/Java, so the same brace-counted
	// cspan.Span machinery applies directly (R-0005: zigSpec previously
	// left CallRe nil, which meant zero call edges, ever, for any Zig file,
	// and also meant every KFunc node's span collapsed to its declaration
	// line regardless of body length).
	CallRe: regexp.MustCompile(`\b([A-Za-z_]\w*)\s*\(`),
	Stop:   zigCallStop,
}

// zigCallStop lists Zig control-flow keywords whose `name (` syntax
// structurally matches CallRe but is never a call into a repo symbol.
var zigCallStop = []string{
	"if", "for", "while", "switch", "catch", "return",
}

func init() { registry = append(registry, zigSpec) }
