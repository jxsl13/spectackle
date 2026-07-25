package langspec

import (
	"reflect"
	"testing"

	"github.com/jxsl13/spectackle/internal/graph"
)

// perlSrc exercises every perlSpec Def: `sub` and `package` (both a plain
// name and a `::`-qualified name, which stays in the captured name).
var perlSrc = []byte(`package MyApp;

sub run {
    return 1;
}

package MyApp::Utils;

sub helper {
    return 2;
}
`)

func TestPerlSpecLangExtensions(t *testing.T) {
	p := SpecParser{S: perlSpec}
	if p.Lang() != graph.LangPerl {
		t.Errorf("Lang() = %v, want %v", p.Lang(), graph.LangPerl)
	}
	if got, want := p.Extensions(), []string{".pl", ".pm"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Extensions() = %v, want %v", got, want)
	}
}

// TestPerlSpecNodes' EndLine expectations were revised by R-0005/T-0123:
// perlSpec now sets CallRe (mirroring c.go), which enables the shared
// brace-counted body-span computation in langspec.go's Parse for every
// KFunc/KMethod def. Before that fix, EndLine unconditionally equaled Line
// for every node regardless of real body length (the finding this proves
// wrong) — package (KType) nodes are unaffected either way, since span
// computation is gated on Kind == KFunc/KMethod.
func TestPerlSpecNodes(t *testing.T) {
	p := SpecParser{S: perlSpec}
	pr, err := p.Parse("pkg/app.pl", perlSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)

	want := map[graph.NodeID]struct {
		Kind    graph.NodeKind
		Line    int
		EndLine int
	}{
		"pl:app.MyApp":        {graph.KType, 1, 1},
		"pl:app.run":          {graph.KFunc, 3, 5},
		"pl:app.MyApp::Utils": {graph.KType, 7, 7},
		"pl:app.helper":       {graph.KFunc, 9, 11},
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
		if n.Lang != graph.LangPerl {
			t.Errorf("%s Lang = %v, want pl", id, n.Lang)
		}
		if n.File != "pkg/app.pl" {
			t.Errorf("%s File = %q, want pkg/app.pl", id, n.File)
		}
	}
}

// TestPerlSpecNegativeLines pins down that an anonymous sub assignment, a
// commented-out sub, and a plain call never mint nodes.
func TestPerlSpecNegativeLines(t *testing.T) {
	p := SpecParser{S: perlSpec}
	pr, err := p.Parse("neg.pl", []byte(`my $f = sub {
    return 1;
};
# sub foo
foo();
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(pr.Nodes) != 0 {
		t.Fatalf("got %d nodes, want 0: %+v", len(pr.Nodes), pr.Nodes)
	}
}

func TestPerlSpecRegisteredInAll(t *testing.T) {
	found := false
	for _, p := range All() {
		if p.Lang() == graph.LangPerl {
			found = true
		}
	}
	if !found {
		t.Error("All() does not contain a parser for graph.LangPerl — perlSpec not registered via init()")
	}
}

// perlGapSrc is R-0005's gap-perl/App.pm fixture verbatim (see
// scratch/findings/perl.md and scratch/gap-perl/App.pm): 3 packages (incl.
// the block-form `package MyApp::Widget { ... }`), 9 subs (incl. an
// Allman-brace sub, a `($$)` prototype, and a modern `($name, $greeting =
// ...)` signature), and 5 same-file call sites this task's CallRe/Stop fix
// must turn into edges: run->helper, run->process_items,
// helper->_private_helper, process_items->_private_helper (via a
// `for my $item (@items)` loop, the exact false-positive trap CallRe's
// no-space requirement exists for), and render->build_label across the
// nested package block. Negatives baked in: a commented-out sub, an
// anonymous sub assignment, and every `my (...)`/`for my $x (...)` unpacking
// form must never become a node or an edge.
var perlGapSrc = []byte(`package MyApp;

use strict;
use warnings;

BEGIN {
    print "loading MyApp\n";
}

# ghost sub, commented out, must never mint a node
# sub ghost {
#     return -1;
# }

sub new {
    my $class = shift;
    my $self  = { count => 0 };
    return bless $self, $class;
}

sub run {
    my ($self) = @_;
    $self->helper();
    $self->process_items(1, 2, 3);
    return 1;
}

sub helper {
    my ($self) = @_;
    return $self->_private_helper() + 1;
}

sub process_items {
    my ($self, @items) = @_;
    my $total = 0;
    for my $item (@items) {
        $total += $self->_private_helper($item);
    }
    return $total;
}

sub _private_helper {
    my ($self, $n) = @_;
    $n //= 0;
    return $n * 2;
}

sub compute
{
    my ($self, $x, $y) = @_;
    return $x + $y;
}

sub max ($$) {
    my ($a, $b) = @_;
    return $a > $b ? $a : $b;
}

sub greet ($name, $greeting = "Hello") {
    return "$greeting, $name!";
}

my $callback = sub {
    my ($x) = @_;
    return $x * $x;
};

package MyApp::Utils;

sub trim {
    my ($s) = @_;
    $s =~ s/^\s+|\s+$//g;
    return $s;
}

package MyApp::Widget {
    sub build {
        my ($class, %opts) = @_;
        my $self = { %opts };
        return bless $self, $class;
    }

    sub render {
        my ($self) = @_;
        return $self->build_label();
    }

    sub build_label {
        my ($self) = @_;
        return "widget";
    }
}

1;
`)

// TestPerlSpecCallEdgesGapFixture is R-0005's headline perl fix: perlSpec
// previously left CallRe nil, so reindexing this exact fixture produced 0
// edges (see scratch/findings/perl.md's EDGES section). With CallRe/Stop set
// (mirroring c.go), the 5 documented same-file call sites now become ECall
// edges — and the deliberately adversarial `my ($self) = @_;` /
// `for my $item (@items) {` unpacking forms (which would false-positive
// "my"/"item" as callees under a `\s*\(`-style CallRe, see perl.go's CallRe
// doc) produce none.
func TestPerlSpecCallEdgesGapFixture(t *testing.T) {
	p := SpecParser{S: perlSpec}
	pr, err := p.Parse("pkg/App.pm", perlGapSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	type want struct {
		src, dst graph.NodeID
		line     int
	}
	wantEdges := []want{
		{"pl:App.run", "pl:App.helper", 23},
		{"pl:App.run", "pl:App.process_items", 24},
		{"pl:App.helper", "pl:App._private_helper", 30},
		{"pl:App.process_items", "pl:App._private_helper", 37},
		{"pl:App.render", "pl:App.build_label", 85},
	}
	if len(pr.Edges) != len(wantEdges) {
		t.Fatalf("got %d edges, want %d: %+v", len(pr.Edges), len(wantEdges), pr.Edges)
	}
	got := map[[2]graph.NodeID]int{}
	for _, e := range pr.Edges {
		if e.Kind != graph.ECall {
			t.Errorf("edge %+v Kind = %v, want ECall", e, e.Kind)
		}
		got[[2]graph.NodeID{e.Src, e.Dst}] = e.Line
	}
	for _, w := range wantEdges {
		line, ok := got[[2]graph.NodeID{w.src, w.dst}]
		if !ok {
			t.Errorf("missing edge %s -ECall-> %s, got %+v", w.src, w.dst, pr.Edges)
			continue
		}
		if line != w.line {
			t.Errorf("edge %s -> %s at line %d, want %d", w.src, w.dst, line, w.line)
		}
	}

	// Negatives: perl's `my (...)`/`for my $x (...)` unpacking idiom must
	// never produce an edge to "my", "item", "self", "class", or "x" — the
	// exact false-positive trap documented in perl.go's CallRe comment.
	banned := []graph.NodeID{"pl:App.my", "pl:App.item", "pl:App.self", "pl:App.class", "pl:App.x", "pl:App.for"}
	for _, e := range pr.Edges {
		for _, b := range banned {
			if e.Dst == b {
				t.Errorf("false-positive edge to %s: %+v", b, e)
			}
		}
	}
}

func TestPerlSpecDeterministic(t *testing.T) {
	p := SpecParser{S: perlSpec}
	pr1, err := p.Parse("pkg/app.pl", perlSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	pr2, err := p.Parse("pkg/app.pl", perlSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !reflect.DeepEqual(pr1, pr2) {
		t.Errorf("Parse is not deterministic across identical runs:\n%+v\nvs\n%+v", pr1, pr2)
	}
}
