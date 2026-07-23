// Package spec implements the topological pillar: cascading, directory-scoped
// EARS spec files. Rules live next to the code they govern:
//
//	.spectacle/global.ears.md    repo-wide architecture rules (always first)
//	<dir>/.spectacle.ears.md     rules scoped to <dir> and everything below
//
// Resolution for a path is global -> ancestor chain top-down -> nearest dir.
// Deeper files extend the inherited set (union); they win only explicitly:
//   - `overrides: [RULE-ID, ...]` in front matter removes inherited rules;
//   - `inherits: false` drops everything inherited, including global rules.
//
// This mirrors how .gitignore/CLAUDE.md cascade, so authors already know the
// mental model, and it is what makes context loading token-efficient: the
// server materializes only the rules on the root->dir spine of the files in
// an impact radius, never the whole spec corpus.
package spec

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/jxsl13/spectacle/internal/ears"
)

// SpecFileName is the per-directory spec file name.
const SpecFileName = ".spectacle.ears.md"

// GlobalSpecPath is the repo-wide rule file, relative to the root.
const GlobalSpecPath = ".spectacle/global.ears.md"

// frontMatter is the YAML header of a spec file.
type frontMatter struct {
	Prefix    string   `yaml:"prefix"`    // rule ID prefix, informational
	Scope     []string `yaml:"scope"`     // globs relative to the spec dir; empty = everything below
	Inherits  *bool    `yaml:"inherits"`  // default true
	Overrides []string `yaml:"overrides"` // inherited rule IDs suppressed for this subtree
}

// SpecFile is one parsed spec file.
type SpecFile struct {
	Path      string // repo-relative path of the spec file
	Dir       string // repo-relative directory it scopes ("" = root, "-" rendered for global)
	Prefix    string
	Scope     []string
	Inherits  bool
	Overrides []string
	Rules     []ears.Rule
	Global    bool
}

// Cascade resolves the applicable rules for paths and nodes.
type Cascade struct {
	root     string
	files    []SpecFile // global first, then by directory depth, stable order
	byID     map[string]*ears.Rule
	findings []ears.Finding // structural + sentence findings gathered at load
}

// Load discovers and parses all spec files under root. Parse and lint
// findings do not fail the load; they are available via Findings().
func Load(root string) (*Cascade, error) {
	c := &Cascade{root: root, byID: map[string]*ears.Rule{}}

	if sf, err := c.parseFile(GlobalSpecPath, true); err == nil {
		c.files = append(c.files, sf)
	} else if !os.IsNotExist(err) {
		return nil, err
	}

	var scoped []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() && (name == ".git" || name == "node_modules" || name == "testdata") {
			return filepath.SkipDir
		}
		if !d.IsDir() && name == SpecFileName {
			rel, _ := filepath.Rel(root, p)
			scoped = append(scoped, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(scoped, func(i, j int) bool { // shallow before deep, then lexical
		di, dj := strings.Count(scoped[i], "/"), strings.Count(scoped[j], "/")
		if di != dj {
			return di < dj
		}
		return scoped[i] < scoped[j]
	})
	for _, rel := range scoped {
		sf, err := c.parseFile(rel, false)
		if err != nil {
			return nil, err
		}
		c.files = append(c.files, sf)
	}

	// E006: duplicate rule IDs across the whole cascade
	for fi := range c.files {
		for ri := range c.files[fi].Rules {
			r := &c.files[fi].Rules[ri]
			if prev, dup := c.byID[r.ID]; dup {
				c.findings = append(c.findings, ears.Finding{
					Code: "E006", Severity: ears.Error, File: r.File, Line: r.Line,
					Msg: fmt.Sprintf("duplicate rule ID %s (first defined at %s:%d)", r.ID, prev.File, prev.Line),
				})
				continue
			}
			c.byID[r.ID] = r
		}
	}
	return c, nil
}

func (c *Cascade) parseFile(rel string, global bool) (SpecFile, error) {
	abs := filepath.Join(c.root, filepath.FromSlash(rel))
	raw, err := os.ReadFile(abs)
	if err != nil {
		return SpecFile{}, err
	}
	src := string(raw)
	var fm frontMatter
	if y := ears.FrontMatter(src); y != "" {
		if err := yaml.Unmarshal([]byte(y), &fm); err != nil {
			return SpecFile{}, fmt.Errorf("spec: %s: front matter: %w", rel, err)
		}
	}
	body, bodyStart := ears.StripFrontMatter(src)
	rules, fs := ears.ParseRules(body, rel, bodyStart)
	c.findings = append(c.findings, fs...)

	dir := path.Dir(rel)
	if dir == "." {
		dir = ""
	}
	inherits := fm.Inherits == nil || *fm.Inherits
	return SpecFile{
		Path: rel, Dir: dir, Prefix: fm.Prefix, Scope: fm.Scope,
		Inherits: inherits, Overrides: fm.Overrides, Rules: rules, Global: global,
	}, nil
}

// Findings returns all lint findings gathered while loading the cascade.
func (c *Cascade) Findings() []ears.Finding { return c.findings }

// All returns every loaded spec file, global first, shallow before deep.
func (c *Cascade) All() []SpecFile { return c.files }

// Rule looks up a rule by ID.
func (c *Cascade) Rule(id string) (ears.Rule, bool) {
	r, ok := c.byID[id]
	if !ok {
		return ears.Rule{}, false
	}
	return *r, true
}

// ResolvedRule pairs a rule with the directory scope it came from.
type ResolvedRule struct {
	ears.Rule
	ScopeDir string // "-" for global, "." for repo root, else the directory
}

// ForPath returns the rules applicable to a repo-relative file path, in
// resolution order (global -> root -> ... -> nearest dir). Overrides and
// inherits:false of deeper files are applied.
func (c *Cascade) ForPath(rel string) []ResolvedRule {
	rel = filepath.ToSlash(rel)
	dir := path.Dir(rel)
	if dir == "." {
		dir = ""
	}

	// collect the applicable spine: global + spec files whose Dir is an
	// ancestor of (or equal to) dir, whose scope globs match rel.
	var spine []*SpecFile
	for i := range c.files {
		sf := &c.files[i]
		if sf.Global {
			spine = append(spine, sf)
			continue
		}
		if !isAncestorDir(sf.Dir, dir) {
			continue
		}
		if matchScope(sf, rel) {
			spine = append(spine, sf)
		}
	}

	var out []ResolvedRule
	suppressed := map[string]bool{}
	for _, sf := range spine {
		if !sf.Global && !sf.Inherits {
			out = out[:0] // deeper file cut the cascade
		}
		for _, id := range sf.Overrides {
			suppressed[id] = true
			for i := 0; i < len(out); i++ {
				if out[i].ID == id {
					out = append(out[:i], out[i+1:]...)
					i--
				}
			}
		}
		for _, r := range sf.Rules {
			if suppressed[r.ID] {
				continue
			}
			out = append(out, ResolvedRule{Rule: r, ScopeDir: scopeDirLabel(sf)})
		}
	}
	return out
}

func scopeDirLabel(sf *SpecFile) string {
	if sf.Global {
		return "-"
	}
	if sf.Dir == "" {
		return "."
	}
	return sf.Dir
}

// isAncestorDir reports whether a ("" = root) is an ancestor of or equal to b.
func isAncestorDir(a, b string) bool {
	if a == "" {
		return true
	}
	return a == b || strings.HasPrefix(b, a+"/")
}

// matchScope checks the spec file's scope globs against a repo-relative path.
// Globs are relative to the spec file's directory. An empty scope matches
// everything below. "**/" prefixes match any (also zero) directory depth.
func matchScope(sf *SpecFile, rel string) bool {
	if len(sf.Scope) == 0 {
		return true
	}
	sub := rel
	if sf.Dir != "" {
		sub = strings.TrimPrefix(rel, sf.Dir+"/")
	}
	for _, g := range sf.Scope {
		if matchGlob(g, sub) {
			return true
		}
	}
	return false
}

func matchGlob(g, p string) bool {
	if g == "**" {
		return true
	}
	if rest, ok := strings.CutPrefix(g, "**/"); ok {
		// try at every path suffix depth, including depth zero
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
	ok, _ := path.Match(g, p)
	return ok
}
