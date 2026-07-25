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
	//
	// The name alternation covers three symbol-name shapes, tried in this
	// order (capture groups 1/2/3 respectively; group 4 is the frame size,
	// TEXT only):
	//
	//   1. Quoted, method/receiver-shaped linker symbols, as emitted for
	//      generated or linkname'd methods: TEXT "".Vector.Add(SB)
	//   2. The common middle-dot form, optionally package-qualified
	//      (runtime·memmove) and optionally register-ABI-tagged
	//      (·addAVX2<ABIInternal>, Go 1.17+). The <...> tag is matched but
	//      not captured, so it is stripped from the minted name.
	//   3. File-local/static symbols using the `<>` linker suffix with no
	//      middle dot at all: TEXT shuffle<>(SB)
	reText = regexp.MustCompile(`^TEXT\s+(?:""\.([\w.]+)|(?:[\w]+)?·([\w·]+)(?:<[\w]*>)?|(\w+)<>)\(SB\)\s*(?:,\s*[\w|]+)?\s*(?:,\s*(\$[\d-]+))?`)
	// GLOBL ·shuffleMask(SB), RODATA, $16   /   GLOBL mask<>(SB), RODATA, $32
	//
	// Same two name shapes as reText's groups 2/3 (no quoted-method or
	// ABI-tag forms apply to data symbols).
	reGlobl = regexp.MustCompile(`^GLOBL\s+(?:(?:[\w]+)?·([\w·]+)|(\w+)<>)\(SB\)`)
)

// Scan extracts symbol definitions from Plan 9 asm source.
func Scan(src []byte) []AsmSym {
	var syms []AsmSym
	for i, line := range strings.Split(string(src), "\n") {
		l := strings.TrimSpace(line)
		if m := reText.FindStringSubmatch(l); m != nil {
			name := firstNonEmpty(m[1], m[2], m[3])
			syms = append(syms, AsmSym{Name: name, Kind: "text", Line: i + 1, Frame: m[4]})
		} else if m := reGlobl.FindStringSubmatch(l); m != nil {
			name := firstNonEmpty(m[1], m[2])
			syms = append(syms, AsmSym{Name: name, Kind: "globl", Line: i + 1})
		}
	}
	return syms
}

// firstNonEmpty returns the first non-empty string among an alternation's
// mutually-exclusive capture groups (at most one is ever populated).
func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}
