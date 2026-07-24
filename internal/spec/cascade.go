// Package spec implements the topological pillar: cascading, directory-scoped
// spec bundles. Rules live next to the code they govern, inside .spectackle
// folders:
//
//	.spectackle/spec.md           root scope (repo-wide architecture rules)
//	<dir>/.spectackle/spec.md     rules scoped to <dir> and everything below
//
// Resolution for a path is root -> ancestor chain top-down -> nearest dir.
// Deeper files extend the inherited set (union); they win only explicitly:
//   - `overrides: [RULE-ID, ...]` in front matter removes inherited rules;
//   - `inherits: false` drops everything inherited.
//
// This mirrors how .gitignore/CLAUDE.md cascade, and it is what makes context
// loading token-efficient: the server materializes only the rules on the
// root->dir spine of the files in an impact radius, never the whole corpus.
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

	"github.com/jxsl13/spectackle/internal/ears"
	"github.com/jxsl13/spectackle/internal/workspace"
)

// SpecRel returns the repo-relative spec bundle path for a context dir
// ("" = root).
func SpecRel(ctx string) string {
	if ctx == "" {
		return workspace.Dot + "/spec.md"
	}
	return ctx + "/" + workspace.Dot + "/spec.md"
}

// frontMatter is the YAML header of a spec file.
type frontMatter struct {
	Schema    string   `yaml:"schema"`    // format stamp, injected by the server
	Prefix    string   `yaml:"prefix"`    // rule ID prefix, informational
	Scope     []string `yaml:"scope"`     // globs relative to the context dir; empty = everything below
	Inherits  *bool    `yaml:"inherits"`  // default true
	Overrides []string `yaml:"overrides"` // inherited rule IDs suppressed for this subtree
}

// SpecFile is one parsed spec bundle.
type SpecFile struct {
	Path      string // repo-relative path of the spec.md
	Dir       string // context dir it scopes ("" = root)
	Prefix    string
	Scope     []string
	Inherits  bool
	Overrides []string
	Rules     []ears.Rule
	Sections  []ears.Section
}

// Cascade resolves the applicable rules for paths and nodes.
type Cascade struct {
	root     string
	files    []SpecFile // by directory depth, shallow before deep
	byID     map[string]*ears.Rule
	findings []ears.Finding
}

// Load discovers and parses all spec bundles under root. Parse and lint
// findings do not fail the load; they are available via Findings().
func Load(root string) (*Cascade, error) {
	c := &Cascade{root: root, byID: map[string]*ears.Rule{}}

	// ws carries Config.Ignore/IgnoreRegex (if root has a config.yaml) so the
	// walk shares the exact same skip decisions as workspace.Root.ContextDirs
	// and the coverage-gap walk — see workspace.Root.SkipDir.
	ws, err := workspace.LoadRoot(root)
	if err != nil {
		return nil, err
	}

	var rels []string
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		if d.Name() == workspace.Dot {
			sp := filepath.Join(p, "spec.md")
			if st, err := os.Stat(sp); err == nil && !st.IsDir() {
				rel, _ := filepath.Rel(root, sp)
				rels = append(rels, filepath.ToSlash(rel))
			}
			return filepath.SkipDir
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if rel == "." {
			rel = ""
		}
		if ws.SkipDir(rel, d.Name()) {
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(rels, func(i, j int) bool { // shallow before deep, then lexical
		di, dj := strings.Count(rels[i], "/"), strings.Count(rels[j], "/")
		if di != dj {
			return di < dj
		}
		return rels[i] < rels[j]
	})
	for _, rel := range rels {
		sf, err := c.parseFile(rel)
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

func (c *Cascade) parseFile(rel string) (SpecFile, error) {
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
	if fm.Schema != "" && fm.Schema != workspace.SchemaStamp {
		return SpecFile{}, fmt.Errorf("spec: %s: schema %q != %q — regenerate, there is no migration", rel, fm.Schema, workspace.SchemaStamp)
	}
	body, bodyStart := ears.StripFrontMatter(src)
	rules, fs := ears.ParseRules(body, rel, bodyStart)
	c.findings = append(c.findings, fs...)

	// context dir = parent of the .spectackle folder
	dir := path.Dir(path.Dir(rel))
	if dir == "." {
		dir = ""
	}
	inherits := fm.Inherits == nil || *fm.Inherits
	return SpecFile{
		Path: rel, Dir: dir, Prefix: fm.Prefix, Scope: fm.Scope,
		Inherits: inherits, Overrides: fm.Overrides, Rules: rules,
		Sections: ears.ParseSections(body),
	}, nil
}

// Findings returns all lint findings gathered while loading the cascade.
func (c *Cascade) Findings() []ears.Finding { return c.findings }

// All returns every loaded spec file, shallow before deep.
func (c *Cascade) All() []SpecFile { return c.files }

// Rule looks up a rule by ID.
func (c *Cascade) Rule(id string) (ears.Rule, bool) {
	r, ok := c.byID[id]
	if !ok {
		return ears.Rule{}, false
	}
	return *r, true
}

// File returns the spec file scoping a context dir, if loaded.
func (c *Cascade) File(dir string) (SpecFile, bool) {
	for _, f := range c.files {
		if f.Dir == dir {
			return f, true
		}
	}
	return SpecFile{}, false
}

// ResolvedRule pairs a rule with the directory scope it came from.
type ResolvedRule struct {
	ears.Rule
	ScopeDir string // "." for root, else the context dir
}

// ForPath returns the rules applicable to a repo-relative file path, in
// resolution order (root -> nearest dir). Overrides and inherits:false of
// deeper files are applied.
func (c *Cascade) ForPath(rel string) []ResolvedRule {
	rel = filepath.ToSlash(rel)
	dir := path.Dir(rel)
	if dir == "." {
		dir = ""
	}

	var spine []*SpecFile
	for i := range c.files {
		sf := &c.files[i]
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
		if sf.Dir != "" && !sf.Inherits {
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
			out = append(out, ResolvedRule{Rule: r, ScopeDir: scopeDirLabel(sf.Dir)})
		}
	}
	return out
}

// ForNode returns the rules binding a graph node (SPX-SPC-007): rules whose
// applies list names the node ID come first — explicit bindings are the
// strongest signal — followed by the cascade rules of the node's file,
// deduplicated by rule ID.
func (c *Cascade) ForNode(id, file string) []ResolvedRule {
	var out []ResolvedRule
	taken := map[string]bool{}
	for i := range c.files {
		sf := &c.files[i]
		for _, r := range sf.Rules {
			for _, n := range r.Applies {
				if n == id && !taken[r.ID] {
					taken[r.ID] = true
					out = append(out, ResolvedRule{Rule: r, ScopeDir: scopeDirLabel(sf.Dir)})
					break
				}
			}
		}
	}
	if file != "" {
		for _, r := range c.ForPath(file) {
			if !taken[r.ID] {
				taken[r.ID] = true
				out = append(out, r)
			}
		}
	}
	return out
}

func scopeDirLabel(dir string) string {
	if dir == "" {
		return "."
	}
	return dir
}

// isAncestorDir reports whether a ("" = root) is an ancestor of or equal to b.
func isAncestorDir(a, b string) bool {
	if a == "" {
		return true
	}
	return a == b || strings.HasPrefix(b, a+"/")
}

// matchScope checks the spec file's scope globs against a repo-relative path.
// Globs are relative to the spec file's context dir. An empty scope matches
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
