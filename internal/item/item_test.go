package item

import (
	"os"
	"strings"
	"testing"

	"github.com/jxsl13/spectacle/internal/workspace"
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
