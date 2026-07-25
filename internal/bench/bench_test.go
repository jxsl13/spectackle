package bench

import (
	"os"
	"os/exec"
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
	// 7 seeded rules across 4 dirs, zero findings. The summary line is the
	// collapse's rendering of a healthy inventory; per-dir listing lines
	// must be absent because nothing is dirty.
	if !strings.Contains(out, "ok rules total=7 dirs=4 findings=0") {
		t.Fatalf("seeded inventory summary missing or wrong (seeds not clean, or seed count drifted):\n%s", out)
	}
	if strings.Contains(out, "ok dir ") {
		t.Fatalf("healthy seeded dirs must collapse into the summary, not list:\n%s", out)
	}
}
