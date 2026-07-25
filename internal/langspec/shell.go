package langspec

import (
	"regexp"

	"github.com/jxsl13/spectackle/internal/graph"
)

// shellSpec covers .sh and .bash files. Qualification is QualFileStem
// (a shell script is its own namespace): `foo()` in deploy.sh mints
// "sh:deploy.foo". Three Defs cover: the two mutually-exclusive
// single-def-per-line shapes shell supports — POSIX `name() {` (with an
// optional `function` keyword prefix) and bash's parenthesis-less
// `function name {` — plus a third Def (below) for a second `name() {`
// definition packed onto the same line after a `;` (R-0005).
//
// R-0005 findings fixed here:
//   - Hyphenated function names (`pre-flight-check() {`, common in
//     CLI-subcommand-style bash scripts): the name character class was
//     `[A-Za-z0-9_]` only, silently dropping the whole symbol; both function
//     Defs and CallRe now allow `-` after the required leading
//     letter/underscore.
//   - Two function defs packed on one line (`a() { echo "a"; }; b() { echo
//     "b"; }`): langspec.go's Parse tries each Def with FindStringSubmatch
//     (first match only), so only "a" was ever found. Rather than needing
//     FindAll semantics in the shared engine (out of scope — see
//     T-0123/R-0005's "do not touch langspec.go" constraint), a third Def
//     specifically anchored at a preceding `;` finds a second packed
//     definition independently of the first Def's match on the same line.
//   - CallRe/Stop (LSP-001): shellSpec previously left CallRe nil, so
//     call-edge extraction was completely absent for shell (not merely
//     imprecise — a structurally different gap from the other four
//     languages here, since shell calls are bare commands with no `(`
//     at all, unlike every brace-language CallRe in this package). See
//     shellCallRe's own doc for the design.
//
// R-0005 findings NOT fixed here (documented, not silently dropped):
//   - Heredoc body text misparsed as a real function definition (`cat
//     <<'EOF' ... fake_func() { ... } ... EOF`): distinguishing heredoc
//     body lines from real source requires tracking open/close heredoc
//     state across lines, which no single-line Def regex can express —
//     langspec.go's Parse tries every Def against every line independently,
//     with no carried state between lines beyond what cspan.Span already
//     does for an already-matched def's own body span. Fixing this needs a
//     heredoc-aware preprocessing pass in the shared engine, out of scope
//     for a same-file regex change (mirrors this task's python.go carve-out
//     for indentation-based spans: some gaps are architecturally below the
//     regex layer this package can reach).
var shellSpec = Spec{
	Lang: graph.LangSh,
	Exts: []string{".sh", ".bash"},
	Qual: QualFileStem,
	Defs: []Def{
		{
			Kind: graph.KFunc,
			// POSIX `foo() {`, optionally bash-prefixed `function foo() {`,
			// hyphenated `pre-flight-check() {`.
			Re:   regexp.MustCompile(`^\s*(?:function\s+)?([A-Za-z_][A-Za-z0-9_-]*)\s*\(\)\s*\{?`),
			Name: 1,
		},
		{
			Kind: graph.KFunc,
			// bash-only `function foo {` (no parens).
			Re:   regexp.MustCompile(`^\s*function\s+([A-Za-z_][A-Za-z0-9_-]*)\s*\{`),
			Name: 1,
		},
		{
			Kind: graph.KFunc,
			// A second `name() {` packed onto the same line after a `;`,
			// e.g. `a() { echo "a"; }; b() { echo "b"; }` — the first Def
			// above only ever matches "a" (FindStringSubmatch stops at the
			// first hit); this Def is anchored on the preceding `;` so it
			// independently finds "b" on the same line.
			Re:   regexp.MustCompile(`;\s*([A-Za-z_][A-Za-z0-9_-]*)\s*\(\)\s*\{`),
			Name: 1,
		},
	},

	CallRe: shellCallRe,
	Stop:   shellCallStop,
}

// shellCallRe finds bare-command call sites for LSP-001, a fundamentally
// different shape from every other CallRe in this package: shell commands
// are invoked with no `(` at all (`deploy`, `main start`), so the
// C-family/perl/php/r `name(` pattern doesn't apply. Instead this matches an
// identifier that starts a simple command — anchored at the start of the
// (single, already-split) line, or immediately after one of `;`, `&`, `|`,
// or a case-arm's closing `)` (which doubles, usefully, as covering
// `pattern) command ;;` case-dispatch) — and consumes the rest of the line
// as that command's arguments (so `main start` captures callee "main", and
// a second bareword later on the same line is never independently
// re-matched as its own call). Line-leading shell keywords that would
// otherwise look like a bare command (`if`, `for`, `local`, ...) are
// filtered via shellCallStop.
//
// Not attempted (documented, not silently dropped): `trap cleanup EXIT`'s
// second-word-is-the-real-callback idiom — recognizing that shape specifically
// would require a shell-`trap`-only regex branch that both this task's time
// budget and the risk of over-fitting to one builtin's calling convention did
// not justify; `cleanup` is simply never captured as a callee from that line.
var shellCallRe = regexp.MustCompile(`(?:^|[;&|)])\s*([A-Za-z_][A-Za-z0-9_-]*)(?:\s+\S.*)?$`)

// shellCallStop lists shell keywords/builtins whose bare-word syntax
// structurally matches shellCallRe (start of a simple command) but is never
// a call into a repo symbol.
var shellCallStop = []string{
	"if", "then", "else", "elif", "fi",
	"for", "while", "until", "do", "done",
	"case", "esac", "in",
	"function", "local", "declare", "export", "readonly", "select", "time",
	"trap", "return", "break", "continue", "exit", "set",
}

func init() { registry = append(registry, shellSpec) }
