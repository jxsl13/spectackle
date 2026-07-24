package index

import (
	"context"
	"reflect"
	"testing"

	"github.com/jxsl13/spectacle/internal/graph"
	"github.com/jxsl13/spectacle/internal/resolve"
	"github.com/jxsl13/spectacle/internal/store"
)

const asmSrc = `#include "textflag.h"

// func mulVec(dst, a, b []float32)
TEXT ·mulVec(SB), NOSPLIT, $0-72
	MOVQ dst_base+0(FP), DI
	RET

GLOBL ·mask(SB), RODATA, $16
`

func TestAsmParserParse(t *testing.T) {
	pr, err := (AsmParser{}).Parse("mat/mat_amd64.s", []byte(asmSrc))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(pr.Nodes) != 2 {
		t.Fatalf("got %d nodes, want 2: %+v", len(pr.Nodes), pr.Nodes)
	}
	proc := pr.Nodes[0]
	if proc.ID != "asm:mat.mulVec" || proc.Kind != graph.KAsmProc || proc.Lang != graph.LangAsm ||
		proc.File != "mat/mat_amd64.s" || proc.Line != 4 || proc.EndLine != 4 || proc.Sig != "$0-72" {
		t.Errorf("TEXT node wrong: %+v", proc)
	}
	v := pr.Nodes[1]
	if v.ID != "asm:mat.mask" || v.Kind != graph.KVar || v.Lang != graph.LangAsm ||
		v.File != "mat/mat_amd64.s" || v.Line != 8 {
		t.Errorf("GLOBL node wrong: %+v", v)
	}
	if len(pr.Edges) != 0 {
		t.Errorf("AsmParser must not emit edges, got %+v", pr.Edges)
	}
}

func TestAsmParserDeterministic(t *testing.T) {
	pr1, err := (AsmParser{}).Parse("mat/mat_amd64.s", []byte(asmSrc))
	if err != nil {
		t.Fatalf("Parse #1: %v", err)
	}
	pr2, err := (AsmParser{}).Parse("mat/mat_amd64.s", []byte(asmSrc))
	if err != nil {
		t.Fatalf("Parse #2: %v", err)
	}
	if !reflect.DeepEqual(pr1.Nodes, pr2.Nodes) {
		t.Errorf("Parse is not deterministic: %+v != %+v", pr1.Nodes, pr2.Nodes)
	}
	if pr1.Hash != pr2.Hash {
		t.Errorf("Hash not stable across identical parses")
	}
}

func TestAsmParserRootDir(t *testing.T) {
	// a .s file directly at the tree root (dir "." ) qualifies by name alone.
	pr, err := (AsmParser{}).Parse("mulvec_amd64.s", []byte("TEXT ·mulVec(SB), NOSPLIT, $0-72\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(pr.Nodes) != 1 || pr.Nodes[0].ID != "asm:mulVec" {
		t.Fatalf("root-dir qualification wrong: %+v", pr.Nodes)
	}
}

// TestAsmParserEndToEnd runs the full pipeline (GoParser + AsmParser +
// resolve.Default()) over a small on-disk tree and checks the resulting EAsm
// edge, exercising the same wiring the CLI uses.
func TestAsmParserEndToEnd(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "mat/mat.go", `package mat

// func mulVec(dst, a, b []float32)
func mulVec(dst, a, b []float32)
`)
	writeFile(t, root, "mat/mat_amd64.s", asmSrc)

	g := graph.NewMem()
	ix := New(g, store.NewMem(), []LanguageParser{GoParser{}, AsmParser{}}, resolve.Default().All())
	if _, err := ix.IndexAll(context.Background(), root); err != nil {
		t.Fatalf("IndexAll: %v", err)
	}

	edges := g.Neighbors("go:mat.mulVec", graph.Out, []graph.EdgeKind{graph.EAsm})
	if len(edges) != 1 {
		t.Fatalf("got %d EAsm edges from go:mat.mulVec, want 1: %+v", len(edges), edges)
	}
	if edges[0].Dst != "asm:mat.mulVec" {
		t.Errorf("EAsm edge dst = %q, want asm:mat.mulVec", edges[0].Dst)
	}
	if _, ok := g.Node("go:mat.mulVec"); !ok {
		t.Fatal("go:mat.mulVec node missing")
	}
	if _, ok := g.Node("asm:mat.mulVec"); !ok {
		t.Fatal("asm:mat.mulVec node missing")
	}
}
