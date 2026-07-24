package journal

import "sort"

// This file is pure analysis over already-read events: it clusters
// redundant rejections and represents supersession between them. It does
// not append, rewrite, or delete anything — the package's append-only
// invariant (see journal.go's doc comment) is untouched by design. Nothing
// here can shrink an event slice; ClusterRedundant places every reject
// event it is given into exactly one cluster (singletons included), so the
// full input eid set is always recoverable from the output. A future task
// wires a dry-run report and an apply path on top of this; this file only
// supplies the analysis and the value types.

// redundancyThreshold is the sentence-token Jaccard cutoff above which two
// reject notes are considered the same learning. This is the same measure
// and threshold MCP-005 already applies to mergeable rule pairs
// (internal/mcpserver/tools.go: mergeCandidates/ruleTokens/jaccard). journal
// cannot import mcpserver (mcpserver imports journal, so that would be a
// cycle), so noteTokens/jaccardSim below are a byte-for-byte reimplementation
// of ruleTokens/jaccard — same stopword list, same tokenization, same
// intersection-over-union formula. Do not retune this threshold here without
// retuning it there; the two are meant to stay one notion of similarity.
const redundancyThreshold = 0.6

// noteTokens normalizes a reject note for similarity exactly like
// mcpserver's ruleTokens normalizes a rule sentence: lowercase, alnum runs
// only, a small set of glue words dropped.
func noteTokens(text string) map[string]bool {
	stop := map[string]bool{
		"when": true, "while": true, "if": true, "then": true, "where": true,
		"shall": true, "the": true, "a": true, "an": true, "to": true,
		"of": true, "and": true, "or": true, "as": true, "is": true,
	}
	toks := map[string]bool{}
	var cur []rune
	flush := func() {
		if len(cur) > 0 {
			if t := string(cur); !stop[t] {
				toks[t] = true
			}
			cur = cur[:0]
		}
	}
	for _, r := range text {
		lr := r
		if lr >= 'A' && lr <= 'Z' {
			lr = lr - 'A' + 'a'
		}
		if (lr >= 'a' && lr <= 'z') || (lr >= '0' && lr <= '9') {
			cur = append(cur, lr)
		} else {
			flush()
		}
	}
	flush()
	return toks
}

// jaccardSim mirrors mcpserver's jaccard exactly: intersection over union of
// two token sets, 0 if either side is empty.
func jaccardSim(a, b map[string]bool) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	inter := 0
	for t := range a {
		if b[t] {
			inter++
		}
	}
	return float64(inter) / float64(len(a)+len(b)-inter)
}

// RedundancyCluster groups reject events whose notes were judged to express
// the same learning. Canonical names the event chosen to represent the
// cluster (see ClusterRedundant's doc comment for the selection rule);
// Superseded lists every other member, sorted. A cluster of one (no
// duplicates found) has an empty Superseded — it is still returned, so that
// every input event is accounted for somewhere in the output.
type RedundancyCluster struct {
	Canonical  string
	Superseded []string
}

// ClusterRedundant groups reject events by note similarity. Only events with
// Ev == EvReject and a non-empty Eid participate; anything else in the slice
// is ignored (this function's contract is a slice of reject events — filter
// before calling if the slice is mixed). Similarity is compared on Note
// alone: Body, Ti, ID and every other item-identifying field are never
// looked at, because two different items rejected for the same reason are
// exactly the case this exists to catch.
//
// Two events cluster together when jaccardSim(noteTokens(a.Note),
// noteTokens(b.Note)) >= redundancyThreshold; clustering is transitive
// (connected components over that edge relation), so a chain of paraphrases
// can end up in one cluster even if the two ends aren't similar enough to
// each other directly.
//
// Canonical selection: earliest-wins by event timestamp T, tied-broken by
// Eid. The first statement of a lesson is the one the later, redundant
// rejections are restating — so the earliest member is the most natural
// thing to point citations at, and it keeps selection independent of
// clustering order or Eid randomness (Eid is minted per-append, not
// meaningful as a tiebreaker on its own merits, but it is stable and total,
// which is what determinism needs when two events land in the same second).
//
// Deterministic: for the same input slice (regardless of input order), the
// returned slice, each cluster's Canonical, and each cluster's Superseded
// order are always identical. Clusters are sorted by Canonical; each
// cluster's Superseded is sorted lexicographically.
func ClusterRedundant(events []Event) []RedundancyCluster {
	rejects := make([]Event, 0, len(events))
	for _, e := range events {
		if e.Ev == EvReject && e.Eid != "" {
			rejects = append(rejects, e)
		}
	}
	if len(rejects) == 0 {
		return nil
	}
	sort.Slice(rejects, func(i, j int) bool { return rejects[i].Eid < rejects[j].Eid })

	tokens := make(map[string]map[string]bool, len(rejects))
	for _, e := range rejects {
		tokens[e.Eid] = noteTokens(e.Note)
	}

	// Union-find over the similarity edge relation. The chosen root is an
	// implementation detail of grouping only — canonical selection below is
	// re-derived from timestamps, independent of which eid a component
	// happens to root at.
	parent := make(map[string]string, len(rejects))
	for _, e := range rejects {
		parent[e.Eid] = e.Eid
	}
	var find func(string) string
	find = func(x string) string {
		for parent[x] != x {
			parent[x] = parent[parent[x]]
			x = parent[x]
		}
		return x
	}
	union := func(a, b string) {
		ra, rb := find(a), find(b)
		if ra == rb {
			return
		}
		if rb < ra {
			ra, rb = rb, ra
		}
		parent[rb] = ra
	}
	for i := 0; i < len(rejects); i++ {
		for k := i + 1; k < len(rejects); k++ {
			a, b := rejects[i], rejects[k]
			if jaccardSim(tokens[a.Eid], tokens[b.Eid]) >= redundancyThreshold {
				union(a.Eid, b.Eid)
			}
		}
	}

	byRoot := make(map[string][]Event, len(rejects))
	for _, e := range rejects {
		r := find(e.Eid)
		byRoot[r] = append(byRoot[r], e)
	}

	clusters := make([]RedundancyCluster, 0, len(byRoot))
	for _, members := range byRoot {
		clusters = append(clusters, newRedundancyCluster(members))
	}
	sort.Slice(clusters, func(i, j int) bool { return clusters[i].Canonical < clusters[j].Canonical })
	return clusters
}

// newRedundancyCluster picks the canonical member (earliest T, tie-break
// Eid) and returns the rest, sorted, as Superseded.
func newRedundancyCluster(members []Event) RedundancyCluster {
	sorted := append([]Event(nil), members...)
	sort.Slice(sorted, func(i, j int) bool {
		if !sorted[i].T.Equal(sorted[j].T) {
			return sorted[i].T.Before(sorted[j].T)
		}
		return sorted[i].Eid < sorted[j].Eid
	})
	c := RedundancyCluster{Canonical: sorted[0].Eid}
	if len(sorted) > 1 {
		c.Superseded = make([]string, 0, len(sorted)-1)
		for _, m := range sorted[1:] {
			c.Superseded = append(c.Superseded, m.Eid)
		}
		sort.Strings(c.Superseded)
	}
	return c
}

// EvSupersede is the journal event kind a Supersession marshals to (see
// Supersession.ToEvent). It is deliberately adjacent to EvCompact rather
// than reusing it: EvCompact means "N events folded away", while
// EvSupersede means "N reject notes recognized as one learning, canonical
// eid named, every underlying reject snapshot left exactly where it was".
// Per the package invariant (journal.go's doc comment), reject/archive/
// compact lines are always retained verbatim through compaction; a future
// compaction implementation must retain EvSupersede lines the same way —
// dropping one would silently un-supersede every citation that was
// retargeted because of it, with no way to tell the rewrite ever happened.
const EvSupersede = "supersede"

// Supersession is the representation of one canonical-selection outcome:
// which reject event is canonical, which reject events it supersedes, and
// the id retargeting that implies for anything citing a superseded id.
// Constructing a Supersession does no I/O and touches no journal — it is a
// plain value. The reject snapshots underneath (Body/Tg/Par/Rnd/Gr on the
// superseded events) are not referenced or altered here; they remain
// individually revocable exactly as journal.go's invariant requires.
type Supersession struct {
	Canonical  string
	Superseded []string
	// Retarget maps each superseded eid to Canonical. It is fully derived
	// from Canonical and Superseded (kept alongside them, not instead of
	// them) so callers doing retargeting don't have to rebuild the map
	// themselves.
	Retarget map[string]string
}

// NewSupersession builds a Supersession from a RedundancyCluster. Clusters
// with no Superseded members still produce a valid, empty-mapped
// Supersession (nothing to retarget); callers typically skip those before
// representing anything, but NewSupersession itself does not filter — it is
// a pure conversion.
func NewSupersession(c RedundancyCluster) Supersession {
	s := Supersession{
		Canonical:  c.Canonical,
		Superseded: append([]string(nil), c.Superseded...),
		Retarget:   make(map[string]string, len(c.Superseded)),
	}
	for _, id := range c.Superseded {
		s.Retarget[id] = c.Canonical
	}
	return s
}

// ToEvent marshals a Supersession into an Event of kind EvSupersede. This is
// representation only — it returns a value, it does not call Append and
// does not touch any journal file. ID carries the canonical eid, Tg carries
// the sorted superseded eids, N records how many were folded in. A later
// task decides whether and when to persist it.
func (s Supersession) ToEvent() Event {
	tg := append([]string(nil), s.Superseded...)
	sort.Strings(tg)
	return Event{
		Ev: EvSupersede,
		ID: s.Canonical,
		Tg: tg,
		N:  len(tg),
	}
}

// RewriteRecord documents one citation rewrite performed by
// RetargetCitations: which citing item was touched, what id it originally
// cited, and what id it now cites. Original is kept so the rewrite is
// reversible/auditable even after the citation itself has been overwritten.
type RewriteRecord struct {
	ItemID    string
	Original  string
	Rewritten string
}

// RetargetCitations rewrites a citation graph according to a set of
// Supersessions, purely: citations is never mutated, a new map (and new
// slices) are returned. citations maps a citing item id to the ids it
// cites. For every cited id that some Supersession lists as Superseded, the
// returned graph points at that Supersession's Canonical instead; every
// other citation is left byte-identical to the input. Every rewrite that was
// performed is reported in the returned []RewriteRecord, in deterministic
// (ItemID, then Original) order — a rewrite nobody can audit is exactly what
// this guards against, and Original in each record is enough to recover
// what the citation pointed at before.
//
// If the same id is listed as Superseded by more than one Supersession
// (which a well-formed set of clusters should never produce, since
// ClusterRedundant partitions events), the Supersession with the
// lexicographically smallest Canonical wins, deterministically.
func RetargetCitations(citations map[string][]string, supersessions []Supersession) (map[string][]string, []RewriteRecord) {
	sorted := append([]Supersession(nil), supersessions...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Canonical < sorted[j].Canonical })

	lookup := make(map[string]string)
	for _, s := range sorted {
		ids := append([]string(nil), s.Superseded...)
		sort.Strings(ids)
		for _, id := range ids {
			if _, exists := lookup[id]; !exists {
				lookup[id] = s.Canonical
			}
		}
	}

	itemIDs := make([]string, 0, len(citations))
	for id := range citations {
		itemIDs = append(itemIDs, id)
	}
	sort.Strings(itemIDs)

	out := make(map[string][]string, len(citations))
	var records []RewriteRecord
	for _, itemID := range itemIDs {
		refs := citations[itemID]
		rewritten := make([]string, len(refs))
		for i, ref := range refs {
			if canon, ok := lookup[ref]; ok && canon != ref {
				rewritten[i] = canon
				records = append(records, RewriteRecord{ItemID: itemID, Original: ref, Rewritten: canon})
			} else {
				rewritten[i] = ref
			}
		}
		out[itemID] = rewritten
	}
	sort.Slice(records, func(i, j int) bool {
		if records[i].ItemID != records[j].ItemID {
			return records[i].ItemID < records[j].ItemID
		}
		return records[i].Original < records[j].Original
	})
	return out, records
}
