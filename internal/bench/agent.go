package bench

// The agent-judge stage (P-01KYDP stage 4): a scripted driver cannot get
// lost, an agent can — so guidance quality is judged by handing a FRESH,
// independent agent a metered fixture and a fixed brief, then scoring the
// run mechanically. The binary only PREPARES (fixture, brief, metering
// shim) and SCORES (goal states reached, bytes consumed); spawning the
// agent is the orchestrator's job and deliberately lives outside this
// repository, so the score stays comparable across server builds: brief
// and fixture are identical by construction.

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// judgeTaskTitle and judgeBugTitle are the goal markers the score looks
// for. They are constants shared by brief and scorer: a drifted pair would
// judge agents against goals the brief never stated.
const (
	judgeTaskTitle = "agent judge run"
	judgeBugTitle  = "bogus report"
)

// agentBrief is the FIXED task brief. Tool names only, zero semantic
// coaching: how the tools work must come from the server's own texts —
// instructions, results, refusals — because those texts are what is being
// judged. Coaching in the brief would mask exactly the guidance gaps the
// judge exists to find.
const agentBrief = `You operate a spec-driven-development workflow through a CLI.

Workspace: %s
Command shape, the ONLY allowed way to act:
  %s call -root %s <tool> '<json-arguments>'

Available tool names: state, draft, move, get, find, grill, check, rule, decide, research.
No other documentation is provided; the tool outputs themselves guide you.

Goals, in any order:
1. A task titled "` + judgeTaskTitle + `" exists and ends in state archived.
2. A bug titled "` + judgeBugTitle + `" exists and ends in state rejected.
3. A final check call reports ok.

Start with the state tool. When all goals are met, stop and output exactly DONE.
`

// trickyTaskTitle and trickyRuleStem are the tricky scenario's goal
// markers, shared by brief and scorer like the basic pair.
const (
	trickyTaskTitle = "stubborn rework loop"
	trickyRuleStem  = "JUDGE-API"
)

// trickyBrief sends the agent through the side states the basic scenario
// never touches (T-01KYE4): rule slot elicitation and lint, the reopen
// counter, the ROUNDS escalation into blocked, and the decide exit. Same
// zero-coaching discipline: tool names only, the server's own texts must
// carry the agent through — those texts are what is being judged.
const trickyBrief = `You operate a spec-driven-development workflow through a CLI.

Workspace: %s
Command shape, the ONLY allowed way to act:
  %s call -root %s <tool> '<json-arguments>'

Available tool names: state, draft, move, get, find, grill, check, rule, decide, research.
No other documentation is provided; the tool outputs themselves guide you.

Goals, in order:
1. A rule with stem "` + trickyRuleStem + `" exists in directory "api". Its response clause must name a concrete number.
2. A task titled "` + trickyTaskTitle + `" is driven to done, reopened to active, driven to done again, and reopened again — keep going until the server refuses to continue this loop and points you at a decision; resolve that decision choosing rescope, so the task ends in state draft.
3. A final check call reports no findings marked E.

Start with the state tool. When all goals are met, stop and output exactly DONE.
`

// worktreeTaskTitle is the worktree scenario's goal marker.
const worktreeTaskTitle = "worktree delivery run"

// worktreeBrief sends the agent through the swarm core (T-01KYE9): claim a
// task, open a worktree, edit under the root the server reports, submit.
// The brief names the DELIVERABLE and one constraint; that the work tool is
// the way remains the judged discovery — the tool list names it, the
// manifest MODES pointer and the start hint carry the rest.
const worktreeBrief = `You operate a spec-driven-development workflow through a CLI.

Workspace: %s
Command shape, the ONLY allowed way to act:
  %s call -root %s <tool> '<json-arguments>'

Available tool names: state, draft, move, get, find, grill, check, rule, decide, research, work, lease, swarm.
No other documentation is provided; the tool outputs themselves guide you.
Workspace files are read-only for you, with one exception: you may edit files under a directory root that a tool's output explicitly reports for that purpose.

Goals, in order:
1. A task titled "` + worktreeTaskTitle + `" exists, and its code change — the function Serve in api/api.go returns 7 instead of 0 — reaches the repository's default branch.
2. That task ends in state done.
3. A final check call reports no findings marked E.

Start with the state tool. When all goals are met, stop and output exactly DONE.
`

// meterShim passes every call through to the real binary while appending
// one line per call to meter.log: <seq> <nonce> <bytes> <exit> <argv>.
// Stdout and stderr are metered together — both reach the agent's context,
// so both are cost. Seq and nonce are tamper evidence (B-01KYE6G): the
// meter lives in a workspace the agent controls, and a live judge under
// pressure rewrote its log. Seq contiguity catches mid-file deletion and
// resets; the nonce catches a regenerated log; tail truncation survives
// both and is caught by the scorer's journal write-event delta instead —
// the erased calls still wrote their journal events.
const meterShim = `#!/bin/sh
# generated by spectackle bench -agent-prep; passes through to the real
# binary and meters output bytes per call. Only the call subcommand may
# pass (B-01KYEA): a live judge reset its own tamper evidence by running
# bench -agent-prep THROUGH this shim — re-prep rewrites the nonce and the
# journal baseline consistently, leaving every integrity check green.
REAL=%q
METER=%q
NONCE=%q
TRANSCRIPT=%q
if [ "$1" != "call" ]; then
  echo "! SHIM E only the call subcommand is allowed through the meter"
  exit 2
fi
OUT=$("$REAL" "$@" 2>&1)
rc=$?
printf '%%s\n' "$OUT"
printf '%%s\n' "$OUT" >> "$TRANSCRIPT"
# read-count-then-append must be atomic under a judge's PARALLEL calls
# (B-01KYGZNT): mkdir is the portable POSIX lock — flock(1) does not exist
# on stock macOS. The trap frees the lock even if the shim is killed.
LOCK="$METER.lock"
i=0
until mkdir "$LOCK" 2>/dev/null; do
  i=$((i+1)); [ $i -gt 500 ] && break
  sleep 0.01
done
trap 'rmdir "$LOCK" 2>/dev/null' EXIT
if [ -f "$METER" ]; then SEQ=$(($(wc -l < "$METER") + 1)); else SEQ=1; fi
# argv can carry REAL newlines (a draft body in the JSON argument): written
# verbatim it spans physical lines, wc -l overcounts, and the seq chain
# reads as holes (the actual warn-2 incident mechanism, B-01KYGZNT).
# One entry = one line, always.
ARGS=$(printf '%%s' "$*" | tr '\n\r' '  ')
printf '%%d %%s %%d %%d %%s\n' "$SEQ" "$NONCE" "${#OUT}" "$rc" "$ARGS" >> "$METER"
rmdir "$LOCK" 2>/dev/null
trap - EXIT
exit $rc
`

// AgentPrep builds a judge workspace in dir: the seeded v2 fixture, the
// fixed brief, and the metering shim wrapping bin. It prints nothing —
// callers report the returned paths.
//
// withManifest prepends the composed connect-time manifest to the brief —
// the judge then simulates an MCP session, which reads exactly that text at
// initialize — and records its byte count in a manifest.size sidecar so the
// scorer can report the session-shaped cost next to the tool diet
// (T-01KYE3X: whether connect-time guidance BUYS anything is measured, not
// assumed). Without the flag the brief is byte-identical to before.
// The returned nonce is the orchestrator's out-of-band anchor (B-01KYEA):
// every in-workspace artifact can be rewritten consistently by a re-prep,
// so the only evidence a reset cannot forge is a value held OUTSIDE the
// workspace since prep time — record it, and pass it back to the scorer.
func AgentPrep(bin, dir string, withManifest bool, scenario string) (briefPath, shimPath, nonce string, err error) {
	if err := Fixture(dir); err != nil {
		return "", "", "", err
	}
	if err := Seed(bin, dir); err != nil {
		return "", "", "", err
	}
	shimPath = filepath.Join(dir, "meter.sh")
	briefPath = filepath.Join(dir, "brief.md")
	meterLog := filepath.Join(dir, "meter.log")
	// Harness artifacts must never be git-visible in the fixture
	// (B-01KYH3SP): the first active-transition sweep tracks everything
	// outside .spectackle, every shim call re-dirties the logs, and the
	// offline merge's checkout then refuses FOREVER — a judge proved
	// empirically that no sanctioned call sequence can leave the tree
	// clean. Written before the judge's first call, appended not clobbered.
	harnessIgnore := "# spectackle bench harness artifacts — never fixture content\n" +
		"meter.log\ntranscript.log\nmeter.sh\nbrief.md\njournal.baseline\n" +
		"trap.hash\nscenario\nmanifest.size\nmeter.log.lock/\n"
	gi := filepath.Join(dir, ".gitignore")
	existing, _ := os.ReadFile(gi)
	if err := os.WriteFile(gi, append(existing, []byte(harnessIgnore)...), 0o644); err != nil {
		return "", "", "", err
	}
	// The prepped tree must be CLEAN: an untracked .gitignore is exactly
	// the out-of-scope dirt the transition guard (issue 178 defect 2)
	// refuses on — a judge would stall on prep debris it never created.
	// Best-effort with identity flags: a bare CI runner has no git config.
	_ = exec.Command("git", "-C", dir, "add", ".gitignore").Run()
	_ = exec.Command("git", "-C", dir, "-c", "user.name=bench", "-c", "user.email=bench@localhost",
		"commit", "-q", "-m", "bench prep: harness ignore rules").Run()

	nonceBytes := make([]byte, 8)
	if _, err := rand.Read(nonceBytes); err != nil {
		return "", "", "", err
	}
	nonce = hex.EncodeToString(nonceBytes)
	if err := os.WriteFile(shimPath, fmt.Appendf(nil, meterShim, bin, meterLog, nonce, filepath.Join(dir, "transcript.log")), 0o755); err != nil {
		return "", "", "", err
	}
	// Journal baseline for the scorer's write-event delta (B-01KYE6G): the
	// seeds above already wrote events, so the tamper check must count only
	// what happens after prep.
	if err := os.WriteFile(filepath.Join(dir, "journal.baseline"),
		fmt.Appendf(nil, "%d\n", journalLines(dir)), 0o644); err != nil {
		return "", "", "", err
	}
	var brief string
	switch scenario {
	case "", "basic":
		brief = fmt.Sprintf(agentBrief, dir, shimPath, dir)
	case "tricky":
		brief = fmt.Sprintf(trickyBrief, dir, shimPath, dir)
		// The scorer branches on this sidecar: tricky goals are judged by
		// different criteria, and inferring the scenario from brief prose
		// would couple the scorer to wording.
		if err := os.WriteFile(filepath.Join(dir, "scenario"), []byte("tricky\n"), 0o644); err != nil {
			return "", "", "", err
		}
	case "worktree":
		brief = fmt.Sprintf(worktreeBrief, dir, shimPath, dir)
		if err := os.WriteFile(filepath.Join(dir, "scenario"), []byte("worktree\n"), 0o644); err != nil {
			return "", "", "", err
		}
	case "outcome-ask":
		// same fixture, traps, and SCORING as outcome (the marker below
		// says outcome deliberately); only the brief adds the ask channel.
		brief = fmt.Sprintf(outcomeAskBrief, dir, shimPath, dir)
		if err := os.WriteFile(filepath.Join(dir, "scenario"), []byte("outcome\n"), 0o644); err != nil {
			return "", "", "", err
		}
		if err := seedOutcomeTrap(dir); err != nil {
			return "", "", "", err
		}
		if err := os.WriteFile(filepath.Join(dir, "trap.hash"), []byte(legacyTrapHash(dir)+"\n"), 0o644); err != nil {
			return "", "", "", err
		}
	case "outcome":
		brief = fmt.Sprintf(outcomeBrief, dir, shimPath, dir)
		if err := os.WriteFile(filepath.Join(dir, "scenario"), []byte("outcome\n"), 0o644); err != nil {
			return "", "", "", err
		}
		if err := seedOutcomeTrap(dir); err != nil {
			return "", "", "", err
		}
		// Trap fingerprint: scoring detects any change to the offscope
		// temptation file against this prep-time hash.
		if err := os.WriteFile(filepath.Join(dir, "trap.hash"), []byte(legacyTrapHash(dir)+"\n"), 0o644); err != nil {
			return "", "", "", err
		}
	default:
		return "", "", "", fmt.Errorf("bench agent-prep: unknown scenario %q (basic|tricky|worktree|outcome|outcome-ask)", scenario)
	}
	if withManifest {
		out, err := exec.Command(bin, "manifest").Output()
		if err != nil {
			return "", "", "", fmt.Errorf("bench agent-prep: manifest: %w", err)
		}
		brief = "Connect-time server instructions (what an MCP session receives at initialize):\n\n" +
			string(out) + "\n---\n\n" + brief
		if err := os.WriteFile(filepath.Join(dir, "manifest.size"),
			fmt.Appendf(nil, "%d\n", len(out)), 0o644); err != nil {
			return "", "", "", err
		}
	}
	if err := os.WriteFile(briefPath, []byte(brief), 0o644); err != nil {
		return "", "", "", err
	}
	return briefPath, shimPath, nonce, nil
}

// scoreWorktree judges the swarm-core scenario (T-01KYE9). The file
// assertion alone cannot distinguish the worktree flow from an illegal
// direct edit on main, so successful work start AND submit calls in the
// meter are a first-class goal column: WorkOK is what proves the change
// traveled through the machinery under judgment.
func scoreWorktree(bin, dir string, sc AgentScore, meterRaw string) (AgentScore, error) {
	sc.Scenario = "worktree"

	if src, err := os.ReadFile(filepath.Join(dir, "api", "api.go")); err == nil &&
		strings.Contains(string(src), "return 7") {
		sc.RuleOK = true // reused column: the delivered-change goal
	}

	stateOut, _, err := callOnce(bin, dir, "state", "{}")
	if err != nil {
		return sc, err
	}
	for line := range strings.SplitSeq(stateOut, "\n") {
		if strings.Contains(line, worktreeTaskTitle) && strings.Contains(line, " done ") {
			sc.TaskState = "done"
		}
	}

	startOK, submitOK := false, false
	for line := range strings.SplitSeq(strings.TrimSpace(meterRaw), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 8 || fields[3] != "0" || fields[7] != "work" {
			continue
		}
		if strings.Contains(line, "start") {
			startOK = true
		}
		if strings.Contains(line, "submit") {
			submitOK = true
		}
	}
	sc.DecideOK = startOK && submitOK // reused column: the flow proof

	sc.GoalsOK = sc.RuleOK && sc.TaskState == "done" && sc.DecideOK && sc.CheckOK
	sc.Valid = sc.GoalsOK && !sc.Disqualified && len(sc.Violations) == 0
	return sc, nil
}

// reShimNonce extracts the nonce prep embedded in meter.sh.
var reShimNonce = regexp.MustCompile(`(?m)^NONCE="?([0-9a-f]{16})"?$`)

// writeTools are the tools whose exit-0 calls can append journal events;
// maxEventsPerWrite is the measured per-call maximum (escalation writes 3:
// the move, the side-step, the minted decision). Both feed the journal
// tamper bound — measured on the live fixture, not assumed.
var writeTools = map[string]bool{
	"draft": true, "move": true, "rule": true, "decide": true,
	"work": true, "knowledge": true, "commands": true,
}

const maxEventsPerWrite = 3

// journalLines counts lines across every .spectackle/journal.ndjson under
// dir — the server-side write ledger the meter is cross-checked against.
func journalLines(dir string) int {
	total := 0
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() != "journal.ndjson" {
			return nil
		}
		if raw, err := os.ReadFile(p); err == nil {
			total += strings.Count(string(raw), "\n")
		}
		return nil
	})
	return total
}

// AgentScore judges a completed run in dir mechanically. Verdict rules:
// both goal items in their goal states, and the final check E-free (the
// B-01KYDM taxonomy: W findings are environmental degradations, E findings
// are failures). Bytes come from meter.log — the agent's whole tool-output
// diet, including every refusal and retry it needed.
type AgentScore struct {
	Calls     int
	Bytes     int
	Tokens    int
	TaskState string // state of the judge task, "" if absent
	BugState  string // state of the judge bug, "" if absent
	CheckOK   bool
	Valid     bool
	// ManifestBytes is the connect-time manifest's size when the run was
	// prepped -with-manifest (read from the manifest.size sidecar), zero
	// otherwise. Kept OUT of Bytes: the aggregate spreads compare tool
	// diets, and folding a per-session constant into them would blur
	// exactly the wandering signal they exist to show.
	ManifestBytes int
	// Scenario is "basic" or "tricky" (from the scenario sidecar); RuleOK
	// is the tricky rule goal, DecideOK the tricky decide-answer proof.
	Scenario string
	RuleOK   bool
	DecideOK bool
	// Disqualified marks a run whose METER failed integrity (B-01KYE6G):
	// sequence gap, nonce mismatch, or more journal write events than
	// metered calls. Distinct from invalid because the remedies differ —
	// invalid means the texts failed the agent, disqualified means the
	// measurement failed the harness. DisqualifyReason names the check.
	Disqualified     bool
	DisqualifyReason string

	// Violations are role-boundary hits (ROLE-BOUNDARY-001): tool-result
	// text instructing the agent to run git, scanned off transcript.log.
	// Non-empty kills Valid — a surface that delegates mechanical git back
	// to the agent fails the run regardless of cost.
	Violations []string
	// GoalsOK is the scenario's goal conjunction ALONE — set beside every
	// Valid assignment so the report can tell "goals held but violations
	// voided the run" from "goals not reached" (B-01KYJ67RF9) without
	// re-deriving scoring in the render.
	GoalsOK bool

	// Outcome scenario (T-01KYFSQQ): hidden-acceptance results as "n/m"
	// fractions — FirstPass at the first done edge (or "unavailable" when
	// no done edge exists to check out), FinalPass on the tree at scoring
	// time — plus the reopen rounds the run paid. Completeness per token
	// is computed from FirstPass; Rounds prices the correction loops.
	FirstPass string
	FinalPass string
	Rounds    int
	// AskCount: exit-0 decide op=ask calls in the meter — the elicitation
	// usage the outcome-ask variant measures (T-01KYH6H9).
	AskCount int

	writeCalls int // metered exit-0 calls to write-capable tools (journal bound)
}

// transcriptViolations scans the shim's transcript tee for role-boundary
// hits (ROLE-BOUNDARY-001). A workspace prepped before the tee existed has
// no transcript.log and scans clean rather than erroring: absence of
// evidence here is absence of the surface under test, not tampering (the
// meter integrity checks own that axis).
func transcriptViolations(dir string) []string {
	traw, err := os.ReadFile(filepath.Join(dir, "transcript.log"))
	if err != nil {
		return nil
	}
	return gitInstructionViolations(string(traw))
}

func ScoreAgentRun(bin, dir string) (AgentScore, error) {
	return ScoreAgentRunAnchored(bin, dir, "")
}

// ScoreAgentRunAnchored additionally verifies the out-of-band nonce anchor
// (B-01KYEA): expectedNonce is the value prep printed, held by the
// orchestrator OUTSIDE the workspace — the one piece of evidence a full
// consistent re-prep cannot forge. A mismatch disqualifies before any goal
// is even considered.
func ScoreAgentRunAnchored(bin, dir, expectedNonce string) (AgentScore, error) {
	var sc AgentScore

	raw, err := os.ReadFile(filepath.Join(dir, "meter.log"))
	if err != nil {
		return sc, fmt.Errorf("bench agent-score: no meter.log — did the agent use the shim? %w", err)
	}
	// The shim's nonce is read back out of meter.sh: prep wrote it there,
	// and a log regenerated without the shim cannot carry it.
	nonce := ""
	if shimRaw, err := os.ReadFile(filepath.Join(dir, "meter.sh")); err == nil {
		if m := reShimNonce.FindStringSubmatch(string(shimRaw)); m != nil {
			nonce = m[1]
		}
	}
	if expectedNonce != "" && nonce != expectedNonce {
		sc.Disqualified = true
		sc.DisqualifyReason = "nonce anchor mismatch: the workspace was re-prepped after the orchestrator's prep"
	}
	var seqs []int
	for line := range strings.SplitSeq(strings.TrimSpace(string(raw)), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		seq, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		n, err := strconv.Atoi(fields[2])
		if err != nil {
			continue
		}
		sc.Calls++
		sc.Bytes += n
		seqs = append(seqs, seq)
		if nonce != "" && fields[1] != nonce && !sc.Disqualified {
			sc.Disqualified = true
			sc.DisqualifyReason = "meter nonce mismatch: log not written by this workspace's shim"
		}
		// argv is `call -root <dir> <tool> <json>`: the tool sits at index 7.
		if len(fields) >= 8 && fields[3] == "0" && fields[4] == "call" && writeTools[fields[7]] {
			sc.writeCalls++
		}
		if len(fields) >= 8 && fields[3] == "0" && fields[7] == "decide" && strings.Contains(line, "ask") {
			sc.AskCount++
		}
	}
	// Sequence COMPLETENESS, order-tolerant (B-01KYGZNT): pre-lock logs
	// from parallel-calling judges append out of order without any hole or
	// duplicate — that is concurrency, not tampering. Sorted seqs must be
	// exactly 1..N; a hole or duplicate still disqualifies. Tail
	// truncation (removing the highest seqs) is hole-free by construction
	// and is owned by the journal write-delta check below, not this one.
	sort.Ints(seqs)
	for i, q := range seqs {
		if q != i+1 && !sc.Disqualified {
			sc.Disqualified = true
			sc.DisqualifyReason = fmt.Sprintf("meter sequence broken: sorted position %d carries seq %d (hole or duplicate)", i+1, q)
			break
		}
	}
	sc.Tokens = sc.Bytes / 4
	// Journal write-event delta: every journal event needs a write call,
	// and one call writes at most maxEventsPerWrite events (measured, not
	// assumed: escalation 3, archive skip 2, plain draft/move/rule 1,
	// decide answer 0). More new events than the metered write calls could
	// have produced means unmetered calls happened — the shim was bypassed
	// or the log tail was truncated, the two tamper shapes sequence
	// contiguity cannot see.
	if baseRaw, err := os.ReadFile(filepath.Join(dir, "journal.baseline")); err == nil {
		if base, err := strconv.Atoi(strings.TrimSpace(string(baseRaw))); err == nil {
			if delta := journalLines(dir) - base; delta > sc.writeCalls*maxEventsPerWrite && !sc.Disqualified {
				sc.Disqualified = true
				sc.DisqualifyReason = fmt.Sprintf("journal shows %d new write events but the metered %d write calls could have produced at most %d",
					delta, sc.writeCalls, sc.writeCalls*maxEventsPerWrite)
			}
		}
	}
	sc.Violations = append(sc.Violations, transcriptViolations(dir)...)
	if raw, err := os.ReadFile(filepath.Join(dir, "manifest.size")); err == nil {
		if n, err := strconv.Atoi(strings.TrimSpace(string(raw))); err == nil {
			sc.ManifestBytes = n
		}
	}

	checkOut, _, err := callOnce(bin, dir, "check", "{}")
	if err != nil {
		return sc, err
	}
	sc.CheckOK = strings.TrimSpace(checkOut) == "ok" || !strings.Contains(checkOut, " E ")

	switch scen, _ := os.ReadFile(filepath.Join(dir, "scenario")); strings.TrimSpace(string(scen)) {
	case "tricky":
		return scoreTricky(bin, dir, sc, string(raw))
	case "worktree":
		return scoreWorktree(bin, dir, sc, string(raw))
	case "outcome":
		return scoreOutcome(bin, dir, sc, string(raw))
	}

	// Basic scenario. Goal states are judged through the same tool surface
	// the agent used: archived and rejected items leave work.md entirely
	// (the journal holds the tombstone and the reject snapshot), so the
	// history and rejection scopes of find are the authoritative — and
	// public — answer.
	histOut, _, err := callOnce(bin, dir, "find", fmt.Sprintf(`{"q":%q,"scope":"history"}`, judgeTaskTitle))
	if err != nil {
		return sc, err
	}
	if strings.Contains(histOut, " archive ") && strings.Contains(histOut, judgeTaskTitle) {
		sc.TaskState = "archived"
	}
	rejOut, _, err := callOnce(bin, dir, "find", fmt.Sprintf(`{"q":%q,"scope":"rejection"}`, judgeBugTitle))
	if err != nil {
		return sc, err
	}
	if strings.Contains(rejOut, " reject ") && strings.Contains(rejOut, judgeBugTitle) {
		sc.BugState = "rejected"
	}

	sc.GoalsOK = sc.TaskState == "archived" && sc.BugState == "rejected" && sc.CheckOK
	sc.Valid = sc.GoalsOK && !sc.Disqualified && len(sc.Violations) == 0
	return sc, nil
}

// scoreTricky judges the side-state scenario (T-01KYE4). Ground truths the
// criteria rest on, probed before this was built: find scope=history shows
// only create and archive events, so the reopen cycle is NOT scorable there
// — instead the chain is proven by its endpoints: the task ends at draft
// (only the rescope exit lands there from blocked) and the meter shows a
// successful decide call answering it (an agent cannot legally answer a
// decision that was never minted, and only the exhausted reopen loop mints
// one for this task).
func scoreTricky(bin, dir string, sc AgentScore, meterRaw string) (AgentScore, error) {
	ruleOut, _, err := callOnce(bin, dir, "find", fmt.Sprintf(`{"q":%q,"scope":"rule"}`, trickyRuleStem))
	if err != nil {
		return sc, err
	}
	ruleOK := strings.Contains(ruleOut, trickyRuleStem)

	// The task's end state comes from state's live item listing: draft
	// items are present there (only archived/rejected tombstone away).
	stateOut, _, err := callOnce(bin, dir, "state", "{}")
	if err != nil {
		return sc, err
	}
	for line := range strings.SplitSeq(stateOut, "\n") {
		if strings.Contains(line, trickyTaskTitle) && strings.Contains(line, " draft ") {
			sc.TaskState = "draft"
		}
	}

	for line := range strings.SplitSeq(strings.TrimSpace(meterRaw), "\n") {
		fields := strings.Fields(line)
		// meter format: <seq> <nonce> <bytes> <exit> <argv...> (B-01KYE6G)
		if len(fields) >= 6 && fields[3] == "0" && strings.Contains(line, " decide ") && strings.Contains(line, "answer") {
			sc.DecideOK = true
		}
	}
	sc.Scenario = "tricky"
	sc.RuleOK = ruleOK
	sc.GoalsOK = ruleOK && sc.TaskState == "draft" && sc.DecideOK && sc.CheckOK
	sc.Valid = sc.GoalsOK && !sc.Disqualified && len(sc.Violations) == 0
	return sc, nil
}

// AgentReport renders one judged run as dense agent lines.
func AgentReport(sc AgentScore) string {
	var b strings.Builder
	fmt.Fprintf(&b, "agent calls=%d bytes=%d ~%d tokens\n", sc.Calls, sc.Bytes, sc.Tokens)
	if sc.ManifestBytes > 0 {
		fmt.Fprintf(&b, "agent session=%dB (manifest %d + tools %d)\n", sc.ManifestBytes+sc.Bytes, sc.ManifestBytes, sc.Bytes)
	}
	switch sc.Scenario {
	case "tricky":
		fmt.Fprintf(&b, "agent goal rule=%v task=%s decide=%v check=%v\n", sc.RuleOK, orAbsent(sc.TaskState), sc.DecideOK, sc.CheckOK)
	case "worktree":
		fmt.Fprintf(&b, "agent goal change=%v task=%s flow=%v check=%v\n", sc.RuleOK, orAbsent(sc.TaskState), sc.DecideOK, sc.CheckOK)
	case "outcome":
		fmt.Fprintf(&b, "agent goal task=%s check=%v\n", orAbsent(sc.TaskState), sc.CheckOK)
		fmt.Fprintf(&b, "agent outcome first-pass=%s final-pass=%s rounds=%d asks=%d\n", orAbsent(sc.FirstPass), orAbsent(sc.FinalPass), sc.Rounds, sc.AskCount)
	default:
		fmt.Fprintf(&b, "agent goal task=%s bug=%s check=%v\n", orAbsent(sc.TaskState), orAbsent(sc.BugState), sc.CheckOK)
	}
	// Violations render BEFORE the verdict, one line each — a run voided
	// by a violation while every goal held used to claim "goals not
	// reached", and the orchestrator had to re-implement the trap script
	// to learn why (B-01KYJ67RF9). Never-silent applies to the judge
	// harness too.
	for _, v := range sc.Violations {
		fmt.Fprintf(&b, "agent violation %s\n", v)
	}
	switch {
	case sc.Disqualified:
		fmt.Fprintf(&b, "agent verdict: DISQUALIFIED — %s\n", sc.DisqualifyReason)
	case sc.Valid:
		b.WriteString("agent verdict: valid — goals reached\n")
	case sc.GoalsOK:
		fmt.Fprintf(&b, "agent verdict: INVALID — violations (%d)\n", len(sc.Violations))
	default:
		b.WriteString("agent verdict: INVALID — goals not reached\n")
	}
	return b.String()
}

func orAbsent(s string) string {
	if s == "" {
		return "absent"
	}
	return s
}

// AggregateReport renders n judged runs: per-run lines prefixed with their
// label, then one summary. One run cannot carry a verdict — at one build,
// valid runs have spanned 17-46 calls and 2279-5896 bytes — so the spread
// IS the signal. Median over mean: a single wanderer must not drag the
// central figure. AllValid gates the caller's exit code: a text change that
// makes one judge in three fail is a regression however good the median
// looks.
func AggregateReport(labels []string, scores []AgentScore) (string, bool) {
	var b strings.Builder
	allValid := true
	calls := make([]int, 0, len(scores))
	bytes := make([]int, 0, len(scores))
	valid, disq := 0, 0
	for i, sc := range scores {
		for line := range strings.SplitSeq(strings.TrimRight(AgentReport(sc), "\n"), "\n") {
			fmt.Fprintf(&b, "%s %s\n", labels[i], line)
		}
		calls = append(calls, sc.Calls)
		bytes = append(bytes, sc.Bytes)
		switch {
		case sc.Disqualified:
			disq++
			allValid = false
		case sc.Valid:
			valid++
		default:
			allValid = false
		}
	}
	fmt.Fprintf(&b, "agents n=%d valid=%d/%d", len(scores), valid, len(scores))
	if disq > 0 {
		// Labeled apart from invalid: invalid means the texts failed the
		// agent, disqualified means the measurement failed the harness.
		fmt.Fprintf(&b, " disqualified=%d", disq)
	}
	fmt.Fprintf(&b, " calls=%s bytes=%s\n", spread(calls), spread(bytes))

	// Outcome efficiency: first-iteration completeness per 10K tokens, per
	// label group. The comparison REFUSES to render when any run in the set
	// is invalid or disqualified — the same all-valid rule the cost A/B
	// already enforces (docs/bench-curves.md): efficiency at unequal
	// validity compares apples to crashed carts.
	hasOutcome := false
	for _, sc := range scores {
		if sc.Scenario == "outcome" {
			hasOutcome = true
			break
		}
	}
	if hasOutcome {
		if !allValid {
			b.WriteString("agents efficiency REFUSED unequal validity — all runs must be valid before completeness per token compares\n")
		} else {
			type acc struct {
				frac   float64
				tokens int
				n      int
			}
			groups := map[string]*acc{}
			var order []string
			for i, sc := range scores {
				if sc.Scenario != "outcome" {
					continue
				}
				g, ok := groups[labels[i]]
				if !ok {
					g = &acc{}
					groups[labels[i]] = g
					order = append(order, labels[i])
				}
				g.frac += outcomeFirstPassFrac(sc.FirstPass)
				g.tokens += sc.Tokens
				g.n++
			}
			for _, lb := range order {
				g := groups[lb]
				meanFrac := g.frac / float64(g.n)
				meanTok := float64(g.tokens) / float64(g.n)
				eff := 0.0
				if meanTok > 0 {
					eff = meanFrac / (meanTok / 10000)
				}
				fmt.Fprintf(&b, "agents efficiency %s first-pass=%.2f per-10Ktok=%.3f n=%d\n", lb, meanFrac, eff, g.n)
			}
		}
	}
	return b.String(), allValid
}

// spread renders min/median/max. Even-length medians take the lower middle:
// judge counts are tiny, and a half-value would suggest precision the
// sample cannot carry.
func spread(vals []int) string {
	s := append([]int(nil), vals...)
	sort.Ints(s)
	return fmt.Sprintf("%d/%d/%d", s[0], s[(len(s)-1)/2], s[len(s)-1])
}
