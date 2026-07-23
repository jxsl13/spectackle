package spec

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/jxsl13/spectacle/internal/ears"
)

// Spec files are server-managed artifacts: contracts are created through the
// MCP tools (add_rule / rm_rule), never hand-written. The files stay
// markdown-on-disk so humans review them in git diffs, and the linter guards
// against out-of-band edits — but the write path is this package.

// AuthorReq describes one rule to persist.
type AuthorReq struct {
	Dir       string // target directory ("" or "." = repo root file)
	Global    bool   // write to .spectacle/global.ears.md instead
	Stem      string // ID stem, e.g. "CUDA-KRN"; default: stem of last rule in target file
	Sentence  string // composed EARS sentence (already linted by the caller)
	Rationale string
	Applies   []string
}

// AuthorRes reports the outcome.
type AuthorRes struct {
	ID       string
	Path     string // repo-relative spec file path
	Findings []ears.Finding
	Written  bool
}

var reIDTail = regexp.MustCompile(`^(.*)-(\d{3})$`)

// NextID returns the next free ID for a stem, scanning the whole cascade so
// the result can never collide (E006).
func (c *Cascade) NextID(stem string) string {
	max := 0
	for id := range c.byID {
		m := reIDTail.FindStringSubmatch(id)
		if m != nil && m[1] == stem {
			if n, err := strconv.Atoi(m[2]); err == nil && n > max {
				max = n
			}
		}
	}
	return fmt.Sprintf("%s-%03d", stem, max+1)
}

// targetSpecPath maps an AuthorReq to its repo-relative spec file path.
func targetSpecPath(req AuthorReq) string {
	if req.Global {
		return GlobalSpecPath
	}
	dir := strings.Trim(filepath.ToSlash(req.Dir), "/")
	if dir == "" || dir == "." {
		return SpecFileName
	}
	return path.Join(dir, SpecFileName)
}

// AddRule lints the sentence and, if error-free, appends it to the target
// spec file (creating the file with front matter when needed) under the next
// free ID. Lint errors block the write; warnings are reported but do not.
func AddRule(root string, c *Cascade, req AuthorReq) (AuthorRes, error) {
	rel := targetSpecPath(req)
	res := AuthorRes{Path: rel}

	res.Findings = ears.LintSentence(req.Sentence, rel, 0)
	for _, f := range res.Findings {
		if f.Severity == ears.Error {
			return res, nil
		}
	}

	// resolve the ID stem: explicit > last rule in target file
	stem := strings.TrimSuffix(strings.TrimSpace(req.Stem), "-")
	var target *SpecFile
	for i := range c.files {
		if c.files[i].Path == rel {
			target = &c.files[i]
		}
	}
	if stem == "" {
		if target != nil && len(target.Rules) > 0 {
			last := target.Rules[len(target.Rules)-1]
			if m := reIDTail.FindStringSubmatch(last.ID); m != nil {
				stem = m[1]
			}
		}
		if stem == "" {
			return res, fmt.Errorf("spec: no ID stem: pass stem (e.g. CUDA-KRN) for a new spec file")
		}
	}
	res.ID = c.NextID(stem)

	abs := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return res, err
	}
	var content string
	if raw, err := os.ReadFile(abs); err == nil {
		content = string(raw)
	} else {
		prefix, _, _ := strings.Cut(stem, "-")
		content = "---\nprefix: " + prefix + "\n---\n"
	}
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	var b strings.Builder
	b.WriteString(content)
	b.WriteString("\n## " + res.ID)
	if len(req.Applies) > 0 {
		b.WriteString(" {applies: " + strings.Join(req.Applies, ",") + "}")
	}
	b.WriteString("\n" + strings.TrimSpace(req.Sentence) + "\n")
	if r := strings.TrimSpace(req.Rationale); r != "" {
		b.WriteString("\nRationale: " + r + "\n")
	}
	if err := os.WriteFile(abs, []byte(b.String()), 0o644); err != nil {
		return res, err
	}
	res.Written = true
	return res, nil
}

// RemoveRule deletes a rule block (heading until the next heading or EOF)
// from its spec file. It returns the repo-relative file path.
func RemoveRule(root string, c *Cascade, id string) (string, error) {
	r, ok := c.Rule(id)
	if !ok {
		return "", fmt.Errorf("spec: unknown rule %s", id)
	}
	abs := filepath.Join(root, filepath.FromSlash(r.File))
	raw, err := os.ReadFile(abs)
	if err != nil {
		return "", err
	}
	lines := strings.Split(string(raw), "\n")
	start := r.Line - 1 // Rule.Line is 1-based and counts from file start
	if start < 0 || start >= len(lines) || !strings.HasPrefix(lines[start], "## "+id) {
		return "", fmt.Errorf("spec: rule %s not found at %s:%d (file changed?)", id, r.File, r.Line)
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "## ") {
			end = i
			break
		}
	}
	// also swallow blank lines directly above the heading
	for start > 0 && strings.TrimSpace(lines[start-1]) == "" {
		start--
	}
	out := strings.Join(append(lines[:start:start], lines[end:]...), "\n")
	if err := os.WriteFile(abs, []byte(out), 0o644); err != nil {
		return "", err
	}
	return r.File, nil
}
