package budget

import (
	"strings"
	"testing"
)

func TestEstimateTokens(t *testing.T) {
	for in, want := range map[string]int{"": 0, "a": 1, "abcd": 1, "abcde": 2} {
		if got := EstimateTokens(in); got != want {
			t.Errorf("EstimateTokens(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestTruncateResumeRoundtrip(t *testing.T) {
	lines := []string{
		strings.Repeat("a", 40), strings.Repeat("b", 40),
		strings.Repeat("c", 40), strings.Repeat("d", 40),
	}
	kept, cur := TruncateRecords(lines, 0, 25) // ~11 tokens/line incl newline
	if len(kept) == 0 || len(kept) >= len(lines) {
		t.Fatalf("expected partial keep, got %d of %d", len(kept), len(lines))
	}
	if cur == "" {
		t.Fatal("expected a resume cursor")
	}
	// resume must continue exactly where truncation stopped
	rest, cur2 := TruncateRecords(lines, Resume(cur), 0)
	if got := append(append([]string{}, kept...), rest...); len(got) != len(lines) {
		t.Fatalf("resume lost records: %d + %d != %d", len(kept), len(rest), len(lines))
	} else {
		for i := range got {
			if got[i] != lines[i] {
				t.Fatalf("resume order broken at %d", i)
			}
		}
	}
	if cur2 != "" {
		t.Fatalf("unlimited budget must not return a cursor, got %q", cur2)
	}
}

func TestTruncateAlwaysProgresses(t *testing.T) {
	// a single oversized record must still be emitted (no infinite cursor loop)
	lines := []string{strings.Repeat("x", 400)}
	kept, cur := TruncateRecords(lines, 0, 1)
	if len(kept) != 1 || cur != "" {
		t.Fatalf("oversized single record: kept=%d cur=%q", len(kept), cur)
	}
}

func TestResumeMalformed(t *testing.T) {
	for _, c := range []string{"", "garbage", "b2ZmPS1i"} { // includes off=-? decode
		if off := Resume(c); off != 0 {
			t.Errorf("Resume(%q) = %d, want 0", c, off)
		}
	}
}

func TestRender(t *testing.T) {
	out := Render([]string{"a", "b"}, "TOK")
	if out != "a\nb\ncur TOK\n" {
		t.Fatalf("Render = %q", out)
	}
	if out := Render(nil, ""); out != "" {
		t.Fatalf("empty Render = %q", out)
	}
}
