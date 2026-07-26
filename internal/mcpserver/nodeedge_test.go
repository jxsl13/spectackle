package mcpserver

// NODE-EDGE-001 compliance (T-01KYG90P): the rendered implementer protocol
// and the per-state hints instruct judgment and tool calls only — the bench
// role-boundary detector must find zero git instructions in them, and every
// state hint carries an or-tail of alternatives (nodes offer options).

import (
	"strings"
	"testing"

	"github.com/jxsl13/spectackle/internal/bench"
	"github.com/jxsl13/spectackle/internal/item"
)

func TestPromptNextProtocolNoGitInstructions(t *testing.T) {
	root := t.TempDir()
	s, sess := connectRootWithPromptsServer(t, root)
	id := serverDraftID(t, s, draftIn{Kind: "task", Title: "node edge protocol probe", Body: ambFixturePad})
	out := getPromptText(t, sess, "next", map[string]string{"item": id})
	if v := bench.GitInstructionViolations(out); len(v) != 0 {
		t.Fatalf("protocol instructs git: %v\n%s", v, out)
	}
}

func TestNextActionOffersAlternatives(t *testing.T) {
	short := func(id string) string { return id }
	for _, st := range []string{item.StateDraft, item.StateSubmitted, item.StateApproved, item.StateActive, item.StateDone} {
		line := nextAction(item.Item{ID: "T-1", State: st}, short)
		if !strings.Contains(line, "| or: ") {
			t.Errorf("state %s hint offers no alternatives: %q", st, line)
		}
		if v := bench.GitInstructionViolations(line); len(v) != 0 {
			t.Errorf("state %s hint instructs git: %v", st, v)
		}
	}
}
