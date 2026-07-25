package mcpserver

// Tool-surface invariants: properties that must hold over the whole
// workspace tree after ANY sequence of mutating tool calls, as opposed to
// the per-tool assertions the rest of this package's tests make.
//
// The one encoded here generalizes B-0003: workAbort passed the item ID to
// journal.Append where every other call site passes the item's context dir,
// and journal.Append -> Root.EnsureScaffold happily scaffolded
// <root>/<item-id>/.spectackle. That bogus directory then read back as a
// real context dir, polluting ContextDirs, the rule cascade and state
// listings. The defect was never the one argument; it was that nothing
// checked WHERE bundles are allowed to exist. This file checks exactly that.

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/jxsl13/spectackle/internal/item"
	"github.com/jxsl13/spectackle/internal/workspace"
	"github.com/jxsl13/spectackle/internal/wt"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// invTrace records the tool sequence as it is executed so a violation can be
// reported with the calls that produced it — a bare boolean failure would
// leave the next occurrence of this class undiagnosable.
type invTrace struct {
	t     *testing.T
	sess  *mcp.ClientSession
	steps []string
}

// call runs one tool through the shared callText helper and appends a dense,
// deterministic one-line record of it to the trace.
func (tr *invTrace) call(name string, args map[string]any) string {
	tr.t.Helper()
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(name)
	for _, k := range keys {
		fmt.Fprintf(&b, " %s=%s", k, invElide(fmt.Sprintf("%v", args[k])))
	}
	tr.steps = append(tr.steps, b.String())
	return callText(tr.t, tr.sess, name, args)
}

// invElide keeps one traced argument to a single short line — a knowledge
// artifact body would otherwise bury the offending path in the failure.
func invElide(s string) string {
	s = strings.ReplaceAll(s, "\n", "\\n")
	if len(s) > 60 {
		return s[:60] + "…(" + fmt.Sprint(len(s)) + "b)"
	}
	return s
}

// note records a non-tool step (a worktree edit, say) in the same trace.
func (tr *invTrace) note(s string) { tr.steps = append(tr.steps, "# "+s) }

func (tr *invTrace) String() string {
	var b strings.Builder
	for i, s := range tr.steps {
		fmt.Fprintf(&b, "  %2d. %s\n", i+1, s)
	}
	return b.String()
}

// invDirs walks root and returns the set of repo-relative directories,
// skipping .git and every .spectackle subtree (whose contents are bundle
// internals, not source directories). "" denotes the root itself.
func invDirs(t *testing.T, root string) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		if p != root && (d.Name() == ".git" || d.Name() == workspace.Dot) {
			return fs.SkipDir
		}
		out[invRel(root, p)] = true
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return out
}

// invBundleParents walks the whole workspace tree and returns every
// repo-relative directory that carries a .spectackle folder — i.e. every
// directory the tool surface has treated as a context dir. The worktree
// subtree is skipped: a linked worktree created by work op=start legitimately
// carries its own copy of the bundles.
func invBundleParents(t *testing.T, root, wtDir string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		if p != root && (d.Name() == ".git" || invUnder(p, wtDir)) {
			return fs.SkipDir
		}
		if d.Name() == workspace.Dot {
			out = append(out, invRel(root, filepath.Dir(p)))
			return fs.SkipDir // bundle internals (cache/, wt/) are not context dirs
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	sort.Strings(out)
	return out
}

// invRel is the repo-relative, slash-separated form of p, with the root
// itself rendered as "" (matching workspace.Root.ContextDirs).
func invRel(root, p string) string {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return p
	}
	rel = filepath.ToSlash(rel)
	if rel == "." {
		return ""
	}
	return rel
}

// invUnder reports whether p is dir or lives beneath it.
func invUnder(p, dir string) bool {
	if dir == "" {
		return false
	}
	rel, err := filepath.Rel(dir, p)
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel))
}

// invScratchRoot builds a git-backed workspace with a handful of real source
// directories, all committed, and returns the root. Its own gitRoot-alike
// exists (rather than reusing swarm_test.go's) because this invariant needs
// nested source dirs present and committed BEFORE the run, so that "legitimate
// context dir" has content to mean.
func invScratchRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, d := range []string{workspace.Dot, "core", "core/engine", "docs"} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(d)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		workspace.Dot + "/config.yaml": "schema: v0\nverify: [\"true\"]\n",
		"main.go":                      "package main\n\nfunc main() {}\n",
		"core/pool.go":                 "package core\n\n// Pool holds buffers.\ntype Pool struct{}\n",
		"core/engine/run.go":           "package engine\n\n// Run drives the loop.\nfunc Run() {}\n",
		"docs/notes.md":                "# notes\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(name)), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := wt.InitTestRepo(root); err != nil {
		t.Skipf("git unavailable: %v", err)
	}
	return root
}

// TestInvariantNoBundleOutsideContextDir is the tool-surface invariant of
// B-0003's whole class: after driving the mutating tool surface end to end,
// EVERY directory in the workspace that carries a .spectackle folder must be
// a directory that already existed as a source directory before the run, or
// the workspace root itself. Nothing the tools do may bring a new
// bundle-carrying directory into existence.
//
// The sharpest tripwire — and B-0003's exact shape — is a bundle parent whose
// name matches item.IDRe: that can only happen when an item ID was passed
// where a context dir was expected.
func TestInvariantNoBundleOutsideContextDir(t *testing.T) {
	root := invScratchRoot(t)
	t.Setenv("SPECTACKLE_AGENT", "inv")

	// the ground truth: source directories as they existed before any tool ran
	before := invDirs(t, root)

	tr := &invTrace{t: t, sess: connectRoot(t, root)}

	// ---- draft: every kind the tool accepts, spread over context dirs ----
	prop := idOfRecord(t, tr.call("draft", map[string]any{"kind": "proposal", "title": "pool buffers per stream",
		"body": "Give each stream its own buffer pool.", "targets": []string{"core/pool.go"}}), "i")
	task := idOfRecord(t, tr.call("draft", map[string]any{"kind": "task", "title": "thread the pool through run",
		"parent": prop, "targets": []string{"core/engine/run.go"}}), "i")
	research := idOfRecord(t, tr.call("draft", map[string]any{"kind": "research", "title": "survey pooling strategies",
		"targets": []string{"docs/notes.md"}}), "i")
	bug := idOfRecord(t, tr.call("draft", map[string]any{"kind": "bug", "title": "pool leaks on reset",
		"targets": []string{"core/pool.go"}}), "i")

	// ---- move: the forward states, plus rejected-with-note and archived ----
	for _, to := range []string{"submitted", "approved", "active", "done"} {
		tr.call("move", map[string]any{"id": research, "to": to})
	}
	tr.call("move", map[string]any{"id": research, "to": "archived", "note": "folded into " + prop})
	tr.call("move", map[string]any{"id": bug, "to": "rejected",
		"note": "not a leak; the reset path reuses the buffer by design"})
	tr.call("move", map[string]any{"id": task, "to": "active"})
	tr.call("move", map[string]any{"id": task, "to": "done"})

	// ---- rule: add, edit, retire ----
	tr.call("rule", map[string]any{"op": "add", "dir": "core", "pattern": "E", "stem": "CORE-POOL",
		"system": "pool allocator", "trigger": "a stream releases a buffer",
		"response": "return the buffer to the per-stream free list within the same call"})
	tr.call("rule", map[string]any{"op": "edit", "id": "CORE-POOL-001", "pattern": "E",
		"system": "pool allocator", "trigger": "a stream releases a buffer",
		"response": "return the buffer to the per-stream free list before the call returns"})
	tr.call("rule", map[string]any{"op": "retire", "id": "CORE-POOL-001"})

	// ---- decide: ask (no elicitation UI -> stays open) then answer ----
	adr := idOfRecord(t, tr.call("decide", map[string]any{"op": "ask", "question": "one pool per stream or one shared pool?",
		"kind": "radio", "options": []string{"per-stream", "shared"}, "item": prop,
		"context": "per-stream trades memory for contention"}), "need")
	tr.call("decide", map[string]any{"op": "answer", "id": adr, "choose": "per-stream",
		"consequences": "higher steady-state memory, no cross-stream lock"})

	// ---- grill ----
	tr.call("grill", map[string]any{"id": prop})

	// ---- lease: claim and release ----
	tr.call("lease", map[string]any{"op": "claim", "paths": []string{"docs"}, "item": prop})
	tr.call("lease", map[string]any{"op": "release", "paths": []string{"docs"}})

	// ---- work: start then abort (B-0003's own call path) ----
	tr.call("move", map[string]any{"id": prop, "to": "submitted"})
	tr.call("move", map[string]any{"id": prop, "to": "approved"})
	if out := tr.call("work", map[string]any{"op": "start", "item": prop}); !strings.Contains(out, "wt "+prop+" open ") {
		t.Fatalf("work start (abort leg): %q\ntrace:\n%s", out, tr)
	}
	if out := tr.call("work", map[string]any{"op": "abort", "item": prop}); !strings.Contains(out, "aborted") {
		t.Fatalf("work abort: %q\ntrace:\n%s", out, tr)
	}

	// ---- work: start then submit ----
	out := tr.call("work", map[string]any{"op": "start", "item": prop})
	wtRoot := ""
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "wt "+prop+" open ") {
			wtRoot = strings.TrimPrefix(l, "wt "+prop+" open ")
		}
	}
	if wtRoot == "" {
		t.Fatalf("work start (submit leg): %q\ntrace:\n%s", out, tr)
	}
	if err := os.WriteFile(filepath.Join(wtRoot, "core", "stream.go"),
		[]byte("package core\n\n// Stream owns a Pool.\ntype Stream struct{ P Pool }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tr.note("write " + filepath.Join(wtRoot, "core", "stream.go"))
	if out := tr.call("work", map[string]any{"op": "submit"}); !strings.Contains(out, "merged to main") {
		t.Fatalf("work submit: %q\ntrace:\n%s", out, tr)
	}

	// ---- compact ----
	tr.call("compact", map[string]any{"apply": true})

	// ---- knowledge: export this workspace, then apply the artifact back ----
	exp := tr.call("knowledge", map[string]any{"op": "export"})
	if body, _, ok := strings.Cut(exp, "\nok export"); ok {
		tr.call("knowledge", map[string]any{"op": "apply", "body": body})
	} else {
		t.Logf("knowledge export produced no artifact body, apply skipped: %q", exp)
	}

	// ---- THE INVARIANT ----
	//
	// Re-read the config so WtDir() reflects any configured worktrees dir;
	// the worktree subtree is legitimately allowed its own bundles.
	ws, err := workspace.LoadRoot(root)
	if err != nil {
		t.Fatalf("reopen workspace: %v", err)
	}
	wtDir := ws.WtDir()

	parents := invBundleParents(t, root, wtDir)
	// guard against a vacuous pass: the run must have produced bundles in at
	// least the root (every draft/move/journal write) and in core (rule
	// op=add dir=core). A walk that finds neither is broken, not clean.
	for _, want := range []string{"", "core"} {
		found := false
		for _, got := range parents {
			if got == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("walk found no bundle in %q — the invariant is not looking at anything; saw %q\ntrace:\n%s",
				want, parents, tr)
		}
	}

	for _, ctx := range parents {
		if before[ctx] {
			continue // a source directory that existed before the run: legitimate
		}
		// name the shape precisely — an item ID standing in for a context dir
		// is B-0003 exactly, and the cheapest signature of the whole class.
		why := "directory did not exist as a source directory before the run"
		for _, seg := range strings.Split(ctx, "/") {
			if item.IDRe.MatchString(seg) {
				why = "path segment " + seg + " is an item ID (item ID passed where a context dir belongs — B-0003's shape)"
				break
			}
		}
		t.Errorf("bundle outside a legitimate context dir: %s/%s\n  %s\nproduced by this tool sequence:\n%s",
			filepath.Join(root, filepath.FromSlash(ctx)), workspace.Dot, why, tr)
	}
}
