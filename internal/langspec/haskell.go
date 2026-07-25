package langspec

import (
	"regexp"

	"github.com/jxsl13/spectackle/internal/graph"
)

// haskellSpec covers .hs files. Qualification is QualFileStem, mirroring
// pythonSpec and rustSpec: one file is treated as one module, so
// `run :: Int -> Int` in App.hs mints "hs:App.run".
//
// The canonical def marker for a function/value is its column-0 top-level
// type signature (`run :: Int -> Int`), not the equation line(s) that follow
// it (`run x = x + 1`). This is a deliberate v0 tradeoff: a type signature
// is optional in Haskell, so a function defined only via equations with no
// signature is not minted as a node. Distinguishing signature-less
// definitions would need tracking indentation-sensitive layout (where
// blocks, let/in), which this single-pass line scanner does not attempt —
// deferred to a later langspec iteration, matching pythonSpec's and
// rustSpec's precedent of documenting v0 conflations/limits rather than
// silently guessing.
var haskellSpec = Spec{
	Lang: graph.LangHs,
	Exts: []string{".hs"},
	Qual: QualFileStem,
	Defs: []Def{
		{
			Kind: graph.KFunc,
			// Column-0 type signature: `run :: Int -> Int`,
			// `helper' :: Int -> Int`. The `^` anchor (no leading `\s*`,
			// unlike pythonSpec/rustSpec) is intentional: it excludes
			// indented signatures inside where/let blocks, which are not
			// top-level definitions.
			Re:   regexp.MustCompile(`^([a-z_]\w*'?)\s*::`),
			Name: 1,
		},
		{
			Kind: graph.KFunc,
			// Multi-line top-level signature: the name alone on its own
			// column-0 line, with `::` landing on an indented continuation
			// line instead (R-0005, this Spec's headline miss — an
			// idiomatic style for long signatures):
			//   multiply
			//     :: Int -> Int -> Int
			// The framework scans one physical line at a time
			// (langspec.go), so a single Def regex cannot itself look ahead
			// to confirm the next line starts with `::` — matching purely
			// on "a bare identifier alone on a column-0 line" instead relies
			// on Haskell's layout (off-side) rule: everything that isn't a
			// new top-level declaration MUST be indented past column 0, so
			// any column-0 line can only ever be starting a fresh
			// declaration. A bare name is therefore always either this
			// multi-line-signature form or a multi-line equation whose
			// argument patterns wrap to the next line (`foo\n  x y = ...`)
			// — both cases genuinely start a definition of that name at
			// this line, so minting a node here is correct either way, not
			// a false positive.
			Re:   regexp.MustCompile(`^([a-z_]\w*'?)\s*$`),
			Name: 1,
		},
		{
			Kind: graph.KFunc,
			// Symbolic/operator-named top-level definition, parenthesized
			// (R-0005): `(<+>) :: Int -> Int -> Int`. The primary signature
			// Def's name group (`[a-z_]\w*'?`) cannot match an operator
			// token at all, so this needs its own Def; the captured name is
			// the bare operator (`<+>`), matching how it's referred to
			// everywhere else (e.g. a call site `1 <+> 2`).
			Re:   regexp.MustCompile(`^\(([^)]+)\)\s*::`),
			Name: 1,
		},
		{
			Kind: graph.KType,
			// `data Tree = Leaf | Node Tree Tree`, `newtype Wrapper = ...`,
			// `type Alias = Int`.
			Re:   regexp.MustCompile(`^(?:data|newtype|type)\s+(\w+)`),
			Name: 1,
		},
		{
			Kind: graph.KType,
			// `class Show a where` or `class (Eq a) => Container a where`
			// (an optional typeclass context before the class being
			// declared). The context, if present, is consumed up through
			// its trailing `=>` so the captured group is always the class
			// name, never a superclass from the context.
			Re:   regexp.MustCompile(`^class\s+(?:[\w() ,=>]+=>\s*)?(\w+)`),
			Name: 1,
		},
	},
}

func init() { registry = append(registry, haskellSpec) }
