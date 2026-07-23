// Package budget implements token estimation, record-boundary truncation and
// resume cursors. Every map/graph/contract tool takes a token budget and, when
// results exceed it, truncates at record (line) boundaries and appends a
// cursor so the LLM can resume cheaply (SPX-ARC-002).
package budget

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
)

// EstimateTokens approximates the token count of s (~4 bytes/token heuristic,
// rounded up). Deliberately provider-agnostic; precision is not required, the
// budget is a soft cap on result size.
func EstimateTokens(s string) int { return (len(s) + 3) / 4 }

// TruncateRecords keeps whole lines from lines[offset:] until the token budget
// is exhausted. If lines remain, it returns an opaque cursor encoding the next
// offset; pass it back via Resume to continue. budget <= 0 means unlimited.
func TruncateRecords(lines []string, offset, budget int) (kept []string, cursor string) {
	if offset < 0 || offset > len(lines) {
		offset = 0
	}
	if budget <= 0 {
		return lines[offset:], ""
	}
	used := 0
	i := offset
	for ; i < len(lines); i++ {
		t := EstimateTokens(lines[i]) + 1 // +1 for the newline
		if used+t > budget && i > offset {
			break
		}
		used += t
	}
	kept = lines[offset:i]
	if i < len(lines) {
		cursor = encodeCursor(i)
	}
	return kept, cursor
}

// Resume decodes a cursor previously returned by TruncateRecords.
// An empty or malformed cursor yields offset 0.
func Resume(cursor string) int {
	if cursor == "" {
		return 0
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return 0
	}
	var off int
	if _, err := fmt.Sscanf(string(raw), "off=%d", &off); err != nil || off < 0 {
		return 0
	}
	return off
}

func encodeCursor(offset int) string {
	return base64.RawURLEncoding.EncodeToString([]byte("off=" + strconv.Itoa(offset)))
}

// Render joins records and, when cursor is non-empty, appends the trailing
// "cur <token>" record defined by the shared output line grammar.
func Render(lines []string, cursor string) string {
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(l)
		b.WriteByte('\n')
	}
	if cursor != "" {
		b.WriteString("cur ")
		b.WriteString(cursor)
		b.WriteByte('\n')
	}
	return b.String()
}
