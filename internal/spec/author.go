package spec

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/jxsl13/spectackle/internal/ears"
	"github.com/jxsl13/spectackle/internal/workspace"
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
	ID   string
	Path string // repo-relative spec file path

	// Root is the absolute dir Path is relative to: the workspace root
	// AddRule/EditRule were called with, not necessarily the main checkout.
	// GitHub issue 27: a caller inside a linked git worktree got back a
	// bare "ok ID .spectackle/spec.md" that read as if it landed in the
	// worktree, when the write had actually landed in the enclosing main
	// checkout — the relative Path alone can't tell the two apart. Root
	// pins down the other half of that join so a caller (or a future
	// mcpserver reporting change) can print filepath.Join(Root, Path) and
	// never wonder which directory it is relative to.
	Root string

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
	res := AuthorRes{Path: rel, Root: ws.Dir}

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
	// The read of abs and the write of abs run as one locked cycle — not the
	// write alone — because a concurrent AddRule/EditRule/RetireRule against
	// the SAME bundle file reads the same bytes Save would otherwise silently
	// overwrite; see withSpecLock.
	err := withSpecLock(ws, req.Dir, func() error {
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
		// One separator line, then the block in the canonical layout — the
		// same renderer EditRule uses, so the two write paths cannot drift
		// apart again (GitHub issue 30). ruleBlockLines already ends the
		// block with a blank element, which becomes the file's trailing
		// newline on join.
		body := "\n" + strings.Join(ruleBlockLines(res.ID, req.Sentence, req.Rationale, req.Applies), "\n")
		return os.WriteFile(abs, []byte(content+body), 0o644)
	})
	if err != nil {
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
	res := AuthorRes{ID: id, Path: old.File, Root: ws.Dir}
	// Linted AFTER the fallbacks above, deliberately: sentence now holds the
	// text this call will actually store, so every finding describes the stored
	// state rather than an intermediate one. (Before the tool layer learned to
	// recover the pattern for a slot edit, a pattern-less edit reached here with
	// sentence="" and lint therefore described the old text while the caller
	// believed they had changed it — the third symptom in GitHub issue 25.)
	res.Findings = ears.LintSentence(sentence, old.File, old.Line)
	for _, f := range res.Findings {
		if f.Severity == ears.Error {
			return res, nil
		}
	}
	abs := filepath.Join(ws.Dir, filepath.FromSlash(old.File))
	// Locate-then-rewrite as one locked cycle: ruleBlock re-reads abs fresh
	// under the lock, so a sibling's just-written edit is what this edit
	// splices against, never a copy read before the lock was held.
	err := withSpecLock(ws, ctxFromSpecRel(old.File), func() error {
		lines, start, end, err := ruleBlock(abs, id, old.Line)
		if err != nil {
			return err
		}
		b := ruleBlockLines(id, sentence, rationale, applies)
		out := append(lines[:start:start], append(b, lines[end:]...)...)
		return os.WriteFile(abs, []byte(strings.Join(out, "\n")), 0o644)
	})
	if err != nil {
		return res, err
	}
	res.Written = true
	return res, nil
}

// ruleBlockLines renders one rule block in THE canonical layout, and is the
// single place that layout is defined:
//
//	## <ID>[ {applies: a,b}]
//	<sentence>
//	[blank]
//	[Rationale: <text>]
//	[blank]
//
// The trailing blank line is the load-bearing part. A rule block's extent runs
// from its heading to the next heading, so the span an edit replaces includes
// the separator before the following rule; a rebuilt block without a trailing
// blank therefore consumed one separator per edit, permanently, and add and
// edit produced different bytes for identical content (GitHub issue 30). Ending
// the block with exactly one blank line makes the layout a property of the rule
// rather than of how the rule got there, which is what lets the two paths
// converge — and it is also correct for the last rule in a file, where the
// trailing blank is the file's final newline.
func ruleBlockLines(id, sentence, rationale string, applies []string) []string {
	head := "## " + id
	if len(applies) > 0 {
		head += " {applies: " + strings.Join(applies, ",") + "}"
	}
	b := []string{head, strings.TrimSpace(sentence)}
	if r := strings.TrimSpace(rationale); r != "" {
		b = append(b, "", "Rationale: "+r)
	}
	return append(b, "")
}

// RetireRule deletes a rule block from its spec bundle; the full text
// survives in the journal (written by the caller). Returns the file path.
func RetireRule(ws workspace.Root, c *Cascade, id string) (string, error) {
	r, ok := c.Rule(id)
	if !ok {
		return "", fmt.Errorf("spec: unknown rule %s", id)
	}
	abs := filepath.Join(ws.Dir, filepath.FromSlash(r.File))
	err := withSpecLock(ws, ctxFromSpecRel(r.File), func() error {
		lines, start, end, err := ruleBlock(abs, id, r.Line)
		if err != nil {
			return err
		}
		for start > 0 && strings.TrimSpace(lines[start-1]) == "" {
			start--
		}
		out := strings.Join(append(lines[:start:start], lines[end:]...), "\n")
		return os.WriteFile(abs, []byte(out), 0o644)
	})
	if err != nil {
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
// reIntentID matches both record-ID eras (ADR-0013's UUIDv7 form and the
// legacy sequential one). Deliberately a local pattern rather than an import
// of internal/item: spec owns rules and prose and does not otherwise depend
// on the lifecycle package, and a string shape is not worth that coupling.
var reIntentID = regexp.MustCompile(`^(?:ADR|[PTBRD])-(?:[0-9]{4}|[0-9A-HJKMNP-TV-Z]{10,})$`)

// intentSpan returns the half-open line range of the `## intent` section —
// its first content line and the line index of the next heading (or len).
// Both are 0 when there is no such section, so a caller's loop over
// [lo, hi) is empty and nothing outside is ever touched.
func intentSpan(lines []string) (int, int) {
	start := -1
	for i, l := range lines {
		if strings.TrimSpace(l) == "## intent" {
			start = i
			break
		}
	}
	if start < 0 {
		return 0, 0
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "## ") {
			end = i
			break
		}
	}
	return start + 1, end
}

// intentRecordID pulls the record ID out of an intent line ("- <ID> <title>…"),
// or "" when the line is not one. It is the idempotency key for AppendIntent:
// the ID identifies the record, while the rest of the line varies between
// archive attempts that carry different notes.
func intentRecordID(line string) string {
	rest, ok := strings.CutPrefix(strings.TrimSpace(line), "- ")
	if !ok {
		return ""
	}
	id, _, _ := strings.Cut(rest, " ")
	if !reIntentID.MatchString(id) {
		return ""
	}
	return id
}

// intentDebrisMarker is the text of internal/lifecycle's truncationMarker with
// its separator stripped. It is duplicated rather than imported because
// lifecycle imports this package, so the dependency cannot run the other way.
// The duplication is pinned by TestTruncationMarkerMatchesSpecDebris in
// internal/lifecycle, which can see both for the same reason the import cannot
// be reversed: lifecycle already depends on this package. That test fails if
// either side is changed alone, so the two cannot drift apart silently.
const intentDebrisMarker = "[body truncated at tombstone retention cap]"

func AppendIntent(ws workspace.Root, ctx, line string) error {
	if err := ws.EnsureScaffold(ctx); err != nil {
		return err
	}
	return withSpecLock(ws, ctx, func() error {
		abs := ws.SpecPath(ctx)
		var content string
		if raw, err := os.ReadFile(abs); err == nil {
			content = string(raw)
		} else {
			content = "---\nschema: " + workspace.SchemaStamp + "\n---\n"
		}
		lines := strings.Split(content, "\n")
		// One intent line per record, ever. The archive closure appends this
		// BEFORE its git merge can succeed, and a stranded closure compensates
		// the item archived->done without removing what already ran — so every
		// retry used to append another copy. That is not a log: `## intent` is
		// the set of statements about what LANDED, and the retry is the
		// operator's only response to a closure that timed out waiting for CI,
		// which means the duplication scaled with CI slowness rather than with
		// anything the author did (B-01KYQJDJJVFC2: three, two and two copies
		// of single lines, measured). Idempotent on the record ID, because a
		// second attempt legitimately carries a different note or gist and
		// must not count as a different line.
		//
		// Per LINE, not per call. `line` is not always one line: knowledge
		// apply's applyIntentEntry passes a whole prose section, many bullets
		// at once. Keying the whole call on its FIRST bullet's ID dropped the
		// entire blob — brand-new records included — whenever that one bullet
		// collided, while still reporting success. A silently skipped intent
		// line is permanent history loss, and that made the guard worse than
		// the duplication it replaced.
		// Also HEAL duplicates already on disk, as insurance rather than as a
		// cure for a proven mechanism. Duplicates were observed live in this
		// repository (three copies of one record's line), but a validator
		// could not reproduce them with plain git: the ort strategy
		// recognizes the byte-identical hunk and merges to one copy. The
		// likeliest source was hand-resolved records conflicts during a long
		// multi-agent session, not git's default merge — so the earlier claim
		// that a three-way merge keeps both insertions is withdrawn
		// (B-01KYQR51GXEQN). What stands is narrower and still worth having:
		// the write-time guard is per-write and cannot see a duplicate that
		// arrived by any other route, and `## intent` is a permanent
		// human-read artifact where a duplicate is visibly wrong.
		//
		// Healing here rather than in a merge driver: a driver needs local git
		// config, and an unconfigured clone falls back silently. This function
		// already reads the file and scans it to enforce one-line-per-record,
		// so the fix costs nothing extra and converges on the next archive.
		// Scoped to the `## intent` section, which is the only part of this
		// file AppendIntent owns. Scanning every line instead deleted
		// lookalikes elsewhere: a bullet in a rule's free-form Rationale, or
		// in one of the whitelisted prose sections (notes/design/context),
		// keyed the same as a real intent line — so whichever came SECOND in
		// file order was dropped, which for a lookalike above `## intent`
		// meant deleting the genuine record. A heal must not reach outside
		// the invariant it enforces.
		lo, hi := intentSpan(lines)
		have := map[string]bool{}
		healed := false
		var kept []string
		for i, l := range lines {
			if i < lo || i >= hi {
				kept = append(kept, l)
				continue
			}
			id := intentRecordID(l)
			if id == "" {
				// An ID-less line is NOT debris by default: applyIntentEntry
				// passes a whole imported prose section through here, and
				// dropping every line without an ID would delete it. That
				// broader heal was written first and rejected — two pinned
				// tests encode the opposite contract, and they are right.
				//
				// Exactly one ID-less shape is known debris: a stray
				// truncation marker, left when the shared truncator still led
				// with a newline and split a capped gist across two lines. It
				// carries no ID, so the dedupe below could never key it, and it
				// accumulated one copy per call — 15 of them in this
				// repository's own spec.md when the cause was found
				// (B-01KYRQXJ99F48). The producer is fixed; this removes what
				// it already wrote.
				if strings.TrimSpace(l) == intentDebrisMarker {
					healed = true
					continue
				}
				kept = append(kept, l)
				continue
			}
			if have[id] {
				healed = true // a duplicate of a record already listed above
				continue
			}
			have[id] = true
			kept = append(kept, l)
		}
		lines = kept

		var keep []string
		for _, l := range strings.Split(line, "\n") {
			if id := intentRecordID(l); id != "" && have[id] {
				continue
			}
			keep = append(keep, l)
		}
		if len(keep) == 0 {
			// nothing to append — but a heal may still be pending, and
			// dropping it here would leave the duplicates for a caller who
			// never appends again
			if !healed {
				return nil
			}
			return os.WriteFile(abs, []byte(strings.Join(lines, "\n")), 0o644)
		}
		line = strings.Join(keep, "\n")
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
	})
}

// withSpecLock serializes one context dir's spec bundle read-modify-write
// across processes via ws.Lock (coord.db's named lock table — see
// coord.DB.WithLock and workspace.Locker). ws.Lock is nil outside a
// swarm-aware caller (tests, migrate, a one-shot CLI run), where there is at
// most one writer already, so running unlocked is correct there, not merely
// tolerated.
//
// Keyed per context dir, matching item's withWorkLock: two different bundles
// must stay writable in parallel, or a global lock would serialize the whole
// swarm behind one file most agents never touch. Rule writes and item writes
// use disjoint lock names (see the "spec:" vs "work:" prefixes) — they touch
// different files and have no shared invariant that requires joint locking,
// so a draft and a rule add in the same context dir proceed independently.
func withSpecLock(ws workspace.Root, ctx string, fn func() error) error {
	if ws.Lock == nil {
		return fn()
	}
	key := ctx
	if key == "" {
		key = "."
	}
	return ws.Lock.WithLock("spec:"+key, fn)
}

// ctxFromSpecRel reverses SpecRel: the context dir named by a spec bundle's
// repo-relative path. EditRule and RetireRule only have the stored file path
// (from the already-loaded Cascade), not the ctx that produced it, so they
// need the inverse to build the same lock name AddRule/AppendIntent use for
// the same file.
func ctxFromSpecRel(rel string) string {
	suffix := "/" + workspace.Dot + "/spec.md"
	if !strings.HasSuffix(rel, suffix) {
		return "" // root: SpecRel("") == ".spectackle/spec.md", no dir prefix
	}
	return strings.TrimSuffix(rel, suffix)
}

// IntentDebrisMarker exposes intentDebrisMarker so internal/lifecycle can
// assert its truncation marker still matches the text this package heals.
func IntentDebrisMarker() string { return intentDebrisMarker }
