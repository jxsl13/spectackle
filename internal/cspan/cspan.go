// Package cspan implements the brace-counting body-span scanner shared by
// internal/langspec (the primary parsing pass) and internal/resolve (the
// FFI bridging resolver). It is a leaf package with zero non-stdlib
// imports, which is exactly why it exists: internal/index imports
// internal/langspec, and internal/resolve sits below internal/index, so
// resolve cannot import langspec without a cycle (langspec -> index ->
// resolve -> langspec). Both layers can import this package instead.
//
// Span (and its helpers SpanFrom, Delta) is T-0053's shipped semantics,
// moved verbatim — not reimplemented — from internal/langspec's former
// braceSpan/braceSpanFrom/braceDelta. Before T-0054, internal/resolve/ffi.go
// carried its own independent, unfixed, K&R-only copy of this same logic
// (ffiBraceSpan); T-0054 deletes that copy and switches ffi.go to this
// package instead, alongside internal/langspec/langspec.go doing the same
// (see docs/validation-ddnet.md's "## ffi re-validation" section for why
// that duplication was the last blocker on ddnet's cpp:->c: FFI crossing).
package cspan

import (
	"regexp"
	"strings"
)

// Span finds the end of the body opened for the def hit on lines[start].
// K&R style (the def line itself opens '{') is handled first and is
// byte-identical to the pre-T-0053 behavior: depth-count forward ('{' +1,
// '}' -1) until depth returns to <= 0, single-line bodies included.
//
// When the def line has no '{' at all, T-0053 extends the search rather
// than bailing immediately:
//   - a def line ending in ';' is a prototype/forward-declaration (or a
//     function-like macro whose expansion has no body) — no body exists, ok
//     is false, and no forward line is ever touched (same early-exit
//     invariant as before this extension).
//   - a def line ending in ',' or '(' is a multi-line parameter list — the
//     header is scanned forward to the line containing the closing ')',
//     and the brace search (same-line or Allman) resumes from there.
//   - otherwise the def is Allman style: the opening '{' lives on one of
//     the next few source lines instead of the def line itself (ddnet's
//     universal C/C++ brace placement). Span looks ahead over subsequent
//     lines, skipping blank lines and pure '//'-comment lines, for at most
//     3 lines; if the first significant line found begins with '{'
//     (ignoring leading whitespace), depth tracking starts there. Anything
//     else — including finding no significant line within the 3-line
//     window — means no body, ok is false, and (as with the K&R no-brace
//     case) lines beyond the window are never touched, so a braceless def
//     can never wander into the next, unrelated def's lines.
func Span(lines []string, start int) (end int, ok bool) {
	if depth, opened := Delta(lines[start]); opened {
		return SpanFrom(lines, start, depth)
	}

	defLine := strings.TrimRight(lines[start], " \t")
	if strings.HasSuffix(defLine, ";") {
		// Prototype, forward declaration, or a function-like macro with no
		// body — bail early exactly as before this extension.
		return 0, false
	}

	header := start
	if strings.HasSuffix(defLine, ",") || strings.HasSuffix(defLine, "(") {
		// Multi-line parameter list: keep scanning the header until the
		// line that closes it, then treat that line as the new header for
		// the same-line-or-Allman brace search below.
		closed := -1
		for i := start + 1; i < len(lines); i++ {
			if strings.Contains(lines[i], ")") {
				closed = i
				break
			}
		}
		if closed == -1 {
			return 0, false
		}
		header = closed
	}

	if header != start {
		headerLine := lines[header]
		if depth, opened := Delta(headerLine); opened {
			// K&R brace placed on the header's closing-paren line.
			return SpanFrom(lines, header, depth)
		}
		if strings.HasSuffix(strings.TrimRight(headerLine, " \t"), ";") {
			// A multi-line prototype — no body.
			return 0, false
		}
	}

	// Allman: the opening '{' lives on a following line. Bounded lookahead
	// (skip blank/`//`-comment lines, at most 3 lines) keeps a braceless
	// def from ever consuming a later, unrelated def's lines.
	sig := -1
	for i := header + 1; i < len(lines) && i <= header+3; i++ {
		t := strings.TrimSpace(lines[i])
		if t == "" || strings.HasPrefix(t, "//") {
			continue
		}
		sig = i
		break
	}
	if sig == -1 || strings.TrimSpace(lines[sig])[0] != '{' {
		return 0, false
	}
	depth, _ := Delta(lines[sig])
	return SpanFrom(lines, sig, depth)
}

// SpanFrom depth-counts forward from lines[start], whose own Delta
// contribution is already folded into depth, until depth returns to <= 0
// (single-line bodies included, matching Span's pre-T-0053 K&R-only
// behavior).
func SpanFrom(lines []string, start, depth int) (end int, ok bool) {
	if depth <= 0 {
		return start, true
	}
	for i := start + 1; i < len(lines); i++ {
		d, _ := Delta(lines[i])
		depth += d
		if depth <= 0 {
			return i, true
		}
	}
	return 0, false
}

// Delta returns the net brace-depth change contributed by line ('{' is +1,
// '}' is -1) and whether the line contains at least one '{'.
func Delta(line string) (delta int, opened bool) {
	for _, r := range line {
		switch r {
		case '{':
			delta++
			opened = true
		case '}':
			delta--
		}
	}
	return delta, opened
}

// KeywordSpan finds the end of the body opened for the def hit on
// lines[start], for end-terminated languages (Lua's function/if/for/do ..
// end, Ruby's def/class/module .. end, and similar) that have no braces for
// Span to count. Depth is counted the same way as Span/Delta, just with
// regexes standing in for '{'/'}': open and close are matched against each
// line with FindAllString, and each match increments/decrements depth
// respectively — nested and repeated keywords on one line (e.g. an
// interleaved "if x then y end" one-liner) are handled by the same net-sum
// logic Delta already uses for braces, no special-casing required.
//
// The def line (lines[start]) is where counting begins: it "counts as one
// open when it matches open" exactly as Span's K&R case treats the def
// line's own '{' — if open doesn't match lines[start] at all, there is
// nothing to close and ok is false without touching any other line. From
// there, depth is tracked forward exactly like SpanFrom: the first line
// where it returns to <= 0 is the end, single-line bodies (open and close
// both on lines[start]) included. If depth never returns to <= 0 before the
// source ends, ok is false.
func KeywordSpan(lines []string, start int, open, close *regexp.Regexp) (end int, ok bool) {
	if start < 0 || start >= len(lines) {
		return 0, false
	}
	depth, opened := keywordDelta(lines[start], open, close)
	if !opened {
		return 0, false
	}
	if depth <= 0 {
		return start, true
	}
	for i := start + 1; i < len(lines); i++ {
		d, _ := keywordDelta(lines[i], open, close)
		depth += d
		if depth <= 0 {
			return i, true
		}
	}
	return 0, false
}

// keywordDelta returns the net depth change contributed by line (each open
// match is +1, each close match is -1) and whether the line contains at
// least one open match.
func keywordDelta(line string, open, close *regexp.Regexp) (delta int, opened bool) {
	opens := len(open.FindAllString(line, -1))
	closes := len(close.FindAllString(line, -1))
	return opens - closes, opens > 0
}
