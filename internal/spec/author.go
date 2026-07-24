package spec

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/jxsl13/spectacle/internal/ears"
	"github.com/jxsl13/spectacle/internal/workspace"
)

// Spec bundles are server-managed artifacts: contracts are created through
// the MCP `rule` tool, never hand-written. The files stay markdown-on-disk so
// humans review them in git diffs, and the linter guards against out-of-band
// edits — but the write path is this file.

// AuthorReq describes one rule to persist.
type AuthorReq struct {
	Dir       string                                    // context dir ("" = root)
	Stem      string                                    // ID stem, e.g. "CUDA-KRN"; default: stem of last rule in target file
	ForceID   string                                    // exact ID to use (replay); skips minting, still lints
	Mint      func(stem string, floor int) (int, error) // collision-free minter (swarm coord); nil = local scan
	Sentence  string                                    // composed EARS sentence
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

// MaxNum returns the highest number used for a stem across the cascade —
// the floor for collision-free minting.
func (c *Cascade) MaxNum(stem string) int {
	max := 0
	for id := range c.byID {
		m := reIDTail.FindStringSubmatch(id)
		if m != nil && m[1] == stem {
			if n, err := strconv.Atoi(m[2]); err == nil && n > max {
				max = n
			}
		}
	}
	return max
}

// NextID returns the next free ID for a stem, scanning the whole cascade so
// the result can never collide locally (E006). Cross-worktree uniqueness
// needs AuthorReq.Mint (coord counters).
func (c *Cascade) NextID(stem string) string {
	return fmt.Sprintf("%s-%03d", stem, c.MaxNum(stem)+1)
}

// AddRule lints the sentence and, if error-free, appends it to the context
// dir's spec bundle (creating the file with front matter when needed) under
// the next free ID. Lint errors block the write; warnings do not.
func AddRule(ws workspace.Root, c *Cascade, req AuthorReq) (AuthorRes, error) {
	req.Dir = strings.Trim(filepath.ToSlash(req.Dir), "/")
	if req.Dir == "." {
		req.Dir = ""
	}
	rel := SpecRel(req.Dir)
	res := AuthorRes{Path: rel}

	res.Findings = ears.LintSentence(req.Sentence, rel, 0)
	for _, f := range res.Findings {
		if f.Severity == ears.Error {
			return res, nil
		}
	}

	// resolve the ID: forced (replay) > minted from stem
	stem := strings.TrimSuffix(strings.TrimSpace(req.Stem), "-")
	if req.ForceID != "" {
		res.ID = req.ForceID
		if m := reIDTail.FindStringSubmatch(req.ForceID); m != nil {
			stem = m[1]
		}
	} else {
		target, hasFile := c.File(req.Dir)
		if stem == "" {
			if hasFile && len(target.Rules) > 0 {
				last := target.Rules[len(target.Rules)-1]
				if m := reIDTail.FindStringSubmatch(last.ID); m != nil {
					stem = m[1]
				}
			}
			if stem == "" {
				return res, fmt.Errorf("spec: no ID stem: pass stem (e.g. CUDA-KRN) for a new spec file")
			}
		}
		if req.Mint != nil {
			n, err := req.Mint(stem, c.MaxNum(stem))
			if err != nil {
				return res, err
			}
			res.ID = fmt.Sprintf("%s-%03d", stem, n)
		} else {
			res.ID = c.NextID(stem)
		}
	}

	if err := ws.EnsureScaffold(req.Dir); err != nil {
		return res, err
	}
	abs := filepath.Join(ws.Dir, filepath.FromSlash(rel))
	var content string
	if raw, err := os.ReadFile(abs); err == nil {
		content = string(raw)
	} else {
		prefix, _, _ := strings.Cut(stem, "-")
		content = "---\nschema: " + workspace.SchemaStamp + "\nprefix: " + prefix + "\n---\n"
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

// EditRule replaces the block of an existing rule in place: new sentence
// (lint-gated), rationale and/or applies list. Empty sentence keeps the old
// one; empty rationale keeps the old one; applies always replaces.
func EditRule(ws workspace.Root, c *Cascade, id, sentence, rationale string, applies []string) (AuthorRes, error) {
	old, ok := c.Rule(id)
	if !ok {
		return AuthorRes{}, fmt.Errorf("spec: unknown rule %s", id)
	}
	if sentence == "" {
		sentence = old.Text
	}
	if rationale == "" {
		rationale = old.Rationale
	}
	if applies == nil {
		applies = old.Applies
	}
	res := AuthorRes{ID: id, Path: old.File}
	res.Findings = ears.LintSentence(sentence, old.File, old.Line)
	for _, f := range res.Findings {
		if f.Severity == ears.Error {
			return res, nil
		}
	}
	abs := filepath.Join(ws.Dir, filepath.FromSlash(old.File))
	lines, start, end, err := ruleBlock(abs, id, old.Line)
	if err != nil {
		return res, err
	}
	var b []string
	head := "## " + id
	if len(applies) > 0 {
		head += " {applies: " + strings.Join(applies, ",") + "}"
	}
	b = append(b, head, strings.TrimSpace(sentence))
	if r := strings.TrimSpace(rationale); r != "" {
		b = append(b, "", "Rationale: "+r)
	}
	out := append(lines[:start:start], append(b, lines[end:]...)...)
	if err := os.WriteFile(abs, []byte(strings.Join(out, "\n")), 0o644); err != nil {
		return res, err
	}
	res.Written = true
	return res, nil
}

// RetireRule deletes a rule block from its spec bundle; the full text
// survives in the journal (written by the caller). Returns the file path.
func RetireRule(ws workspace.Root, c *Cascade, id string) (string, error) {
	r, ok := c.Rule(id)
	if !ok {
		return "", fmt.Errorf("spec: unknown rule %s", id)
	}
	abs := filepath.Join(ws.Dir, filepath.FromSlash(r.File))
	lines, start, end, err := ruleBlock(abs, id, r.Line)
	if err != nil {
		return "", err
	}
	for start > 0 && strings.TrimSpace(lines[start-1]) == "" {
		start--
	}
	out := strings.Join(append(lines[:start:start], lines[end:]...), "\n")
	if err := os.WriteFile(abs, []byte(out), 0o644); err != nil {
		return "", err
	}
	return r.File, nil
}

// ruleBlock locates the [start,end) line range of a rule's block (heading
// until next heading or EOF) in the file at abs. line is 1-based.
func ruleBlock(abs, id string, line int) (lines []string, start, end int, err error) {
	raw, err := os.ReadFile(abs)
	if err != nil {
		return nil, 0, 0, err
	}
	lines = strings.Split(string(raw), "\n")
	start = line - 1
	if start < 0 || start >= len(lines) || !strings.HasPrefix(lines[start], "## "+id) {
		return nil, 0, 0, fmt.Errorf("spec: rule %s not found at line %d (file changed?)", id, line)
	}
	end = len(lines)
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "## ") {
			end = i
			break
		}
	}
	return lines, start, end, nil
}

// AppendIntent appends one line to the `## intent` prose section of a
// context dir's spec bundle (creating file and section as needed) — the
// archive-time delta merge target.
func AppendIntent(ws workspace.Root, ctx, line string) error {
	if err := ws.EnsureScaffold(ctx); err != nil {
		return err
	}
	abs := ws.SpecPath(ctx)
	var content string
	if raw, err := os.ReadFile(abs); err == nil {
		content = string(raw)
	} else {
		content = "---\nschema: " + workspace.SchemaStamp + "\n---\n"
	}
	lines := strings.Split(content, "\n")
	// find the intent section's end (last non-empty line before next heading)
	secStart := -1
	for i, l := range lines {
		if strings.TrimSpace(l) == "## intent" {
			secStart = i
			break
		}
	}
	if secStart < 0 {
		if !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
		content += "\n## intent\n" + line + "\n"
		return os.WriteFile(abs, []byte(content), 0o644)
	}
	insert := len(lines)
	for i := secStart + 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "## ") {
			insert = i
			break
		}
	}
	for insert > secStart+1 && strings.TrimSpace(lines[insert-1]) == "" {
		insert--
	}
	out := append(lines[:insert:insert], append([]string{line}, lines[insert:]...)...)
	return os.WriteFile(abs, []byte(strings.Join(out, "\n")), 0o644)
}
