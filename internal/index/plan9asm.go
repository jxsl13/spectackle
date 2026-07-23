package index

import (
	"regexp"
	"strings"
)

// Plan 9 assembly (Go's asm dialect) has no viable tree-sitter grammar:
// existing asm grammars target GAS/NASM, while Plan 9 uses TEXT ·name(SB),
// pseudo-registers (SB/FP/SP/PC) and the middle dot. We only need symbol
// definitions, so a line scanner is more robust and dependency-free.

// AsmSym is one TEXT or GLOBL definition in a .s file.
type AsmSym struct {
	Name   string // symbol name without the leading middle dot
	Kind   string // "text" or "globl"
	Line   int    // 1-based
	Frame  string // raw frame/arg size suffix, e.g. "$0-56" (TEXT only)
}

var (
	// TEXT ·mulVec(SB), NOSPLIT, $0-56   /   TEXT runtime·memmove(SB), ...
	reText  = regexp.MustCompile(`^TEXT\s+(?:[\w]+)?·([\w·]+)\(SB\)\s*(?:,\s*[\w|]+)?\s*(?:,\s*(\$[\d-]+))?`)
	reGlobl = regexp.MustCompile(`^GLOBL\s+(?:[\w]+)?·([\w·]+)\(SB\)`)
)

// ScanPlan9Asm extracts symbol definitions from Plan 9 asm source.
func ScanPlan9Asm(src []byte) []AsmSym {
	var syms []AsmSym
	for i, line := range strings.Split(string(src), "\n") {
		l := strings.TrimSpace(line)
		if m := reText.FindStringSubmatch(l); m != nil {
			syms = append(syms, AsmSym{Name: m[1], Kind: "text", Line: i + 1, Frame: m[2]})
		} else if m := reGlobl.FindStringSubmatch(l); m != nil {
			syms = append(syms, AsmSym{Name: m[1], Kind: "globl", Line: i + 1})
		}
	}
	return syms
}
