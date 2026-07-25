package forge

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// CredentialRunner executes `git credential fill`, writing input to stdin
// and returning its stdout. Production uses execCredential; tests inject a
// stub (see Token) so credential resolution never shells out to a real git
// process — there is nothing for a unit test to authenticate against, and
// a real invocation could hang waiting on a prompt or leak a developer's
// actual token into test output.
type CredentialRunner func(input string) (output string, err error)

// execCredential is the real CredentialRunner: it shells out to git's own
// credential helper. This is what makes the GitHub forge work with ZERO new
// configuration for anyone who can already push to the remote — no gh
// binary, no second auth system, no token anyone has to go set up by hand.
func execCredential(input string) (string, error) {
	cmd := exec.Command("git", "credential", "fill")
	cmd.Stdin = strings.NewReader(input)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("forge: git credential fill: %w", err)
	}
	return string(out), nil
}

// Token resolves credentials for host, first hit wins: GITHUB_TOKEN, then
// GH_TOKEN from the environment, then git's credential helper (runner; nil
// uses execCredential). The environment always wins over the helper — it
// is the explicit, no-prompt override CI sets — and the helper is the
// fallback that needs no new configuration from anyone who can already
// push to the remote.
func Token(host string, runner CredentialRunner) (string, error) {
	if t := os.Getenv("GITHUB_TOKEN"); t != "" {
		return t, nil
	}
	if t := os.Getenv("GH_TOKEN"); t != "" {
		return t, nil
	}
	if runner == nil {
		runner = execCredential
	}
	// `git credential fill` reads key=value lines terminated by a blank
	// line and writes the resolved fields (including password=) the same
	// way back on stdout.
	input := fmt.Sprintf("protocol=https\nhost=%s\n\n", host)
	out, err := runner(input)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(out, "\n") {
		if v, ok := strings.CutPrefix(line, "password="); ok {
			return strings.TrimSpace(v), nil
		}
	}
	return "", fmt.Errorf("forge: git credential fill returned no password for host %q", host)
}
