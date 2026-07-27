package mcpserver

// Config resolution across the serving-worktree split (B-01KYHTJ4AP): the
// behavioral git fields — mode, enabled, commits — follow the SERVING root,
// repo-global plumbing (remote, base) follows the primary checkout, and a
// divergence renders one stateless never-silent line per transition. The
// live incident: a worktree branch's `mode: online` was invisible because
// every edge read the primary checkout's stale config.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jxsl13/spectackle/internal/wt"
)

// servedWorktree preps the resident-orchestrator topology: a primary
// checkout plus a linked worktree the server is rooted at, whose committed
// config may diverge from the primary's.
func servedWorktree(t *testing.T, wsGitYAML string) (mainRoot, wtDir string) {
	t.Helper()
	mainRoot = gitRoot(t)
	wtDir = filepath.Join(mainRoot, ".spectackle", "wt", "serving")
	if err := wt.Add(mainRoot, wtDir, "serving/modesplit", "HEAD", "main", false); err != nil {
		t.Fatal(err)
	}
	if wsGitYAML != "" {
		if err := os.WriteFile(filepath.Join(wtDir, ".spectackle", "config.yaml"),
			[]byte(wsGitYAML), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return mainRoot, wtDir
}

// The incident shape: primary config predates the flip (no git block →
// offline default), the serving branch says online — the ONLINE shape must
// run and the divergence must be said.
func TestServingRootModeWinsOverMain(t *testing.T) {
	_, wtDir := servedWorktree(t, "schema: v1\ngit:\n  mode: online\n")
	sess := connectRoot(t, wtDir)
	id := draftID(t, sess, map[string]any{
		"kind": "task", "title": "ws online beats main offline", "body": ambFixturePad})
	out := callText(t, sess, "move", map[string]any{"id": id, "to": "active"})
	if strings.Contains(out, "g offline commit") {
		t.Fatalf("edge ran the offline shape despite ws mode online:\n%s", out)
	}
	// online-shape evidence in a remote-less fixture: the push was
	// ATTEMPTED (offline never pushes) — its no-remote error is expected
	if !strings.Contains(out, "push") {
		t.Fatalf("online shape (push attempt) missing:\n%s", out)
	}
	if !strings.Contains(out, "w git config diverges") || !strings.Contains(out, "mode ws=online main=offline -> online") {
		t.Fatalf("divergence line missing or wrong:\n%s", out)
	}
}

func TestMainOnlineDoesNotLeakIntoOfflineServingRoot(t *testing.T) {
	mainRoot, wtDir := servedWorktree(t, "schema: v1\ngit:\n  mode: offline\n")
	if err := os.WriteFile(filepath.Join(mainRoot, ".spectackle", "config.yaml"),
		[]byte("schema: v1\ngit:\n  mode: online\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sess := connectRoot(t, wtDir)
	id := draftID(t, sess, map[string]any{
		"kind": "task", "title": "ws offline beats main online", "body": ambFixturePad})
	out := callText(t, sess, "move", map[string]any{"id": id, "to": "active"})
	if strings.Contains(out, "g branch ") {
		t.Fatalf("edge ran the online shape despite ws mode offline:\n%s", out)
	}
	if !strings.Contains(out, "w git config diverges") || !strings.Contains(out, "mode ws=offline main=online -> offline") {
		t.Fatalf("divergence line missing or wrong:\n%s", out)
	}
}

// Identical configs — worktree topology or plain root — render NO
// divergence noise: the common case stays byte-identical.
func TestNoDivergenceLineWhenConfigsAgree(t *testing.T) {
	_, wtDir := servedWorktree(t, "")
	sess := connectRoot(t, wtDir)
	id := draftID(t, sess, map[string]any{
		"kind": "task", "title": "agreeing configs stay quiet", "body": ambFixturePad})
	out := callText(t, sess, "move", map[string]any{"id": id, "to": "active"})
	if strings.Contains(out, "w git config diverges") {
		t.Fatalf("divergence line rendered without divergence:\n%s", out)
	}
}

// Remote and base stay the primary checkout's — a branch config must not
// re-point pushes (the deliberate asymmetry, locked as a unit assertion).
func TestEffectiveGitKeepsMainRemoteAndBase(t *testing.T) {
	_, wtDir := servedWorktree(t, "schema: v1\ngit:\n  mode: online\n  remote: fork\n  base: dev\n")
	s, _ := connectRootWithServer(t, wtDir)
	g := s.effectiveGit()
	if g.Mode != "online" {
		t.Fatalf("Mode must follow the serving root, got %q", g.Mode)
	}
	if g.Remote != s.main.Cfg.Git.Remote || g.Remote == "fork" {
		t.Fatalf("Remote must stay the primary's, got %q", g.Remote)
	}
	if g.Base == "dev" {
		t.Fatalf("Base must stay the primary's, got %q", g.Base)
	}
}

// enabled disagreement: both engines — the gitflow gate AND the
// edge-commit capture — must resolve it the same way (they disagreed
// before this fix: edgecommit read ws, gitGate read main).
func TestGitGateAndEdgeCommitsAgreeOnEnabled(t *testing.T) {
	mainRoot, wtDir := servedWorktree(t, "schema: v1\ngit:\n  enabled: false\n")
	sess := connectRoot(t, wtDir)
	pre := strings.Count(closureGit(t, mainRoot, "log", "--oneline", "--all"), "\n")
	id := draftID(t, sess, map[string]any{
		"kind": "task", "title": "disabled on the serving root", "body": ambFixturePad})
	out := callText(t, sess, "move", map[string]any{"id": id, "to": "active"})
	if !strings.Contains(out, "g git off: disabled in config") {
		t.Fatalf("gitflow gate must honor the serving root's disable:\n%s", out)
	}
	post := strings.Count(closureGit(t, mainRoot, "log", "--oneline", "--all"), "\n")
	if post != pre {
		t.Fatalf("edge-commit engine committed despite the serving root's disable: %d -> %d commits", pre, post)
	}
}
