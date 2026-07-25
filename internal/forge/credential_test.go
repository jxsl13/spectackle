package forge

import (
	"fmt"
	"strings"
	"testing"
)

// stubRunner replaces execCredential so these tests never shell out to a
// real `git credential fill` — there is nothing on a test machine for that
// to authenticate against, and it could hang waiting on an interactive
// prompt.
func stubRunner(t *testing.T, output string, err error) (CredentialRunner, *[]string) {
	t.Helper()
	var calls []string
	return func(input string) (string, error) {
		calls = append(calls, input)
		return output, err
	}, &calls
}

func TestTokenPrefersEnvironmentOverHelper(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "env-token")
	t.Setenv("GH_TOKEN", "")
	runner, calls := stubRunner(t, "", fmt.Errorf("helper must not be called"))

	tok, err := Token("github.com", runner)
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if tok != "env-token" {
		t.Fatalf("Token = %q, want env-token", tok)
	}
	if len(*calls) != 0 {
		t.Fatalf("helper was invoked despite GITHUB_TOKEN being set: %v", *calls)
	}
}

func TestTokenFallsBackToGHToken(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "gh-token")
	runner, calls := stubRunner(t, "", fmt.Errorf("helper must not be called"))

	tok, err := Token("github.com", runner)
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if tok != "gh-token" {
		t.Fatalf("Token = %q, want gh-token", tok)
	}
	if len(*calls) != 0 {
		t.Fatalf("helper was invoked despite GH_TOKEN being set: %v", *calls)
	}
}

func TestTokenFallsBackToCredentialHelper(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")
	runner, calls := stubRunner(t, "protocol=https\nhost=github.com\nusername=x\npassword=helper-token\n", nil)

	tok, err := Token("github.com", runner)
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if tok != "helper-token" {
		t.Fatalf("Token = %q, want helper-token", tok)
	}
	if len(*calls) != 1 {
		t.Fatalf("expected exactly one helper call, got %d", len(*calls))
	}
	if !strings.Contains((*calls)[0], "host=github.com") {
		t.Fatalf("helper input missing host: %q", (*calls)[0])
	}
}

func TestTokenErrorsWhenHelperHasNoPassword(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")
	runner, _ := stubRunner(t, "protocol=https\nhost=github.com\n", nil)

	if _, err := Token("github.com", runner); err == nil {
		t.Fatal("expected an error when the helper returns no password field")
	}
}
