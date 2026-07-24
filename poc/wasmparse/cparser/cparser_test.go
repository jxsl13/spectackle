package cparser

import (
	"testing"

	"github.com/jxsl13/spectackle/poc/wasmparse/triple"
)

const sample = `#include <stddef.h>

typedef struct Point {
    int x;
    int y;
} Point;

struct Plain {
    int a;
};

#define MAX(a, b) ((a) > (b) ? (a) : (b))

int launch_saxpy(int n, float a, const float *x, float *y);

int launch_saxpy(int n, float a, const float *x, float *y) {
    int i;
    for (i = 0; i < n; i++) {
        y[i] = a * x[i] + y[i];
    }
    return 0;
}
`

func TestParseBasic(t *testing.T) {
	p, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	triples, err := p.Parse("sample.c", []byte(sample))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	want := []triple.Triple{
		{Name: "Point", Kind: triple.KindType, Line: 3},
		{Name: "Plain", Kind: triple.KindType, Line: 8},
		{Name: "MAX", Kind: triple.KindFunc, Line: 12},
		{Name: "launch_saxpy", Kind: triple.KindFunc, Line: 14},
		{Name: "launch_saxpy", Kind: triple.KindFunc, Line: 16},
	}
	if len(triples) != len(want) {
		t.Fatalf("got %d triples %v, want %d %v", len(triples), triples, len(want), want)
	}
	for i, w := range want {
		if triples[i] != w {
			t.Errorf("triple[%d] = %v, want %v", i, triples[i], w)
		}
	}
}
