package mcpserver

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jxsl13/spectackle/internal/item"
	"github.com/jxsl13/spectackle/internal/wt"
)

// requireGrillRoot is gitRoot with feedback.grill=require: the hard gate the
// verdict machinery arms (T-01KYD94KP4FBHR0RGR2P8CZNBZ).
func requireGrillRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".spectackle"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".spectackle", "config.yaml"),
		[]byte("schema: v1\nfeedback:\n  grill: require\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := wt.InitTestRepo(root); err != nil {
		t.Skipf("git unavailable: %v", err)
	}
	return root
}

// RED-RUN 1 (written before the implementation, per the brief): under
// feedback.grill=require, a grilled-but-unreviewed proposal must NOT reach
// approved — the stamp records that a pack rendered, not that anyone judged
// it. The gate must demand a passing EvReview bound to the current body from
// a non-author identity.
func TestMoveToApprovedRequiresPassingReviewVerdict(t *testing.T) {
	root := requireGrillRoot(t)
	t.Setenv("SPECTACKLE_AGENT", "author-a")
	sess := connectRoot(t, root)
	prop := draftID(t, sess, map[string]any{
		"kind": "proposal", "title": "needs an independent review before approval"})
	callText(t, sess, "grill", map[string]any{"id": prop})
	out := callText(t, sess, "move", map[string]any{"id": prop, "to": "approved"})
	if !strings.Contains(out, "! GRILL E") || !strings.Contains(out, "review") {
		t.Fatalf("move approved a grilled-but-unreviewed proposal: %q", out)
	}
}

// RED-RUN 2: the reviewer must not be the author — a verdict from the
// identity that created the item is refused with the exact record.
func TestVerdictByAuthorRefused(t *testing.T) {
	root := requireGrillRoot(t)
	t.Setenv("SPECTACKLE_AGENT", "author-a")
	sess := connectRoot(t, root)
	prop := draftID(t, sess, map[string]any{
		"kind": "proposal", "title": "author may not judge own work"})
	callText(t, sess, "grill", map[string]any{"id": prop})
	out := callText(t, sess, "grill", map[string]any{"op": "verdict", "id": prop, "pass": true})
	if !strings.Contains(out, "reviewer is the author") {
		t.Fatalf("author verdict not refused: %q", out)
	}
}

// The full verdict lifecycle: a second, deliberately named identity passes;
// a body edit expires the verdict (move refuses naming the stale review);
// re-grill plus re-verdict re-opens the gate.
func TestVerdictLifecycleHashBinding(t *testing.T) {
	root := requireGrillRoot(t)
	t.Setenv("SPECTACKLE_AGENT", "author-a")
	author := connectRoot(t, root)
	prop := draftID(t, author, map[string]any{
		"kind": "proposal", "title": "hash binding end to end"})
	callText(t, author, "grill", map[string]any{"id": prop})

	t.Setenv("SPECTACKLE_AGENT", "reviewer-b")
	reviewer := connectRoot(t, root)
	out := callText(t, reviewer, "grill", map[string]any{"op": "verdict", "id": prop, "pass": true})
	if !strings.Contains(out, "ok review") || !strings.Contains(out, "pass by reviewer-b") {
		t.Fatalf("second-identity verdict refused: %q", out)
	}
	out = callText(t, author, "move", map[string]any{"id": prop, "to": "approved"})
	if !strings.Contains(out, "approved") || strings.Contains(out, "! GRILL E") {
		t.Fatalf("passing verdict did not open the gate: %q", out)
	}
	// A body edit after review must expire the verdict. There is no
	// body-edit tool (B-01KYER), so the test writes through item.Upsert
	// the way lifecycle-internal code does.
	prop2 := draftID(t, author, map[string]any{
		"kind": "proposal", "title": "second proposal for stale check"})
	callText(t, author, "grill", map[string]any{"id": prop2})
	callText(t, reviewer, "grill", map[string]any{"op": "verdict", "id": prop2, "pass": true})
	s2, _ := connectRootWithServer(t, root)
	it2, ok, err := itemGetForTest(s2, prop2)
	if err != nil || !ok {
		t.Fatalf("test lookup 2: %v %v", ok, err)
	}
	it2.Body = it2.Body + "\nedited after review"
	if err := itemUpsertForTest(s2, it2); err != nil {
		t.Fatal(err)
	}
	out = callText(t, author, "move", map[string]any{"id": prop2, "to": "approved"})
	if !strings.Contains(out, "stale review") {
		t.Fatalf("edited body did not expire the verdict: %q", out)
	}
	callText(t, author, "grill", map[string]any{"id": prop2})
	callText(t, reviewer, "grill", map[string]any{"op": "verdict", "id": prop2, "pass": true})
	out = callText(t, author, "move", map[string]any{"id": prop2, "to": "approved"})
	if !strings.Contains(out, "approved") {
		t.Fatalf("re-review did not reopen the gate: %q", out)
	}
}

// Computed findings are not the reviewer's to waive: a planted nopath
// finding blocks pass=true with the exact refusal.
func TestVerdictCannotWaiveComputedFindings(t *testing.T) {
	root := requireGrillRoot(t)
	t.Setenv("SPECTACKLE_AGENT", "author-a")
	author := connectRoot(t, root)
	prop := draftID(t, author, map[string]any{
		"kind": "proposal", "title": "plants a missing path",
		"body": "touches internal/definitely/missing.go on purpose"})
	callText(t, author, "grill", map[string]any{"id": prop})
	t.Setenv("SPECTACKLE_AGENT", "reviewer-b")
	reviewer := connectRoot(t, root)
	out := callText(t, reviewer, "grill", map[string]any{"op": "verdict", "id": prop, "pass": true})
	if !strings.Contains(out, "computed findings open") {
		t.Fatalf("open findings waived by pass=true: %q", out)
	}
	out = callText(t, reviewer, "grill", map[string]any{"op": "verdict", "id": prop, "pass": false, "findings": "the nopath finding is real: the named file does not exist and the brief must name real files before approval"})
	if !strings.Contains(out, "ok review") {
		t.Fatalf("failing verdict with findings refused: %q", out)
	}
}

// An ephemeral (generated) identity may not judge: the author-check would
// pass by pure chance for per-call clients with SPECTACKLE_AGENT unset.
func TestVerdictEphemeralIdentityRefused(t *testing.T) {
	root := requireGrillRoot(t)
	t.Setenv("SPECTACKLE_AGENT", "author-a")
	author := connectRoot(t, root)
	prop := draftID(t, author, map[string]any{
		"kind": "proposal", "title": "anonymous reviewers are refused"})
	callText(t, author, "grill", map[string]any{"id": prop})
	t.Setenv("SPECTACKLE_AGENT", "ag-beef")
	anon := connectRoot(t, root)
	out := callText(t, anon, "grill", map[string]any{"op": "verdict", "id": prop, "pass": true})
	if !strings.Contains(out, "anonymous reviewer") {
		t.Fatalf("generated-shape identity not refused: %q", out)
	}
}

// A failing verdict without findings is refused; word padding in the BODY
// changes no computed count (the deleted substring checks must stay dead).
func TestVerdictFindingsFloorAndPaddingImmunity(t *testing.T) {
	root := requireGrillRoot(t)
	t.Setenv("SPECTACKLE_AGENT", "author-a")
	author := connectRoot(t, root)
	padded := "scope disjointness rollback exit criterion rejected alternative " +
		"touches internal/definitely/missing.go despite the ritual words"
	prop := draftID(t, author, map[string]any{
		"kind": "proposal", "title": "padding buys nothing", "body": padded})
	out := callText(t, author, "grill", map[string]any{"id": prop})
	if !strings.Contains(out, "g nopath internal/definitely/missing.go") {
		t.Fatalf("padded body suppressed a computed finding: %q", out)
	}
	if !strings.Contains(out, "open=1") {
		t.Fatalf("open count wrong for padded body: %q", out)
	}
	t.Setenv("SPECTACKLE_AGENT", "reviewer-b")
	reviewer := connectRoot(t, root)
	out = callText(t, reviewer, "grill", map[string]any{"op": "verdict", "id": prop, "pass": false})
	if !strings.Contains(out, "must say why") {
		t.Fatalf("empty failing findings not refused: %q", out)
	}
}

// research-demand: an uncovered target path with zero rejection hits trips
// the class; citing an R-item ref closes it.
func TestResearchDemandFiresAndCloses(t *testing.T) {
	root := requireGrillRoot(t)
	t.Setenv("SPECTACKLE_AGENT", "author-a")
	author := connectRoot(t, root)
	if err := os.MkdirAll(filepath.Join(root, "uncharted"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "uncharted", "novel.go"), []byte("package uncharted\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	prop := draftID(t, author, map[string]any{
		"kind": "proposal", "title": "wanders into terra incognita",
		"targets": []string{"uncharted/novel.go"}})
	out := callText(t, author, "grill", map[string]any{"id": prop})
	if !strings.Contains(out, "g research-needed uncharted/novel.go") {
		t.Fatalf("research-demand did not fire: %q", out)
	}
	rItem := draftID(t, author, map[string]any{
		"kind": "research", "title": "a study of the uncharted package"})
	prop2 := draftID(t, author, map[string]any{
		"kind": "proposal", "title": "returns with a study in hand",
		"targets": []string{"uncharted/novel.go"}, "refs": []string{rItem}})
	out = callText(t, author, "grill", map[string]any{"id": prop2})
	if strings.Contains(out, "research-needed") {
		t.Fatalf("cited R-item did not close research-demand: %q", out)
	}
}

// The pack's byte cost is COMPUTED, not self-reported: a fixed synthetic
// item renders under a hardcoded ceiling asserted by this test on every
// run — the regression bound the brief demands instead of prose a verifier
// would rubber-stamp. The ceiling was set from this fixture's measured
// render at introduction time plus headroom; move it only with a new
// measured justification in this comment.
func TestGrillPackByteBound(t *testing.T) {
	root := requireGrillRoot(t)
	t.Setenv("SPECTACKLE_AGENT", "author-a")
	sess := connectRoot(t, root)
	prop := draftID(t, sess, map[string]any{
		"kind": "proposal", "title": "byte bound fixture",
		"body":    "touches internal/definitely/missing.go and docs/nowhere.md",
		"targets": []string{"main.go"}})
	out := callText(t, sess, "grill", map[string]any{"id": prop})
	const ceiling = 1200
	if len(out) > ceiling {
		t.Fatalf("pack render %dB exceeds the %dB ceiling", len(out), ceiling)
	}
}

// itemGetForTest/itemUpsertForTest reach the item layer the way
// lifecycle-internal code does — the only body-edit path while B-01KYER
// (no body-edit tool) stands.
func itemGetForTest(s *Server, id string) (item.Item, bool, error) {
	sc, err := s.idScope()
	if err != nil {
		return item.Item{}, false, err
	}
	full, bad := sc.expand(id)
	if bad != nil {
		return item.Item{}, false, errors.New("prefix did not resolve")
	}
	return item.Get(s.ws, full)
}

func itemUpsertForTest(s *Server, it item.Item) error { return item.Upsert(s.ws, it) }

// The rejected-revival lane must cross the gate like any other approval:
// draft -> rejected -> approved reached approved with no grill and no
// verdict in two calls (cross-verification; the pre-change gate refused it).
func TestRejectedRevivalCrossesReviewGate(t *testing.T) {
	root := requireGrillRoot(t)
	t.Setenv("SPECTACKLE_AGENT", "author-a")
	sess := connectRoot(t, root)
	prop := draftID(t, sess, map[string]any{
		"kind": "proposal", "title": "gate bypass via rejected revival"})
	callText(t, sess, "move", map[string]any{"id": prop, "to": "rejected", "note": "parking it"})
	out := callText(t, sess, "move", map[string]any{"id": prop, "to": "approved"})
	if !strings.Contains(out, "! GRILL E") {
		t.Fatalf("rejected revival bypassed the review gate: %q", out)
	}
}

// The verdict binds the judged SUBSTANCE — body AND targets: four of five
// computed classes read the target set, so a targets-only edit after review
// must expire the verdict exactly like a body edit (cross-verification).
func TestTargetsEditExpiresVerdict(t *testing.T) {
	root := requireGrillRoot(t)
	t.Setenv("SPECTACKLE_AGENT", "author-a")
	author := connectRoot(t, root)
	// The R-ref closes the research-demand class the bare fixture would
	// otherwise trip (uncontracted target, zero history) — open must be 0
	// for the pass verdict below.
	study := draftID(t, author, map[string]any{
		"kind": "research", "title": "study of the main package"})
	prop := draftID(t, author, map[string]any{
		"kind": "proposal", "title": "targets are judged substance",
		"targets": []string{"main.go"}, "refs": []string{study}})
	callText(t, author, "grill", map[string]any{"id": prop})
	t.Setenv("SPECTACKLE_AGENT", "reviewer-b")
	reviewer := connectRoot(t, root)
	out := callText(t, reviewer, "grill", map[string]any{"op": "verdict", "id": prop, "pass": true})
	if !strings.Contains(out, "ok review") {
		t.Fatalf("verdict setup failed: %q", out)
	}
	s, _ := connectRootWithServer(t, root)
	it, ok, err := itemGetForTest(s, prop)
	if err != nil || !ok {
		t.Fatalf("lookup: %v %v", ok, err)
	}
	it.Targets = append(it.Targets, ".spectackle/journal.ndjson")
	if err := itemUpsertForTest(s, it); err != nil {
		t.Fatal(err)
	}
	out = callText(t, author, "move", map[string]any{"id": prop, "to": "approved"})
	if !strings.Contains(out, "stale review") {
		t.Fatalf("targets-only edit did not expire the verdict: %q", out)
	}
}
