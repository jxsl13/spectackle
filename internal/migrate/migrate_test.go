package migrate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jxsl13/spectackle/internal/ids"
)

// v0Fixture writes a workspace on the old stamp: a root bundle plus two nested
// context dirs, with the reference shapes the migration has to carry —
// parent/refs/needs headers, a decide option record's `blocks:` line, an ID
// mentioned in prose, an archive tombstone whose record is gone from work.md,
// a reject snapshot, and a spec.md intent line.
func v0Fixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	write(".spectackle/config.yaml", "schema: v0\nlangs:\n  - go\n")
	write(".spectackle/spec.md", "---\nschema: v0\n---\n\n## intent\n\n- P-0002 old work: shipped\n")
	write(".spectackle/work.md", `---
schema: v0
---

## P-0001 keep kernels resident
kind: proposal
state: active
created: 2026-01-05
refs: R-0003, ADR-0001
targets: gpu/kern.cu

Supersedes P-0002. See ADR-0001 for the decision.

## T-0004 thread the pool through run
kind: task
state: approved
created: 2026-01-06
parent: P-0001
needs: ADR-0001

Body mentioning T-0004 itself and P-0001.

## ADR-0001 one pool per stream or one shared pool?
kind: adr
state: submitted
created: 2026-01-06
status: proposed

kind: radio
option: per-stream
option: shared
blocks: T-0004
`)
	write(".spectackle/journal.ndjson", strings.Join([]string{
		`{"t":"2026-01-05T10:00:00Z","eid":"a1","ag":"alice","ev":"create","id":"P-0001","k":"proposal","ti":"keep kernels resident"}`,
		`{"t":"2026-01-04T09:00:00Z","eid":"a0","ag":"alice","ev":"create","id":"P-0002","k":"proposal","ti":"old work"}`,
		`{"t":"2026-01-04T11:00:00Z","eid":"a2","ag":"alice","ev":"archive","id":"P-0002","k":"proposal","ti":"old work","sum":"shipped, folded into P-0001"}`,
		`{"t":"2026-01-06T10:00:00Z","eid":"a3","ag":"bob","ev":"create","id":"T-0004","k":"task","ti":"thread the pool through run","par":"P-0001"}`,
		`{"t":"2026-01-06T10:05:00Z","eid":"a4","ag":"bob","ev":"create","id":"ADR-0001","k":"adr","ti":"one pool per stream or one shared pool?"}`,
		`{"t":"2026-01-06T11:00:00Z","eid":"a5","ag":"bob","ev":"escalate","id":"T-0004","nd":["ADR-0001"],"rnd":3,"note":"decide ADR-0001"}`,
		`{"t":"2026-01-07T10:00:00Z","eid":"a6","ag":"bob","ev":"reject","id":"B-0005","k":"bug","ti":"pool leaks","body":"Not a leak; see P-0001.","par":"P-0001","note":"by design","n":2}`,
	}, "\n")+"\n")
	write(".spectackle/anchors.tsv", "CORE-POOL-001\tgo:pool.Get\tpool.go\t1\t9\tabc\n")

	write("core/.spectackle/spec.md", "---\nschema: v0\nprefix: CORE\n---\n\n## intent\n\n- T-0004 threading: done\n")
	write("core/.spectackle/work.md", `---
schema: v0
---

## R-0003 survey pooling strategies
kind: research
state: draft
created: 2026-01-05
targets: core/pool.go

Feeds P-0001.
`)
	write("core/.spectackle/journal.ndjson",
		`{"t":"2026-01-05T09:00:00Z","eid":"b1","ag":"alice","ev":"create","id":"R-0003","k":"research","ti":"survey pooling strategies","dir":"core"}`+"\n")

	write("core/engine/.spectackle/work.md", "---\nschema: v0\n---\n")
	write("core/engine/.spectackle/journal.ndjson", "")
	return dir
}

func read(t *testing.T, dir, rel string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// TestMigrateFixtureRewritesEveryReference is the main pass: every record gets
// a new ID, every reference to it follows — headers, prose, the decide option
// record, journal fields, spec.md intent lines — and no legacy ID survives
// anywhere in the bundle.
func TestMigrateFixtureRewritesEveryReference(t *testing.T) {
	dir := v0Fixture(t)

	rep, err := Run(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Migrated {
		t.Fatal("fixture on the old stamp was not migrated")
	}

	// every record found, live and tombstoned alike
	for _, legacy := range []string{"P-0001", "P-0002", "T-0004", "ADR-0001", "R-0003", "B-0005"} {
		to, ok := rep.Remap[legacy]
		if !ok {
			t.Fatalf("%s missing from the remap: %v", legacy, rep.Remap)
		}
		if !ids.ValidRecordID(strings.SplitN(to, "-", 2)[1]) {
			t.Fatalf("%s -> %q is not a canonical record ID", legacy, to)
		}
	}
	// the kind letter is preserved, and legacy D would have become ADR
	for legacy, want := range map[string]string{
		"P-0001": "P-", "T-0004": "T-", "ADR-0001": "ADR-", "R-0003": "R-", "B-0005": "B-",
	} {
		if !strings.HasPrefix(rep.Remap[legacy], want) {
			t.Fatalf("%s -> %s, want the %s prefix", legacy, rep.Remap[legacy], want)
		}
	}

	files, err := bundleFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range files {
		body := read(t, dir, rel)
		if m := legacyIDRe.FindString(body); m != "" {
			t.Fatalf("%s still carries the legacy ID %s:\n%s", rel, m, body)
		}
		if strings.Contains(body, "schema: "+From) {
			t.Fatalf("%s was not restamped:\n%s", rel, body)
		}
	}

	// references resolve to the same new ID on both sides
	rootWork := read(t, dir, ".spectackle/work.md")
	if !strings.Contains(rootWork, "parent: "+rep.Remap["P-0001"]) {
		t.Fatalf("parent header not rewritten:\n%s", rootWork)
	}
	if !strings.Contains(rootWork, "refs: "+rep.Remap["R-0003"]+", "+rep.Remap["ADR-0001"]) {
		t.Fatalf("refs header not rewritten:\n%s", rootWork)
	}
	if !strings.Contains(rootWork, "needs: "+rep.Remap["ADR-0001"]) {
		t.Fatalf("needs header not rewritten:\n%s", rootWork)
	}
	if !strings.Contains(rootWork, "blocks: "+rep.Remap["T-0004"]) {
		t.Fatalf("decide option record's blocks line not rewritten:\n%s", rootWork)
	}
	if !strings.Contains(rootWork, "Supersedes "+rep.Remap["P-0002"]) {
		t.Fatalf("prose reference not rewritten:\n%s", rootWork)
	}
	// the archived record exists only as a tombstone; its ID had to map too
	jrnl := read(t, dir, ".spectackle/journal.ndjson")
	if !strings.Contains(jrnl, `"id":"`+rep.Remap["P-0002"]+`"`) {
		t.Fatalf("tombstone ID not rewritten:\n%s", jrnl)
	}
	if !strings.Contains(jrnl, "shipped, folded into "+rep.Remap["P-0001"]) {
		t.Fatalf("tombstone summary reference not rewritten:\n%s", jrnl)
	}
	if !strings.Contains(jrnl, `"nd":["`+rep.Remap["ADR-0001"]+`"]`) {
		t.Fatalf("escalate needs list not rewritten:\n%s", jrnl)
	}
	if !strings.Contains(jrnl, `"par":"`+rep.Remap["P-0001"]+`"`) {
		t.Fatalf("reject snapshot parent not rewritten:\n%s", jrnl)
	}
	// nested cascade, not just the root
	if !strings.Contains(read(t, dir, "core/.spectackle/spec.md"), rep.Remap["T-0004"]) {
		t.Fatal("nested spec.md intent line not rewritten")
	}
	if !strings.Contains(read(t, dir, "core/.spectackle/work.md"), "## "+rep.Remap["R-0003"]) {
		t.Fatal("nested work.md heading not rewritten")
	}
}

// TestMigrateIsIdempotent: the second run reports nothing and writes nothing.
func TestMigrateIsIdempotent(t *testing.T) {
	dir := v0Fixture(t)
	if _, err := Run(dir); err != nil {
		t.Fatal(err)
	}
	files, err := bundleFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	before := map[string]string{}
	for _, rel := range files {
		before[rel] = read(t, dir, rel)
	}

	rep, err := Run(dir)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Migrated || len(rep.Files) != 0 {
		t.Fatalf("second run was not a no-op: %+v", rep)
	}
	for rel, want := range before {
		if got := read(t, dir, rel); got != want {
			t.Fatalf("%s changed on the second run:\n--- before\n%s\n--- after\n%s", rel, want, got)
		}
	}
	if need, err := Needed(dir); err != nil || need {
		t.Fatalf("Needed after migrating = %v, %v; want false", need, err)
	}
}

// TestMigrateLeavesCurrentWorkspaceAlone: a workspace already on the new stamp
// is not touched, and carries no backup.
func TestMigrateLeavesCurrentWorkspaceAlone(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, Dot), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nschema: " + To + "\n---\n\n## T-01KYCJB0G0ET4V4XWXSDT7PGFK already new\nkind: task\nstate: draft\n"
	if err := os.WriteFile(filepath.Join(dir, Dot, "work.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, Dot, "config.yaml"), []byte("schema: "+To+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	rep, err := Run(dir)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Migrated {
		t.Fatalf("a current workspace was migrated: %+v", rep)
	}
	if got := read(t, dir, Dot+"/work.md"); got != body {
		t.Fatalf("work.md rewritten:\n%s", got)
	}
	if _, err := os.Stat(filepath.Join(dir, Dot, backupPrefix+From)); err == nil {
		t.Fatal("a backup was created for a workspace that needed no migration")
	}
}

// TestMigrateIsDeterministic: two copies of one fixture migrate to identical
// IDs. This is what keeps two clones of a repository from minting divergent IDs
// for the same committed records and colliding on the next merge — the exact
// failure P-0088 exists to close, reintroduced by a nondeterministic migration.
func TestMigrateIsDeterministic(t *testing.T) {
	a, b := v0Fixture(t), v0Fixture(t)
	repA, err := Run(a)
	if err != nil {
		t.Fatal(err)
	}
	repB, err := Run(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(repA.Remap) != len(repB.Remap) {
		t.Fatalf("remap sizes differ: %d vs %d", len(repA.Remap), len(repB.Remap))
	}
	for legacy, to := range repA.Remap {
		if repB.Remap[legacy] != to {
			t.Fatalf("%s migrated to %s in one copy and %s in the other", legacy, to, repB.Remap[legacy])
		}
	}
	for _, rel := range repA.Files {
		if read(t, a, rel) != read(t, b, rel) {
			t.Fatalf("%s differs between two migrations of the same fixture", rel)
		}
	}
}

// TestMigratePreservesChronology: the new IDs sort in the order the records
// were created, because each is stamped with its own journal timestamp. Minting
// from wall-clock-now would collapse the whole archive into the migration
// moment and lose the ordering these IDs are supposed to carry.
func TestMigratePreservesChronology(t *testing.T) {
	dir := v0Fixture(t)
	rep, err := Run(dir)
	if err != nil {
		t.Fatal(err)
	}
	// fixture creation order: P-0002 (Jan 4) < R-0003 (Jan 5 09:00) <
	// P-0001 (Jan 5 10:00) < T-0004 (Jan 6 10:00) < ADR-0001 (Jan 6 10:05)
	order := []string{"P-0002", "R-0003", "P-0001", "T-0004", "ADR-0001"}
	for i := 1; i < len(order); i++ {
		prev := tailOf(t, rep.Remap[order[i-1]])
		cur := tailOf(t, rep.Remap[order[i]])
		if !(prev < cur) {
			t.Fatalf("%s (%s) does not sort before %s (%s)", order[i-1], prev, order[i], cur)
		}
	}
}

func tailOf(t *testing.T, id string) string {
	t.Helper()
	_, tail, ok := strings.Cut(id, "-")
	if !ok {
		t.Fatalf("malformed migrated ID %q", id)
	}
	return tail
}

// TestMigrateAbortedMidWriteStillLoads: a crash between the first and last
// write leaves a mix of old and new files. The retained backup with no
// completion marker is what marks that, and the next open rolls it back to a
// workspace that loads — then migrates it again, to completion.
func TestMigrateAbortedMidWriteStillLoads(t *testing.T) {
	dir := v0Fixture(t)

	// Simulate the crash: back up two files as Run does, rewrite ONE of them
	// to a bogus half-migrated state, and leave the marker absent.
	backupAbs := filepath.Join(dir, Dot, backupPrefix+From)
	for _, rel := range []string{".spectackle/work.md", ".spectackle/journal.ndjson"} {
		if err := copyInto(filepath.Join(dir, filepath.FromSlash(rel)), filepath.Join(backupAbs, rel)); err != nil {
			t.Fatal(err)
		}
	}
	half := "---\nschema: " + To + "\n---\n\n## T-01KYCJB0G0ET4V4XWXSDT7PGFK half migrated\nkind: task\nstate: draft\n"
	if err := os.WriteFile(filepath.Join(dir, ".spectackle", "work.md"), []byte(half), 0o644); err != nil {
		t.Fatal(err)
	}

	if need, err := Needed(dir); err != nil || !need {
		t.Fatalf("an interrupted attempt was not detected: %v %v", need, err)
	}
	rep, err := Run(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Migrated {
		t.Fatal("Run after a rollback did not migrate")
	}
	// rolled back, then migrated properly: the original record is back, under
	// its new ID, and the bogus half-migrated heading is gone.
	work := read(t, dir, ".spectackle/work.md")
	if strings.Contains(work, "half migrated") {
		t.Fatalf("the interrupted write was not rolled back:\n%s", work)
	}
	if !strings.Contains(work, "## "+rep.Remap["P-0001"]+" keep kernels resident") {
		t.Fatalf("the original record did not survive the rollback:\n%s", work)
	}
	if m := legacyIDRe.FindString(work); m != "" {
		t.Fatalf("post-rollback migration left %s behind:\n%s", m, work)
	}
}

// TestMigrateRetainsRecoverableBackup: the pre-migration bundles stay on disk,
// marked complete, so a user who wants the old state back has it.
func TestMigrateRetainsRecoverableBackup(t *testing.T) {
	dir := v0Fixture(t)
	rep, err := Run(dir)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Backup == "" {
		t.Fatal("no backup path reported")
	}
	backupAbs := filepath.Join(dir, filepath.FromSlash(rep.Backup))
	if _, err := os.Stat(filepath.Join(backupAbs, doneMarker)); err != nil {
		t.Fatalf("completion marker missing: %v", err)
	}
	old := read(t, dir, rep.Backup+"/.spectackle/work.md")
	if !strings.Contains(old, "## P-0001 keep kernels resident") {
		t.Fatalf("the backup does not hold the pre-migration record:\n%s", old)
	}
	if !strings.Contains(old, "schema: "+From) {
		t.Fatalf("the backup was restamped:\n%s", old)
	}
	// and a completed backup is never rolled back by a later open
	if rep2, err := Recover(dir); err != nil || rep2.Migrated {
		t.Fatalf("Recover rolled back a completed migration: %+v %v", rep2, err)
	}
}

// TestMigrateRefusesUnknownStamp: a workspace from a future version is refused,
// not guessed at.
func TestMigrateRefusesUnknownStamp(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, Dot), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, Dot, "work.md"), []byte("---\nschema: v99\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(dir); err == nil || !strings.Contains(err.Error(), "unknown schema stamp") {
		t.Fatalf("unknown stamp not refused: %v", err)
	}
}

// TestMigratePreservesUnknownJournalFields: the rewrite must be faithful to
// everything it does not understand. A field this migration has never heard of
// survives byte-for-byte, and so does key order — which is why the journal is
// decoded generically instead of through the Event struct.
func TestMigratePreservesUnknownJournalFields(t *testing.T) {
	dir := v0Fixture(t)
	line := `{"t":"2026-01-08T10:00:00Z","eid":"z9","ev":"create","id":"T-0009","k":"task","ti":"future","future_field":{"nested":[1,2,3]},"n":7}`
	p := filepath.Join(dir, ".spectackle", "journal.ndjson")
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, append(raw, []byte(line+"\n")...), 0o644); err != nil {
		t.Fatal(err)
	}

	rep, err := Run(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := read(t, dir, ".spectackle/journal.ndjson")
	want := `{"t":"2026-01-08T10:00:00Z","eid":"z9","ev":"create","id":"` +
		rep.Remap["T-0009"] + `","k":"task","ti":"future","future_field":{"nested":[1,2,3]},"n":7}`
	if !strings.Contains(got, want) {
		t.Fatalf("unknown field or key order not preserved.\nwant line: %s\ngot:\n%s", want, got)
	}
}

// Issue 178 defect 1: a config.yaml whose schema line escapes the generic
// regex (CRLF, trailing comment) must still leave the migration at To —
// and COMPLETE must be withheld if any file would stay half-stamped.
func TestForceConfigStampVariants(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"crlf", "schema: v0\r\nlangs: [go]\r\n", "schema: v1\r\nlangs: [go]\r\n"},
		{"comment", "schema: v0  # old stamp\nlangs: [go]\n", "schema: v1\nlangs: [go]\n"},
		{"plain", "schema: v0\n", "schema: v1\n"},
		{"missing", "langs: [go]\n", "schema: v1\nlangs: [go]\n"},
		{"already", "schema: v1\nlangs: [go]\n", "schema: v1\nlangs: [go]\n"},
	} {
		if got := string(forceConfigStamp([]byte(tc.in))); got != tc.want {
			t.Fatalf("%s: got %q want %q", tc.name, got, tc.want)
		}
	}
}

// The issue-178 brick, end to end: a v0 bundle whose config.yaml carries a
// CRLF schema line migrates to a FULLY v1 bundle with COMPLETE written —
// and a second open is a no-op.
func TestMigrateStampsCRLFConfig(t *testing.T) {
	dir := v0Fixture(t)
	cfg := filepath.Join(dir, Dot, "config.yaml")
	raw, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	crlf := strings.ReplaceAll(string(raw), "\n", "\r\n")
	if err := os.WriteFile(cfg, []byte(crlf), 0o644); err != nil {
		t.Fatal(err)
	}
	rep, err := Run(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Migrated {
		t.Fatal("fixture must migrate")
	}
	after, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(after), "schema: "+To) || strings.Contains(string(after), "schema: "+From) {
		t.Fatalf("config not stamped to %s:\n%s", To, after)
	}
	if _, err := os.Stat(filepath.Join(dir, Dot, backupPrefix+From, doneMarker)); err != nil {
		t.Fatal("COMPLETE must exist after a fully-stamped migration")
	}
	if rep2, err := Run(dir); err != nil || rep2.Migrated {
		t.Fatalf("second open must be a no-op: %+v %v", rep2, err)
	}
}
