package langspec

import (
	"regexp"

	"github.com/jxsl13/spectackle/internal/graph"
)

// rSpec covers .r files (including uppercase .R sources — see the
// case-sensitivity note below). Qualification is QualFileStem, mirroring
// pythonSpec and rustSpec: one file is treated as one module, so
// `run <- function(x) x` in app.R mints "r:app.run".
//
// Exts intentionally lists only the lowercase ".r", not both ".r" and ".R".
// Checked internal/langspec/langspec.go first per the brief: SpecParser
// itself does no extension matching at all (Extensions() just returns
// p.S.Exts verbatim; matching happens one layer up, in
// internal/index.New/indexer.IndexAll). There, index.New's byExt map is
// keyed by the exact strings from Extensions() (index/indexer.go:66-71), but
// every lookup against that map first lowercases the file's extension
// (index/indexer.go:104 `strings.ToLower(filepath.Ext(p))` and :140
// `ix.byExt[strings.ToLower(filepath.Ext(rel))]`) — matching index.LangOf's
// own lowercase-fallback behavior (index/langs.go:68). So a source file
// named "Foo.R" is already routed to the ".r" entry without needing a
// separate ".R" entry in Exts; adding one would be redundant. This is
// confirmed end-to-end by TestRSpecIndexAllUppercaseExtension in r_test.go,
// which runs a real .R file through index.New + IndexAll.
//
// R-0005 findings fixed here:
//   - Backtick-quoted names (custom infix operators such as the literal
//     source text %+% wrapped in backticks, and other non-syntactic
//     identifiers): the name capture was `[\w.]+`, which excludes backticks
//     and `%`. Both function Defs now alternate between a backtick-quoted
//     span and the plain `[\w.]+` shape; the backticks stay in the captured
//     name (mirroring perl.go keeping `::` in package names), so a
//     backtick-quoted infix operator assigned via `<-` mints a node whose
//     name still carries its backticks.
//   - R6 class definitions (`Counter <- R6::R6Class("Counter", public =
//     list(...))`) minted no node at all — only the public/private
//     `name = function(` methods inside were caught, as flat unassociated
//     top-level nodes. A new KType Def now mints the class name itself from
//     the assignment target.
//   - S4 OOP (`setClass`/`setGeneric`/`setMethod`) was entirely
//     unrecognized. Three new Defs mint a KType node for setClass and KFunc
//     nodes for setGeneric/setMethod, named from each call's first quoted
//     string argument (the class/generic name) — QualFileStem then dedupes a
//     setGeneric/setMethod pair sharing a generic name the same way this
//     framework already dedupes any other same-name collision (see
//     python.go's Vector.__init__/Matrix.__init__ note).
//   - CallRe/Stop (LSP-001): rSpec previously left CallRe nil, so no R
//     function's body was ever call-scanned or brace-spanned (EndLine
//     always == Line). R function bodies are brace-delimited exactly like
//     C/C++ when they aren't one-line expression bodies (`simple_add <-
//     function(a, b) a + b`, which cspan.Span correctly leaves un-spanned:
//     no `{` on the def line and no Allman `{` on a following line, so
//     ok=false and EndLine stays Line — accurate, since there is genuinely
//     no brace body to bound). CallRe requires the callee to touch its `(`
//     with no whitespace, same reasoning as perl.go/php.go: R's
//     `for (i in ...)`/`if (...)` control forms are written with a space
//     before `(` in every fixture here, so the no-space requirement already
//     excludes them without needing to enumerate every possible loop/branch
//     spelling; Stop below backstops the no-space forms (`if(`, `for(`) and
//     the S4/R6 call-like defining forms (`setClass(`, `R6Class(`, ...)
//     whose own definition line falls inside a matched def's call-scanned
//     span (e.g. `function(` appears on literally every function's own def
//     line, so it must always be stopped).
//
// R-0005 findings NOT fixed here (documented, not silently dropped, both
// [medium] and [low] per the finding severities):
//   - [medium] Arrow split across lines (`broken_style <-` on one line,
//     `function(x) {` starting the next): the name lives on one line and
//     `function(` on the next, but every Def here (like every Def in this
//     package) is matched independently per line via
//     `def.Re.FindStringSubmatch(line)` in langspec.go's Parse — there is no
//     lookahead/lookbehind across lines available to a Spec's Def. A
//     same-line heuristic that mints a node from a bare trailing `name <-`
//     was considered and rejected: it cannot distinguish that shape from an
//     ordinary multi-line variable assignment (`x <-\n  5`), which would
//     become a false-positive KFunc node. Fixing this for real needs
//     cross-line lookahead in the shared engine (out of scope; mirrors this
//     task's python.go/shell.go carve-outs for gaps below the per-line-regex
//     layer).
//   - [low] `assign("name", function(...) ...)` dynamic-definition idiom and
//     [low] right-assignment form (`function(x) x * 3 -> triple`): per the
//     task brief, low-severity findings are not chased unless free: neither
//     is — `assign(...)` needs a new Def distinguishing a real dynamic def
//     from any other `assign(...)` call, and right-assignment needs a
//     trailing-`-> name` capture shape not shared by anything else in this
//     Spec, so both are new, untested surface area rather than incidental.
var rSpec = Spec{
	Lang: graph.LangR,
	Exts: []string{".r"},
	Qual: QualFileStem,
	Defs: []Def{
		{
			Kind: graph.KFunc,
			// `run <- function(x) { ... }`, `my.func <- function(x, y) ...`
			// (dot-names are idiomatic in R), `` `%+%` <- function(e1, e2) ``
			// (backtick-quoted custom infix operator, backticks retained).
			Re:   regexp.MustCompile("^\\s*(`[^`]+`|[\\w.]+)\\s*<-\\s*function\\s*\\("),
			Name: 1,
		},
		{
			Kind: graph.KFunc,
			// `run = function(x) { ... }` (the less common `=` assignment
			// form of the same top-level function definition), backtick
			// names included as above.
			Re:   regexp.MustCompile("^\\s*(`[^`]+`|[\\w.]+)\\s*=\\s*function\\s*\\("),
			Name: 1,
		},
		{
			Kind: graph.KType,
			// `Counter <- R6::R6Class("Counter", public = list(...))` — mint
			// the class from its assignment target; the public/private
			// methods inside `list(...)` are separately caught by the two
			// KFunc Defs above (already true before this fix).
			Re:   regexp.MustCompile(`^\s*([\w.]+)\s*<-\s*R6::R6Class\s*\(`),
			Name: 1,
		},
		{
			Kind: graph.KType,
			// `setClass("Rectangle", representation(...))`.
			Re:   regexp.MustCompile(`^\s*setClass\s*\(\s*"([^"]+)"`),
			Name: 1,
		},
		{
			Kind: graph.KFunc,
			// `setGeneric("area", function(shape) standardGeneric("area"))`.
			Re:   regexp.MustCompile(`^\s*setGeneric\s*\(\s*"([^"]+)"`),
			Name: 1,
		},
		{
			Kind: graph.KFunc,
			// `setMethod("area", "Rectangle", function(shape) { ... })` —
			// named from the generic (first quoted arg), same as setGeneric;
			// a class-qualified id (folding in "Rectangle" too) would need a
			// second Name-like capture the Def shape doesn't support.
			Re:   regexp.MustCompile(`^\s*setMethod\s*\(\s*"([^"]+)"`),
			Name: 1,
		},
	},

	CallRe: regexp.MustCompile(`\b([A-Za-z_.][\w.]*)\(`),
	Stop:   rCallStop,
}

// rCallStop lists R keywords/S4-and-R6 defining calls whose `name(` syntax
// structurally matches CallRe but is never a call into a repo symbol.
// "function" must always be stopped: it appears literally on every function
// definition's own line, which is itself part of that def's call-scanned
// body span.
var rCallStop = []string{
	"function", "if", "for", "while",
	"setClass", "setGeneric", "setMethod", "representation",
	"standardGeneric", "R6Class",
}

func init() { registry = append(registry, rSpec) }
