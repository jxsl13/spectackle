package mcpserver

// bench tool e2e (T-01KYJN4BGB): the five ops round-trip through the dense
// grammars, deltas judge under direction+noise, EvBench events survive the
// journal fold with the superseded raw values intact, ls paginates, and rm
// restarts versioning at v1.

import (
	"os"
	"strings"
	"testing"

	"github.com/jxsl13/spectackle/internal/journal"
)

const benchFrame = "os=linux arch=amd64 cpu=ryzen-5800x ram=32gb gpu=rtx-3060-12gb"

func benchText(t *testing.T, s *Server, in benchIn) string {
	t.Helper()
	res, _, err := s.bench(in)
	return resText(t, res, err)
}

// benchID pulls the first M- record ID out of a render (put emits the
// SHORT display form — resolution accepts it as a prefix).
func benchID(t *testing.T, out string) string {
	t.Helper()
	for _, tok := range strings.Fields(out) {
		if strings.HasPrefix(tok, "M-") {
			return tok
		}
	}
	t.Fatalf("no M- ID in %q", out)
	return ""
}

// benchRefuse asserts the call refused (IsError) and returns the message.
func benchRefuse(t *testing.T, s *Server, in benchIn) string {
	t.Helper()
	res, _, err := s.bench(in)
	if err != nil {
		t.Fatalf("transport error, want an in-band refusal: %v", err)
	}
	if res == nil || !res.IsError {
		t.Fatalf("expected a refusal, got: %q", resText(t, res, err))
	}
	return resText(t, res, nil)
}

func TestBenchPutVersionDeltaUnchanged(t *testing.T) {
	s := newTestServer(t, t.TempDir())
	out := benchText(t, s, benchIn{Op: "put", Name: "tokenize throughput", Frame: benchFrame,
		Metrics: "time:ns/op:-", Results: "go: time=100"})
	if !strings.Contains(out, " v1 ") || !strings.Contains(out, " new") {
		t.Fatalf("first put must be v1/new: %q", out)
	}
	// identical content: idempotent replay, still v1
	out = benchText(t, s, benchIn{Op: "put", Name: "Tokenize Throughput", Frame: benchFrame,
		Metrics: "time:ns/op:-", Results: "go: time=100"})
	if !strings.Contains(out, "unchanged") || !strings.Contains(out, " v1 ") {
		t.Fatalf("identical put must render unchanged at v1: %q", out)
	}
	// changed value: v2, delta judged under dir "-" (lower better), head trimmed
	out = benchText(t, s, benchIn{Op: "put", Name: "tokenize throughput", Frame: benchFrame,
		Results: "go: time=90"}) // metrics omitted: inherit
	for _, want := range []string{" v2 ", "d go time 90 ns/op Δ-10 better", "better=1 worse=0 tie=0", "trimmed=1"} {
		if !strings.Contains(out, want) {
			t.Fatalf("changed put missing %q: %q", want, out)
		}
	}
	// the journal event carries the delta AND the superseded raw values
	events, err := journal.Read(s.ws, "")
	if err != nil {
		t.Fatal(err)
	}
	var ev *journal.Event
	for i := range events {
		if events[i].Ev == journal.EvBench && strings.Contains(events[i].Body, "superseded") {
			ev = &events[i]
		}
	}
	if ev == nil {
		t.Fatal("no EvBench event with a superseded body")
	}
	if !strings.Contains(ev.Sum, "Δ-10") || !strings.Contains(ev.Body, "time=100") {
		t.Fatalf("EvBench must carry delta (Sum) and raw prior values (Body): sum=%q body=%q", ev.Sum, ev.Body)
	}
}

func TestBenchDeltaNoiseAndDiagnosticTies(t *testing.T) {
	s := newTestServer(t, t.TempDir())
	// noise 5 absorbs a delta of 3; dir "~" never judges
	benchText(t, s, benchIn{Op: "put", Name: "noise-bench", Frame: benchFrame,
		Metrics: "time:ns/op:-:5 alloc:B/op:~", Results: "go: time=100 alloc=512"})
	out := benchText(t, s, benchIn{Op: "put", Name: "noise-bench", Frame: benchFrame,
		Results: "go: time=97 alloc=1024"})
	if !strings.Contains(out, "d go time 97 ns/op Δ-3 ~") {
		t.Fatalf("a delta within noise must render ~: %q", out)
	}
	if !strings.Contains(out, "better=0 worse=0 tie=2") {
		t.Fatalf("noise + diagnostic must both count as ties: %q", out)
	}
	if strings.Contains(out, "alloc=1024 ") && strings.Contains(out, "better") && strings.Contains(out, "d go alloc") &&
		(strings.Contains(out, "d go alloc 1024 B/op Δ+512 better") || strings.Contains(out, "d go alloc 1024 B/op Δ+512 worse")) {
		t.Fatalf("a diagnostic metric must never carry a verdict: %q", out)
	}
	// past the noise floor the verdict lands
	out = benchText(t, s, benchIn{Op: "put", Name: "noise-bench", Frame: benchFrame,
		Results: "go: time=200 alloc=1024"})
	if !strings.Contains(out, "d go time 200 ns/op Δ+103 worse") {
		t.Fatalf("a regression past noise must judge worse: %q", out)
	}
}

func TestBenchGetRendersFrameValuesWinner(t *testing.T) {
	s := newTestServer(t, t.TempDir())
	out := benchText(t, s, benchIn{Op: "put", Name: "impl-race", Frame: benchFrame + " impl=cpu-native",
		Metrics: "ops:ops/s:+", Results: "go: ops=10; rust@crates-1.2: ops=12"})
	id := benchID(t, out)
	got := benchText(t, s, benchIn{Op: "get", ID: id})
	for _, want := range []string{"m " + id, "impl-race", "f arch=amd64", "impl=cpu-native", "u go ops 10 ops/s", "u rust ops 12 ops/s *"} {
		if !strings.Contains(got, want) {
			t.Fatalf("get missing %q: %q", want, got)
		}
	}
	if strings.Contains(got, "u go ops 10 ops/s *") {
		t.Fatalf("the loser must not be starred: %q", got)
	}
}

func TestBenchLsFiltersAndPaginates(t *testing.T) {
	s := newTestServer(t, t.TempDir())
	for _, n := range []string{"alpha-bench", "beta-bench", "gamma-bench"} {
		benchText(t, s, benchIn{Op: "put", Name: n, Frame: benchFrame,
			Metrics: "time:ns/op:-", Results: "go: time=1"})
	}
	benchText(t, s, benchIn{Op: "put", Name: "other-host", Frame: "os=darwin arch=arm64 cpu=m2-pro ram=32gb gpu=none",
		Metrics: "time:ns/op:-", Results: "go: time=1"})
	out := benchText(t, s, benchIn{Op: "ls"})
	if c := strings.Count(out, "m M-"); c != 4 {
		t.Fatalf("ls must list all 4 heads, got %d: %q", c, out)
	}
	out = benchText(t, s, benchIn{Op: "ls", Name: "beta"})
	if !strings.Contains(out, "beta-bench") || strings.Contains(out, "alpha-bench") {
		t.Fatalf("name filter wrong: %q", out)
	}
	out = benchText(t, s, benchIn{Op: "ls", Frame: "os=darwin"})
	if !strings.Contains(out, "other-host") || strings.Contains(out, "alpha-bench") {
		t.Fatalf("frame filter wrong: %q", out)
	}
	// pagination: a tiny budget forces a cursor, the cursor resumes
	out = benchText(t, s, benchIn{Op: "ls", Budget: 1})
	if !strings.Contains(out, "cur ") {
		t.Fatalf("a tiny budget must emit a cursor: %q", out)
	}
	cur := ""
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		if rest, ok := strings.CutPrefix(l, "cur "); ok {
			cur = rest
		}
	}
	next := benchText(t, s, benchIn{Op: "ls", Budget: 1, Cur: cur})
	if next == out || !strings.Contains(next, "m M-") {
		t.Fatalf("the cursor must resume with different lines: %q", next)
	}
}

func TestBenchCmpDeltasAndUnitMismatch(t *testing.T) {
	s := newTestServer(t, t.TempDir())
	out := benchText(t, s, benchIn{Op: "put", Name: "old-run", Frame: benchFrame,
		Metrics: "time:ns/op:-", Results: "go: time=100"})
	oldID := benchID(t, out)
	out = benchText(t, s, benchIn{Op: "put", Name: "new-run", Frame: benchFrame,
		Metrics: "time:ns/op:-", Results: "go: time=90"})
	newID := benchID(t, out)
	got := benchText(t, s, benchIn{Op: "cmp", ID: oldID, ID2: newID})
	if !strings.Contains(got, "d go time 100 -> 90 ns/op Δ-10 better") {
		t.Fatalf("cmp delta wrong: %q", got)
	}
	// same metric name, different unit: refuse — units are never converted
	out = benchText(t, s, benchIn{Op: "put", Name: "ms-run", Frame: benchFrame,
		Metrics: "time:ms/op:-", Results: "go: time=1"})
	msID := benchID(t, out)
	msg := benchRefuse(t, s, benchIn{Op: "cmp", ID: oldID, ID2: msID})
	if !strings.Contains(msg, "units differ") {
		t.Fatalf("unit mismatch must refuse naming the units: %q", msg)
	}
	// disjoint impls/metrics: nothing to compare is a refusal, not empty ok
	out = benchText(t, s, benchIn{Op: "put", Name: "disjoint", Frame: benchFrame,
		Metrics: "ops:ops/s:+", Results: "zig: ops=5"})
	dzID := benchID(t, out)
	if msg := benchRefuse(t, s, benchIn{Op: "cmp", ID: oldID, ID2: dzID}); !strings.Contains(msg, "no (impl, metric) pair") {
		t.Fatalf("disjoint cmp must refuse: %q", msg)
	}
}

func TestBenchRmThenPutRestartsAtV1(t *testing.T) {
	s := newTestServer(t, t.TempDir())
	benchText(t, s, benchIn{Op: "put", Name: "reborn", Frame: benchFrame,
		Metrics: "time:ns/op:-", Results: "go: time=100"})
	// depth 1 trims v1 away, so the LIVE id is v2's — a v1 id no longer
	// resolves after the trim, by design
	out := benchText(t, s, benchIn{Op: "put", Name: "reborn", Frame: benchFrame, Results: "go: time=90"})
	id := benchID(t, out)
	got := benchText(t, s, benchIn{Op: "rm", ID: id})
	if !strings.Contains(got, "removed 1 version(s)") {
		t.Fatalf("rm must report the retained count (depth 1): %q", got)
	}
	out = benchText(t, s, benchIn{Op: "put", Name: "reborn", Frame: benchFrame,
		Metrics: "time:ns/op:-", Results: "go: time=80"})
	if !strings.Contains(out, " v1 ") || !strings.Contains(out, " new") {
		t.Fatalf("a put after rm must restart at v1: %q", out)
	}
	// the removal is journaled — no silent resurrection
	events, err := journal.Read(s.ws, "")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range events {
		if e.Ev == journal.EvBench && strings.Contains(e.Sum, "removed") {
			found = true
		}
	}
	if !found {
		t.Fatal("rm must journal an EvBench removal event")
	}
}

func TestBenchGrammarRefusals(t *testing.T) {
	s := newTestServer(t, t.TempDir())
	noGPU := "os=linux arch=amd64 cpu=x ram=32gb"
	if msg := benchRefuse(t, s, benchIn{Op: "put", Name: "x", Frame: noGPU,
		Metrics: "time:ns/op:-", Results: "go: time=1"}); !strings.Contains(msg, "gpu") {
		t.Fatalf("missing required dim must name it: %q", msg)
	}
	if msg := benchRefuse(t, s, benchIn{Op: "put", Name: "x", Frame: "os=linux notakv " + benchFrame,
		Metrics: "time:ns/op:-", Results: "go: time=1"}); !strings.Contains(msg, "not k=v") {
		t.Fatalf("bad dim token must refuse: %q", msg)
	}
	if msg := benchRefuse(t, s, benchIn{Op: "put", Name: "x", Frame: benchFrame,
		Metrics: "time:ns/op:-", Results: "go time=1"}); !strings.Contains(msg, "label") {
		t.Fatalf("results without the label: prefix must refuse: %q", msg)
	}
	// a result referencing an undeclared metric refuses via Validate
	if msg := benchRefuse(t, s, benchIn{Op: "put", Name: "x", Frame: benchFrame,
		Metrics: "time:ns/op:-", Results: "go: bogus=1"}); !strings.Contains(msg, "undeclared") {
		t.Fatalf("an undeclared metric must refuse: %q", msg)
	}
	// first put for a key without metric declarations
	if msg := benchRefuse(t, s, benchIn{Op: "put", Name: "fresh", Frame: benchFrame,
		Results: "go: time=1"}); !strings.Contains(msg, "metrics declarations") {
		t.Fatalf("a first put without metrics must refuse: %q", msg)
	}
	// ParseFloat accepts the literal NaN — the store must still refuse it
	if msg := benchRefuse(t, s, benchIn{Op: "put", Name: "x", Frame: benchFrame,
		Metrics: "time:ns/op:-", Results: "go: time=NaN"}); !strings.Contains(msg, "not finite") {
		t.Fatalf("NaN must refuse at the door: %q", msg)
	}
	if msg := benchRefuse(t, s, benchIn{Op: "get", ID: "M-NOPE"}); !strings.Contains(msg, "no record matches") {
		t.Fatalf("unknown ID must refuse: %q", msg)
	}
	if msg := benchRefuse(t, s, benchIn{Op: "frobnicate"}); !strings.Contains(msg, "put|get|ls|rm|cmp") {
		t.Fatalf("unknown op must list the legal ones: %q", msg)
	}
}

// TestBenchAmbiguousPrefixIsNotUnknown: a prefix matching several records
// refuses NAMING the candidates — indistinguishable from "no match" was a
// cross-validation FAIL.
func TestBenchAmbiguousPrefixIsNotUnknown(t *testing.T) {
	s := newTestServer(t, t.TempDir())
	for _, n := range []string{"amb-one", "amb-two"} {
		benchText(t, s, benchIn{Op: "put", Name: n, Frame: benchFrame,
			Metrics: "time:ns/op:-", Results: "go: time=1"})
	}
	// every record ID starts with "M-" — the two-record prefix is ambiguous
	msg := benchRefuse(t, s, benchIn{Op: "get", ID: "M-"})
	if !strings.Contains(msg, "matches 2 records") || strings.Contains(msg, "no record matches") {
		t.Fatalf("ambiguity must name the candidates, not read as unknown: %q", msg)
	}
}

// TestBenchQuarantineWarningLeadsPageOne: the QUAR warning renders FIRST so
// budget truncation can never page it out of the first response.
func TestBenchQuarantineWarningLeadsPageOne(t *testing.T) {
	s := newTestServer(t, t.TempDir())
	benchText(t, s, benchIn{Op: "put", Name: "quar-bench", Frame: benchFrame,
		Metrics: "time:ns/op:-", Results: "go: time=1"})
	path := s.ws.BenchPath("")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("not json at all\n"); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	out := benchText(t, s, benchIn{Op: "ls", Budget: 1})
	first := strings.SplitN(out, "\n", 2)[0]
	if !strings.HasPrefix(first, "! QUAR W") {
		t.Fatalf("the quarantine warning must lead page one: %q", out)
	}
}

// TestBenchEvBenchSurvivesFold: the compaction fold keeps EvBench events —
// the superseded raw values in Body are exactly what the depth-1 trim
// discarded from bench.ndjson, so losing the event loses the forensics
// (ADR-01KYJMWEWQ).
func TestBenchEvBenchSurvivesFold(t *testing.T) {
	root := t.TempDir()
	srv, sess := connectRootWithServer(t, root)
	benchText(t, srv, benchIn{Op: "put", Name: "fold-bench", Frame: benchFrame,
		Metrics: "time:ns/op:-", Results: "go: time=100"})
	benchText(t, srv, benchIn{Op: "put", Name: "fold-bench", Frame: benchFrame, Results: "go: time=90"})
	// droppable noise so the fold actually runs (an all-keepable journal
	// folds nothing and reports no "ok folded")
	for i := 0; i < 2; i++ {
		if err := journal.Append(srv.ws, "", journal.Event{
			Ev: journal.EvCreate, ID: "T-FOLDNOISE", K: "task",
		}); err != nil {
			t.Fatal(err)
		}
	}

	srv.ws.Cfg.Compact.JournalMax = 1
	out := callText(t, sess, "compact", map[string]any{"apply": true})
	if !strings.Contains(out, "ok folded") {
		t.Fatalf("fold did not run: %q", out)
	}
	events, err := journal.Read(srv.ws, "")
	if err != nil {
		t.Fatal(err)
	}
	var ev *journal.Event
	for i := range events {
		if events[i].Ev == journal.EvBench && strings.Contains(events[i].Body, "superseded v1") {
			ev = &events[i]
		}
	}
	if ev == nil {
		t.Fatal("the fold dropped the EvBench delta event")
	}
	if !strings.Contains(ev.Body, "time=100") || !strings.Contains(ev.Sum, "Δ-10") {
		t.Fatalf("EvBench must survive with raw values and delta intact: sum=%q body=%q", ev.Sum, ev.Body)
	}
}
