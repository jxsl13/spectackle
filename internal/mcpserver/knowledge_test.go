package mcpserver

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jxsl13/spectackle/internal/drift"
	"github.com/jxsl13/spectackle/internal/item"
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
	// the source identifies THE WORKSPACE, not the running binary's module
	// (B-01KYMCHNJCFBP) — with no git remote it falls back to the
	// workspace directory name, which is what this fixture exercises.
	wantSource := filepath.Base(root)
	if !strings.Contains(out, wantSource) {
		t.Fatalf("export missing the workspace source %q: %q", wantSource, out)
	}
	if strings.Contains(out, "jxsl13/spectackle") {
		t.Fatalf("export must not stamp the running binary's module as the source: %q", out)
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
	wantSource := filepath.Base(root)
	if len(a.Entries[0].Sources) != 1 || a.Entries[0].Sources[0].Source != wantSource {
		t.Fatalf("brownfield entry source = %+v, want the workspace label %q", a.Entries[0].Sources, wantSource)
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

// TestExportSourceIdentifiesTheWorkspace pins B-01KYMCHNJCFBP (found by
// an independent gap hunt): export used to stamp the RUNNING BINARY's
// module path, so every repository a shared installation touched exported
// the identical source — merges reported sources=1, a rule found in two
// repos counted once, and both sides of a conflict rendered the same src=.
func TestExportSourceIdentifiesTheWorkspace(t *testing.T) {
	srcOf := func(t *testing.T, root string) string {
		t.Helper()
		sess := connectRoot(t, root)
		out := callText(t, sess, "knowledge", map[string]any{"op": "export"})
		for _, l := range strings.Split(out, "\n") {
			if rest, ok := strings.CutPrefix(strings.TrimSpace(l), "- "); ok && strings.Contains(l, "sources") {
				return rest
			}
			if rest, ok := strings.CutPrefix(strings.TrimSpace(l), "source: "); ok {
				return rest
			}
		}
		// the artifact renders sources as a yaml list; take the first
		// indented entry after the sources: key
		lines := strings.Split(out, "\n")
		for i, l := range lines {
			if strings.HasPrefix(strings.TrimSpace(l), "sources:") && i+1 < len(lines) {
				return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(lines[i+1]), "-"))
			}
		}
		t.Fatalf("no source in the exported artifact:\n%s", out)
		return ""
	}
	seed := func(t *testing.T, name string) string {
		t.Helper()
		root := filepath.Join(t.TempDir(), name)
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "x.go"), []byte("package x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		sess := connectRoot(t, root)
		callText(t, sess, "rule", map[string]any{
			"op": "add", "dir": "", "pattern": "U", "stem": "SRC",
			"system": "the " + name + " probe", "response": "return exactly 1",
		})
		return root
	}
	a, b := seed(t, "alpha-repo"), seed(t, "beta-repo")
	sa, sb := srcOf(t, a), srcOf(t, b)
	if sa == sb {
		t.Fatalf("two unrelated workspaces exported the SAME source %q — provenance cannot distinguish them", sa)
	}
	if !strings.Contains(sa, "alpha-repo") || !strings.Contains(sb, "beta-repo") {
		t.Fatalf("each source must identify its own workspace: %q / %q", sa, sb)
	}
}

// TestNormalizeRepoLabelCollapsesCloneURLShapes: two clones of ONE
// repository must never look like two sources.
func TestNormalizeRepoLabelCollapsesCloneURLShapes(t *testing.T) {
	want := "github.com/o/r"
	for _, u := range []string{
		"https://github.com/o/r.git", "https://github.com/o/r",
		"git@github.com:o/r.git", "ssh://git@github.com/o/r.git",
		"https://user:tok@github.com/o/r.git", "https://github.com/o/r/",
	} {
		if got := normalizeRepoLabel(u); got != want {
			t.Errorf("normalizeRepoLabel(%q) = %q, want %q", u, got, want)
		}
	}
	if got := normalizeRepoLabel("  "); got != "" {
		t.Errorf("an empty remote must yield an empty label, got %q", got)
	}
	// a self-hosted remote reached with and without an explicit SSH port
	// is still ONE source (cross-val-prov WARN)
	for _, u := range []string{
		"ssh://git@example.com:2222/o/r.git", "ssh://git@example.com/o/r.git",
		"git@example.com:o/r.git",
	} {
		if got := normalizeRepoLabel(u); got != "example.com/o/r" {
			t.Errorf("normalizeRepoLabel(%q) = %q, want %q", u, got, "example.com/o/r")
		}
	}
}

// TestArtifactPathsResolveAgainstTheWorkspace pins B-01KYMCJG8HFYW: a
// relative artifact path used to hit the SERVER PROCESS's cwd, so an
// independent gap hunt exporting with path=kb-export.md wrote the file
// into the spectackle source repo instead of the workspace it named.
func TestArtifactPathsResolveAgainstTheWorkspace(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "x.go"), []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sess := connectRoot(t, root)
	callText(t, sess, "rule", map[string]any{
		"op": "add", "dir": "", "pattern": "U", "stem": "PTH",
		"system": "the path probe", "response": "return exactly 1",
	})

	// export with a RELATIVE path lands under the workspace, not the cwd
	out := callText(t, sess, "knowledge", map[string]any{"op": "export", "path": "kb.md"})
	if !strings.Contains(out, "written=kb.md") {
		t.Fatalf("export did not report the write: %q", out)
	}
	if _, err := os.Stat(filepath.Join(root, "kb.md")); err != nil {
		t.Fatalf("the artifact must land under the workspace: %v", err)
	}
	cwd, _ := os.Getwd()
	if _, err := os.Stat(filepath.Join(cwd, "kb.md")); err == nil {
		t.Fatal("the artifact leaked into the process cwd")
	}
	// and apply reads the same relative path back
	if out := callText(t, sess, "knowledge", map[string]any{"op": "apply", "path": "kb.md"}); !strings.Contains(out, "ok applied") {
		t.Fatalf("apply must read the workspace-relative artifact: %q", out)
	}
	// an absolute path still works verbatim
	abs := filepath.Join(t.TempDir(), "abs.md")
	if out := callText(t, sess, "knowledge", map[string]any{"op": "export", "path": abs}); !strings.Contains(out, "written="+abs) {
		t.Fatalf("absolute export: %q", out)
	}
	if _, err := os.Stat(abs); err != nil {
		t.Fatalf("the absolute artifact must land verbatim: %v", err)
	}
	// an empty artifact says so instead of blaming the schema version
	empty := filepath.Join(root, "empty.md")
	if err := os.WriteFile(empty, []byte("   \n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out := callText(t, sess, "knowledge", map[string]any{"op": "apply", "path": "empty.md"}); !strings.Contains(out, "empty artifact") {
		t.Fatalf("an empty artifact must be diagnosed as empty: %q", out)
	}
}

// TestApplyMintsADecisionPerConflict implements ADR-01KYMKEG7YE2P: a
// merge conflict between two artifacts must become an OPEN DECISION in
// the applying workspace, never a silently dropped entry (both sides
// used to vanish, since merge strips conflicts from its condensate).
func TestApplyMintsADecisionPerConflict(t *testing.T) {
	// two artifacts: one shared rule (no conflict) and one question two
	// sources answer differently
	_, sess := conflictingArtifacts(t)

	out := callText(t, sess, "knowledge", map[string]any{"op": "apply", "paths": []string{"a.md", "b.md"}})
	if !strings.Contains(out, "conflicts=1") {
		t.Fatalf("the conflict must be counted in the trailer:\n%s", out)
	}
	if !strings.Contains(out, "need decision ") || !strings.Contains(out, "serialization") {
		t.Fatalf("the conflict must open a decision naming its question:\n%s", out)
	}
	// the NON-conflicting union still applied
	if !strings.Contains(callText(t, sess, "find", map[string]any{"q": "shared module", "scope": "rule"}), "SHALL return exactly 1") {
		t.Fatal("the non-conflicting rule must still apply")
	}
	// and NEITHER decision was silently adopted
	adrs := callText(t, sess, "find", map[string]any{"q": "serialization", "scope": "adr"})
	if strings.Contains(adrs, "choice: protobuf") || strings.Contains(adrs, "choice: json") {
		t.Fatalf("no side may be auto-adopted:\n%s", adrs)
	}

	// answering lands the winner as a real accepted ADR, loser retained
	adrID := ""
	for _, f := range strings.Fields(out) {
		if strings.HasPrefix(f, "ADR-") {
			adrID = f
		}
	}
	if adrID == "" {
		t.Fatalf("no ADR id in the render:\n%s", out)
	}
	if got := callText(t, sess, "decide", map[string]any{"op": "answer", "id": adrID, "choose": "repo-a: protobuf"}); !strings.Contains(got, "ok "+adrID) {
		t.Fatalf("answering the conflict decision: %q", got)
	}
	body := callText(t, sess, "get", map[string]any{"id": adrID})
	if !strings.Contains(body, "choice: repo-a: protobuf") {
		t.Fatalf("the winning decision must land: %q", body)
	}
	if !strings.Contains(body, "repo-b: json") {
		t.Fatalf("the LOSING side must stay readable in the record: %q", body)
	}

	// Idempotency, asserted as EXACT equality of the minted-ID set. The
	// round-1 form of this check allowed "at most one new ADR-", which is
	// precisely what one duplicate produces — it passed while a re-apply
	// was minting a second decision for a question already answered.
	before := decisionIDs(t, sess, "serialization")
	out2 := callText(t, sess, "knowledge", map[string]any{"op": "apply", "paths": []string{"a.md", "b.md"}})
	after := decisionIDs(t, sess, "serialization")
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("re-apply must mint no second decision: %v -> %v\n%s", before, after, out2)
	}
	// ...and says so: the sources still disagree, this workspace simply
	// already holds the answer
	if !strings.Contains(out2, "settled=1") || strings.Contains(out2, "conflicts=") {
		t.Fatalf("a settled conflict is counted as settled, not re-asked:\n%s", out2)
	}
}

// decisionIDs is the set of ADR ids find reports for a query — the exact
// unit the idempotency assertion compares.
func decisionIDs(t *testing.T, sess *mcp.ClientSession, q string) []string {
	t.Helper()
	var ids []string
	for _, f := range strings.Fields(callText(t, sess, "find", map[string]any{"q": q, "scope": "adr"})) {
		if strings.HasPrefix(f, "ADR-") {
			ids = append(ids, f)
		}
	}
	// The render abbreviates IDs adaptively, so the same record can appear
	// at two widths in one result; fold to the shortest prefix so the set
	// compares records, not renderings.
	sort.Strings(ids)
	var out []string
	for _, id := range ids {
		if len(out) > 0 && strings.HasPrefix(id, out[len(out)-1]) {
			continue
		}
		out = append(out, id)
	}
	return out
}

// TestReApplyBeforeAnsweringMintsOneDecision: the duplicate-suppression
// must hold while the decision is still OPEN, not only once answered — the
// re-apply case an agent actually hits, having been told to answer and
// not yet having done it. Two unlinked ADRs asking the identical question
// is the worst outcome available here: neither is authoritative.
func TestReApplyBeforeAnsweringMintsOneDecision(t *testing.T) {
	root, sess := conflictingArtifacts(t)
	_ = root
	first := callText(t, sess, "knowledge", map[string]any{"op": "apply", "paths": []string{"a.md", "b.md"}})
	if !strings.Contains(first, "conflicts=1") {
		t.Fatalf("setup: %s", first)
	}
	opened := decisionIDs(t, sess, "serialization")
	second := callText(t, sess, "knowledge", map[string]any{"op": "apply", "paths": []string{"a.md", "b.md"}})
	if got := decisionIDs(t, sess, "serialization"); !reflect.DeepEqual(opened, got) {
		t.Fatalf("an unanswered decision must not be asked twice: %v -> %v\n%s", opened, got, second)
	}
}

// TestSingleArtifactCarryingBothSidesOpensDecision: the conflict safety net
// cannot be gated on the number of artifacts. `knowledge export` of a
// workspace that answered the same question twice emits ONE artifact
// carrying both answers, so an artifact count is not a conflict count —
// and the single-artifact route dropping a side silently is this feature's
// own bug, reached through this feature's own tool.
func TestSingleArtifactCarryingBothSidesOpensDecision(t *testing.T) {
	root := t.TempDir()
	one := "---\nschema: v1\nkind: knowledge\nsources:\n    - repo-a\n    - repo-b\n---\n\n" +
		"## adr 2222222222222222\nquestion: which serialization should the rpc layer use?\ndecision: protobuf\nstatus: accepted\ncount: 1\nsources:\n    - source: repo-a\n      dir: \"\"\n\n" +
		"## adr 2222222222222222\nquestion: which serialization should the rpc layer use?\ndecision: json\nstatus: accepted\ncount: 1\nsources:\n    - source: repo-b\n      dir: \"\"\n"
	if err := os.WriteFile(filepath.Join(root, "both.md"), []byte(one), 0o644); err != nil {
		t.Fatal(err)
	}
	sess := connectRoot(t, root)
	for _, in := range []map[string]any{
		{"op": "apply", "path": "both.md"},
		{"op": "apply", "body": one},
	} {
		fresh := connectRoot(t, t.TempDir())
		if in["body"] == nil {
			fresh = sess
		}
		out := callText(t, fresh, "knowledge", in)
		if !strings.Contains(out, "conflicts=1") || !strings.Contains(out, "need decision ") {
			t.Fatalf("one artifact carrying both sides must open a decision (%v):\n%s", in, out)
		}
		if strings.Contains(out, "choice: protobuf") || strings.Contains(out, "choice: json") {
			t.Fatalf("no side may be adopted (%v):\n%s", in, out)
		}
	}
}

// TestEmptyDecisionSideIsNeverBlank: a source that asked but never answered
// is still a side, and it must not render as a choosable-but-empty option.
func TestEmptyDecisionSideIsNeverBlank(t *testing.T) {
	root := t.TempDir()
	one := "---\nschema: v1\nkind: knowledge\nsources:\n    - repo-a\n    - repo-b\n---\n\n" +
		"## adr 2222222222222222\nquestion: which serialization should the rpc layer use?\ndecision: protobuf\nstatus: accepted\ncount: 1\nsources:\n    - source: repo-a\n      dir: \"\"\n\n" +
		"## adr 2222222222222222\nquestion: which serialization should the rpc layer use?\nstatus: proposed\ncount: 1\nsources:\n    - source: repo-b\n      dir: \"\"\n"
	if err := os.WriteFile(filepath.Join(root, "one.md"), []byte(one), 0o644); err != nil {
		t.Fatal(err)
	}
	sess := connectRoot(t, root)
	out := callText(t, sess, "knowledge", map[string]any{"op": "apply", "path": "one.md"})
	adrID := ""
	for _, f := range strings.Fields(out) {
		if strings.HasPrefix(f, "ADR-") {
			adrID = f
		}
	}
	if adrID == "" {
		t.Fatalf("the conflict must open a decision:\n%s", out)
	}
	body := callText(t, sess, "get", map[string]any{"id": adrID})
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "option:") &&
			strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(line), ":")) == "option: repo-b" {
			t.Fatalf("an undecided side must not render as a blank option: %q\n%s", line, body)
		}
	}
	if !strings.Contains(body, "repo-b: (no decision recorded)") {
		t.Fatalf("the undecided side must stay visible and named:\n%s", body)
	}
}

// conflictingArtifacts writes the two-source fixture (one shared rule, one
// question answered differently) and returns a session on a fresh root.
func conflictingArtifacts(t *testing.T) (string, *mcp.ClientSession) {
	t.Helper()
	art := func(src, decision string) string {
		return "---\nschema: v1\nkind: knowledge\nsources:\n    - " + src + "\n---\n\n" +
			"## rule 1111111111111111\ntext: The shared module SHALL return exactly 1.\ncount: 1\nsources:\n    - source: " + src + "\n      dir: \"\"\n\n" +
			"## adr 2222222222222222\nquestion: which serialization should the rpc layer use?\ndecision: " + decision + "\nstatus: accepted\ncount: 1\nsources:\n    - source: " + src + "\n      dir: \"\"\n"
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.md"), []byte(art("repo-a", "protobuf")), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "b.md"), []byte(art("repo-b", "json")), 0o644); err != nil {
		t.Fatal(err)
	}
	return root, connectRoot(t, root)
}

// TestArchivedDecisionKeepsBothSides is the regression for the defect that
// made this whole feature a no-op: archive() retained a tombstone body only
// for kind=research, and a decide-minted ADR's first body line is the
// machine field "kind: radio" — so every conflict decision archived to the
// byte-identical, contentless summary "adr <question> — kind: radio" and
// BOTH sides became unrecoverable from get, from work.md and from find.
// Reachable with no user intent at all: the project's own compact
// housekeeping archives resolved decisions.
func TestArchivedDecisionKeepsBothSides(t *testing.T) {
	_, sess := conflictingArtifacts(t)
	out := callText(t, sess, "knowledge", map[string]any{"op": "apply", "paths": []string{"a.md", "b.md"}})
	adrID := ""
	for _, f := range strings.Fields(out) {
		if strings.HasPrefix(f, "ADR-") {
			adrID = f
		}
	}
	if adrID == "" {
		t.Fatalf("setup: no decision minted:\n%s", out)
	}
	if got := callText(t, sess, "decide", map[string]any{"op": "answer", "id": adrID, "choose": "repo-a: protobuf"}); !strings.Contains(got, "ok "+adrID) {
		t.Fatalf("setup: answering: %q", got)
	}
	if got := callText(t, sess, "move", map[string]any{"id": adrID, "to": "archived", "note": "curated"}); strings.Contains(got, "! ") {
		t.Fatalf("setup: archiving: %q", got)
	}

	tomb := callText(t, sess, "get", map[string]any{"id": adrID})
	if !strings.Contains(tomb, "journal tombstone") {
		t.Fatalf("setup: expected a tombstone: %q", tomb)
	}
	if !strings.Contains(tomb, "repo-a: protobuf") {
		t.Fatalf("the WINNER must survive archiving:\n%s", tomb)
	}
	if !strings.Contains(tomb, "repo-b: json") {
		t.Fatalf("the LOSER must survive archiving — that is the whole promise:\n%s", tomb)
	}
	if !strings.Contains(tomb, "decision: repo-a: protobuf") {
		t.Fatalf("which side WON must survive: the ADR fields have no journal channel of their own:\n%s", tomb)
	}
	// and both sides stay searchable, not merely fetchable by an ID the
	// reader would have to already know
	if hist := callText(t, sess, "find", map[string]any{"q": "json", "scope": "history"}); !strings.Contains(hist, adrID) {
		t.Fatalf("the losing side must stay findable in history:\n%s", hist)
	}
}

// TestApplySingleArtifactUnchanged: the new multi-artifact branch must
// not disturb the one-artifact path.
func TestApplySingleArtifactUnchanged(t *testing.T) {
	root := t.TempDir()
	body := "---\nschema: v1\nkind: knowledge\nsources:\n    - solo\n---\n\n" +
		"## rule 3333333333333333\ntext: The solo module SHALL return exactly 2.\ncount: 1\nsources:\n    - source: solo\n      dir: \"\"\n"
	sess := connectRoot(t, root)
	out := callText(t, sess, "knowledge", map[string]any{"op": "apply", "body": body})
	if !strings.Contains(out, "ok applied added=1") || strings.Contains(out, "conflicts=") {
		t.Fatalf("single-artifact apply must be unchanged: %q", out)
	}
}

// TestArchivedDecisionStaysSettled: a workspace that answered a conflict and
// then ARCHIVED the decision — the normal, correct end of the lifecycle —
// must not be asked the same question again on the next import. Extract
// reads work.md, which an archived record has left, so the settled check
// has to consult tombstones as well; without it the only workspace that
// loops is the one that did the curation properly.
func TestArchivedDecisionStaysSettled(t *testing.T) {
	_, sess := conflictingArtifacts(t)
	out := callText(t, sess, "knowledge", map[string]any{"op": "apply", "paths": []string{"a.md", "b.md"}})
	adrID := ""
	for _, f := range strings.Fields(out) {
		if strings.HasPrefix(f, "ADR-") {
			adrID = f
		}
	}
	if adrID == "" {
		t.Fatalf("setup: no decision minted:\n%s", out)
	}
	if got := callText(t, sess, "decide", map[string]any{"op": "answer", "id": adrID, "choose": "repo-a: protobuf"}); !strings.Contains(got, "ok "+adrID) {
		t.Fatalf("setup: answering: %q", got)
	}
	if got := callText(t, sess, "move", map[string]any{"id": adrID, "to": "archived", "note": "curated"}); strings.Contains(got, "! ") {
		t.Fatalf("setup: archiving: %q", got)
	}

	again := callText(t, sess, "knowledge", map[string]any{"op": "apply", "paths": []string{"a.md", "b.md"}})
	if strings.Contains(again, "need decision") || strings.Contains(again, "conflicts=") {
		t.Fatalf("an archived decision must stay settled, not be re-asked:\n%s", again)
	}
	if !strings.Contains(again, "settled=1") {
		t.Fatalf("the still-disagreeing sources must be reported as settled:\n%s", again)
	}
}

// TestDivergingImportIsReported: FoldInto skips an incoming entry whose
// identity the workspace already holds, which is correct precedence but on
// its own cannot be told apart from agreement. An import that CONTRADICTS a
// decision already held must be reported — silently dropping it is the same
// shape as the loss this feature exists to close, on the one-artifact side.
func TestDivergingImportIsReported(t *testing.T) {
	root := t.TempDir()
	sess := connectRoot(t, root)
	// the workspace reaches its own answer first
	ask := callText(t, sess, "decide", map[string]any{
		"op": "ask", "question": "which serialization should the rpc layer use?", "kind": "text",
	})
	local := ""
	for _, f := range strings.Fields(ask) {
		if strings.HasPrefix(f, "ADR-") {
			local = f
		}
	}
	if local == "" {
		t.Fatalf("setup: %s", ask)
	}
	callText(t, sess, "decide", map[string]any{"op": "answer", "id": local, "choose": "protobuf"})

	// an import disagrees
	incoming := "---\nschema: v1\nkind: knowledge\nsources:\n    - repo-b\n---\n\n" +
		"## adr 2222222222222222\nquestion: which serialization should the rpc layer use?\ndecision: json\nstatus: accepted\ncount: 1\nsources:\n    - source: repo-b\n      dir: \"\"\n"
	out := callText(t, sess, "knowledge", map[string]any{"op": "apply", "body": incoming})
	if !strings.Contains(out, "diverged=1") {
		t.Fatalf("a contradicting import must not vanish without a word:\n%s", out)
	}
	if !strings.Contains(out, "ours=\"protobuf\"") || !strings.Contains(out, "theirs=\"json\"") {
		t.Fatalf("the divergence must name both sides:\n%s", out)
	}
	// precedence is unchanged: ours still stands, theirs is NOT adopted
	if !strings.Contains(out, "kept ours") {
		t.Fatalf("the render must say which side stands:\n%s", out)
	}
	if body := callText(t, sess, "get", map[string]any{"id": local}); !strings.Contains(body, "protobuf") || strings.Contains(body, "choice: json") {
		t.Fatalf("the local decision must stand: %q", body)
	}
	// an import that AGREES stays quiet
	agreeing := strings.Replace(incoming, "decision: json", "decision: protobuf", 1)
	quiet := callText(t, sess, "knowledge", map[string]any{"op": "apply", "body": agreeing})
	if strings.Contains(quiet, "diverged=") || strings.Contains(quiet, "x adr") {
		t.Fatalf("agreement is not a divergence:\n%s", quiet)
	}
}

// TestUnansweredDecisionGistIsNotAMachineField: archiving a decision nobody
// answered used to write "kind: radio" as the spec.md intent line and the
// journal summary — byte-identical across every parked decision the
// repository ever had, and therefore useless as history.
func TestUnansweredDecisionGistIsNotAMachineField(t *testing.T) {
	root, sess := conflictingArtifacts(t)
	out := callText(t, sess, "knowledge", map[string]any{"op": "apply", "paths": []string{"a.md", "b.md"}})
	adrID := ""
	for _, f := range strings.Fields(out) {
		if strings.HasPrefix(f, "ADR-") {
			adrID = f
		}
	}
	if adrID == "" {
		t.Fatalf("setup: %s", out)
	}
	// archive it WITHOUT answering
	if got := callText(t, sess, "move", map[string]any{"id": adrID, "to": "archived", "note": ""}); strings.Contains(got, "! ") {
		t.Fatalf("setup: archiving unanswered: %q", got)
	}
	specMD, err := os.ReadFile(filepath.Join(root, ".spectackle", "spec.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(specMD), "\n") {
		if strings.Contains(line, adrID) && strings.Contains(line, "kind: radio") {
			t.Fatalf("the permanent history line must not be a machine field: %q", line)
		}
	}
	if hist := callText(t, sess, "find", map[string]any{"q": "kind: radio", "scope": "history"}); strings.Contains(hist, "— kind: radio") {
		t.Fatalf("nor the journal summary:\n%s", hist)
	}
}

// TestConcurrentApplyMintsOneDecision: the settle check and the mint are not
// one atomic step, so two agents applying the same pair at once both found
// the board empty and both minted — two unlinked ADRs asking the identical
// question, neither authoritative. Resolution is yield-always and one-sided
// (IDs are time-ordered, so "an earlier one exists, therefore mine is the
// duplicate" needs no agreement from the other side), which is the same
// shape the worktree-lease tiebreak uses.
func TestConcurrentApplyMintsOneDecision(t *testing.T) {
	root, sess := conflictingArtifacts(t)
	_ = root
	second := connectRoot(t, root)

	var wg sync.WaitGroup
	outs := make([]string, 2)
	for i, s := range []*mcp.ClientSession{sess, second} {
		wg.Add(1)
		go func(i int, s *mcp.ClientSession) {
			defer wg.Done()
			outs[i] = callText(t, s, "knowledge", map[string]any{"op": "apply", "paths": []string{"a.md", "b.md"}})
		}(i, s)
	}
	wg.Wait()

	ids := decisionIDs(t, sess, "serialization")
	if len(ids) != 1 {
		t.Fatalf("two racing applies must leave exactly one decision, got %v\nA:\n%s\nB:\n%s", ids, outs[0], outs[1])
	}
	// and both callers were told the SAME id — the loser reports the winner
	// rather than an id it just withdrew
	for i, o := range outs {
		if strings.Contains(o, "need decision") && !strings.Contains(o, shortOf(ids[0])) {
			t.Fatalf("caller %d was told about a withdrawn decision:\n%s\nsurvivor=%s", i, o, ids[0])
		}
	}
}

// shortOf is the display prefix of a full record ID, as the render shows it.
func shortOf(id string) string {
	if len(id) > 17 {
		return id[:17]
	}
	return id
}

// TestDivergenceNamesTheDifferingField: when the quoted headline value
// agrees, the disagreement is in a field the `x` line does not quote —
// printing it anyway rendered the same string twice for a real, correctly
// detected divergence.
func TestDivergenceNamesTheDifferingField(t *testing.T) {
	root := t.TempDir()
	sess := connectRoot(t, root)
	ask := callText(t, sess, "decide", map[string]any{
		"op": "ask", "question": "which serialization should the rpc layer use?",
		"kind": "text", "context": "measured on our own traffic",
	})
	local := ""
	for _, f := range strings.Fields(ask) {
		if strings.HasPrefix(f, "ADR-") {
			local = f
		}
	}
	callText(t, sess, "decide", map[string]any{
		"op": "answer", "id": local, "choose": "protobuf", "consequences": "schema registry needed",
	})

	// same decision, different context/consequences/status
	incoming := "---\nschema: v1\nkind: knowledge\nsources:\n    - repo-b\n---\n\n" +
		"## adr 2222222222222222\nquestion: which serialization should the rpc layer use?\n" +
		"decision: protobuf\ncontext: inherited from the platform team\n" +
		"consequences: none noted\nstatus: superseded\ncount: 1\nsources:\n    - source: repo-b\n      dir: \"\"\n"
	out := callText(t, sess, "knowledge", map[string]any{"op": "apply", "body": incoming})
	if !strings.Contains(out, "diverged=1") {
		t.Fatalf("a context/status-only disagreement is still a divergence:\n%s", out)
	}
	if strings.Contains(out, `ours="protobuf" theirs="protobuf"`) {
		t.Fatalf("the render must not print the same value twice:\n%s", out)
	}
	for _, want := range []string{"context", "consequences", "status"} {
		if !strings.Contains(out, want) {
			t.Fatalf("the differing field %q must be named:\n%s", want, out)
		}
	}
}

// TestFailedMintIsNotCountedSettled: `settled` used to be derived as
// len(conflicts)-open, which folded every conflict whose mint FAILED into
// the count — reporting a conflict that still needs a decision as one this
// workspace had already answered. It is counted now, never subtracted.
func TestFailedMintIsNotCountedSettled(t *testing.T) {
	_, sess := conflictingArtifacts(t)
	out := callText(t, sess, "knowledge", map[string]any{"op": "apply", "paths": []string{"a.md", "b.md"}})
	// baseline: one real conflict, minted, nothing settled
	if !strings.Contains(out, "conflicts=1") || strings.Contains(out, "settled=") {
		t.Fatalf("first apply mints and settles nothing:\n%s", out)
	}
	// second apply: genuinely settled, and counted as exactly that
	again := callText(t, sess, "knowledge", map[string]any{"op": "apply", "paths": []string{"a.md", "b.md"}})
	if !strings.Contains(again, "settled=1") || strings.Contains(again, "conflicts=") {
		t.Fatalf("second apply settles the one conflict:\n%s", again)
	}
	// the counts must be consistent with the lines actually rendered: a
	// settled conflict emits no `need decision`, an open one emits exactly one
	if n := strings.Count(again, "need decision"); n != 0 {
		t.Fatalf("a settled conflict must not ask again (%d asks):\n%s", n, again)
	}
	if n := strings.Count(out, "need decision"); n != 1 {
		t.Fatalf("conflicts=1 must correspond to exactly one ask (%d):\n%s", n, out)
	}
}

// TestArchivedRecordRendersWhatItRetained: lifecycle.Tombstone was taught to
// restore parent/targets/rules/refs and the ADR fields, but the archived
// branch of get kept rendering only the record line and the retained body —
// so the writer carried the fields and the reader dropped them, one level up
// from the boundary bug that motivated carrying them. A field a caller
// cannot see through get is not recovered, whatever the journal holds.
func TestArchivedRecordRendersWhatItRetained(t *testing.T) {
	root := t.TempDir()
	sess := connectRoot(t, root)

	ask := callText(t, sess, "decide", map[string]any{
		"op": "ask", "question": "which cache eviction policy?", "kind": "text",
		"context": "CTXMARK",
	})
	id := ""
	for _, f := range strings.Fields(ask) {
		if strings.HasPrefix(f, "ADR-") {
			id = f
		}
	}
	if id == "" {
		t.Fatalf("setup: %s", ask)
	}
	callText(t, sess, "decide", map[string]any{
		"op": "answer", "id": id, "choose": "DECMARK", "consequences": "CONSMARK",
	})
	if got := callText(t, sess, "move", map[string]any{"id": id, "to": "archived", "note": "curated"}); strings.Contains(got, "! ") {
		t.Fatalf("setup: archiving: %q", got)
	}

	out := callText(t, sess, "get", map[string]any{"id": id})
	if !strings.Contains(out, "journal tombstone") {
		t.Fatalf("expected a tombstone render: %q", out)
	}
	for _, want := range []string{"context: CTXMARK", "decision: DECMARK", "consequences: CONSMARK", "status: accepted"} {
		if !strings.Contains(out, want) {
			t.Fatalf("archived render dropped %q:\n%s", want, out)
		}
	}

	// a kind that retains NO body must still not have its archive summary
	// printed back as though it were content
	tk := callText(t, sess, "draft", map[string]any{
		"kind": "task", "title": "plain task", "body": "BODYMARK should not survive archiving",
	})
	tid := ""
	for _, f := range strings.Fields(tk) {
		if strings.HasPrefix(f, "T-") {
			tid = f
		}
	}
	if tid == "" {
		t.Fatalf("setup task: %s", tk)
	}
	callText(t, sess, "move", map[string]any{"id": tid, "to": "archived", "note": "done"})
	tout := callText(t, sess, "get", map[string]any{"id": tid})
	if strings.Contains(tout, "BODYMARK") {
		t.Fatalf("a task tombstone must stay a compact summary:\n%s", tout)
	}
}

// TestRoundsExhaustedRefusesInsteadOfReportingSuccess: the rounds-exhausted
// move used to return through text() — exit 0 — leading with an
// `i <id> <kind> blocked ...` record line, byte-identical in shape to the one
// a SUCCESSFUL move renders. Three of four independent judges read that as
// moved-plus-warning; one then issued five further moves against an item that
// had never entered active. The absence of that line is the assertion: a test
// checking only for the new wording would pass with the old line still there.
func TestRoundsExhaustedRefusesInsteadOfReportingSuccess(t *testing.T) {
	root := t.TempDir()
	sess := connectRoot(t, root)
	out := callText(t, sess, "draft", map[string]any{
		"kind": "task", "title": "stubborn rework loop", "body": "a body of ordinary length",
	})
	id := ""
	for _, f := range strings.Fields(out) {
		if strings.HasPrefix(f, "T-") {
			id = f
		}
	}
	if id == "" {
		t.Fatalf("setup: %s", out)
	}
	// drive done/active until the server refuses
	var last string
	for i := 0; i < 8; i++ {
		to := "done"
		if i%2 == 1 {
			to = "active"
		}
		last = callText(t, sess, "move", map[string]any{"id": id, "to": to})
		if strings.Contains(last, "ROUNDS E") {
			break
		}
	}
	if !strings.Contains(last, "ROUNDS E") {
		t.Fatalf("never escalated:\n%s", last)
	}
	// THE defect: no success-shaped record line for a state nobody requested
	for _, line := range strings.Split(last, "\n") {
		if strings.HasPrefix(line, "i ") {
			t.Fatalf("a refused move must not render a record line: %q\n%s", line, last)
		}
	}
	if !strings.Contains(last, "REFUSED") {
		t.Fatalf("the refusal must say the requested move did not happen:\n%s", last)
	}
	// and the resolution must be callable, not a prose list
	if !strings.Contains(last, `decide {"op":"answer"`) || !strings.Contains(last, `"choose"`) {
		t.Fatalf("the refusal must hand back a callable decide object:\n%s", last)
	}
	// the ADR id must still be extractable from it
	adr := ""
	for _, f := range strings.Fields(strings.ReplaceAll(last, `"`, " ")) {
		if strings.HasPrefix(f, "ADR-") {
			adr = f
		}
	}
	if adr == "" {
		t.Fatalf("the refusal must name the decision:\n%s", last)
	}
	if got := callText(t, sess, "decide", map[string]any{"op": "answer", "id": adr, "choose": "rescope"}); strings.Contains(got, "! ") {
		t.Fatalf("the handed-back call must work: %q", got)
	}
}

// TestStatusFromOutsideIsValidated: item.Item.Status was a bare string whose
// enum lived only in a doc comment and a jsonschema DESCRIPTION, neither of
// which validates. The exposure was not a caller typo but an IMPORTED
// artifact: the ADR-apply path assigned d.Status = e.Status straight from
// another repository's entry, so a foreign artifact could inject any string —
// including "superseded", which is a consequence of a replacement and not a
// claim an artifact is in a position to make (B-01KYNA4PJNF5K).
func TestStatusFromOutsideIsValidated(t *testing.T) {
	artifact := func(status string) string {
		return "---\nschema: v1\nkind: knowledge\nsources:\n    - repo-a\n---\n\n" +
			"## adr 2222222222222222\nquestion: which serialization?\ndecision: protobuf\n" +
			"status: " + status + "\ncount: 1\nsources:\n    - source: repo-a\n      dir: \"\"\n"
	}

	t.Run("a bogus status does not poison the workspace", func(t *testing.T) {
		sess := connectRoot(t, t.TempDir())
		out := callText(t, sess, "knowledge", map[string]any{"op": "apply", "body": artifact("totally-made-up")})
		if !strings.Contains(out, "! ARG E") || !strings.Contains(out, "not adoptable") {
			t.Fatalf("an invalid imported status must be refused:\n%s", out)
		}
		// and the refusal names the legal set, so the caller learns it
		for _, want := range item.Statuses() {
			if !strings.Contains(out, want) {
				t.Fatalf("the refusal must name %q:\n%s", want, out)
			}
		}
	})

	t.Run("superseded is refused even though it is a legal value", func(t *testing.T) {
		sess := connectRoot(t, t.TempDir())
		out := callText(t, sess, "knowledge", map[string]any{"op": "apply", "body": artifact("superseded")})
		if !strings.Contains(out, "! ARG E") {
			t.Fatalf("an artifact may not assert superseded:\n%s", out)
		}
		// the refusal must say WHY, so the caller learns the rule rather than
		// just being blocked by it
		if !strings.Contains(out, "replacement this workspace does not have") {
			t.Fatalf("the refusal must say why, not just no:\n%s", out)
		}
	})

	t.Run("the legal values still apply", func(t *testing.T) {
		for _, st := range []string{"proposed", "accepted", "deprecated"} {
			sess := connectRoot(t, t.TempDir())
			out := callText(t, sess, "knowledge", map[string]any{"op": "apply", "body": artifact(st)})
			if strings.Contains(out, "! ARG E") {
				t.Fatalf("status %q must be accepted: %s", st, out)
			}
		}
	})

	t.Run("a refusal leaves NO stray record behind", func(t *testing.T) {
		sess := connectRoot(t, t.TempDir())
		before := callText(t, sess, "find", map[string]any{"q": "serialization", "scope": "adr"})
		out := callText(t, sess, "knowledge", map[string]any{"op": "apply", "body": artifact("superseded")})
		if !strings.Contains(out, "! ARG E") {
			t.Fatalf("setup: expected a refusal:\n%s", out)
		}
		// lifecycle.Draft persists and journals, so a check placed AFTER it
		// left a permanent content-less ADR — which an export then re-emitted
		// and a third workspace promoted to a full accepted ADR. The guard
		// meant to keep bad data out was manufacturing worse data.
		after := callText(t, sess, "find", map[string]any{"q": "serialization", "scope": "adr"})
		if after != before {
			t.Fatalf("a refused apply must mint nothing:\nbefore: %q\nafter:  %q", before, after)
		}
		if st := callText(t, sess, "state", map[string]any{}); strings.Contains(st, "adr ") {
			t.Fatalf("a refused apply left a stray record in state:\n%s", st)
		}
	})

	t.Run("export MAY assert superseded — the asymmetry is deliberate", func(t *testing.T) {
		// Exporting says "this repository's decision is superseded", a true
		// fact about the source recorded against Entry.Sources. Importing the
		// same value would say it about the IMPORTER, which holds no
		// replacement record. Describing your own history and adopting
		// someone else's claim are different acts, so export allows what
		// apply refuses. Pinned so nobody "fixes" the asymmetry away.
		sess := connectRoot(t, t.TempDir())
		out := callText(t, sess, "knowledge", map[string]any{
			"op": "export",
			"entries": []map[string]any{{
				"kind": "adr", "question": "which serialization?", "decision": "protobuf",
				"status": "superseded",
			}},
		})
		if strings.Contains(out, "! ARG E") {
			t.Fatalf("export must be able to state its own superseded history:\n%s", out)
		}
		if !strings.Contains(out, "status: superseded") {
			t.Fatalf("the exported artifact must carry the status verbatim:\n%s", out)
		}
	})

	t.Run("the caller-authored export path is guarded too", func(t *testing.T) {
		sess := connectRoot(t, t.TempDir())
		out := callText(t, sess, "knowledge", map[string]any{
			"op": "export",
			"entries": []map[string]any{{
				"kind": "adr", "question": "q?", "decision": "d", "status": "nonsense",
			}},
		})
		if !strings.Contains(out, "! ARG E") || !strings.Contains(out, "status") {
			t.Fatalf("a caller-authored entry's status must be validated:\n%s", out)
		}
	})
}
