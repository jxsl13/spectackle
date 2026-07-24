package knowledge

import (
	"reflect"
	"testing"
)

// TestFoldIntoAdditive: an incoming artifact with one entry current already
// has (by content key) and one it lacks yields exactly the entry current
// lacks, plus a Skipped count for the one it already had.
func TestFoldIntoAdditive(t *testing.T) {
	shared := ruleEntry("repoA", "", "The system SHALL log to stderr only.")
	novel := ruleEntry("repoB", "gpu", "The system SHALL check errors after every syscall.")

	current := Artifact{Sources: []string{"repoA"}, Entries: []Entry{shared}}
	incoming := Artifact{Sources: []string{"repoB"}, Entries: []Entry{
		// same identity as current's shared entry, but a different Sources
		// slice (as it would arrive from a different repository) — FoldInto
		// must still recognize it as already-present by (Kind, Key), not by
		// deep entry equality.
		ruleEntry("repoB", "", shared.Text),
		novel,
	}}

	plan := FoldInto(current, incoming)
	if plan.Skipped != 1 {
		t.Fatalf("Skipped = %d, want 1", plan.Skipped)
	}
	if len(plan.Entries) != 1 || plan.Entries[0].Key != novel.Key {
		t.Fatalf("Entries = %+v, want just novel (key %s)", plan.Entries, novel.Key)
	}
}

// TestFoldIntoDedupsOnContentKeyNotID: two entries with the same content Key
// arriving under what would be different receiving-repo rule IDs (this
// package never carries an ID pre-application — see NewEntry's doc) must
// still collapse to one addition. This is the property a fleet apply relies
// on: the receiving repo mints its own IDs, so identity can only ever be the
// content key.
func TestFoldIntoDedupsOnContentKeyNotID(t *testing.T) {
	text := "The build SHALL pin every dependency version."
	current := Artifact{}
	incoming := Artifact{Entries: []Entry{
		ruleEntry("repoA", "", text),
		ruleEntry("repoB", "", text), // same Text -> same Key, different Sources
	}}
	plan := FoldInto(current, incoming)
	if len(plan.Entries) != 1 {
		t.Fatalf("Entries = %d, want 1 (deduped on content key)", len(plan.Entries))
	}
	if plan.Skipped != 1 {
		t.Fatalf("Skipped = %d, want 1 (the incoming-internal duplicate)", plan.Skipped)
	}
}

// TestFoldIntoIdempotent: applying the plan's own entries back into current
// and folding the SAME incoming artifact a second time must yield nothing
// left to add — the idempotence proof a fleet rollout depends on (re-running
// apply against a workspace that already has the artifact must be a no-op).
func TestFoldIntoIdempotent(t *testing.T) {
	incoming := Artifact{Entries: []Entry{
		ruleEntry("repoA", "", "The system SHALL log to stderr only."),
		adrEntry("repoA", "How should retries work?", "Retry up to 3 times with backoff."),
	}}

	current := Artifact{}
	plan1 := FoldInto(current, incoming)
	if len(plan1.Entries) != 2 {
		t.Fatalf("first fold: Entries = %d, want 2", len(plan1.Entries))
	}
	if plan1.Skipped != 0 {
		t.Fatalf("first fold: Skipped = %d, want 0", plan1.Skipped)
	}

	// Simulate persisting plan1's entries: the workspace's own knowledge now
	// carries them (this is what re-Extracting the workspace after a real
	// apply would report).
	current.Entries = append(current.Entries, plan1.Entries...)

	plan2 := FoldInto(current, incoming)
	if len(plan2.Entries) != 0 {
		t.Fatalf("second fold: Entries = %+v, want none — idempotence broken", plan2.Entries)
	}
	if plan2.Skipped != len(incoming.Entries) {
		t.Fatalf("second fold: Skipped = %d, want %d", plan2.Skipped, len(incoming.Entries))
	}
}

// TestFoldIntoNeverTouchesCurrent: current's own entries — including one
// incoming does not even mention — pass through FoldInto untouched: no
// entry of current is ever dropped, mutated, or reported as removable.
// FoldInto only ever describes additions.
func TestFoldIntoNeverTouchesCurrent(t *testing.T) {
	localOnly := ruleEntry("thisrepo", "", "The CLI SHALL exit non-zero on any lint error.")
	current := Artifact{Entries: []Entry{localOnly}}
	before := append([]Entry(nil), current.Entries...)

	incoming := Artifact{Entries: []Entry{
		ruleEntry("repoB", "", "The system SHALL check errors after every syscall."),
	}}
	_ = FoldInto(current, incoming)

	if !reflect.DeepEqual(current.Entries, before) {
		t.Fatalf("FoldInto mutated current.Entries: got %+v, want %+v", current.Entries, before)
	}
}

// TestFoldIntoEmptyIncoming: folding an empty artifact adds nothing and
// skips nothing — there is nothing to react to.
func TestFoldIntoEmptyIncoming(t *testing.T) {
	current := Artifact{Entries: []Entry{ruleEntry("repoA", "", "The system SHALL log to stderr only.")}}
	plan := FoldInto(current, Artifact{})
	if len(plan.Entries) != 0 || plan.Skipped != 0 {
		t.Fatalf("plan = %+v, want empty", plan)
	}
}
