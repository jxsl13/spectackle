package langspec

import (
	"reflect"
	"testing"

	"github.com/jxsl13/spectacle/internal/graph"
)

// dartSrc exercises every dartSpec Def: a top-level `void main() {}`, a
// top-level function with a generic return type (`Future<int> load()
// async {}`), a class (KType) containing a >=2-space-indent instance
// method and a static method, and an `extension` (KType) containing a
// >=2-space-indent method.
var dartSrc = []byte(`void main() {
  runApp();
}

Future<int> load() async {
  return 1;
}

abstract class Animal {
  String name = 'dog';

  void speak() {
    print(name);
  }

  static void reset() {
    name = '';
  }
}

extension StringCasing on String {
  String shout() {
    return toUpperCase();
  }
}
`)

func TestDartSpecLangExtensions(t *testing.T) {
	p := SpecParser{S: dartSpec}
	if p.Lang() != graph.LangDart {
		t.Errorf("Lang() = %v, want %v", p.Lang(), graph.LangDart)
	}
	if got, want := p.Extensions(), []string{".dart"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Extensions() = %v, want %v", got, want)
	}
}

func TestDartSpecNodes(t *testing.T) {
	p := SpecParser{S: dartSpec}
	pr, err := p.Parse("pkg/app.dart", dartSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)

	want := map[graph.NodeID]struct {
		Kind graph.NodeKind
		Line int
	}{
		"dart:app.main":         {graph.KFunc, 1},
		"dart:app.load":         {graph.KFunc, 5},
		"dart:app.Animal":       {graph.KType, 9},
		"dart:app.speak":        {graph.KFunc, 12},
		"dart:app.reset":        {graph.KFunc, 16},
		"dart:app.StringCasing": {graph.KType, 21},
		"dart:app.shout":        {graph.KFunc, 22},
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
		if n.Lang != graph.LangDart {
			t.Errorf("%s Lang = %v, want dart", id, n.Lang)
		}
		if n.File != "pkg/app.dart" {
			t.Errorf("%s File = %q, want pkg/app.dart", id, n.File)
		}
	}
}

// TestDartSpecNegativeLines pins down that an if-statement, a constructor
// call assigned to a local (`final a = Foo();`), and an import directive
// never mint nodes: none of them start with a recognized return-type token
// (control flow / `final` / `import` are all lowercase keywords absent from
// the KFunc return-type alternation) or a `class`/`mixin`/`enum`/
// `extension` keyword (KType).
func TestDartSpecNegativeLines(t *testing.T) {
	p := SpecParser{S: dartSpec}
	pr, err := p.Parse("neg.dart", []byte(`import 'package:flutter/material.dart';

if (x) {
  print(x);
}
final a = Foo();
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(pr.Nodes) != 0 {
		t.Fatalf("got %d nodes, want 0: %+v", len(pr.Nodes), pr.Nodes)
	}
}

func TestDartSpecRegisteredInAll(t *testing.T) {
	found := false
	for _, p := range All() {
		if p.Lang() == graph.LangDart {
			found = true
		}
	}
	if !found {
		t.Error("All() does not contain a parser for graph.LangDart — dartSpec not registered via init()")
	}
}

func TestDartSpecDeterministic(t *testing.T) {
	p := SpecParser{S: dartSpec}
	pr1, err := p.Parse("pkg/app.dart", dartSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	pr2, err := p.Parse("pkg/app.dart", dartSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !reflect.DeepEqual(pr1, pr2) {
		t.Errorf("Parse is not deterministic across identical runs:\n%+v\nvs\n%+v", pr1, pr2)
	}
}
