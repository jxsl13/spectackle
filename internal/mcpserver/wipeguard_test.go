package mcpserver

// Never-destroy worktree guard (B-01KYH8JBB): a dirty orphaned worktree
// refuses adoption naming holder and recovery, files intact; force
// discards; a clean tree adopts; the holder itself resumes untouched; and
// submit's refusal names the holder instead of disagreeing with status.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// startWorkFixtureLive preps holder-a with an open worktree and returns
// the STILL-RUNNING server+session — the holder stays alive on its
// heartbeat ticker until the caller closes both. shortTTL=true shrinks
// the agent TTL to 1s so orphan-adoption paths become reachable after a
// >1s sleep; the live-holder test keeps the default TTL, under which the
// running server cannot flicker dead between checks.
func startWorkFixtureLive(t *testing.T, shortTTL bool) (root, id string, srv *Server, sess *mcp.ClientSession) {
	t.Helper()
	root = gitRoot(t)
	writeOfflineGitConfig(t, root)
	if shortTTL {
		raw, err := os.ReadFile(filepath.Join(root, ".spectackle", "config.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, ".spectackle", "config.yaml"),
			append(raw, []byte("swarm:\n  agent_ttl: 1\n")...), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("SPECTACKLE_AGENT", "holder-a")
	srv, sess = connectRootWithServer(t, root)
	id = draftID(t, sess, map[string]any{
		"kind": "task", "title": "wipe guard fixture", "body": ambFixturePad})
	callText(t, sess, "move", map[string]any{"id": id, "to": "approved"})
	out := callText(t, sess, "work", map[string]any{"op": "start", "item": id})
	if !strings.Contains(out, "wt ") {
		t.Fatalf("work start failed: %q", out)
	}
	return root, id, srv, sess
}

func startWorkFixture(t *testing.T) (root, id string) {
	t.Helper()
	root, id, srv, sess := startWorkFixtureLive(t, true)
	// Close session AND server: the in-process SERVER keeps heartbeating
	// holder-a on a ticker regardless of the client session, which raced
	// the 1s TTL and flaked the orphan-adoption paths (the holder
	// flickered alive across three fixture drafts before this).
	_ = sess.Close()
	_ = srv.Close()
	return root, id
}

// wipeguardRoot extracts the worktree root from a work op=status render.
func wipeguardRoot(t *testing.T, status string) string {
	t.Helper()
	wtRoot := ""
	for _, l := range strings.Split(status, "\n") {
		if strings.HasPrefix(l, "wt ") && strings.Contains(l, " open ") {
			f := strings.Fields(l)
			wtRoot = f[len(f)-1]
		}
	}
	if wtRoot == "" {
		t.Fatalf("no worktree root in %q", status)
	}
	return wtRoot
}

func TestDirtyOrphanRefusesAndPreserves(t *testing.T) {
	root, id := startWorkFixture(t)
	t.Setenv("SPECTACKLE_AGENT", "holder-a")
	srv2, sess := connectRootWithServer(t, root)
	wtRoot := wipeguardRoot(t, callText(t, sess, "work", map[string]any{"op": "status"}))
	precious := filepath.Join(wtRoot, "precious.go")
	if err := os.WriteFile(precious, []byte("package main\n\n// uncommitted work that must survive.\nfunc precious() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// a rotated identity tries to start: refuse, name holder + recovery,
	// leave the file (the holder reads dead after the TTL — close session
	// AND server, both heartbeat)
	_ = sess.Close()
	_ = srv2.Close()
	time.Sleep(1500 * time.Millisecond)
	t.Setenv("SPECTACKLE_AGENT", "rotated-b")
	rotated := connectRoot(t, root)
	out := callText(t, rotated, "work", map[string]any{"op": "start", "item": id})
	if !strings.Contains(out, "uncommitted file") || !strings.Contains(out, "holder-a") || !strings.Contains(out, "force=true") {
		t.Fatalf("dirty adoption must refuse naming holder and recovery: %q", out)
	}
	if _, err := os.Stat(precious); err != nil {
		t.Fatal("uncommitted file destroyed by a refused start")
	}

	// submit from the rotated identity: names the holder, no disagreement
	out = callText(t, rotated, "work", map[string]any{"op": "submit", "item": id})
	if !strings.Contains(out, "held by holder-a") || !strings.Contains(out, "SPECTACKLE_AGENT=holder-a") {
		t.Fatalf("submit must name the holder and the reattach fix: %q", out)
	}

	// force discards deliberately
	out = callText(t, rotated, "work", map[string]any{"op": "start", "item": id, "force": true})
	if !strings.Contains(out, "wt ") {
		t.Fatalf("forced start must adopt: %q", out)
	}
	if _, err := os.Stat(precious); err == nil {
		t.Fatal("force must actually discard the old tree")
	}
}

func TestHolderResumesUntouched(t *testing.T) {
	root, id := startWorkFixture(t)
	t.Setenv("SPECTACKLE_AGENT", "holder-a")
	_, sess := connectRootWithServer(t, root)
	wtRoot := wipeguardRoot(t, callText(t, sess, "work", map[string]any{"op": "status"}))
	precious := filepath.Join(wtRoot, "precious.go")
	if err := os.WriteFile(precious, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := callText(t, sess, "work", map[string]any{"op": "start", "item": id})
	if !strings.Contains(out, "resumed") {
		t.Fatalf("holder restart must resume, not recreate: %q", out)
	}
	if _, err := os.Stat(precious); err != nil {
		t.Fatal("holder resume destroyed its own uncommitted work")
	}
}

func TestCleanOrphanAdopts(t *testing.T) {
	root, id := startWorkFixture(t)
	time.Sleep(1500 * time.Millisecond)
	_ = root
	t.Setenv("SPECTACKLE_AGENT", "rotated-b")
	rotated := connectRoot(t, root)
	out := callText(t, rotated, "work", map[string]any{"op": "start", "item": id})
	if !strings.Contains(out, "wt ") {
		t.Fatalf("clean orphan must adopt without force: %q", out)
	}
}

// Staged-but-uncommitted work is as precious as unstaged: `git add`ed
// files are invisible to both the unstaged diff and --others, so before
// the --cached leg they read clean and died with the tree (H1).
func TestStagedOrphanRefusesAndPreserves(t *testing.T) {
	root, id, srv, sess := startWorkFixtureLive(t, true)
	wtRoot := wipeguardRoot(t, callText(t, sess, "work", map[string]any{"op": "status"}))
	precious := filepath.Join(wtRoot, "precious.go")
	if err := os.WriteFile(precious, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	add := exec.Command("git", "-C", wtRoot, "add", "precious.go")
	if out, err := add.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v %s", err, out)
	}
	_ = sess.Close()
	_ = srv.Close()
	time.Sleep(1500 * time.Millisecond)
	t.Setenv("SPECTACKLE_AGENT", "rotated-b")
	rotated := connectRoot(t, root)
	out := callText(t, rotated, "work", map[string]any{"op": "start", "item": id})
	if !strings.Contains(out, "uncommitted file") || !strings.Contains(out, "force=true") {
		t.Fatalf("staged-only dirt must refuse adoption: %q", out)
	}
	if _, err := os.Stat(precious); err != nil {
		t.Fatal("staged uncommitted file destroyed by a refused start")
	}
}

// A worktree whose dirt cannot be READ is not clean: broken metadata must
// refuse instead of failing open into removal (H2).
func TestUnreadableOrphanRefuses(t *testing.T) {
	root, id, srv, sess := startWorkFixtureLive(t, true)
	wtRoot := wipeguardRoot(t, callText(t, sess, "work", map[string]any{"op": "status"}))
	precious := filepath.Join(wtRoot, "precious.go")
	if err := os.WriteFile(precious, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = sess.Close()
	_ = srv.Close()
	// sever the worktree's gitdir link — DirtyFiles now errors
	if err := os.WriteFile(filepath.Join(wtRoot, ".git"), []byte("gitdir: /nonexistent\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1500 * time.Millisecond)
	t.Setenv("SPECTACKLE_AGENT", "rotated-b")
	rotated := connectRoot(t, root)
	out := callText(t, rotated, "work", map[string]any{"op": "start", "item": id})
	if !strings.Contains(out, "unreadable") || !strings.Contains(out, "force=true") {
		t.Fatalf("unreadable worktree must refuse, not fail open: %q", out)
	}
	if _, err := os.Stat(precious); err != nil {
		t.Fatal("file destroyed through the fail-open path")
	}
}

// Foreign abort on a dead holder's dirty tree is the same loss class as a
// silent start one habit away (B-01KYHC7APA): it refuses naming count,
// holder, and the forced form; force discards; the holder itself may
// always abort its own dirty tree — abort IS its explicit discard.
func TestForeignDeadAbortRefusesAndPreserves(t *testing.T) {
	root, id, srv, sess := startWorkFixtureLive(t, true)
	wtRoot := wipeguardRoot(t, callText(t, sess, "work", map[string]any{"op": "status"}))
	precious := filepath.Join(wtRoot, "precious.go")
	if err := os.WriteFile(precious, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = sess.Close()
	_ = srv.Close()
	time.Sleep(1500 * time.Millisecond)
	t.Setenv("SPECTACKLE_AGENT", "rotated-b")
	rotated := connectRoot(t, root)
	out := callText(t, rotated, "work", map[string]any{"op": "abort", "item": id})
	if !strings.Contains(out, "uncommitted file") || !strings.Contains(out, "holder-a") || !strings.Contains(out, "op=abort force=true") {
		t.Fatalf("foreign abort on a dirty dead tree must refuse naming holder and recovery: %q", out)
	}
	if _, err := os.Stat(precious); err != nil {
		t.Fatal("uncommitted file destroyed by a refused abort")
	}
	out = callText(t, rotated, "work", map[string]any{"op": "abort", "item": id, "force": true})
	if strings.Contains(out, "! WT E") {
		t.Fatalf("forced abort must tear down: %q", out)
	}
	if _, err := os.Stat(precious); err == nil {
		t.Fatal("forced abort must actually discard the tree")
	}
}

func TestHolderSelfAbortSucceedsDirty(t *testing.T) {
	_, id, srv, sess := startWorkFixtureLive(t, true)
	defer func() { _ = sess.Close(); _ = srv.Close() }()
	wtRoot := wipeguardRoot(t, callText(t, sess, "work", map[string]any{"op": "status"}))
	if err := os.WriteFile(filepath.Join(wtRoot, "precious.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := callText(t, sess, "work", map[string]any{"op": "abort", "item": id})
	if strings.Contains(out, "! WT E") {
		t.Fatalf("the holder's own abort is its explicit discard and must succeed dirty: %q", out)
	}
}

// Path (b): a DIFFERENT identity against a LIVE holder refuses at op=start
// naming the reattach env var — previously only abort/submit pinned this.
func TestLiveHolderStartRefuses(t *testing.T) {
	root, id, srv, sess := startWorkFixtureLive(t, false)
	defer func() { _ = sess.Close(); _ = srv.Close() }()
	t.Setenv("SPECTACKLE_AGENT", "intruder-c")
	intruder := connectRoot(t, root)
	out := callText(t, intruder, "work", map[string]any{"op": "start", "item": id})
	if !strings.Contains(out, "live agent holder-a") || !strings.Contains(out, "SPECTACKLE_AGENT=holder-a") {
		t.Fatalf("live holder must refuse a foreign start naming the reattach: %q", out)
	}
}
