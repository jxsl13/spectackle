package tswasm

import (
	"context"
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

func TestParseFileBasic(t *testing.T) {
	ctx := context.Background()
	ex, err := New(ctx)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	triples, err := ex.ParseFile(ctx, []byte(sample))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	want := map[triple.Key]int{
		{Name: "Point", Kind: triple.KindType}:        3,
		{Name: "Plain", Kind: triple.KindType}:        8,
		{Name: "MAX", Kind: triple.KindFunc}:          12,
		{Name: "launch_saxpy", Kind: triple.KindFunc}: 14, // proto + def both on distinct lines
	}

	got := map[triple.Key][]int{}
	for _, tr := range triples {
		got[tr.Key()] = append(got[tr.Key()], tr.Line)
	}
	t.Logf("triples: %v", triples)

	for k := range want {
		if _, ok := got[k]; !ok {
			t.Errorf("missing expected triple key %v; got=%v", k, got)
		}
	}
	// launch_saxpy must appear twice: once for the prototype (line 14),
	// once for the definition (line 16).
	if lines := got[triple.Key{Name: "launch_saxpy", Kind: triple.KindFunc}]; len(lines) != 2 {
		t.Errorf("launch_saxpy: want 2 occurrences (proto+def), got %v", lines)
	}
}
