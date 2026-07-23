package index

import "testing"

func TestScanPlan9Asm(t *testing.T) {
	src := []byte(`#include "textflag.h"

// func mulVec(dst, a, b []float32)
TEXT ·mulVec(SB), NOSPLIT, $0-72
	MOVQ dst_base+0(FP), DI
	RET

GLOBL ·shuffleMask(SB), RODATA, $16
`)
	syms := ScanPlan9Asm(src)
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
