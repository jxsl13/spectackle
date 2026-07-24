// Package plan9 scans Plan 9 assembly (Go's asm dialect) source for TEXT and
// GLOBL symbol definitions.
//
// Plan 9 asm has no viable tree-sitter grammar: existing asm grammars target
// GAS/NASM, while Plan 9 uses TEXT ·name(SB), pseudo-registers
// (SB/FP/SP/PC) and the middle dot. We only need symbol definitions, so a
// line scanner is more robust and dependency-free.
//
// This package is intentionally dependency-free (no graph/index/resolve
// imports): both internal/index (AsmParser, same-language nodes) and
// internal/resolve (Plan9AsmResolver, the Go<->asm EAsm edge) need this
// scanner, and internal/index already imports internal/resolve — a shared
// leaf package is the only way to give both sides the scanner without an
// import cycle.
package plan9

import (
	"regexp"
	"strings"
)

// AsmSym is one TEXT or GLOBL definition in a .s file.
type AsmSym struct {
	Name  string // symbol name without the leading middle dot
	Kind  string // "text" or "globl"
	Line  int    // 1-based
	Frame string // raw frame/arg size suffix, e.g. "$0-56" (TEXT only)
}

var (
	// TEXT ·mulVec(SB), NOSPLIT, $0-56   /   TEXT runtime·memmove(SB), ...
	reText  = regexp.MustCompile(`^TEXT\s+(?:[\w]+)?·([\w·]+)\(SB\)\s*(?:,\s*[\w|]+)?\s*(?:,\s*(\$[\d-]+))?`)
	reGlobl = regexp.MustCompile(`^GLOBL\s+(?:[\w]+)?·([\w·]+)\(SB\)`)
)

// Scan extracts symbol definitions from Plan 9 asm source.
func Scan(src []byte) []AsmSym {
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
