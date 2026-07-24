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

func TestPhpSpecNodes(t *testing.T) {
	p := SpecParser{S: phpSpec}
	pr, err := p.Parse("pkg/app.php", phpSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)

	want := map[graph.NodeID]struct {
		Kind graph.NodeKind
		Line int
	}{
		"php:app.top_level":  {graph.KFunc, 3},
		"php:app.Foo":        {graph.KType, 7},
		"php:app.run":        {graph.KFunc, 8},
		"php:app.helper":     {graph.KFunc, 12},
		"php:app.contract":   {graph.KFunc, 16},
		"php:app.Shape":      {graph.KType, 19},
		"php:app.area":       {graph.KFunc, 20},
		"php:app.Circle":     {graph.KType, 23},
		"php:app.getArea":    {graph.KFunc, 24},
		"php:app.Renderable": {graph.KType, 29},
		"php:app.render":     {graph.KFunc, 30},
		"php:app.Loggable":   {graph.KType, 33},
		"php:app.log":        {graph.KFunc, 34},
		"php:app.Suit":       {graph.KType, 39},
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
		if n.Line != w.Line || n.EndLine != w.Line {
			t.Errorf("%s Line/EndLine = %d/%d, want %d", id, n.Line, n.EndLine, w.Line)
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
