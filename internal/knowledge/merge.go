package knowledge

import (
	"sort"

	"github.com/jxsl13/spectackle/internal/drift"
)

// Conflict is surfaced instead of silently merged, when two or more entries
// share an identity (same Kind and content Key) but disagree in substance.
// Today this only arises for ADRs, since a rule's or intent section's Key is
// derived from its entire payload — identical Key already implies identical
// content, so there is nothing left to disagree about. An ADR's Key is
// derived from its Question alone, so two repositories answering the same
// question differently produce a Conflict here rather than a coin-flip pick.
//
// Entries holds one Entry per distinct answer, each already scoped to only
// the sources that assert it — a human reads both (or all) sides intact,
// nothing is synthesized.
type Conflict struct {
	Kind    EntryKind
	Key     string
	Entries []Entry
}

// Merge condenses N artifacts into one: entries are grouped by identity
// (Kind, Key), their provenance is unioned, and the count of distinct
// sources becomes the recurrence rank — the signal this package uses instead
// of ever rewriting a sentence to look "more generic". The result is sorted
// by that rank (see sortEntries) so a human curates a ranked list top-down.
//
// Merge is associative in effect and idempotent: Merge(a) reproduces a
// modulo provenance normalization (sorted, deduped), and Merge(a, a) unions
// a's own provenance with itself rather than double-counting it as two
// sources — an artifact that is itself the product of an earlier Merge (and
// so already carries multi-source entries) folds in correctly, which is
// what lets Merge be called pairwise or all-at-once interchangeably.
func Merge(as ...Artifact) (Artifact, []Conflict, error) {
	type bucketKey struct {
		kind EntryKind
		key  string
	}
	var order []bucketKey
	groups := map[bucketKey][]Entry{}
	for _, a := range as {
		for _, e := range a.Entries {
			bk := bucketKey{e.Kind, e.Key}
			if _, seen := groups[bk]; !seen {
				order = append(order, bk)
			}
			groups[bk] = append(groups[bk], e)
		}
	}

	var merged []Entry
	var conflicts []Conflict
	for _, bk := range order {
		values := groupBySubstance(groups[bk])
		if len(values) == 1 {
			merged = append(merged, values[0])
			continue
		}
		conflicts = append(conflicts, Conflict{Kind: bk.kind, Key: bk.key, Entries: values})
	}

	sortEntries(merged)
	sort.SliceStable(conflicts, func(i, j int) bool {
		if conflicts[i].Kind != conflicts[j].Kind {
			return conflicts[i].Kind < conflicts[j].Kind
		}
		return conflicts[i].Key < conflicts[j].Key
	})

	sources := unionSourceLabels(as...)
	return Artifact{Sources: sources, Entries: merged}, conflicts, nil
}

// groupBySubstance folds a set of same-identity entries into one Entry per
// distinct payload, unioning provenance (and thus recomputing Count) for
// entries that agree. A single-element result means every input entry
// agreed; more than one means Merge must report a Conflict instead of
// picking a winner.
func groupBySubstance(entries []Entry) []Entry {
	var buckets []Entry
	for _, e := range entries {
		placed := false
		for i := range buckets {
			if substanceEqual(buckets[i], e) {
				buckets[i].Sources = unionProvenance(buckets[i].Sources, e.Sources)
				if buckets[i].Rationale == "" {
					buckets[i].Rationale = e.Rationale
				}
				placed = true
				break
			}
		}
		if !placed {
			cp := e
			cp.Sources = unionProvenance(nil, e.Sources)
			buckets = append(buckets, cp)
		}
	}
	for i := range buckets {
		buckets[i].Count = distinctSourceCount(buckets[i].Sources)
	}
	// Deterministic order among distinct answers to the same identity
	// (matters for Conflict.Entries): most-asserted answer first, then a
	// content-derived tiebreak so two runs never reshuffle it.
	sort.SliceStable(buckets, func(i, j int) bool {
		if buckets[i].Count != buckets[j].Count {
			return buckets[i].Count > buckets[j].Count
		}
		return substanceHash(buckets[i]) < substanceHash(buckets[j])
	})
	return buckets
}

// substanceEqual reports whether two same-identity (same Kind, same Key)
// entries carry the same content. For rules and intent sections the Key is
// already derived from the entire payload, so any two entries reaching this
// function with matching Key are trivially equal in substance — this branch
// exists mainly to keep the check honest (and correct in the astronomically
// unlikely event of a hash collision) rather than to do real work. For ADRs,
// the Key covers only the Question, so this is where a genuine disagreement
// is detected: same question, different Context/Decision/Consequences/
// Status.
func substanceEqual(a, b Entry) bool {
	switch a.Kind {
	case KindRule:
		return drift.NormHash([]byte(a.Text)) == drift.NormHash([]byte(b.Text))
	case KindIntent:
		return drift.NormHash([]byte(a.Prose)) == drift.NormHash([]byte(b.Prose))
	case KindADR:
		return drift.NormHash([]byte(a.Context)) == drift.NormHash([]byte(b.Context)) &&
			drift.NormHash([]byte(a.Decision)) == drift.NormHash([]byte(b.Decision)) &&
			drift.NormHash([]byte(a.Consequences)) == drift.NormHash([]byte(b.Consequences)) &&
			a.Status == b.Status
	default:
		return false
	}
}

// substanceHash gives groupBySubstance a deterministic, content-derived
// tiebreak among distinct answers that does not depend on input/source
// order.
func substanceHash(e Entry) string {
	return drift.NormHash([]byte(e.Text + "\x00" + e.Context + "\x00" + e.Decision + "\x00" + e.Consequences + "\x00" + e.Status + "\x00" + e.Prose))
}

// unionProvenance merges two provenance lists, deduped by (Source, Dir) and
// sorted — the mechanism that keeps Merge(a, a) from double-counting a's own
// sources.
func unionProvenance(a, b []Provenance) []Provenance {
	seen := make(map[Provenance]bool, len(a)+len(b))
	var out []Provenance
	for _, p := range a {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	for _, p := range b {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	sortProvenance(out)
	return out
}

// distinctSourceCount counts unique Source labels (ignoring Dir) — the
// recurrence rank.
func distinctSourceCount(ps []Provenance) int {
	set := make(map[string]bool, len(ps))
	for _, p := range ps {
		set[p.Source] = true
	}
	return len(set)
}

// unionSourceLabels is the top-level Artifact.Sources of a merge: every
// repository label named by any input artifact, deduped and sorted.
func unionSourceLabels(as ...Artifact) []string {
	var all []string
	for _, a := range as {
		all = append(all, a.Sources...)
	}
	return dedupSorted(all)
}
