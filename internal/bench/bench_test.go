package bench

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestScriptCoversAllStatesByConstruction: the script must NAME every state in
// its move destinations — if a state can only be reached implicitly, the
// coverage claim rests on runtime luck rather than on the script.
func TestScriptCoversAllStatesByConstruction(t *testing.T) {
	var all strings.Builder
	for _, st := range Script() {
		all.WriteString(st.Args + "\n")
	}
	for _, state := range []string{"submitted", "approved", "active", "done", "archived", "rejected", "draft"} {
		if !strings.Contains(all.String(), `"`+state+`"`) {
			t.Fatalf("script never targets state %q", state)
		}
	}
	// blocked is unreachable as a move destination BY DESIGN (only Escalate
	// sets it); the script reaches it via round exhaustion, asserted live.
}

// TestDisambiguateRewritesFromRefusal pins the agent-realistic recovery: the
// stale prefix is replaced by the refusal's extending candidate, in both the
// args and the capture table.
func TestDisambiguateRewritesFromRefusal(t *testing.T) {
	captured := map[string]string{"ID": "T-01ABC"}
	args, ok := disambiguate(`{"id":"T-01ABC","to":"done"}`,
		"! ARG E T-01ABC ambiguous prefix — 2 records: T-01ABCDE T-01ABCDF", captured)
	if !ok || !strings.Contains(args, "T-01ABCDE") || captured["ID"] != "T-01ABCDE" {
		t.Fatalf("disambiguate failed: %q %v %v", args, ok, captured)
	}
}

// TestFullRunAgainstBuiltBinary is the harness's own end-to-end: build the
// binary once, run the full script, demand validity. Skipped in -short since
// it builds and drives a real lifecycle.
func TestFullRunAgainstBuiltBinary(t *testing.T) {
	if testing.Short() {
		t.Skip("full lifecycle run: skipped in -short")
	}
	bin := t.TempDir() + "/spx"
	cmd := exec.Command("go", "build", "-o", bin, "../..")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v: %s", err, out)
	}
	res, err := Run(bin)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Valid {
		t.Fatalf("baseline run invalid:\n%s", Report(res))
	}
	for _, state := range coverageStates {
		if !res.Coverage[state] {
			t.Fatalf("state %s not covered:\n%s", state, Report(res))
		}
	}
}

// TestSeededFixtureRendersMultiDirCleanInventory pins fixture v2's two
// load-bearing properties (T-01KYDT): the seeded workspace must present a
// MULTI-dir rules inventory (v1's one-dir workspace underestimated the
// state-surface change class 40x), and the seeds must be lint-CLEAN —
// findings would put their dirs on every state listing and mask the
// healthy-inventory collapse the enrichment exists to measure. Skipped in
// -short (builds the binary).
func TestSeededFixtureRendersMultiDirCleanInventory(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and drives the real binary: skipped in -short")
	}
	bin := t.TempDir() + "/spx"
	cmd := exec.Command("go", "build", "-o", bin, "../..")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v: %s", err, out)
	}

	dir := t.TempDir()
	if err := Fixture(dir); err != nil {
		t.Fatal(err)
	}
	if err := Seed(bin, dir); err != nil {
		t.Fatal(err)
	}
	out, refused, err := callOnce(bin, dir, "state", "{}")
	if err != nil || refused {
		t.Fatalf("state over seeded fixture: refused=%v err=%v\n%s", refused, err, out)
	}
	// 8 seeded rules across 5 dirs (root included, B-01KYE0RCK), zero
	// findings. The summary line is the collapse's rendering of a healthy
	// inventory; per-dir listing lines must be absent because nothing is
	// dirty.
	if !strings.Contains(out, "ok rules total=8 dirs=5 findings=0") {
		t.Fatalf("seeded inventory summary missing or wrong (seeds not clean, or seed count drifted):\n%s", out)
	}
	if strings.Contains(out, "ok dir ") {
		t.Fatalf("healthy seeded dirs must collapse into the summary, not list:\n%s", out)
	}

	// The straight path the judge brief promises must exist (B-01KYE0RCK):
	// a fresh seeded workspace answers check with no uncovered-gap line for
	// a diligent agent to rabbit-hole into, and no E finding. Environmental
	// W degradations are tolerated deliberately — a CI runner whose older
	// toolchain disables the typed pass answers TYPED W (B-01KYDM taxonomy),
	// and failing on that would measure the runner, not the fixture; the
	// first version of this assertion demanded plain ok and went red on
	// exactly that runner.
	checkOut, refused, err := callOnce(bin, dir, "check", "{}")
	if err != nil || refused {
		t.Fatalf("check on seeded fixture: refused=%v err=%v\n%s", refused, err, checkOut)
	}
	if strings.Contains(checkOut, "uncovered") {
		t.Fatalf("seeded fixture still reports an uncovered gap:\n%s", checkOut)
	}
	if strings.Contains(checkOut, " E ") {
		t.Fatalf("seeded fixture check carries an E finding:\n%s", checkOut)
	}
}

// TestAgentJudgePrepAndScore drives the whole stage-4 loop without an
// agent: prep builds brief, shim, and seeded fixture; a scripted stand-in
// then reaches the brief's goals THROUGH THE SHIM (so metering is
// exercised end to end); score must report valid with a positive metered
// byte count. A second workspace left short of the goals must score
// invalid. Skipped in -short (builds the binary).
func TestAgentJudgePrepAndScore(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and drives the real binary: skipped in -short")
	}
	bin := t.TempDir() + "/spx"
	cmd := exec.Command("go", "build", "-o", bin, "../..")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v: %s", err, out)
	}

	dir := t.TempDir()
	brief, shim, _, err := AgentPrep(bin, dir, false, false, "basic")
	if err != nil {
		t.Fatalf("AgentPrep: %v", err)
	}
	briefBytes, err := os.ReadFile(brief)
	if err != nil {
		t.Fatal(err)
	}
	for _, must := range []string{judgeTaskTitle, judgeBugTitle, "state, draft, move"} {
		if !strings.Contains(string(briefBytes), must) {
			t.Fatalf("brief missing %q:\n%s", must, briefBytes)
		}
	}
	if info, err := os.Stat(shim); err != nil || info.Mode()&0o100 == 0 {
		t.Fatalf("shim not executable: %v %v", info, err)
	}

	// The scripted stand-in reaches the goals through the shim.
	viaShim := func(tool, args string) string {
		out, err := exec.Command(shim, "call", "-root", dir, tool, args).CombinedOutput()
		if err != nil {
			if _, isExit := err.(*exec.ExitError); !isExit {
				t.Fatalf("shim call %s: %v: %s", tool, err, out)
			}
		}
		return string(out)
	}
	taskOut := viaShim("draft", `{"kind":"task","title":"`+judgeTaskTitle+`"}`)
	m := reItemID.FindStringSubmatch(taskOut)
	if m == nil {
		t.Fatalf("no task ID in: %s", taskOut)
	}
	viaShim("move", `{"id":"`+m[1]+`","to":"archived","note":"judged"}`)
	bugOut := viaShim("draft", `{"kind":"bug","title":"`+judgeBugTitle+`"}`)
	if m = reItemID.FindStringSubmatch(bugOut); m == nil {
		t.Fatalf("no bug ID in: %s", bugOut)
	}
	viaShim("move", `{"id":"`+m[1]+`","to":"rejected","note":"bogus by design"}`)
	viaShim("check", `{}`)

	sc, err := ScoreAgentRun(bin, dir)
	if err != nil {
		t.Fatalf("ScoreAgentRun: %v", err)
	}
	if !sc.Valid {
		t.Fatalf("scripted goal run scored invalid:\n%s", AgentReport(sc))
	}
	if sc.Calls < 5 || sc.Bytes <= 0 {
		t.Fatalf("metering did not capture the calls: %+v", sc)
	}

	// Short-of-goals workspace: prep only, nothing driven — invalid, and
	// the missing goals are named as absent.
	dir2 := t.TempDir()
	if _, _, _, err := AgentPrep(bin, dir2, false, false, "basic"); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(filepath.Join(dir2, "meter.sh"), "call", "-root", dir2, "state", "{}").CombinedOutput(); err != nil {
		t.Fatalf("one metered call in dir2: %v: %s", err, out)
	}
	sc2, err := ScoreAgentRun(bin, dir2)
	if err != nil {
		t.Fatal(err)
	}
	if sc2.Valid {
		t.Fatal("goal-less run scored valid")
	}
	if !strings.Contains(AgentReport(sc2), "task=absent") {
		t.Fatalf("missing goal not named absent:\n%s", AgentReport(sc2))
	}
}

// TestAgentReportViolationsNeverSilent pins B-01KYJ67RF9: a run whose
// goals all held but which a violation voided must SHOW the violation and
// say so in the verdict — two 2026-07-27 outcome judges scored
// task=archived check=true first-pass 5/5 yet the report claimed "goals
// not reached" and never rendered why.
func TestAgentReportViolationsNeverSilent(t *testing.T) {
	sc := AgentScore{
		Scenario: "outcome", Calls: 10, Bytes: 100, Tokens: 25,
		TaskState: "archived", BugState: "rejected", CheckOK: true,
		GoalsOK:    true,
		Violations: []string{"vacuous test: judge_test.go asserts nothing"},
	}
	out := AgentReport(sc)
	if !strings.Contains(out, "agent violation vacuous test: judge_test.go asserts nothing") {
		t.Fatalf("the violation must render before the verdict:\n%s", out)
	}
	if !strings.Contains(out, "INVALID — violations (1)") {
		t.Fatalf("goals-held + violation must say violations, not goals:\n%s", out)
	}
	if strings.Contains(out, "goals not reached") {
		t.Fatalf("the mislabel is back:\n%s", out)
	}
	// goals actually red: the old text stays
	red := AgentScore{Scenario: "outcome", TaskState: "done", CheckOK: false}
	if out := AgentReport(red); !strings.Contains(out, "INVALID — goals not reached") {
		t.Fatalf("goals-red must keep the goals-not-reached verdict:\n%s", out)
	}
	// valid runs render no violation machinery
	green := AgentScore{Scenario: "outcome", GoalsOK: true, Valid: true}
	if out := AgentReport(green); !strings.Contains(out, "valid — goals reached") || strings.Contains(out, "violation") {
		t.Fatalf("a valid run must stay clean:\n%s", out)
	}
}

// TestAggregateReportSpreadAndExitGate pins the n-run judge summary
// (T-01KYE2): min/median/max spreads (lower-middle median — tiny samples
// carry no half-value precision), the validity ratio, and the all-valid
// gate — one failing judge in three is a regression however good the
// median looks.
func TestAggregateReportSpreadAndExitGate(t *testing.T) {
	scores := []AgentScore{
		{Calls: 12, Bytes: 1038, Tokens: 259, TaskState: "archived", BugState: "rejected", CheckOK: true, Valid: true},
		{Calls: 46, Bytes: 5896, Tokens: 1474, TaskState: "archived", BugState: "rejected", CheckOK: true, Valid: true},
		{Calls: 14, Bytes: 1119, Tokens: 279, TaskState: "archived", BugState: "", CheckOK: true, Valid: false},
	}
	out, allValid := AggregateReport([]string{"e", "c", "f"}, scores)
	if allValid {
		t.Fatal("one invalid run must fail the aggregate gate")
	}
	if !strings.Contains(out, "agents n=3 valid=2/3 calls=12/14/46 bytes=1038/1119/5896") {
		t.Fatalf("aggregate line wrong:\n%s", out)
	}
	// per-run lines carry their labels
	if !strings.Contains(out, "c agent calls=46") || !strings.Contains(out, "f agent goal task=archived bug=absent") {
		t.Fatalf("per-run labeling wrong:\n%s", out)
	}

	// all-valid pair gates true
	_, ok := AggregateReport([]string{"a", "b"}, scores[:2])
	if !ok {
		t.Fatal("two valid runs must pass the gate")
	}
}

// TestAgentPrepWithManifest pins T-01KYE3X: -with-manifest prepends the
// connect-time manifest to the brief and records its size in the sidecar;
// plain prep produces neither; the score report carries the session line
// exactly when the sidecar exists.
func TestAgentPrepWithManifest(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and drives the real binary: skipped in -short")
	}
	bin := t.TempDir() + "/spx"
	cmd := exec.Command("go", "build", "-o", bin, "../..")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v: %s", err, out)
	}

	dir := t.TempDir()
	brief, _, _, err := AgentPrep(bin, dir, true, false, "basic")
	if err != nil {
		t.Fatal(err)
	}
	briefBytes, err := os.ReadFile(brief)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(briefBytes), "Connect-time server instructions") ||
		!strings.Contains(string(briefBytes), "spectackle — spec-lifecycle server") {
		t.Fatalf("with-manifest brief missing the manifest preamble:\n%.200s", briefBytes)
	}
	sidecar, err := os.ReadFile(filepath.Join(dir, "manifest.size"))
	if err != nil {
		t.Fatalf("manifest.size sidecar missing: %v", err)
	}
	manifestOut, err := exec.Command(bin, "manifest").Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(sidecar)) != fmt.Sprint(len(manifestOut)) {
		t.Fatalf("sidecar %s does not match manifest length %d", sidecar, len(manifestOut))
	}

	// The session line appears in the score report for this run.
	if out, err := exec.Command(filepath.Join(dir, "meter.sh"), "call", "-root", dir, "state", "{}").CombinedOutput(); err != nil {
		t.Fatalf("metered call: %v: %s", err, out)
	}
	sc, err := ScoreAgentRun(bin, dir)
	if err != nil {
		t.Fatal(err)
	}
	if sc.ManifestBytes != len(manifestOut) {
		t.Fatalf("score ManifestBytes = %d, want %d", sc.ManifestBytes, len(manifestOut))
	}
	if !strings.Contains(AgentReport(sc), "agent session=") {
		t.Fatalf("session line missing:\n%s", AgentReport(sc))
	}

	// Plain prep: no sidecar, no session line, brief without preamble.
	dir2 := t.TempDir()
	brief2, _, _, err := AgentPrep(bin, dir2, false, false, "basic")
	if err != nil {
		t.Fatal(err)
	}
	b2, _ := os.ReadFile(brief2)
	if strings.Contains(string(b2), "Connect-time") {
		t.Fatal("plain brief gained the manifest preamble")
	}
	if _, err := os.Stat(filepath.Join(dir2, "manifest.size")); !os.IsNotExist(err) {
		t.Fatal("plain prep wrote a manifest.size sidecar")
	}
}

// TestAgentPrepWithSchema pins T-01KYSPFXHNFZ7: -with-schema prepends the
// VERBATIM tools/list payload to the brief and records its size in the
// schema.size sidecar, the scored report names the regime, and the DEFAULT
// stays name-only so historical batches — every reference curve in
// docs/bench-curves.md was measured name-only — cannot be silently mixed
// with a with-schema batch. The default half asserts ABSENCE, not merely
// the presence of the other mode: a flipped default would pass a
// presence-only test.
func TestAgentPrepWithSchema(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and drives the real binary: skipped in -short")
	}
	bin := t.TempDir() + "/spx"
	cmd := exec.Command("go", "build", "-o", bin, "../..")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v: %s", err, out)
	}

	dir := t.TempDir()
	brief, _, _, err := AgentPrep(bin, dir, false, true, "basic")
	if err != nil {
		t.Fatal(err)
	}
	briefBytes, err := os.ReadFile(brief)
	if err != nil {
		t.Fatal(err)
	}
	// Ground truth from the same probe the mode injects, so the assertion
	// cannot drift when a tool description is edited.
	payload, n := toolsListPayload(bin)
	if n == 0 {
		t.Fatal("toolsListPayload metered 0 — the probe failed, not the mode")
	}
	// Anchor on a TOP-LEVEL tool Description, never an inputSchema property
	// description. Measured on the live payload, the two are encoded
	// differently: inputSchema is marshalled by the default HTML-escaping
	// encoder, so get's property text arrives as `sec:<dir>`, while
	// the top-level description fields carry the same `sec:<dir>#<name>`
	// unescaped. A test anchored on the wrong one fails for the wrong reason
	// — encoding, not injection.
	var decoded struct {
		Result struct {
			Tools []struct {
				Name        string `json:"name"`
				Description string `json:"description"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
		t.Fatalf("decode tools/list payload: %v", err)
	}
	var desc string
	for _, tool := range decoded.Result.Tools {
		if tool.Name == "get" {
			desc = tool.Description
		}
	}
	if len(desc) < 40 {
		t.Fatalf("no usable top-level description for tool get (%q) — the payload shape changed", desc)
	}
	if !strings.Contains(string(briefBytes), desc) {
		t.Fatalf("with-schema brief does not carry the get description VERBATIM:\nwant substring: %q", desc)
	}
	if !strings.Contains(string(briefBytes), "Connect-time tool schemas") {
		t.Fatalf("with-schema brief missing the schema header:\n%.200s", briefBytes)
	}

	sidecar, err := os.ReadFile(filepath.Join(dir, "schema.size"))
	if err != nil {
		t.Fatalf("schema.size sidecar missing: %v", err)
	}
	if strings.TrimSpace(string(sidecar)) != fmt.Sprint(n) {
		t.Fatalf("sidecar %s does not match tools/list payload length %d", sidecar, n)
	}
	// Magnitude guard, same discipline as TestSchemaMeteringIsRealAndInert:
	// the sidecar must prove a real read, not a silent zero dressed up as a
	// measurement. Deliberately loose — a byte count every schema edit had to
	// chase would be a maintenance tax, not a signal.
	if n < 4000 {
		t.Errorf("schema.size = %d; under 4KB means the tools/list read silently failed", n)
	}

	if out, err := exec.Command(filepath.Join(dir, "meter.sh"), "call", "-root", dir, "state", "{}").CombinedOutput(); err != nil {
		t.Fatalf("metered call: %v: %s", err, out)
	}
	sc, err := ScoreAgentRun(bin, dir)
	if err != nil {
		t.Fatal(err)
	}
	if sc.SchemaBytes != n {
		t.Fatalf("score SchemaBytes = %d, want %d", sc.SchemaBytes, n)
	}
	rep := AgentReport(sc)
	if !strings.Contains(rep, "brief-mode with-schema") {
		t.Fatalf("report does not name the with-schema regime:\n%s", rep)
	}
	// The session line must render for -with-schema ALONE: gated on the
	// manifest sidecar only, it would go missing exactly when the largest
	// session cost is present.
	if !strings.Contains(rep, fmt.Sprintf("agent session=%dB (schema %d + manifest 0 + tools %d)", n+sc.Bytes, n, sc.Bytes)) {
		t.Fatalf("session line missing or not schema-aware:\n%s", rep)
	}

	// Both flags together: the FILE order must be manifest-then-schema,
	// matching a real session — instructions arrive at initialize, tools/list
	// only after. Both branches prepend, so this order is the opposite of the
	// order the branches run in, which is exactly why it is pinned.
	dirBoth := t.TempDir()
	briefBoth, _, _, err := AgentPrep(bin, dirBoth, true, true, "basic")
	if err != nil {
		t.Fatal(err)
	}
	bothBytes, err := os.ReadFile(briefBoth)
	if err != nil {
		t.Fatal(err)
	}
	iMan := strings.Index(string(bothBytes), "Connect-time server instructions")
	iSch := strings.Index(string(bothBytes), "Connect-time tool schemas")
	if iMan < 0 || iSch < 0 {
		t.Fatalf("both-flag brief missing a preamble: manifest at %d, schema at %d", iMan, iSch)
	}
	if iMan > iSch {
		t.Fatalf("both-flag brief is schema-then-manifest (manifest at %d, schema at %d); a real session reads the manifest FIRST", iMan, iSch)
	}

	// An UNMETERABLE schema side must REFUSE the prep, not sail on with a
	// zero. A with-schema run whose brief carries no schema would be set
	// beside name-only history as if it were a with-schema batch — the exact
	// unlabeled-regime mixing this whole mode exists to prevent. Driven
	// through a wrapper that forwards everything except `serve`, so the
	// tools/list probe fails while fixture and seed still succeed.
	stubDir := t.TempDir()
	stub := filepath.Join(stubDir, "spx-noserve")
	if err := os.WriteFile(stub, fmt.Appendf(nil,
		"#!/bin/sh\nif [ \"$1\" = serve ]; then exit 1; fi\nexec %q \"$@\"\n", bin), 0o755); err != nil {
		t.Fatal(err)
	}
	dirStub := t.TempDir()
	if _, _, _, err := AgentPrep(stub, dirStub, false, true, "basic"); err == nil {
		t.Fatal("with-schema prep succeeded although tools/list metered 0 bytes")
	} else if !strings.Contains(err.Error(), "not metered") {
		t.Fatalf("unexpected error for an unmeterable schema side: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dirStub, "schema.size")); !os.IsNotExist(err) {
		t.Fatal("refused prep still wrote a schema.size sidecar")
	}

	// Default mode: no schema anywhere, and the report says so.
	dir2 := t.TempDir()
	brief2, _, _, err := AgentPrep(bin, dir2, false, false, "basic")
	if err != nil {
		t.Fatal(err)
	}
	b2, _ := os.ReadFile(brief2)
	if strings.Contains(string(b2), "inputSchema") {
		t.Fatal("default brief gained the tools/list payload — the name-only default flipped")
	}
	if _, err := os.Stat(filepath.Join(dir2, "schema.size")); !os.IsNotExist(err) {
		t.Fatal("default prep wrote a schema.size sidecar")
	}
	if out, err := exec.Command(filepath.Join(dir2, "meter.sh"), "call", "-root", dir2, "state", "{}").CombinedOutput(); err != nil {
		t.Fatalf("metered call: %v: %s", err, out)
	}
	sc2, err := ScoreAgentRun(bin, dir2)
	if err != nil {
		t.Fatal(err)
	}
	if sc2.SchemaBytes != 0 {
		t.Fatalf("default run scored SchemaBytes = %d, want 0", sc2.SchemaBytes)
	}
	rep2 := AgentReport(sc2)
	if !strings.Contains(rep2, "agent brief-mode name-only") {
		t.Fatalf("default report is not labeled name-only — an unlabeled batch is worse than no batch:\n%s", rep2)
	}
	if strings.Contains(rep2, "with-schema") {
		t.Fatalf("default report claims the with-schema regime:\n%s", rep2)
	}
}

// TestTrickyScenarioPrepAndScore drives the side-state scenario (T-01KYE4)
// without an agent: tricky prep writes the scenario sidecar and its own
// brief; a scripted stand-in reaches all three goals through the shim (rule
// with slots, reopen loop to blocked, decide rescope); the scorer must
// report valid with the tricky goal line. An unfinished tricky workspace
// (rule only) must score invalid. Skipped in -short (builds the binary).
func TestTrickyScenarioPrepAndScore(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and drives the real binary: skipped in -short")
	}
	bin := t.TempDir() + "/spx"
	cmd := exec.Command("go", "build", "-o", bin, "../..")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v: %s", err, out)
	}

	dir := t.TempDir()
	brief, shim, _, err := AgentPrep(bin, dir, false, false, "tricky")
	if err != nil {
		t.Fatal(err)
	}
	briefBytes, _ := os.ReadFile(brief)
	for _, must := range []string{trickyRuleStem, trickyTaskTitle, "rescope"} {
		if !strings.Contains(string(briefBytes), must) {
			t.Fatalf("tricky brief missing %q", must)
		}
	}

	viaShim := func(tool, args string) string {
		out, err := exec.Command(shim, "call", "-root", dir, tool, args).CombinedOutput()
		if err != nil {
			if _, isExit := err.(*exec.ExitError); !isExit {
				t.Fatalf("shim call %s: %v: %s", tool, err, out)
			}
		}
		return string(out)
	}

	// Goal 1: the rule.
	viaShim("rule", `{"op":"add","dir":"api","stem":"`+trickyRuleStem+`","pattern":"U","system":"judged api","response":"terminate with exit code 0"}`)
	// Goal 2: the reopen loop into blocked, resolved via rescope.
	taskOut := viaShim("draft", `{"kind":"task","title":"`+trickyTaskTitle+`"}`)
	m := reItemID.FindStringSubmatch(taskOut)
	if m == nil {
		t.Fatalf("no task ID in: %s", taskOut)
	}
	id := m[1]
	for _, to := range []string{"active", "done", "active", "done"} {
		viaShim("move", `{"id":"`+id+`","to":"`+to+`"}`)
	}
	escOut := viaShim("move", `{"id":"`+id+`","to":"active"}`) // exhausts max_rounds=2
	adr := reDecideID.FindStringSubmatch(escOut)
	if adr == nil {
		t.Fatalf("escalation did not name a decision:\n%s", escOut)
	}
	viaShim("decide", `{"op":"answer","id":"`+adr[1]+`","choose":"rescope"}`)
	viaShim("check", `{}`)

	sc, err := ScoreAgentRun(bin, dir)
	if err != nil {
		t.Fatal(err)
	}
	if sc.Scenario != "tricky" || !sc.Valid {
		t.Fatalf("tricky scripted run scored invalid:\n%s", AgentReport(sc))
	}
	if !strings.Contains(AgentReport(sc), "agent goal rule=true task=draft decide=true") {
		t.Fatalf("tricky goal line wrong:\n%s", AgentReport(sc))
	}

	// Rule-only workspace: invalid, decide and task goals absent.
	dir2 := t.TempDir()
	_, shim2, _, err := AgentPrep(bin, dir2, false, false, "tricky")
	if err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(shim2, "call", "-root", dir2, "rule",
		`{"op":"add","dir":"api","stem":"`+trickyRuleStem+`","pattern":"U","system":"judged api","response":"terminate with exit code 0"}`).CombinedOutput(); err != nil {
		t.Fatalf("rule in dir2: %v: %s", err, out)
	}
	sc2, err := ScoreAgentRun(bin, dir2)
	if err != nil {
		t.Fatal(err)
	}
	if sc2.Valid {
		t.Fatal("rule-only tricky run scored valid")
	}
}

// TestMeterTamperDetection pins B-01KYE6G's three checks against a real
// prepped workspace: a straight shim-driven run stays valid; a mid-file
// deletion breaks sequence contiguity; a regenerated log fails the nonce;
// a tail truncation — invisible to both, since remaining lines stay
// contiguous and carry the true nonce — is caught by the journal
// write-event delta, because the erased calls still wrote their events.
// Each yields DISQUALIFIED, which is never counted valid and is labeled
// apart from invalid in the aggregate. Skipped in -short (builds).
func TestMeterTamperDetection(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and drives the real binary: skipped in -short")
	}
	bin := t.TempDir() + "/spx"
	cmd := exec.Command("go", "build", "-o", bin, "../..")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v: %s", err, out)
	}

	prep := func() (dir, shim string) {
		d := t.TempDir()
		_, sh, _, err := AgentPrep(bin, d, false, false, "basic")
		if err != nil {
			t.Fatal(err)
		}
		return d, sh
	}
	drive := func(dir, shim string) {
		taskOut, _ := exec.Command(shim, "call", "-root", dir, "draft", `{"kind":"task","title":"`+judgeTaskTitle+`"}`).CombinedOutput()
		m := reItemID.FindStringSubmatch(string(taskOut))
		if m == nil {
			t.Fatalf("no task ID: %s", taskOut)
		}
		exec.Command(shim, "call", "-root", dir, "move", `{"id":"`+m[1]+`","to":"archived","note":"x"}`).Run()
		bugOut, _ := exec.Command(shim, "call", "-root", dir, "draft", `{"kind":"bug","title":"`+judgeBugTitle+`"}`).CombinedOutput()
		if m = reItemID.FindStringSubmatch(string(bugOut)); m == nil {
			t.Fatalf("no bug ID: %s", bugOut)
		}
		exec.Command(shim, "call", "-root", dir, "move", `{"id":"`+m[1]+`","to":"rejected","note":"y"}`).Run()
	}

	// Straight run: valid, not disqualified.
	dir, shim := prep()
	drive(dir, shim)
	sc, err := ScoreAgentRun(bin, dir)
	if err != nil {
		t.Fatal(err)
	}
	if sc.Disqualified || !sc.Valid {
		t.Fatalf("straight run misjudged:\n%s", AgentReport(sc))
	}

	meterPath := filepath.Join(dir, "meter.log")
	pristine, err := os.ReadFile(meterPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(pristine), "\n"), "\n")
	if len(lines) < 3 {
		t.Fatalf("need at least 3 metered lines, have %d", len(lines))
	}

	// Mid-file deletion: drop line 2 — sequence gap.
	tampered := append([]string{lines[0]}, lines[2:]...)
	os.WriteFile(meterPath, []byte(strings.Join(tampered, "\n")+"\n"), 0o644)
	if sc, _ = ScoreAgentRun(bin, dir); !sc.Disqualified || !strings.Contains(sc.DisqualifyReason, "sequence broken") {
		t.Fatalf("mid-file deletion not caught:\n%s", AgentReport(sc))
	}

	// Regenerated log: right shape, wrong nonce.
	fake := make([]string, len(lines))
	for i := range lines {
		f := strings.Fields(lines[i])
		f[1] = "deadbeefdeadbeef"
		fake[i] = strings.Join(f, " ")
	}
	os.WriteFile(meterPath, []byte(strings.Join(fake, "\n")+"\n"), 0o644)
	if sc, _ = ScoreAgentRun(bin, dir); !sc.Disqualified || !strings.Contains(sc.DisqualifyReason, "nonce") {
		t.Fatalf("nonce mismatch not caught:\n%s", AgentReport(sc))
	}

	// Tail truncation: keep only line 1 — contiguous, true nonce, but the
	// journal knows how many writes happened.
	os.WriteFile(meterPath, []byte(lines[0]+"\n"), 0o644)
	if sc, _ = ScoreAgentRun(bin, dir); !sc.Disqualified || !strings.Contains(sc.DisqualifyReason, "journal") {
		t.Fatalf("tail truncation not caught:\n%s", AgentReport(sc))
	}

	// The aggregate labels disqualified apart and fails the gate.
	out, ok := AggregateReport([]string{"x"}, []AgentScore{sc})
	if ok || !strings.Contains(out, "disqualified=1") {
		t.Fatalf("aggregate labeling wrong:\n%s", out)
	}
}

// TestWorktreeScenarioPrepAndScore drives the swarm-core scenario
// (T-01KYE9) without an agent: draft, approve, work start, an edit under
// the root start reports, submit with the explicit item — every shim call
// its own process, so the B-01KYE8 reattach is exercised through the
// metered surface — then done and check; the scorer must report valid with
// the worktree goal line. A workspace whose file was edited directly on
// main without work calls must score invalid on the flow column. Skipped
// in -short (builds the binary).
func TestWorktreeScenarioPrepAndScore(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and drives the real binary: skipped in -short")
	}
	bin := t.TempDir() + "/spx"
	cmd := exec.Command("go", "build", "-o", bin, "../..")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v: %s", err, out)
	}

	dir := t.TempDir()
	brief, shim, _, err := AgentPrep(bin, dir, false, false, "worktree")
	if err != nil {
		t.Fatal(err)
	}
	briefBytes, _ := os.ReadFile(brief)
	for _, must := range []string{worktreeTaskTitle, "returns 7", "read-only"} {
		if !strings.Contains(string(briefBytes), must) {
			t.Fatalf("worktree brief missing %q", must)
		}
	}

	// One stable agent identity across the per-call processes — exactly
	// what SPECTACKLE_AGENT provides real CLI flows and every judge spawn
	// sets; without it each process mints a random agent and the B-01KYE8
	// identity gate correctly refuses the reattach.
	viaShim := func(tool, args string) string {
		c := exec.Command(shim, "call", "-root", dir, tool, args)
		c.Env = append(os.Environ(), "SPECTACKLE_AGENT=worktree-sim")
		out, err := c.CombinedOutput()
		if err != nil {
			if _, isExit := err.(*exec.ExitError); !isExit {
				t.Fatalf("shim call %s: %v: %s", tool, err, out)
			}
		}
		return string(out)
	}

	taskOut := viaShim("draft", `{"kind":"task","title":"`+worktreeTaskTitle+`"}`)
	m := reItemID.FindStringSubmatch(taskOut)
	if m == nil {
		t.Fatalf("no task ID in: %s", taskOut)
	}
	id := m[1]
	viaShim("move", `{"id":"`+id+`","to":"approved"}`)
	startOut := viaShim("work", `{"op":"start","item":"`+id+`"}`)
	wm := regexp.MustCompile(`(?m)^wt \S+ open (.+)$`).FindStringSubmatch(startOut)
	if wm == nil {
		t.Fatalf("start did not report a root:\n%s", startOut)
	}
	if err := os.WriteFile(filepath.Join(wm[1], "api", "api.go"),
		[]byte("package api\n\n// Serve is the fixture's api entry point.\nfunc Serve() int { return 7 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	subOut := viaShim("work", `{"op":"submit","item":"`+id+`"}`)
	if !strings.Contains(subOut, "merged to main") {
		t.Fatalf("submit did not merge:\n%s", subOut)
	}
	viaShim("move", `{"id":"`+id+`","to":"done"}`)
	viaShim("check", `{}`)

	sc, err := ScoreAgentRun(bin, dir)
	if err != nil {
		t.Fatal(err)
	}
	if sc.Scenario != "worktree" || !sc.Valid {
		t.Fatalf("worktree scripted run scored invalid:\n%s", AgentReport(sc))
	}
	if !strings.Contains(AgentReport(sc), "agent goal change=true task=done flow=true") {
		t.Fatalf("worktree goal line wrong:\n%s", AgentReport(sc))
	}

	// The cheat: direct edit on main, no work calls — flow must fail it.
	dir2 := t.TempDir()
	_, shim2, _, err := AgentPrep(bin, dir2, false, false, "worktree")
	if err != nil {
		t.Fatal(err)
	}
	cheatOut, _ := exec.Command(shim2, "call", "-root", dir2, "draft", `{"kind":"task","title":"`+worktreeTaskTitle+`"}`).CombinedOutput()
	cm := reItemID.FindStringSubmatch(string(cheatOut))
	if cm == nil {
		t.Fatalf("no task ID in cheat run: %s", cheatOut)
	}
	os.WriteFile(filepath.Join(dir2, "api", "api.go"), []byte("package api\n\nfunc Serve() int { return 7 }\n"), 0o644)
	exec.Command(shim2, "call", "-root", dir2, "move", `{"id":"`+cm[1]+`","to":"done"}`).Run()
	sc2, err := ScoreAgentRun(bin, dir2)
	if err != nil {
		t.Fatal(err)
	}
	if sc2.Valid || sc2.DecideOK {
		t.Fatalf("direct-edit cheat scored valid / flow=true:\n%s", AgentReport(sc2))
	}
}

// TestShimGuardAndNonceAnchor pins B-01KYEA's two halves: the shim refuses
// every subcommand but call (the live judge reset its evidence by running
// bench -agent-prep THROUGH the shim), and the out-of-band nonce anchor —
// the value prep returns to the orchestrator — disqualifies a re-prepped
// workspace that every in-workspace check would call consistent. Skipped
// in -short (builds the binary).
func TestShimGuardAndNonceAnchor(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and drives the real binary: skipped in -short")
	}
	bin := t.TempDir() + "/spx"
	cmd := exec.Command("go", "build", "-o", bin, "../..")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v: %s", err, out)
	}

	dir := t.TempDir()
	_, shim, nonce1, err := AgentPrep(bin, dir, false, false, "basic")
	if err != nil {
		t.Fatal(err)
	}
	if nonce1 == "" {
		t.Fatal("prep returned an empty nonce")
	}

	// The guard: bench and manifest through the shim refuse without
	// executing — and without metering a line.
	for _, sub := range []string{"bench", "manifest"} {
		out, err := exec.Command(shim, sub, "-agent-prep", dir).CombinedOutput()
		if err == nil || !strings.Contains(string(out), "! SHIM E") {
			t.Fatalf("shim let %q through: err=%v out=%s", sub, err, out)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "meter.log")); !os.IsNotExist(err) {
		t.Fatal("refused shim invocations must not meter")
	}

	// One legitimate metered call, then the honest anchor passes...
	if out, err := exec.Command(shim, "call", "-root", dir, "state", "{}").CombinedOutput(); err != nil {
		t.Fatalf("metered call: %v: %s", err, out)
	}
	sc, err := ScoreAgentRunAnchored(bin, dir, nonce1)
	if err != nil {
		t.Fatal(err)
	}
	if sc.Disqualified {
		t.Fatalf("honest anchor disqualified:\n%s", AgentReport(sc))
	}

	// ...and a re-prep (fresh nonce, consistent workspace) is caught ONLY
	// by the anchor: every in-workspace check sees a coherent picture.
	if _, _, nonce2, err := AgentPrep(bin, dir, false, false, "basic"); err != nil {
		t.Fatal(err)
	} else if nonce2 == nonce1 {
		t.Fatal("re-prep kept the nonce — the anchor would be meaningless")
	}
	if out, err := exec.Command(shim, "call", "-root", dir, "state", "{}").CombinedOutput(); err != nil {
		t.Fatalf("post-reprep call: %v: %s", err, out)
	}
	sc2, err := ScoreAgentRunAnchored(bin, dir, nonce1)
	if err != nil {
		t.Fatal(err)
	}
	if !sc2.Disqualified || !strings.Contains(sc2.DisqualifyReason, "anchor") {
		t.Fatalf("re-prep not caught by the anchor:\n%s", AgentReport(sc2))
	}
}

// TestSchemaMeteringIsRealAndInert pins both halves of the tools/list metering
// (B-01KYS711ZFFG0), and each half is a bug this function actually had.
//
// The first: the metering must return a real measurement, not 0. Closing stdin
// before reading ends the session before the server writes anything, so the
// first version silently reported 0 and the schema line vanished from the report
// entirely — a metric that is absent looks exactly like a metric that is zero.
//
// The second half of this test is deliberately WEAKER than it first looks, and
// says so rather than implying otherwise. Serving against the FIXTURE dir
// scaffolds records into it and moved the per-call total 3039B -> 3081B, so the
// act of measuring changed the number measured. But comparing two Run calls
// cannot catch that: each Run builds its own fresh fixture, so the perturbation
// happens identically in both and the totals still agree — verified by mutation,
// which this test does NOT kill. What actually prevents it is the signature:
// schemaBytes takes no workspace root, so it has nothing to perturb, and
// reintroducing the bug requires changing the signature in a way a reader sees.
// That assertion is GONE, and its removal is the second lesson here. It claimed
// the scripted total does not wander between runs. It does: each Run mints fresh
// record IDs, and a same-millisecond pair can need a longer disambiguating
// prefix (see internal/ids), which shifts rendered byte totals. It passed ~30
// local runs and failed on a loaded CI runner at 4162B vs 4168B, blocking an
// unrelated archive. So it asserted a property the program does not have, in
// service of a guarantee it could not check either way.
//
// What remains below asserts what IS stable: the schema and manifest sizes.
// Neither carries a record ID — schemaBytes uses its own throwaway root, and
// manifest() is a compile-time const plus a defect-report paragraph whose URL
// comes from the MODULE PATH (debug.ReadBuildInfo's bi.Main.Path). It never
// reads bi.Main.Version, so the size does not move with how the binary was
// built: measured 4299B under plain `go build`, under -buildvcs=false, and
// under -ldflags injecting a fake version.
//
// That measurement is here because two earlier versions of this sentence were
// wrong. "Static text" was imprecise (ReadBuildInfo is a runtime call), and the
// correction was worse — it claimed manifest bytes swing with the version and
// cited BENCH-001. They do not, and BENCH-001's incident is the VERSION LINE in
// the render, a different metric. A precise-sounding sentence that invents a
// mechanism is harder to catch than a vague one, so this states what was
// measured rather than what sounds right.
//
// Mutation-verified: making SchemaBytes vary between calls fails it.
func TestSchemaMeteringIsRealAndInert(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary")
	}
	bin := t.TempDir() + "/spx"
	cmd := exec.Command("go", "build", "-o", bin, "../..")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v: %s", err, out)
	}

	// Half one: a real measurement. The floor is deliberately loose — this
	// asserts "metered at all", not a byte count that every schema edit would
	// have to chase. tools/list carries a dozen tools with descriptions and
	// input schemas, so anything under 4KB means the read failed.
	got := schemaBytes(bin)
	if got < 4096 {
		t.Errorf("schemaBytes = %d; the tools/list read failed and the metric is silently absent", got)
	}
	// It must also be the largest per-session cost, which is the whole reason
	// this field exists: the manifest line presented itself as THE session cost
	// while being a fraction of it.
	man, err := exec.Command(bin, "manifest").Output()
	if err != nil {
		t.Fatal(err)
	}
	// A floor RELATIVE to the manifest, not merely above it. The looser check
	// let a verifier's mutation through: metering the initialize response
	// instead of tools/list reports 4598B, which still clears a bare
	// "> manifest" test because initialize is mostly the manifest text plus an
	// envelope. Schema is measured at ~4.7x the manifest, so 2x is a wide
	// margin that still refuses the wrong response outright.
	if got <= 2*len(man) {
		t.Errorf("schema %dB is not comfortably above 2x manifest %dB — this is what metering the WRONG response line looks like (initialize is mostly manifest text)", got, len(man))
	}

	// Half two: Run carries the measurement at all. NOT a reproducibility check
	// — the per-call total is legitimately NOT byte-stable across runs, because
	// each Run mints fresh record IDs and the adaptive shortener picks a prefix
	// width from what is unambiguous in that workspace, so two runs can render
	// IDs of different widths. An earlier version of this test asserted equality
	// and passed locally, then failed on a loaded CI runner at 4162B vs 4168B.
	// It was asserting a property the program does not have.
	//
	// It also never pinned what it claimed: a verifier showed by mutation that
	// comparing two Runs cannot detect the metering perturbing its fixture,
	// since each Run builds its own. The real protection is the signature —
	// schemaBytes takes no workspace root, so it has nothing to perturb. The
	// schema and manifest figures ARE stable, because they carry no record IDs.
	a, err := Run(bin)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Run(bin)
	if err != nil {
		t.Fatal(err)
	}
	if a.SchemaBytes != b.SchemaBytes || a.ManifestBytes != b.ManifestBytes {
		t.Errorf("session measurements are not stable: schema %d/%d, manifest %d/%d — these carry no record IDs and must not wander",
			a.SchemaBytes, b.SchemaBytes, a.ManifestBytes, b.ManifestBytes)
	}
	if a.SchemaBytes == 0 || b.SchemaBytes == 0 {
		t.Errorf("Run did not carry the schema measurement: %d, %d", a.SchemaBytes, b.SchemaBytes)
	}
}
