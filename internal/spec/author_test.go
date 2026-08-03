package spec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jxsl13/spectackle/internal/ears"
	"github.com/jxsl13/spectackle/internal/workspace"
)

// TestAddRuleCreatesBundleAndMintsID: empty root, AddRule Dir="pkg" Stem="PKG-API"
// valid sentence -> res.Written true, res.ID=="PKG-API-001", res.Path=="pkg/.spectackle/spec.md";
// re-Load finds the rule; file has front matter with prefix PKG.
func TestAddRuleCreatesBundleAndMintsID(t *testing.T) {
	root := t.TempDir()
	ws := workspace.Root{Dir: root}
	c, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}

	req := AuthorReq{
		Dir:      "pkg",
		Stem:     "PKG-API",
		Sentence: "The system SHALL log to `stderr` only.",
	}
	res, err := AddRule(ws, c, req)
	if err != nil {
		t.Fatal(err)
	}

	if !res.Written {
		t.Errorf("res.Written = false, want true")
	}
	if res.ID != "PKG-API-001" {
		t.Errorf("res.ID = %q, want %q", res.ID, "PKG-API-001")
	}
	if res.Path != "pkg/.spectackle/spec.md" {
		t.Errorf("res.Path = %q, want %q", res.Path, "pkg/.spectackle/spec.md")
	}

	// re-Load and verify rule persisted
	c2, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	rule, ok := c2.Rule("PKG-API-001")
	if !ok {
		t.Fatal("rule PKG-API-001 not found after re-load")
	}
	if rule.File != "pkg/.spectackle/spec.md" {
		t.Errorf("rule.File = %q, want %q", rule.File, "pkg/.spectackle/spec.md")
	}

	// Check front matter has prefix
	abs := filepath.Join(root, "pkg", ".spectackle", "spec.md")
	content, _ := os.ReadFile(abs)
	if !strings.Contains(string(content), "prefix: PKG") {
		t.Errorf("file front matter missing prefix: PKG")
	}
}

// TestAddRuleLintErrorBlocksWrite: invalid sentence -> res.Written false,
// res.Findings has a Severity==ears.Error, no file written (Load has no such rule).
func TestAddRuleLintErrorBlocksWrite(t *testing.T) {
	root := t.TempDir()
	ws := workspace.Root{Dir: root}
	c, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}

	req := AuthorReq{
		Dir:      "pkg",
		Stem:     "PKG-API",
		Sentence: "do stuff", // invalid EARS
	}
	res, err := AddRule(ws, c, req)
	if err != nil {
		t.Fatal(err)
	}

	if res.Written {
		t.Errorf("res.Written = true, want false (due to lint error)")
	}

	// Check that at least one finding has Error severity
	hasError := false
	for _, f := range res.Findings {
		if f.Severity == ears.Error {
			hasError = true
			break
		}
	}
	if !hasError {
		t.Errorf("no Error-severity finding in res.Findings: %v", res.Findings)
	}

	// re-Load and verify no rule written
	c2, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	_, found := c2.Rule("PKG-API-001")
	if found {
		t.Fatal("rule should not have been written due to lint error")
	}
}

// TestAddRuleAppliesAndRationale: Applies []{"go:pkg.A"} + Rationale set ->
// reloaded rule heading has {applies: go:pkg.A}, Rationale line present.
func TestAddRuleAppliesAndRationale(t *testing.T) {
	root := t.TempDir()
	ws := workspace.Root{Dir: root}
	c, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}

	req := AuthorReq{
		Dir:       "pkg",
		Stem:      "PKG-API",
		Sentence:  "The system SHALL log to `stderr` only.",
		Rationale: "Logging to stderr ensures output reaches the terminal.",
		Applies:   []string{"go:pkg.A"},
	}
	_, err = AddRule(ws, c, req)
	if err != nil {
		t.Fatal(err)
	}

	// re-Load and verify applies and rationale
	c2, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	rule, ok := c2.Rule("PKG-API-001")
	if !ok {
		t.Fatal("rule PKG-API-001 not found after re-load")
	}

	if len(rule.Applies) == 0 || rule.Applies[0] != "go:pkg.A" {
		t.Errorf("rule.Applies = %v, want [go:pkg.A]", rule.Applies)
	}

	if rule.Rationale != "Logging to stderr ensures output reaches the terminal." {
		t.Errorf("rule.Rationale = %q, want %q", rule.Rationale, "Logging to stderr ensures output reaches the terminal.")
	}

	// verify in file
	abs := filepath.Join(root, "pkg", ".spectackle", "spec.md")
	content, _ := os.ReadFile(abs)
	contentStr := string(content)
	if !strings.Contains(contentStr, "{applies: go:pkg.A}") {
		t.Errorf("file missing {applies: go:pkg.A} in heading")
	}
	if !strings.Contains(contentStr, "Rationale:") {
		t.Errorf("file missing Rationale line")
	}
}

// TestAddRuleForceIDReplay: ForceID "PKG-API-042" -> res.ID that exact, no minting.
func TestAddRuleForceIDReplay(t *testing.T) {
	root := t.TempDir()
	ws := workspace.Root{Dir: root}
	c, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}

	req := AuthorReq{
		Dir:      "pkg",
		Stem:     "PKG-API",
		ForceID:  "PKG-API-042",
		Sentence: "The system SHALL log to `stderr` only.",
	}
	res, err := AddRule(ws, c, req)
	if err != nil {
		t.Fatal(err)
	}

	if res.ID != "PKG-API-042" {
		t.Errorf("res.ID = %q, want %q", res.ID, "PKG-API-042")
	}
}

// TestAddRuleStemInference: add PKG-API-001 first, then AddRule same Dir with
// empty Stem -> infers PKG-API and mints -002.
func TestAddRuleStemInference(t *testing.T) {
	root := t.TempDir()
	ws := workspace.Root{Dir: root}
	c, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}

	// First rule
	req1 := AuthorReq{
		Dir:      "pkg",
		Stem:     "PKG-API",
		Sentence: "The system SHALL log to `stderr` only.",
	}
	_, err = AddRule(ws, c, req1)
	if err != nil {
		t.Fatal(err)
	}

	// re-Load to reflect first rule
	c, err = Load(root)
	if err != nil {
		t.Fatal(err)
	}

	// Second rule with empty Stem, same Dir
	req2 := AuthorReq{
		Dir:      "pkg",
		Stem:     "", // empty, should infer PKG-API
		Sentence: "The system SHALL validate input carefully.",
	}
	res2, err := AddRule(ws, c, req2)
	if err != nil {
		t.Fatal(err)
	}

	if res2.ID != "PKG-API-002" {
		t.Errorf("res2.ID = %q, want %q", res2.ID, "PKG-API-002")
	}
}

// TestAddRuleMissingStemErrors: empty Dir with no existing file and empty Stem
// -> non-nil error.
func TestAddRuleMissingStemErrors(t *testing.T) {
	root := t.TempDir()
	ws := workspace.Root{Dir: root}
	c, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}

	req := AuthorReq{
		Dir:      "new",
		Stem:     "", // no stem, no existing file
		Sentence: "The system SHALL log to `stderr` only.",
	}
	_, err = AddRule(ws, c, req)
	if err == nil {
		t.Fatal("expected error for missing stem, got nil")
	}
}

// TestMaxNumAndNextID: after adding PKG-API-001/002, c.MaxNum("PKG-API")==2
// and c.NextID("PKG-API")=="PKG-API-003" (reload c first).
func TestMaxNumAndNextID(t *testing.T) {
	root := t.TempDir()
	ws := workspace.Root{Dir: root}
	c, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}

	// Add two rules
	req1 := AuthorReq{
		Dir:      "pkg",
		Stem:     "PKG-API",
		Sentence: "The system SHALL log to `stderr` only.",
	}
	_, err = AddRule(ws, c, req1)
	if err != nil {
		t.Fatal(err)
	}

	c, err = Load(root)
	if err != nil {
		t.Fatal(err)
	}

	req2 := AuthorReq{
		Dir:      "pkg",
		Stem:     "PKG-API",
		Sentence: "The system SHALL validate input carefully.",
	}
	_, err = AddRule(ws, c, req2)
	if err != nil {
		t.Fatal(err)
	}

	// reload
	c, err = Load(root)
	if err != nil {
		t.Fatal(err)
	}

	max := c.MaxNum("PKG-API")
	if max != 2 {
		t.Errorf("c.MaxNum(PKG-API) = %d, want 2", max)
	}

	nextID := c.NextID("PKG-API")
	if nextID != "PKG-API-003" {
		t.Errorf("c.NextID(PKG-API) = %q, want %q", nextID, "PKG-API-003")
	}
}

// TestEditRuleReplacesSentence: add a rule, EditRule new valid sentence ->
// reloaded rule.Text changed, Written true.
func TestEditRuleReplacesSentence(t *testing.T) {
	root := t.TempDir()
	ws := workspace.Root{Dir: root}
	c, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}

	// Add rule
	req := AuthorReq{
		Dir:      "pkg",
		Stem:     "PKG-API",
		Sentence: "The system SHALL log to `stderr` only.",
	}
	res, err := AddRule(ws, c, req)
	if err != nil {
		t.Fatal(err)
	}

	id := res.ID

	// re-Load to reflect added rule
	c, err = Load(root)
	if err != nil {
		t.Fatal(err)
	}

	// Edit rule with new sentence
	editRes, err := EditRule(ws, c, id, "The system SHALL validate input strictly.", "", nil)
	if err != nil {
		t.Fatal(err)
	}

	if !editRes.Written {
		t.Errorf("editRes.Written = false, want true")
	}

	// re-Load and verify sentence changed
	c, err = Load(root)
	if err != nil {
		t.Fatal(err)
	}
	rule, ok := c.Rule(id)
	if !ok {
		t.Fatal("rule not found after re-load")
	}
	if rule.Text != "The system SHALL validate input strictly." {
		t.Errorf("rule.Text = %q, want %q", rule.Text, "The system SHALL validate input strictly.")
	}
}

// TestEditRuleEmptyKeepsOld: EditRule with sentence="" rationale="" applies=nil
// -> old text/rationale/applies preserved.
func TestEditRuleEmptyKeepsOld(t *testing.T) {
	root := t.TempDir()
	ws := workspace.Root{Dir: root}
	c, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}

	// Add rule with rationale and applies
	req := AuthorReq{
		Dir:       "pkg",
		Stem:      "PKG-API",
		Sentence:  "The system SHALL log to `stderr` only.",
		Rationale: "For auditing purposes.",
		Applies:   []string{"go:pkg.A"},
	}
	res, err := AddRule(ws, c, req)
	if err != nil {
		t.Fatal(err)
	}

	id := res.ID

	// re-Load to reflect added rule
	c, err = Load(root)
	if err != nil {
		t.Fatal(err)
	}

	// Edit with empty fields
	_, err = EditRule(ws, c, id, "", "", nil)
	if err != nil {
		t.Fatal(err)
	}

	// re-Load and verify old values preserved
	c, err = Load(root)
	if err != nil {
		t.Fatal(err)
	}
	rule, ok := c.Rule(id)
	if !ok {
		t.Fatal("rule not found after re-load")
	}

	if rule.Text != "The system SHALL log to `stderr` only." {
		t.Errorf("rule.Text changed when editing with empty sentence")
	}
	if rule.Rationale != "For auditing purposes." {
		t.Errorf("rule.Rationale changed when editing with empty rationale")
	}
	if len(rule.Applies) == 0 || rule.Applies[0] != "go:pkg.A" {
		t.Errorf("rule.Applies changed when editing with nil applies")
	}
}

// TestEditRuleLintErrorKeepsFile: EditRule invalid sentence -> Written false,
// Findings error, reloaded rule still old text.
func TestEditRuleLintErrorKeepsFile(t *testing.T) {
	root := t.TempDir()
	ws := workspace.Root{Dir: root}
	c, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}

	// Add rule
	req := AuthorReq{
		Dir:      "pkg",
		Stem:     "PKG-API",
		Sentence: "The system SHALL log to `stderr` only.",
	}
	res, err := AddRule(ws, c, req)
	if err != nil {
		t.Fatal(err)
	}

	id := res.ID
	oldText := "The system SHALL log to `stderr` only."

	// re-Load to reflect added rule
	c, err = Load(root)
	if err != nil {
		t.Fatal(err)
	}

	// Edit with invalid sentence
	editRes, err := EditRule(ws, c, id, "do stuff", "", nil)
	if err != nil {
		t.Fatal(err)
	}

	if editRes.Written {
		t.Errorf("editRes.Written = true, want false (due to lint error)")
	}

	// Check for error finding
	hasError := false
	for _, f := range editRes.Findings {
		if f.Severity == ears.Error {
			hasError = true
			break
		}
	}
	if !hasError {
		t.Errorf("no Error-severity finding in editRes.Findings: %v", editRes.Findings)
	}

	// re-Load and verify text unchanged
	c, err = Load(root)
	if err != nil {
		t.Fatal(err)
	}
	rule, ok := c.Rule(id)
	if !ok {
		t.Fatal("rule not found after re-load")
	}
	if rule.Text != oldText {
		t.Errorf("rule.Text = %q, want %q (should be unchanged)", rule.Text, oldText)
	}
}

// TestEditRuleUnknownID: non-nil error.
func TestEditRuleUnknownID(t *testing.T) {
	root := t.TempDir()
	ws := workspace.Root{Dir: root}
	c, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}

	_, err = EditRule(ws, c, "UNKNOWN-999", "The system SHALL log to `stderr` only.", "", nil)
	if err == nil {
		t.Fatal("expected error for unknown rule ID, got nil")
	}
}

// TestRetireRuleDeletesBlock: add two rules, RetireRule the first ->
// reload: first gone, second present, returned path correct.
func TestRetireRuleDeletesBlock(t *testing.T) {
	root := t.TempDir()
	ws := workspace.Root{Dir: root}
	c, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}

	// Add two rules
	req1 := AuthorReq{
		Dir:      "pkg",
		Stem:     "PKG-API",
		Sentence: "The system SHALL log to `stderr` only.",
	}
	res1, err := AddRule(ws, c, req1)
	if err != nil {
		t.Fatal(err)
	}

	c, err = Load(root)
	if err != nil {
		t.Fatal(err)
	}

	req2 := AuthorReq{
		Dir:      "pkg",
		Stem:     "PKG-API",
		Sentence: "The system SHALL validate input carefully.",
	}
	res2, err := AddRule(ws, c, req2)
	if err != nil {
		t.Fatal(err)
	}

	// Retire first rule
	retirePath, err := RetireRule(ws, c, res1.ID)
	if err != nil {
		t.Fatal(err)
	}

	if retirePath != "pkg/.spectackle/spec.md" {
		t.Errorf("retirePath = %q, want %q", retirePath, "pkg/.spectackle/spec.md")
	}

	// re-Load and verify first gone, second present
	c, err = Load(root)
	if err != nil {
		t.Fatal(err)
	}

	_, found1 := c.Rule(res1.ID)
	if found1 {
		t.Fatal("first rule should be retired")
	}

	_, found2 := c.Rule(res2.ID)
	if !found2 {
		t.Fatal("second rule should still exist")
	}
}

// TestRetireRuleUnknownID: non-nil error.
func TestRetireRuleUnknownID(t *testing.T) {
	root := t.TempDir()
	ws := workspace.Root{Dir: root}
	c, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}

	_, err = RetireRule(ws, c, "UNKNOWN-999")
	if err == nil {
		t.Fatal("expected error for unknown rule ID, got nil")
	}
}

// TestAppendIntentCreatesAndInserts: AppendIntent ctx="" 'first line' then 'second line' ->
// reloaded spec.md '## intent' section contains both lines in order
// (read the file bytes to assert; intent is a prose section).
func TestAppendIntentCreatesAndInserts(t *testing.T) {
	root := t.TempDir()
	ws := workspace.Root{Dir: root}

	// First intent
	err := AppendIntent(ws, "", "first line")
	if err != nil {
		t.Fatal(err)
	}

	// Second intent
	err = AppendIntent(ws, "", "second line")
	if err != nil {
		t.Fatal(err)
	}

	// re-Load and verify intent section
	c, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}

	sf, ok := c.File("")
	if !ok {
		t.Fatal("root spec file not loaded")
	}

	// Find intent section
	var intentText string
	for _, sec := range sf.Sections {
		if sec.Name == "intent" {
			intentText = sec.Text
			break
		}
	}

	if intentText == "" {
		t.Fatal("intent section not found in spec file")
	}

	// Check both lines are present in order
	lines := strings.Split(intentText, "\n")
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 lines in intent section, got %d", len(lines))
	}

	// The intent section text should contain both lines
	// Note: lines are trimmed, so first line should be at start
	if !strings.Contains(intentText, "first line") {
		t.Errorf("intent section missing 'first line'")
	}
	if !strings.Contains(intentText, "second line") {
		t.Errorf("intent section missing 'second line'")
	}

	// Verify order: first line should come before second line
	firstIdx := strings.Index(intentText, "first line")
	secondIdx := strings.Index(intentText, "second line")
	if firstIdx < 0 || secondIdx < 0 || firstIdx >= secondIdx {
		t.Errorf("lines not in expected order in intent section")
	}
}

// TestAppendIntentIsIdempotentPerRecord: the archive closure appends the
// intent line BEFORE its git merge can succeed, and a stranded closure
// compensates the item archived->done without removing what already ran. So
// every retry appended another copy — and retrying is the operator's only
// response to a closure that timed out waiting for CI, which made the
// duplication scale with CI slowness rather than with anything the author
// did (B-01KYQJDJJVFC2 measured three, two and two copies of single lines).
func TestAppendIntentIsIdempotentPerRecord(t *testing.T) {
	ws := workspace.Root{Dir: t.TempDir()}
	if err := ws.EnsureScaffold(""); err != nil {
		t.Fatal(err)
	}
	first := "- T-01KYQJDJJVFC2T0NF9MM84YQ41 a task: measured 79 to 59 calls"
	// a retry legitimately carries a different note; it is still the same record
	retry := "- T-01KYQJDJJVFC2T0NF9MM84YQ41 a task: retried after CI concluded"
	for _, l := range []string{first, retry, first} {
		if err := AppendIntent(ws, "", l); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile(ws.SpecPath(""))
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(raw), "T-01KYQJDJJVFC2T0NF9MM84YQ41"); n != 1 {
		t.Fatalf("one intent line per record, got %d:\n%s", n, raw)
	}
	// the FIRST wins — the record of what landed, not the latest retry's note
	if !strings.Contains(string(raw), "measured 79 to 59 calls") {
		t.Fatalf("the first line must be the one kept:\n%s", raw)
	}
	// a different record still appends
	if err := AppendIntent(ws, "", "- P-0007 another record: something else"); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(ws.SpecPath(""))
	if !strings.Contains(string(raw), "P-0007") {
		t.Fatalf("a different record must still append:\n%s", raw)
	}
	// a non-record line (plain prose) is not deduped by ID and still appends
	for i := 0; i < 2; i++ {
		if err := AppendIntent(ws, "", "- plain prose with no record id"); err != nil {
			t.Fatal(err)
		}
	}
	raw, _ = os.ReadFile(ws.SpecPath(""))
	if n := strings.Count(string(raw), "plain prose with no record id"); n != 2 {
		t.Fatalf("non-record lines keep their existing append behavior, got %d", n)
	}
}

// TestRemoveIntentDropsOnlyTheNamedRecord is the other half of the first-wins
// contract above (B-01KYS6ZKRQEHW finding 2). AppendIntent runs before the
// archive edge's git merge can succeed; when that merge strands, the refused
// attempt used to leave its line behind, and first-wins then froze the FAILED
// attempt's note in as the record's outcome forever. RemoveIntent is what
// lets the compensation leave no "first" behind — so it has to drop exactly
// one line and nothing that merely looks like it.
func TestRemoveIntentDropsOnlyTheNamedRecord(t *testing.T) {
	ws := workspace.Root{Dir: t.TempDir()}
	if err := ws.EnsureScaffold(""); err != nil {
		t.Fatal(err)
	}
	doomed := "- T-01KYS6ZKRQEHWT0NF9MM84YQ41 the refused archive: note of an attempt that did not land"
	keeper := "- P-0007 another record: this one really landed"
	prose := "- a prose bullet with no record id at all"
	for _, l := range []string{doomed, keeper, prose} {
		if err := AppendIntent(ws, "", l); err != nil {
			t.Fatal(err)
		}
	}
	if err := RemoveIntent(ws, "", "T-01KYS6ZKRQEHWT0NF9MM84YQ41"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(ws.SpecPath(""))
	if err != nil {
		t.Fatal(err)
	}
	got := string(raw)
	if strings.Contains(got, "T-01KYS6ZKRQEHWT0NF9MM84YQ41") {
		t.Fatalf("the refused record's line survived:\n%s", got)
	}
	if !strings.Contains(got, keeper) {
		t.Fatalf("a different record's line was collateral:\n%s", got)
	}
	if !strings.Contains(got, prose) {
		t.Fatalf("an ID-less prose bullet was collateral:\n%s", got)
	}
	// removing what is not there is a no-op, not an error: the compensation
	// path runs for every refused archive, including those whose AppendIntent
	// was itself a no-op.
	before := got
	if err := RemoveIntent(ws, "", "T-01KYS6ZKRQEHWT0NF9MM84YQ41"); err != nil {
		t.Fatalf("removing an absent record errored: %v", err)
	}
	raw, _ = os.ReadFile(ws.SpecPath(""))
	if string(raw) != before {
		t.Fatalf("a no-op removal rewrote the bundle:\nbefore:\n%s\nafter:\n%s", before, raw)
	}
	// and the record is appendable again afterwards, carrying the RETRY's
	// note — which is the whole point: nothing "first" is left to win.
	retry := "- T-01KYS6ZKRQEHWT0NF9MM84YQ41 the retry: the note of the attempt that landed"
	if err := AppendIntent(ws, "", retry); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(ws.SpecPath(""))
	if !strings.Contains(string(raw), "the note of the attempt that landed") {
		t.Fatalf("after removal the retry's note must be the one recorded:\n%s", raw)
	}
	if n := strings.Count(string(raw), "T-01KYS6ZKRQEHWT0NF9MM84YQ41"); n != 1 {
		t.Fatalf("one intent line per record still, got %d:\n%s", n, raw)
	}
}

// A lookalike bullet OUTSIDE `## intent` must survive: an ID-shaped line in a
// rule's free-form Rationale or in a whitelisted prose section keys exactly
// like a real intent line, and reaching outside the invariant deletes genuine
// content — the same trap AppendIntent's heal already documents.
func TestRemoveIntentStaysInsideTheIntentSection(t *testing.T) {
	ws := workspace.Root{Dir: t.TempDir()}
	if err := ws.EnsureScaffold(""); err != nil {
		t.Fatal(err)
	}
	line := "- T-01KYS6ZKRQEHWT0NF9MM84YQ41 archived: landed"
	if err := AppendIntent(ws, "", line); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(ws.SpecPath(""))
	if err != nil {
		t.Fatal(err)
	}
	// a lookalike in a later section, exactly as a hand-written rationale
	// bullet would read
	withNotes := string(raw) + "\n## notes\n" + line + "\n"
	if err := os.WriteFile(ws.SpecPath(""), []byte(withNotes), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RemoveIntent(ws, "", "T-01KYS6ZKRQEHWT0NF9MM84YQ41"); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(ws.SpecPath(""))
	if n := strings.Count(string(raw), "T-01KYS6ZKRQEHWT0NF9MM84YQ41"); n != 1 {
		t.Fatalf("exactly the `## notes` copy must survive, got %d occurrences:\n%s", n, raw)
	}
	notes := string(raw)[strings.Index(string(raw), "## notes"):]
	if !strings.Contains(notes, line) {
		t.Fatalf("the surviving copy is not the one in `## notes`:\n%s", raw)
	}
}

// TestAppendIntentDedupesPerLineNotPerCall: `line` is not always one line.
// knowledge apply's applyIntentEntry passes a whole prose section — many
// bullets in one call — so keying the whole call on its FIRST bullet's ID
// dropped the entire blob, brand-new records included, whenever that one
// bullet collided, while the tool still reported success. A silently skipped
// intent line is permanent history loss, which made that guard worse than
// the duplication it replaced.
func TestAppendIntentDedupesPerLineNotPerCall(t *testing.T) {
	ws := workspace.Root{Dir: t.TempDir()}
	if err := ws.EnsureScaffold(""); err != nil {
		t.Fatal(err)
	}
	existing := "- T-01KYQJDJJVFC2T0NF9MM84YQ41 already here: original gist"
	if err := AppendIntent(ws, "", existing); err != nil {
		t.Fatal(err)
	}
	// a blob whose FIRST bullet collides, and whose others are brand new
	blob := existing + "\n" +
		"- P-01KYQJDJJVFC2T0NF9MM84YQ42 brand new one: NEWCONTENTONE\n" +
		"- B-01KYQJDJJVFC2T0NF9MM84YQ43 brand new two: NEWCONTENTTWO"
	if err := AppendIntent(ws, "", blob); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(ws.SpecPath(""))
	if err != nil {
		t.Fatal(err)
	}
	got := string(raw)
	for _, want := range []string{"NEWCONTENTONE", "NEWCONTENTTWO"} {
		if !strings.Contains(got, want) {
			t.Fatalf("a colliding first bullet must not drop the rest of the blob; %q missing:\n%s", want, got)
		}
	}
	if n := strings.Count(got, "T-01KYQJDJJVFC2T0NF9MM84YQ41"); n != 1 {
		t.Fatalf("the colliding bullet must still dedupe, got %d copies:\n%s", n, got)
	}
	// re-applying the same blob adds nothing at all
	before := len(got)
	if err := AppendIntent(ws, "", blob); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(ws.SpecPath(""))
	if len(raw) != before {
		t.Fatalf("re-applying a fully-known blob must be a no-op: %d -> %d bytes", before, len(raw))
	}
}

// TestAppendIntentHealsMergeDuplicates: the write-time guard cannot prevent
// duplicates on its own. git gives spec.md a default three-way merge — only
// journal.ndjson and bench.ndjson are declared merge=union — so two branches
// that each appended the SAME line at a different position merge to two
// copies, two independent insertions both kept. Every archive in the worktree
// flow merges, so the guard was necessary and not sufficient
// (B-01KYQR51GXEQN). AppendIntent heals what it finds.
func TestAppendIntentHealsMergeDuplicates(t *testing.T) {
	seed := func(t *testing.T, body string) workspace.Root {
		t.Helper()
		ws := workspace.Root{Dir: t.TempDir()}
		if err := ws.EnsureScaffold(""); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(ws.SpecPath(""), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return ws
	}
	dup := "- T-01KYQR51GXEQNT0NF9MM84YQ41 first record: landed"
	other := "- P-01KYQR51GXEQNT0NF9MM84YQ42 second record: also landed"
	// exactly the shape a merge produces: the same line twice, at different
	// positions, with another record between them
	merged := "---\nschema: v1\n---\n\n## intent\n" + dup + "\n" + other + "\n" + dup + "\n"

	t.Run("healed while appending something new", func(t *testing.T) {
		ws := seed(t, merged)
		if err := AppendIntent(ws, "", "- B-01KYQR51GXEQNT0NF9MM84YQ43 third: new"); err != nil {
			t.Fatal(err)
		}
		raw, _ := os.ReadFile(ws.SpecPath(""))
		if n := strings.Count(string(raw), "T0NF9MM84YQ41"); n != 1 {
			t.Fatalf("the merge duplicate must be healed, got %d copies:\n%s", n, raw)
		}
		for _, want := range []string{"T0NF9MM84YQ42", "T0NF9MM84YQ43"} {
			if !strings.Contains(string(raw), want) {
				t.Fatalf("healing must not drop %s:\n%s", want, raw)
			}
		}
	})

	t.Run("healed even when there is nothing to append", func(t *testing.T) {
		// the caller re-archives a record that is already listed, so the
		// append is a no-op — the heal must still land, or duplicates
		// survive for anyone who never appends again
		ws := seed(t, merged)
		if err := AppendIntent(ws, "", dup); err != nil {
			t.Fatal(err)
		}
		raw, _ := os.ReadFile(ws.SpecPath(""))
		if n := strings.Count(string(raw), "T0NF9MM84YQ41"); n != 1 {
			t.Fatalf("a no-op append must still heal, got %d copies:\n%s", n, raw)
		}
		if !strings.Contains(string(raw), "T0NF9MM84YQ42") {
			t.Fatalf("the other record must survive:\n%s", raw)
		}
	})

	t.Run("a clean file is left byte-identical", func(t *testing.T) {
		clean := "---\nschema: v1\n---\n\n## intent\n" + dup + "\n" + other + "\n"
		ws := seed(t, clean)
		if err := AppendIntent(ws, "", dup); err != nil {
			t.Fatal(err)
		}
		raw, _ := os.ReadFile(ws.SpecPath(""))
		if string(raw) != clean {
			t.Fatalf("a no-op on a clean file must not rewrite it:\nwant %q\ngot  %q", clean, raw)
		}
	})
}

// TestAppendIntentHealDoesNotReachOutsideItsSection: the heal keyed every line
// in the file, so a bullet that merely LOOKS like an intent line — in a rule's
// free-form Rationale, or in one of the whitelisted prose sections
// (notes/design/context, see docs/spec-cascade.md) — collided with the real
// record. Whichever came second in file order was dropped, so a lookalike
// ABOVE `## intent` meant deleting the genuine entry. A heal must not reach
// outside the invariant it enforces.
func TestAppendIntentHealDoesNotReachOutsideItsSection(t *testing.T) {
	id := "T-01KYQR51GXEQNT0NF9MM84YQ41"
	real := "- " + id + " the real record: it landed"
	look := "- " + id + " lookalike bullet, not an intent entry"

	for _, tc := range []struct{ name, body string }{
		{"lookalike BEFORE the intent section", "---\nschema: v1\n---\n\n## SOME-RULE-001\nRationale: see also\n" + look + "\n\n## intent\n" + real + "\n"},
		{"lookalike AFTER the intent section", "---\nschema: v1\n---\n\n## intent\n" + real + "\n\n## notes\n" + look + "\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ws := workspace.Root{Dir: t.TempDir()}
			if err := ws.EnsureScaffold(""); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(ws.SpecPath(""), []byte(tc.body), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := AppendIntent(ws, "", "- B-01KYQR51GXEQNT0NF9MM84YQ99 unrelated: new"); err != nil {
				t.Fatal(err)
			}
			raw, _ := os.ReadFile(ws.SpecPath(""))
			got := string(raw)
			if !strings.Contains(got, real) {
				t.Fatalf("the genuine intent entry must survive:\n%s", got)
			}
			if !strings.Contains(got, look) {
				t.Fatalf("a lookalike outside the section is not ours to delete:\n%s", got)
			}
			if !strings.Contains(got, "YQ99") {
				t.Fatalf("the new record must still be appended:\n%s", got)
			}
		})
	}

	// and a file with NO intent section at all must be left completely alone
	// apart from the section AppendIntent creates
	t.Run("no intent section", func(t *testing.T) {
		ws := workspace.Root{Dir: t.TempDir()}
		if err := ws.EnsureScaffold(""); err != nil {
			t.Fatal(err)
		}
		body := "---\nschema: v1\n---\n\n## notes\n" + look + "\n" + look + "\n"
		if err := os.WriteFile(ws.SpecPath(""), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := AppendIntent(ws, "", real); err != nil {
			t.Fatal(err)
		}
		raw, _ := os.ReadFile(ws.SpecPath(""))
		if n := strings.Count(string(raw), look); n != 2 {
			t.Fatalf("duplicated bullets in another section stay untouched, got %d:\n%s", n, raw)
		}
	})
}

// TestAppendIntentDropsIDLessDebris covers the other half of B-01KYRQXJ99F48:
// a line in the intent span with no record ID matched nothing in the dedupe, so
// it survived every heal and accumulated one copy per call. Every legitimate
// line there carries an ID, so debris is droppable — and dropping it repairs
// the spec.md files that already carry a stray truncation marker.
func TestAppendIntentDropsIDLessDebris(t *testing.T) {
	ws := workspace.Root{Dir: t.TempDir()}
	if err := AppendIntent(ws, "", "- T-0001 first: gist one"); err != nil {
		t.Fatal(err)
	}
	// Simulate the damage already on disk, then let a later append heal it.
	raw, err := os.ReadFile(ws.SpecPath(""))
	if err != nil {
		t.Fatal(err)
	}
	dirty := strings.Replace(string(raw), "- T-0001 first: gist one",
		"- T-0001 first: gist one\n[body truncated at tombstone retention cap]\n[body truncated at tombstone retention cap]", 1)
	if err := os.WriteFile(ws.SpecPath(""), []byte(dirty), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := AppendIntent(ws, "", "- T-0002 second: gist two"); err != nil {
		t.Fatal(err)
	}
	out, err := os.ReadFile(ws.SpecPath(""))
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(out), "[body truncated"); n != 0 {
		t.Errorf("ID-less debris survived the heal (%d copies):\n%s", n, out)
	}
	for _, want := range []string{"- T-0001 first: gist one", "- T-0002 second: gist two"} {
		if strings.Count(string(out), want) != 1 {
			t.Errorf("want exactly one %q:\n%s", want, out)
		}
	}
}
