package knowledge

import (
	"reflect"
	"strings"
	"testing"

	"github.com/jxsl13/spectackle/internal/workspace"
)

// sampleArtifact's Entries are listed in the format's canonical order —
// Kind ascending ("adr" < "intent" < "rule"), then Count descending, then
// Key ascending — the same order Marshal/Parse round-trip exactly, so this
// fixture doubles as a worked example of that ordering rule.
func sampleArtifact() Artifact {
	return Artifact{
		Sources: []string{"github.com/acme/repoA", "github.com/acme/repoB"},
		Entries: []Entry{
			{
				Kind:         KindADR,
				Question:     "How should retries work?",
				Context:      "Jobs fail transiently under load.",
				Decision:     "Retry up to 3 times with backoff.",
				Consequences: "Slightly higher tail latency on failure paths.",
				Status:       "accepted",
				Count:        1,
				Sources:      []Provenance{{Source: "github.com/acme/repoA", Dir: ""}},
				Key:          "cccc3333dddd4444",
			},
			{
				Kind:    KindIntent,
				Prose:   "Metal shader rules.",
				Count:   1,
				Sources: []Provenance{{Source: "github.com/acme/repoB", Dir: "gpu/metal"}},
				Key:     "eeee5555ffff6666",
			},
			{
				Kind:      KindRule,
				Text:      "The system SHALL log to `stderr` only.",
				Rationale: "Keeps container logs single-stream.",
				Count:     2,
				Sources: []Provenance{
					{Source: "github.com/acme/repoA", Dir: ""},
					{Source: "github.com/acme/repoB", Dir: "gpu"},
				},
				Key: "aaaa1111bbbb2222",
			},
		},
	}
}

// TestRoundTrip: Parse(Marshal(a)) must deep-equal a. This is the artifact
// format's central contract — it is the interchange format between
// repositories, so a lossy round trip would silently corrupt provenance or
// payload on every hop.
func TestRoundTrip(t *testing.T) {
	a := sampleArtifact()
	out, err := Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Parse(out)
	if err != nil {
		t.Fatalf("Parse(Marshal(a)) failed: %v\n---\n%s", err, out)
	}
	if !reflect.DeepEqual(a, got) {
		t.Fatalf("round trip mismatch:\n in=%#v\nout=%#v\n---\n%s", a, got, out)
	}
}

// TestMarshalDeterministic: two Marshal calls on the same content, even fed
// in different entry/provenance order, must produce byte-identical output —
// a condensate that reshuffles on every run is unreviewable in a diff.
func TestMarshalDeterministic(t *testing.T) {
	a := sampleArtifact()
	b := sampleArtifact()
	// reverse b's entries and one entry's provenance to prove Marshal
	// canonicalizes rather than trusting caller order.
	b.Entries[0], b.Entries[2] = b.Entries[2], b.Entries[0]
	b.Entries[0].Sources[0], b.Entries[0].Sources[len(b.Entries[0].Sources)-1] =
		b.Entries[0].Sources[len(b.Entries[0].Sources)-1], b.Entries[0].Sources[0]
	b.Sources[0], b.Sources[1] = b.Sources[1], b.Sources[0]

	outA, err := Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	outB, err := Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	if string(outA) != string(outB) {
		t.Fatalf("Marshal not deterministic under reordering:\n--- A ---\n%s\n--- B ---\n%s", outA, outB)
	}

	// and calling Marshal twice on the exact same value is trivially stable.
	outA2, err := Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	if string(outA) != string(outA2) {
		t.Fatalf("Marshal not stable across repeated calls:\n--- 1 ---\n%s\n--- 2 ---\n%s", outA, outA2)
	}
}

func TestSchemaStampRejected(t *testing.T) {
	bad := "---\nschema: v99\nkind: knowledge\nsources: []\n---\n"
	if _, err := Parse([]byte(bad)); err == nil {
		t.Fatal("expected schema mismatch error, got nil")
	}

	// sanity: the real stamp parses (empty artifact, no entries).
	ok := "---\nschema: " + workspace.SchemaStamp + "\nkind: knowledge\nsources: []\n---\n"
	a, err := Parse([]byte(ok))
	if err != nil {
		t.Fatalf("valid schema stamp rejected: %v", err)
	}
	if len(a.Entries) != 0 {
		t.Fatalf("expected no entries, got %+v", a.Entries)
	}
}

func TestParseRejectsWrongKind(t *testing.T) {
	bad := "---\nschema: " + workspace.SchemaStamp + "\nkind: spec\nsources: []\n---\n"
	if _, err := Parse([]byte(bad)); err == nil {
		t.Fatal("expected kind mismatch error, got nil")
	}
}

// TestMarshalExample pins one small, human-readable example of the format
// (a single rule entry) so the shape is visible in a test failure diff, not
// just asserted in prose.
func TestMarshalExample(t *testing.T) {
	a := Artifact{
		Sources: []string{"github.com/acme/repoA"},
		Entries: []Entry{{
			Kind:    KindRule,
			Text:    "The system SHALL log to stderr only.",
			Count:   1,
			Sources: []Provenance{{Source: "github.com/acme/repoA", Dir: ""}},
			Key:     "deadbeef",
		}},
	}
	out, err := Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, want := range []string{
		"schema: " + workspace.SchemaStamp,
		"kind: knowledge",
		"## rule deadbeef",
		"text: The system SHALL log to stderr only.",
		"count: 1",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("Marshal output missing %q:\n%s", want, s)
		}
	}
}
