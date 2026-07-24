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
