package langspec

import (
	"regexp"

	"github.com/jxsl13/spectackle/internal/graph"
)

// perlSpec covers .pl and .pm files. Qualification is QualFileStem (a Perl
// script/module file is its own namespace): `sub run` in app.pl mints
// "pl:app.run".
//
// R-0005: perlSpec previously left CallRe nil even though Perl `sub` bodies
// are brace-delimited exactly like C/C++ (c.go, cpp.go) — the shared
// brace-counted body-span + call-edge machinery in langspec.go was simply
// never wired up. Fixed by setting CallRe/Stop, mirroring c.go: this
// simultaneously fixes the EndLine-always-==-Line span collapse (gated on
// the same CallRe != nil check) and turns on same-file call-edge extraction,
// including `$self->method()` calls (CallRe matches the bare method name;
// the preceding `->` is irrelevant to the `\b(\w+)\(` scan).
//
// CallRe requires the callee name to touch its `(` with no whitespace
// (unlike cFamilyCallStop's C-family regex, which allows `\s*`): Perl's
// idiomatic `my ($self) = @_;` / `for my $item (@items) {` unpacking forms
// put a bare identifier immediately before a space-then-`(`, and — unlike C
// keywords — the identifier there (a loop variable's name after the `$`
// sigil is stripped by \b, e.g. "item") is not a fixed, Stop-listable
// keyword: it is whatever the source happens to name its variable. Requiring
// immediate adjacency (no space) structurally excludes both `my (` and
// `$var (` unpacking forms without needing to predict every possible
// variable name, while still matching every real call in this repo's
// fixtures (`$self->helper();`, `_private_helper($item)`, ...), which are
// all written with the callee directly touching its `(`.
var perlSpec = Spec{
	Lang: graph.LangPerl,
	Exts: []string{".pl", ".pm"},
	Qual: QualFileStem,
	Defs: []Def{
		{
			Kind: graph.KFunc,
			// `sub run {`.
			Re:   regexp.MustCompile(`^\s*sub\s+([A-Za-z_][A-Za-z0-9_]*)`),
			Name: 1,
		},
		{
			Kind: graph.KType,
			// `package MyApp;`, `package MyApp::Utils;` — `::` stays in the
			// captured name.
			Re:   regexp.MustCompile(`^\s*package\s+([A-Za-z_][A-Za-z0-9_:]*)`),
			Name: 1,
		},
	},

	CallRe: regexp.MustCompile(`\b([A-Za-z_]\w*)\(`),
	Stop:   perlCallStop,
}

// perlCallStop lists Perl keywords whose `name(` (or, idiomatically,
// `name (`, already excluded by CallRe's no-space requirement) syntax
// structurally matches CallRe but is never a call into a repo symbol.
var perlCallStop = []string{
	"if", "unless", "elsif", "while", "until", "for", "foreach",
	"my", "local", "our", "return", "print", "printf", "sprintf",
}

func init() { registry = append(registry, perlSpec) }
