package bench

import (
	"os"
	"os/exec"
	"path/filepath"
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
	cmd := exec.Command("go", "build", "-o", bin, "../../cmd/spectackle")
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
	cmd := exec.Command("go", "build", "-o", bin, "../../cmd/spectackle")
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
	// a fresh seeded workspace answers check with plain ok, no gap line for
	// a diligent agent to rabbit-hole into.
	checkOut, refused, err := callOnce(bin, dir, "check", "{}")
	if err != nil || refused {
		t.Fatalf("check on seeded fixture: refused=%v err=%v\n%s", refused, err, checkOut)
	}
	if strings.TrimSpace(checkOut) != "ok" {
		t.Fatalf("fresh seeded fixture check must be plain ok, got:\n%s", checkOut)
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
	cmd := exec.Command("go", "build", "-o", bin, "../../cmd/spectackle")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v: %s", err, out)
	}

	dir := t.TempDir()
	brief, shim, err := AgentPrep(bin, dir)
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
	if _, _, err := AgentPrep(bin, dir2); err != nil {
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
