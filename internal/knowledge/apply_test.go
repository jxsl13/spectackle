package knowledge

import (
	"reflect"
	"testing"
)

// TestFoldIntoAdditive: every incoming entry the current artifact lacks
// comes back; nothing current already carries is duplicated.
func TestFoldIntoAdditive(t *testing.T) {
	have := ruleEntry("acme/repoA", "", "The system SHALL log to stderr only.")
	want := ruleEntry("acme/repoB", "", "The build SHALL pin every dependency version.")

	current := Artifact{Sources: []string{"acme/repoA"}, Entries: []Entry{have}}
	incoming := Artifact{Sources: []string{"acme/repoB"}, Entries: []Entry{have, want}}

	delta := FoldInto(current, incoming)
	if len(delta.Entries) != 1 {
		t.Fatalf("want 1 delta entry, got %d: %+v", len(delta.Entries), delta.Entries)
	}
	if delta.Entries[0].Key != want.Key {
		t.Fatalf("delta entry key = %q, want %q (the already-present rule must not reappear)", delta.Entries[0].Key, want.Key)
	}
}

// TestFoldIntoIdempotent: once a delta has been (simulated as) folded into
// the workspace — current now contains what the first FoldInto returned —
// folding the same incoming artifact again yields nothing.
func TestFoldIntoIdempotent(t *testing.T) {
	a := ruleEntry("acme/repoA", "", "The system SHALL log to stderr only.")
	b := adrEntry("acme/repoA", "How should retries work?", "Retry up to 3 times with backoff.")
	incoming := Artifact{Sources: []string{"acme/repoA"}, Entries: []Entry{a, b}}

	current := Artifact{} // empty workspace
	first := FoldInto(current, incoming)
	if len(first.Entries) != 2 {
		t.Fatalf("first fold: want 2 entries, got %d", len(first.Entries))
	}

	// simulate "these landed": the workspace's own next Extract would now
	// carry both entries.
	current = Artifact{Sources: current.Sources, Entries: append(append([]Entry(nil), current.Entries...), first.Entries...)}

	second := FoldInto(current, incoming)
	if len(second.Entries) != 0 {
		t.Fatalf("second fold on an already-current workspace: want 0 entries, got %d: %+v", len(second.Entries), second.Entries)
	}
}

// TestFoldIntoDedupsWithinIncoming: a hand-assembled or concatenated
// incoming artifact may itself carry the same (Kind, Key) twice — FoldInto
// must not duplicate it in the delta.
func TestFoldIntoDedupsWithinIncoming(t *testing.T) {
	text := "The system SHALL check errors after every syscall."
	e1 := ruleEntry("acme/repoA", "", text)
	e2 := ruleEntry("acme/repoB", "", text) // same content, different asserting repo — same Key
	incoming := Artifact{Sources: []string{"acme/repoA", "acme/repoB"}, Entries: []Entry{e1, e2}}

	delta := FoldInto(Artifact{}, incoming)
	if len(delta.Entries) != 1 {
		t.Fatalf("want 1 deduped delta entry, got %d: %+v", len(delta.Entries), delta.Entries)
	}
}

// TestFoldIntoNeverMutatesInputs: FoldInto must not alias or mutate either
// argument's backing slices — both current and incoming are read-only from
// its point of view.
func TestFoldIntoNeverMutatesInputs(t *testing.T) {
	current := Artifact{Entries: []Entry{ruleEntry("acme/repoA", "", "The system SHALL log to stderr only.")}}
	incomingEntries := []Entry{
		ruleEntry("acme/repoA", "", "The system SHALL log to stderr only."),
		ruleEntry("acme/repoB", "", "The build SHALL pin every dependency version."),
	}
	incoming := Artifact{Entries: incomingEntries}
	currentCopy := Artifact{Entries: append([]Entry(nil), current.Entries...)}
	incomingCopy := Artifact{Entries: append([]Entry(nil), incoming.Entries...)}

	_ = FoldInto(current, incoming)

	if !reflect.DeepEqual(current.Entries, currentCopy.Entries) {
		t.Fatalf("FoldInto mutated current: %+v vs %+v", current.Entries, currentCopy.Entries)
	}
	if !reflect.DeepEqual(incoming.Entries, incomingCopy.Entries) {
		t.Fatalf("FoldInto mutated incoming: %+v vs %+v", incoming.Entries, incomingCopy.Entries)
	}
}

// TestFoldIntoEmptyIncoming: nothing to add is a valid, non-error result —
// an empty (nil) Entries slice, not a panic or a spurious non-nil artifact.
func TestFoldIntoEmptyIncoming(t *testing.T) {
	current := Artifact{Entries: []Entry{ruleEntry("acme/repoA", "", "The system SHALL log to stderr only.")}}
	delta := FoldInto(current, Artifact{})
	if len(delta.Entries) != 0 {
		t.Fatalf("want no delta entries, got %+v", delta.Entries)
	}
}
