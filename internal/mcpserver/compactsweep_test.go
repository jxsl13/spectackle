package mcpserver

// Compact's done-item sweep runs the move HANDLER, not raw lifecycle.Move
// (B-01KYHEQG6PEB): the validate-require gate and the git closure edge
// apply to compact exactly as to move, and a blocked item stays done with
// the reason rendered — journal compaction never closes a lifecycle more
// cheaply than the move edge would.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jxsl13/spectackle/internal/wt"
)

func TestCompactSweepBlocksUnvalidatedDone(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".spectackle"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".spectackle", "config.yaml"),
		[]byte("schema: v1\nfeedback:\n  validate: require\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := wt.InitTestRepo(root); err != nil {
		t.Skipf("git unavailable: %v", err)
	}
	t.Setenv("SPECTACKLE_AGENT", "implementer-a")
	sess := connectRoot(t, root)
	task := draftID(t, sess, map[string]any{
		"kind": "task", "title": "done but never validated"})
	callText(t, sess, "move", map[string]any{"id": task, "to": "done"})

	out := callText(t, sess, "compact", map[string]any{"apply": true})
	if !strings.Contains(out, "done-item") || !strings.Contains(out, "blocked:") || !strings.Contains(out, "VALIDATE") {
		t.Fatalf("unvalidated done item must render blocked with the gate's reason: %q", out)
	}
	if strings.Contains(out, "ok archived") {
		t.Fatalf("nothing may archive past the validate gate: %q", out)
	}
	got := callText(t, sess, "get", map[string]any{"id": task})
	if !strings.Contains(got, " done ") {
		t.Fatalf("blocked item must STAY done: %q", got)
	}
}

func TestCompactSweepArchivesThroughClosure(t *testing.T) {
	root := gitRoot(t)
	writeOfflineGitConfig(t, root)
	sess := connectRoot(t, root)
	task := draftID(t, sess, map[string]any{
		"kind": "task", "title": "done and closable offline"})
	callText(t, sess, "move", map[string]any{"id": task, "to": "done"})

	out := callText(t, sess, "compact", map[string]any{"apply": true})
	if !strings.Contains(out, "ok archived") {
		t.Fatalf("a gate-clean done item must archive through the handler: %q", out)
	}
	got := callText(t, sess, "get", map[string]any{"id": task})
	if !strings.Contains(got, "archived") {
		t.Fatalf("item must be archived after the sweep: %q", got)
	}
}
