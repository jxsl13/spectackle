package langspec

import (
	"reflect"
	"testing"

	"github.com/jxsl13/spectackle/internal/graph"
)

// --- php fixture -----------------------------------------------------------

var phpSrc = []byte(`<?php

function top_level($x) {
    return $x * 2;
}

class Foo extends Base {
    public function run($x) {
        return $x;
    }

    private static function helper() {
        return null;
    }

    abstract public function contract();
}

abstract class Shape {
    abstract public function area();
}

final class Circle extends Shape {
    public function getArea() {
        return 3.14;
    }
}

interface Renderable {
    public function render();
}

trait Loggable {
    public function log($msg) {
        echo $msg;
    }
}

enum Suit {
    case Hearts;
    case Spades;
}
`)

func TestPhpSpecLangExtensions(t *testing.T) {
	p := SpecParser{S: phpSpec}
	if p.Lang() != graph.Lang("php") {
		t.Errorf("Lang() = %v, want php", p.Lang())
	}
	if got, want := p.Extensions(), []string{".php"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Extensions() = %v, want %v", got, want)
	}
}

// TestPhpSpecNodes' EndLine expectations were revised by R-0005/T-0123:
// phpSpec now sets CallRe (mirroring c.go), which enables the shared
// brace-counted body-span for every KFunc/KMethod def; class/interface/
// trait/enum (KType) nodes are unaffected — span computation is gated on
// Kind == KFunc/KMethod in langspec.go, a framework-wide rule (see
// c_test.go's struct/enum nodes for the identical limitation). Bodyless
// stubs (`abstract public function contract();`, an interface method ending
// in `;`) correctly keep EndLine == Line: cspan.Span bails on a
// prototype/no-body def line ending in `;`.
func TestPhpSpecNodes(t *testing.T) {
	p := SpecParser{S: phpSpec}
	pr, err := p.Parse("pkg/app.php", phpSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)

	want := map[graph.NodeID]struct {
		Kind    graph.NodeKind
		Line    int
		EndLine int
	}{
		"php:app.top_level":  {graph.KFunc, 3, 5},
		"php:app.Foo":        {graph.KType, 7, 7},
		"php:app.run":        {graph.KFunc, 8, 10},
		"php:app.helper":     {graph.KFunc, 12, 14},
		"php:app.contract":   {graph.KFunc, 16, 16},
		"php:app.Shape":      {graph.KType, 19, 19},
		"php:app.area":       {graph.KFunc, 20, 20},
		"php:app.Circle":     {graph.KType, 23, 23},
		"php:app.getArea":    {graph.KFunc, 24, 26},
		"php:app.Renderable": {graph.KType, 29, 29},
		"php:app.render":     {graph.KFunc, 30, 30},
		"php:app.Loggable":   {graph.KType, 33, 33},
		"php:app.log":        {graph.KFunc, 34, 36},
		"php:app.Suit":       {graph.KType, 39, 39},
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
		if n.Lang != graph.Lang("php") {
			t.Errorf("%s Lang = %v, want php", id, n.Lang)
		}
	}
}

// phpClosureNegativeSrc proves anonymous closures never mint a node: the
// function Def requires an identifier immediately after `function`
// (\w+), so `function () use (&$x) {` and `function ($x) {` have no
// identifier for the Name group to capture and are skipped entirely.
var phpClosureNegativeSrc = []byte(`<?php

function makeCounter() {
    $count = 0;
    return function () use (&$count) {
        $count++;
        return $count;
    };
}

$double = function ($x) {
    return $x * 2;
};

$triple = fn($x) => $x * 3;
`)

func TestPhpSpecClosureNegative(t *testing.T) {
	p := SpecParser{S: phpSpec}
	pr, err := p.Parse("pkg/closures.php", phpClosureNegativeSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)

	if len(pr.Nodes) != 1 {
		t.Fatalf("got %d nodes, want 1 (only makeCounter; closures/arrow fns must not match): %+v", len(pr.Nodes), pr.Nodes)
	}
	if n, ok := byID["php:closures.makeCounter"]; !ok || n.Kind != graph.KFunc {
		t.Errorf("php:closures.makeCounter missing or wrong kind, got %+v ok=%v", n, ok)
	}
}

// phpCallSrc is adapted from R-0005's gap-php/sample.php (see
// scratch/findings/php.md, scratch/gap-php/sample.php): the same
// standaloneHelper->doubleIt top-level chain, and UserService's
// __construct->log, updateProfile->validate->trim, and
// create->initialize (via `new self(...)` + `$svc->initialize()`) chains,
// inlined into one class (dropping the trait indirection) to keep the
// edge assertions below self-contained and exact.
var phpCallSrc = []byte(`<?php

function standaloneHelper(int $x): int
{
    return doubleIt($x);
}

function doubleIt(int $x): int
{
    return $x * 2;
}

final class UserService
{
    public function __construct(
        private readonly string $id,
        string $name
    ) {
        $this->log("constructed {$name}");
    }

    public function updateProfile(string $name): self
    {
        return $this->validate($name);
    }

    private function validate(string $name): string
    {
        return trim($name);
    }

    public static function create(string $id, string $name): self
    {
        $svc = new self($id, $name);
        return $svc->initialize();
    }

    public function initialize(): self
    {
        return $this;
    }

    private function log(string $msg): void
    {
        echo $msg;
    }
}
`)

// TestPhpSpecCallEdgesGapFixture is R-0005's headline php fix: phpSpec
// previously left CallRe nil, so reindexing the gap-php sample reported
// "0 edges" even though standaloneHelper/create/__construct/updateProfile
// each call another named symbol in the same file (see
// scratch/findings/php.md's EDGES section). With CallRe/Stop set, those
// calls now become ECall edges — and `new self(...)` (PHP's construct-call
// idiom) must never itself become an edge to a "self" node (phpCallStop).
func TestPhpSpecCallEdgesGapFixture(t *testing.T) {
	p := SpecParser{S: phpSpec}
	pr, err := p.Parse("pkg/sample.php", phpCallSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	type want struct {
		src, dst graph.NodeID
	}
	wantEdges := map[want]bool{
		{"php:sample.standaloneHelper", "php:sample.doubleIt"}: true,
		{"php:sample.__construct", "php:sample.log"}:           true,
		{"php:sample.updateProfile", "php:sample.validate"}:    true,
		{"php:sample.validate", "php:sample.trim"}:             true, // dangling: trim is a PHP builtin, not a repo symbol — Impact tolerates it (see langspec.go's callEdges doc).
		{"php:sample.create", "php:sample.initialize"}:         true,
	}
	got := map[want]graph.Edge{}
	for _, e := range pr.Edges {
		if e.Kind != graph.ECall {
			t.Errorf("edge %+v Kind = %v, want ECall", e, e.Kind)
		}
		got[want{e.Src, e.Dst}] = e
	}
	for w := range wantEdges {
		if _, ok := got[w]; !ok {
			t.Errorf("missing edge %s -ECall-> %s, got %+v", w.src, w.dst, pr.Edges)
		}
	}
	// Negative: `new self($id, $name)` inside create's body must never
	// become an edge to a "self" node — phpCallStop's job.
	if e, ok := got[want{"php:sample.create", "php:sample.self"}]; ok {
		t.Errorf("false-positive edge to php:sample.self (from `new self(...)`): %+v", e)
	}
}

func TestPhpSpecDeterministic(t *testing.T) {
	p := SpecParser{S: phpSpec}
	pr1, err := p.Parse("pkg/app.php", phpSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	pr2, err := p.Parse("pkg/app.php", phpSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !reflect.DeepEqual(pr1, pr2) {
		t.Errorf("Parse is not deterministic across identical runs:\n%+v\nvs\n%+v", pr1, pr2)
	}
}

func TestPhpSpecInRegistry(t *testing.T) {
	found := false
	for _, p := range All() {
		if p.Lang() == graph.Lang("php") {
			found = true
		}
	}
	if !found {
		t.Error("All() does not contain a php parser; phpSpec not registered")
	}
}
