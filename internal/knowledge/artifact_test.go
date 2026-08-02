package knowledge

import (
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/jxsl13/spectackle/internal/drift"
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
				Key:          contentKey(Entry{Kind: KindADR, Question: "How should retries work?"}),
			},
			{
				Kind:    KindIntent,
				Prose:   "Metal shader rules.",
				Count:   1,
				Sources: []Provenance{{Source: "github.com/acme/repoB", Dir: "gpu/metal"}},
				Key:     contentKey(Entry{Kind: KindIntent, Prose: "Metal shader rules."}),
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
				Key: contentKey(Entry{Kind: KindRule, Text: "The system SHALL log to `stderr` only."}),
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

// TestDerivedFromProvenance: an LLM-authored generalization has no single
// asserting repository — Sources may be empty — but its DerivedFrom sources
// (the repositories the generalization was drawn from) must round-trip
// intact, and asserted-by/derived-from must not collapse into each other:
// an entry with both populated must come back with both, distinct.
func TestDerivedFromProvenance(t *testing.T) {
	a := Artifact{
		Sources: []string{"github.com/acme/repoA", "github.com/acme/repoB"},
		Entries: []Entry{
			{
				// purely derived: no repository literally asserts this
				// text, so Sources is empty.
				Kind:  KindRule,
				Text:  "Configuration SHALL live in one place.",
				Count: 2,
				DerivedFrom: []Provenance{
					{Source: "github.com/acme/repoA", Dir: ""},
					{Source: "github.com/acme/repoB", Dir: "gpu"},
				},
				Key: contentKey(Entry{Kind: KindRule, Text: "Configuration SHALL live in one place."}),
			},
			{
				// both kinds of evidence at once: repoA asserts it
				// verbatim, repoB only fed an LLM's generalization of it.
				Kind:    KindRule,
				Text:    "The system SHALL check errors after every syscall.",
				Count:   2,
				Sources: []Provenance{{Source: "github.com/acme/repoA", Dir: ""}},
				DerivedFrom: []Provenance{
					{Source: "github.com/acme/repoB", Dir: ""},
				},
				Key: contentKey(Entry{Kind: KindRule, Text: "The system SHALL check errors after every syscall."}),
			},
		},
	}
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
	if len(got.Entries[0].Sources) != 0 {
		t.Fatalf("purely derived entry gained an asserted-by Source: %+v", got.Entries[0])
	}
	if len(got.Entries[0].DerivedFrom) != 2 {
		t.Fatalf("purely derived entry lost DerivedFrom rows: %+v", got.Entries[0])
	}
	mixed := got.Entries[1]
	if len(mixed.Sources) != 1 || len(mixed.DerivedFrom) != 1 || mixed.Sources[0] == mixed.DerivedFrom[0] {
		t.Fatalf("asserted-by and derived-from collapsed into each other: %+v", mixed)
	}
	// the wire form itself must use two distinct keys, not one shared one.
	s := string(out)
	if !strings.Contains(s, "sources:") || !strings.Contains(s, "derived_from:") {
		t.Fatalf("Marshal output does not carry both sources and derived_from as distinct keys:\n%s", s)
	}
}

// TestNewEntryKeyMatchesExtract: the whole point of computing an LLM-
// supplied entry's key inside the package (never accepting one from the
// caller) is that it lands on exactly the same key Extract would compute
// for identical text — that identity is what lets an LLM-authored entry
// merge with one Extract lifted out of a repository's own files. Assert
// the equality directly, for all three kinds.
func TestNewEntryKeyMatchesExtract(t *testing.T) {
	prov := []Provenance{{Source: "github.com/acme/repoA"}}

	ruleText := "The system SHALL log to stderr only."
	rule, err := NewEntry(KindRule, Entry{Text: ruleText}, prov, nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := drift.NormHash([]byte(ruleText)); rule.Key != want {
		t.Fatalf("rule entry key = %q, want %q (Extract's formula)", rule.Key, want)
	}

	question := "How should retries work?"
	adr, err := NewEntry(KindADR, Entry{Question: question, Decision: "Retry up to 3 times with backoff."}, prov, nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := drift.NormHash([]byte(question)); adr.Key != want {
		t.Fatalf("adr entry key = %q, want %q (Extract's formula)", adr.Key, want)
	}

	prose := "This service exists to keep GPU kernels honest."
	intent, err := NewEntry(KindIntent, Entry{Prose: prose}, prov, nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := drift.NormHash([]byte(prose)); intent.Key != want {
		t.Fatalf("intent entry key = %q, want %q (Extract's formula)", intent.Key, want)
	}

	// an LLM-authored entry with no asserting repository at all (purely
	// derived) still gets the same key — Key never depends on provenance.
	derived, err := NewEntry(KindRule, Entry{Text: ruleText}, nil, prov)
	if err != nil {
		t.Fatal(err)
	}
	if derived.Key != rule.Key {
		t.Fatalf("derived-only entry key = %q, want %q (same text, same key regardless of provenance shape)", derived.Key, rule.Key)
	}
}

// TestNewEntryRejectsMalformed: NewEntry must reject rather than silently
// normalize a bad input — this package still never invents or coerces
// prose on its own.
func TestNewEntryRejectsMalformed(t *testing.T) {
	prov := []Provenance{{Source: "github.com/acme/repoA"}}

	cases := []struct {
		name    string
		kind    EntryKind
		payload Entry
		derived []Provenance
	}{
		{"unknown kind", EntryKind("essay"), Entry{Text: "whatever"}, nil},
		{"empty rule text", KindRule, Entry{Text: "   "}, nil},
		{"adr missing question", KindADR, Entry{Decision: "Retry."}, nil},
		{"adr missing decision", KindADR, Entry{Question: "How should retries work?"}, nil},
		{"empty intent prose", KindIntent, Entry{Prose: ""}, nil},
		{"no provenance at all", KindRule, Entry{Text: "The system SHALL log to stderr only."}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assertedBy := prov
			if c.name == "no provenance at all" {
				assertedBy = nil
			}
			if _, err := NewEntry(c.kind, c.payload, assertedBy, c.derived); err == nil {
				t.Fatalf("NewEntry(%s) accepted a malformed entry, want error", c.name)
			}
		})
	}
}

// TestParseRecomputesContentKey: the key on the wire is transport, not
// identity. NewEntry refuses a caller-supplied key outright and Extract
// derives one from content, but Parse used to take the section heading
// verbatim — so an artifact written by hand, by an older key scheme, or by
// any producer that simply chose differently defeated every downstream
// identity check at once: FoldInto re-added the same rule on every apply,
// Merge never saw two repositories answering one question, and knowledge
// apply re-asked a decision already on the board.
func TestParseRecomputesContentKey(t *testing.T) {
	raw := "---\nschema: v1\nkind: knowledge\nsources:\n    - repo-a\n---\n\n" +
		"## rule not-a-content-key\ntext: The system SHALL log to `stderr` only.\ncount: 1\nsources:\n    - source: repo-a\n      dir: \"\"\n\n" +
		"## adr also-not-one\nquestion: How should retries work?\ndecision: thrice\ncount: 1\nsources:\n    - source: repo-a\n      dir: \"\"\n"
	got, err := Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	want := map[EntryKind]string{
		KindRule: contentKey(Entry{Kind: KindRule, Text: "The system SHALL log to `stderr` only."}),
		KindADR:  contentKey(Entry{Kind: KindADR, Question: "How should retries work?"}),
	}
	for _, e := range got.Entries {
		if e.Key != want[e.Kind] {
			t.Fatalf("%s key not recomputed from content: got %q want %q", e.Kind, e.Key, want[e.Kind])
		}
	}
	// and the recomputed key is the SAME one this repository's own Extract
	// would mint for that sentence — which is the whole point: it is what
	// makes two repositories recognize one sentence as one entry.
	if len(got.Entries) != 2 {
		t.Fatalf("expected both entries, got %d", len(got.Entries))
	}
}

// TestParseKeepsWireKeyWhenNothingToHash: an entry with no identifying
// payload cannot be content-keyed, and collapsing every such entry onto one
// empty identity would make them dedup against each other.
func TestParseKeepsWireKeyWhenNothingToHash(t *testing.T) {
	raw := "---\nschema: v1\nkind: knowledge\nsources:\n    - repo-a\n---\n\n" +
		"## rule wire-key-one\nrationale: no text at all\ncount: 1\n\n" +
		"## rule wire-key-two\nrationale: also none\ncount: 1\n"
	got, err := Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(got.Entries))
	}
	if got.Entries[0].Key == got.Entries[1].Key {
		t.Fatalf("unhashable entries must keep distinct wire keys, both got %q", got.Entries[0].Key)
	}
}

// TestParseErrorLineIsInputCoordinates pins B-01KYRVXQ02FDH's line-number
// face at this package's own gate: a yaml complaint Parse surfaces must
// carry a line number in the CALLER'S coordinate system, because the caller
// holds the bytes it passed in and nothing else. Parse used to hand yaml
// only the failing entry's slice of the body, so the reported number was
// entry-relative — `line 6` of an entry that started at line 17 of the
// input, a coordinate that resolves in the caller's own text to a line with
// nothing wrong with it. Misdirection is worse than no error: the reader
// spends the recovery budget at the wrong place first.
func TestParseErrorLineIsInputCoordinates(t *testing.T) {
	raw, err := Marshal(sampleArtifact())
	if err != nil {
		t.Fatal(err)
	}
	src := string(raw)
	if !strings.HasSuffix(src, "\n") {
		src += "\n"
	}
	// exactly what `knowledge op=export` appends to its own artifact — a
	// plain scalar with no colon, which yaml refuses inside the mapping
	// the preceding entry opened.
	const bad = "ok export entries=3 rule=1 adr=1 intent=1"
	src += bad + "\n"

	// the line the caller would count to, 1-based, in the bytes it passed.
	want := 0
	for i, l := range strings.Split(src, "\n") {
		if l == bad {
			want = i + 1
			break
		}
	}
	if want == 0 {
		t.Fatalf("fixture is wrong, %q is not a line of the input", bad)
	}

	_, err = Parse([]byte(src))
	if err == nil {
		t.Fatalf("appending %q parsed cleanly, the fixture no longer reproduces anything", bad)
	}
	m := regexp.MustCompile(`line (\d+)`).FindStringSubmatch(err.Error())
	if m == nil {
		t.Fatalf("parse error carries no line coordinate at all: %v", err)
	}
	got, convErr := strconv.Atoi(m[1])
	if convErr != nil {
		t.Fatal(convErr)
	}
	if got != want {
		t.Fatalf("parse error names line %d; %q is line %d of the input Parse was handed — the caller cannot locate line %d: %v",
			got, bad, want, got, err)
	}
}
