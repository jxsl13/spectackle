package langspec

import (
	"regexp"

	"github.com/jxsl13/spectackle/internal/graph"
)

// metalSpec covers Metal/MSL (.metal) files. Qualification is QualFlat:
// entry points (kernel, vertex, fragment) are globally named, like CUDA
// kernels, so `kernel void add_arrays(...)` mints "msl:add_arrays".
//
// Metal is case-sensitive, so no (?i) flags on any Def.
//
// CallRe is now set (T-0122; R-0005 metal.md's [high] "intra-file calls
// between Metal functions/kernels never become call edges at all"
// finding): MSL is C++14-based and brace-delimited exactly like C/C++, so
// the same brace-counted body span (cspan.Span) + call-site scan
// (SpecParser.callEdges) that c.go/cpp.go already use applies unchanged.
// Setting CallRe also fixes R-0005 metal.md's [medium] EndLine finding as a
// side effect: with CallRe nil, Parse never invoked cspan.Span at all, so
// every Metal node's EndLine collapsed to Line regardless of the real body
// length — that was purely a consequence of CallRe being unset, not a
// separate defect to fix independently.
var metalSpec = Spec{
	Lang: graph.LangMSL,
	Exts: []string{".metal"},
	Qual: QualFlat,
	Defs: []Def{
		{
			Kind: graph.KKernel,
			// `kernel void add_arrays(device const float* a [[buffer(0)]], ...)`.
			// The alternation tried first (before requiring a balanced
			// close) is a multi-line parameter list: a first physical line
			// ending in `,` after the opening paren, whether or not it
			// happens to contain its own nested/balanced parens from a
			// `[[buffer(N)]]`-style attribute (R-0005 metal.md's [high]
			// "multi-line kernel signature whose first physical line has no
			// closing paren anywhere before EOL" and [medium] "... still
			// matches, but the greedy/backtracking single-line regex
			// backtracks to the WRONG `)`" findings — trying this
			// alternative first, anchored to end-of-line, is what keeps the
			// balanced-close alternative from ever getting a chance to
			// backtrack into a nested attribute paren on a still-open
			// signature).
			Re:   regexp.MustCompile(`^\s*kernel\s+[\w:<>,*&\s]+?\b(\w+)\s*\((?:[^;{]*,\s*$|([^;{]*)\))`),
			Name: 1,
			Sig:  2,
		},
		{
			Kind: graph.KKernel,
			// `vertex float4 vertexShader(...)`, `fragment half4 fragmentShader(...)`
			// — same multi-line-param-list tolerance as the kernel Def above.
			Re:   regexp.MustCompile(`^\s*(?:vertex|fragment)\s+[\w:<>,*&\s]+?\b(\w+)\s*\((?:[^;{]*,\s*$|([^;{]*)\))`),
			Name: 1,
			Sig:  2,
		},
		{
			Kind: graph.KFunc,
			// Helper functions: `static inline float3 foo(...)`, `float
			// compute(...) {`. Now also tolerates: an optional leading
			// `template <...>` (R-0005 metal.md's [high] "C++ function
			// template declared on a single physical line" finding — MSL
			// supports templated helpers); a multi-token generic/container
			// return type with a comma or space inside its angle brackets,
			// e.g. `array<float, 4>` (R-0005 metal.md's [high] "return type
			// is a multi-token generic/container type" finding); a bare
			// Allman signature with nothing after the closing paren, brace
			// deferred to a following line (R-0005 metal.md's [high]
			// "Allman-style brace" finding); and a multi-line parameter
			// list ending in `,`. Prototype-only lines (ending in `;`)
			// still never match.
			Re:   regexp.MustCompile(`^\s*(?:template\s*<[^>]*>\s*)?(?:static\s+|inline\s+)*(?:[A-Za-z_]\w*(?:<[^>]*>)?)\s+(\w+)\s*\((?:([^;{}]*)\)\s*(?:\{.*)?$|[^;{}]*,\s*$)`),
			Name: 1,
			Sig:  2,
		},
	},

	// CallRe/Stop (LSP-001): same shape as cSpec/cppSpec's CallRe (see
	// c.go's comment), plus "buffer"/"texture" added to Stop: MSL's
	// `[[buffer(N)]]`/`[[texture(N)]]` attribute syntax sits inside a def's
	// signature line, which is itself part of the brace-counted body span
	// scanned for calls — without stopping these, every kernel/vertex/
	// fragment with a buffer or texture argument would mint a spurious
	// `-> msl:buffer`/`-> msl:texture` edge from its own signature.
	CallRe: regexp.MustCompile(`\b([A-Za-z_]\w*)\s*\(`),
	Stop:   append([]string{"buffer", "texture"}, cFamilyCallStop...),
}

func init() { registry = append(registry, metalSpec) }
