package mcpserver

// Never-destroy worktree guard (B-01KYH8JBB): a dirty orphaned worktree
// refuses adoption naming holder and recovery, files intact; force
// discards; a clean tree adopts; the holder itself resumes untouched; and
// submit's refusal names the holder instead of disagreeing with status.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func startWorkFixture(t *testing.T) (root, id string) {
	t.Helper()
	root = gitRoot(t)
	writeOfflineGitConfig(t, root)
	// a 1s agent TTL lets the orphan-adoption paths become reachable: the
	// holder's in-process heartbeat reads dead after the sleep below.
	raw, err := os.ReadFile(filepath.Join(root, ".spectackle", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".spectackle", "config.yaml"),
		append(raw, []byte("swarm:\n  agent_ttl: 1\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SPECTACKLE_AGENT", "holder-a")
	srv, sess := connectRootWithServer(t, root)
	id = draftID(t, sess, map[string]any{
		"kind": "task", "title": "wipe guard fixture", "body": ambFixturePad})
	callText(t, sess, "move", map[string]any{"id": id, "to": "approved"})
	out := callText(t, sess, "work", map[string]any{"op": "start", "item": id})
	if !strings.Contains(out, "wt ") {
		t.Fatalf("work start failed: %q", out)
	}
	// Close session AND server: the in-process SERVER keeps heartbeating
	// holder-a on a ticker regardless of the client session, which raced
	// the 1s TTL and flaked the orphan-adoption paths (the holder
	// flickered alive across three fixture drafts before this).
	_ = sess.Close()
	_ = srv.Close()
	return root, id
}

func TestDirtyOrphanRefusesAndPreserves(t *testing.T) {
	root, id := startWorkFixture(t)
	t.Setenv("SPECTACKLE_AGENT", "holder-a")
	srv2, sess := connectRootWithServer(t, root)
	status := callText(t, sess, "work", map[string]any{"op": "status"})
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
	status := callText(t, sess, "work", map[string]any{"op": "status"})
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
