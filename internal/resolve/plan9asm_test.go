package resolve

import (
	"context"
	"sort"
	"testing"

	"github.com/jxsl13/spectackle/internal/graph"
)

// asmFS is a tiny in-memory FileSet exposing only .s files under LangAsm.
type asmFS struct {
	files map[string]string
}

func (m asmFS) ByLang(l graph.Lang) []string {
	if l != graph.LangAsm {
		return nil
	}
	var paths []string
	for p := range m.files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return paths
}

func (m asmFS) Read(path string) ([]byte, error) {
	return []byte(m.files[path]), nil
}

const testAsmSrc = `#include "textflag.h"

// func mulVec(dst, a, b []float32)
TEXT ·mulVec(SB), NOSPLIT, $0-72
	MOVQ dst_base+0(FP), DI
	RET
`

func TestPlan9AsmResolverPairsGoAndAsmNodes(t *testing.T) {
	g := graph.NewMem()
	g.Upsert([]graph.Node{
		{ID: "go:mat.mulVec", Kind: graph.KFunc, Lang: graph.LangGo, File: "mat/mat.go", Line: 3},
		{ID: "asm:mat.mulVec", Kind: graph.KAsmProc, Lang: graph.LangAsm, File: "mat/mat_amd64.s", Line: 4},
	}, nil)
	fs := asmFS{files: map[string]string{"mat/mat_amd64.s": testAsmSrc}}

	edges, err := (Plan9AsmResolver{}).Resolve(context.Background(), g, fs)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(edges) != 1 {
		t.Fatalf("got %d edges, want 1: %+v", len(edges), edges)
	}
	e := edges[0]
	if e.Src != "go:mat.mulVec" || e.Dst != "asm:mat.mulVec" {
		t.Errorf("edge endpoints = %q -> %q, want go:mat.mulVec -> asm:mat.mulVec", e.Src, e.Dst)
	}
	if e.Kind != graph.EAsm {
		t.Errorf("edge kind = %v, want EAsm", e.Kind)
	}
	if e.File != "mat/mat_amd64.s" || e.Line != 4 {
		t.Errorf("edge site = %s:%d, want mat/mat_amd64.s:4", e.File, e.Line)
	}
}

func TestPlan9AsmResolverNoGoNodeNoEdge(t *testing.T) {
	// only the asm side is present in the graph (Go decl never indexed, or
	// removed) -> no edge, and no error.
	g := graph.NewMem()
	g.Upsert([]graph.Node{
		{ID: "asm:mat.mulVec", Kind: graph.KAsmProc, Lang: graph.LangAsm, File: "mat/mat_amd64.s", Line: 4},
	}, nil)
	fs := asmFS{files: map[string]string{"mat/mat_amd64.s": testAsmSrc}}

	edges, err := (Plan9AsmResolver{}).Resolve(context.Background(), g, fs)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(edges) != 0 {
		t.Fatalf("want no edges without a matching go node, got %+v", edges)
	}
}

func TestPlan9AsmResolverNilFileSet(t *testing.T) {
	edges, err := (Plan9AsmResolver{}).Resolve(context.Background(), graph.NewMem(), nil)
	if err != nil || edges != nil {
		t.Fatalf("Resolve with nil fs = %v, %v; want nil, nil", edges, err)
	}
}

func TestPlan9AsmResolverIgnoresGlobl(t *testing.T) {
	g := graph.NewMem()
	g.Upsert([]graph.Node{
		{ID: "go:mat.mask", Kind: graph.KVar, Lang: graph.LangGo, File: "mat/mat.go", Line: 3},
		{ID: "asm:mat.mask", Kind: graph.KVar, Lang: graph.LangAsm, File: "mat/mat_amd64.s", Line: 1},
	}, nil)
	fs := asmFS{files: map[string]string{"mat/mat_amd64.s": "GLOBL ·mask(SB), RODATA, $16\n"}}

	edges, err := (Plan9AsmResolver{}).Resolve(context.Background(), g, fs)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(edges) != 0 {
		t.Fatalf("GLOBL symbols must not be resolved in M1.5, got %+v", edges)
	}
}
