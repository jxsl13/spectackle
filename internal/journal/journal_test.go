package journal

import (
	"testing"
	"time"

	"github.com/jxsl13/spectacle/internal/workspace"
)

func TestAppendReadRoundtrip(t *testing.T) {
	ws := workspace.Root{Dir: t.TempDir()}
	e := Event{
		Ev: EvReject, ID: "P-0001", K: "proposal", Ti: "vram cache",
		Note: "breaks scheduling", Sum: "proposal vram cache",
		Body: "full body", Tg: []string{"go:a.F"}, Par: "P-0000",
	}
	if err := Append(ws, "", e); err != nil {
		t.Fatal(err)
	}
	if err := Append(ws, "", Event{Ev: EvCreate, ID: "T-0001", K: "task", Ti: "x"}); err != nil {
		t.Fatal(err)
	}
	events, err := Read(ws, "")
	if err != nil || len(events) != 2 {
		t.Fatalf("Read = %d events, %v", len(events), err)
	}
	got := events[0]
	if got.Ev != EvReject || got.ID != "P-0001" || got.Note != "breaks scheduling" ||
		got.Body != "full body" || len(got.Tg) != 1 || got.Par != "P-0000" {
		t.Fatalf("roundtrip mismatch: %+v", got)
	}
	if got.T.IsZero() || got.T.After(time.Now().UTC()) {
		t.Fatalf("timestamp not stamped: %v", got.T)
	}
	// missing journal = empty, not error
	if es, err := Read(ws, "nowhere"); err != nil || es != nil {
		t.Fatalf("missing journal: %v %v", es, err)
	}
}

func TestReadAllAcrossContexts(t *testing.T) {
	ws := workspace.Root{Dir: t.TempDir()}
	if err := Append(ws, "", Event{Ev: EvCreate, ID: "P-0001"}); err != nil {
		t.Fatal(err)
	}
	if err := Append(ws, "gpu", Event{Ev: EvCreate, ID: "T-0001"}); err != nil {
		t.Fatal(err)
	}
	events, err := ReadAll(ws)
	if err != nil || len(events) != 2 {
		t.Fatalf("ReadAll = %d, %v", len(events), err)
	}
	// Dir is backfilled from the context the event was read from
	for _, e := range events {
		if e.ID == "T-0001" && e.Dir != "gpu" {
			t.Fatalf("Dir not backfilled: %+v", e)
		}
	}
}

func TestFeedbackEventKindsDefined(t *testing.T) {
	for _, ev := range []string{EvGrill, EvDecide, EvEscalate} {
		if ev == "" {
			t.Fatal("feedback event kind not defined")
		}
	}
}

func TestFeedbackFieldsRoundtrip(t *testing.T) {
	ws := workspace.Root{Dir: t.TempDir()}
	// reject snapshot roundtrip: Rnd/Gr/Nd/Ov carry the feedback-loop state
	e := Event{
		Ev: EvReject, ID: "T-0001", K: "task", Ti: "grilled task",
		Note: "not worth it", Sum: "task grilled task",
		Rnd: 3, Gr: "needs more test coverage", Nd: []string{"D-0001"}, Ov: true,
	}
	if err := Append(ws, "", e); err != nil {
		t.Fatal(err)
	}
	if err := Append(ws, "", Event{Ev: EvEscalate, ID: "T-0002", Rnd: 3, Nd: []string{"D-0002"}}); err != nil {
		t.Fatal(err)
	}
	if err := Append(ws, "", Event{Ev: EvGrill, ID: "T-0002", Gr: "flaky under load"}); err != nil {
		t.Fatal(err)
	}
	if err := Append(ws, "", Event{Ev: EvDecide, ID: "T-0002", To: "draft", Ov: true}); err != nil {
		t.Fatal(err)
	}
	events, err := Read(ws, "")
	if err != nil || len(events) != 4 {
		t.Fatalf("Read = %d events, %v", len(events), err)
	}
	got := events[0]
	if got.Rnd != 3 || got.Gr != "needs more test coverage" || len(got.Nd) != 1 || got.Nd[0] != "D-0001" || !got.Ov {
		t.Fatalf("reject snapshot feedback fields mismatch: %+v", got)
	}
	if events[1].Ev != EvEscalate || events[1].Rnd != 3 || len(events[1].Nd) != 1 {
		t.Fatalf("escalate event mismatch: %+v", events[1])
	}
	if events[2].Ev != EvGrill || events[2].Gr != "flaky under load" {
		t.Fatalf("grill event mismatch: %+v", events[2])
	}
	if events[3].Ev != EvDecide || events[3].To != "draft" || !events[3].Ov {
		t.Fatalf("decide event mismatch: %+v", events[3])
	}
}

func TestRewrite(t *testing.T) {
	ws := workspace.Root{Dir: t.TempDir()}
	for i := 0; i < 3; i++ {
		if err := Append(ws, "", Event{Ev: EvCreate, ID: "T-0001"}); err != nil {
			t.Fatal(err)
		}
	}
	keep := []Event{{T: time.Now().UTC(), Ev: EvReject, ID: "B-0001", Note: "kept"}}
	if err := Rewrite(ws, "", keep); err != nil {
		t.Fatal(err)
	}
	events, _ := Read(ws, "")
	if len(events) != 1 || events[0].Ev != EvReject {
		t.Fatalf("Rewrite = %+v", events)
	}
}
