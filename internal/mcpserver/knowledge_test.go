package mcpserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jxsl13/spectackle/internal/drift"
	"github.com/jxsl13/spectackle/internal/knowledge"
)

// mustEntry builds a valid, correctly-keyed Entry via knowledge.NewEntry —
// the same constructor a caller-supplied brownfield export entry goes
// through — for fixtures these tests marshal into inline artifacts.
func mustEntry(t *testing.T, kind knowledge.EntryKind, payload knowledge.Entry, source string) knowledge.Entry {
	t.Helper()
	e, err := knowledge.NewEntry(kind, payload, []knowledge.Provenance{{Source: source}}, nil)
	if err != nil {
		t.Fatalf("NewEntry: %v", err)
	}
	return e
}

func marshalArtifact(t *testing.T, a knowledge.Artifact) string {
	t.Helper()
	out, err := knowledge.Marshal(a)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	return string(out)
}

// TestKnowledgeExportExtractsWorkspace (test 1): export with no entries over
// a small workspace carrying one rule and one ADR must produce an artifact
// whose entries include both, with source == the derived module path.
func TestKnowledgeExportExtractsWorkspace(t *testing.T) {
	root := t.TempDir()
	sess := connectRoot(t, root)

	callText(t, sess, "rule", map[string]any{
		"op": "add", "dir": "pkg", "pattern": "E", "stem": "PKG",
		"system":   "the exporter",
		"response": "flush its write buffer before returning",
		"trigger":  "Close is called",
	})
	callText(t, sess, "decide", map[string]any{
		"op": "ask", "question": "which serialization format?",
		"options": []string{"yaml", "json"}, "context": "portability across tools",
	})

	out := callText(t, sess, "knowledge", map[string]any{"op": "export"})
	if !strings.Contains(out, "flush its write buffer before returning") {
		t.Fatalf("export missing the workspace rule: %q", out)
	}
	if !strings.Contains(out, "which serialization format?") {
		t.Fatalf("export missing the workspace ADR: %q", out)
	}
	wantSource := strings.TrimPrefix(moduleRepoURL(), "https://")
	if !strings.Contains(out, "sources") || !strings.Contains(out, wantSource) {
		t.Fatalf("export source should be the derived module path %q: %q", wantSource, out)
	}
	if !strings.Contains(out, "ok export "+wantSource) {
		t.Fatalf("export summary line missing derived source: %q", out)
	}
}

// TestKnowledgeExportBrownfieldKeyMatchesExtract (test 2): a caller-supplied
// entry (the brownfield path — no .spectackle bundle to extract from) is
// routed through NewEntry, so it must key identically to what Extract would
// compute for the same rule text (drift.NormHash of the trimmed sentence).
func TestKnowledgeExportBrownfieldKeyMatchesExtract(t *testing.T) {
	root := t.TempDir()
	sess := connectRoot(t, root)

	text := "The importer SHALL reject a truncated archive with a non-zero exit code."
	out := callText(t, sess, "knowledge", map[string]any{
		"op": "export",
		"entries": []map[string]any{
			{"kind": "rule", "text": text, "asserted_by": []string{"github.com/acme/legacy"}},
		},
	})
	if !strings.Contains(out, "entries=1 rule=1") {
		t.Fatalf("brownfield export should carry exactly one rule entry: %q", out)
	}

	artStart := strings.Index(out, "---")
	if artStart < 0 {
		t.Fatalf("export missing marshaled artifact: %q", out)
	}
	art, err := knowledge.Parse([]byte(out[artStart:]))
	if err != nil {
		t.Fatalf("Parse re-exported artifact: %v", err)
	}
	if len(art.Entries) != 1 {
		t.Fatalf("parsed artifact entries = %d, want 1", len(art.Entries))
	}
	wantKey := drift.NormHash([]byte(text))
	if art.Entries[0].Key != wantKey {
		t.Fatalf("brownfield entry key = %s, want %s (Extract's own hash of the same text)", art.Entries[0].Key, wantKey)
	}
}

// TestKnowledgeMergePoolsAndReportsConflict (test 3): merging two artifacts
// pools a shared rule's recurrence count and surfaces a same-question,
// different-decision ADR pair as a conflict rather than silently picking
// one answer.
func TestKnowledgeMergePoolsAndReportsConflict(t *testing.T) {
	root := t.TempDir()
	sess := connectRoot(t, root)

	sharedText := "The service SHALL retry a failed publish up to 3 times."
	a := knowledge.Artifact{Sources: []string{"repoA"}, Entries: []knowledge.Entry{
		mustEntry(t, knowledge.KindRule, knowledge.Entry{Text: sharedText}, "repoA"),
		mustEntry(t, knowledge.KindADR, knowledge.Entry{Question: "sync or async startup?", Decision: "synchronous"}, "repoA"),
	}}
	b := knowledge.Artifact{Sources: []string{"repoB"}, Entries: []knowledge.Entry{
		mustEntry(t, knowledge.KindRule, knowledge.Entry{Text: sharedText}, "repoB"),
		mustEntry(t, knowledge.KindADR, knowledge.Entry{Question: "sync or async startup?", Decision: "asynchronous"}, "repoB"),
	}}

	out := callText(t, sess, "knowledge", map[string]any{
		"op":        "merge",
		"artifacts": []string{marshalArtifact(t, a), marshalArtifact(t, b)},
	})
	if !strings.Contains(out, "conflicts=1") {
		t.Fatalf("expected exactly one conflict: %q", out)
	}
	if !strings.Contains(out, "cf adr") {
		t.Fatalf("conflict not reported as a dense cf record: %q", out)
	}
	if !strings.Contains(out, `decision="synchronous"`) || !strings.Contains(out, `decision="asynchronous"`) {
		t.Fatalf("conflict must show both answers intact, not pick one: %q", out)
	}
	// the shared rule recurs across both artifacts -> count=2 in the
	// condensate; Merge's own recurrence pooling, just surfaced here.
	if !strings.Contains(out, "count: 2") {
		t.Fatalf("shared rule's recurrence count should pool to 2: %q", out)
	}
}

// fleetArtifact is the shared fixture for the apply tests (4-7): one rule
// and one already-resolved ADR from a foreign repo, with no applies binding
// on the rule (apply never sets one — MCP-009).
func fleetArtifact(t *testing.T) string {
	t.Helper()
	a := knowledge.Artifact{Sources: []string{"github.com/acme/fleet"}, Entries: []knowledge.Entry{
		mustEntry(t, knowledge.KindRule, knowledge.Entry{
			Text: "The worker SHALL call `Ack` only after its handler returns without error.",
		}, "github.com/acme/fleet"),
		mustEntry(t, knowledge.KindADR, knowledge.Entry{
			Question: "at-least-once or exactly-once delivery?", Decision: "at-least-once",
			Consequences: "handlers must be idempotent", Options: []string{"at-least-once", "exactly-once"},
		}, "github.com/acme/fleet"),
	}}
	return marshalArtifact(t, a)
}

// TestKnowledgeApplyIntoEmptyWorkspace (test 4): entries land, the added
// rule lints clean under check, and the response reports real counts.
func TestKnowledgeApplyIntoEmptyWorkspace(t *testing.T) {
	root := t.TempDir()
	sess := connectRoot(t, root)

	out := callText(t, sess, "knowledge", map[string]any{
		"op": "apply", "artifacts": []string{fleetArtifact(t)}, "dir": "queue", "stem": "FLT",
	})
	if !strings.Contains(out, "ok added=2 skipped=0") {
		t.Fatalf("apply should add both entries with nothing skipped: %q", out)
	}
	if !strings.Contains(out, "call `Ack` only after its handler returns") {
		t.Fatalf("applied rule text missing from response: %q", out)
	}
	if !strings.Contains(out, "at-least-once or exactly-once delivery?") {
		t.Fatalf("applied ADR missing from response: %q", out)
	}

	// the new rule lints clean workspace-wide: check's own lint pass
	// (c.Findings(), independent of anchors — an applied rule has none)
	// must carry zero findings of any severity for it.
	chk := callText(t, sess, "check", map[string]any{})
	if strings.Contains(chk, "! E") || strings.Contains(chk, "! W") {
		t.Fatalf("applied rule failed lint: %q", chk)
	}

	// resolvable by ID, minted under the requested stem
	out = callText(t, sess, "get", map[string]any{"id": "FLT-001"})
	if !strings.Contains(out, "call `Ack` only after its handler returns") {
		t.Fatalf("applied rule not resolvable via get FLT-001: %q", out)
	}

	// the ADR item persisted as done, with its decision recorded (find's
	// dense i-line for items omits state — item.Record via get carries it)
	out = callText(t, sess, "get", map[string]any{"id": "ADR-0001"})
	if !strings.Contains(out, "i ADR-0001 adr done") {
		t.Fatalf("applied ADR should persist as a resolved (done) item: %q", out)
	}
	if !strings.Contains(out, "decision: at-least-once") {
		t.Fatalf("applied ADR decision not recorded: %q", out)
	}
}

// TestKnowledgeApplyTwiceIsIdempotent (test 5): re-applying the same
// artifact adds nothing the second time — the idempotence proof a fleet
// rollout depends on.
func TestKnowledgeApplyTwiceIsIdempotent(t *testing.T) {
	root := t.TempDir()
	sess := connectRoot(t, root)
	art := fleetArtifact(t)

	first := callText(t, sess, "knowledge", map[string]any{"op": "apply", "artifacts": []string{art}, "dir": "queue", "stem": "FLT"})
	if !strings.Contains(first, "ok added=2 skipped=0") {
		t.Fatalf("first apply: %q", first)
	}

	second := callText(t, sess, "knowledge", map[string]any{"op": "apply", "artifacts": []string{art}, "dir": "queue", "stem": "FLT"})
	if !strings.Contains(second, "ok added=0 skipped=2") {
		t.Fatalf("second apply must add nothing (idempotent): %q", second)
	}
}

// TestKnowledgeApplyNeverDeletes (test 6): a rule already in the workspace,
// absent from the incoming artifact, survives an apply call untouched.
func TestKnowledgeApplyNeverDeletes(t *testing.T) {
	root := t.TempDir()
	sess := connectRoot(t, root)

	callText(t, sess, "rule", map[string]any{
		"op": "add", "dir": "queue", "pattern": "U", "stem": "LOCAL",
		"system": "the local queue", "response": "persist every message to disk before acking it upstream",
	})

	callText(t, sess, "knowledge", map[string]any{"op": "apply", "artifacts": []string{fleetArtifact(t)}, "dir": "queue", "stem": "FLT"})

	out := callText(t, sess, "get", map[string]any{"id": "LOCAL-001"})
	if !strings.Contains(out, "persist every message to disk before acking it upstream") {
		t.Fatalf("apply must never delete/overwrite a pre-existing local rule: %q", out)
	}
}

// TestKnowledgeApplyGapsMatchCheck (test 7): apply's own gaps= trailer must
// equal check's actual coverage-gap count (g uncovered) for the same
// workspace state — both computed by literally the same coverageGaps walk
// (tools.go), so this is an equality proof, not an approximation. A
// deliberately UNRELATED, still-uncovered directory ("otherpkg") makes the
// count nonzero and independent of whatever dir apply itself wrote to.
func TestKnowledgeApplyGapsMatchCheck(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "otherpkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "otherpkg", "code.go"), []byte("package otherpkg\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sess := connectRoot(t, root)

	applyOut := callText(t, sess, "knowledge", map[string]any{"op": "apply", "artifacts": []string{fleetArtifact(t)}, "dir": "queue", "stem": "FLT"})
	gotGaps := extractGaps(t, applyOut)

	chk := callText(t, sess, "check", map[string]any{})
	wantGaps := strings.Count(chk, "g uncovered ")
	if wantGaps == 0 {
		t.Fatalf("test fixture broken: check should report at least the otherpkg gap: %q", chk)
	}
	if gotGaps != wantGaps {
		t.Fatalf("apply reported gaps=%d, check actually reports %d uncovered dirs: apply=%q check=%q", gotGaps, wantGaps, applyOut, chk)
	}
}

func extractGaps(t *testing.T, out string) int {
	t.Helper()
	i := strings.Index(out, "gaps=")
	if i < 0 {
		t.Fatalf("no gaps= trailer in apply output: %q", out)
	}
	rest := out[i+len("gaps="):]
	end := strings.IndexAny(rest, " \n")
	if end < 0 {
		end = len(rest)
	}
	n := 0
	for _, c := range rest[:end] {
		if c < '0' || c > '9' {
			t.Fatalf("malformed gaps= value in %q", out)
		}
		n = n*10 + int(c-'0')
	}
	return n
}

// TestKnowledgeApplyRequiresExactlyOneArtifact: apply is documented as
// folding ONE artifact — zero or several is a caller error, not silently
// picking the first or merging them implicitly (that is what op=merge is
// for).
func TestKnowledgeApplyRequiresExactlyOneArtifact(t *testing.T) {
	root := t.TempDir()
	sess := connectRoot(t, root)

	out := callText(t, sess, "knowledge", map[string]any{"op": "apply"})
	if !strings.Contains(out, "! ARG E") {
		t.Fatalf("apply with no artifact should be a caller error: %q", out)
	}

	out = callText(t, sess, "knowledge", map[string]any{
		"op": "apply", "artifacts": []string{fleetArtifact(t), fleetArtifact(t)},
	})
	if !strings.Contains(out, "! ARG E") {
		t.Fatalf("apply with two artifacts should be a caller error: %q", out)
	}
}

// TestKnowledgeExportWritesOutFile: export's out= path receives the same
// bytes returned inline, and a destination inside .spectackle is refused
// (server-owned territory).
func TestKnowledgeExportWritesOutFile(t *testing.T) {
	root := t.TempDir()
	sess := connectRoot(t, root)
	callText(t, sess, "rule", map[string]any{
		"op": "add", "dir": "", "pattern": "U", "stem": "ROOT",
		"system": "the CLI", "response": "print its version string when invoked with --version",
	})

	out := filepath.Join(root, "export.md")
	res := callText(t, sess, "knowledge", map[string]any{"op": "export", "out": out})
	if !strings.Contains(res, "ok wrote "+out) {
		t.Fatalf("export should confirm the write: %q", res)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("out file not written: %v", err)
	}
	if !strings.Contains(string(data), "print its version string") {
		t.Fatalf("out file missing the exported rule: %s", data)
	}

	blocked := callText(t, sess, "knowledge", map[string]any{
		"op": "export", "out": filepath.Join(root, ".spectackle", "sneaky.md"),
	})
	if !strings.Contains(blocked, "! ARG E") {
		t.Fatalf("export must refuse writing inside .spectackle: %q", blocked)
	}
}
