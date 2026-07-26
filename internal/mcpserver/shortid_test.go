package mcpserver

// Short display IDs on human-facing git surfaces (T-01KYG0ZX): branches,
// PR titles, and commit subjects carry kind + 13 body characters; machine
// trailers and record files keep the full form. In git-managed FILES the
// form does not matter (user clarification 2026-07-27) — only the git/
// GitHub surfaces shorten.

import (
	"os/exec"
	"strings"
	"testing"
)

func TestShortDisplayID(t *testing.T) {
	full := "T-01KYFXDC6KFNCB66W8JVHTZZNK"
	want := "T-01KYFXDC6KFNC"
	if got := shortDisplayID(full); got != want {
		t.Fatalf("shortDisplayID(%s) = %s, want %s", full, got, want)
	}
	// legacy/short-shaped IDs pass through
	for _, id := range []string{"P-0007", "ADR-0013", "nodash"} {
		if got := shortDisplayID(id); got != id {
			t.Fatalf("shortDisplayID(%s) = %s, want passthrough", id, got)
		}
	}
}

func TestTaskBranchShortForm(t *testing.T) {
	full := "T-01KYFXDC6KFNCB66W8JVHTZZNK"
	got := taskBranch(full)
	if got != "spectackle/T-01KYFXDC6KFNC" {
		t.Fatalf("taskBranch = %s", got)
	}
	if strings.Contains(got, full) {
		t.Fatal("branch must not carry the full ULID")
	}
}

// The subject/trailer split in one real edge commit: subject carries the
// short display form, the Spectackle-Item trailer the full ID.
func TestEdgeCommitSubjectShortTrailerFull(t *testing.T) {
	root := gitRoot(t)
	sess := connectRoot(t, root)
	prop := draftID(t, sess, map[string]any{
		"kind": "proposal", "title": "short subject fixture"})
	full := fullIDOf(t, root, prop)
	cmd := exec.Command("git", "log", "-1", "--format=%s%n%b")
	cmd.Dir = root
	raw, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	subject, body, _ := strings.Cut(string(raw), "\n")
	if strings.Contains(subject, full) {
		t.Fatalf("subject carries the full ULID: %q", subject)
	}
	if !strings.Contains(subject, shortDisplayID(full)) {
		t.Fatalf("subject missing the short form %s: %q", shortDisplayID(full), subject)
	}
	if !strings.Contains(body, "Spectackle-Item: "+full) {
		t.Fatalf("trailer must keep the FULL ID: %q", body)
	}
}
