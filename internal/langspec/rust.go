package langspec

import (
	"regexp"

	"github.com/jxsl13/spectackle/internal/graph"
)

// rustSpec covers .rs files. Qualification is QualFileStem, mirroring
// pythonSpec and javascriptSpec: one file is treated as one module, so
// `fn run` in app.rs mints "rs:app.run".
//
// Indented `fn` lines (impl/trait methods) mint the same KFunc kind as
// top-level functions in this v0, matching pythonSpec's def/method
// convention — distinguishing KMethod would need tracking the enclosing
// impl/trait block, deferred to a later langspec iteration. The same
// deferral covers impl-block-qualified method names (R-0005: `Point::new`
// vs `Circle::new` both mint the bare file-stem-qualified name "new" and
// collide, disambiguated only by the indexer's "~2"/"~3" suffixes) — fixing
// that needs the parser to track which impl/trait block a line is inside,
// a structural change beyond this v0's per-line regex Defs.
var rustSpec = Spec{
	Lang: graph.LangRs,
	Exts: []string{".rs"},
	Qual: QualFileStem,
	Defs: []Def{
		{
			Kind: graph.KFunc,
			// `fn run(x: i32) {`, `pub fn run(...)`, `pub(crate) fn run(...)`,
			// `async fn run(...)`, `unsafe fn run(...)`, `pub const fn run(...)`
			// (R-0005: `const` was missing from the modifier chain, which both
			// dropped the node here AND let it fall through to the KVar Def
			// below, which mis-captured the literal word "fn" as a spurious
			// variable name — see that Def's comment), and indented trait/impl
			// methods such as `    fn area(&self) -> f64;`.
			Re:   regexp.MustCompile(`^\s*(?:pub(?:\(.*?\))?\s+)?(?:const\s+)?(?:async\s+)?(?:unsafe\s+)?fn\s+(\w+)`),
			Name: 1,
		},
		{
			Kind: graph.KType,
			// `struct Point {`, `pub enum Color {`, `trait Shape {`,
			// `union RawValue {` (R-0005: union was the one struct-shaped
			// item kind missing from this alternation).
			Re:   regexp.MustCompile(`^\s*(?:pub(?:\(.*?\))?\s+)?(?:struct|enum|trait|union)\s+(\w+)`),
			Name: 1,
		},
		{
			Kind: graph.KType,
			// `pub type ShapeList = Vec<Box<dyn Shape>>;` (R-0005: type
			// aliases had no Def at all — a named type binding `find
			// scope=code` should surface but silently didn't).
			Re:   regexp.MustCompile(`^\s*(?:pub(?:\(.*?\))?\s+)?type\s+(\w+)`),
			Name: 1,
		},
		{
			Kind: graph.KVar,
			// `pub const MAX: i32 = 100;`, `static COUNTER: i32 = 0;`,
			// `pub(crate) static REGISTRY: [i32; 4] = [0, 0, 0, 0];` (R-0005:
			// the old `pub\s+` prefix didn't accept any `pub(...)` form, unlike
			// the KFunc/KType Defs above). The trailing `\s*:` requirement
			// (every real Rust const/static always has a `NAME: Type = ...`
			// shape, type annotation mandatory) is what keeps this Def from
			// also matching `pub const fn compute_default() -> i32 {` and
			// mis-capturing the literal word "fn" as a variable name — that
			// line has no `:` anywhere, so this Def simply never matches it,
			// no negative lookahead required (R-0005, RE2 has none anyway).
			Re:   regexp.MustCompile(`^\s*(?:pub(?:\(.*?\))?\s+)?(?:const|static)\s+(\w+)\s*:`),
			Name: 1,
		},
	},

	// CallRe/Stop (LSP-001): Rust function/method/impl bodies are
	// brace-delimited exactly like C/C++/Java, so the same brace-counted
	// cspan.Span machinery applies directly (R-0005: rustSpec previously
	// left CallRe nil, which meant zero call edges, ever, for any Rust
	// file, and also meant every KFunc node's span collapsed to its
	// single declaration line regardless of body length).
	CallRe: regexp.MustCompile(`\b([A-Za-z_]\w*)\s*\(`),
	Stop:   rustCallStop,
}

// rustCallStop lists Rust control-flow keywords whose `name (` syntax
// structurally matches CallRe but is never a call into a repo symbol.
var rustCallStop = []string{
	"if", "for", "while", "match", "loop", "return",
}

func init() { registry = append(registry, rustSpec) }
