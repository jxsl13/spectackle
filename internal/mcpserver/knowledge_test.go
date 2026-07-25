package mcpserver

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/jxsl13/spectackle/internal/drift"
	"github.com/jxsl13/spectackle/internal/knowledge"
	"github.com/jxsl13/spectackle/internal/workspace"
)

// reAdded/reGaps pull the trailer counters out of a knowledge op=apply
// result (`ok applied added=<n> gaps=<n>`) for assertions that need the
// actual number, not just a substring match.
var (
	reAdded = regexp.MustCompile(`added=(\d+)`)
	reGaps  = regexp.MustCompile(`gaps=(\d+)`)

	// reLintError matches only ERROR-severity findings ("! <code> E ..."),
	// not warnings ("! <code> W ...") — spec.AddRule itself only blocks a
	// write on error severity (warnings do not), so "lint clean" here means
	// no error-severity finding, matching the codebase's own severity model.
	reLintError = regexp.MustCompile(`^! \S+ E `)
)

func mustMatchInt(t *testing.T, re *regexp.Regexp, s string) int {
	t.Helper()
	m := re.FindStringSubmatch(s)
	if m == nil {
		t.Fatalf("pattern %s not found in %q", re, s)
	}
	n := 0
	for _, c := range m[1] {
		n = n*10 + int(c-'0')
	}
	return n
}

// countGLines counts check's dense `g ` (coverage-gap) records in a check
// result — the independent ground truth knowledge apply's own `gaps=`
// trailer must match (test 7, MCP-009's counts contract).
func countGLines(out string) int {
	n := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "g ") {
			n++
		}
	}
	return n
}

// 1. export with no entries over a small seeded workspace: the artifact
// carries its rule and its ADR, and source is the derived module path
// (moduleRepoURL's own derivation, minus the https:// prefix).
func TestKnowledgeExportNoEntries(t *testing.T) {
	root := t.TempDir()
	sess := connectRoot(t, root)

	out := callText(t, sess, "rule", map[string]any{
		"op": "add", "pattern": "U", "stem": "EXP",
		"system": "the export path", "response": "carry this sentence into a portable artifact verbatim",
	})
	if !strings.Contains(out, "ok EXP-001") {
		t.Fatalf("rule add: %q", out)
	}
	callText(t, sess, "decide", map[string]any{
		"op": "ask", "question": "how should retries work?",
		"kind": "text", "context": "jobs fail transiently",
	})
	callText(t, sess, "decide", map[string]any{
		"op": "answer", "id": "ADR-0001", "choose": "retry up to 3 times with backoff",
		"consequences": "slightly higher tail latency",
	})

	out = callText(t, sess, "knowledge", map[string]any{"op": "export"})
	if !strings.Contains(out, "ok export entries=2 rule=1 adr=1 intent=0") {
		t.Fatalf("export trailer: %q", out)
	}
	wantSource := strings.TrimPrefix(moduleRepoURL(), "https://")
	if !strings.Contains(out, "sources:\n    - "+wantSource) && !strings.Contains(out, wantSource) {
		t.Fatalf("export missing derived module-path source %q: %q", wantSource, out)
	}
	if !strings.Contains(out, "carry this sentence into a portable artifact verbatim") {
		t.Fatalf("export missing the rule's sentence: %q", out)
	}
	if !strings.Contains(out, "how should retries work?") {
		t.Fatalf("export missing the ADR's question: %q", out)
	}
}

// 2. export with caller-supplied entries (brownfield, no bundle): entries
// are validated and keyed through knowledge.NewEntry — a supplied key is
// impossible by construction (knowledgeEntryIn carries no key field at
// all) — assert the resulting key equals what Extract would compute for
// the same text (drift.NormHash).
func TestKnowledgeExportBrownfieldEntries(t *testing.T) {
	root := t.TempDir()
	sess := connectRoot(t, root)

	ruleText := "The system SHALL log to stderr only."
	out := callText(t, sess, "knowledge", map[string]any{
		"op": "export",
		"entries": []map[string]any{
			{"kind": "rule", "text": ruleText, "rationale": "single-stream container logs"},
		},
	})
	if !strings.Contains(out, "ok export entries=1 rule=1 adr=0 intent=0") {
		t.Fatalf("brownfield export trailer: %q", out)
	}

	artifactPart, _, ok := strings.Cut(out, "\nok export")
	if !ok {
		t.Fatalf("could not split artifact body from trailer: %q", out)
	}
	a, err := knowledge.Parse([]byte(artifactPart + "\n"))
	if err != nil {
		t.Fatalf("re-parsing the exported artifact: %v\n%s", err, artifactPart)
	}
	if len(a.Entries) != 1 {
		t.Fatalf("want 1 entry, got %d: %+v", len(a.Entries), a.Entries)
	}
	want := drift.NormHash([]byte(ruleText))
	if a.Entries[0].Key != want {
		t.Fatalf("brownfield entry key = %q, want %q (Extract's own formula)", a.Entries[0].Key, want)
	}
	wantSource := strings.TrimPrefix(moduleRepoURL(), "https://")
	if len(a.Entries[0].Sources) != 1 || a.Entries[0].Sources[0].Source != wantSource {
		t.Fatalf("brownfield entry source = %+v, want %q", a.Entries[0].Sources, wantSource)
	}
}

// TestKnowledgeExportBrownfieldRejectsMalformed: NewEntry's own validation
// (non-empty required fields) surfaces as an ARG error, not a silently
// coerced entry.
func TestKnowledgeExportBrownfieldRejectsMalformed(t *testing.T) {
	root := t.TempDir()
	sess := connectRoot(t, root)
	out := callText(t, sess, "knowledge", map[string]any{
		"op":      "export",
		"entries": []map[string]any{{"kind": "rule", "text": "   "}},
	})
	if !strings.HasPrefix(out, "! ARG E -") {
		t.Fatalf("malformed brownfield entry should reject: %q", out)
	}
}

// 3. merge of two artifacts: recurrence counts pool across artifacts, and a
// same-question-different-decision ADR is reported as a conflict (`x`
// record), never silently merged away.
func TestKnowledgeMergeConflict(t *testing.T) {
	root := t.TempDir()
	sess := connectRoot(t, root)

	widely := "The system SHALL check errors after every syscall."
	a := knowledge.Artifact{Sources: []string{"acme/repoA"}, Entries: []knowledge.Entry{
		{Kind: knowledge.KindRule, Text: widely, Count: 1,
			Sources: []knowledge.Provenance{{Source: "acme/repoA"}}, Key: drift.NormHash([]byte(widely))},
		{Kind: knowledge.KindADR, Question: "how should retries work?", Decision: "retry 3 times", Count: 1,
			Sources: []knowledge.Provenance{{Source: "acme/repoA"}}, Key: drift.NormHash([]byte("how should retries work?"))},
	}}
	b := knowledge.Artifact{Sources: []string{"acme/repoB"}, Entries: []knowledge.Entry{
		{Kind: knowledge.KindRule, Text: widely, Count: 1,
			Sources: []knowledge.Provenance{{Source: "acme/repoB"}}, Key: drift.NormHash([]byte(widely))},
		{Kind: knowledge.KindADR, Question: "how should retries work?", Decision: "retry once, then give up", Count: 1,
			Sources: []knowledge.Provenance{{Source: "acme/repoB"}}, Key: drift.NormHash([]byte("how should retries work?"))},
	}}
	rawA, err := knowledge.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	rawB, err := knowledge.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	pathA := writeTempArtifact(t, root, "a.md", rawA)
	pathB := writeTempArtifact(t, root, "b.md", rawB)

	out := callText(t, sess, "knowledge", map[string]any{
		"op": "merge", "paths": []string{pathA, pathB},
	})
	if !strings.Contains(out, "ok merge sources=2 entries=1 conflicts=1") {
		t.Fatalf("merge trailer: %q", out)
	}
	// the widely-asserted rule survives merge with recurrence pooled to 2.
	if !strings.Contains(out, "count: 2") {
		t.Fatalf("merge did not pool recurrence: %q", out)
	}
	// both competing ADR answers surface as x records — neither silently wins.
	if !strings.Contains(out, "retry 3 times") || !strings.Contains(out, "retry once, then give up") {
		t.Fatalf("merge conflict did not surface both competing decisions: %q", out)
	}
	if strings.Count(out, "x adr ") != 2 {
		t.Fatalf("want 2 conflict records (one per competing entry), got %d: %q", strings.Count(out, "x adr "), out)
	}
}

// 4. apply into an empty workspace: entries land, rules lint clean
// afterwards, counts are reported.
func TestKnowledgeApplyEmptyWorkspace(t *testing.T) {
	root := t.TempDir()
	sess := connectRoot(t, root)

	body := sampleImportArtifact(t)
	out := callText(t, sess, "knowledge", map[string]any{"op": "apply", "body": body})
	added := mustMatchInt(t, reAdded, out)
	if added != 3 {
		t.Fatalf("want added=3 (2 rules + 1 adr), got added=%d: %q", added, out)
	}

	checkOut := callText(t, sess, "check", map[string]any{})
	for _, line := range strings.Split(checkOut, "\n") {
		if reLintError.MatchString(line) {
			t.Fatalf("applied entries did not lint clean (error severity): %q in %q", line, checkOut)
		}
	}
}

// 5. apply twice: the second call adds nothing — the idempotence proof.
func TestKnowledgeApplyTwiceIsIdempotent(t *testing.T) {
	root := t.TempDir()
	sess := connectRoot(t, root)

	body := sampleImportArtifact(t)
	first := callText(t, sess, "knowledge", map[string]any{"op": "apply", "body": body})
	if n := mustMatchInt(t, reAdded, first); n == 0 {
		t.Fatalf("first apply added nothing, test is vacuous: %q", first)
	}

	second := callText(t, sess, "knowledge", map[string]any{"op": "apply", "body": body})
	if n := mustMatchInt(t, reAdded, second); n != 0 {
		t.Fatalf("second apply of the SAME artifact: want added=0, got added=%d: %q", n, second)
	}
}

// 6. apply never deletes: a workspace rule absent from the incoming
// artifact is still present after apply.
func TestKnowledgeApplyNeverDeletes(t *testing.T) {
	root := t.TempDir()
	sess := connectRoot(t, root)

	callText(t, sess, "rule", map[string]any{
		"op": "add", "pattern": "U", "stem": "LOCAL",
		"system": "this repository's own convention", "response": "stay put across every apply call",
	})

	callText(t, sess, "knowledge", map[string]any{"op": "apply", "body": sampleImportArtifact(t)})

	out := callText(t, sess, "get", map[string]any{"id": "LOCAL-001"})
	if !strings.Contains(out, "stay put across every apply call") {
		t.Fatalf("local rule absent from the artifact was deleted by apply: %q", out)
	}
}

// 7. gap reporting: after apply, the reported gap count matches what check
// actually reports — apply must recompute the same two gap-producing
// checks (uncovered dirs + orphan applies) check itself runs, not guess.
func TestKnowledgeApplyGapReportingMatchesCheck(t *testing.T) {
	root := t.TempDir()
	sess := connectRoot(t, root)
	ws := workspace.Root{Dir: root}

	// seed a genuine orphan gap unrelated to what apply is about to add
	// (same technique as TestCheckOrphanApplies): a rule anchored to two
	// nodes, then one anchor row dropped out from under it.
	out := callText(t, sess, "rule", map[string]any{
		"op": "add", "dir": "orphan_probe", "pattern": "U", "stem": "ORP-PRB",
		"system":   "the orphan prober",
		"response": "anchor to two nodes for the gap-reporting test",
		"applies":  []string{"go:pkg.A", "go:pkg.B"},
	})
	if !strings.Contains(out, "ok ORP-PRB-001") {
		t.Fatalf("rule add: %q", out)
	}
	anchors, err := drift.Load(ws)
	if err != nil {
		t.Fatal(err)
	}
	var kept []drift.Anchor
	for _, a := range anchors {
		if a.Rule == "ORP-PRB-001" && a.Node == "go:pkg.A" {
			continue
		}
		kept = append(kept, a)
	}
	if err := drift.Save(ws, kept); err != nil {
		t.Fatal(err)
	}

	applyOut := callText(t, sess, "knowledge", map[string]any{"op": "apply", "body": sampleImportArtifact(t)})
	gaps := mustMatchInt(t, reGaps, applyOut)

	checkOut := callText(t, sess, "check", map[string]any{})
	want := countGLines(checkOut)
	if gaps != want {
		t.Fatalf("apply reported gaps=%d, check itself reports %d g-lines:\napply: %q\ncheck: %q", gaps, want, applyOut, checkOut)
	}
	if want == 0 {
		t.Fatalf("test is vacuous: check reported no g-lines at all")
	}
}

// sampleImportArtifact builds a small, fixed, marshaled artifact (2 rules +
// 1 ADR, all unanchored/unscoped as any portable artifact's entries are) for
// the apply tests above.
func sampleImportArtifact(t *testing.T) string {
	t.Helper()
	r1 := "The build SHALL pin every dependency version."
	r2 := "The system SHALL check errors after every syscall."
	q := "how should retries work?"
	a := knowledge.Artifact{Sources: []string{"acme/upstream"}, Entries: []knowledge.Entry{
		{Kind: knowledge.KindRule, Text: r1, Count: 1,
			Sources: []knowledge.Provenance{{Source: "acme/upstream"}}, Key: drift.NormHash([]byte(r1))},
		{Kind: knowledge.KindRule, Text: r2, Count: 1,
			Sources: []knowledge.Provenance{{Source: "acme/upstream"}}, Key: drift.NormHash([]byte(r2))},
		{Kind: knowledge.KindADR, Question: q, Decision: "retry up to 3 times with backoff", Status: "accepted", Count: 1,
			Sources: []knowledge.Provenance{{Source: "acme/upstream"}}, Key: drift.NormHash([]byte(q))},
	}}
	raw, err := knowledge.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func writeTempArtifact(t *testing.T, dir, name string, raw []byte) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}
