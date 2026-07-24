package journal

import (
	"reflect"
	"sort"
	"testing"
	"time"
)

func mkReject(eid string, t time.Time, id, note string) Event {
	return Event{Eid: eid, T: t, Ev: EvReject, ID: id, Note: note}
}

// 1. Two rejections whose notes state the same lesson in different words
// cluster together above the threshold; two unrelated ones do not.
func TestClusterRedundant_SimilarNotesCluster(t *testing.T) {
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name       string
		events     []Event
		wantEid1   string // if non-empty, expect this pair clustered together
		wantEid2   string
		wantSingle []string // eids expected as singleton clusters
	}{
		{
			name: "paraphrased lesson clusters",
			events: []Event{
				mkReject("e1", base, "P-0001", "vram cache breaks scheduling under load"),
				mkReject("e2", base.Add(time.Minute), "P-0002", "vram cache breaks scheduling under load again"),
			},
			wantEid1: "e1",
			wantEid2: "e2",
		},
		{
			name: "unrelated notes do not cluster",
			events: []Event{
				mkReject("e1", base, "P-0001", "vram cache breaks scheduling under load"),
				mkReject("e2", base.Add(time.Minute), "P-0002", "completely different concern about disk latency"),
			},
			wantSingle: []string{"e1", "e2"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ClusterRedundant(tc.events)
			if tc.wantEid1 != "" {
				if len(got) != 1 {
					t.Fatalf("expected 1 cluster, got %d: %+v", len(got), got)
				}
				c := got[0]
				members := append([]string{c.Canonical}, c.Superseded...)
				sort.Strings(members)
				want := []string{tc.wantEid1, tc.wantEid2}
				sort.Strings(want)
				if !reflect.DeepEqual(members, want) {
					t.Fatalf("cluster members = %v, want %v", members, want)
				}
			}
			if tc.wantSingle != nil {
				if len(got) != len(tc.wantSingle) {
					t.Fatalf("expected %d singleton clusters, got %d: %+v", len(tc.wantSingle), len(got), got)
				}
				for _, c := range got {
					if len(c.Superseded) != 0 {
						t.Fatalf("expected singleton cluster, got Superseded=%v for canonical %s", c.Superseded, c.Canonical)
					}
				}
			}
		})
	}
}

// 2. Determinism: the same input twice yields byte-identical cluster
// output including order.
func TestClusterRedundant_Deterministic(t *testing.T) {
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	events := []Event{
		mkReject("e3", base.Add(2*time.Minute), "P-0003", "cache eviction thrashes under concurrent writers"),
		mkReject("e1", base, "P-0001", "vram cache breaks scheduling under load"),
		mkReject("e2", base.Add(time.Minute), "P-0002", "vram cache breaks scheduling under load again"),
		mkReject("e4", base.Add(3*time.Minute), "P-0004", "cache eviction thrashes with concurrent writers too"),
	}
	got1 := ClusterRedundant(events)
	got2 := ClusterRedundant(events)
	if !reflect.DeepEqual(got1, got2) {
		t.Fatalf("non-deterministic: %+v vs %+v", got1, got2)
	}
	// Also stable under input reordering.
	reordered := []Event{events[3], events[1], events[0], events[2]}
	got3 := ClusterRedundant(reordered)
	if !reflect.DeepEqual(got1, got3) {
		t.Fatalf("order-dependent: %+v vs %+v", got1, got3)
	}
}

// 3. Clustering ignores item identity: two different item ids, same note,
// still cluster.
func TestClusterRedundant_IgnoresItemIdentity(t *testing.T) {
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	events := []Event{
		mkReject("e1", base, "P-0001", "duplicate rejection reason stated identically"),
		mkReject("e2", base.Add(time.Minute), "T-9999", "duplicate rejection reason stated identically"),
	}
	// Different item IDs and different item kinds (P vs T) should not
	// prevent clustering, since only Note is compared.
	events[0].K = "proposal"
	events[1].K = "task"
	events[0].Body = "full body of item one"
	events[1].Body = "an entirely unrelated body of item two"

	got := ClusterRedundant(events)
	if len(got) != 1 {
		t.Fatalf("expected 1 cluster despite differing item identity, got %d: %+v", len(got), got)
	}
	if len(got[0].Superseded) != 1 {
		t.Fatalf("expected 1 superseded member, got %+v", got[0])
	}
}

// 4. Canonical selection follows the documented rule (earliest-wins by T,
// tie-broken by Eid), proven on a cluster of three.
func TestClusterRedundant_CanonicalIsEarliest(t *testing.T) {
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	note := "gpu scheduler starves low priority jobs"
	events := []Event{
		mkReject("zzz", base.Add(10*time.Minute), "P-0003", note+" yet again"),
		mkReject("aaa", base, "P-0001", note),
		mkReject("mmm", base.Add(5*time.Minute), "P-0002", note+" once more"),
	}
	got := ClusterRedundant(events)
	if len(got) != 1 {
		t.Fatalf("expected 1 cluster, got %d: %+v", len(got), got)
	}
	if got[0].Canonical != "aaa" {
		t.Fatalf("canonical = %q, want %q (earliest T)", got[0].Canonical, "aaa")
	}
	want := []string{"mmm", "zzz"}
	if !reflect.DeepEqual(got[0].Superseded, want) {
		t.Fatalf("superseded = %v, want %v", got[0].Superseded, want)
	}

	// Tie-break by Eid when timestamps are equal.
	tied := []Event{
		mkReject("b-eid", base, "P-0001", note),
		mkReject("a-eid", base, "P-0002", note+" restated"),
		mkReject("c-eid", base, "P-0003", note+" restated again"),
	}
	gotTied := ClusterRedundant(tied)
	if len(gotTied) != 1 || gotTied[0].Canonical != "a-eid" {
		t.Fatalf("tie-break canonical = %+v, want a-eid", gotTied)
	}
}

// 5. Retargeting rewrites citations pointing at a superseded id to the
// canonical one, leaves others alone, and reports every rewrite with its
// original target.
func TestRetargetCitations(t *testing.T) {
	supersessions := []Supersession{
		NewSupersession(RedundancyCluster{Canonical: "canon1", Superseded: []string{"dup1", "dup2"}}),
	}
	citations := map[string][]string{
		"itemA": {"dup1", "unrelated"},
		"itemB": {"dup2", "canon1"},
		"itemC": {"unrelated", "also-unrelated"},
	}
	// Keep an original copy to assert non-mutation.
	origA := append([]string(nil), citations["itemA"]...)

	out, records := RetargetCitations(citations, supersessions)

	wantOut := map[string][]string{
		"itemA": {"canon1", "unrelated"},
		"itemB": {"canon1", "canon1"},
		"itemC": {"unrelated", "also-unrelated"},
	}
	if !reflect.DeepEqual(out, wantOut) {
		t.Fatalf("retargeted citations = %+v, want %+v", out, wantOut)
	}
	if !reflect.DeepEqual(citations["itemA"], origA) {
		t.Fatalf("input citations mutated: %v vs original %v", citations["itemA"], origA)
	}

	wantRecords := []RewriteRecord{
		{ItemID: "itemA", Original: "dup1", Rewritten: "canon1"},
		{ItemID: "itemB", Original: "dup2", Rewritten: "canon1"},
	}
	if !reflect.DeepEqual(records, wantRecords) {
		t.Fatalf("records = %+v, want %+v", records, wantRecords)
	}
	// Original target must be recoverable from the record.
	for _, r := range records {
		if r.Original == "" || r.Original == r.Rewritten {
			t.Fatalf("record does not preserve a recoverable original target: %+v", r)
		}
	}
}

// 6. Invariant guard: nothing in this API can produce a shortened event
// slice. Every input reject event's eid must still be present in
// whatever ClusterRedundant returns — as the Canonical of its cluster or
// in that cluster's Superseded list. This is the test that fails if
// someone later adds a deletion path.
func TestClusterRedundant_NeverDropsAnEvent(t *testing.T) {
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	events := []Event{
		mkReject("e1", base, "P-0001", "vram cache breaks scheduling under load"),
		mkReject("e2", base.Add(time.Minute), "P-0002", "vram cache breaks scheduling under load again"),
		mkReject("e3", base.Add(2*time.Minute), "P-0003", "totally unrelated disk latency concern"),
		mkReject("e4", base.Add(3*time.Minute), "P-0004", "cache eviction thrashes under concurrent writers"),
		mkReject("e5", base.Add(4*time.Minute), "P-0005", "cache eviction thrashes with concurrent writers too"),
		mkReject("e6", base.Add(5*time.Minute), "P-0006", ""), // empty note: never clusters, still must survive
	}
	wantIDs := make(map[string]bool, len(events))
	for _, e := range events {
		wantIDs[e.Eid] = true
	}

	clusters := ClusterRedundant(events)

	gotIDs := make(map[string]bool)
	for _, c := range clusters {
		if gotIDs[c.Canonical] {
			t.Fatalf("eid %q referenced by more than one cluster", c.Canonical)
		}
		gotIDs[c.Canonical] = true
		for _, s := range c.Superseded {
			if gotIDs[s] {
				t.Fatalf("eid %q referenced by more than one cluster", s)
			}
			gotIDs[s] = true
		}
	}

	if len(gotIDs) != len(wantIDs) {
		t.Fatalf("event count changed: input %d distinct eids, output references %d", len(wantIDs), len(gotIDs))
	}
	for id := range wantIDs {
		if !gotIDs[id] {
			t.Fatalf("input event %q not present anywhere in ClusterRedundant's output — event was dropped", id)
		}
	}
	for id := range gotIDs {
		if !wantIDs[id] {
			t.Fatalf("output references eid %q that was never in the input — event was fabricated", id)
		}
	}

	// Also: a Supersession built from any of these clusters keeps every
	// member's id reachable, either as Canonical or in Superseded/Retarget.
	for _, c := range clusters {
		s := NewSupersession(c)
		if s.Canonical != c.Canonical {
			t.Fatalf("Supersession dropped canonical: %+v", s)
		}
		if len(s.Retarget) != len(c.Superseded) {
			t.Fatalf("Supersession dropped superseded members: cluster=%+v supersession=%+v", c, s)
		}
		for _, id := range c.Superseded {
			if s.Retarget[id] != s.Canonical {
				t.Fatalf("Supersession lost retarget for %q: %+v", id, s)
			}
		}
	}
}
