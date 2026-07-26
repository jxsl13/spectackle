package mcpserver

// The backward path in every machine-facing surface (T-01KYD88KV): the
// manifest names the three durable stores and the post-merge rebuild, the
// workflow template carries independent re-verification / note guidance /
// restart / research capture, and promptNext leads with the one computed
// next action for the item's state. All of it is guidance plus computed
// hints — the hard gates live in the validation verdict and the research
// consumption gate, not here.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jxsl13/spectackle/internal/item"
)

// Manifest substance, computed (mirror of T-0098): multi-substring
// assertions, not presence-of-paragraph.
func TestManifestTeachesBackwardPath(t *testing.T) {
	for _, want := range []string{
		"BACKPROP",
		"spec.md rules",
		"journal tombstone notes",
		"knowledge artifacts",
		"make dev",
		"the note IS the training signal",
	} {
		if !strings.Contains(instructions, want) {
			t.Errorf("manifest missing backward-path fragment %q", want)
		}
	}
}

// Manifest size ceiling: measured base before this task was 3083 bytes
// (len of the instructions const); the brief allows base + 800.
func TestManifestSizeCeiling(t *testing.T) {
	const ceiling = 3083 + 800
	if n := len(instructions); n > ceiling {
		t.Errorf("instructions = %d bytes, ceiling %d (base 3083 + 800)", n, ceiling)
	}
}

// Template line ceiling: measured base before this task was 62 lines;
// the brief allows growth <= 40.
func TestWorkflowTemplateLineCeiling(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("templates", "commands", "workflow.md.tmpl"))
	if err != nil {
		t.Fatal(err)
	}
	const ceiling = 62 + 40
	if n := strings.Count(string(raw), "\n"); n > ceiling {
		t.Errorf("workflow.md.tmpl = %d lines, ceiling %d (base 62 + 40)", n, ceiling)
	}
	for _, want := range []string{"9.", "10.", "make dev", "VERIFY block from the diff alone", "explicitly closed with a no-action note"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("workflow.md.tmpl missing %q", want)
		}
	}
}

// The one computed next action per state — exact first line.
func TestNextActionPerState(t *testing.T) {
	short := func(id string) string { return id }
	cases := []struct {
		name string
		it   item.Item
		want string
	}{
		{"draft ungrilled", item.Item{ID: "T-1", State: item.StateDraft},
			"next grill id=T-1 — render the critique, then an independent verdict"},
		{"draft grilled open gaps", item.Item{ID: "T-1", State: item.StateDraft, Grilled: "2026-07-26 open=3"},
			"next close the 3 open findings, then grill id=T-1 op=verdict"},
		{"draft grilled clean", item.Item{ID: "T-1", State: item.StateDraft, Grilled: "2026-07-26 open=0"},
			"next move id=T-1 to=submitted"},
		{"submitted", item.Item{ID: "T-1", State: item.StateSubmitted},
			"next move id=T-1 to=approved — or to=rejected with a note"},
		{"approved", item.Item{ID: "T-1", State: item.StateApproved},
			"next work op=start item=T-1"},
		{"active", item.Item{ID: "T-1", State: item.StateActive},
			"next implement + test, then work op=submit"},
		{"done", item.Item{ID: "T-1", State: item.StateDone},
			"next check until ok, then move id=T-1 to=archived with a substance note"},
		{"blocked", item.Item{ID: "T-1", State: item.StateBlocked, Needs: []string{"A-9"}},
			"next decide op=answer id=A-9 — the linked decision is the only exit"},
	}
	for _, c := range cases {
		if got := nextAction(c.it, short); got != c.want {
			t.Errorf("%s: nextAction = %q, want %q", c.name, got, c.want)
		}
	}
}

// promptNext leads with the computed next action (integration over the
// rendered prompt, explicit-item path).
func TestPromptNextLeadsWithNextAction(t *testing.T) {
	root := t.TempDir()
	s, sess := connectRootWithPromptsServer(t, root)
	id := serverDraftID(t, s, draftIn{Kind: "task", Title: "backward path probe task"})
	res := getPromptText(t, sess, "next", map[string]string{"item": id})
	if !strings.HasPrefix(res, "next grill id=") {
		t.Fatalf("promptNext must lead with the computed next action, got: %q", res)
	}
}

// commands op=gen regenerates files carrying steps 9 and 10.
func TestCommandsGenBackwardSteps(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".claude", "commands"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, sess := connectCommands(t, root, nil)
	out := callText(t, sess, "commands", map[string]any{"op": "gen"})
	if !strings.Contains(out, "ok") {
		t.Fatalf("gen: %q", out)
	}
	raw, err := os.ReadFile(filepath.Join(root, ".claude", "commands", "spectackle.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"9.", "10.", "make dev", "explicitly closed with a no-action note"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("generated workflow command missing %q", want)
		}
	}
}
