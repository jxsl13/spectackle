package mcpserver

// Pre-push hook recommendation (T-01KYDNN): the hint fires only when
// verify commands exist and no hook runs them; the default path never
// writes into .git; the opt-in install produces a hook that fails when
// the verify gate fails.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jxsl13/spectackle/internal/wt"
)

func TestHookHintFiresAndClears(t *testing.T) {
	root := gitRoot(t)
	if err := os.WriteFile(filepath.Join(root, ".spectackle", "config.yaml"),
		[]byte("schema: v1\nverify:\n  - \"true\"\ngit:\n  enabled: false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, sess := connectRootWithServer(t, root)
	out := callText(t, sess, "state", map[string]any{})
	if !strings.Contains(out, "w hook pre-push absent") {
		t.Fatalf("hint must fire with verify configured and no hook: %q", out)
	}
	// the default path never wrote into .git
	if _, err := os.Stat(wt.HookPath(root)); err == nil {
		t.Fatal("no hook may exist without the explicit opt-in")
	}
	// opt-in install clears the hint
	if err := wt.InstallPrePushHook(root); err != nil {
		t.Fatal(err)
	}
	s.markDirty()
	out = callText(t, sess, "state", map[string]any{})
	if strings.Contains(out, "w hook pre-push absent") {
		t.Fatalf("hint must clear once the hook runs the gate: %q", out)
	}
}

func TestHookHintSilentWithoutVerify(t *testing.T) {
	root := gitRoot(t)
	if err := os.WriteFile(filepath.Join(root, ".spectackle", "config.yaml"),
		[]byte("schema: v1\ngit:\n  enabled: false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, sess := connectRootWithServer(t, root)
	out := callText(t, sess, "state", map[string]any{})
	if strings.Contains(out, "w hook") {
		t.Fatalf("no verify commands, no hint: %q", out)
	}
}

func TestInstallRefusesForeignHook(t *testing.T) {
	root := gitRoot(t)
	p := wt.HookPath(root)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("#!/bin/sh\necho custom\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := wt.InstallPrePushHook(root); !os.IsExist(err) {
		t.Fatalf("foreign hook must not be clobbered: %v", err)
	}
}

// The installed hook enforces the gate for real: with verify=["false"] the
// script exits non-zero, with ["true"] zero. The hook shells to spectackle
// on PATH — the test builds one into a temp dir and prepends it.
func TestInstalledHookRunsVerifyGate(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary")
	}
	bindir := t.TempDir()
	build := exec.Command("go", "build", "-o", filepath.Join(bindir, "spectackle"), "github.com/jxsl13/spectackle/cmd/spectackle")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	root := gitRoot(t)
	if err := os.WriteFile(filepath.Join(root, ".spectackle", "config.yaml"),
		[]byte("schema: v1\nverify:\n  - \"false\"\ngit:\n  enabled: false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := wt.InstallPrePushHook(root); err != nil {
		t.Fatal(err)
	}
	run := func() error {
		c := exec.Command(wt.HookPath(root))
		c.Dir = root
		c.Env = append(os.Environ(), "PATH="+bindir+":"+os.Getenv("PATH"))
		return c.Run()
	}
	if err := run(); err == nil {
		t.Fatal("failing verify must fail the hook")
	}
	if err := os.WriteFile(filepath.Join(root, ".spectackle", "config.yaml"),
		[]byte("schema: v1\nverify:\n  - \"true\"\ngit:\n  enabled: false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run(); err != nil {
		t.Fatalf("passing verify must pass the hook: %v", err)
	}
}
