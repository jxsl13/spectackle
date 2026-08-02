// Package bench measures what the MCP surface costs and whether it works: it
// drives a complete lifecycle over a generated fixture workspace through the
// REAL tool surface and meters every result byte, then verifies the workspace
// actually reached the states the workflow promises.
//
// The agent-judge reference curves and the operating discipline (n=3
// batches, the all-valid gate, nonce anchors, DISQUALIFIED vs INVALID)
// live in docs/bench-curves.md — read it before comparing any judge batch
// against history.
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
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
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
	// ManifestBytes is the initialize-handshake manifest's size, metered
	// SEPARATELY from the per-call total: its cost multiplier is sessions,
	// not calls, and folding it into Bytes would corrupt both figures
	// (T-01KYE3 — 7238 bytes stayed invisible through eleven benchmarked
	// landings for lack of this line).
	ManifestBytes int

	// SchemaBytes is the tools/list payload's size: every tool's description
	// and input schema, as the wire delivers it at connect time.
	//
	// It is metered because it was the largest thing nobody was counting. The
	// manifest line presented itself as THE once-per-session cost at 4.3KB,
	// while a real handshake writes 24623B — tools/list is 81% of it, measured,
	// and the initialize result is ~95% manifest text, so schema+manifest tracks
	// the real wire to within 1.2% rather than double-counting. So every
	// byte-economy conclusion drawn from this benchmark was reasoning about a
	// denominator missing three quarters of what a session actually pays, and
	// BENCH-001 ("revert a surface change whose judged metric did not improve")
	// is only as trustworthy as the metric it defers to (B-01KYS711ZFFG0).
	//
	// Measured over the wire rather than by marshalling the tool structs, so it
	// cannot drift from what a client receives.
	SchemaBytes int
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
	"item record": "\ni ",
	// "g records " matches both "committed" and "clean": under the
	// edge-commit engine (T-01KYD94MG) the structured edge commit lands
	// first and the transition sweep finds the records clean — either way
	// the family spoke, which is what never-silent asserts.
	"records commit":    "g records ",
	"refusal":           "! ARG E",
	"rounds escalation": "! ROUNDS E",
}

// closureFamilies is the transition surface set (T-01KYHAH1GJ): the bench
// fixture runs mode: offline, whose closure surface is commit-only — the
// old PR-theater families (branch/draft/ready/merged) died with the
// collapse. Kept as a one-entry any-of table so an online-fixture bench
// variant can extend it without reshaping the scorer.
var closureFamilies = map[string][]string{
	"gitflow closure": {"g offline commit ", "g records "},
}

var reItemID = regexp.MustCompile(`\bi ((?:ADR|[PTBRD])-[0-9A-HJKMNP-TV-Z]+)`)

// matches the decision ID whether the refusal names it bare (`decide ADR-x`)
// or inside the callable object the rounds refusal now hands back
// (`decide {"op":"answer","id":"ADR-x",...}`).
var reDecideID = regexp.MustCompile(`decide (?:\{"op":"answer","id":")?((?:ADR|[PTBRD])-[0-9A-HJKMNP-TV-Z]+)`)

// Fixture generates a workspace that exercises every state and the tricky
// shapes: offline git mode (the whole gitflow runs with no network),
// max_rounds=2 (so one reopen succeeds and the second escalates to blocked),
// and a small code tree so the graph and gitflow have something real.
func Fixture(dir string) error {
	for _, cmd := range [][]string{
		{"git", "-C", dir, "init", "-q", "-b", "main"},
		// Repo-local identity: the server inherits git config (B-01KYDK), so
		// the fixture declares its own — measurements must not depend on the
		// host being configured, nor pay the fallback line's bytes on bare
		// CI runners while a configured laptop measures without them.
		{"git", "-C", dir, "config", "user.name", "bench"},
		{"git", "-C", dir, "config", "user.email", "bench@localhost"},
		{"git", "-C", dir, "commit", "-q", "--allow-empty", "-m", "init"},
	} {
		if out, err := exec.Command(cmd[0], cmd[1:]...).CombinedOutput(); err != nil {
			return fmt.Errorf("bench fixture: %v: %s", err, out)
		}
	}
	// Fixture v2 (T-01KYDT): a realistic directory topology, not a single
	// file. v1's one-dir workspace underestimated whole change classes — the
	// state rules-inventory collapse measured -17B here vs -702B on the real
	// repository, a 40x gap that made the harness unable to rank exactly the
	// changes it exists to rank. Each dir below also receives seeded rules
	// (see Seed) so per-dir surfaces have real breadth.
	files := map[string]string{
		"go.mod":                  "module example.com/benchfix\n\ngo 1.26\n",
		"main.go":                 "package main\n\nfunc main() {}\n",
		"api/api.go":              "package api\n\n// Serve is the fixture's api entry point.\nfunc Serve() int { return 0 }\n",
		"api/handlers/echo.go":    "package handlers\n\n// Echo returns its input.\nfunc Echo(s string) string { return s }\n",
		"store/store.go":          "package store\n\n// Get returns the stored value.\nfunc Get(k string) string { return k }\n",
		"cli/cli.go":              "package cli\n\n// Parse parses fixture arguments.\nfunc Parse(args []string) int { return len(args) }\n",
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
	if err != nil && !strings.Contains(string(out), "nothing to commit") {
		return fmt.Errorf("bench fixture: commit: %v: %s", err, out)
	}
	return nil
}

// seedRules are the ambient per-dir contracts Seed installs before metering
// starts. They go in through the real tool surface — the server stays the
// sole author of record files, so a grammar change can never desynchronize a
// hand-written fixture — and they are deliberately lint-clean: a finding
// would put its dir on every state call's listing and mask the very
// healthy-inventory collapse class the enriched fixture exists to measure.
// Every response clause names a number the W001 lint accepts as verifiable —
// a finding-tripping seed would defeat the enrichment (see the doc above).
// The root entry exists so a fresh workspace answers check with plain ok
// (B-01KYE0RCK): without it the root main.go is uncovered, check leads with
// the gap line, and a judge agent whose brief promises check-reports-ok
// rabbit-holes into rule-writing to clear a gap the brief never mentioned —
// the scripted bench adds the same root rule in its own first step for the
// same reason.
var seedRules = []struct{ dir, stem, response string }{
	{"", "FIX-ROOT", "terminate with exit code 0"},
	{"api", "API-STATUS", "terminate with exit code 0"},
	{"api", "API-EMPTY", "return HTTP status 204"},
	{"api/handlers", "HND-ECHO", "return the input with HTTP status 200"},
	{"store", "STO-HIT", "return the value for the key within 50 milliseconds"},
	{"store", "STO-MISS", "return error code 404"},
	{"cli", "CLI-COUNT", "terminate with exit code 2"},
	{"cli", "CLI-ZERO", "terminate with exit code 0"},
}

// Seed installs the ambient rules over the fixture, unmetered: this is the
// workspace's pre-existing history, not part of the scripted session under
// measurement. A seed refusal is a hard error — a fixture that failed to
// take shape would silently bench a thinner workspace than reported.
func Seed(bin, dir string) error {
	for _, r := range seedRules {
		label := r.dir
		if label == "" {
			label = "root"
		}
		args := fmt.Sprintf(
			`{"op":"add","dir":%q,"stem":%q,"pattern":"U","system":"fixture %s","response":%q}`,
			r.dir, r.stem, label, r.response)
		out, refused, err := callOnce(bin, dir, "rule", args)
		if err != nil {
			return fmt.Errorf("bench seed %s/%s: %w", r.dir, r.stem, err)
		}
		if refused {
			return fmt.Errorf("bench seed %s/%s refused: %s", r.dir, r.stem, out)
		}
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
		// requested transition is REFUSED — the item is forced to blocked,
		// which is not what the caller asked for — so the exit is non-zero.
		// It used to ride a success result, and judges read that as
		// moved-plus-warning (R-01KYQ4XNAFFNY).
		{Label: "escalate/T2", Name: "move", Args: `{"id":"{ID2}","to":"active"}`, Capture: "ADR", ExpectRefusal: true},
		{Label: "resolve/rescope", Name: "decide", Args: `{"op":"answer","id":"{ADR}","choose":"rescope"}`},
		{Label: "find/rejections", Name: "find", Args: `{"q":"bench rejection","scope":"rejection"}`},
		{Label: "state/final", Name: "state", Args: `{}`},
		{Label: "check/final", Name: "check", Args: `{}`},
	}
}

// Run drives the script over a fresh fixture with the given server binary.
// toolsListPayload meters the tools/list response the way a client receives it:
// spawn the server on stdio, complete the handshake, ask for the tool list, and
// count the bytes of that one response. Marshalling the registered mcp.Tool
// structs in-process would be simpler and would drift — the wire adds the
// envelope, and a change to how schemas are emitted would not show up.
//
// Returns the RAW response line alongside its length, because the agent-prep
// -with-schema mode (T-01KYSPFXHNFZ7) must inject exactly the bytes it reports:
// rendering the tools some other way would make the injected surface and the
// metered size two different things, and the whole point of the mode is that
// they are one thing.
//
// Returns ("", 0) when anything goes wrong, and 0 means "not metered" everywhere
// it is read. That distinction matters: an unmeterable side must not be silently
// treated as zero bytes, which is the mistake the manifest delta already guards
// against by refusing to subtract across a measured and an unmeasured side.
func toolsListPayload(bin string) (string, int) {
	// A DEDICATED throwaway root, never the fixture. Serving against the
	// fixture scaffolds records into it, and the first version of this function
	// did exactly that: the per-call total moved 3039B -> 3081B purely because
	// the metering had touched the workspace it was measuring. A probe that
	// perturbs its own subject is worse than no probe.
	root, err := os.MkdirTemp("", "spx-schema-*")
	if err != nil {
		return "", 0
	}
	defer os.RemoveAll(root)
	cmd := exec.Command(bin, "serve", "-root", root)
	stdin, perr := cmd.StdinPipe()
	if perr != nil {
		return "", 0
	}
	stdout, perr := cmd.StdoutPipe()
	if perr != nil {
		return "", 0
	}
	if err := cmd.Start(); err != nil {
		return "", 0
	}
	defer func() {
		_ = stdin.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()
	// One line per JSON-RPC message, exactly as the smoke gate drives it.
	for _, msg := range []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"bench","version":"0"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
	} {
		if _, err := io.WriteString(stdin, msg+"\n"); err != nil {
			return "", 0
		}
	}
	// READ until the response arrives; do not close stdin first. Closing it
	// immediately ends the session before the server has written anything —
	// which is why the smoke gate in the Makefile sleeps before its pipe drains,
	// and why the first version of this function silently metered 0.
	type found struct {
		line string
		n    int
	}
	ch := make(chan found, 1)
	go func() {
		sc := bufio.NewScanner(stdout)
		// Headroom, not a present need: the payload is ~20KB against bufio's
		// 64KB default. It is here so a grown tool surface fails loudly at the
		// 8MB cap rather than silently truncating at 64KB.
		sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
		for sc.Scan() {
			line := sc.Text()
			// Decode the id rather than substring-matching `"id":2`. A tool
			// DESCRIPTION containing that literal would false-match, and more
			// importantly a one-character slip to `"id":1` silently meters the
			// initialize result instead — 4598B against 20023B, a 4.4x
			// understatement reported with full confidence. A verifier proved
			// the magnitude-only assertions here did not catch that.
			var env struct {
				ID     json.Number     `json:"id"`
				Result json.RawMessage `json:"result"`
			}
			if err := json.Unmarshal([]byte(line), &env); err != nil {
				continue
			}
			if env.ID.String() == "2" && len(env.Result) > 0 {
				ch <- found{line, len(line)}
				return
			}
		}
		ch <- found{"", 0}
	}()
	select {
	case f := <-ch:
		return f.line, f.n
	case <-time.After(30 * time.Second):
		return "", 0
	}
}

// schemaBytes is the size-only view of the tools/list payload, which is all
// the A/B reporting side needs. Kept as a wrapper rather than a second probe:
// two spawns would meter two different server processes, and the mode that
// injects the payload must be able to prove its bytes are the metered ones.
func schemaBytes(bin string) int {
	_, n := toolsListPayload(bin)
	return n
}

func Run(bin string) (Result, error) {
	dir, err := os.MkdirTemp("", "spectackle-bench-*")
	if err != nil {
		return Result{}, err
	}
	defer os.RemoveAll(dir)
	if err := Fixture(dir); err != nil {
		return Result{}, err
	}
	if err := Seed(bin, dir); err != nil {
		return Result{}, err
	}

	res := Result{Coverage: map[string]bool{}}
	if out, err := exec.Command(bin, "manifest").Output(); err == nil {
		res.ManifestBytes = len(out)
	}
	res.SchemaBytes = schemaBytes(bin)
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
	res.Violations = append(res.Violations, gitInstructionViolations(full)...)
	for family, marker := range recordFamilies {
		if !strings.Contains(full, marker) {
			res.Violations = append(res.Violations, "record family missing: "+family)
		}
	}
	for family, markers := range closureFamilies {
		hit := false
		for _, m := range markers {
			if strings.Contains(full, m) {
				hit = true
				break
			}
		}
		if !hit {
			res.Violations = append(res.Violations, "record family missing: "+family)
		}
	}
	for _, want := range coverageStates {
		if !res.Coverage[want] {
			res.Violations = append(res.Violations, "state never observed: "+want)
		}
	}
	// The final check verdict follows the server's own severity taxonomy
	// (B-01KYDM): E findings fail validity, W findings are environmental
	// degradations noted but not fatal — a CI runner whose typed-call pass
	// cannot load the fixture module reports TYPED W, and a harness that
	// failed on that would measure the runner, not the surface.
	checkOut := ""
	if n := len(res.Steps); n > 0 {
		checkOut = full[len(full)-res.Steps[n-1].Bytes:]
	}
	switch {
	case strings.TrimSpace(checkOut) == "ok":
	case !strings.Contains(checkOut, " E "):
		res.Violations = append(res.Violations, "note: final check degraded (W findings only)")
	default:
		res.Violations = append(res.Violations, "final check carries E findings")
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

// ROLE-BOUNDARY-001's verifiable artifact: the server performs every
// mechanical git and forge step itself, so no tool result may instruct the
// caller to run git. Two shapes, calibrated 2026-07-26 against a full
// scripted run (3662B transcript, zero hits on the legitimate surface —
// g branch/g pr N merged/g records/h hints/refusals match neither):
// an imperative instruction ("run `git push`") and a bare command line
// ("git commit -m ..."). A hit is a validity failure, never a note.
var reGitImperative = regexp.MustCompile("(?i)\\b(?:run|execute|invoke)\\s+`?git\\b")
var reGitCommandLine = regexp.MustCompile(`(?m)^\s*(?:\$\s*)?git\s+(?:add|commit|push|checkout|switch|merge|rebase|pull|fetch|reset|cherry-pick)\b`)

// gitInstructionViolations scans a transcript (concatenated tool results)
// for text instructing the caller to run git, one violation per match line.
// GitInstructionViolations is the exported form for other packages'
// compliance tests (NODE-EDGE-001: rendered hints and protocols must never
// instruct mechanical git work) — same detector, same calibration.
func GitInstructionViolations(transcript string) []string {
	return gitInstructionViolations(transcript)
}

func gitInstructionViolations(transcript string) []string {
	var out []string
	for _, m := range reGitImperative.FindAllString(transcript, -1) {
		out = append(out, "git instruction to caller (ROLE-BOUNDARY-001): "+m)
	}
	for _, m := range reGitCommandLine.FindAllString(transcript, -1) {
		out = append(out, "git command line in tool result (ROLE-BOUNDARY-001): "+strings.TrimSpace(m))
	}
	return out
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
	// The fixture label marks the workspace generation (v2: multi-dir seeds,
	// T-01KYDT; v3: root coverage added, B-01KYE0RCK): absolute totals are
	// not comparable across generations, and the label is how a reader of
	// two reports knows whether they may compare them at all.
	fmt.Fprintf(&b, "bench total %dB ~%d tokens valid=%v fixture=v3\n", r.Bytes, r.Tokens, r.Valid)
	if r.SchemaBytes > 0 {
		fmt.Fprintf(&b, "bench schema %dB ~%d tokens (tools/list, once per session, excluded from total)\n", r.SchemaBytes, r.SchemaBytes/4)
	}
	if r.SchemaBytes > 0 && r.ManifestBytes > 0 {
		// The session line is the honest denominator: what a connect actually
		// costs before a single tool call. It is printed because reading the
		// manifest line alone understated it roughly fourfold, and every
		// surface-trim argument in this repository was ranked against that
		// understatement.
		ses := r.SchemaBytes + r.ManifestBytes
		fmt.Fprintf(&b, "bench session %dB ~%d tokens (schema+manifest, paid once before any call)\n", ses, ses/4)
	}
	if r.ManifestBytes > 0 {
		fmt.Fprintf(&b, "bench manifest %dB ~%d tokens (once per session, excluded from total)\n", r.ManifestBytes, r.ManifestBytes/4)
	}
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
	// The section headers NAME the binaries: an A/B whose reader cannot tell
	// which binary sat in which slot invites inverted conclusions — exactly
	// the failure that misrecorded one merge's delta with the sign flipped
	// (found during T-01KYDT, corrected in its record).
	out.WriteString("== baseline " + baseline + " ==\n" + Report(a))
	out.WriteString("== candidate " + candidate + " ==\n" + Report(b))
	fmt.Fprintf(&out, "bench delta %+dB ~%+d tokens (candidate minus baseline)\n", b.Bytes-a.Bytes, b.Tokens-a.Tokens)
	// The delta is only honest when BOTH sides metered: a binary predating
	// the manifest subcommand answers 0, and 3473 minus an unmeterable 0
	// would read as the manifest having grown by its own size.
	switch {
	case a.ManifestBytes > 0 && b.ManifestBytes > 0:
		fmt.Fprintf(&out, "bench manifest delta %+dB (once per session, separate multiplier)\n", b.ManifestBytes-a.ManifestBytes)
	case a.ManifestBytes > 0 || b.ManifestBytes > 0:
		out.WriteString("bench manifest delta n/a — one side predates the manifest subcommand; compare the per-side manifest lines by hand\n")
	}
	// Same measured-versus-unmeasured guard as above, for the same reason: a
	// side that could not be metered reads as 0, and subtracting it would report
	// the schema as having grown or shrunk by its own entire size.
	switch {
	case a.SchemaBytes > 0 && b.SchemaBytes > 0:
		fmt.Fprintf(&out, "bench schema delta %+dB (tools/list, once per session, separate multiplier)\n", b.SchemaBytes-a.SchemaBytes)
	case a.SchemaBytes > 0 || b.SchemaBytes > 0:
		out.WriteString("bench schema delta n/a — one side could not be metered; compare the per-side schema lines by hand\n")
	}
	d := b.Bytes - a.Bytes
	switch {
	case a.Valid && !b.Valid:
		out.WriteString("bench verdict: CANDIDATE LOSES — validity regressed, tokens irrelevant\n")
	case !a.Valid && b.Valid:
		out.WriteString("bench verdict: candidate wins — validity restored\n")
	// Time-ordered IDs give adaptive display prefixes whose length can
	// differ by a character between two runs minted in different moments —
	// a ±2B floor of run-to-run noise measured on self-vs-self A/Bs. A
	// delta inside the floor is indistinguishable from jitter in a single
	// run, and claiming a win on it would be measuring the clock.
	case d >= -2 && d <= 2:
		out.WriteString("bench verdict: tie within the ±2B run-noise floor — rerun to confirm any real delta\n")
	case d < 0:
		out.WriteString("bench verdict: candidate wins — fewer tokens at equal validity\n")
	default:
		out.WriteString("bench verdict: candidate loses — more tokens at equal validity\n")
	}
	return out.String(), nil
}
