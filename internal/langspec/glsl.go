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
// CallRe is now set (T-0122; R-0005 glsl.md's [high] "call edges between
// functions" finding, and its [medium] EndLine finding as a direct
// consequence — with CallRe nil, Parse never invoked cspan.Span, so
// EndLine always collapsed to Line regardless of body length): GLSL
// functions are brace-delimited exactly like C, so the same brace-counted
// body span + call-site scan c.go/cpp.go already use applies unchanged;
// this was a purely in-architecture fix (glslSpec simply never enabled the
// mechanism other C-family langspecs already ship), not a scanner
// limitation, per R-0005 glsl.md's own "TREE-SITTER DELTA" analysis.
var glslSpec = Spec{
	Lang: graph.LangGLSL,
	Exts: []string{".comp", ".vert", ".frag", ".geom", ".tesc", ".tese", ".glsl"},
	Qual: QualFlat,
	Defs: []Def{
		{
			Kind: graph.KType,
			// `struct Material { ... };` — GLSL's only named-type
			// construct (R-0005 glsl.md's [high] "struct declarations"
			// finding). Trailing `{` required so an ordinary declaration of
			// an existing struct-typed variable never mints a bogus type.
			Re:   regexp.MustCompile(`^\s*struct\s+(\w+)\s*\{`),
			Name: 1,
		},
		{
			Kind: graph.KFunc,
			// `void main() {`, `float sdf(vec3 p) {`, `vec4 shade(...) {`
			// Prototype with `;` never matches because the balanced-close
			// alternative ends with `\{`. layout qualifiers and control flow
			// are rejected by the mandatory return-type prefix (a bare
			// control keyword like `if`/`for`/`while` can never supply the
			// two separate tokens — TYPE then NAME — the prefix requires
			// before `(`). Two additions over the pre-T-0122 shape:
			//   - an optional leading parenthesized qualifier, e.g.
			//     `subroutine(shading_t) vec3 f(...) {` (R-0005 glsl.md's
			//     [medium] "qualifier/attribute token ... same line as the
			//     signature" finding — the qualifier's own parens used to
			//     break the mandatory TYPE-SEQUENCE prefix before it ever
			//     reached the real return type);
			//   - a multi-line signature whose params wrap onto following
			//     lines, e.g. `vec3 computeLighting(\n    vec3 normal,\n
			//     ...\n) {` (R-0005 glsl.md's [high] "multi-line function
			//     signature" finding) — the trailing alternation accepts
			//     params ending in `,` (continues) or nothing at all right
			//     after `(` (bare open, continues), in addition to the
			//     original balanced-close-then-`{` shape.
			Re:   regexp.MustCompile(`^\s*(?:subroutine\s*\([^)]*\)\s*)?(?:[A-Za-z_]\w*(?:\s+[A-Za-z_]\w*)*\s+|void\s+)(\w+)\s*\((?:([^;{}]*)\)\s*\{|[^;{}]*,\s*$|$)`),
			Name: 1,
			Sig:  2,
		},
	},

	// CallRe/Stop (LSP-001): same shape as cSpec/cppSpec's CallRe (see
	// c.go's comment). Stop is cFamilyCallStop plus "subroutine": a
	// subroutine-qualified def's own signature line (part of its
	// brace-counted body span, like any def line) contains
	// `subroutine(shading_t)`, which would otherwise mint a spurious
	// `-> glsl:subroutine` edge from every such function.
	CallRe: regexp.MustCompile(`\b([A-Za-z_]\w*)\s*\(`),
	Stop:   append([]string{"subroutine"}, cFamilyCallStop...),
}

func init() { registry = append(registry, glslSpec) }
