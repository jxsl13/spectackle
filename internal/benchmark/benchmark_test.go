package benchmark

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func frame(kv ...string) map[string]string {
	m := map[string]string{"os": "linux", "arch": "amd64", "cpu": "ryzen-5800x", "ram": "32gb", "gpu": "rtx-3060-12gb"}
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i]] = kv[i+1]
	}
	return m
}

func rec(name string, f map[string]string, label string, val float64) Record {
	key, err := CanonicalKey(name, f)
	if err != nil {
		panic(err)
	}
	return Record{
		ID: "M-" + strings.ToUpper(name) + "-" + label, Name: name, Key: key,
		Frame:   CanonicalFrame(f),
		Metrics: []Metric{{Name: "time", Unit: "ns/op", Dir: "-", Noise: 2}},
		Impls:   []Impl{{Label: label, Res: map[string]float64{"time": val}}},
		T:       time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC),
	}
}

func TestCanonicalKeyDeterministicAndFolded(t *testing.T) {
	a, err := CanonicalKey("Tokenize Throughput", frame("impl", "CUDA"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := CanonicalKey("tokenize throughput", frame("IMPL", "cuda"))
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatalf("case/order must not matter: %q vs %q", a, b)
	}
	if !strings.HasPrefix(a, "tokenize-throughput|") {
		t.Fatalf("name folding wrong: %q", a)
	}
}

func TestCanonicalKeyRequiredDimsAndSentinels(t *testing.T) {
	f := frame()
	delete(f, "gpu")
	if _, err := CanonicalKey("x", f); err == nil || !strings.Contains(err.Error(), "gpu") {
		t.Fatalf("missing required dim must refuse naming it: %v", err)
	}
	f["gpu"] = "none"
	if _, err := CanonicalKey("x", f); err != nil {
		t.Fatalf("sentinel none must be legal: %v", err)
	}
	// machine-independent: any collapses hosts into ONE key
	all := map[string]string{"os": "any", "arch": "any", "cpu": "any", "ram": "any", "gpu": "any"}
	k1, err := CanonicalKey("bytes-per-lifecycle", all)
	if err != nil {
		t.Fatalf("sentinel any must be legal: %v", err)
	}
	k2, _ := CanonicalKey("bytes-per-lifecycle", map[string]string{"os": "ANY", "arch": "any", "cpu": "any", "ram": "any", "gpu": "any"})
	if k1 != k2 {
		t.Fatal("any keys must collapse")
	}
}

func TestCanonicalKeyCollisionSemantics(t *testing.T) {
	k1, _ := CanonicalKey("x", frame("ram", "32gb"))
	k2, _ := CanonicalKey("x", frame("ram", "64gb"))
	if k1 == k2 {
		t.Fatal("hosts differing only in ram must be different keys")
	}
	if _, err := CanonicalKey("bad|name", frame()); err == nil {
		t.Fatal("separator in name must refuse")
	}
	if _, err := CanonicalKey("x", frame("k=v", "y")); err == nil {
		t.Fatal("separator in dim key must refuse")
	}
}

func TestPutVersioningIdempotencyAndTrim(t *testing.T) {
	st := &Store{byKey: map[string][]Record{}}
	r1 := rec("bench-a", frame(), "go", 100)
	stored, prev, changed, err := st.Put(r1, 1)
	if err != nil || !changed || prev != nil || stored.Ver != 1 {
		t.Fatalf("first put: %+v prev=%v changed=%v err=%v", stored, prev, changed, err)
	}
	// idempotent replay
	if _, _, changed, _ := st.Put(rec("bench-a", frame(), "go", 100), 1); changed {
		t.Fatal("identical content must be an idempotent replay")
	}
	// content change increments and reports the superseded head
	r2 := rec("bench-a", frame(), "go", 90)
	stored, prev, changed, err = st.Put(r2, 1)
	if err != nil || !changed || stored.Ver != 2 {
		t.Fatalf("second put: %+v err=%v", stored, err)
	}
	if prev == nil || prev.Ver != 1 || prev.Impls[0].Res["time"] != 100 {
		t.Fatalf("the superseded head must return with its raw values: %+v", prev)
	}
	// depth 1: only the head is retained
	if got := len(st.Versions(stored.Key)); got != 1 {
		t.Fatalf("depth 1 must retain one version, got %d", got)
	}
	// depth 3 keeps history
	st2 := &Store{byKey: map[string][]Record{}}
	for i, v := range []float64{100, 90, 80, 70} {
		r := rec("bench-b", frame(), "go", v)
		r.ID = r.ID + string(rune('0'+i))
		if _, _, _, err := st2.Put(r, 3); err != nil {
			t.Fatal(err)
		}
	}
	vs := st2.Versions(mustKey(t, "bench-b"))
	if len(vs) != 3 || vs[0].Ver != 2 || vs[2].Ver != 4 {
		t.Fatalf("depth 3 must keep the newest three: %+v", vs)
	}
}

func mustKey(t *testing.T, name string) string {
	t.Helper()
	k, err := CanonicalKey(name, frame())
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func TestLoadVerifyQuarantineAndDedup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bench.ndjson")
	good := rec("bench-c", frame(), "go", 50)
	st := &Store{byKey: map[string][]Record{}}
	if _, _, _, err := st.Put(good, 1); err != nil {
		t.Fatal(err)
	}
	if err := st.Save(path); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	// corrupt a copy: stored key mismatching name+frame, plus a garbage line,
	// plus a duplicate of the good line (union-merge shape)
	tampered := strings.Replace(string(raw), good.Key, "forged|key", 1)
	content := string(raw) + tampered + "not json at all\n" + string(raw)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	st2, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(st2.Quarantine) != 2 {
		t.Fatalf("forged key + garbage must quarantine (2), got %d", len(st2.Quarantine))
	}
	if got := len(st2.Versions(good.Key)); got != 1 {
		t.Fatalf("union duplicate must dedup to one, got %d", got)
	}
	// quarantined lines survive a rewrite byte-identically
	if err := st2.Save(path); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if !strings.Contains(string(after), "forged|key") || !strings.Contains(string(after), "not json at all") {
		t.Fatal("quarantined lines must be preserved on save")
	}
}

func TestVersionCollisionResolvesDeterministically(t *testing.T) {
	// two clones minted DIFFERENT records at the same (key, ver): newer T
	// wins, ties break by ID — both clones converge
	a := rec("bench-d", frame(), "go", 10)
	a.Ver = 1
	b := rec("bench-d", frame(), "rust", 9)
	b.ID = "M-BENCH-D-rust"
	b.Ver = 1
	b.T = a.T.Add(time.Hour)
	dir := t.TempDir()
	path := filepath.Join(dir, "bench.ndjson")
	var lines []string
	for _, r := range []Record{a, b} {
		raw, err := jsonMarshal(r)
		if err != nil {
			t.Fatal(err)
		}
		lines = append(lines, raw)
	}
	// order 1
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	st1, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	// order 2 (reversed)
	if err := os.WriteFile(path, []byte(lines[1]+"\n"+lines[0]+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	st2, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	h1, _ := st1.Head(a.Key)
	h2, _ := st2.Head(a.Key)
	if h1.ID != h2.ID || h1.ID != b.ID {
		t.Fatalf("collision resolution must converge on the newer T: %q vs %q", h1.ID, h2.ID)
	}
	// the losing record measured something different — it must be
	// QUARANTINED, not silently dropped (B-01KYJTASR5EKW), in both orders
	if len(st1.Quarantine) != 1 || len(st2.Quarantine) != 1 {
		t.Fatalf("the collision loser must quarantine: %d vs %d lines", len(st1.Quarantine), len(st2.Quarantine))
	}
	if !strings.Contains(st1.Quarantine[0], a.ID) {
		t.Fatalf("the quarantined line must be the loser verbatim: %q", st1.Quarantine[0])
	}
	// and survive the next rewrite
	if err := st1.Save(path); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if !strings.Contains(string(after), a.ID) {
		t.Fatal("the losing record vanished from the file on save")
	}
}

// TestDupIDDifferentContentQuarantines: reusing a live record ID with
// different values is a hand edit or forge, not a union artifact — the
// line quarantines instead of vanishing (B-01KYJTASR5EKW finding 2).
func TestDupIDDifferentContentQuarantines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bench.ndjson")
	good := rec("bench-dup", frame(), "go", 1)
	st := &Store{byKey: map[string][]Record{}}
	if _, _, _, err := st.Put(good, 1); err != nil {
		t.Fatal(err)
	}
	if err := st.Save(path); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	forged := strings.Replace(string(raw), `"time":1`, `"time":999`, 1)
	if forged == string(raw) {
		t.Fatal("fixture: value replacement did not apply")
	}
	if err := os.WriteFile(path, []byte(string(raw)+forged), 0o644); err != nil {
		t.Fatal(err)
	}
	st2, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(st2.Quarantine) != 1 || !strings.Contains(st2.Quarantine[0], "999") {
		t.Fatalf("the content-diverging duplicate must quarantine: %+v", st2.Quarantine)
	}
	head, _ := st2.Head(good.Key)
	if head.Impls[0].Res["time"] != 1 {
		t.Fatalf("the FIRST line must stay the live record: %+v", head)
	}
	if err := st2.Save(path); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if !strings.Contains(string(after), "999") {
		t.Fatal("the quarantined duplicate vanished on save")
	}
}

func jsonMarshal(r Record) (string, error) {
	st := &Store{byKey: map[string][]Record{r.Key: {r}}}
	dir, err := os.MkdirTemp("", "bm-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	p := filepath.Join(dir, "x.ndjson")
	if err := st.Save(p); err != nil {
		return "", err
	}
	raw, err := os.ReadFile(p)
	return strings.TrimSpace(string(raw)), err
}

// Round 2 (cross-val-bench): the four foundation holes, pinned.
func TestValidateRefusesNonCanonicalFrame(t *testing.T) {
	r := rec("bench-e", frame(), "go", 5)
	r.Frame["cpu"] = "Ryzen 5800X" // unfolded — key stays consistent internally
	r.Key, _ = CanonicalKey(r.Name, r.Frame)
	if err := r.Validate(); err == nil || !strings.Contains(err.Error(), "not canonical") {
		t.Fatalf("an unfolded stored frame must refuse: %v", err)
	}
}

func TestValidateRefusesNonFiniteValues(t *testing.T) {
	r := rec("bench-f", frame(), "go", 5)
	r.Impls[0].Res["time"] = math.NaN()
	if err := r.Validate(); err == nil || !strings.Contains(err.Error(), "not finite") {
		t.Fatalf("NaN value must refuse: %v", err)
	}
	r.Impls[0].Res["time"] = math.Inf(1)
	if err := r.Validate(); err == nil {
		t.Fatal("Inf value must refuse")
	}
	// and Put refuses at the door — the store never holds a Save-poisoning
	// record (one NaN blocked persistence for the WHOLE file before)
	st := &Store{byKey: map[string][]Record{}}
	bad := rec("bench-f", frame(), "go", 5)
	bad.Impls[0].Res["time"] = math.NaN()
	if _, _, _, err := st.Put(bad, 1); err == nil {
		t.Fatal("Put must refuse a non-finite value")
	}
	good := rec("bench-f", frame(), "go", 5)
	if _, _, _, err := st.Put(good, 1); err != nil {
		t.Fatal(err)
	}
	if err := st.Save(filepath.Join(t.TempDir(), "bench.ndjson")); err != nil {
		t.Fatalf("a clean store must save: %v", err)
	}
}

func TestCanonicalKeyFoldsDimKeyCase(t *testing.T) {
	f := map[string]string{"OS": "linux", "arch": "amd64", "cpu": "x", "ram": "x", "gpu": "none"}
	if _, err := CanonicalKey("x", f); err != nil {
		t.Fatalf("an uppercase required dim KEY must fold, not refuse: %v", err)
	}
	bad := frame()
	bad["impl"] = "cu\tda"
	if _, err := CanonicalKey("x", bad); err == nil {
		t.Fatal("a tab inside a dim value must refuse")
	}
}
