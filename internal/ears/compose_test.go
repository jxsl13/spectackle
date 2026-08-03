package ears

import (
	"os"
	"path/filepath"
	"testing"
)

// TestComposeUbiquitous: leading "the " is stripped from the system and the
// trailing "." trimmed from the response then re-added by Compose.
func TestComposeUbiquitous(t *testing.T) {
	got, err := Compose(PUbiquitous, Slots{System: "the server", Response: "log to stderr."})
	if err != nil {
		t.Fatal(err)
	}
	if want := "The server SHALL log to stderr."; got != want {
		t.Errorf("Compose = %q, want %q", got, want)
	}
}

func TestComposeEvent(t *testing.T) {
	got, err := Compose(PEvent, Slots{System: "the wrapper", Response: "check status", Trigger: "a kernel is launched"})
	if err != nil {
		t.Fatal(err)
	}
	if want := "WHEN a kernel is launched, the wrapper SHALL check status."; got != want {
		t.Errorf("Compose = %q, want %q", got, want)
	}
}

func TestComposeStateUnwantedOptional(t *testing.T) {
	cases := []struct {
		name string
		p    Pattern
		s    Slots
		want string
	}{
		{"state", PState,
			Slots{System: "the cache", Response: "serve reads", State: "a write is pending"},
			"WHILE a write is pending, the cache SHALL serve reads."},
		{"unwanted", PUnwanted,
			Slots{System: "the server", Response: "reject the claim", Condition: "a lease overlaps"},
			"IF a lease overlaps, THEN the server SHALL reject the claim."},
		{"optional", POptional,
			Slots{System: "the driver", Response: "emit metrics", Feature: "telemetry is enabled"},
			"WHERE telemetry is enabled, the driver SHALL emit metrics."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Compose(c.p, c.s)
			if err != nil {
				t.Fatal(err)
			}
			if got != c.want {
				t.Errorf("Compose = %q, want %q", got, c.want)
			}
		})
	}
}

// TestComposeComplexClauseOrder: Complex clauses are emitted in the EARS
// canonical order WHERE < WHILE < WHEN/IF, and a Condition (no Trigger)
// takes the "IF …, THEN the …" form.
func TestComposeComplexClauseOrder(t *testing.T) {
	got, err := Compose(PComplex, Slots{
		System: "the server", Response: "flush",
		Trigger: "t happens", State: "s holds", Feature: "f is set",
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := "WHERE f is set, WHILE s holds, WHEN t happens, the server SHALL flush."; got != want {
		t.Errorf("Compose(trigger) = %q, want %q", got, want)
	}

	got, err = Compose(PComplex, Slots{
		System: "the server", Response: "flush",
		Condition: "c holds", State: "s holds",
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := "WHILE s holds, IF c holds, THEN the server SHALL flush."; got != want {
		t.Errorf("Compose(condition) = %q, want %q", got, want)
	}
}

// TestComposeRoundTripsThroughClassify pins the key invariant: composition
// and recognition are inverse — every Composed sentence Classifies back to
// the pattern it was composed for.
func TestComposeRoundTripsThroughClassify(t *testing.T) {
	cases := []struct {
		p Pattern
		s Slots
	}{
		{PUbiquitous, Slots{System: "the server", Response: "log to stderr"}},
		{PEvent, Slots{System: "the wrapper", Response: "check status", Trigger: "a kernel is launched"}},
		{PState, Slots{System: "the cache", Response: "serve reads", State: "a write is pending"}},
		{PUnwanted, Slots{System: "the server", Response: "reject the claim", Condition: "a lease overlaps"}},
		{POptional, Slots{System: "the driver", Response: "emit metrics", Feature: "telemetry is enabled"}},
		{PComplex, Slots{System: "the server", Response: "flush", Trigger: "t happens", State: "s holds"}},
	}
	for _, c := range cases {
		t.Run(c.p.String(), func(t *testing.T) {
			sentence, err := Compose(c.p, c.s)
			if err != nil {
				t.Fatal(err)
			}
			if got := Classify(sentence); got != c.p {
				t.Errorf("Classify(%q) = %s, want %s", sentence, got, c.p)
			}
		})
	}
}

func TestComposeMissingSlotErrors(t *testing.T) {
	got, err := Compose(PEvent, Slots{System: "x", Response: "y"}) // no Trigger
	if err == nil {
		t.Fatal("expected error for missing trigger, got nil")
	}
	if got != "" {
		t.Errorf("sentence = %q, want empty on error", got)
	}
}

func TestMissingSlots(t *testing.T) {
	has := func(ss []string, want string) bool {
		for _, s := range ss {
			if s == want {
				return true
			}
		}
		return false
	}

	if m := MissingSlots(PUbiquitous, Slots{}); !has(m, "system") || !has(m, "response") {
		t.Errorf("empty ubiquitous missing = %v, want system+response", m)
	}
	if m := MissingSlots(PEvent, Slots{System: "x", Response: "y"}); !has(m, "trigger") {
		t.Errorf("event missing = %v, want trigger", m)
	}
	if m := MissingSlots(PComplex, Slots{System: "x", Response: "y", Trigger: "t"}); !has(m, "two of: trigger,state,condition,feature") {
		t.Errorf("complex with one clause missing = %v, want the 'two of' entry", m)
	}
	if m := MissingSlots(PUbiquitous, Slots{System: "x", Response: "y"}); len(m) != 0 {
		t.Errorf("fully filled ubiquitous missing = %v, want empty", m)
	}
}

func TestPatternFromString(t *testing.T) {
	cases := map[string]Pattern{
		"U": PUbiquitous, "E": PEvent, "S": PState,
		"N": PUnwanted, "O": POptional, "C": PComplex,
		"e": PEvent, "c": PComplex, // lowercase accepted
		" x ": PInvalid, "": PInvalid,
	}
	for in, want := range cases {
		if got := PatternFromString(in); got != want {
			t.Errorf("PatternFromString(%q) = %s, want %s", in, got, want)
		}
	}
	// round-trip over PatternLetters(), NOT over a hand-listed slice of the
	// six constants: the literal could not notice a seventh pattern, which is
	// the drift T-01KYT2EHRMEAH swept. Note this loop is deliberately weak on
	// its own — String() and PatternFromString index the SAME array, so an
	// added letter round-trips cleanly. What it does pin is that no letter the
	// package advertises is unparseable, and that index 0 stays out.
	letters := PatternLetters()
	if len(letters) == 0 {
		t.Fatal("PatternLetters() is empty — the pattern set cannot be advertised at all")
	}
	for _, n := range letters {
		if n == "?" {
			t.Errorf("PatternLetters() leaks the PInvalid sentinel %q", n)
		}
		p := PatternFromString(n)
		if p == PInvalid {
			t.Errorf("advertised letter %q parses as PInvalid", n)
			continue
		}
		if got := p.String(); got != n {
			t.Errorf("round-trip %q: PatternFromString -> %s -> String() = %q", n, p, got)
		}
	}
}

func TestParseSections(t *testing.T) {
	text := "## intent\nline a\n\n## SPX-FOO-001\nThe system SHALL do x.\n## notes\nn1\n"
	secs := ParseSections(text)
	if len(secs) != 2 {
		t.Fatalf("got %d sections, want 2: %+v", len(secs), secs)
	}
	if secs[0].Name != "intent" || secs[0].Text != "line a" || secs[0].Line != 1 {
		t.Errorf("section 0 = %+v, want intent/'line a'/line 1", secs[0])
	}
	if secs[1].Name != "notes" || secs[1].Text != "n1" {
		t.Errorf("section 1 = %+v, want notes/'n1'", secs[1])
	}
}

func TestIsProseSection(t *testing.T) {
	for _, name := range []string{"intent", "notes", "design", "context"} {
		if !IsProseSection(name) {
			t.Errorf("IsProseSection(%q) = false, want true", name)
		}
	}
	for _, name := range []string{"SPX-FOO-001", "random", ""} {
		if IsProseSection(name) {
			t.Errorf("IsProseSection(%q) = true, want false", name)
		}
	}
}

func TestLintFile(t *testing.T) {
	dir := t.TempDir()

	countErrors := func(fs []Finding) int {
		n := 0
		for _, f := range fs {
			if f.Severity == Error {
				n++
			}
		}
		return n
	}

	// a file with a valid rule + a vague (invalid) one: at least one Error
	mixed := filepath.Join(dir, "mixed.md")
	if err := os.WriteFile(mixed, []byte("---\nschema: v1\nprefix: A\n---\n## A-001\nThe system SHALL log to `stderr` only.\n\n## A-002\nhandle errors appropriately.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fs, err := LintFile(mixed)
	if err != nil {
		t.Fatal(err)
	}
	if countErrors(fs) < 1 {
		t.Errorf("mixed file: got %d errors, want >=1: %+v", countErrors(fs), fs)
	}

	// a file with only the valid rule: no Error findings
	clean := filepath.Join(dir, "clean.md")
	if err := os.WriteFile(clean, []byte("---\nschema: v1\nprefix: A\n---\n## A-001\nThe system SHALL log to `stderr` only.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fs, err = LintFile(clean)
	if err != nil {
		t.Fatal(err)
	}
	if countErrors(fs) != 0 {
		t.Errorf("clean file: got %d errors, want 0: %+v", countErrors(fs), fs)
	}
}
