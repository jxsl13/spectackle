package langspec

import (
	"regexp"

	"github.com/jxsl13/spectackle/internal/graph"
)

// javascriptSpec covers .js/.mjs files. Qualification is QualFileStem,
// mirroring pythonSpec: one file is one module, so `function run` in app.js
// mints "js:app.run".
//
// R-0005 hardening: the original three Defs (function/class/arrow) were
// anchored with a bare `^` and no leading `\s*`, so any declaration with
// column>0 leading whitespace — nested functions, and every class method —
// never matched at all; `export default` broke both the function and class
// Defs outright (the extra `default` token sits where the anchor expected
// `function`/`class`); and CommonJS's `exports.foo = function(){}` idiom
// had no Def whatsoever. All three are fixed below, plus a new class-method
// Def (constructor/instance/async/static/getter/setter — JS has no
// `function` keyword for any of these, so there was previously no method
// Def at all) and CallRe/Stop so call edges and real brace-counted body
// spans exist for JS for the first time (LSP-001).
var javascriptSpec = Spec{
	Lang: graph.LangJS,
	Exts: []string{".js", ".mjs"},
	Qual: QualFileStem,
	Defs: []Def{
		{
			Kind: graph.KFunc,
			// `function run(x) {`, `export function run(x) {`,
			// `async function run(x) {`, `function* gen() {`,
			// `export default function mainEntry() {` (leading `\s*`
			// tolerates nesting/indentation; `(?:default\s+)?` tolerates
			// the ES module default-export form).
			Re:   regexp.MustCompile(`^\s*(?:export\s+)?(?:default\s+)?(?:async\s+)?function\s*\*?\s+(\w+)`),
			Name: 1,
		},
		{
			Kind: graph.KType,
			// `class Foo {`, `export class Foo extends Base {`,
			// `export default class Widget {` (`(?:default\s+)?` mirrors
			// the function Def's default-export fix).
			Re:   regexp.MustCompile(`^(?:export\s+)?(?:default\s+)?class\s+(\w+)`),
			Name: 1,
		},
		{
			Kind: graph.KFunc,
			// `const run = (x) => {`, `export const run = async (x) => {`,
			// `let run = x => x * 2` — leading `\s*` tolerates a nested
			// (block-scoped) arrow declaration the same way the function
			// Def now does.
			Re:   regexp.MustCompile(`^\s*(?:export\s+)?(?:const|let|var)\s+(\w+)\s*=\s*(?:async\s*)?(?:\(|\w+\s*=>)`),
			Name: 1,
		},
		{
			Kind: graph.KFunc,
			// Class instance/static/async methods, the constructor, and
			// getter/setter accessors — none carry a `function` keyword,
			// which is exactly why no method Def existed before (R-0005's
			// headline miss): `constructor(value) {`, `addValue(x) {`,
			// `async fetchRemote() {`, `static create() {`,
			// `get value() {`. Requires >=2-space indentation (methods are
			// always nested in a class body) and the captured name to
			// butt directly against `(` with zero intervening whitespace —
			// idiomatic JS never puts a space between a method name and
			// its parameter list, whereas every control-flow keyword this
			// must NOT match (`if (`, `for (`, `while (`, `switch (`,
			// `catch (`) is always written with a space before `(`
			// (enforced by every mainstream style guide/formatter). Go's
			// RE2 engine has no lookahead, so this adjacency requirement —
			// not a keyword blocklist — is what keeps control-flow lines
			// from matching.
			Re:   regexp.MustCompile(`^\s{2,}(?:static\s+)?(?:async\s+)?(?:get\s+|set\s+)?(?:\*\s*)?(\w+)\(`),
			Name: 1,
		},
		{
			Kind: graph.KFunc,
			// CommonJS export assignment (R-0005): `exports.helper =
			// function helper(x) {` / `module.exports.helper = function
			// (x) {`. An extremely common Node.js idiom that matched no
			// Def at all before. Name captures the exported property
			// (`helper`), not the (optional, often redundant) function
			// expression's own name.
			Re:   regexp.MustCompile(`^\s*(?:module\.)?exports\.(\w+)\s*=\s*(?:async\s+)?function\b`),
			Name: 1,
		},
	},

	// CallRe/Stop (LSP-001): JS/MJS function and method bodies are
	// brace-delimited, so the same brace-counted cspan.Span machinery
	// c.go/java.go use applies directly. Previously javascriptSpec left
	// CallRe nil entirely, which per langspec.go's documented default
	// meant zero call edges, ever, for any JS file, AND every node's
	// EndLine collapsed to its Line (no real body span) since the
	// brace-counting path is only reached when CallRe is set.
	CallRe: regexp.MustCompile(`\b([A-Za-z_]\w*)\s*\(`),
	Stop:   javascriptCallStop,
}

// javascriptCallStop lists JS control-flow keywords whose `name (` syntax
// (with the idiomatic space before `(`) structurally matches CallRe — which
// unlike the method Def above allows optional whitespace before `(` — but
// is never a call into a repo symbol. "function" is included for anonymous
// function expressions (`function (x) { ... }`), whose keyword directly
// abuts `(` when the expression is unnamed; "async" and "await" are
// included because an arrow function's own def line (`let f = async (x) =>
// {`) sits inside its own brace-counted body span and CallRe's
// `\s*` (unlike the method Def's strict adjacency) lets "async (" match as
// if "async" were a callee.
var javascriptCallStop = []string{
	"if", "for", "while", "switch", "catch", "function", "return",
	"async", "await",
}

// Note: javascriptSpec is registered directly in langspec.go's `registry`
// slice literal (not via an init() append like the other Specs in this
// package), so no init() is added here — doing so would double-register
// it.
