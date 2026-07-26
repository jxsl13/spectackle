package bench

// ROLE-BOUNDARY-001's bench assertion (T-01KYFSN4): a transcript whose tool
// results instruct the agent to run git fails validity. Calibrated against
// the live scripted surface before pinning — the negative control below is
// built from the real record shapes that surface emits.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitInstructionViolationsPositive(t *testing.T) {
	cases := []struct {
		name, transcript string
	}{
		{"imperative", "i T-1 task done .\nplease run `git push origin` to finish\nok"},
		{"command line", "i T-1 task done .\ngit commit -m \"land it\"\nok"},
		{"dollar prompt", "steps:\n  $ git rebase main\nok"},
	}
	for _, c := range cases {
		got := gitInstructionViolations(c.transcript)
		if len(got) != 1 {
			t.Errorf("%s: want exactly 1 violation, got %d: %v", c.name, len(got), got)
		}
	}
}

// The current legitimate surface must never trip the detector: these lines
// are the real record shapes (gitflow g records, hints, refusals) plus the
// merge vocabulary that legitimately contains the words git-adjacent
// detectors overreach on.
func TestGitInstructionViolationsNegative(t *testing.T) {
	transcript := strings.Join([]string{
		"i T-01ABC task done . some title",
		"g branch spectackle/T-01ABC",
		"g pr 12 draft offline://pr/12",
		"g pr 12 ready offline://pr/12",
		"g pr 12 merged abc123",
		"g records committed 2 files",
		"g records clean",
		"h . binary stale — rebuild+restart: make dev",
		"! ARG E move needs id",
		"! ROUNDS E rounds limit hit",
		"ok validate T-01ABC pass by cross-val",
		"the digital git history is preserved on main", // prose containing 'git' mid-sentence
		"ok",
	}, "\n")
	if got := gitInstructionViolations(transcript); len(got) != 0 {
		t.Errorf("legitimate surface tripped the detector: %v", got)
	}
}

// Detector hits must never carry the "note:" prefix — Run()'s validity loop
// treats note-prefixed violations as informational, and a role-boundary hit
// has to flip Valid=false.
func TestGitInstructionViolationsAreFatalShaped(t *testing.T) {
	got := gitInstructionViolations("run `git push` now\n$ git merge x\n")
	if len(got) != 2 {
		t.Fatalf("two-hit input must yield exactly 2 violations, got %d: %v", len(got), got)
	}
	for _, v := range got {
		if strings.HasPrefix(v, "note:") {
			t.Errorf("violation is note-shaped, would not fail validity: %q", v)
		}
	}
}

// Agent-mode wiring: a workspace transcript.log carrying an instruction
// surfaces through transcriptViolations (the hook ScoreAgentRunAnchored
// feeds into AgentScore.Violations, which every scenario's Valid gate
// requires empty).
func TestTranscriptViolationsScan(t *testing.T) {
	dir := t.TempDir()
	if got := transcriptViolations(dir); got != nil {
		t.Fatalf("missing transcript.log must scan clean, got %v", got)
	}
	if err := os.WriteFile(filepath.Join(dir, "transcript.log"), []byte("all done — now run `git push origin main`\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := transcriptViolations(dir); len(got) != 1 {
		t.Fatalf("want 1 transcript violation, got %v", got)
	}
}
