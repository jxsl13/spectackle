package langspec

import (
	"reflect"
	"testing"

	"github.com/jxsl13/spectackle/internal/graph"
)

// --- typescript fixture -------------------------------------------------

var typescriptSrc = []byte(`export function run(x: number): number {
  return x;
}

class Foo extends Base {
  greet(): void {}
}

export const add = (a: number, b: number): number => a + b;

async function fetchData(): Promise<null> {
  return null;
}

const mul = async (a: number, b: number) => a * b;

let square = (x: number) => x * x;

export interface Shape {
  area(): number;
}

interface Point {
  x: number;
  y: number;
}

export type ID = string | number;

type Callback = (x: number) => void;

export enum Color {
  Red,
  Green,
  Blue,
}

const enum Direction {
  Up,
  Down,
}

export const App = () => {
  return null;
};
`)

func TestTypescriptSpecLangExtensions(t *testing.T) {
	p := SpecParser{S: typescriptSpec}
	if p.Lang() != graph.Lang("ts") {
		t.Errorf("Lang() = %v, want ts", p.Lang())
	}
	if got, want := p.Extensions(), []string{".ts", ".tsx"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Extensions() = %v, want %v", got, want)
	}
}

func TestTypescriptSpecNodes(t *testing.T) {
	p := SpecParser{S: typescriptSpec}
	pr, err := p.Parse("pkg/app.ts", typescriptSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)

	// Two nodes here are what T-0120 added, and both were [high] R-0005
	// findings: ts:app.greet is a class method (no method Def existed at
	// all before), ts:app.area is the same Def matching a body-less
	// interface member, which the Def documents as deliberate. EndLine now
	// differs from Line wherever a brace body spans lines, because
	// typescriptSpec sets CallRe.
	want := map[graph.NodeID]struct {
		Kind          graph.NodeKind
		Line, EndLine int
	}{
		"ts:app.run":       {graph.KFunc, 1, 3},
		"ts:app.Foo":       {graph.KType, 5, 5},
		"ts:app.greet":     {graph.KFunc, 6, 6},
		"ts:app.add":       {graph.KFunc, 9, 9},
		"ts:app.fetchData": {graph.KFunc, 11, 13},
		"ts:app.mul":       {graph.KFunc, 15, 15},
		"ts:app.square":    {graph.KFunc, 17, 17},
		"ts:app.Shape":     {graph.KType, 19, 19},
		"ts:app.area":      {graph.KFunc, 20, 20},
		"ts:app.Point":     {graph.KType, 23, 23},
		"ts:app.ID":        {graph.KType, 28, 28},
		"ts:app.Callback":  {graph.KType, 30, 30},
		"ts:app.Color":     {graph.KType, 32, 32},
		"ts:app.Direction": {graph.KType, 38, 38},
		"ts:app.App":       {graph.KFunc, 43, 45},
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
		if n.Lang != graph.Lang("ts") {
			t.Errorf("%s Lang = %v, want ts", id, n.Lang)
		}
	}
}

// tsxSrc proves .tsx files (JSX-flavored TypeScript) parse identically: a
// component defined as an arrow function assigned to an exported const is
// the dominant real-world shape and must mint a KFunc node, same as any
// other arrow-function Def match.
var tsxSrc = []byte(`import React from "react";

export interface ButtonProps {
  label: string;
}

export const Button = (props: ButtonProps) => {
  return <button>{props.label}</button>;
};

function Header() {
  return <h1>Title</h1>;
}
`)

func TestTypescriptSpecTSXComponent(t *testing.T) {
	p := SpecParser{S: typescriptSpec}
	pr, err := p.Parse("pkg/Button.tsx", tsxSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)

	if n, ok := byID["ts:Button.ButtonProps"]; !ok || n.Kind != graph.KType {
		t.Errorf("ts:Button.ButtonProps missing or wrong kind, got %+v ok=%v", n, ok)
	}
	if n, ok := byID["ts:Button.Button"]; !ok || n.Kind != graph.KFunc {
		t.Errorf("ts:Button.Button missing or wrong kind, got %+v ok=%v", n, ok)
	}
	if n, ok := byID["ts:Button.Header"]; !ok || n.Kind != graph.KFunc {
		t.Errorf("ts:Button.Header missing or wrong kind, got %+v ok=%v", n, ok)
	}
}

func TestTypescriptSpecDeterministic(t *testing.T) {
	p := SpecParser{S: typescriptSpec}
	pr1, err := p.Parse("pkg/app.ts", typescriptSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	pr2, err := p.Parse("pkg/app.ts", typescriptSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !reflect.DeepEqual(pr1, pr2) {
		t.Errorf("Parse is not deterministic across identical runs:\n%+v\nvs\n%+v", pr1, pr2)
	}
}

func TestTypescriptSpecInRegistry(t *testing.T) {
	found := false
	for _, p := range All() {
		if p.Lang() == graph.Lang("ts") {
			found = true
		}
	}
	if !found {
		t.Error("All() does not contain a ts parser; typescriptSpec not registered")
	}
}
