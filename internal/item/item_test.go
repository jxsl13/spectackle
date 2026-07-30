package item

import (
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jxsl13/spectackle/internal/ids"
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
		got.Body != in.Body {
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
		Created: "2026-07-24", Rounds: 2, Grilled: "needs more tests", Needs: []string{"D-0001", "D-0002"}, Override: true,
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
	if id := MintID("adr"); !IDRe.MatchString(id) || !strings.HasPrefix(id, "ADR-") {
		t.Fatalf("MintID(adr) = %q, want an ADR-prefixed record ID", id)
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

// ---------------------------------------------------------------------------
// ADR-0013: the two ID schemes (T-0135)
// ---------------------------------------------------------------------------

// TestIDReAcceptsBothSchemes is the acceptance contract of the ID grammar: a
// legacy sequential ID and an ADR-0013 record ID are equally valid, and
// neither near-miss shape is. Legacy acceptance is not a courtesy — archived
// records exist only as journal tombstones that lifecycle.Tombstone finds by
// exact ID, and the tool boundary screens IDs through IDRe before the lookup,
// so the moment a legacy ID stops matching the archive stops being readable.
func TestIDReAcceptsBothSchemes(t *testing.T) {
	legacy := []string{"P-0001", "T-0042", "B-0003", "R-0007", "ADR-0013", "D-0007"}
	for _, id := range legacy {
		if !IDRe.MatchString(id) {
			t.Errorf("IDRe rejects legacy ID %q — archived history would become unreachable", id)
		}
		if !LegacyIDRe.MatchString(id) {
			t.Errorf("LegacyIDRe does not recognize %q as legacy", id)
		}
		if KindOf(id) == "" {
			t.Errorf("KindOf(%q) = \"\"", id)
		}
	}

	for _, kind := range []string{"proposal", "task", "bug", "research", "adr"} {
		id := MintID(kind)
		if !IDRe.MatchString(id) {
			t.Errorf("IDRe rejects freshly minted %s ID %q", kind, id)
		}
		if LegacyIDRe.MatchString(id) {
			t.Errorf("LegacyIDRe matches record ID %q", id)
		}
	}

	bad := []string{
		"",                              // empty
		"X-0003",                        // unknown kind letter
		"P-3",                           // too few digits
		"P-00001",                       // too many digits
		"p-0001",                        // lowercase prefix
		"T-01KYD04YNJF7QSYR15D4ZXPND",   // one char short of a record ID
		"T-01KYD04YNJF7QSYR15D4ZXPNDNX", // one char long
		"T-I1KYD04YNJF7QSYR15D4ZXPNDN",  // 'I' is excluded from Crockford base32
		"T-01KYD04YNJF7QSYR15D4ZXPNDL",  // 'L' is excluded
		"T-01KYD04YNJF7QSYR15D4ZXPNDO",  // 'O' is excluded
		"T-01KYD04YNJF7QSYR15D4ZXPNDU",  // 'U' is excluded
		"T-91KYD04YNJF7QSYR15D4ZXPNDN",  // first char > '7' overflows 128 bits
		"T-01kyd04ynjf7qsyr15d4zxpndn",  // lowercase tail
		"not-an-id",
		"T-0001 ", // trailing space
	}
	for _, id := range bad {
		if IDRe.MatchString(id) {
			t.Errorf("IDRe accepts malformed ID %q", id)
		}
		if KindOf(id) != "" {
			t.Errorf("KindOf(%q) = %q, want \"\"", id, KindOf(id))
		}
	}
}

// TestIDReAgreesWithIDsPackage pins the regexp to internal/ids rather than to
// a hand-copied alphabet: IDRe's record half must accept exactly the strings
// ids.ValidRecordID accepts. A divergence here is the silent kind — IDs would
// mint fine and then be refused at a boundary, or vice versa.
func TestIDReAgreesWithIDsPackage(t *testing.T) {
	// every canonical symbol, in every position that can hold it
	for _, c := range "0123456789ABCDEFGHJKMNPQRSTVWXYZ" {
		tail := "0" + strings.Repeat(string(c), ids.RecordIDLen-1)
		if got, want := IDRe.MatchString("T-"+tail), ids.ValidRecordID(tail); got != want {
			t.Errorf("IDRe(T-%s) = %v, ids.ValidRecordID = %v", tail, got, want)
		}
		lead := string(c) + strings.Repeat("0", ids.RecordIDLen-1)
		if got, want := IDRe.MatchString("T-"+lead), ids.ValidRecordID(lead); got != want {
			t.Errorf("IDRe(T-%s) = %v, ids.ValidRecordID = %v", lead, got, want)
		}
	}
	// 500 real mints, since only these carry the version/variant bits
	for i := 0; i < 500; i++ {
		id := MintID("task")
		tail := strings.TrimPrefix(id, "T-")
		if !ids.ValidRecordID(tail) {
			t.Fatalf("MintID produced a tail ids rejects: %q", id)
		}
		if !IDRe.MatchString(id) {
			t.Fatalf("IDRe rejects minted %q", id)
		}
	}
}

// TestMintIDUniqueAndOrdered: minting is the collision fix, so the tight loop
// is the point — R-0006 reproduced two clones minting the same counter value.
// A UUIDv7 needs no coordination, and because it leads with a millisecond
// timestamp its canonical text also sorts by mint time.
func TestMintIDUniqueAndOrdered(t *testing.T) {
	const n = 5000
	seen := make(map[string]bool, n)
	for i := 0; i < n; i++ {
		id := MintID("task")
		if seen[id] {
			t.Fatalf("MintID collided after %d mints: %q", i, id)
		}
		seen[id] = true
	}

	// unknown kind mints nothing rather than an ID with an empty prefix
	if got := MintID("epic"); got != "" {
		t.Errorf("MintID(epic) = %q, want \"\"", got)
	}
	if got := MintIDAt("epic", time.Now()); got != "" {
		t.Errorf("MintIDAt(epic) = %q, want \"\"", got)
	}

	// distinct milliseconds sort by time, in ID order, per kind prefix
	base := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	var ordered []string
	for i := 0; i < 50; i++ {
		ordered = append(ordered, MintIDAt("task", base.Add(time.Duration(i)*time.Millisecond)))
	}
	if !sort.StringsAreSorted(ordered) {
		t.Errorf("IDs minted at ascending times are not in ascending string order: %v", ordered)
	}

	// the stamped time survives the encoding, which is what lets a migration
	// preserve an archived record's chronology instead of flattening it
	id := MintIDAt("research", base)
	rid, err := ids.ParseRecordID(strings.TrimPrefix(id, "R-"))
	if err != nil {
		t.Fatalf("ParseRecordID(%q): %v", id, err)
	}
	if !rid.Time().Equal(base) {
		t.Errorf("MintIDAt time = %v, want %v", rid.Time(), base)
	}
}

// TestKindOfBothSchemes: the kind-derivation helper answers off the prefix,
// which both schemes share, so nothing downstream needs to know which scheme
// a record predates.
func TestKindOfBothSchemes(t *testing.T) {
	kinds := map[string]string{
		"proposal": "P", "task": "T", "bug": "B", "research": "R", "adr": "ADR",
	}
	for kind, letter := range kinds {
		legacy := letter + "-0007"
		if got := KindOf(legacy); got != kind {
			t.Errorf("KindOf(%q) = %q, want %q", legacy, got, kind)
		}
		minted := MintID(kind)
		if got := KindOf(minted); got != kind {
			t.Errorf("KindOf(%q) = %q, want %q", minted, got, kind)
		}
	}
	// D is the pre-rename adr letter and still answers adr, in both shapes
	if got := KindOf("D-0007"); got != "adr" {
		t.Errorf("KindOf(D-0007) = %q, want adr", got)
	}
	if got := KindOf("D-" + strings.TrimPrefix(MintID("adr"), "ADR-")); got != "adr" {
		t.Errorf("KindOf(D-<record id>) = %q, want adr", got)
	}
}

// TestLegacyWorkRoundTripsByteIdentically: a work.md written by an older
// version — legacy IDs throughout, including a legacy parent and refs — must
// load with every field intact and rewrite to exactly the same bytes. Byte
// identity is the real assertion: it proves the widened heading regexp did
// not quietly reclassify part of a block as body, which would corrupt a file
// the moment any tool touched the context dir.
func TestLegacyWorkRoundTripsByteIdentically(t *testing.T) {
	root := ws(t)
	legacy := "---\nschema: " + workspace.SchemaStamp + "\n---\n" +
		"\n## P-0007 cache kernels in VRAM\n" +
		"kind: proposal\nstate: active\ncreated: 2025-11-03\n" +
		"refs: R-0002, ADR-0004\n" +
		"targets: go:a.F, gpu/kern.cu\n" +
		"\nThe proposal body.\n\nWith a second paragraph.\n" +
		"\n## T-0009 wire the cache\n" +
		"kind: task\nstate: draft\ncreated: 2025-11-04\nparent: P-0007\n" +
		"needs: D-0003\n" +
		"\nTask body.\n"
	if err := os.WriteFile(root.WorkPath(""), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}

	items, err := LoadWork(root.WorkPath(""), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("LoadWork read %d items, want 2: %+v", len(items), items)
	}
	p, task := items[0], items[1]
	if p.ID != "P-0007" || p.Kind != "proposal" || p.State != "active" ||
		p.Title != "cache kernels in VRAM" || p.Created != "2025-11-03" ||
		len(p.Refs) != 2 || p.Refs[1] != "ADR-0004" || len(p.Targets) != 2 ||
		p.Body != "The proposal body.\n\nWith a second paragraph." {
		t.Fatalf("legacy proposal mis-parsed: %+v", p)
	}
	if task.ID != "T-0009" || task.Parent != "P-0007" || len(task.Needs) != 1 ||
		task.Needs[0] != "D-0003" || task.Body != "Task body." {
		t.Fatalf("legacy task mis-parsed: %+v", task)
	}

	// rewriting through the ordinary write path reproduces the file exactly
	if err := Upsert(root, task); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(root.WorkPath(""))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != legacy {
		t.Fatalf("legacy work.md did not round-trip byte-identically:\n--- want ---\n%s\n--- got ---\n%s", legacy, got)
	}
}

// TestMixedSchemeWorkFile: the two schemes coexist in one work.md, which is
// exactly the state a workspace is in between minting its first record ID and
// migrating its old ones.
func TestMixedSchemeWorkFile(t *testing.T) {
	root := ws(t)
	old := Item{ID: "P-0007", Kind: "proposal", State: StateActive, Title: "legacy one", Created: "2025-11-03"}
	fresh := Item{ID: MintID("task"), Kind: "task", State: StateDraft, Title: "new one",
		Parent: "P-0007", Created: "2026-07-25"}
	if err := Upsert(root, old); err != nil {
		t.Fatal(err)
	}
	if err := Upsert(root, fresh); err != nil {
		t.Fatal(err)
	}
	items, err := LoadWork(root.WorkPath(""), "")
	if err != nil || len(items) != 2 {
		t.Fatalf("LoadWork = %+v, %v", items, err)
	}
	if items[0].ID != old.ID || items[1].ID != fresh.ID || items[1].Parent != "P-0007" {
		t.Fatalf("mixed-scheme file mis-parsed: %+v", items)
	}

	// Get resolves either scheme
	for _, want := range []string{old.ID, fresh.ID} {
		got, ok, err := Get(root, want)
		if err != nil || !ok || got.ID != want {
			t.Fatalf("Get(%q) = %+v, %v, %v", want, got, ok, err)
		}
	}

	// UnknownRefs validates a record ID exactly as it validates a legacy one
	known := map[string]bool{old.ID: true, fresh.ID: true}
	if got := UnknownRefs(fresh.ID, []string{old.ID}, known); len(got) != 0 {
		t.Fatalf("UnknownRefs rejected a legacy ref from a record-ID item: %v", got)
	}
	if got := UnknownRefs(old.ID, []string{fresh.ID}, known); len(got) != 0 {
		t.Fatalf("UnknownRefs rejected a record-ID ref: %v", got)
	}
	if got := UnknownRefs(old.ID, []string{"T-" + strings.Repeat("Z", 26)}, known); len(got) != 1 {
		t.Fatalf("UnknownRefs accepted an unknown record ID: %v", got)
	}
}

// TestHeaderFieldRoundTrip is the property test B-01KYN3E973F20 asked for. The
// bug survived because every existing test used single-line ADR values, so the
// table is deliberately made of the values that break a naive line-per-field
// header: embedded newlines, continuation lines shaped exactly like header
// fields, the ": " separator inside a value, significant leading and trailing
// whitespace, and a line that looks like an item heading.
func TestHeaderFieldRoundTrip(t *testing.T) {
	vals := []string{
		"Line one.\nLine two.",                // the reported reproduction
		"a\nb\nc",                             // more than one continuation
		"cost: higher memory\nlatency: lower", // continuations shaped like fields
		"  leading and trailing  ",            // whitespace is significant
		"\nleading newline is allowed",        // writes an empty value line
		"kind: text",                          // separator inside the value
		"ends with a separator: ",             // and at the very end
		"## looks like an item heading",       // as the whole value
		"a\n## looks like an item heading",    // and as a continuation
		"tab\there",                           // whitespace that is not a space
		"unicode — em dash, umlaut ü",         // not byte-sliced anywhere
	}
	for _, v := range vals {
		t.Run(strings.ReplaceAll(v, "\n", `\n`), func(t *testing.T) {
			root := ws(t)
			in := Item{
				ID: "ADR-0001", Kind: "adr", State: StateDraft, Title: "which cache",
				Created: "2026-07-30", Status: "accepted",
				Context: v, Decision: v, Consequences: v,
				Body: "body stays body.\n\nSecond paragraph.",
			}
			if err := Upsert(root, in); err != nil {
				t.Fatalf("Upsert: %v", err)
			}
			items, err := LoadWork(root.WorkPath(""), "")
			if err != nil || len(items) != 1 {
				t.Fatalf("LoadWork = %+v, %v", items, err)
			}
			got := items[0]
			for _, f := range []struct {
				name, want, got string
			}{
				{"Context", v, got.Context},
				{"Decision", v, got.Decision},
				{"Consequences", v, got.Consequences},
				{"Status", in.Status, got.Status},
				{"Title", in.Title, got.Title},
				{"Kind", in.Kind, got.Kind},
				{"Body", in.Body, got.Body},
			} {
				if f.got != f.want {
					t.Errorf("%s: got %q, want %q", f.name, f.got, f.want)
				}
			}
		})
	}
}

// TestHeaderRefusesUnwritableValue pins the other half of the fix: a value the
// header cannot represent is refused at the write path, and the refusal leaves
// the file exactly as it was. The guard runs while the buffer is built, before
// os.WriteFile — the ordering matters, because a guard that fires after the
// write is how a content-less ADR became permanent once before.
func TestHeaderRefusesUnwritableValue(t *testing.T) {
	for name, bad := range map[string]func(*Item){
		"blank line in prose": func(it *Item) { it.Context = "a\n\nb" },
		"trailing newline":    func(it *Item) { it.Consequences = "a\n" },
		"newline in title":    func(it *Item) { it.Title = "two\nlines" },
		"newline in status":   func(it *Item) { it.Status = "acce\npted" },
		"carriage in created": func(it *Item) { it.Created = "2026-07-30\r" },
		// The list fields are comma-joined onto one header line, so a newline
		// in an element wrote a second header line the parser then believed.
		// Reachable through the public draft tool: this exact targets value
		// made an item read back state=archived, a terminal state no
		// transition can reach.
		"newline in targets": func(it *Item) { it.Targets = []string{"go:x\nstate: archived"} },
		"newline in needs":   func(it *Item) { it.Needs = []string{"T-0001\nkind: bug"} },
		"newline in refs":    func(it *Item) { it.Refs = []string{"R-0001\nparent: P-0001"} },
		"comma in targets":   func(it *Item) { it.Targets = []string{"go:a,go:b"} },
	} {
		t.Run(name, func(t *testing.T) {
			root := ws(t)
			keep := Item{
				ID: "ADR-0001", Kind: "adr", State: StateDraft, Title: "keep me",
				Created: "2026-07-30", Decision: "redis", Status: "accepted",
			}
			if err := Upsert(root, keep); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(root.WorkPath(""))
			if err != nil {
				t.Fatal(err)
			}
			it := keep
			it.ID = "ADR-0002"
			bad(&it)
			if err := Upsert(root, it); err == nil {
				t.Fatal("Upsert accepted a value the header cannot read back")
			}
			after, err := os.ReadFile(root.WorkPath(""))
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(before) {
				t.Errorf("refusal changed the file:\nbefore:\n%s\nafter:\n%s", before, after)
			}
		})
	}
}
