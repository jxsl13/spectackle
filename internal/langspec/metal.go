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
// CallRe stays nil: Metal functions are brace-delimited, but the
// specification of call-edge capture (see Spec.CallRe doc) requires
// brace-counted body spans — not yet implemented for this language.
var metalSpec = Spec{
	Lang: graph.LangMSL,
	Exts: []string{".metal"},
	Qual: QualFlat,
	Defs: []Def{
		{
			Kind: graph.KKernel,
			// `kernel void add_arrays(device const float* a [[buffer(0)]], ...)`
			Re:   regexp.MustCompile(`^\s*kernel\s+[\w:<>,*&\s]+?\b(\w+)\s*\(([^;{]*)\)`),
			Name: 1,
			Sig:  2,
		},
		{
			Kind: graph.KKernel,
			// `vertex float4 vertexShader(...)`, `fragment half4 fragmentShader(...)`
			Re:   regexp.MustCompile(`^\s*(?:vertex|fragment)\s+[\w:<>,*&\s]+?\b(\w+)\s*\(([^;{]*)\)`),
			Name: 1,
			Sig:  2,
		},
		{
			Kind: graph.KFunc,
			// Helper functions: `static inline float3 foo(...)`, `float compute(...) {`
			// Prototype with `;` never matches because regex ends with `\{`.
			Re:   regexp.MustCompile(`^\s*(?:static\s+|inline\s+)*(?:[A-Za-z_][\w:<>]*)\s+(\w+)\s*\(([^;{]*)\)\s*\{`),
			Name: 1,
			Sig:  2,
		},
	},
}

func init() { registry = append(registry, metalSpec) }
