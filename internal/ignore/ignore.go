// Package ignore answers "is this repo-relative path gitignored" by asking
// git, not by reimplementing gitignore. Negation, directory-only patterns,
// nested .gitignore files, .git/info/exclude and core.excludesFile are more
// semantics than a prefix match gets right, and a subtly wrong matcher would
// silently exclude real code from the graph — worse than the index bloat
// this package exists to fix (issue 26 / T-01KYD8E: a gitignored virtualenv
// and model directory inflated one real checkout's index 13x, and the copy
// inside the virtualenv sorted first and stole the unsuffixed node ID that
// an `applies` rule had anchored to).
package ignore

import (
	"os/exec"
	"strings"
)

// Matcher answers whether a repo-relative slash path is excluded by git,
// backed by one batched git invocation taken at construction time (see New).
// Ignored is then a map lookup, never a subprocess — that matters because a
// workspace walk calls it once per directory entry, and a spawn per entry is
// unacceptable on a tree of any real size.
type Matcher struct {
	dirs  map[string]bool // repo-relative, no trailing slash: this directory is wholly ignored
	files map[string]bool // repo-relative: this exact file is ignored
}

// New builds a Matcher for the working tree rooted at root by running:
//
//	git -C root ls-files --others --ignored --exclude-standard --directory -z
//
// This form was picked over `git check-ignore --stdin` because check-ignore
// only answers for paths handed to it up front, and a matcher must answer
// for paths a walk hasn't reached yet — ls-files needs no candidate list.
// --exclude-standard folds in .gitignore at every level, .git/info/exclude
// and core.excludesFile in the same pass, so none of that semantics is
// reimplemented here. --directory is what keeps a wholly-ignored directory
// (a virtualenv, a model dir) a SINGLE entry instead of git recursing
// through it and hand back one path per file inside: without it, a 1.2GB
// .venv comes back as tens of thousands of lines for no benefit, since every
// one of them shares the same fate. A directory that contains a negated
// file (e.g. `!keep.go`) is not wholly ignored, so git does not collapse
// it — it lists the individually-ignored files instead, and Ignored
// correctly reports the directory itself as NOT ignored (the walk must
// still descend into it to reach the kept file).
//
// Not a git repository, git missing from PATH, or git failing for any other
// reason: New returns a Matcher that ignores nothing. That is a
// degradation to pre-issue-26 behavior, never an error — a workspace that
// isn't a git repo, or lacks git, must walk exactly as it did before this
// package existed.
func New(root string) *Matcher {
	m := &Matcher{dirs: map[string]bool{}, files: map[string]bool{}}
	out, err := exec.Command("git", "-C", root, "ls-files", "--others", "--ignored", "--exclude-standard", "--directory", "-z").Output()
	if err != nil {
		return m // no git, no repo, or any other git failure — see doc above
	}
	for _, entry := range strings.Split(string(out), "\x00") {
		if entry == "" {
			continue
		}
		if dir, ok := strings.CutSuffix(entry, "/"); ok {
			m.dirs[dir] = true
		} else {
			m.files[entry] = true
		}
	}
	return m
}

// Ignored reports whether rel — a repo-relative, slash-separated path, no
// leading "./" — is excluded by git. A nil Matcher answers false, same as
// New's own degraded case, so callers that hold one optionally never need a
// separate nil check.
func (m *Matcher) Ignored(rel string) bool {
	if m == nil || rel == "" {
		return false
	}
	if m.files[rel] || m.dirs[rel] {
		return true
	}
	// A wholly-ignored ancestor directory is never re-listed for the paths
	// beneath it (see New's --directory note above), so climb the path until
	// one matches or we run out of ancestors.
	for {
		i := strings.LastIndexByte(rel, '/')
		if i < 0 {
			return false
		}
		rel = rel[:i]
		if m.dirs[rel] {
			return true
		}
	}
}
