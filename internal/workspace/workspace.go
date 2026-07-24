// Package workspace implements root detection and the .spectacle folder
// contract: every file the server writes lives inside a .spectacle/ folder
// (root or nested context dirs); the rest of the workspace is never touched.
//
// Root detection order: walk up from the start dir for .spectacle/config.yaml
// (the root marker — nested context dirs also have .spectacle/ folders, so the
// folder alone is ambiguous), then for .git, then fall back to the -root flag.
package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Dot is the folder name every server write is confined to.
const Dot = ".spectacle"

// SchemaStamp is injected into every server-written file's frontmatter.
// It marks the file format of a pre-1.0 codebase: the format may break at any
// time, the stamp changes with it, and there is no migration — an unknown
// stamp is a tool error ("regenerate"), caches simply rebuild.
const SchemaStamp = "v0"

// Config is .spectacle/config.yaml (root only).
type Config struct {
	Schema        string     `yaml:"schema"`
	Langs         []string   `yaml:"langs"`
	Ignore        []string   `yaml:"ignore"`
	BudgetDefault int        `yaml:"budget_default"`
	Compact       CompactCfg `yaml:"compact"`
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
	}
}

// Root is a detected workspace.
type Root struct {
	Dir string // absolute path
	Cfg Config
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
	if r.Cfg.BudgetDefault == 0 {
		r.Cfg.BudgetDefault = 2000
	}
	if r.Cfg.Compact.JournalMax == 0 {
		r.Cfg.Compact.JournalMax = 500
	}
	if r.Cfg.Compact.DoneMax == 0 {
		r.Cfg.Compact.DoneMax = 8
	}
	return r, nil
}

// SpectacleDir maps a repo-relative context dir ("" = root) to the absolute
// path of its .spectacle folder.
func (r Root) SpectacleDir(ctx string) string {
	return filepath.Join(r.Dir, filepath.FromSlash(ctx), Dot)
}

// SpecPath / WorkPath / JournalPath locate the three bundle files of a
// context dir, repo-relative ("" = root).
func (r Root) SpecPath(ctx string) string    { return filepath.Join(r.SpectacleDir(ctx), "spec.md") }
func (r Root) WorkPath(ctx string) string    { return filepath.Join(r.SpectacleDir(ctx), "work.md") }
func (r Root) JournalPath(ctx string) string { return filepath.Join(r.SpectacleDir(ctx), "journal.ndjson") }

// AnchorsPath is root-only (workspace-wide bindings).
func (r Root) AnchorsPath() string { return filepath.Join(r.Dir, Dot, "anchors.tsv") }

// CacheDir is root-only and excluded from git by the server-written .gitignore.
func (r Root) CacheDir() string { return filepath.Join(r.Dir, Dot, "cache") }

// ContextDirs returns every repo-relative dir (incl. "" for root) that has a
// .spectacle folder with at least one bundle file, shallow before deep.
func (r Root) ContextDirs() ([]string, error) {
	var out []string
	err := filepath.WalkDir(r.Dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		switch d.Name() {
		case ".git", "node_modules", "testdata":
			return filepath.SkipDir
		case Dot:
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

// EnsureScaffold creates the .spectacle folder of a context dir with its
// server-written housekeeping files. For the root it additionally writes
// config.yaml, .gitignore (cache/) and the cache dir.
func (r Root) EnsureScaffold(ctx string) error {
	dot := r.SpectacleDir(ctx)
	if err := os.MkdirAll(dot, 0o755); err != nil {
		return err
	}
	if err := writeIfAbsent(filepath.Join(dot, ".gitattributes"), "journal.ndjson merge=union\n"); err != nil {
		return err
	}
	if ctx != "" {
		return nil
	}
	if err := writeIfAbsent(filepath.Join(dot, ".gitignore"), "cache/\n"); err != nil {
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

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func dirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}
