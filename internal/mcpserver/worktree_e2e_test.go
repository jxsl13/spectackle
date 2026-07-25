package mcpserver

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jxsl13/spectackle/internal/journal"
)

// The old worktree test bed only ever exercised a repository whose primary
// branch was literally named main, whose main never advanced while a
// worktree was open, whose worktrees carried no live record state, and whose
// submit ran in the very process that opened the worktree. All four are
// false in real use, and each falsehood hid a defect:
//
//	B-0002  wtItem was set only by in-session work op=start, so a per-call
//	        stdio client (or a restarted server) could never submit.
//	B-0004  MergeMain hardcoded "main", merging a stale ref on repos that
//	        develop on another branch and dying at the fast-forward.
//	B-0005  CommitCode parsed porcelain through a helper that trims the
//	        leading space off the first status line, misreading an
//	        unstaged-only .spectackle change as staged.
//	B-0006  the worktree's live (deliberately uncommitted) .spectackle state
//	        made git refuse the integrate merge when the primary tip touched
//	        the same paths.
//
// TestWorktreeSubmitEndToEndOffPrimaryBranch violates all four conditions at
// once in a single submit, and then asserts the record-only case (B-0005) on
// a second, code-free submit.

// gitAt runs git in dir with the same fixed identity internal/wt uses, so
// the test never depends on the host's git config.
func gitAt(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-C", dir,
		"-c", "user.name=spectackle", "-c", "user.email=spectackle@localhost"}, args...)
	out, err := exec.Command("git", full...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// gitFails reports whether a git invocation exits non-zero (used for
// "this ref does NOT carry that path" assertions).
func gitFails(dir string, args ...string) bool {
	full := append([]string{"-C", dir}, args...)
	return exec.Command("git", full...).Run() != nil
}

// commitPrimary snapshots the primary checkout's whole tree. Worktrees and
// the cache live under .spectackle/{wt,cache}/, which the server-written
// .spectackle/.gitignore excludes, so -A is safe here.
func commitPrimary(t *testing.T, root, msg string) string {
	t.Helper()
	gitAt(t, root, "add", "-A")
	gitAt(t, root, "commit", "-m", msg)
	return gitAt(t, root, "rev-parse", "HEAD")
}

// offPrimaryBranchRepo builds the fixture the old tests never had: a git
// workspace developing on a branch that is NOT called main, with a stale
// `main` branch left behind and diverged. A hardcoded merge target therefore
// resolves to a real — but wrong — ref, exactly as it did in the wild.
// It returns the workspace root and the frozen sha of the stale main.
func offPrimaryBranchRepo(t *testing.T) (root, staleMain string) {
	t.Helper()
	root = gitRoot(t) // scaffolded workspace, git repo, one commit on main
	gitAt(t, root, "checkout", "-b", "develop")
	gitAt(t, root, "checkout", "main")
	if err := os.WriteFile(filepath.Join(root, "stale.go"), []byte("package main // stale\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	staleMain = commitPrimary(t, root, "stale main diverges")
	gitAt(t, root, "checkout", "develop")
	if got := gitAt(t, root, "rev-parse", "--abbrev-ref", "HEAD"); got != "develop" {
		t.Fatalf("primary checkout is on %q, want develop", got)
	}
	return root, staleMain
}

// mainJournal decodes the primary checkout's root journal.
func mainJournal(t *testing.T, root string) []journal.Event {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, ".spectackle", "journal.ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	var events []journal.Event
	for _, l := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if strings.TrimSpace(l) == "" {
			continue
		}
		var e journal.Event
		if err := json.Unmarshal([]byte(l), &e); err != nil {
			t.Fatalf("main journal line %q: %v", l, err)
		}
		events = append(events, e)
	}
	return events
}

func hasEvent(events []journal.Event, ev, id, to string) bool {
	for _, e := range events {
		if e.Ev == ev && e.ID == id && (to == "" || e.To == to) {
			return true
		}
	}
	return false
}

func TestWorktreeSubmitEndToEndOffPrimaryBranch(t *testing.T) {
	root, staleMain := offPrimaryBranchRepo(t)

	// ---- record state on the primary, then commit it, so the worktree's
	// .spectackle files are TRACKED and can genuinely collide later.
	t.Setenv("SPECTACKLE_AGENT", "alice")
	alice := connectRoot(t, root)
	callText(t, alice, "draft", map[string]any{
		"kind": "proposal", "title": "off-branch submit", "targets": []string{"main.go"}})
	callText(t, alice, "move", map[string]any{"id": "P-0001", "to": "approved"})
	base := commitPrimary(t, root, "records: P-0001 approved")

	// ---- open the worktree and make a real code change in it.
	out := callText(t, alice, "work", map[string]any{"op": "start", "item": "P-0001"})
	wtRoot := wtRootOf(t, out, "P-0001")
	if err := os.WriteFile(filepath.Join(wtRoot, "pool.go"), []byte("package main // pooled\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wtRoot, "ok.txt"), []byte("ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// the worktree carries LIVE, uncommitted record state (copyBundles seeds
	// it, the approved->active move rewrites it, CommitCode excludes it).
	for _, rel := range []string{".spectackle/journal.ndjson", ".spectackle/work.md"} {
		if gitAt(t, wtRoot, "diff", "--name-only", "--", rel) == "" {
			t.Fatalf("fixture broken: %s is not live-modified in the worktree", rel)
		}
	}

	// ---- the primary checkout ADVANCES while the worktree is open, and its
	// new commit touches the very same .spectackle journal paths the
	// worktree carries dirty (B-0006's exact collision).
	t.Setenv("SPECTACKLE_AGENT", "bob")
	bob := connectRoot(t, root)
	callText(t, bob, "draft", map[string]any{
		"kind": "proposal", "title": "primary advances underneath", "targets": []string{"other.go"}})
	advanced := commitPrimary(t, root, "primary advances .spectackle")
	if advanced == base {
		t.Fatal("fixture broken: primary did not advance")
	}
	touched := gitAt(t, root, "diff-tree", "--no-commit-id", "--name-only", "-r", advanced)
	if !strings.Contains(touched, ".spectackle/journal.ndjson") {
		t.Fatalf("fixture broken: primary advance did not touch the journal:\n%s", touched)
	}

	// ---- submit from a FRESH server rooted at the worktree, with the same
	// agent identity but a different Server instance than the one that ran
	// work op=start (B-0002's cross-process rebinding).
	t.Setenv("SPECTACKLE_AGENT", "alice")
	fresh := connectRoot(t, wtRoot)
	out = callText(t, fresh, "work", map[string]any{"op": "submit", "item": "P-0001"})
	if !strings.Contains(out, "merged to main") {
		t.Fatalf("cross-process submit off a non-main primary branch failed: %q", out)
	}

	// 1. the PRIMARY branch advanced and carries the code change.
	if gitAt(t, root, "rev-parse", "develop") == advanced {
		t.Fatal("develop did not advance past the pre-submit tip")
	}
	if gitFails(root, "cat-file", "-e", "develop:pool.go") {
		t.Fatal("code change is not on the primary branch develop")
	}
	if _, err := os.Stat(filepath.Join(root, "pool.go")); err != nil {
		t.Fatalf("code change did not reach the primary checkout: %v", err)
	}
	// ...and the stale main was neither merged from nor advanced.
	if got := gitAt(t, root, "rev-parse", "main"); got != staleMain {
		t.Fatalf("stale main moved: %s -> %s", staleMain, got)
	}
	if !gitFails(root, "cat-file", "-e", "main:pool.go") {
		t.Fatal("code change landed on the stale main branch")
	}
	if _, err := os.Stat(filepath.Join(root, "stale.go")); err == nil {
		t.Fatal("the stale main branch was merged into the worktree and reached the primary checkout")
	}

	// 2. the item's record state landed on the PRIMARY side. Only the
	// worktree ever saw approved->active; main had it at approved.
	out = callText(t, fresh, "get", map[string]any{"id": "P-0001"})
	if !strings.Contains(out, "P-0001 proposal active") {
		t.Fatalf("item record state did not land on the primary side: %q", out)
	}

	// 3. neither side's record delta was lost: the worktree's live move
	// event AND the primary's concurrently committed draft are both there.
	events := mainJournal(t, root)
	if !hasEvent(events, journal.EvMove, "P-0001", "active") {
		t.Fatal("the worktree's live record delta (P-0001 -> active) was lost")
	}
	if !hasEvent(events, journal.EvCreate, "P-0002", "") {
		t.Fatal("the primary's concurrent record delta (P-0002 create) was lost")
	}
	if !hasEvent(events, journal.EvSubmit, "P-0001", "") {
		t.Fatal("submit event missing from the primary journal")
	}
	// exactly once — replay is a semantic merge, not a re-append.
	moves := 0
	for _, e := range events {
		if e.Ev == journal.EvMove && e.ID == "P-0001" && e.To == "active" {
			moves++
		}
	}
	if moves != 1 {
		t.Fatalf("P-0001 -> active replayed %d times, want exactly 1", moves)
	}
	if _, err := os.Stat(wtRoot); !os.IsNotExist(err) {
		t.Fatal("worktree not torn down after submit")
	}

	// ---- RECORD-ONLY SUBMIT (B-0005). A second worktree whose ONLY
	// difference from its branch point is bundle state: no code changes at
	// all. CommitCode must recognise that nothing is staged and skip the
	// commit; misreading the unstaged .spectackle delta as staged makes git
	// refuse an empty commit and the whole submit dies.
	callText(t, fresh, "draft", map[string]any{
		"kind": "proposal", "title": "record only", "targets": []string{"main.go"}})
	callText(t, fresh, "move", map[string]any{"id": "P-0003", "to": "approved"})
	out = callText(t, fresh, "work", map[string]any{"op": "start", "item": "P-0003"})
	wt2 := wtRootOf(t, out, "P-0003")
	// no code edit whatsoever — prove it: the only dirt is .spectackle.
	for _, l := range strings.Split(gitAt(t, wt2, "status", "--porcelain"), "\n") {
		if l == "" {
			continue
		}
		if !strings.Contains(l, ".spectackle/") {
			t.Fatalf("fixture broken: record-only worktree has a code change: %q", l)
		}
	}
	tipBefore := gitAt(t, root, "rev-parse", "develop")

	out = callText(t, fresh, "work", map[string]any{"op": "submit", "item": "P-0003"})
	if strings.Contains(out, "! WT E commit") {
		t.Fatalf("record-only submit tried to make an empty commit: %q", out)
	}
	if !strings.Contains(out, "merged to main") {
		t.Fatalf("record-only submit failed: %q", out)
	}
	if got := gitAt(t, root, "rev-parse", "develop"); got != tipBefore {
		t.Fatalf("record-only submit created a code commit: develop %s -> %s", tipBefore, got)
	}
	out = callText(t, fresh, "get", map[string]any{"id": "P-0003"})
	if !strings.Contains(out, "P-0003 proposal active") {
		t.Fatalf("record-only submit did not replay item state onto main: %q", out)
	}
	if !hasEvent(mainJournal(t, root), journal.EvMove, "P-0003", "active") {
		t.Fatal("record-only submit lost the worktree's record delta")
	}
}
