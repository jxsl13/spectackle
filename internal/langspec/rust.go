package langspec

import (
	"regexp"

	"github.com/jxsl13/spectacle/internal/graph"
)

// rustSpec covers .rs files. Qualification is QualFileStem, mirroring
// pythonSpec and javascriptSpec: one file is treated as one module, so
// `fn run` in app.rs mints "rs:app.run". graph.Lang("rs") is used directly
// rather than a new graph.LangRust const — the orchestrator wires up shared
// constants (and extLang registration) once every langspec batch lands.
//
// Indented `fn` lines (impl/trait methods) mint the same KFunc kind as
// top-level functions in this v0, matching pythonSpec's def/method
// convention — distinguishing KMethod would need tracking the enclosing
// impl/trait block, deferred to a later langspec iteration.
var rustSpec = Spec{
	Lang: graph.Lang("rs"),
	Exts: []string{".rs"},
	Qual: QualFileStem,
	Defs: []Def{
		{
			Kind: graph.KFunc,
			// `fn run(x: i32) {`, `pub fn run(...)`, `pub(crate) fn run(...)`,
			// `async fn run(...)`, `unsafe fn run(...)`, and indented trait/impl
			// methods such as `    fn area(&self) -> f64;`.
			Re:   regexp.MustCompile(`^\s*(?:pub(?:\(.*?\))?\s+)?(?:async\s+)?(?:unsafe\s+)?fn\s+(\w+)`),
			Name: 1,
		},
		{
			Kind: graph.KType,
			// `struct Point {`, `pub enum Color {`, `trait Shape {`.
			Re:   regexp.MustCompile(`^\s*(?:pub(?:\(.*?\))?\s+)?(?:struct|enum|trait)\s+(\w+)`),
			Name: 1,
		},
		{
			Kind: graph.KVar,
			// `pub const MAX: i32 = 100;`, `static COUNTER: i32 = 0;`.
			Re:   regexp.MustCompile(`^\s*(?:pub\s+)?(?:const|static)\s+(\w+)`),
			Name: 1,
		},
	},
}

func init() { registry = append(registry, rustSpec) }
