// Package workspace implements root detection and the .spectackle folder
// contract: every file the server writes lives inside a .spectackle/ folder
// (root or nested context dirs); the rest of the workspace is never touched.
//
// Root detection order: walk up from the start dir for .spectackle/config.yaml
// (the root marker — nested context dirs also have .spectackle/ folders, so the
// folder alone is ambiguous), then for .git, then fall back to the -root flag.
package workspace

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Dot is the folder name every server write is confined to.
const Dot = ".spectackle"

// SchemaStamp is injected into every server-written file's frontmatter.
// It marks the file format of a pre-1.0 codebase: the format may break at any
// time, the stamp changes with it, and there is no migration — an unknown
// stamp is a tool error ("regenerate"), caches simply rebuild.
const SchemaStamp = "v0"

// Config is .spectackle/config.yaml (root only).
type Config struct {
	Schema        string      `yaml:"schema"`
	Langs         []string    `yaml:"langs"`
	Ignore        []string    `yaml:"ignore"`       // glob patterns, repo-relative slash paths (see SkipDir)
	IgnoreRegex   []string    `yaml:"ignore_regex"` // RE2 patterns, matched against repo-relative slash paths
	BudgetDefault int         `yaml:"budget_default"`
	Compact       CompactCfg  `yaml:"compact"`
	Verify        []string    `yaml:"verify"` // shell commands gating work-submit (e.g. "make test")
	Swarm         SwarmCfg    `yaml:"swarm"`
	WorktreesDir  string      `yaml:"worktrees_dir"` // override for .spectackle/wt (abs or root-relative)
	Feedback      FeedbackCfg `yaml:"feedback"`
}

// SwarmCfg tunes multi-agent coordination.
type SwarmCfg struct {
	LeaseTTL int `yaml:"lease_ttl"` // seconds a scope lease lives without refresh
	AgentTTL int `yaml:"agent_ttl"` // seconds without heartbeat before an agent counts as gone
}

// FeedbackCfg tunes the SDD orchestration v2 feedback loop (see
// internal/lifecycle: Move's done->active reopen counter and Escalate).
type FeedbackCfg struct {
	MaxRounds int    `yaml:"max_rounds"` // reopen attempts before an item escalates to blocked
	Grill     string `yaml:"grill"`      // optional shell command that produces grill feedback on reopen
}

// CompactCfg holds the compact-due thresholds surfaced by `check`.
type CompactCfg struct {
	JournalMax int `yaml:"journal_max"` // journal events since last compact
	DoneMax    int `yaml:"done_max"`    // done-but-unarchived items
}

func defaultConfig() Config {
	return Config{
		Schema:        SchemaStamp,
		Langs:         []string{"go"},
		Ignore:        []string{".git/**", "bin/**"},
		BudgetDefault: 2000,
		Compact:       CompactCfg{JournalMax: 500, DoneMax: 8},
		Swarm:         SwarmCfg{LeaseTTL: 600, AgentTTL: 900},
		Feedback:      FeedbackCfg{MaxRounds: 3},
	}
}

// Root is a detected workspace.
type Root struct {
	Dir   string // absolute path
	Agent string // agent identity writing through this workspace ("" outside swarm contexts)
	Cfg   Config
}

// Detect finds the workspace root starting at start (usually the cwd).
func Detect(start, flagRoot string) (Root, error) {
	abs, err := filepath.Abs(start)
	if err != nil {
		return Root{}, err
	}
	if d, ok := walkUp(abs, func(dir string) bool {
		return fileExists(filepath.Join(dir, Dot, "config.yaml"))
	}); ok {
		return load(d)
	}
	if d, ok := walkUp(abs, func(dir string) bool {
		return dirExists(filepath.Join(dir, ".git"))
	}); ok {
		return load(d)
	}
	if flagRoot != "" {
		fr, err := filepath.Abs(flagRoot)
		if err != nil {
			return Root{}, err
		}
		return load(fr)
	}
	return Root{}, fmt.Errorf("workspace: no %s/config.yaml or .git found above %s (pass -root)", Dot, abs)
}

func walkUp(start string, ok func(string) bool) (string, bool) {
	d := start
	for {
		if ok(d) {
			return d, true
		}
		parent := filepath.Dir(d)
		if parent == d {
			return "", false
		}
		d = parent
	}
}

func load(dir string) (Root, error) {
	r := Root{Dir: dir, Cfg: defaultConfig()}
	raw, err := os.ReadFile(filepath.Join(dir, Dot, "config.yaml"))
	if os.IsNotExist(err) {
		return r, nil
	}
	if err != nil {
		return Root{}, err
	}
	if err := yaml.Unmarshal(raw, &r.Cfg); err != nil {
		return Root{}, fmt.Errorf("workspace: config.yaml: %w", err)
	}
	if r.Cfg.Schema != "" && r.Cfg.Schema != SchemaStamp {
		return Root{}, fmt.Errorf("workspace: config schema %q != %q — regenerate the file, there is no migration", r.Cfg.Schema, SchemaStamp)
	}
	for _, pat := range r.Cfg.IgnoreRegex {
		if _, err := regexp.Compile(pat); err != nil {
			return Root{}, fmt.Errorf("workspace: config.yaml: ignore_regex %q: %w", pat, err)
		}
	}
	if r.Cfg.BudgetDefault == 0 {
		r.Cfg.BudgetDefault = 2000
	}
	if r.Cfg.Compact.JournalMax == 0 {
		r.Cfg.Compact.JournalMax = 500
	}
	if r.Cfg.Compact.DoneMax == 0 {
		r.Cfg.Compact.DoneMax = 8
	}
	if r.Cfg.Swarm.LeaseTTL == 0 {
		r.Cfg.Swarm.LeaseTTL = 600
	}
	if r.Cfg.Swarm.AgentTTL == 0 {
		r.Cfg.Swarm.AgentTTL = 900
	}
	if r.Cfg.Feedback.MaxRounds == 0 {
		r.Cfg.Feedback.MaxRounds = 3
	}
	return r, nil
}

// LoadRoot reads dir/.spectackle/config.yaml (if present) and returns a Root
// scoped to dir, without walking up to find the workspace root. Callers that
// already know the root — e.g. a package that only receives a bare root
// string, like spec.Load — use this to get a fully configured Root (in
// particular one whose SkipDir honors the workspace's Config.Ignore /
// IgnoreRegex) without duplicating Detect's walk-up logic.
func LoadRoot(dir string) (Root, error) {
	return load(dir)
}

// SpectackleDir maps a repo-relative context dir ("" = root) to the absolute
// path of its .spectackle folder.
func (r Root) SpectackleDir(ctx string) string {
	return filepath.Join(r.Dir, filepath.FromSlash(ctx), Dot)
}

// SpecPath / WorkPath / JournalPath locate the three bundle files of a
// context dir, repo-relative ("" = root).
func (r Root) SpecPath(ctx string) string { return filepath.Join(r.SpectackleDir(ctx), "spec.md") }
func (r Root) WorkPath(ctx string) string { return filepath.Join(r.SpectackleDir(ctx), "work.md") }
func (r Root) JournalPath(ctx string) string {
	return filepath.Join(r.SpectackleDir(ctx), "journal.ndjson")
}

// AnchorsPath is root-only (workspace-wide bindings).
func (r Root) AnchorsPath() string { return filepath.Join(r.Dir, Dot, "anchors.tsv") }

// CacheDir is root-only and excluded from git by the server-written .gitignore.
func (r Root) CacheDir() string { return filepath.Join(r.Dir, Dot, "cache") }

// CoordPath is the shared multi-agent coordination DB (main repo only).
func (r Root) CoordPath() string { return filepath.Join(r.CacheDir(), "coord.db") }

// WtDir is where agent worktrees live. NOT under cache/ — cache is
// disposable, in-flight work is not.
func (r Root) WtDir() string {
	if d := r.Cfg.WorktreesDir; d != "" {
		if filepath.IsAbs(d) {
			return d
		}
		return filepath.Join(r.Dir, d)
	}
	return filepath.Join(r.Dir, Dot, "wt")
}

// defaultSkipNames are directory basenames every workspace walk skips
// unconditionally: VCS metadata, dependency/build output, and spectackle's
// own state folder. This is the generic, harness-independent replacement for
// what used to be a hardcoded '.claude' skip — agent worktrees under
// .claude/worktrees/<name> are ordinary git linked worktrees, so they are
// now caught by IsNestedGitBoundary instead of by name.
var defaultSkipNames = map[string]bool{
	".git": true, "node_modules": true, "testdata": true,
	"bin": true, "vendor": true, Dot: true,
}

// DefaultSkipName reports whether name is one of the built-in directory
// basenames every workspace walk skips unconditionally, regardless of
// config.yaml. Exposed so packages that cannot hold a full Root (e.g.
// internal/index, which is handed a bare root string) still share the same
// built-in set as Root.SkipDir.
func DefaultSkipName(name string) bool { return defaultSkipNames[name] }

// IsNestedGitBoundary reports whether dir is itself a separate git boundary:
// dir/.git exists, as either a directory (the main clone, or a nested/vendored
// clone) or a file (a linked worktree's or a submodule's `gitdir: ...`
// pointer). Any such subdirectory belongs to a different git checkout and
// every workspace walk must skip it wholesale — this is what a hardcoded
// '.claude' skip used to approximate (agent worktrees under
// .claude/worktrees/<name> happen to be linked git worktrees), generalized to
// any harness, any location, any worktree/clone/submodule layout.
func IsNestedGitBoundary(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

// matchIgnoreGlob matches a config.yaml ignore glob against a repo-relative
// slash path. "**" matches everything; a "**/" prefix matches any (including
// zero) directory depth; a trailing "/**" matches the named directory itself
// and everything below it (so "bin/**" prunes the bin/ directory, not just
// files inside it); otherwise it's a plain path.Match.
func matchIgnoreGlob(g, p string) bool {
	if g == "**" {
		return true
	}
	if rest, ok := strings.CutPrefix(g, "**/"); ok {
		for {
			if ok, _ := path.Match(rest, p); ok {
				return true
			}
			i := strings.IndexByte(p, '/')
			if i < 0 {
				return false
			}
			p = p[i+1:]
		}
	}
	if base, ok := strings.CutSuffix(g, "/**"); ok {
		if p == base || strings.HasPrefix(p, base+"/") {
			return true
		}
	}
	ok, _ := path.Match(g, p)
	return ok
}

// SkipDir is the single entry point every workspace walk (ContextDirs,
// spec.Load, the coverage-gap walk, and — via DefaultSkipName /
// IsNestedGitBoundary — the indexer) shares to decide whether to prune a
// directory. rel is the repo-relative, slash-separated path of the directory
// ("" for the workspace root, which is never itself pruned); name is its
// basename. True when any of these hold:
//   - rel is a nested git boundary (IsNestedGitBoundary) — never checked for
//     the root itself;
//   - name is one of the built-in defaults (DefaultSkipName);
//   - rel matches a configured Config.Ignore glob;
//   - rel matches a configured Config.IgnoreRegex pattern.
func (r Root) SkipDir(rel, name string) bool {
	if rel != "" && IsNestedGitBoundary(filepath.Join(r.Dir, filepath.FromSlash(rel))) {
		return true
	}
	if defaultSkipNames[name] {
		return true
	}
	for _, g := range r.Cfg.Ignore {
		if matchIgnoreGlob(g, rel) {
			return true
		}
	}
	for _, pat := range r.Cfg.IgnoreRegex {
		re, err := regexp.Compile(pat)
		if err != nil {
			continue // malformed patterns are rejected at load() time; ignore defensively here
		}
		if re.MatchString(rel) {
			return true
		}
	}
	return false
}

// ContextDirs returns every repo-relative dir (incl. "" for root) that has a
// .spectackle folder with at least one bundle file, shallow before deep.
func (r Root) ContextDirs() ([]string, error) {
	var out []string
	err := filepath.WalkDir(r.Dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		if d.Name() == Dot {
			ctx, _ := filepath.Rel(r.Dir, filepath.Dir(p))
			ctx = filepath.ToSlash(ctx)
			if ctx == "." {
				ctx = ""
			}
			for _, f := range []string{"spec.md", "work.md", "journal.ndjson"} {
				if fileExists(filepath.Join(p, f)) {
					out = append(out, ctx)
					break
				}
			}
			return filepath.SkipDir
		}
		rel, _ := filepath.Rel(r.Dir, p)
		rel = filepath.ToSlash(rel)
		if rel == "." {
			rel = ""
		}
		if r.SkipDir(rel, d.Name()) {
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		di, dj := strings.Count(out[i], "/"), strings.Count(out[j], "/")
		if out[i] == "" || out[j] == "" {
			return out[i] == ""
		}
		if di != dj {
			return di < dj
		}
		return out[i] < out[j]
	})
	return out, nil
}

// NearestContext returns the closest ancestor context dir for a repo-relative
// path (falls back to "" root). ctxs must come from ContextDirs.
func NearestContext(ctxs []string, rel string) string {
	rel = strings.Trim(filepath.ToSlash(rel), "/")
	best := ""
	for _, c := range ctxs {
		if c == "" {
			continue
		}
		if rel == c || strings.HasPrefix(rel, c+"/") {
			if len(c) > len(best) {
				best = c
			}
		}
	}
	return best
}

// EnsureScaffold creates the .spectackle folder of a context dir with its
// server-written housekeeping files. For the root it additionally writes
// config.yaml, .gitignore (cache/) and the cache dir.
func (r Root) EnsureScaffold(ctx string) error {
	dot := r.SpectackleDir(ctx)
	if err := os.MkdirAll(dot, 0o755); err != nil {
		return err
	}
	if err := writeIfAbsent(filepath.Join(dot, ".gitattributes"), "journal.ndjson merge=union\n"); err != nil {
		return err
	}
	if ctx != "" {
		return nil
	}
	// ensure-lines, not write-if-absent: repos scaffolded by older versions
	// have a .gitignore without wt/ and must not start committing worktrees.
	if err := ensureLines(filepath.Join(dot, ".gitignore"), "cache/", "wt/"); err != nil {
		return err
	}
	if !fileExists(filepath.Join(dot, "config.yaml")) {
		raw, err := yaml.Marshal(defaultConfig())
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dot, "config.yaml"), raw, 0o644); err != nil {
			return err
		}
	}
	return os.MkdirAll(r.CacheDir(), 0o755)
}

func writeIfAbsent(path, content string) error {
	if fileExists(path) {
		return nil
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

// ensureLines appends any of the given lines that are missing from the file.
func ensureLines(path string, lines ...string) error {
	raw, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	have := map[string]bool{}
	for _, l := range strings.Split(string(raw), "\n") {
		have[strings.TrimSpace(l)] = true
	}
	out := string(raw)
	changed := false
	for _, l := range lines {
		if !have[l] {
			if out != "" && !strings.HasSuffix(out, "\n") {
				out += "\n"
			}
			out += l + "\n"
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return os.WriteFile(path, []byte(out), 0o644)
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func dirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}
