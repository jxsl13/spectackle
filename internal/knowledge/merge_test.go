package knowledge

import (
	"reflect"
	"testing"

	"github.com/jxsl13/spectackle/internal/drift"
)

func ruleEntry(source, dir, text string) Entry {
	return Entry{
		Kind:    KindRule,
		Text:    text,
		Count:   1,
		Sources: []Provenance{{Source: source, Dir: dir}},
		Key:     drift.NormHash([]byte(text)),
	}
}

func adrEntry(source, question, decision string) Entry {
	return Entry{
		Kind:     KindADR,
		Question: question,
		Decision: decision,
		Count:    1,
		Sources:  []Provenance{{Source: source, Dir: ""}},
		Key:      drift.NormHash([]byte(question)),
	}
}

// TestMergeRecurrenceRanking: a convention five (here: three) repositories
// independently assert outranks one only two agree on, which outranks one
// only a single repository has — recurrence, not any paraphrase judgment,
// drives the order.
func TestMergeRecurrenceRanking(t *testing.T) {
	widely := "The system SHALL log to stderr only."
	common := "The system SHALL check errors after every syscall."
	rare := "The build SHALL pin every dependency version."

	a := Artifact{Sources: []string{"repoA"}, Entries: []Entry{
		ruleEntry("repoA", "", widely), ruleEntry("repoA", "", common), ruleEntry("repoA", "", rare),
	}}
	b := Artifact{Sources: []string{"repoB"}, Entries: []Entry{
		ruleEntry("repoB", "", widely), ruleEntry("repoB", "", common),
	}}
	c := Artifact{Sources: []string{"repoC"}, Entries: []Entry{
		ruleEntry("repoC", "", widely),
	}}

	merged, conflicts, err := Merge(a, b, c)
	if err != nil {
		t.Fatal(err)
	}
	if len(conflicts) != 0 {
		t.Fatalf("expected no conflicts, got %+v", conflicts)
	}
	if len(merged.Entries) != 3 {
		t.Fatalf("want 3 distinct rule entries, got %d: %+v", len(merged.Entries), merged.Entries)
	}
	if merged.Entries[0].Text != widely || merged.Entries[0].Count != 3 {
		t.Fatalf("rank 0 = %+v, want text=%q count=3", merged.Entries[0], widely)
	}
	if merged.Entries[1].Text != common || merged.Entries[1].Count != 2 {
		t.Fatalf("rank 1 = %+v, want text=%q count=2", merged.Entries[1], common)
	}
	if merged.Entries[2].Text != rare || merged.Entries[2].Count != 1 {
		t.Fatalf("rank 2 = %+v, want text=%q count=1", merged.Entries[2], rare)
	}
	wantSources := []string{"repoA", "repoB", "repoC"}
	if !reflect.DeepEqual(merged.Sources, wantSources) {
		t.Fatalf("merged.Sources = %v, want %v", merged.Sources, wantSources)
	}
}

// TestMergeIdempotent: Merge(a) reproduces a (modulo provenance
// normalization — a is already canonical here), and Merge(a, a) must not
// double-count a's own sources as two distinct assertors.
func TestMergeIdempotent(t *testing.T) {
	text := "The system SHALL log to stderr only."
	a := Artifact{Sources: []string{"repoA"}, Entries: []Entry{ruleEntry("repoA", "", text)}}

	once, conflicts, err := Merge(a)
	if err != nil {
		t.Fatal(err)
	}
	if len(conflicts) != 0 {
		t.Fatalf("Merge(a) produced conflicts: %+v", conflicts)
	}
	if !reflect.DeepEqual(once, a) {
		t.Fatalf("Merge(a) != a:\n got=%+v\nwant=%+v", once, a)
	}

	twice, conflicts, err := Merge(a, a)
	if err != nil {
		t.Fatal(err)
	}
	if len(conflicts) != 0 {
		t.Fatalf("Merge(a, a) produced conflicts: %+v", conflicts)
	}
	if len(twice.Entries) != 1 {
		t.Fatalf("Merge(a, a) split one entry into %d", len(twice.Entries))
	}
	if twice.Entries[0].Count != 1 {
		t.Fatalf("Merge(a, a) double-counted a's own source: Count = %d, want 1", twice.Entries[0].Count)
	}
	if len(twice.Entries[0].Sources) != 1 {
		t.Fatalf("Merge(a, a) duplicated provenance rows: %+v", twice.Entries[0].Sources)
	}
	if !reflect.DeepEqual(twice, once) {
		t.Fatalf("Merge(a, a) != Merge(a):\n got=%+v\nwant=%+v", twice, once)
	}
}

// TestMergeADRConflictSurfaced: two ADRs answering the same question
// differently must come back as a Conflict, never a silent pick — erasing
// the disagreement would be the worst possible behavior here.
func TestMergeADRConflictSurfaced(t *testing.T) {
	q := "How should retries work?"
	a := Artifact{Sources: []string{"repoA"}, Entries: []Entry{adrEntry("repoA", q, "Retry up to 3 times with backoff.")}}
	b := Artifact{Sources: []string{"repoB"}, Entries: []Entry{adrEntry("repoB", q, "Never retry; fail fast and alert.")}}

	merged, conflicts, err := Merge(a, b)
	if err != nil {
		t.Fatal(err)
	}
	if len(merged.Entries) != 0 {
		t.Fatalf("disagreeing ADRs must not be silently merged, got: %+v", merged.Entries)
	}
	if len(conflicts) != 1 {
		t.Fatalf("want exactly 1 conflict, got %d: %+v", len(conflicts), conflicts)
	}
	conf := conflicts[0]
	if conf.Kind != KindADR {
		t.Fatalf("conflict kind = %s, want adr", conf.Kind)
	}
	if len(conf.Entries) != 2 {
		t.Fatalf("want 2 distinct answers in the conflict, got %d: %+v", len(conf.Entries), conf.Entries)
	}
	decisions := map[string]bool{}
	for _, e := range conf.Entries {
		if e.Question != q {
			t.Fatalf("conflict entry lost the shared question: %+v", e)
		}
		decisions[e.Decision] = true
	}
	if !decisions["Retry up to 3 times with backoff."] || !decisions["Never retry; fail fast and alert."] {
		t.Fatalf("both original decisions must survive intact, got: %+v", conf.Entries)
	}
}

// TestMergeAgreeingADRsDoNotConflict: same question, same decision from two
// sources is recurrence, not disagreement — it must merge normally.
func TestMergeAgreeingADRsDoNotConflict(t *testing.T) {
	q := "How should retries work?"
	dec := "Retry up to 3 times with backoff."
	a := Artifact{Sources: []string{"repoA"}, Entries: []Entry{adrEntry("repoA", q, dec)}}
	b := Artifact{Sources: []string{"repoB"}, Entries: []Entry{adrEntry("repoB", q, dec)}}

	merged, conflicts, err := Merge(a, b)
	if err != nil {
		t.Fatal(err)
	}
	if len(conflicts) != 0 {
		t.Fatalf("agreeing ADRs must not conflict, got %+v", conflicts)
	}
	if len(merged.Entries) != 1 || merged.Entries[0].Count != 2 {
		t.Fatalf("want 1 entry with count 2, got %+v", merged.Entries)
	}
}

// TestMergeDeterministicOrdering: two runs over the same set of artifacts,
// fed in different argument order, must produce the same result — a
// condensate that reshuffles on every run is unreviewable in a diff.
func TestMergeDeterministicOrdering(t *testing.T) {
	widely := "The system SHALL log to stderr only."
	common := "The system SHALL check errors after every syscall."
	a := Artifact{Sources: []string{"repoA"}, Entries: []Entry{ruleEntry("repoA", "", widely), ruleEntry("repoA", "", common)}}
	b := Artifact{Sources: []string{"repoB"}, Entries: []Entry{ruleEntry("repoB", "", widely)}}
	c := Artifact{Sources: []string{"repoC"}, Entries: []Entry{ruleEntry("repoC", "", widely), ruleEntry("repoC", "", common)}}

	m1, _, err := Merge(a, b, c)
	if err != nil {
		t.Fatal(err)
	}
	m2, _, err := Merge(c, a, b)
	if err != nil {
		t.Fatal(err)
	}
	m3, _, err := Merge(b, c, a)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(m1, m2) || !reflect.DeepEqual(m2, m3) {
		t.Fatalf("Merge is not order-independent:\nm1=%+v\nm2=%+v\nm3=%+v", m1, m2, m3)
	}

	out1, err := Marshal(m1)
	if err != nil {
		t.Fatal(err)
	}
	out2, err := Marshal(m2)
	if err != nil {
		t.Fatal(err)
	}
	if string(out1) != string(out2) {
		t.Fatalf("Marshal(Merge(...)) not byte-identical across argument orders:\n--- 1 ---\n%s\n--- 2 ---\n%s", out1, out2)
	}
}

// TestMergeAssociative: Merge(Merge(a, b), c) must agree with Merge(a, b, c)
// — associativity is what lets a condensate be built incrementally (e.g. one
// repository at a time) instead of requiring every artifact up front.
func TestMergeAssociative(t *testing.T) {
	widely := "The system SHALL log to stderr only."
	a := Artifact{Sources: []string{"repoA"}, Entries: []Entry{ruleEntry("repoA", "", widely)}}
	b := Artifact{Sources: []string{"repoB"}, Entries: []Entry{ruleEntry("repoB", "", widely)}}
	c := Artifact{Sources: []string{"repoC"}, Entries: []Entry{ruleEntry("repoC", "", widely)}}

	allAtOnce, _, err := Merge(a, b, c)
	if err != nil {
		t.Fatal(err)
	}
	ab, _, err := Merge(a, b)
	if err != nil {
		t.Fatal(err)
	}
	incremental, _, err := Merge(ab, c)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(allAtOnce, incremental) {
		t.Fatalf("Merge not associative:\n all-at-once=%+v\n incremental=%+v", allAtOnce, incremental)
	}
}
