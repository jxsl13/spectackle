package item

import (
	"os"
	"strings"
	"testing"

	"github.com/jxsl13/spectackle/internal/workspace"
)

func ws(t *testing.T) workspace.Root {
	t.Helper()
	root := workspace.Root{Dir: t.TempDir()}
	if err := root.EnsureScaffold(""); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestUpsertLoadRoundtrip(t *testing.T) {
	root := ws(t)
	in := Item{
		ID: "P-0001", Kind: "proposal", State: StateDraft, Title: "strided access",
		Dir: "", Parent: "", Created: "2026-07-24",
		Targets: []string{"go:a.F", "gpu/kern.cu"},
		Rules:   []string{"CUDA-KRN-001"},
		Body:    "Line one.\n\nLine two with detail.",
	}
	if err := Upsert(root, in); err != nil {
		t.Fatal(err)
	}
	items, err := LoadWork(root.WorkPath(""), "")
	if err != nil || len(items) != 1 {
		t.Fatalf("LoadWork = %+v, %v", items, err)
	}
	got := items[0]
	if got.ID != in.ID || got.Kind != in.Kind || got.State != in.State ||
		got.Title != in.Title || got.Created != in.Created ||
		len(got.Targets) != 2 || got.Targets[1] != "gpu/kern.cu" ||
		len(got.Rules) != 1 || got.Body != in.Body {
		t.Fatalf("roundtrip mismatch:\n in=%+v\nout=%+v", in, got)
	}
	// frontmatter carries the schema stamp
	raw, _ := os.ReadFile(root.WorkPath(""))
	if !strings.Contains(string(raw), "schema: "+workspace.SchemaStamp) {
		t.Fatalf("work.md missing schema stamp:\n%s", raw)
	}

	// upsert replaces in place, no duplicate blocks
	in.State = StateActive
	if err := Upsert(root, in); err != nil {
		t.Fatal(err)
	}
	items, _ = LoadWork(root.WorkPath(""), "")
	if len(items) != 1 || items[0].State != StateActive {
		t.Fatalf("upsert replace failed: %+v", items)
	}
}

func TestRemoveAndLoadAll(t *testing.T) {
	root := ws(t)
	a := Item{ID: "T-0001", Kind: "task", State: StateDraft, Title: "a", Dir: ""}
	b := Item{ID: "T-0002", Kind: "task", State: StateDraft, Title: "b", Dir: "gpu"}
	if err := Upsert(root, a); err != nil {
		t.Fatal(err)
	}
	if err := Upsert(root, b); err != nil {
		t.Fatal(err)
	}
	all, err := LoadAll(root)
	if err != nil || len(all) != 2 {
		t.Fatalf("LoadAll = %+v, %v", all, err)
	}
	if it, ok, _ := Get(root, "T-0002"); !ok || it.Dir != "gpu" {
		t.Fatalf("Get = %+v %v", it, ok)
	}
	if err := Remove(root, a); err != nil {
		t.Fatal(err)
	}
	all, _ = LoadAll(root)
	if len(all) != 1 || all[0].ID != "T-0002" {
		t.Fatalf("Remove left %+v", all)
	}
}

func TestUpsertLoadRoundtripFeedbackFields(t *testing.T) {
	root := ws(t)
	in := Item{
		ID: "T-0001", Kind: "task", State: StateActive, Title: "feedback loop",
		Created: "2026-07-24", Goal: "go test ./...",
		Rounds: 2, Grilled: "needs more tests", Needs: []string{"D-0001", "D-0002"}, Override: true,
	}
	if err := Upsert(root, in); err != nil {
		t.Fatal(err)
	}
	items, err := LoadWork(root.WorkPath(""), "")
	if err != nil || len(items) != 1 {
		t.Fatalf("LoadWork = %+v, %v", items, err)
	}
	got := items[0]
	if got.Rounds != 2 || got.Grilled != "needs more tests" ||
		len(got.Needs) != 2 || got.Needs[0] != "D-0001" || got.Needs[1] != "D-0002" || !got.Override {
		t.Fatalf("feedback fields roundtrip mismatch: %+v", got)
	}

	// blocked is a valid state and survives roundtrip like any other
	in.State = StateBlocked
	if err := Upsert(root, in); err != nil {
		t.Fatal(err)
	}
	items, _ = LoadWork(root.WorkPath(""), "")
	if len(items) != 1 || items[0].State != StateBlocked {
		t.Fatalf("blocked state roundtrip failed: %+v", items)
	}

	// zero-value feedback fields do not pollute the output
	plain := Item{ID: "T-0002", Kind: "task", State: StateDraft, Title: "plain"}
	if err := Upsert(root, plain); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(root.WorkPath(""))
	if strings.Contains(string(raw), "T-0002") {
		idx := strings.Index(string(raw), "## T-0002")
		block := string(raw)[idx:]
		if strings.Contains(block, "rounds:") || strings.Contains(block, "grilled:") ||
			strings.Contains(block, "needs:") || strings.Contains(block, "override:") {
			t.Fatalf("zero-value feedback fields written for plain item:\n%s", block)
		}
	}
}

func TestUpsertLoadRoundtripADRFields(t *testing.T) {
	root := ws(t)
	in := Item{
		ID: "ADR-0001", Kind: "adr", State: StateDraft, Title: "record the context pattern",
		Created:      "2026-07-24",
		Context:      "We need a consistent way to record architectural forces and constraints across items.",
		Decision:     "Adopt first-class ADR fields that mirror the existing header mechanism.",
		Consequences: "Future ADR items are structured instead of prose; older items keep working unchanged.",
		Status:       "accepted",
	}
	if err := Upsert(root, in); err != nil {
		t.Fatal(err)
	}
	items, err := LoadWork(root.WorkPath(""), "")
	if err != nil || len(items) != 1 {
		t.Fatalf("LoadWork = %+v, %v", items, err)
	}
	got := items[0]
	if got.Context != in.Context || got.Decision != in.Decision ||
		got.Consequences != in.Consequences || got.Status != in.Status {
		t.Fatalf("ADR field roundtrip mismatch:\n in=%+v\nout=%+v", in, got)
	}
}

func TestUpsertLoadNoStrayADRKeys(t *testing.T) {
	root := ws(t)
	plain := Item{ID: "P-0001", Kind: "proposal", State: StateDraft, Title: "plain proposal", Created: "2026-07-24"}
	task := Item{ID: "T-0001", Kind: "task", State: StateDraft, Title: "plain task", Created: "2026-07-24"}
	if err := Upsert(root, plain); err != nil {
		t.Fatal(err)
	}
	if err := Upsert(root, task); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(root.WorkPath(""))
	for _, key := range []string{"context:", "decision:", "consequences:", "status:"} {
		if strings.Contains(string(raw), key) {
			t.Fatalf("stray ADR key %q written for non-adr items:\n%s", key, raw)
		}
	}
	items, err := LoadWork(root.WorkPath(""), "")
	if err != nil || len(items) != 2 {
		t.Fatalf("LoadWork = %+v, %v", items, err)
	}
	for _, it := range items {
		if it.Context != "" || it.Decision != "" || it.Consequences != "" || it.Status != "" {
			t.Fatalf("non-adr item unexpectedly carries ADR fields: %+v", it)
		}
	}
}

func TestUpsertLoadRoundtripRefs(t *testing.T) {
	root := ws(t)
	in := Item{
		ID: "R-0003", Kind: "research", State: StateDone, Title: "strided vs coalesced",
		Created: "2026-07-24",
		Refs:    []string{"R-0001", "R-0002", "P-0001"},
	}
	if err := Upsert(root, in); err != nil {
		t.Fatal(err)
	}
	items, err := LoadWork(root.WorkPath(""), "")
	if err != nil || len(items) != 1 {
		t.Fatalf("LoadWork = %+v, %v", items, err)
	}
	got := items[0]
	if len(got.Refs) != 3 || got.Refs[0] != "R-0001" || got.Refs[1] != "R-0002" || got.Refs[2] != "P-0001" {
		t.Fatalf("Refs roundtrip order mismatch: %+v", got.Refs)
	}
	if got.ID != in.ID || got.Kind != in.Kind || got.State != in.State || got.Title != in.Title || got.Created != in.Created {
		t.Fatalf("roundtrip mismatch:\n in=%+v\nout=%+v", in, got)
	}
}

func TestRefsEmptyRendersByteIdentical(t *testing.T) {
	rootA := ws(t)
	rootB := ws(t)
	withoutRefsField := Item{ID: "T-0001", Kind: "task", State: StateDraft, Title: "no refs field at all", Created: "2026-07-24"}
	withEmptyRefs := Item{ID: "T-0001", Kind: "task", State: StateDraft, Title: "no refs field at all", Created: "2026-07-24", Refs: []string{}}
	if err := Upsert(rootA, withoutRefsField); err != nil {
		t.Fatal(err)
	}
	if err := Upsert(rootB, withEmptyRefs); err != nil {
		t.Fatal(err)
	}
	rawA, err := os.ReadFile(rootA.WorkPath(""))
	if err != nil {
		t.Fatal(err)
	}
	rawB, err := os.ReadFile(rootB.WorkPath(""))
	if err != nil {
		t.Fatal(err)
	}
	if string(rawA) != string(rawB) {
		t.Fatalf("empty Refs is not byte-identical to no Refs:\nA=%q\nB=%q", rawA, rawB)
	}
	if strings.Contains(string(rawA), "refs:") {
		t.Fatalf("refs: key written for item with no refs:\n%s", rawA)
	}
}

func TestLoadWorkPreExistingFileNoRefsLine(t *testing.T) {
	root := ws(t)
	// Hand-write a work.md as it would have looked before Refs existed:
	// no "refs:" header line anywhere.
	content := "---\nschema: " + workspace.SchemaStamp + "\n---\n\n" +
		"## T-0001 legacy item\n" +
		"kind: task\n" +
		"state: draft\n" +
		"created: 2026-01-01\n" +
		"parent: P-0001\n"
	if err := os.WriteFile(root.WorkPath(""), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	items, err := LoadWork(root.WorkPath(""), "")
	if err != nil {
		t.Fatalf("LoadWork errored on pre-Refs file: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("LoadWork = %+v", items)
	}
	if items[0].Refs != nil {
		t.Fatalf("expected nil/empty Refs for pre-Refs file, got %+v", items[0].Refs)
	}
}

func TestRefsDuplicatesCollapseOnWrite(t *testing.T) {
	root := ws(t)
	in := Item{
		ID: "R-0001", Kind: "research", State: StateDraft, Title: "dedup check",
		Created: "2026-07-24",
		Refs:    []string{"R-0002", "P-0001", "R-0002", "P-0001", "R-0003"},
	}
	if err := Upsert(root, in); err != nil {
		t.Fatal(err)
	}
	items, err := LoadWork(root.WorkPath(""), "")
	if err != nil || len(items) != 1 {
		t.Fatalf("LoadWork = %+v, %v", items, err)
	}
	want := []string{"R-0002", "P-0001", "R-0003"}
	got := items[0].Refs
	if len(got) != len(want) {
		t.Fatalf("Refs dedup = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Refs dedup order = %+v, want %+v", got, want)
		}
	}
}

func TestUnknownRefs(t *testing.T) {
	known := map[string]bool{"R-0001": true, "R-0002": true, "P-0001": true}

	// all known: empty result
	if got := UnknownRefs("T-0001", []string{"R-0001", "P-0001"}, known); len(got) != 0 {
		t.Fatalf("UnknownRefs = %+v, want empty", got)
	}

	// missing IDs reported in input order
	got := UnknownRefs("T-0001", []string{"R-0001", "R-0099", "R-0002", "P-9999"}, known)
	want := []string{"R-0099", "P-9999"}
	if len(got) != len(want) {
		t.Fatalf("UnknownRefs = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("UnknownRefs = %+v, want %+v", got, want)
		}
	}

	// malformed ref and self-reference are both reported
	got = UnknownRefs("T-0001", []string{"not-an-id", "T-0001", "R-0001"}, map[string]bool{"T-0001": true, "R-0001": true})
	want = []string{"not-an-id", "T-0001"}
	if len(got) != len(want) {
		t.Fatalf("UnknownRefs malformed/self = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("UnknownRefs malformed/self = %+v, want %+v", got, want)
		}
	}
}

func TestADRKindAndIDs(t *testing.T) {
	if !ValidKind("adr") {
		t.Fatal("adr not a valid kind")
	}
	if Letter("adr") != "ADR" {
		t.Fatalf("Letter(adr) = %q, want ADR", Letter("adr"))
	}
	if !IDRe.MatchString("ADR-0001") {
		t.Fatal("IDRe rejects ADR- ids")
	}
	if NextID("adr", 0) != "ADR-0001" {
		t.Fatalf("NextID(adr, 0) = %s", NextID("adr", 0))
	}
	if Num("ADR-0007") != 7 {
		t.Fatalf("Num(ADR-0007) = %d, want 7", Num("ADR-0007"))
	}
	// legacy: D was the ID letter for adr items before the decision->adr
	// rename; existing D-xxxx items in .spectackle files are not migrated,
	// so IDRe and Num must keep reading them.
	if !IDRe.MatchString("D-0001") {
		t.Fatal("IDRe must still tolerate legacy D- ids")
	}
	if Num("D-0007") != 7 {
		t.Fatalf("Num(D-0007) = %d, want 7", Num("D-0007"))
	}
}

func TestIDHelpers(t *testing.T) {
	if !ValidKind("proposal") || ValidKind("epic") {
		t.Fatal("ValidKind broken")
	}
	if NextID("task", 41) != "T-0042" {
		t.Fatalf("NextID = %s", NextID("task", 41))
	}
	if Num("P-0007") != 7 || Num("P-broken") != 0 {
		t.Fatal("Num broken")
	}
	if !IDRe.MatchString("B-0003") || IDRe.MatchString("X-0003") || IDRe.MatchString("P-3") {
		t.Fatal("IDRe broken")
	}
	if got := Record(Item{ID: "P-0001", Kind: "proposal", State: "draft", Dir: "", Title: "t"}); got != "i P-0001 proposal draft . t" {
		t.Fatalf("Record = %q", got)
	}
}
