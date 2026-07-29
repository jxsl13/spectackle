package mcpserver

// Package-local contract coverage (T-01KYD87ZN): COVERED(pkg) holds iff a
// non-root bundle sits at the dir or an ancestor below root, or a root rule
// with a non-empty applies binds an anchored node in the dir's subtree.
// Default visibility lives in state (not string-matched by CI); check COUNTS
// uncovered dirs as findings only under coverage_gate: package, because
// check's single ok path is full-string-compared by the CI self-hosting gate
// and any unconditional output would turn it red.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeCoverageFixture scaffolds a root with two source packages:
// internal/alphapkg (func Alpha) and internal/betapkg (func Beta).
func writeCoverageFixture(t *testing.T, root string, gate string) {
	t.Helper()
	cfg := "schema: v1\n"
	if gate != "" {
		cfg += "coverage_gate: " + gate + "\n"
	}
	if err := os.MkdirAll(filepath.Join(root, ".spectackle"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".spectackle", "config.yaml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	for pkg, fn := range map[string]string{"alphapkg": "Alpha", "betapkg": "Beta"} {
		dir := filepath.Join(root, "internal", pkg)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		src := "package " + pkg + "\n\nfunc " + fn + "() int {\n\treturn 1\n}\n"
		if err := os.WriteFile(filepath.Join(dir, pkg+".go"), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// Default (no coverage_gate): check emits nothing for coverage — removing
// the key restores byte-identical output, proving the key alone toggles it.
func TestCoverageCheckSilentByDefault(t *testing.T) {
	root := t.TempDir()
	writeCoverageFixture(t, root, "")
	sess := connectRoot(t, root)
	before := callText(t, sess, "check", map[string]any{})
	if strings.Contains(before, "nocontract") {
		t.Fatalf("ungated check must not report coverage: %q", before)
	}
	// a clean tree reports zero findings — with its counts, so the answer is
	// distinguishable from a no-op stub (R-01KYQ4XNAFFNY)
	if !strings.HasPrefix(before, "ok check 0 findings (E=0 W=0)") {
		t.Fatalf("ungated check on a clean tree must report zero findings: %q", before)
	}

	gatedRoot := t.TempDir()
	writeCoverageFixture(t, gatedRoot, "package")
	gated := connectRoot(t, gatedRoot)
	out := callText(t, gated, "check", map[string]any{})
	if !strings.Contains(out, "g nocontract internal/alphapkg") ||
		!strings.Contains(out, "g nocontract internal/betapkg") {
		t.Fatalf("gated check must count both uncovered packages: %q", out)
	}
	if out == "ok" || strings.HasSuffix(out, "\nok") {
		t.Fatalf("gated findings must not end ok: %q", out)
	}
}

// state marks exactly the uncovered dirs; one applies-bound root rule into
// alphapkg removes exactly alphapkg's token, and an empty-applies root rule
// silences nothing (the lazy-sentence mitigation).
func TestCoverageStateVisibility(t *testing.T) {
	root := t.TempDir()
	writeCoverageFixture(t, root, "")
	sess := connectRoot(t, root)

	out := callText(t, sess, "state", map[string]any{})
	for _, d := range []string{"internal/alphapkg", "internal/betapkg"} {
		if !strings.Contains(out, "ok dir "+d+" rules=0 uncovered") {
			t.Fatalf("state must mark %s uncovered: %q", d, out)
		}
	}

	// An applies-bound root rule into alphapkg covers alphapkg only.
	ruleOut := callText(t, sess, "rule", map[string]any{
		"op": "add", "dir": "", "pattern": "U", "stem": "COV-PRB",
		"system":   "the coverage probe workspace",
		"response": "keep Alpha returning a stable constant across refactors",
		"applies":  []string{"go:alphapkg.Alpha"},
	})
	if !strings.Contains(ruleOut, "ok COV-PRB-001") {
		t.Fatalf("rule add: %q", ruleOut)
	}
	out = callText(t, sess, "state", map[string]any{})
	if strings.Contains(out, "internal/alphapkg rules=0 uncovered") {
		t.Fatalf("applies-bound rule must cover alphapkg: %q", out)
	}
	if !strings.Contains(out, "ok dir internal/betapkg rules=0 uncovered") {
		t.Fatalf("betapkg must stay uncovered — a root rule elsewhere silences nothing: %q", out)
	}
}

// A nested bundle covers its dir and everything below it.
func TestCoverageNestedBundleCovers(t *testing.T) {
	root := t.TempDir()
	writeCoverageFixture(t, root, "")
	sess := connectRoot(t, root)
	ruleOut := callText(t, sess, "rule", map[string]any{
		"op": "add", "dir": "internal/betapkg", "pattern": "U", "stem": "COV-NST",
		"system":   "the betapkg package",
		"response": "keep Beta free of side effects in every call path",
	})
	if !strings.Contains(ruleOut, "ok COV-NST-001") {
		t.Fatalf("rule add: %q", ruleOut)
	}
	out := callText(t, sess, "state", map[string]any{})
	if strings.Contains(out, "internal/betapkg rules=0 uncovered") {
		t.Fatalf("nested bundle must cover its dir: %q", out)
	}
	if !strings.Contains(out, "ok dir internal/alphapkg rules=0 uncovered") {
		t.Fatalf("alphapkg must stay uncovered: %q", out)
	}
}

// Cap holds: 40 uncovered dirs -> 20 records + "+20 more" tail.
func TestCoverageGateCap(t *testing.T) {
	root := t.TempDir()
	writeCoverageFixture(t, root, "package")
	for i := 0; i < 38; i++ { // fixture already has 2
		pkg := fmt.Sprintf("cap%02d", i)
		dir := filepath.Join(root, "internal", pkg)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		src := "package " + pkg + "\n\nfunc F() int { return 1 }\n"
		if err := os.WriteFile(filepath.Join(dir, pkg+".go"), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	sess := connectRoot(t, root)
	out := callText(t, sess, "check", map[string]any{"budget": 20000})
	if n := strings.Count(out, "g nocontract "); n != 21 { // 20 dirs + tail record
		t.Fatalf("cap: want 20 records + tail, got %d nocontract lines: %q", n, out)
	}
	if !strings.Contains(out, "g nocontract +20 more") {
		t.Fatalf("missing overflow tail: %q", out)
	}
}

// A config carrying keys this build does not know still loads — the same
// YAML tolerance that lets an older server ignore coverage_gate.
func TestCoverageUnknownKeyTolerance(t *testing.T) {
	root := t.TempDir()
	writeCoverageFixture(t, root, "")
	p := filepath.Join(root, ".spectackle", "config.yaml")
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, append(raw, []byte("future_unknown_key: x\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	sess := connectRoot(t, root)
	if out := callText(t, sess, "check", map[string]any{}); !strings.Contains(out, "0 findings (E=0 W=0)") {
		t.Fatalf("unknown config keys must be ignored at load (check still clean): %q", out)
	}
}
