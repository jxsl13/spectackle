package plan9

import "testing"

func TestScan(t *testing.T) {
	src := []byte(`#include "textflag.h"

// func mulVec(dst, a, b []float32)
TEXT ·mulVec(SB), NOSPLIT, $0-72
	MOVQ dst_base+0(FP), DI
	RET

GLOBL ·shuffleMask(SB), RODATA, $16
`)
	syms := Scan(src)
	if len(syms) != 2 {
		t.Fatalf("got %d syms, want 2: %+v", len(syms), syms)
	}
	if syms[0].Name != "mulVec" || syms[0].Kind != "text" || syms[0].Line != 4 || syms[0].Frame != "$0-72" {
		t.Errorf("TEXT parsed wrong: %+v", syms[0])
	}
	if syms[1].Name != "shuffleMask" || syms[1].Kind != "globl" || syms[1].Line != 8 {
		t.Errorf("GLOBL parsed wrong: %+v", syms[1])
	}
}

func TestScanTextStaticSuffix(t *testing.T) {
	src := []byte("TEXT shuffle<>(SB), NOSPLIT, $0-16\n\tRET\n")
	syms := Scan(src)
	if len(syms) != 1 {
		t.Fatalf("got %d syms, want 1: %+v", len(syms), syms)
	}
	if syms[0].Name != "shuffle" || syms[0].Kind != "text" || syms[0].Frame != "$0-16" {
		t.Errorf("TEXT <> static suffix parsed wrong: %+v", syms[0])
	}
}

func TestScanTextABIInternal(t *testing.T) {
	src := []byte("TEXT ·addAVX2<ABIInternal>(SB), NOSPLIT, $0-24\n\tRET\n")
	syms := Scan(src)
	if len(syms) != 1 {
		t.Fatalf("got %d syms, want 1: %+v", len(syms), syms)
	}
	if syms[0].Name != "addAVX2" || syms[0].Kind != "text" || syms[0].Frame != "$0-24" {
		t.Errorf("TEXT <ABIInternal> tag parsed wrong: %+v", syms[0])
	}
}

func TestScanTextQuotedMethod(t *testing.T) {
	src := []byte(`TEXT "".Vector.Add(SB), NOSPLIT, $0-40
	RET
`)
	syms := Scan(src)
	if len(syms) != 1 {
		t.Fatalf("got %d syms, want 1: %+v", len(syms), syms)
	}
	if syms[0].Name != "Vector.Add" || syms[0].Kind != "text" || syms[0].Frame != "$0-40" {
		t.Errorf("TEXT quoted method-shaped symbol parsed wrong: %+v", syms[0])
	}
}

func TestScanGloblStaticSuffix(t *testing.T) {
	src := []byte("GLOBL mask<>(SB), RODATA, $32\n")
	syms := Scan(src)
	if len(syms) != 1 {
		t.Fatalf("got %d syms, want 1: %+v", len(syms), syms)
	}
	if syms[0].Name != "mask" || syms[0].Kind != "globl" {
		t.Errorf("GLOBL <> static suffix parsed wrong: %+v", syms[0])
	}
}
