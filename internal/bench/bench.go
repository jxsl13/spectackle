// Package bench measures what the MCP surface costs and whether it works: it
// drives a complete lifecycle over a generated fixture workspace through the
// REAL tool surface and meters every result byte, then verifies the workspace
// actually reached the states the workflow promises.
//
// It exists because token cost and outcome quality pull against each other,
// and both sides of that trade have bitten this repository in the same week —
// texts trimmed for the output diet hid real ambiguity, and texts grown for
// clarity taxed every call of every session. Variants must therefore be
// MEASURED against each other, never argued: the A/B mode runs one script
// over one fixture against two server binaries and reports the deltas.
//
// Validity outranks tokens by construction. A run whose workspace ends in the
// wrong state, or whose check does not end ok, is invalid, and an invalid run
// never counts as a win regardless of how few tokens it spent.
package bench

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Step is one scripted tool call. Refusals are part of the surface under
// measurement — their grammar is read by the LLM exactly like success records
// — so steps that must refuse say so, and a refusal where success was
// expected (or the reverse) fails validity.
type Step struct {
	Name string // tool name
	Args string // JSON arguments; {ID}, {ID2}, {BID}, {ADR} expand to captured IDs
	// Capture stores the first item ID of the result under the given key.
	Capture string
	// ExpectRefusal marks a step that must exit non-zero with a "! ... E"
	// record. Optional marks a step whose outcome is informational only
	// (e.g. prefix-ambiguity probes, which depend on mint timing).
	ExpectRefusal bool
	Optional      bool
	// Label names the step in the per-step report.
	Label string
}

// Result is the metered outcome of one run.
type Result struct {
	Steps      []StepResult
	Bytes      int
	Tokens     int // Bytes/4 — a stated estimate, not a tokenizer
	Valid      bool
	Violations []string // why Valid is false, or informational notes
	Coverage   map[string]bool
}

// StepResult is one call's metering.
type StepResult struct {
	Label   string
	Bytes   int
	Refused bool
}

// coverageStates are the eight lifecycle states every full run must touch.
var coverageStates = []string{
	"draft", "submitted", "approved", "active", "done", "archived", "rejected", "blocked",
}

// recordFamilies are the promised output families whose ABSENCE from a full
// run means the surface went silent somewhere it must not (ADR-01KYDG).
var recordFamilies = map[string]string{
	"item record":       "\ni ",
	"gitflow branch":    "g branch ",
	"pr draft":          " draft offline://",
	"pr ready":          " ready offline://",
	"pr merged":         " merged ",
	"records commit":    "g records committed",
	"refusal":           "! ARG E",
	"rounds escalation": "! ROUNDS E",
}

var reItemID = regexp.MustCompile(`\bi ((?:ADR|[PTBRD])-[0-9A-HJKMNP-TV-Z]+)`)
var reDecideID = regexp.MustCompile(`decide ((?:ADR|[PTBRD])-[0-9A-HJKMNP-TV-Z]+)`)

// Fixture generates a workspace that exercises every state and the tricky
// shapes: offline git mode (the whole gitflow runs with no network),
// max_rounds=2 (so one reopen succeeds and the second escalates to blocked),
// and a small code tree so the graph and gitflow have something real.
func Fixture(dir string) error {
	for _, cmd := range [][]string{
		{"git", "-C", dir, "init", "-q", "-b", "main"},
		{"git", "-C", dir, "-c", "user.name=bench", "-c", "user.email=bench@localhost", "commit", "-q", "--allow-empty", "-m", "init"},
	} {
		if out, err := exec.Command(cmd[0], cmd[1:]...).CombinedOutput(); err != nil {
			return fmt.Errorf("bench fixture: %v: %s", err, out)
		}
	}
	files := map[string]string{
		"go.mod":                  "module example.com/benchfix\n\ngo 1.26\n",
		"main.go":                 "package main\n\nfunc main() {}\n",
		".spectackle/config.yaml": "schema: v1\ngit:\n  mode: offline\n  base: main\nfeedback:\n  max_rounds: 2\n",
	}
	for rel, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			return err
		}
	}
	if out, err := exec.Command("git", "-C", dir, "add", "-A").CombinedOutput(); err != nil {
		return fmt.Errorf("bench fixture: add: %v: %s", err, out)
	}
	out, err := exec.Command("git", "-C", dir, "-c", "user.name=bench", "-c", "user.email=bench@localhost", "commit", "-q", "-m", "fixture").CombinedOutput()
	if err != nil {
		return fmt.Errorf("bench fixture: commit: %v: %s", err, out)
	}
	return nil
}

// Script is the full-lifecycle walk: every state, a rejection with
// revocation, a reopen, a blocked escalation resolved by decide, the refusal
// grammar, and the read surface (state, get, grill, find, check) whose tokens
// are the recurring cost of every real session.
func Script() []Step {
	return []Step{
		{Label: "state/initial", Name: "state", Args: `{}`},
		// a real contract over the fixture code: check must end ok because the
		// workspace IS clean, not because validity looked away — and the rule
		// tool's slot grammar joins the metered surface.
		{Label: "rule/add", Name: "rule", Args: `{"op":"add","dir":"","stem":"FIX-API","pattern":"U","system":"fixture binary","response":"terminate with exit code 0"}`},
		{Label: "draft/T1", Name: "draft", Args: `{"kind":"task","title":"alpha lifecycle subject"}`, Capture: "ID"},
		{Label: "draft/T2", Name: "draft", Args: `{"kind":"task","title":"beta reopen subject"}`, Capture: "ID2"},
		{Label: "draft/B1", Name: "draft", Args: `{"kind":"bug","title":"gamma rejection subject"}`, Capture: "BID"},
		{Label: "refuse/unknown-kind", Name: "draft", Args: `{"kind":"epic","title":"x"}`, ExpectRefusal: true},
		{Label: "refuse/noteless-reject", Name: "move", Args: `{"id":"{BID}","to":"rejected"}`, ExpectRefusal: true},
		{Label: "reject/B1", Name: "move", Args: `{"id":"{BID}","to":"rejected","note":"bench rejection corpus entry"}`},
		{Label: "revoke/B1", Name: "move", Args: `{"id":"{BID}","to":"draft"}`},
		{Label: "move/T1-submitted", Name: "move", Args: `{"id":"{ID}","to":"submitted"}`},
		{Label: "move/T1-approved", Name: "move", Args: `{"id":"{ID}","to":"approved"}`},
		{Label: "move/T1-active", Name: "move", Args: `{"id":"{ID}","to":"active"}`},
		{Label: "grill/T1", Name: "grill", Args: `{"id":"{ID}"}`},
		{Label: "move/T1-done", Name: "move", Args: `{"id":"{ID}","to":"done"}`},
		{Label: "move/T1-archived", Name: "move", Args: `{"id":"{ID}","to":"archived","note":"alpha shipped"}`},
		{Label: "get/T1-tombstone", Name: "get", Args: `{"id":"{ID}"}`},
		{Label: "move/T2-active", Name: "move", Args: `{"id":"{ID2}","to":"active"}`},
		{Label: "move/T2-done", Name: "move", Args: `{"id":"{ID2}","to":"done"}`},
		{Label: "reopen/T2", Name: "move", Args: `{"id":"{ID2}","to":"active"}`},
		{Label: "move/T2-done-again", Name: "move", Args: `{"id":"{ID2}","to":"done"}`},
		// the second reopen exhausts max_rounds=2: blocked + ADR minted. The
		// transition SUCCEEDS (the item lands in blocked, a new state), so the
		// exit is zero; the ! ROUNDS E record rides a success result.
		{Label: "escalate/T2", Name: "move", Args: `{"id":"{ID2}","to":"active"}`, Capture: "ADR"},
		{Label: "resolve/rescope", Name: "decide", Args: `{"op":"answer","id":"{ADR}","choose":"rescope"}`},
		{Label: "find/rejections", Name: "find", Args: `{"q":"bench rejection","scope":"rejection"}`},
		{Label: "state/final", Name: "state", Args: `{}`},
		{Label: "check/final", Name: "check", Args: `{}`},
	}
}

// Run drives the script over a fresh fixture with the given server binary.
func Run(bin string) (Result, error) {
	dir, err := os.MkdirTemp("", "spectackle-bench-*")
	if err != nil {
		return Result{}, err
	}
	defer os.RemoveAll(dir)
	if err := Fixture(dir); err != nil {
		return Result{}, err
	}

	res := Result{Coverage: map[string]bool{}}
	captured := map[string]string{}
	var all strings.Builder

	for _, st := range Script() {
		args := st.Args
		for k, v := range captured {
			args = strings.ReplaceAll(args, "{"+k+"}", v)
		}
		s, refused, err := callOnce(bin, dir, st.Name, args)
		if err != nil {
			return res, fmt.Errorf("bench: %s: %w", st.Label, err)
		}
		// A captured display-form ID can turn ambiguous the moment sibling
		// records are minted — the intrinsic instability of shortest-unique
		// prefixes, and the FIRST thing this harness caught live (its own
		// driver hit it on run one). A real agent must recover exactly like
		// this: read the candidates off the refusal, pick the one extending
		// the stale prefix, update its notion of the ID, retry once. The
		// retry's bytes are metered like everything else, so the ambiguity's
		// token cost is part of the surface's score rather than hidden.
		if refused && strings.Contains(s, "ambiguous prefix") {
			if fixedArgs, fixed := disambiguate(args, s, captured); fixed {
				res.Violations = append(res.Violations, "note: ambiguity retry at "+st.Label)
				res.Bytes += len(s)
				s2, refused2, err2 := callOnce(bin, dir, st.Name, fixedArgs)
				if err2 != nil {
					return res, fmt.Errorf("bench: %s retry: %w", st.Label, err2)
				}
				s, refused = s2, refused2
			}
		}
		all.WriteString(s)
		res.Steps = append(res.Steps, StepResult{Label: st.Label, Bytes: len(s), Refused: refused})
		res.Bytes += len(s)

		if st.Capture != "" {
			if st.Capture == "ADR" {
				if m := reDecideID.FindStringSubmatch(s); m != nil {
					captured[st.Capture] = m[1]
				}
			} else if m := reItemID.FindStringSubmatch(s); m != nil {
				captured[st.Capture] = m[1]
			}
			if captured[st.Capture] == "" {
				res.Violations = append(res.Violations, "capture "+st.Capture+" failed at "+st.Label)
			}
		}
		if !st.Optional && refused != st.ExpectRefusal {
			res.Violations = append(res.Violations,
				fmt.Sprintf("%s: refused=%v want %v", st.Label, refused, st.ExpectRefusal))
		}
		for _, want := range coverageStates {
			if strings.Contains(s, " "+want+" ") || strings.Contains(s, " "+want+"\n") {
				res.Coverage[want] = true
			}
		}
	}

	full := all.String()
	for family, marker := range recordFamilies {
		if !strings.Contains(full, marker) {
			res.Violations = append(res.Violations, "record family missing: "+family)
		}
	}
	for _, want := range coverageStates {
		if !res.Coverage[want] {
			res.Violations = append(res.Violations, "state never observed: "+want)
		}
	}
	// the final check must end ok
	if !strings.HasSuffix(strings.TrimSpace(full), "ok") {
		res.Violations = append(res.Violations, "final check did not end ok")
	}

	res.Tokens = res.Bytes / 4
	res.Valid = true
	for _, v := range res.Violations {
		if !strings.HasPrefix(v, "note:") {
			res.Valid = false
			break
		}
	}
	return res, nil
}

// callOnce runs one tool call, returning stdout, whether it refused, and any
// transport-level error. Stderr is diagnostics, not surface, and is dropped.
func callOnce(bin, dir, name, args string) (string, bool, error) {
	cmd := exec.Command(bin, "call", "-root", dir, name, args)
	cmd.Env = append(os.Environ(), "SPECTACKLE_AGENT=bench")
	out, runErr := cmd.Output()
	if ee, ok := runErr.(*exec.ExitError); ok {
		return string(out), ee.ExitCode() != 0, nil
	}
	if runErr != nil {
		return "", false, runErr
	}
	return string(out), false, nil
}

var reAmbCandidates = regexp.MustCompile(`records: (.+)$`)

// disambiguate rewrites args by replacing the stale prefix with the refusal's
// candidate that extends it, and updates the capture table so later steps use
// the longer form too.
func disambiguate(args, refusal string, captured map[string]string) (string, bool) {
	m := reAmbCandidates.FindStringSubmatch(strings.TrimSpace(refusal))
	if m == nil {
		return args, false
	}
	for _, cand := range strings.Fields(m[1]) {
		for key, val := range captured {
			if val != "" && strings.HasPrefix(cand, val) && strings.Contains(args, val) {
				captured[key] = cand
				return strings.ReplaceAll(args, val, cand), true
			}
		}
	}
	return args, false
}

// Report renders one run as a dense text block, largest steps first.
func Report(r Result) string {
	var b strings.Builder
	steps := append([]StepResult(nil), r.Steps...)
	sort.Slice(steps, func(i, j int) bool { return steps[i].Bytes > steps[j].Bytes })
	for _, s := range steps {
		fmt.Fprintf(&b, "bench %6dB %s\n", s.Bytes, s.Label)
	}
	fmt.Fprintf(&b, "bench total %dB ~%d tokens valid=%v\n", r.Bytes, r.Tokens, r.Valid)
	for _, v := range r.Violations {
		fmt.Fprintf(&b, "bench ! %s\n", v)
	}
	return b.String()
}

// AB runs the script against two binaries and reports the deltas. Validity
// gates the comparison: a candidate that saves tokens and loses validity has
// lost, and the report says so in one line.
func AB(baseline, candidate string) (string, error) {
	a, err := Run(baseline)
	if err != nil {
		return "", err
	}
	b, err := Run(candidate)
	if err != nil {
		return "", err
	}
	var out strings.Builder
	out.WriteString("== baseline ==\n" + Report(a))
	out.WriteString("== candidate ==\n" + Report(b))
	fmt.Fprintf(&out, "bench delta %+dB ~%+d tokens\n", b.Bytes-a.Bytes, b.Tokens-a.Tokens)
	switch {
	case a.Valid && !b.Valid:
		out.WriteString("bench verdict: CANDIDATE LOSES — validity regressed, tokens irrelevant\n")
	case !a.Valid && b.Valid:
		out.WriteString("bench verdict: candidate wins — validity restored\n")
	case b.Bytes < a.Bytes:
		out.WriteString("bench verdict: candidate wins — fewer tokens at equal validity\n")
	case b.Bytes == a.Bytes:
		out.WriteString("bench verdict: tie\n")
	default:
		out.WriteString("bench verdict: candidate loses — more tokens at equal validity\n")
	}
	return out.String(), nil
}
