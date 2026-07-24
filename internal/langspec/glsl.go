package langspec

import (
	"regexp"

	"github.com/jxsl13/spectackle/internal/graph"
)

// glslSpec covers GLSL (.glsl, .vert, .frag, .comp, .geom, .tesc, .tese).
// Qualification is QualFlat: entry points (main, compute shaders, etc.) are
// globally named, like CUDA kernels, so `void main() {` mints "glsl:main".
//
// GLSL is case-sensitive, so no (?i) flags on any Def.
//
// CallRe stays nil: GLSL functions are brace-delimited, but the
// specification of call-edge capture (see Spec.CallRe doc) requires
// brace-counted body spans — not yet implemented for this language.
var glslSpec = Spec{
	Lang: graph.LangGLSL,
	Exts: []string{".comp", ".vert", ".frag", ".geom", ".tesc", ".tese", ".glsl"},
	Qual: QualFlat,
	Defs: []Def{
		{
			Kind: graph.KFunc,
			// `void main() {`, `float sdf(vec3 p) {`, `vec4 shade(...) {`
			// Prototype with `;` never matches because regex ends with `\{`.
			// layout qualifiers and control flow are rejected by the prefix requirement.
			Re:   regexp.MustCompile(`^\s*(?:[A-Za-z_]\w*(?:\s+[A-Za-z_]\w*)*\s+|void\s+)(\w+)\s*\(([^;{]*)\)\s*\{`),
			Name: 1,
			Sig:  2,
		},
	},
}

func init() { registry = append(registry, glslSpec) }
