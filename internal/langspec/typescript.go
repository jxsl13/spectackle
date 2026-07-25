package langspec

import (
	"regexp"

	"github.com/jxsl13/spectackle/internal/graph"
)

// typescriptSpec covers .ts/.tsx files. Qualification is QualFileStem,
// mirroring javascriptSpec: one file is one module, so `function run` in
// app.ts mints "ts:app.run". The function/class/arrow shapes are copied
// (and, per R-0005 below, hardened) from javascriptSpec — TypeScript is a
// superset of JavaScript for these forms — plus TypeScript-only
// declarations: interface, type alias, enum, and namespace/module.
//
// R-0005 hardening: TypeScript's dominant idiom, the class method (instance
// method, constructor, static method, getter/setter, decorated method —
// none of which carry a `function` keyword), had no Def at all; `abstract`
// between `export` and `class` broke the class Def outright; a generic
// type alias (`type Result<T> = ...`) broke the type-alias Def, which
// required `\s*=` immediately after the name; every Def was anchored with
// a bare `^` and no leading `\s*`, so a namespace body's nested
// declarations (and any other indented construct) could never match; and
// CallRe was never set, so there were zero call edges and every node's
// EndLine collapsed to its Line regardless of real body length. All are
// fixed below, mirroring javascriptSpec's method Def and CallRe/Stop setup.
var typescriptSpec = Spec{
	Lang: graph.LangTS,
	Exts: []string{".ts", ".tsx"},
	Qual: QualFileStem,
	Defs: []Def{
		{
			Kind: graph.KFunc,
			// `function run(x) {`, `export function run(x) {`,
			// `async function run(x) {`, `function* gen() {`, and (leading
			// `\s*`) a namespace-nested `export function clamp(...) {`.
			Re:   regexp.MustCompile(`^\s*(?:export\s+)?(?:default\s+)?(?:async\s+)?function\s*\*?\s+(\w+)`),
			Name: 1,
		},
		{
			Kind: graph.KType,
			// `class Foo {`, `export class Foo extends Base {`,
			// `export abstract class BaseService {` (the `abstract`
			// keyword previously broke this Def entirely — the class
			// itself got no node, not just its methods), `export default
			// class Widget {`.
			Re:   regexp.MustCompile(`^(?:export\s+)?(?:default\s+)?(?:abstract\s+)?class\s+(\w+)`),
			Name: 1,
		},
		{
			Kind: graph.KFunc,
			// `const run = (x) => {`, `export const run = async (x) => {`,
			// `let run = x => x * 2`, TSX component arrows like
			// `export const App = () => { ... }` — leading `\s*` tolerates
			// nesting the same way the function Def now does.
			Re:   regexp.MustCompile(`^\s*(?:export\s+)?(?:const|let|var)\s+(\w+)\s*=\s*(?:async\s*)?(?:\(|\w+\s*=>)`),
			Name: 1,
		},
		{
			Kind: graph.KType,
			// `interface Foo {` or `export interface Foo extends Base {`
			Re:   regexp.MustCompile(`^(?:export\s+)?interface\s+(\w+)`),
			Name: 1,
		},
		{
			Kind: graph.KType,
			// `type Foo = ...` or `export type Result<T> = ...` — the
			// optional `(?:<[^>]*>)?` between the name and `=` is R-0005's
			// fix: a generic type alias previously broke this Def, which
			// required `\s*=` immediately after the captured name.
			Re:   regexp.MustCompile(`^(?:export\s+)?type\s+(\w+)(?:<[^>]*>)?\s*=`),
			Name: 1,
		},
		{
			Kind: graph.KType,
			// `enum Foo {`, `export enum Foo {`, `export const enum Foo {`
			Re:   regexp.MustCompile(`^(?:export\s+)?(?:const\s+)?enum\s+(\w+)`),
			Name: 1,
		},
		{
			Kind: graph.KType,
			// `namespace Utils {`, `export namespace Utils {`,
			// `module Foo {` (TypeScript's older, now-discouraged
			// `module` synonym for `namespace`) — R-0005: previously no
			// Def existed for either form at all.
			Re:   regexp.MustCompile(`^(?:export\s+)?(?:namespace|module)\s+(\w+)`),
			Name: 1,
		},
		{
			Kind: graph.KFunc,
			// Class instance/static/async methods, the constructor, and
			// getter/setter accessors — TypeScript's dominant OOP idiom
			// and, per R-0005, previously entirely unmatched (no method
			// Def existed at all): `constructor(private readonly apiUrl:
			// string) {`, `async getUser(id: string): Promise<User> {`,
			// `static create(...): UserService {`, `get size(): number
			// {`, `set apiRoot(url: string) {`, `abstract getUser(id:
			// string): Promise<User>;` (abstract/interface method, no
			// body — semicolon-terminated), `private lookupCache(` (a
			// multi-line signature: the params and closing `)` continue
			// on later lines; cspan.Span's own multi-line header scan
			// resolves the rest once CallRe below is wired). A decorated
			// method (`@Get(...)` on the preceding line) needs no special
			// handling: the decorator is a separate line the scanner
			// simply doesn't match, and the method line under it matches
			// this Def exactly like any other method.
			//
			// Requires >=2-space indentation (methods are always nested
			// in a class body) and the captured name to butt directly
			// against `(` (optionally through a `<T>` generic list) with
			// zero intervening whitespace — idiomatic TS never puts a
			// space between a method name and its parameter list, whereas
			// every control-flow keyword this must NOT match (`if (`,
			// `for (`, `while (`, `switch (`, `catch (`) is always
			// written with a space before `(`. Go's RE2 engine has no
			// lookahead, so this adjacency requirement — not a keyword
			// blocklist — is what keeps control-flow lines (e.g. `if
			// (cached) return cached;`) from matching. The optional
			// `(?:\s*:[^{;]*)?` after the parameter list skips past a
			// return-type annotation (`: Promise<User>`, `: User |
			// undefined`, …) without needing to parse it.
			//
			// The two endings are deliberately asymmetric: a body (`{`) or
			// an open multi-line signature (nothing left on the line) need
			// no return-type annotation to match (most methods — the
			// constructor included — have neither), but a bare `;`
			// ending REQUIRES one (`\s*:[^{;]*;\s*$`, no `?`). Without
			// that requirement a plain call statement sitting at
			// method-body indentation — `super();`, `helperCall();` — is
			// indistinguishable from a semicolon-terminated abstract/
			// interface method decl; requiring the `: ReturnType`
			// annotation (which every abstract/interface member in
			// idiomatic TS carries, even if only `: void`) is what a
			// bare call statement structurally never has.
			Re:   regexp.MustCompile(`^\s{2,}(?:(?:public|private|protected|static|readonly|abstract|override|async)\s+)*(?:get\s+|set\s+)?(?:\*\s*)?(\w+)(?:<[^>]*>)?\(([^)]*)\)?(?:(?:\s*:[^{;]*)?\s*(?:\{.*$|$)|\s*:[^{;]*;\s*$)`),
			Name: 1,
		},
	},

	// CallRe/Stop (LSP-001): TS function/method bodies are brace-delimited,
	// so the same brace-counted cspan.Span machinery c.go/java.go use
	// applies directly. Previously typescriptSpec left CallRe nil entirely
	// (R-0005), which meant zero call edges, ever, for any TS/TSX file, and
	// every node's EndLine collapsed to its Line regardless of real body
	// length (the brace-counting path is only reached when CallRe is set).
	CallRe: regexp.MustCompile(`\b([A-Za-z_]\w*)\s*\(`),
	Stop:   typescriptCallStop,
}

// typescriptCallStop lists TS control-flow keywords whose `name (` syntax
// (with the idiomatic space before `(`) structurally matches CallRe — which
// unlike the method Def above allows optional whitespace before `(` — but
// is never a call into a repo symbol. "function" covers anonymous function
// expressions; "async"/"await" cover an arrow function's own def line
// sitting inside its own brace-counted body span (`const f = async (x) =>
// {`), where CallRe's `\s*` would otherwise read "async" as a callee.
var typescriptCallStop = []string{
	"if", "for", "while", "switch", "catch", "function", "return",
	"async", "await",
}

func init() { registry = append(registry, typescriptSpec) }
