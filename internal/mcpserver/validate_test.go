package mcpserver

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jxsl13/spectackle/internal/item"
	"github.com/jxsl13/spectackle/internal/journal"
	"github.com/jxsl13/spectackle/internal/workspace"
	"github.com/jxsl13/spectackle/internal/wt"
)

// requireValidateRoot arms feedback.validate=require — the hard archive gate
// this task builds (T-01KYD94M3EFXCBVRVWZCS5KBE9).
func requireValidateRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".spectackle"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".spectackle", "config.yaml"),
		[]byte("schema: v1\nfeedback:\n  validate: require\n"), 0o644); err != nil {
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

// RED-RUN (written before the implementation, per the brief): under
// feedback.validate=require, a done task must NOT reach archived without a
// passing validation verdict — done rolling into archived on the
// orchestrator's say-so is the judge-free gap this task closes.
func TestArchiveRequiresPassingValidation(t *testing.T) {
	root := requireValidateRoot(t)
	t.Setenv("SPECTACKLE_AGENT", "implementer-a")
	sess := connectRoot(t, root)
	task := draftID(t, sess, map[string]any{
		"kind": "task", "title": "implemented but never validated"})
	callText(t, sess, "move", map[string]any{"id": task, "to": "done"})
	out := callText(t, sess, "move", map[string]any{"id": task, "to": "archived"})
	if !strings.Contains(out, "! VALIDATE E") {
		t.Fatalf("archive proceeded without a validation verdict: %q", out)
	}
}

// gitCommitAs commits the working tree citing the item's FULL ID — the
// attribution itemDiff's fallback recovers.
func gitCommitAs(t *testing.T, root, fullID, msg string) {
	t.Helper()
	for _, args := range [][]string{{"add", "-A"}, {"commit", "-q", "-m", "spectackle " + fullID + ": " + msg}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
}

func fullIDOf(t *testing.T, root, prefix string) string {
	t.Helper()
	s, _ := connectRootWithServer(t, root)
	it, ok, err := itemGetForTest(s, prefix)
	if err != nil || !ok {
		t.Fatalf("full ID lookup %q: %v %v", prefix, ok, err)
	}
	return it.ID
}

// The full validation lifecycle: implementer refused, independent validator
// passes, archive derives its note from the verdict.
func TestValidateVerdictLifecycle(t *testing.T) {
	root := requireValidateRoot(t)
	t.Setenv("SPECTACKLE_AGENT", "impl-a")
	impl := connectRoot(t, root)
	task := draftID(t, impl, map[string]any{
		"kind": "task", "title": "adds the feature",
		"targets": []string{"feature.go", "feature_test.go", "docs/feature.md"}})
	full := fullIDOf(t, root, task)
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "feature.md"),
		[]byte("Feature returns the answer.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "feature.go"),
		[]byte("package main\n\nfunc Feature() int { return 42 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "feature_test.go"),
		[]byte("package main\n\nimport \"testing\"\n\nfunc TestFeature(t *testing.T) {\n\tif Feature() != 42 {\n\t\tt.Fatal(\"wrong\")\n\t}\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCommitAs(t, root, full, "implement")
	callText(t, impl, "move", map[string]any{"id": task, "to": "done"})

	callText(t, impl, "validate", map[string]any{"id": task})
	out := callText(t, impl, "validate", map[string]any{"op": "verdict", "id": task, "pass": true})
	if !strings.Contains(out, "validator implemented this") {
		t.Fatalf("implementer verdict not refused: %q", out)
	}
	t.Setenv("SPECTACKLE_AGENT", "val-b")
	val := connectRoot(t, root)
	out = callText(t, val, "validate", map[string]any{"op": "verdict", "id": task, "pass": true})
	if !strings.Contains(out, "ok validate") || !strings.Contains(out, "pass by val-b") {
		t.Fatalf("independent verdict refused: %q", out)
	}
	out = callText(t, impl, "move", map[string]any{"id": task, "to": "archived"})
	if !strings.Contains(out, "archived") {
		t.Fatalf("archive refused after passing validation: %q", out)
	}
}

// A failing verdict reopens done→active with the findings as the next
// brief, rendered FIRST by get; a clean re-round archives.
func TestValidateFailReopensWithFindingsAsBrief(t *testing.T) {
	root := requireValidateRoot(t)
	t.Setenv("SPECTACKLE_AGENT", "impl-a")
	impl := connectRoot(t, root)
	task := draftID(t, impl, map[string]any{
		"kind": "task", "title": "incomplete first round",
		"targets": []string{"feature.go", "feature_test.go", "docs/feature.md"}})
	full := fullIDOf(t, root, task)
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "feature.md"),
		[]byte("Feature returns the answer.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "feature.go"),
		[]byte("package main\n\nfunc Feature() int { return 42 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "feature_test.go"),
		[]byte("package main\n\nimport \"testing\"\n\nfunc TestFeature(t *testing.T) {\n\tif Feature() != 42 {\n\t\tt.Fatal(\"wrong\")\n\t}\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCommitAs(t, root, full, "round one")
	callText(t, impl, "move", map[string]any{"id": task, "to": "done"})

	t.Setenv("SPECTACKLE_AGENT", "val-b")
	val := connectRoot(t, root)
	callText(t, val, "validate", map[string]any{"id": task})
	findings := "the negative-input edge case is unhandled and untested; round two must add the guard and a regression test for it"
	out := callText(t, val, "validate", map[string]any{"op": "verdict", "id": task, "pass": false, "findings": findings})
	if !strings.Contains(out, "reopened done→active") {
		t.Fatalf("failing verdict did not reopen: %q", out)
	}
	out = callText(t, impl, "get", map[string]any{"id": task})
	if !strings.Contains(out, " active ") && !strings.Contains(out, "task active") {
		t.Fatalf("item not active after reopen: %q", out)
	}
	if !strings.Contains(out, "validate fail val-b :: "+findings[:40]) {
		t.Fatalf("findings not rendered as the brief: %q", out)
	}
	// round two: touch the code, done, re-validate clean, archive
	if err := os.WriteFile(filepath.Join(root, "feature.go"),
		[]byte("package main\n\nfunc Feature() int { return 42 }\n\nfunc guard() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCommitAs(t, root, full, "round two")
	callText(t, impl, "move", map[string]any{"id": task, "to": "done"})
	callText(t, val, "validate", map[string]any{"id": task})
	out = callText(t, val, "validate", map[string]any{"op": "verdict", "id": task, "pass": true})
	if !strings.Contains(out, "pass by val-b") {
		t.Fatalf("round-two verdict refused: %q", out)
	}
	out = callText(t, impl, "move", map[string]any{"id": task, "to": "archived"})
	if !strings.Contains(out, "archived") {
		t.Fatalf("archive refused after clean round two: %q", out)
	}
}

// Computed findings are not waivable: an untouched declared target blocks a
// bare pass.
func TestValidateWaiverRefusal(t *testing.T) {
	root := requireValidateRoot(t)
	t.Setenv("SPECTACKLE_AGENT", "impl-a")
	impl := connectRoot(t, root)
	task := draftID(t, impl, map[string]any{
		"kind": "task", "title": "declares more than it lands",
		"targets": []string{"feature.go", "ghost.go"}})
	full := fullIDOf(t, root, task)
	if err := os.WriteFile(filepath.Join(root, "feature.go"),
		[]byte("package main\n\nfunc feature() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCommitAs(t, root, full, "only half")
	callText(t, impl, "move", map[string]any{"id": task, "to": "done"})
	t.Setenv("SPECTACKLE_AGENT", "val-b")
	val := connectRoot(t, root)
	out := callText(t, val, "validate", map[string]any{"id": task})
	if !strings.Contains(out, "v untouched ghost.go") {
		t.Fatalf("untouched target not computed: %q", out)
	}
	out = callText(t, val, "validate", map[string]any{"op": "verdict", "id": task, "pass": true})
	if !strings.Contains(out, "computed findings open") {
		t.Fatalf("open findings waived by pass=true: %q", out)
	}
}

// The verdict binds the diff: a new commit citing the item expires it.
func TestValidateDiffBinding(t *testing.T) {
	root := requireValidateRoot(t)
	t.Setenv("SPECTACKLE_AGENT", "impl-a")
	impl := connectRoot(t, root)
	task := draftID(t, impl, map[string]any{
		"kind": "task", "title": "diff binding", "targets": []string{"feature.go"}})
	full := fullIDOf(t, root, task)
	if err := os.WriteFile(filepath.Join(root, "feature.go"),
		[]byte("package main\n\nfunc feature() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCommitAs(t, root, full, "round one")
	callText(t, impl, "move", map[string]any{"id": task, "to": "done"})
	t.Setenv("SPECTACKLE_AGENT", "val-b")
	val := connectRoot(t, root)
	callText(t, val, "validate", map[string]any{"id": task})
	callText(t, val, "validate", map[string]any{"op": "verdict", "id": task, "pass": true})
	if err := os.WriteFile(filepath.Join(root, "feature.go"),
		[]byte("package main\n\nfunc feature() {}\n\nfunc sneaky() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCommitAs(t, root, full, "post-verdict change")
	out := callText(t, impl, "move", map[string]any{"id": task, "to": "archived"})
	if !strings.Contains(out, "stale validation") {
		t.Fatalf("post-verdict commit did not expire the verdict: %q", out)
	}
	callText(t, val, "validate", map[string]any{"id": task})
	callText(t, val, "validate", map[string]any{"op": "verdict", "id": task, "pass": true})
	out = callText(t, impl, "move", map[string]any{"id": task, "to": "archived"})
	if !strings.Contains(out, "archived") {
		t.Fatalf("re-validation did not reopen the gate: %q", out)
	}
}

// Each computed class fires on a fixture built to trip exactly it and stays
// silent on the clean shape — unit level, no server round trips.
func TestValidateClassFixtures(t *testing.T) {
	vac := vacuousTestLines("x_test.go", []byte(`package x

import "testing"

func TestEmptySubtest(t *testing.T) {
	t.Run("does nothing", func(t *testing.T) {})
	if 1 != 1 {
		t.Fatal("never")
	}
}
`))
	if len(vac) != 1 || !strings.Contains(vac[0], "subtest without assertion") {
		t.Fatalf("empty subtest not flagged: %v", vac)
	}
	clean := vacuousTestLines("y_test.go", []byte(`package y

import "testing"

func TestHealthy(t *testing.T) {
	t.Run("asserts", func(t *testing.T) { t.Log("x"); if false { t.Fatal("no") } })
	if false {
		t.Fatal("no")
	}
}
`))
	for _, l := range clean {
		if strings.Contains(l, "subtest without assertion") {
			t.Fatalf("healthy subtest flagged: %v", clean)
		}
	}
	fake := validateBench(itemStub(), "+func BenchmarkFake(b *testing.B) {\n+\tdoWork()\n+}\n")
	if len(fake) != 1 || !strings.Contains(fake[0], "v fakebench BenchmarkFake") {
		t.Fatalf("fake bench not flagged: %v", fake)
	}
	real := validateBench(itemStub(), "+func BenchmarkReal(b *testing.B) {\n+\tfor i := 0; i < b.N; i++ {\n+\t\tdoWork()\n+\t}\n+}\n")
	for _, l := range real {
		if strings.Contains(l, "fakebench") {
			t.Fatalf("honest bench flagged: %v", real)
		}
	}
}

func itemStub() item.Item { return item.Item{Kind: "task"} }

// The documentation dimension: exported symbols added with zero .md changes
// draws the nodocs finding; touching docs silences it (user directive: the
// solution is complete from ALL aspects, documentation included).
func TestValidateNodocsClass(t *testing.T) {
	noDocs := "+func Exported() {}\ndiff --git a/x.go b/x.go\n"
	found := false
	for _, l := range (&Server{}).validateComputedForTest(noDocs) {
		if strings.Contains(l, "v nodocs") {
			found = true
		}
	}
	if !found {
		t.Fatalf("exported-without-docs not flagged")
	}
	withDocs := "diff --git a/x.go b/x.go\n+func Exported() {}\ndiff --git a/docs/x.md b/docs/x.md\n+documented\n"
	for _, l := range (&Server{}).validateComputedForTest(withDocs) {
		if strings.Contains(l, "v nodocs") {
			t.Fatalf("documented change flagged nodocs")
		}
	}
}

// Merge-mode diff binding (cross-verification breaks-gate repro): a
// post-verdict commit CITING the item must expire the verdict even when the
// primary diff source is the branch merge.
func TestValidateMergeModeDiffBinding(t *testing.T) {
	root := requireValidateRoot(t)
	t.Setenv("SPECTACKLE_AGENT", "impl-a")
	impl := connectRoot(t, root)
	task := draftID(t, impl, map[string]any{
		"kind": "task", "title": "merge mode binding", "targets": []string{"feature.go"}})
	full := fullIDOf(t, root, task)
	branch := "spectackle/" + full
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	run("checkout", "-q", "-b", branch)
	if err := os.WriteFile(filepath.Join(root, "feature.go"),
		[]byte("package main\n\nfunc feature() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCommitAs(t, root, full, "implement on branch")
	run("checkout", "-q", "main")
	run("merge", "--no-ff", "-q", "-m", "Merge "+branch, branch)
	callText(t, impl, "move", map[string]any{"id": task, "to": "done"})

	t.Setenv("SPECTACKLE_AGENT", "val-b")
	val := connectRoot(t, root)
	out := callText(t, val, "validate", map[string]any{"id": task})
	if !strings.Contains(out, "d source merge of "+branch) {
		t.Fatalf("merge source not recovered: %q", out)
	}
	callText(t, val, "validate", map[string]any{"op": "verdict", "id": task, "pass": true})
	// post-verdict citing commit on main
	if err := os.WriteFile(filepath.Join(root, "feature.go"),
		[]byte("package main\n\nfunc feature() {}\n\nfunc sneak() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCommitAs(t, root, full, "post-verdict fix")
	out = callText(t, impl, "move", map[string]any{"id": task, "to": "archived"})
	if !strings.Contains(out, "stale validation") {
		t.Fatalf("merge-mode post-verdict citing commit did not expire the verdict: %q", out)
	}
}

// The reopen counts a round (journaled rnd) and the archive note derives
// from the verdict — the two properties the first battery exercised without
// asserting (cross-verification).
func TestValidateRoundsAndDerivedNote(t *testing.T) {
	root := requireValidateRoot(t)
	t.Setenv("SPECTACKLE_AGENT", "impl-a")
	impl := connectRoot(t, root)
	task := draftID(t, impl, map[string]any{
		"kind": "task", "title": "rounds and notes", "targets": []string{"feature.go", "docs/f.md"}})
	full := fullIDOf(t, root, task)
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "f.md"), []byte("doc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "feature.go"),
		[]byte("package main\n\nfunc feature() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCommitAs(t, root, full, "round one")
	callText(t, impl, "move", map[string]any{"id": task, "to": "done"})
	t.Setenv("SPECTACKLE_AGENT", "val-b")
	val := connectRoot(t, root)
	callText(t, val, "validate", map[string]any{"id": task})
	callText(t, val, "validate", map[string]any{"op": "verdict", "id": task, "pass": false,
		"findings": "round one misses the guard entirely; add it plus a regression test before the next done"})
	events, err := journal.Read(mustWS(t, root), "")
	if err != nil {
		t.Fatal(err)
	}
	gotRound := false
	for _, e := range events {
		if e.Ev == journal.EvMove && e.ID == full && e.To == item.StateActive && e.Rnd == 1 {
			gotRound = true
		}
	}
	if !gotRound {
		t.Fatalf("reopen did not journal rnd=1")
	}
	// clean round two, archive, note derives from the verdict
	if err := os.WriteFile(filepath.Join(root, "feature.go"),
		[]byte("package main\n\nfunc feature() {}\n\nfunc guard() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCommitAs(t, root, full, "round two")
	callText(t, impl, "move", map[string]any{"id": task, "to": "done"})
	callText(t, val, "validate", map[string]any{"id": task})
	callText(t, val, "validate", map[string]any{"op": "verdict", "id": task, "pass": true})
	callText(t, impl, "move", map[string]any{"id": task, "to": "archived", "note": "shipped"})
	events, err = journal.Read(mustWS(t, root), "")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range events {
		if e.ID == full && e.Ev == journal.EvMove && e.To == item.StateArchived {
			if strings.Contains(e.Note, "validated pass by val-b") && strings.Contains(e.Note, "shipped") {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("archive note not derived from the verdict")
	}
}

// Exhausting max_rounds through repeated failing verdicts escalates to
// blocked with the ADR-item — SPX-SWM-007 unchanged through the new path.
func TestValidateEscalationUnchanged(t *testing.T) {
	root := requireValidateRoot(t)
	t.Setenv("SPECTACKLE_AGENT", "impl-a")
	impl := connectRoot(t, root)
	task := draftID(t, impl, map[string]any{
		"kind": "task", "title": "keeps failing validation", "targets": []string{"feature.go"}})
	full := fullIDOf(t, root, task)
	if err := os.WriteFile(filepath.Join(root, "feature.go"),
		[]byte("package main\n\nfunc feature() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCommitAs(t, root, full, "attempt")
	t.Setenv("SPECTACKLE_AGENT", "val-b")
	val := connectRoot(t, root)
	blocked := false
	for i := 0; i < 4; i++ {
		callText(t, impl, "move", map[string]any{"id": task, "to": "done"})
		callText(t, val, "validate", map[string]any{"id": task})
		out := callText(t, val, "validate", map[string]any{"op": "verdict", "id": task, "pass": false,
			"findings": "still incomplete: the guard remains missing and untested after this round of implementation"})
		if strings.Contains(out, "blocked") && strings.Contains(out, "decide") {
			blocked = true
			break
		}
	}
	if !blocked {
		t.Fatalf("repeated failing verdicts never escalated to blocked")
	}
	_ = full
}

func mustWS(t *testing.T, root string) workspace.Root {
	t.Helper()
	ws, err := workspace.Detect(root, root)
	if err != nil {
		t.Fatal(err)
	}
	return ws
}
