package mcpserver

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jxsl13/spectackle/internal/budget"
	"github.com/jxsl13/spectackle/internal/evidence"
	"github.com/jxsl13/spectackle/internal/item"
	"github.com/jxsl13/spectackle/internal/journal"
	"github.com/jxsl13/spectackle/internal/lifecycle"
)

// validate is the post-implementation judge's evidence and verdict
// (T-01KYD94M3): a computed pack over the item's REAL diff — where
// declared-vs-landed divergence, vacuous tests, and fake benchmarks are
// computable exactly — and an independent recorded verdict gating
// done→archived. Findings reopen the item through the EXISTING done→active
// hop so the feedback IS the next brief and rounds count toward the
// existing escalation. The server renders and records; the validator
// judges — mirror of grill's review machinery, one phase later.

type validateIn struct {
	ID       string            `json:"id" jsonschema:"item ID, e.g. T-0007"`
	Op       string            `json:"op,omitempty" jsonschema:"pack (default) renders the diff evidence; verdict records the independent validation"`
	Pass     *bool             `json:"pass,omitempty" jsonschema:"verdict: true = the implementation is judged complete and honest"`
	Findings string            `json:"findings,omitempty" jsonschema:"verdict: required on pass=false — they reopen the item as the implementer's next brief"`
	Waivers  map[string]string `json:"waivers,omitempty" jsonschema:"verdict: per-finding waivers, key (class:subject from the pack) to reason — every open finding must be fixed or waived (T-01KYD9J)"`
	Budget   int               `json:"budget,omitempty" jsonschema:"token budget, default 1500"`
	Cur      string            `json:"cur,omitempty" jsonschema:"resume cursor"`
}

// maxFindingsBytes caps the journaled findings: an LLM-written field
// replayed on every future get must have a ceiling.
const maxFindingsBytes = 2000

// itemDiff recovers the item's real diff: the merge commit of its
// spectackle/<id> branch first (diff against the pre-merge parent), commits
// whose messages cite the item ID as the fallback, and an honest absence
// marker when neither exists — validation must not be skippable just
// because attribution is hard, but it must say what it could not see.
func (s *Server) itemDiff(id string) (diff, source string) {
	git := func(args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = s.ws.Dir
		out, err := cmd.Output()
		if err != nil {
			return ""
		}
		return string(out)
	}
	// Citation scans EXCLUDE records-only commits: the edge-commit engine
	// (T-01KYD94MG) stamps every records write with the item ID, and
	// attributing those to the diff made each render invalidate itself
	// (the render's own edge commit cited the item). Server records
	// commits are recognizable by construction: their subjects start
	// "spectackle(" — code checkpoints ("spectackle <id>: …") do not.
	citing := func(rangeSpec string) []string {
		out := git("log", "--format=%H %s", "--grep", id, rangeSpec)
		var shas []string
		for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
			h, subj, ok := strings.Cut(l, " ")
			if !ok || strings.HasPrefix(subj, "spectackle(") {
				continue
			}
			shas = append(shas, h)
		}
		return shas
	}
	// The SHORT branch form is a strict prefix of the legacy full form, so
	// one --grep matches merges from both naming eras (T-01KYG0ZX).
	branch := "spectackle/" + shortDisplayID(id)
	if m := strings.TrimSpace(git("log", "--merges", "--format=%H", "-n", "1", "--grep", branch, "HEAD")); m != "" {
		if d := git("diff", m+"^1", m); d != "" {
			// Post-merge commits citing the item are part of its attributed
			// diff: without them a verdict recorded before a citing
			// follow-up commit stayed fresh forever in merge mode — the
			// primary workflow — violating the diff-binding contract
			// (cross-verification of this task, live repro). STATED
			// RESIDUAL: a post-verdict commit NOT citing the item is
			// invisible to attribution in every mode — this binds the
			// current ATTRIBUTED diff, never the whole tree.
			post := citing(m + "..HEAD")
			var b strings.Builder
			b.WriteString(d)
			for i := len(post) - 1; i >= 0; i-- {
				b.WriteString(git("show", "--format=commit %h", post[i]))
				if b.Len() > 400_000 {
					break
				}
			}
			src := "merge of " + branch
			if len(post) > 0 {
				src += fmt.Sprintf(" + %d citing commits after it", len(post))
			}
			return capBytes(b.String(), 400_000), src
		}
	}
	shas := citing("HEAD")
	if len(shas) > 0 {
		var b strings.Builder
		// oldest first so the concatenated patches read in order
		for i := len(shas) - 1; i >= 0; i-- {
			b.WriteString(git("show", "--format=commit %h", shas[i]))
			if b.Len() > 400_000 {
				break
			}
		}
		if b.Len() > 0 {
			return capBytes(b.String(), 400_000), fmt.Sprintf("%d commits citing the item", len(shas))
		}
	}
	return "", "none"
}

func capBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "\n[diff truncated]"
}

func diffHash(diff string) string {
	if diff == "" {
		return "absent"
	}
	h := sha256.Sum256([]byte(diff))
	return hex.EncodeToString(h[:])
}

// validateHash binds the verdict to the judged substance: the attributed
// diff AND the declared targets — the untouched/offscope classes are
// functions of the target set, and a post-verdict target edit reached
// archive with a brand-new unaddressed finding when only the diff was
// bound (cross-verification of T-01KYD9J).
func validateHash(it item.Item, diff string) string {
	h := sha256.New()
	h.Write([]byte(diffHash(diff)))
	for _, t := range it.Targets {
		h.Write([]byte{0})
		h.Write([]byte(t))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// diffHunks parses the +side line ranges per file out of a unified diff —
// the changed-node detector intersects function spans with them (T-01KYD9R).
func diffHunks(diff string) map[string][][2]int {
	out := map[string][][2]int{}
	cur := ""
	for _, l := range strings.Split(diff, "\n") {
		if strings.HasPrefix(l, "diff --git a/") {
			f := strings.TrimPrefix(l, "diff --git a/")
			if i := strings.Index(f, " b/"); i >= 0 {
				cur = f[i+3:]
			}
			continue
		}
		if strings.HasPrefix(l, "@@") && cur != "" {
			var aStart, aLen, bStart, bLen int
			if n, _ := fmt.Sscanf(l, "@@ -%d,%d +%d,%d @@", &aStart, &aLen, &bStart, &bLen); n >= 3 {
				if bLen == 0 {
					bLen = 1
				}
				out[cur] = append(out[cur], [2]int{bStart, bStart + bLen - 1})
			}
		}
	}
	return out
}

// diffAddedLines parses the NEW-side line ranges of added ('+') runs per
// file (T-01KYFPNCX). diffHunks spans whole hunks INCLUDING context lines,
// which made the dup detector implicate pre-existing neighbors of an
// insertion (short8 vs shortHash, flagged on two landings); a dup finding
// must implicate only functions the diff actually added lines to.
func diffAddedLines(diff string) map[string][][2]int {
	out := map[string][][2]int{}
	cur := ""
	newLine := 0
	inHunk := false
	runStart := -1
	flush := func() {
		if runStart >= 0 && cur != "" {
			out[cur] = append(out[cur], [2]int{runStart, newLine - 1})
		}
		runStart = -1
	}
	for _, l := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(l, "diff --git a/"):
			flush()
			inHunk = false
			f := strings.TrimPrefix(l, "diff --git a/")
			if i := strings.Index(f, " b/"); i >= 0 {
				cur = f[i+3:]
			}
		case strings.HasPrefix(l, "@@"):
			flush()
			var aStart, aLen, bStart, bLen int
			if n, _ := fmt.Sscanf(l, "@@ -%d,%d +%d,%d @@", &aStart, &aLen, &bStart, &bLen); n >= 3 {
				newLine = bStart
				inHunk = true
			}
		case !inHunk:
			// content outside an @@ hunk is not diff body (synthetic or
			// malformed input) — never counted
		case strings.HasPrefix(l, "+++"), strings.HasPrefix(l, "---"):
			// file headers, not content
		case strings.HasPrefix(l, "+"):
			if runStart < 0 {
				runStart = newLine
			}
			newLine++
		case strings.HasPrefix(l, "-"):
			flush()
		default:
			flush()
			newLine++
		}
	}
	flush()
	return out
}

// diffFiles parses changed paths with +/- counts out of a unified diff.
func diffFiles(diff string) (files []string, adds, dels map[string]int) {
	adds, dels = map[string]int{}, map[string]int{}
	cur := ""
	seen := map[string]bool{}
	for _, l := range strings.Split(diff, "\n") {
		if strings.HasPrefix(l, "diff --git a/") {
			f := strings.TrimPrefix(l, "diff --git a/")
			if i := strings.Index(f, " b/"); i >= 0 {
				cur = f[i+3:]
			}
			if cur != "" && !seen[cur] {
				seen[cur] = true
				files = append(files, cur)
			}
			continue
		}
		switch {
		case strings.HasPrefix(l, "+") && !strings.HasPrefix(l, "+++"):
			adds[cur]++
		case strings.HasPrefix(l, "-") && !strings.HasPrefix(l, "---"):
			dels[cur]++
		}
	}
	return files, adds, dels
}

// validateComputed renders the computed finding classes over the diff; every
// line is one OPEN finding the validator must judge — evidence, never a
// veto, but not waivable by a bare pass either.
func (s *Server) validateComputed(it item.Item, diff string) []string {
	var out []string
	files, _, _ := diffFiles(diff)
	changed := map[string]bool{}
	prodChanged, testChanged := 0, 0
	for _, f := range files {
		changed[f] = true
		if strings.HasSuffix(f, "_test.go") {
			testChanged++
		} else if strings.HasSuffix(f, ".go") {
			prodChanged++
		}
	}
	// The declare-nothing dodge closes here: no targets at all leaves scope
	// unjudgeable, and both scope classes below would silently disarm
	// (cross-verification finding).
	if len(it.Targets) == 0 && diff != "" && (it.Kind == "task" || it.Kind == "bug") {
		out = append(out, "v notargets - nothing declared, scope unjudgeable")
	}
	// declared targets never touched — T-0135's 4-declared/15-landed is
	// exactly this computation's reason to exist, in both directions.
	for _, t := range it.Targets {
		p, ok := targetPath(t)
		if !ok {
			continue
		}
		hit := false
		for f := range changed {
			if f == p || strings.HasPrefix(f, strings.TrimSuffix(p, "/")+"/") {
				hit = true
				break
			}
		}
		if !hit && diff != "" {
			out = append(out, "v untouched "+p)
		}
	}
	// files changed outside every declared target (records are the
	// server's own writes, never offscope)
	offscope := 0
	for _, f := range files {
		if strings.Contains(f, ".spectackle/") {
			continue
		}
		in := false
		// The test sibling of a declared file is in scope: declaring x.go
		// conventionally covers x_test.go, and the first armed landing
		// flagged its own tests as offscope (false-positive shape).
		prod := strings.TrimSuffix(f, "_test.go") + ".go"
		for _, t := range it.Targets {
			p, ok := targetPath(t)
			if !ok {
				continue
			}
			if f == p || prod == p || strings.HasPrefix(f, strings.TrimSuffix(p, "/")+"/") {
				in = true
				break
			}
		}
		if !in && len(it.Targets) > 0 {
			if offscope < 10 {
				out = append(out, "v offscope "+f)
			}
			offscope++
		}
	}
	if offscope > 10 {
		out = append(out, fmt.Sprintf("v offscope +%d more", offscope-10))
	}
	// test honesty over CHANGED test files
	out = append(out, s.validateTests(it, diff, files)...)
	// benchmark honesty, only when the diff touches Benchmark funcs
	out = append(out, validateBench(it, diff)...)
	// the fix-in-test smell
	if it.Kind == "bug" && testChanged > 0 && prodChanged == 0 && diff != "" {
		out = append(out, "v testonly - bug fix with no production change")
	}
	// redundancy (T-01KYD9R): each function node the diff added or changed
	// compared against the fingerprint index; a >= 0.85 match is a
	// finding the validator judges — deliberate duplication (a fixture, a
	// fork-on-purpose) is a waiver with a reason, never the server's guess.
	out = append(out, s.validateDups(diff)...)
	// documentation completeness (user directive 2026-07-26: the solution
	// is complete from ALL aspects — docs included): a diff that adds
	// exported symbols or touches machine-facing text surfaces with zero
	// .md changes draws the finding. TRIPWIRE against omission, not a
	// quality judgment — whether the docs are RIGHT is the validator's
	// call; this only makes forgetting them impossible to miss.
	docChanged := false
	// exported-production scan only: Test functions are exported by Go
	// convention and flagged nodocs on a test-heavy diff (first live
	// false-positive of this class after the literal-range and
	// test-sibling shapes)
	exportedAdded := false
	inTestFile := false
	for _, l := range strings.Split(diff, "\n") {
		if strings.HasPrefix(l, "diff --git") {
			inTestFile = strings.Contains(l, "_test.go")
			continue
		}
		if !inTestFile && reAddedFunc.MatchString(l) {
			exportedAdded = true
		}
	}
	for _, f := range files {
		if strings.HasSuffix(f, ".md") && !strings.Contains(f, ".spectackle/") {
			docChanged = true
		}
	}
	if diff != "" && exportedAdded && !docChanged {
		out = append(out, "v nodocs - exported symbols added, no documentation touched")
	}
	return out
}

var reAddedFunc = regexp.MustCompile(`(?m)^\+func (?:\([^)]+\) )?([A-Z]\w*)\(`)

// validateTests computes the test-honesty classes: production symbols added
// by the diff with zero references from any test content, and vacuity in
// changed test files (a subtest body with no assertion call; a range whose
// assertions all sit inside it with no emptiness guard before it).
func (s *Server) validateTests(it item.Item, diff string, files []string) []string {
	var out []string
	// (a) untested added symbols, cap 10: token search over ALL test
	// content — the changed test files plus existing tests of the repo.
	var testBlob strings.Builder
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			if data, err := s.readWorkspaceFile(f); err == nil {
				testBlob.Write(data)
			}
		}
	}
	dirs := map[string]bool{}
	for _, f := range files {
		if strings.HasSuffix(f, ".go") {
			dirs[pathDir(f)] = true
		}
	}
	testBlob.WriteString(s.targetTestBlob(dirs))
	inProd := map[string]bool{}
	inTest := false
	for _, l := range strings.Split(diff, "\n") {
		if strings.HasPrefix(l, "diff --git") {
			inTest = strings.Contains(l, "_test.go")
			continue
		}
		if inTest {
			continue
		}
		for _, m := range reAddedFunc.FindAllStringSubmatch(l, -1) {
			inProd[m[1]] = true
		}
	}
	syms := make([]string, 0, len(inProd))
	for s := range inProd {
		syms = append(syms, s)
	}
	sort.Strings(syms)
	untested := 0
	for _, sym := range syms {
		if !strings.Contains(testBlob.String(), sym) {
			if untested < 10 {
				out = append(out, "v untested "+sym)
			}
			untested++
		}
	}
	if untested > 10 {
		out = append(out, fmt.Sprintf("v untested +%d more", untested-10))
	}
	// (b) vacuity in changed test files, cap 10
	vac := 0
	for _, f := range files {
		if !strings.HasSuffix(f, "_test.go") {
			continue
		}
		data, err := s.readWorkspaceFile(f)
		if err != nil {
			continue
		}
		for _, line := range vacuousTestLines(f, data) {
			if vac < 10 {
				out = append(out, line)
			}
			vac++
		}
	}
	if vac > 10 {
		out = append(out, fmt.Sprintf("v vacuous +%d more", vac-10))
	}
	return out
}

// vacuousTestLines parses one test file and reports subtest function
// literals containing no assertion call, and range statements that hold ALL
// of their test's assertions with no emptiness guard before them — the
// loop-over-empty-collection pass.
func vacuousTestLines(path string, src []byte) []string {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, 0)
	if err != nil {
		return nil
	}
	isAssert := func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return false
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return false
		}
		switch sel.Sel.Name {
		case "Error", "Errorf", "Fatal", "Fatalf", "Fail", "FailNow", "Skip", "Skipf":
			return true
		}
		return false
	}
	countAsserts := func(n ast.Node) int {
		c := 0
		ast.Inspect(n, func(x ast.Node) bool {
			if isAssert(x) {
				c++
			}
			return true
		})
		return c
	}
	var out []string
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Body == nil || !strings.HasPrefix(fn.Name.Name, "Test") {
			continue
		}
		total := countAsserts(fn.Body)
		ast.Inspect(fn.Body, func(x ast.Node) bool {
			// t.Run(..., func(...){ no assertion }) — the empty subtest
			if call, ok := x.(*ast.CallExpr); ok {
				if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Run" && len(call.Args) == 2 {
					if lit, ok := call.Args[1].(*ast.FuncLit); ok && countAsserts(lit.Body) == 0 {
						out = append(out, fmt.Sprintf("v vacuous %s:%d subtest without assertion", path, fset.Position(lit.Pos()).Line))
					}
				}
			}
			// range holding every assertion, no emptiness guard before it —
			// but a range over a composite literal (the table-driven test
			// idiom) cannot be empty and is exempt (first live landing
			// flagged a two-entry literal table; false-positive shape).
			if rng, ok := x.(*ast.RangeStmt); ok && total > 0 {
				inRange := countAsserts(rng.Body)
				if inRange == total && !hasLenGuard(fn.Body, rng, fset) && !rangesOverLiteral(fn.Body, rng) {
					out = append(out, fmt.Sprintf("v vacuous %s:%d assertions only inside a range with no emptiness guard", path, fset.Position(rng.Pos()).Line))
				}
			}
			return true
		})
	}
	return out
}

// hasLenGuard reports whether any len(...) expression occurs in the test
// body BEFORE the range statement — the cheap shape of an emptiness guard.
func hasLenGuard(body *ast.BlockStmt, rng *ast.RangeStmt, fset *token.FileSet) bool {
	found := false
	ast.Inspect(body, func(x ast.Node) bool {
		if x == nil || x.Pos() >= rng.Pos() {
			return false
		}
		if call, ok := x.(*ast.CallExpr); ok {
			if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "len" {
				found = true
			}
		}
		return !found
	})
	return found
}

var (
	reBenchFunc  = regexp.MustCompile(`(?m)^\+func (Benchmark\w+)\(`)
	reBenchClaim = regexp.MustCompile(`\bBenchmark\w+\b`)
)

// validateBench computes benchmark honesty: an added Benchmark whose diff
// hunk never consumes b.N or b.Loop, and benchmark names claimed in the
// item's body with no matching Benchmark func anywhere in the diff.
func validateBench(it item.Item, diff string) []string {
	var out []string
	added := map[string]bool{}
	for _, m := range reBenchFunc.FindAllStringSubmatch(diff, -1) {
		added[m[1]] = true
	}
	for name := range added {
		// bound the scan to the added lines after this func's declaration
		i := strings.Index(diff, "+func "+name+"(")
		if i < 0 {
			continue
		}
		seg := diff[i:]
		if j := strings.Index(seg[1:], "\n+func "); j >= 0 {
			seg = seg[:j+1]
		}
		if !strings.Contains(seg, "b.N") && !strings.Contains(seg, "b.Loop") {
			out = append(out, "v fakebench "+name)
		}
	}
	if len(added) > 0 || strings.Contains(diff, "_bench_test.go") {
		for _, m := range reBenchClaim.FindAllString(it.Body, -1) {
			if !strings.Contains(diff, "func "+m+"(") {
				out = append(out, "v benchclaim "+m)
				break
			}
		}
	}
	sort.Strings(out)
	if len(out) > 10 {
		out = append(out[:10], fmt.Sprintf("v bench +%d more", len(out)-10))
	}
	return out
}

func pathDir(f string) string {
	if i := strings.LastIndex(f, "/"); i >= 0 {
		return f[:i]
	}
	return "."
}

// readWorkspaceFile reads one repo-relative file from the serving root.
func (s *Server) readWorkspaceFile(rel string) ([]byte, error) {
	return os.ReadFile(filepath.Join(s.ws.Dir, filepath.FromSlash(rel)))
}

// validateState scans the journal once: the implementer identities (every
// start/submit ag, else the create ag), the latest pack render (Op=render:
// diff hash + open count) and the latest verdict (Op=verdict).
func (s *Server) validateState(id string) (implementers map[string]bool, renderHash string, openN int, openKeys []string, verdict *journal.Event, err error) {
	events, err := journal.ReadAll(s.ws)
	if err != nil {
		return nil, "", 0, nil, nil, err
	}
	implementers = map[string]bool{}
	creator := ""
	for i := range events {
		e := events[i]
		if e.ID != id {
			continue
		}
		switch e.Ev {
		case journal.EvCreate:
			if creator == "" {
				creator = e.Ag
			}
		case journal.EvStart, journal.EvSubmit:
			implementers[e.Ag] = true
		case journal.EvValidate:
			switch e.Op {
			case "render":
				renderHash, openN, openKeys = e.Hash, e.Open, e.Keys
			case "verdict":
				verdict = &events[i]
			}
		}
	}
	if len(implementers) == 0 && creator != "" {
		implementers[creator] = true
	}
	return implementers, renderHash, openN, openKeys, verdict, nil
}

func (s *Server) validate(in validateIn) (*mcp.CallToolResult, any, error) {
	if in.Op == "verdict" {
		return s.validateVerdict(in)
	}
	if in.Op != "" && in.Op != "pack" {
		return refuse("! ARG E - op must be pack|verdict")
	}
	if in.Budget <= 0 {
		in.Budget = 1500
	}
	sc, err := s.idScope()
	if err != nil {
		return nil, nil, err
	}
	id, bad := sc.expand(in.ID)
	if bad != nil {
		return bad, nil, nil
	}
	it, ok, err := item.Get(s.ws, id)
	if err != nil {
		return nil, nil, err
	}
	if !ok {
		return s.nearest(id)
	}

	diff, source := s.itemDiff(it.ID)
	var lines []string
	lines = append(lines, sc.record(it))
	if v := s.latestValidateLine(it, diff); v != "" {
		lines = append(lines, v)
	}
	files, adds, dels := diffFiles(diff)
	var diffSec []string
	diffSec = append(diffSec, "d source "+source)
	if diff == "" {
		diffSec = append(diffSec, "d absent — verdict proceeds on pack-absent evidence; say what you could not see")
	}
	for i, f := range files {
		if i >= 20 {
			diffSec = append(diffSec, fmt.Sprintf("d +%d more files", len(files)-20))
			break
		}
		diffSec = append(diffSec, fmt.Sprintf("d %s +%d -%d", f, adds[f], dels[f]))
	}
	lines = append(lines, "#diff")
	lines = append(lines, diffSec...)

	// Findings render pre-archive only (T-01KYFXEQ): the classes
	// recompute against a tree that has moved on past the tombstone and
	// mislead — the verdict trail below is what an archived-item reader
	// needs.
	var computed []string
	if it.State == item.StateArchived {
		lines = append(lines, "computed: suppressed (archived)")
	} else {
		computed = s.validateComputed(it, diff)
		if len(computed) > 0 {
			lines = append(lines, "#computed")
			lines = append(lines, computed...)
		}
	}
	if _, _, _, _, v, err := s.validateState(it.ID); err == nil && v != nil && v.Hash == validateHash(it, diff) {
		for _, w := range v.Wv {
			lines = append(lines, "v waived "+w)
		}
	}
	var verifySec []string
	for _, c := range s.main.Cfg.Verify {
		verifySec = append(verifySec, "g verify "+c)
	}
	if strings.TrimSpace(it.Goal) != "" {
		verifySec = append(verifySec, "g goal "+it.Goal)
	}
	if last := s.lastGateResult(it.ID); last != "" {
		verifySec = append(verifySec, "g gate "+last)
	}
	if len(verifySec) > 0 {
		if wr := s.waiverRate(); wr != "" {
			lines = append(lines, wr)
		}
		lines = append(lines, "#verify")
		lines = append(lines, verifySec...)
	}

	open := len(computed)
	if err := journal.Append(s.ws, it.Dir, journal.Event{
		Ev: journal.EvValidate, ID: it.ID, Dir: it.Dir, Op: "render",
		Hash: validateHash(it, diff), Open: open, Keys: findingKeys(computed),
	}); err != nil {
		return nil, nil, err
	}
	s.markDirty()
	lines = append(lines, fmt.Sprintf("ok validate %s rendered open=%d", sc.short(it.ID), open))

	kept, cur := budget.TruncateRecords(lines[1:], budget.Resume(in.Cur), in.Budget)
	return text(budget.Render(append(lines[:1:1], kept...), cur))
}

// latestValidateLine renders the standing verdict beside the pack — the
// single highest-value line, exempt from truncation via its position.
func (s *Server) latestValidateLine(it item.Item, curDiff string) string {
	_, _, _, _, v, err := s.validateState(it.ID)
	if err != nil || v == nil {
		return ""
	}
	verdict := "fail"
	if v.Pass {
		verdict = "pass"
	}
	line := "validate " + verdict + " " + v.Ag
	if v.Hash != validateHash(it, curDiff) {
		line += " (stale — diff or targets changed since)"
	}
	if v.Note != "" {
		line += " :: " + v.Note
	}
	return line
}

// validateVerdict records the independent validation verdict, mirroring
// grill's review refusals one phase later: the validator may not be the
// implementer, may not be anonymous, must have rendered the CURRENT diff,
// may not bare-pass over open computed findings, and a fail must say why —
// the findings reopen the item as its next brief. Same IDENTITY LIMIT as
// EvReview: ag-string divergence, not independence; the residual is
// accepted and stated there.
func (s *Server) validateVerdict(in validateIn) (*mcp.CallToolResult, any, error) {
	sc, err := s.idScope()
	if err != nil {
		return nil, nil, err
	}
	id, bad := sc.expand(in.ID)
	if bad != nil {
		return bad, nil, nil
	}
	it, ok, err := item.Get(s.ws, id)
	if err != nil {
		return nil, nil, err
	}
	if !ok {
		return s.nearest(id)
	}
	short := sc.short(it.ID)
	if reEphemeralAgent.MatchString(s.agent) {
		return refuse("! VALIDATE E " + short + " anonymous validator - set SPECTACKLE_AGENT to a deliberate name")
	}
	if in.Pass == nil {
		return refuse("! ARG E - verdict requires pass")
	}
	implementers, renderHash, openN, openKeys, _, err := s.validateState(it.ID)
	if err != nil {
		return nil, nil, err
	}
	if implementers[s.agent] {
		return refuse("! VALIDATE E " + short + " validator implemented this - use a fresh agent identity")
	}
	diff, _ := s.itemDiff(it.ID)
	if renderHash != validateHash(it, diff) {
		return refuse("! VALIDATE E " + short + " no pack rendered for the current diff and targets - validate it first")
	}
	if !*in.Pass && strings.TrimSpace(in.Findings) == "" {
		return refuse("! VALIDATE E " + short + " a failing verdict must say why - the findings reopen the item as its next brief")
	}
	// Per-finding addressal (T-01KYD9J), mirroring the review verdict: the
	// validator's judgment is authoritative, per finding, with recorded
	// reasons — a residuals-pass no longer burns a round the way the first
	// armed landing did.
	gap, wv, ignored := addressalGap(openN, openKeys, *in.Pass, in.Findings, in.Waivers)
	if gap != "" {
		return refuse("! VALIDATE E " + short + " " + gap)
	}
	warnIgnored := ""
	for _, k := range ignored {
		warnIgnored += "! VALIDATE W waiver for absent key " + k + " ignored (not in the current render)\n"
	}
	warn := ""
	findings := in.Findings
	if !*in.Pass {
		if len(findings) < 80 {
			warn = "! VALIDATE W findings under 80 chars - token-thin validations are a known tell (tripwire, padding-gameable)\n"
		}
	}
	if len(findings) > maxFindingsBytes {
		findings = findings[:maxFindingsBytes] + "…[truncated]"
	}
	if err := journal.Append(s.ws, it.Dir, journal.Event{
		Ev: journal.EvValidate, ID: it.ID, Dir: it.Dir, Op: "verdict",
		Pass: *in.Pass, Hash: validateHash(it, diff), Note: findings, Wv: wv,
	}); err != nil {
		return nil, nil, err
	}
	verdict := "fail"
	if *in.Pass {
		verdict = "pass"
	}
	_ = s.cd.Emit("validate", it.ID, verdict+" by "+s.agent)
	s.markDirty()

	// A failing verdict REOPENS a done item through the existing hop: the
	// findings become the brief, the round counts, exhaustion escalates
	// exactly as SPX-SWM-007 specifies.
	if !*in.Pass && it.State == item.StateDone {
		if _, err := lifecycle.Move(s.ws, it.ID, item.StateActive, "validate: "+findings); err != nil {
			var rex lifecycle.ErrRoundsExhausted
			if errors.As(err, &rex) {
				blocked, dec, eErr := lifecycle.Escalate(s.ws, s.minter(), rex.Item)
				if eErr != nil {
					return nil, nil, eErr
				}
				_ = s.cd.Emit("escalate", blocked.ID, "rounds limit — decide "+dec.ID)
				return text(warn + fmt.Sprintf("ok validate %s fail by %s\ni %s blocked rounds exhausted — decide %s (rescope|reject|override-once)", short, s.agent, short, sc.short(dec.ID)))
			}
			return nil, nil, err
		}
		return text(warnIgnored + warn + fmt.Sprintf("ok validate %s fail by %s — reopened done→active; the findings are the next brief", short, s.agent))
	}
	return text(warnIgnored + warn + "ok validate " + short + " " + verdict + " by " + s.agent)
}

// validateGateGap answers what stands between a task/bug and archive:
// "" when a passing, identity-valid, current-diff validation exists.
func (s *Server) validateGateGap(it item.Item) (string, error) {
	implementers, _, _, _, v, err := s.validateState(it.ID)
	if err != nil {
		return "", err
	}
	diff, _ := s.itemDiff(it.ID)
	switch {
	case v == nil:
		return "no validation verdict — validate op=verdict from a second identity", nil
	case v.Hash != validateHash(it, diff):
		return "stale validation (diff changed since) — re-render, then re-verdict", nil
	case !v.Pass:
		return "failing validation — address its findings, re-validate", nil
	case implementers[v.Ag]:
		return "validation by the implementer — a second identity must judge it", nil
	case reEphemeralAgent.MatchString(v.Ag):
		return "anonymous validation — verdicts need a deliberate SPECTACKLE_AGENT", nil
	}
	return "", nil
}

// validateRisk names the landed-diff input that flips a warn-mode archive
// gate to require (T-01KYFXDCH): distinct file count at or over
// feedback.risk_files, or any landed file inside a feedback.dangerous_paths
// glob. Computed STRICTLY from the attributed diff — declared targets are
// gameable and never consulted. Empty string = no risk tripped.
func (s *Server) validateRisk(id string) string {
	diff, _ := s.itemDiff(id)
	if diff == "" {
		return ""
	}
	files, _, _ := diffFiles(diff)
	return riskTrip(files, s.ws.Cfg.Feedback.RiskFiles, s.ws.Cfg.Feedback.DangerousPaths)
}

// riskTrip is the pure decision: file count at/over the threshold (0 means
// the default 8), or any file inside a dangerous glob. Returns the tripped
// input spelled out for the refusal line, or "".
func riskTrip(files []string, threshold int, dangerous []string) string {
	if threshold <= 0 {
		threshold = 8
	}
	if len(files) >= threshold {
		return fmt.Sprintf("landed %d files >= risk_files %d", len(files), threshold)
	}
	for _, f := range files {
		for _, pat := range dangerous {
			if dangerousMatch(f, pat) {
				return "landed " + f + " matches dangerous_paths " + pat
			}
		}
	}
	return ""
}

// dangerousMatch supports the two shapes users write: "dir/**" (subtree
// prefix) and plain path.Match globs.
func dangerousMatch(file, pattern string) bool {
	if rest, ok := strings.CutSuffix(pattern, "/**"); ok {
		return file == rest || strings.HasPrefix(file, rest+"/")
	}
	ok, err := path.Match(pattern, file)
	return err == nil && ok
}

// derivedArchiveNote composes the archive note FROM the passing verdict —
// derived, not fakeable prose, at zero agent effort. An explicit note
// appends after the derived part, never instead of it.
func (s *Server) derivedArchiveNote(it item.Item, explicit string) string {
	_, _, _, _, v, err := s.validateState(it.ID)
	if err != nil || v == nil || !v.Pass {
		return explicit
	}
	note := "validated pass by " + v.Ag + " diff " + shortHash(v.Hash)
	if v.Note != "" {
		note += " :: " + v.Note
	}
	if explicit != "" {
		note += " — " + explicit
	}
	return note
}

// hashPrefix truncates a hex hash for display; the standing dup-detector
// false positive (short8 twin, T-01KYFPNCX) is retired by making this THE
// one truncation helper.
func hashPrefix(h string, n int) string {
	if len(h) > n {
		return h[:n]
	}
	return h
}

func shortHash(h string) string { return hashPrefix(h, 12) }

// validateComputedForTest exercises the diff-only classes without a
// workspace — the unit seam TestValidateNodocsClass uses.
func (s *Server) validateComputedForTest(diff string) []string {
	return s.validateComputed(item.Item{Kind: "task"}, diff)
}

// lastGateResult renders what the gate trail last recorded for the item —
// the validator sees what was proven versus asserted (brief section the
// cross-verification found omitted). A move to done means the local gates
// passed at that edge; a same-state move noted "gate fail" is a recorded
// failure round.
func (s *Server) lastGateResult(id string) string {
	events, err := journal.ReadAll(s.ws)
	if err != nil {
		return ""
	}
	last := ""
	for _, e := range events {
		if e.ID != id || e.Ev != journal.EvMove {
			continue
		}
		switch {
		case strings.Contains(e.Note, "gate fail"):
			last = "last=fail (recorded gate failure round)"
		case e.To == item.StateDone:
			last = "last=pass (local gates passed at the done edge)"
		}
	}
	return last
}

// rangesOverLiteral reports whether the range's operand is a composite
// literal, directly or via an identifier assigned one in this function —
// the table-driven idiom, non-empty by construction.
func rangesOverLiteral(body *ast.BlockStmt, rng *ast.RangeStmt) bool {
	if _, ok := rng.X.(*ast.CompositeLit); ok {
		return true
	}
	id, ok := rng.X.(*ast.Ident)
	if !ok {
		return false
	}
	literal := false
	ast.Inspect(body, func(x ast.Node) bool {
		if x == nil || x.Pos() >= rng.Pos() {
			return false
		}
		if as, ok := x.(*ast.AssignStmt); ok {
			for i, lhs := range as.Lhs {
				l, lok := lhs.(*ast.Ident)
				if !lok || l.Name != id.Name || i >= len(as.Rhs) {
					continue
				}
				if _, cok := as.Rhs[i].(*ast.CompositeLit); cok {
					literal = true
				}
			}
		}
		return !literal
	})
	return literal
}

// validateDups builds the lazy fingerprint index and reports duplicates of
// the diff's changed function nodes (T-01KYD9R). Changed = the node's span
// intersects a +side hunk of the attributed diff.
func (s *Server) validateDups(diff string) []string {
	if diff == "" {
		return nil
	}
	// Added-line scoping (T-01KYFPNCX): only functions the diff ADDED
	// lines to can carry a dup finding — context-only neighbors are
	// pre-existing code the task never wrote.
	hunks := diffAddedLines(diff)
	if len(hunks) == 0 {
		return nil
	}
	load := func(rel string) []byte {
		data, err := s.readWorkspaceFile(rel)
		if err != nil {
			return nil
		}
		return data
	}
	index, truncated := evidence.BuildDupIndex(s.g, load)
	var changed []evidence.IndexedNode
	for _, n := range index {
		ranges, ok := hunks[n.File]
		if !ok {
			continue
		}
		nd, found := s.g.Node(n.ID)
		if !found {
			continue
		}
		for _, r := range ranges {
			if nd.Line <= r[1] && nd.EndLine >= r[0] {
				changed = append(changed, n)
				break
			}
		}
	}
	out := evidence.Duplicates(changed, index)
	if truncated {
		out = append(out, "v dup-index truncated")
	}
	return out
}
