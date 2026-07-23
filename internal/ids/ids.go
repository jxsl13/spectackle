// Package ids defines the minting and validation rules for stable node IDs.
//
// A NodeID is the cheapest currency in the whole system: the LLM references
// graph nodes by ID instead of by file contents, so IDs must be short,
// deterministic and byte-identical across re-indexing runs of unchanged files.
//
// Format: "<lang>:<qualified-name>", e.g.
//
//	go:saxpy.Saxpy      Go function Saxpy in package saxpy
//	c:launch_saxpy      C symbol launch_saxpy
//	cu:saxpy_kernel     CUDA __global__ kernel
//	asm:mat.mulVec      Plan 9 asm TEXT ·mulVec in package mat
//
// When two distinct definitions mint the same ID (e.g. static C functions of
// the same name in different translation units), the indexer disambiguates by
// appending "~2", "~3", ... in deterministic file-path order.
package ids

import (
	"fmt"
	"regexp"
	"strings"
)

// idRe validates "<lang>:<qual>" with an optional "~N" collision suffix.
var idRe = regexp.MustCompile(`^[a-z]{1,5}:[^\s~]+(~[2-9][0-9]*)?$`)

// Mint builds a NodeID string from a language tag and a qualified name.
// The qualified name must not contain whitespace; spaces are replaced by "_".
func Mint(lang, qual string) string {
	qual = strings.ReplaceAll(strings.TrimSpace(qual), " ", "_")
	return lang + ":" + qual
}

// WithCollision returns id with a "~n" disambiguation suffix (n >= 2).
func WithCollision(id string, n int) string {
	return fmt.Sprintf("%s~%d", id, n)
}

// Parse splits an ID into language tag and qualified name (collision suffix
// retained in qual). It returns an error for malformed IDs.
func Parse(id string) (lang, qual string, err error) {
	if !Valid(id) {
		return "", "", fmt.Errorf("ids: malformed node ID %q", id)
	}
	lang, qual, _ = strings.Cut(id, ":")
	return lang, qual, nil
}

// Valid reports whether id conforms to the NodeID grammar.
func Valid(id string) bool { return idRe.MatchString(id) }
