package mcpserver

// Version resolution (B-01KYJ66TWW): a go-installed binary must report its
// real module version, not the hardcoded dev default — the ldflags tag
// wins when set, the embedded build info fills in otherwise.

import (
	"strings"
	"testing"
)

func TestResolvedVersionLdflagsWins(t *testing.T) {
	old := Version
	defer func() { Version = old }()
	Version = "v9.9.9-test"
	if got := ResolvedVersion(); got != "v9.9.9-test" {
		t.Fatalf("ldflags tag must win: %q", got)
	}
}

func TestResolvedVersionDevFallsToBuildInfo(t *testing.T) {
	old := Version
	defer func() { Version = old }()
	Version = "dev"
	got := ResolvedVersion()
	// In-tree test builds embed "(devel)" or no main version — the
	// fallback then honestly returns the dev default; a module-installed
	// build returns its real tag. Either way it never fabricates.
	if got != "dev" && !strings.HasPrefix(got, "v") && got != "(devel)" {
		t.Fatalf("unexpected resolution: %q", got)
	}
}

// The state surface prints the RESOLVED version too (cross-val-version
// found it reading the raw var — the same lie on a fifth surface).
func TestStateVersionLineResolved(t *testing.T) {
	old := Version
	defer func() { Version = old }()
	Version = "v9.9.9-test"
	root := t.TempDir()
	sess := connectRoot(t, root)
	out := callText(t, sess, "state", map[string]any{})
	if !strings.Contains(out, "ok spectackle v9.9.9-test") {
		t.Fatalf("state must print the resolved version: %q", out)
	}
}
