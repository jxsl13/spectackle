package mcpserver

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jxsl13/spectackle/internal/wt"
)

func gitLogSubjects(t *testing.T, root string) []string {
	t.Helper()
	cmd := exec.Command("git", "log", "--format=%s")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git log: %v", err)
	}
	return strings.Split(strings.TrimSpace(string(out)), "\n")
}

// RED-RUN (T-01KYD94MG, written first): one edge traversal = one commit,
// structured subject, parseable trailers — the journal and git history
// agree by construction, with the driving LLM issuing zero git commands.
func TestEdgeCommitsOnePerCall(t *testing.T) {
	root := gitRoot(t)
	t.Setenv("SPECTACKLE_AGENT", "edge-author")
	sess := connectRoot(t, root)
	before := len(gitLogSubjects(t, root))
	prop := draftID(t, sess, map[string]any{
		"kind": "proposal", "title": "edge commit fixture"})
	callText(t, sess, "move", map[string]any{"id": prop, "to": "approved"})
	callText(t, sess, "move", map[string]any{"id": prop, "to": "rejected", "note": "not worth it"})
	subjects := gitLogSubjects(t, root)
	added := len(subjects) - before
	if added != 3 {
		t.Fatalf("three edges produced %d commits: %v", added, subjects)
	}
	for _, want := range []string{"spectackle(create):", "spectackle(move):", "spectackle(reject):"} {
		found := false
		for _, s := range subjects[:3] {
			if strings.HasPrefix(s, want) {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing %q subject in %v", want, subjects[:3])
		}
	}
	// trailers parse: the newest commit carries the full ID and agent.
	cmd := exec.Command("git", "log", "-1", "--format=%B")
	cmd.Dir = root
	raw, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	for _, tr := range []string{"Spectackle-Ev: reject", "Spectackle-Agent: edge-author", "Spectackle-Item: "} {
		if !strings.Contains(body, tr) {
			t.Fatalf("trailer %q missing in %q", tr, body)
		}
	}
}

// The staged bystander survives: an unrelated staged file is neither
// committed nor unstaged by an edge commit.
func TestEdgeCommitSparesStagedBystander(t *testing.T) {
	root := gitRoot(t)
	t.Setenv("SPECTACKLE_AGENT", "edge-author")
	sess := connectRoot(t, root)
	if err := os.WriteFile(filepath.Join(root, "bystander.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	add := exec.Command("git", "add", "bystander.go")
	add.Dir = root
	if out, err := add.CombinedOutput(); err != nil {
		t.Fatalf("stage: %v %s", err, out)
	}
	draftID(t, sess, map[string]any{"kind": "proposal", "title": "spares the bystander"})
	show := exec.Command("git", "show", "--stat", "--format=", "HEAD")
	show.Dir = root
	out, err := show.Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "bystander.go") {
		t.Fatalf("edge commit swept the staged bystander:\n%s", out)
	}
	diff := exec.Command("git", "diff", "--cached", "--name-only")
	diff.Dir = root
	out, err = diff.Output()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "bystander.go") {
		t.Fatalf("bystander no longer staged: %q", out)
	}
}

// git: commits: off is byte-identical tool behavior with zero commits.
func TestEdgeCommitsOffKnob(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".spectackle"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".spectackle", "config.yaml"),
		[]byte("schema: v1\ngit:\n  commits: \"off\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := wt.InitTestRepo(root); err != nil {
		t.Skipf("git unavailable: %v", err)
	}
	t.Setenv("SPECTACKLE_AGENT", "edge-author")
	sess := connectRoot(t, root)
	before := len(gitLogSubjects(t, root))
	draftID(t, sess, map[string]any{"kind": "proposal", "title": "no commit under off"})
	if after := len(gitLogSubjects(t, root)); after != before {
		t.Fatalf("off knob still committed: %d -> %d", before, after)
	}
}

// A workspace without git succeeds silently — a checkpoint is a bonus,
// never a failure mode.
func TestEdgeCommitsNoGitWorkspace(t *testing.T) {
	root := t.TempDir()
	t.Setenv("SPECTACKLE_AGENT", "edge-author")
	sess := connectRoot(t, root)
	out := callText(t, sess, "draft", map[string]any{"kind": "proposal", "title": "no git here"})
	if strings.Contains(out, "! ") {
		t.Fatalf("no-git draft degraded: %q", out)
	}
}

// Forward-skip is one decision, one commit: draft->active traverses two
// boundaries in one call and yields one commit with From/To trailers.
func TestEdgeCommitForwardSkipIsOne(t *testing.T) {
	root := gitRoot(t)
	t.Setenv("SPECTACKLE_AGENT", "edge-author")
	sess := connectRoot(t, root)
	prop := draftID(t, sess, map[string]any{"kind": "proposal", "title": "skips ahead"})
	before := len(gitLogSubjects(t, root))
	callText(t, sess, "move", map[string]any{"id": prop, "to": "active"})
	subjects := gitLogSubjects(t, root)
	if len(subjects)-before < 1 {
		t.Fatalf("forward skip produced no commit")
	}
	cmd := exec.Command("git", "log", "-1", "--format=%B")
	cmd.Dir = root
	raw, _ := cmd.Output()
	if !strings.Contains(string(raw), "Spectackle-From: draft") || !strings.Contains(string(raw), "Spectackle-To: active") {
		t.Fatalf("skip trailers wrong: %q", raw)
	}
}

// Decision visibility: git log --grep on the full item ID returns that
// item's complete edge history.
func TestEdgeCommitGrepReturnsFullHistory(t *testing.T) {
	root := gitRoot(t)
	t.Setenv("SPECTACKLE_AGENT", "edge-author")
	sess := connectRoot(t, root)
	prop := draftID(t, sess, map[string]any{"kind": "proposal", "title": "grep me"})
	full := fullIDOf(t, root, prop)
	callText(t, sess, "move", map[string]any{"id": prop, "to": "approved"})
	callText(t, sess, "move", map[string]any{"id": prop, "to": "rejected", "note": "history demo"})
	cmd := exec.Command("git", "log", "--grep", full, "--format=%s")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != 3 {
		t.Fatalf("grep returned %d edges, want 3: %v", len(lines), lines)
	}
}

// Concurrency: two servers on one root driving drafts concurrently — one
// commit per call, no index.lock failure surfacing to either caller (the
// coord lock serializes the whole write, commit included).
func TestEdgeCommitConcurrentAgents(t *testing.T) {
	root := gitRoot(t)
	alice, bob := twoAgents(t, root)
	before := len(gitLogSubjects(t, root))
	done := make(chan string, 8)
	for i := 0; i < 4; i++ {
		go func(n int) { done <- callText(t, alice, "draft", map[string]any{"kind": "task", "title": "a-task"}) }(i)
		go func(n int) { done <- callText(t, bob, "draft", map[string]any{"kind": "task", "title": "b-task"}) }(i)
	}
	for i := 0; i < 8; i++ {
		if out := <-done; strings.Contains(out, "! ") && !strings.Contains(out, "! LEASE") {
			t.Fatalf("concurrent draft degraded: %q", out)
		}
	}
	added := len(gitLogSubjects(t, root)) - before
	if added != 8 {
		t.Fatalf("8 concurrent drafts produced %d commits", added)
	}
}

// Cross-verification regressions pinned: the master git opt-out disarms the
// engine, and a swarm task worktree stands down entirely (record-state
// reconciliation belongs to replay — the B-0006 class).
func TestEdgeCommitsHonorMasterGitOptOut(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".spectackle"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".spectackle", "config.yaml"),
		[]byte("schema: v1\ngit:\n  enabled: false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := wt.InitTestRepo(root); err != nil {
		t.Skipf("git unavailable: %v", err)
	}
	t.Setenv("SPECTACKLE_AGENT", "edge-author")
	sess := connectRoot(t, root)
	before := len(gitLogSubjects(t, root))
	draftID(t, sess, map[string]any{"kind": "proposal", "title": "opted out"})
	if after := len(gitLogSubjects(t, root)); after != before {
		t.Fatalf("enabled:false still committed: %d -> %d", before, after)
	}
}

// In-worktree calls make no engine commits: the item branch carries only
// what the submit flow owns, and work.md never git-conflicts at submit.
func TestEdgeCommitsStandDownInsideWorktree(t *testing.T) {
	root := gitRoot(t)
	t.Setenv("SPECTACKLE_AGENT", "edge-author")
	sess := connectRoot(t, root)
	prop := draftID(t, sess, map[string]any{
		"kind": "proposal", "title": "worktree standdown", "targets": []string{"main.go"}})
	callText(t, sess, "move", map[string]any{"id": prop, "to": "approved"})
	out := callText(t, sess, "work", map[string]any{"op": "start", "item": prop})
	if !strings.Contains(out, "wt "+prop+" open ") {
		t.Fatalf("start: %q", out)
	}
	wtRoot := ""
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "wt "+prop+" open ") {
			wtRoot = strings.TrimPrefix(l, "wt "+prop+" open ")
		}
	}
	beforeWt := len(gitLogSubjects(t, wtRoot))
	draftID(t, sess, map[string]any{"kind": "task", "title": "drafted inside the worktree"})
	if after := len(gitLogSubjects(t, wtRoot)); after != beforeWt {
		t.Fatalf("in-worktree call committed on the item branch: %d -> %d", beforeWt, after)
	}
	callText(t, sess, "work", map[string]any{"op": "abort", "item": prop})
}

// An unknown git.commits value fails at load — a typo'd opt-out must never
// silently arm the engine.
func TestEdgeCommitsKnobValidatedAtLoad(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".spectackle"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".spectackle", "config.yaml"),
		[]byte("schema: v1\ngit:\n  commits: disabled\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := New(root); err == nil || !strings.Contains(err.Error(), "git.commits") {
		t.Fatalf("typo'd knob did not fail at load: %v", err)
	}
}
