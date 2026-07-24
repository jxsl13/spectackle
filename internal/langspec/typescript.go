package langspec

import (
	"regexp"

	"github.com/jxsl13/spectacle/internal/graph"
)

// typescriptSpec covers .ts/.tsx files. Qualification is QualFileStem,
// mirroring javascriptSpec: one file is one module, so `function run` in
// app.ts mints "ts:app.run". The three function/class/arrow shapes are
// copied verbatim from javascriptSpec (TypeScript is a superset of
// JavaScript for these forms), plus three TypeScript-only type-level
// declarations: interface, type alias, and enum.
var typescriptSpec = Spec{
	Lang: graph.Lang("ts"),
	Exts: []string{".ts", ".tsx"},
	Qual: QualFileStem,
	Defs: []Def{
		{
			Kind: graph.KFunc,
			// `function run(x) {`, `export function run(x) {`,
			// `async function run(x) {`, `function* gen() {`.
			Re:   regexp.MustCompile(`^(?:export\s+)?(?:async\s+)?function\s*\*?\s+(\w+)`),
			Name: 1,
		},
		{
			Kind: graph.KType,
			// `class Foo {` or `export class Foo extends Base {`
			Re:   regexp.MustCompile(`^(?:export\s+)?class\s+(\w+)`),
			Name: 1,
		},
		{
			Kind: graph.KFunc,
			// `const run = (x) => {`, `export const run = async (x) => {`,
			// `let run = x => x * 2`, and TSX component arrows like
			// `export const App = () => { ... }`.
			Re:   regexp.MustCompile(`^(?:export\s+)?(?:const|let|var)\s+(\w+)\s*=\s*(?:async\s*)?(?:\(|\w+\s*=>)`),
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
			// `type Foo = ...` or `export type Foo<T> = ...`
			Re:   regexp.MustCompile(`^(?:export\s+)?type\s+(\w+)\s*=`),
			Name: 1,
		},
		{
			Kind: graph.KType,
			// `enum Foo {`, `export enum Foo {`, `export const enum Foo {`
			Re:   regexp.MustCompile(`^(?:export\s+)?(?:const\s+)?enum\s+(\w+)`),
			Name: 1,
		},
	},
}

func init() { registry = append(registry, typescriptSpec) }
