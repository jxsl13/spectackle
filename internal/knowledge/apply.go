package knowledge

// FoldInto computes the additive diff of incoming over current — the entries
// a workspace's own knowledge (current, as Extract would report it right
// now) does not yet carry. This is the "apply an artifact to a target repo"
// step internal/knowledge's package doc named as deliberately out of scope
// for the base package; it stays a pure function here, with no workspace or
// file I/O of its own, because actually persisting Plan.Entries goes through
// the very same server-side write paths every other .spectackle mutation
// does (spec.AddRule for a KindRule entry, lifecycle.Draft+item.Upsert for a
// KindADR entry) — see internal/mcpserver's knowledge tool, the caller. This
// package has no dependency on those write paths and no business making
// one: it only decides WHAT to add, never HOW to write it.
//
// The name is deliberately not "Apply": that identifier already means "fold
// a Resolution into an artifact" (merge.go) — a different operation on a
// different pair of types. FoldInto reads as the mirror of that: fold an
// Artifact into a workspace's own knowledge, rather than a Resolution into
// an Artifact.
//
// Identity for this diff is (Kind, Key) — the same identity Merge groups
// entries by — never a rule ID or item ID: the receiving repository mints
// its own IDs on write, so the same sentence arriving twice (say, from two
// fleet artifacts, or the same artifact applied twice) has to be recognized
// by content, not by an ID that was never shared in the first place.
func FoldInto(current, incoming Artifact) Plan {
	type bucketKey struct {
		kind EntryKind
		key  string
	}
	have := make(map[bucketKey]bool, len(current.Entries))
	for _, e := range current.Entries {
		have[bucketKey{e.Kind, e.Key}] = true
	}

	var plan Plan
	seen := map[bucketKey]bool{}
	for _, e := range incoming.Entries {
		bk := bucketKey{e.Kind, e.Key}
		if have[bk] || seen[bk] {
			// Additive only: an identity current already carries is skipped
			// wholesale, its substance never compared. If current's answer
			// disagrees with incoming's (e.g. two ADRs answering the same
			// question differently), that disagreement is current's own
			// local specialization — FoldInto does not overwrite it, and
			// does not even surface it as a Conflict the way Merge would;
			// Merge is the tool for reconciling two artifacts BEFORE one is
			// applied, not this one.
			plan.Skipped++
			continue
		}
		seen[bk] = true
		plan.Entries = append(plan.Entries, e)
	}
	sortEntries(plan.Entries)
	return plan
}

// Plan is FoldInto's result: Entries is what incoming has that current
// lacks — the additive worklist a caller persists — and Skipped counts
// incoming entries dropped because current (or an earlier entry in this
// same incoming, deduped against itself too) already carries that identity.
// Applying the same artifact to the same workspace twice is the
// idempotence contract this package promises: the second FoldInto call (run
// against a current that now reflects the first call's writes) returns an
// empty Entries and Skipped == len(incoming.Entries) — nothing left to add,
// nothing silently redone.
type Plan struct {
	Entries []Entry
	Skipped int
}
