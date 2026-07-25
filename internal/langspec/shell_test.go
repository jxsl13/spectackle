package langspec

import (
	"reflect"
	"testing"

	"github.com/jxsl13/spectackle/internal/graph"
)

// shellSrc exercises every shellSpec Def: POSIX `name() {`, bash
// `function name() {` (still matched by the POSIX Def since the `function`
// keyword prefix is optional there), and bash's parenthesis-less
// `function name {`.
var shellSrc = []byte(`foo() {
    echo "hi"
}

function bar() {
    echo "hi"
}

function baz {
    echo "hi"
}
`)

func TestShellSpecLangExtensions(t *testing.T) {
	p := SpecParser{S: shellSpec}
	if p.Lang() != graph.LangSh {
		t.Errorf("Lang() = %v, want %v", p.Lang(), graph.LangSh)
	}
	if got, want := p.Extensions(), []string{".sh", ".bash"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Extensions() = %v, want %v", got, want)
	}
}

// TestShellSpecNodes' EndLine expectations were revised by R-0005/T-0123:
// shellSpec now sets CallRe, which enables the shared brace-counted
// body-span for every KFunc def (the finding this proves wrong: EndLine
// unconditionally equaled Line before this fix, regardless of real body
// length).
func TestShellSpecNodes(t *testing.T) {
	p := SpecParser{S: shellSpec}
	pr, err := p.Parse("pkg/app.sh", shellSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)

	want := map[graph.NodeID]struct {
		Kind    graph.NodeKind
		Line    int
		EndLine int
	}{
		"sh:app.foo": {graph.KFunc, 1, 3},
		"sh:app.bar": {graph.KFunc, 5, 7},
		"sh:app.baz": {graph.KFunc, 9, 11},
	}
	if len(pr.Nodes) != len(want) {
		t.Fatalf("got %d nodes, want %d: %+v", len(pr.Nodes), len(want), pr.Nodes)
	}
	for id, w := range want {
		n, ok := byID[id]
		if !ok {
			t.Fatalf("node %s missing, got %+v", id, pr.Nodes)
		}
		if n.Kind != w.Kind {
			t.Errorf("%s Kind = %v, want %v", id, n.Kind, w.Kind)
		}
		if n.Line != w.Line || n.EndLine != w.EndLine {
			t.Errorf("%s Line/EndLine = %d/%d, want %d/%d", id, n.Line, n.EndLine, w.Line, w.EndLine)
		}
		if n.Lang != graph.LangSh {
			t.Errorf("%s Lang = %v, want sh", id, n.Lang)
		}
		if n.File != "pkg/app.sh" {
			t.Errorf("%s File = %q, want pkg/app.sh", id, n.File)
		}
	}
}

// TestShellSpecNegativeLines pins down that command invocations, an
// if-statement, a subshell, and a case pattern never mint nodes.
func TestShellSpecNegativeLines(t *testing.T) {
	p := SpecParser{S: shellSpec}
	pr, err := p.Parse("neg.sh", []byte(`echo foo
if [ -f myfile ]; then
    echo yes
fi
(cd /tmp)
case "$x" in
  foo)
    echo match
    ;;
esac
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(pr.Nodes) != 0 {
		t.Fatalf("got %d nodes, want 0: %+v", len(pr.Nodes), pr.Nodes)
	}
}

// TestShellSpecHyphenatedFunctionName is R-0005's fix for a kebab-case
// function name (common in CLI-subcommand-style bash scripts): the name
// character class was `[A-Za-z0-9_]` only, silently dropping the whole
// symbol.
func TestShellSpecHyphenatedFunctionName(t *testing.T) {
	p := SpecParser{S: shellSpec}
	pr, err := p.Parse("pkg/app.sh", []byte("pre-flight-check() {\n    echo \"checking\"\n}\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)
	n, ok := byID["sh:app.pre-flight-check"]
	if !ok {
		t.Fatalf("node sh:app.pre-flight-check missing, got %+v", pr.Nodes)
	}
	if n.Kind != graph.KFunc || n.Line != 1 {
		t.Errorf("sh:app.pre-flight-check = %+v, want Kind=KFunc Line=1", n)
	}
}

// TestShellSpecPackedFunctionsOnOneLine is R-0005's fix for two function
// defs packed onto a single line: langspec.go's Parse tries each Def with
// FindStringSubmatch (first match only), so only "a" was ever found before
// this task added a third Def anchored on the preceding `;`.
func TestShellSpecPackedFunctionsOnOneLine(t *testing.T) {
	p := SpecParser{S: shellSpec}
	pr, err := p.Parse("pkg/app.sh", []byte(`a() { echo "a"; }; b() { echo "b"; }
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)
	for _, name := range []string{"a", "b"} {
		id := graph.NodeID("sh:app." + name)
		n, ok := byID[id]
		if !ok {
			t.Fatalf("node %s missing, got %+v", id, pr.Nodes)
		}
		if n.Kind != graph.KFunc || n.Line != 1 {
			t.Errorf("%s = %+v, want Kind=KFunc Line=1", id, n)
		}
	}
	if len(pr.Nodes) != 2 {
		t.Fatalf("got %d nodes, want 2: %+v", len(pr.Nodes), pr.Nodes)
	}
}

// shellGapSrc is R-0005's gap-shell/deploy.sh fixture verbatim (see
// scratch/findings/shell.md, scratch/gap-shell/deploy.sh).
var shellGapSrc = []byte(`#!/usr/bin/env bash
set -euo pipefail

deploy() {
    echo "deploying"
    build
    test_run
}

function build() {
    echo "building"
    test_run
}

function test_run {
    echo "testing"
}

multiline_setup()
{
    echo "multi-line setup"
}

pre-flight-check() {
    echo "checking"
}

outer() {
    inner() {
        echo "inner"
    }
    inner
}

a() { echo "a"; }; b() { echo "b"; }

_helper2() {
    echo "helper"
}

start_service() {
    echo "starting"
}

stop_service() {
    echo "stopping"
}

main() {
    local cmd="${1:-}"
    case "$cmd" in
        start) start_service ;;
        stop) stop_service ;;
        *) echo "usage: $0 {start|stop}" ;;
    esac
}

in_subshell() (
    cd /tmp
    echo "in subshell"
)

run_all() {
    deploy
    outer
    main start
    in_subshell
}

trap cleanup EXIT
cleanup() {
    echo "cleanup"
}
`)

// TestShellSpecCallEdgesGapFixture is R-0005's headline shell fix: shellSpec
// previously left CallRe nil, so reindexing this exact fixture produced 0
// edges even though deploy()/run_all()/outer()/main() all call other
// functions in the same file (see scratch/findings/shell.md's EDGES
// section). With shellCallRe/shellCallStop set, the bare-command call sites
// (no `(` at all, unlike every other language's CallRe here) now become
// edges — including through main()'s `case ... in / pattern) command ;;`
// dispatch, which shellCallRe's `)`-anchored branch happens to also cover.
func TestShellSpecCallEdgesGapFixture(t *testing.T) {
	p := SpecParser{S: shellSpec}
	pr, err := p.Parse("pkg/deploy.sh", shellGapSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	type edge struct{ src, dst graph.NodeID }
	wantEdges := map[edge]bool{
		{"sh:deploy.deploy", "sh:deploy.build"}:        true,
		{"sh:deploy.deploy", "sh:deploy.test_run"}:     true,
		{"sh:deploy.build", "sh:deploy.test_run"}:      true,
		{"sh:deploy.outer", "sh:deploy.inner"}:         true,
		{"sh:deploy.main", "sh:deploy.start_service"}:  true,
		{"sh:deploy.main", "sh:deploy.stop_service"}:   true,
		{"sh:deploy.run_all", "sh:deploy.deploy"}:      true,
		{"sh:deploy.run_all", "sh:deploy.outer"}:       true,
		{"sh:deploy.run_all", "sh:deploy.main"}:        true,
		{"sh:deploy.run_all", "sh:deploy.in_subshell"}: true,
	}
	got := map[edge]graph.Edge{}
	for _, e := range pr.Edges {
		if e.Kind != graph.ECall {
			t.Errorf("edge %+v Kind = %v, want ECall", e, e.Kind)
		}
		got[edge{e.Src, e.Dst}] = e
	}
	for w := range wantEdges {
		if _, ok := got[w]; !ok {
			t.Errorf("missing edge %s -ECall-> %s, got %+v", w.src, w.dst, pr.Edges)
		}
	}
	// Negatives: shell control-flow keywords (case-arm labels included)
	// must never become callees.
	banned := []graph.NodeID{"sh:deploy.if", "sh:deploy.local", "sh:deploy.case", "sh:deploy.start", "sh:deploy.stop"}
	for _, e := range pr.Edges {
		for _, b := range banned {
			if e.Dst == b {
				t.Errorf("false-positive edge to %s: %+v", b, e)
			}
		}
	}
}

// TestShellSpecHeredocFalsePositiveDocumented pins down a known,
// deliberately-not-fixed R-0005 gap (see shellSpec's doc comment): a
// function-def-shaped line inside a heredoc body is still misparsed as a
// real function definition, because distinguishing heredoc content from
// real source needs cross-line state no single-line Def regex can express.
// This test exists so that behavior is an intentional, documented constant
// rather than a silent surprise if it ever changes.
func TestShellSpecHeredocFalsePositiveDocumented(t *testing.T) {
	p := SpecParser{S: shellSpec}
	pr, err := p.Parse("pkg/app.sh", []byte("cat <<'EOF'\nfake_func() {\n    echo \"this is heredoc text, not real shell code\"\n}\nEOF\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)
	if _, ok := byID["sh:app.fake_func"]; !ok {
		t.Fatalf("expected (known, undesirable) false-positive node sh:app.fake_func not found — if this now fails, the heredoc gap may have been fixed elsewhere; update this test and shellSpec's doc comment accordingly")
	}
}

func TestShellSpecRegisteredInAll(t *testing.T) {
	found := false
	for _, p := range All() {
		if p.Lang() == graph.LangSh {
			found = true
		}
	}
	if !found {
		t.Error("All() does not contain a parser for graph.LangSh — shellSpec not registered via init()")
	}
}

func TestShellSpecDeterministic(t *testing.T) {
	p := SpecParser{S: shellSpec}
	pr1, err := p.Parse("pkg/app.sh", shellSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	pr2, err := p.Parse("pkg/app.sh", shellSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !reflect.DeepEqual(pr1, pr2) {
		t.Errorf("Parse is not deterministic across identical runs:\n%+v\nvs\n%+v", pr1, pr2)
	}
}
