package lifecycle

import (
	"os"
	"strings"
	"testing"

	"github.com/jxsl13/spectacle/internal/item"
	"github.com/jxsl13/spectacle/internal/journal"
	"github.com/jxsl13/spectacle/internal/workspace"
)

func ws(t *testing.T) workspace.Root {
	t.Helper()
	root := workspace.Root{Dir: t.TempDir()}
	if err := root.EnsureScaffold(""); err != nil {
		t.Fatal(err)
	}
	// nested context dir so scope mapping has something to snap to
	if err := root.EnsureScaffold("gpu"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root.SpecPath("gpu"), []byte("---\nschema: v0\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestDraftScopeMapping(t *testing.T) {
	root := ws(t)
	// path targets under gpu/ snap to the gpu context
	it, err := Draft(root, nil, "proposal", "kernel work", "", "", "", []string{"gpu/kern.cu", "gpu/host.go"})
	if err != nil || it.Dir != "gpu" || it.ID != "P-0001" || it.State != item.StateDraft {
		t.Fatalf("Draft = %+v, %v", it, err)
	}
	// no targets -> root; per-kind counter independent of proposals
	it2, err := Draft(root, nil, "task", "cleanup", "", "", "", nil)
	if err != nil || it2.Dir != "" || it2.ID != "T-0001" {
		t.Fatalf("Draft = %+v, %v", it2, err)
	}
	// explicit dir wins
	it3, err := Draft(root, nil, "bug", "b", "", "gpu", "", nil)
	if err != nil || it3.Dir != "gpu" {
		t.Fatalf("Draft dir = %+v, %v", it3, err)
	}
	// unknown kind and unknown parent are rejected
	if _, err := Draft(root, nil, "epic", "x", "", "", "", nil); err == nil {
		t.Fatal("unknown kind accepted")
	}
	if _, err := Draft(root, nil, "task", "x", "", "", "P-9999", nil); err == nil {
		t.Fatal("unknown parent accepted")
	}
}

func TestMoveGuards(t *testing.T) {
	root := ws(t)
	Draft(root, nil, "proposal", "p", "", "", "", nil)

	// illegal transition names the allowed set
	if _, err := Move(root, "P-0001", item.StateDone, ""); err == nil || !strings.Contains(err.Error(), "allowed") {
		t.Fatalf("illegal transition: %v", err)
	}
	// rejection requires a note
	if _, err := Move(root, "P-0001", item.StateRejected, " "); err == nil || !strings.Contains(err.Error(), "note") {
		t.Fatalf("noteless rejection: %v", err)
	}
	// archive requires done
	Move(root, "P-0001", item.StateSubmitted, "")
	if _, err := Move(root, "P-0001", item.StateArchived, ""); err == nil {
		t.Fatal("archive from submitted accepted")
	}
	// unknown item
	if _, err := Move(root, "P-4242", item.StateActive, ""); err == nil {
		t.Fatal("unknown item accepted")
	}
}

func TestRejectionSnapshotAndRevocation(t *testing.T) {
	root := ws(t)
	Draft(root, nil, "proposal", "vram cache", "Keep kernels resident.", "", "", []string{"gpu/kern.cu"})
	if _, err := Move(root, "P-0001", item.StateRejected, "breaks multi-tenant scheduling"); err != nil {
		t.Fatal(err)
	}
	// gone from work.md
	if _, ok, _ := item.Get(root, "P-0001"); ok {
		t.Fatal("rejected item still in work.md")
	}
	// snapshot in journal
	events, _ := journal.ReadAll(root)
	var rej *journal.Event
	for i := range events {
		if events[i].Ev == journal.EvReject {
			rej = &events[i]
		}
	}
	if rej == nil || rej.Body != "Keep kernels resident." || len(rej.Tg) != 1 || rej.Note == "" {
		t.Fatalf("reject snapshot incomplete: %+v", rej)
	}
	// revocation restores the full item into a previous state
	it, err := Move(root, "P-0001", item.StateDraft, "")
	if err != nil || it.State != item.StateDraft {
		t.Fatalf("revocation = %+v, %v", it, err)
	}
	got, ok, _ := item.Get(root, "P-0001")
	if !ok || got.Body != "Keep kernels resident." || len(got.Targets) != 1 {
		t.Fatalf("restored item incomplete: %+v", got)
	}
	// ID minting does not reuse the rejected/restored number
	it2, _ := Draft(root, nil, "proposal", "next", "", "", "", nil)
	if it2.ID != "P-0002" {
		t.Fatalf("counter reused an ID: %s", it2.ID)
	}
}

func TestArchiveMergesIntentAndFoldsChildren(t *testing.T) {
	root := ws(t)
	Draft(root, nil, "proposal", "strided access", "Delta text.", "gpu", "", nil)
	Draft(root, nil, "task", "kernel change", "", "gpu", "P-0001", nil)

	Move(root, "P-0001", item.StateSubmitted, "")
	Move(root, "P-0001", item.StateApproved, "")
	Move(root, "P-0001", item.StateActive, "")
	Move(root, "T-0001", item.StateActive, "")

	// open child blocks the parent's done->archived path
	Move(root, "P-0001", item.StateDone, "")
	if _, err := Move(root, "P-0001", item.StateArchived, ""); err == nil || !strings.Contains(err.Error(), "T-0001") {
		t.Fatalf("open child not enforced: %v", err)
	}
	Move(root, "T-0001", item.StateDone, "")
	if _, err := Move(root, "P-0001", item.StateArchived, "shipped"); err != nil {
		t.Fatal(err)
	}
	// delta merged into the gpu spec's intent section
	raw, _ := os.ReadFile(root.SpecPath("gpu"))
	if !strings.Contains(string(raw), "## intent") || !strings.Contains(string(raw), "P-0001 strided access: shipped") {
		t.Fatalf("intent merge missing:\n%s", raw)
	}
	// both parent and done child left work.md
	all, _ := item.LoadAll(root)
	if len(all) != 0 {
		t.Fatalf("items left behind after archive: %+v", all)
	}
	// archive events journaled for both
	archived := 0
	events, _ := journal.ReadAll(root)
	for _, e := range events {
		if e.Ev == journal.EvArchive {
			archived++
		}
	}
	if archived != 2 {
		t.Fatalf("expected 2 archive events, got %d", archived)
	}
}
