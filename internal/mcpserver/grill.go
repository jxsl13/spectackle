package mcpserver

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jxsl13/spectackle/internal/budget"
	"github.com/jxsl13/spectackle/internal/drift"
	"github.com/jxsl13/spectackle/internal/evidence"
	"github.com/jxsl13/spectackle/internal/graph"
	"github.com/jxsl13/spectackle/internal/item"
	"github.com/jxsl13/spectackle/internal/journal"
	"github.com/jxsl13/spectackle/internal/spec"
	"github.com/jxsl13/spectackle/internal/wt"
)

// grill is review evidence plus the review verdict (T-01KYD94KP4). The
// default op renders the computed critique — the classes the author cannot
// fake — and stamps it.Grilled with "<date> open=<n>", journaling the
// render's body hash. op=verdict records the INDEPENDENT review: a journal
// event bound to the reviewing identity and the judged body, refused for
// authors, anonymous (generated) identities, unrendered or edited bodies.
// The approval gate keys on that verdict, not the stamp: a date records
// that a pack rendered, never that anyone judged it — twelve stamps in
// this repository's history changed zero bodies.

type grillIn struct {
	ID       string            `json:"id" jsonschema:"item ID, e.g. P-0007"`
	Op       string            `json:"op,omitempty" jsonschema:"pack (default) renders the critique; verdict records the independent review"`
	Pass     *bool             `json:"pass,omitempty" jsonschema:"verdict: true = approved by the reviewer"`
	Findings string            `json:"findings,omitempty" jsonschema:"verdict: the review findings — required on pass=false, they become the author's next brief"`
	Waivers  map[string]string `json:"waivers,omitempty" jsonschema:"verdict: per-finding waivers, key (class:subject from the pack) to reason — every open finding must be fixed or waived (T-01KYD9J)"`
	Lenses   string            `json:"lenses,omitempty" jsonschema:"verdict: comma-separated lens labels the reviewer walked sequentially (e.g. correctness,security,refute); prefix per-lens findings with [lens]"`
	Panel    int               `json:"panel,omitempty" jsonschema:"verdict: declare an n-agent review panel for THIS item — legal only on a live risk signal (open irreversible/blast finding, or override-once spent); capped by swarm.panel_max"`
	Budget   int               `json:"budget,omitempty" jsonschema:"token budget, default 1500"`
	Cur      string            `json:"cur,omitempty" jsonschema:"resume cursor"`
}

func (s *Server) grill(in grillIn) (*mcp.CallToolResult, any, error) {
	if in.Op == "verdict" {
		return s.grillVerdict(in)
	}
	if in.Op != "" && in.Op != "pack" {
		return refuse("! ARG E - op must be pack|verdict")
	}
	if in.Budget <= 0 {
		in.Budget = 1500
	}
	// short prefixes are accepted everywhere an item ID is (ADR-0013); the
	// same scope renders the pack, so every ID grill emits can be fed back
	// into grill or get verbatim.
	sc, err := s.idScope()
	if err != nil {
		return nil, nil, err
	}
	id, bad := sc.expand(in.ID)
	if bad != nil {
		return bad, nil, nil
	}
	in.ID = id
	it, ok, err := item.Get(s.ws, in.ID)
	if err != nil {
		return nil, nil, err
	}
	if !ok {
		return s.nearest(in.ID)
	}

	c, err := spec.Load(s.ws.Dir)
	if err != nil {
		return nil, nil, err
	}
	anchors, err := drift.Load(s.ws)
	if err != nil {
		return nil, nil, err
	}
	anchored := map[string]bool{}
	for _, a := range anchors {
		anchored[string(a.Node)] = true
	}
	all, err := item.LoadAll(s.ws)
	if err != nil {
		return nil, nil, err
	}

	var lines []string
	lines = append(lines, sc.record(it))

	// The verdict line renders FIRST and is exempt from budget truncation
	// (it and the record line survive any cut): the reviewer's state is the
	// single highest-value line in the pack.
	_, _, _, _, rev, rerr := s.reviewState(it.ID)
	if rerr != nil {
		return nil, nil, rerr
	}
	if rev != nil {
		mark := ""
		if rev.Hash != reviewHash(it) {
			mark = " (stale — body edited since)"
		}
		verdict := "fail"
		if rev.Pass {
			verdict = "pass"
		}
		line := "review " + verdict + " " + rev.Ag + mark
		if len(rev.Ln) > 0 {
			line += " lenses=" + strings.Join(rev.Ln, ",")
		}
		if rev.Note != "" {
			line += " :: " + rev.Note
		}
		lines = append(lines, line)
		if wr := s.waiverRate(); wr != "" {
			lines = append(lines, wr)
		}
	}

	addSection := func(name string, recs []string) {
		if len(recs) == 0 {
			return
		}
		lines = append(lines, name)
		lines = append(lines, recs...)
	}

	// Findings render pre-archive only (T-01KYFXEQ): tombstone readers
	// want the verdict trail above, not a re-critique — the classes
	// recompute against a tree that has moved on and mislead.
	if it.State == item.StateArchived {
		lines = append(lines, "computed: suppressed (archived)")
		kept, cur := budget.TruncateRecords(lines[1:], budget.Resume(in.Cur), in.Budget)
		return text(budget.Render(append(lines[:1:1], kept...), cur))
	}

	// Computed classes render before the lower-value sections: they are
	// what the reviewer must address and what open=<n> counts.
	rej, err := s.grillRejections(it)
	if err != nil {
		return nil, nil, err
	}
	computed := s.grillComputed(it, c, len(rej))
	// Ambiguity classes (T-01KYFXBY): novelty needs a history signal — the
	// journal corpus, searched once here, not inside the pure computation.
	hist, err := s.cache.Search(it.Title, []string{"journal"}, packHits)
	if err != nil {
		return nil, nil, err
	}
	computed = append(computed, s.grillAmbiguity(it, c, len(hist))...)
	addSection("#computed", computed)
	// #evidence (T-01KYD88KE): the B-0009 unconsumed sweep and the B-0003
	// caller-divergence sweep over declared targets. Unsuppressed
	// unconsumed records count into the open tally for task and bug kinds
	// only (a proposal declares intent, not code); divergence is always
	// informational — a minority shape may be the point of the change.
	ev := s.grillEvidence(it)
	addSection("#evidence", ev)
	for _, l := range ev {
		if strings.HasPrefix(l, "e unconsumed ") && (it.Kind == "task" || it.Kind == "bug") {
			computed = append(computed, l)
		}
	}
	// standing waivers render beside the findings they judged — visible
	// judgment, hash-bound like the verdict (T-01KYD9J)
	if rev != nil && rev.Hash == reviewHash(it) {
		for _, w := range rev.Wv {
			lines = append(lines, "g waived "+w)
		}
	}
	addSection("#targets", grillTargets(s.g, it.Targets, anchored))
	addSection("#contracts", grillContracts(c, it.Targets))
	addSection("#tests", s.grillTests(it.Targets))
	addSection("#rejections", rej)
	addSection("#questions", grillQuestions(it))
	_ = all // child-brief heuristics deleted (word-presence checks, T-01KYD94KP4)

	// evidence — the ONE write this tool performs: stamp the item as
	// grilled today with the open computed-finding count, journal the
	// render with the body hash it bound to, and let siblings piggyback.
	open := len(computed)
	it.Grilled = time.Now().UTC().Format("2006-01-02") + fmt.Sprintf(" open=%d", open)
	if err := item.Upsert(s.ws, it); err != nil {
		return nil, nil, err
	}
	if err := journal.Append(s.ws, it.Dir, journal.Event{
		Ev: journal.EvGrill, ID: it.ID, Dir: it.Dir, Gr: it.Grilled,
		Hash: reviewHash(it), Open: open, Keys: findingKeys(computed),
	}); err != nil {
		return nil, nil, err
	}
	_ = s.cd.Emit("grill", it.ID, "grilled "+it.Grilled)
	s.markDirty()

	lines = append(lines, fmt.Sprintf("ok grilled %s %s", sc.short(it.ID), it.Grilled))

	// The record line, verdict line, and the ENTIRE #computed section are
	// exempt from budget truncation: finding keys must be enumerable or
	// verdict addressal is impossible against keys the reviewer cannot see
	// (T-01KYD9J, absorbing the key-truncation-exemption dependency).
	keep := 1
	if rev != nil {
		keep = 2
	}
	if len(computed) > 0 {
		keep += 1 + len(computed)
	}
	if keep > len(lines) {
		keep = len(lines)
	}
	kept, cur := budget.TruncateRecords(lines[keep:], budget.Resume(in.Cur), in.Budget)
	return text(budget.Render(append(lines[:keep:keep], kept...), cur))
}

// grillTargets checks node-ID-shaped targets against the graph and the
// anchor set: missing from the graph entirely -> nf, present but never bound
// by any rule's applies list -> g unanchored. Path-shaped targets are the
// #contracts section's concern instead (paths aren't graph nodes).
func grillTargets(g graph.Graph, targets []string, anchored map[string]bool) []string {
	var out []string
	for _, t := range targets {
		if strings.ContainsAny(t, "/") || !strings.Contains(t, ":") {
			continue
		}
		if _, ok := g.Node(graph.NodeID(t)); !ok {
			out = append(out, "nf "+t)
			continue
		}
		if !anchored[t] {
			out = append(out, "g unanchored "+t)
		}
	}
	return out
}

// grillContracts flags path-shaped targets with zero resolved EARS rules —
// code the item touches with no contract governing it at all.
func grillContracts(c *spec.Cascade, targets []string) []string {
	var out []string
	for _, t := range targets {
		p, ok := targetPath(t)
		if !ok {
			continue
		}
		if len(c.ForPath(p)) == 0 {
			out = append(out, "g nocontract "+t)
		}
	}
	return out
}

// The child-brief heuristics (short-body/no-path/no-verify) and the
// scope/rollback/exit-criterion substring questions were DELETED here
// (T-01KYD94KP4): every one was a word-presence test satisfiable by padding
// — T-0137 gamed them with a well-formed paragraph — and their job belongs
// to the independent reviewer's verdict, which the computed classes below
// feed with evidence the author cannot fake.

// grillTests flags target packages under internal/ or cmd/ that have no
// *_test.go file at all — a coarse, package-level test-coverage smell check
// (not a coverage percentage), scoped to the item's own targets only.
func (s *Server) grillTests(targets []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, t := range targets {
		p, ok := targetPath(t)
		if !ok {
			continue
		}
		if !strings.HasPrefix(p, "internal/") && !strings.HasPrefix(p, "cmd/") {
			continue
		}
		dir := p
		if strings.Contains(filepath.Base(p), ".") {
			dir = filepath.Dir(p)
		}
		dir = filepath.ToSlash(dir)
		if seen[dir] {
			continue
		}
		seen[dir] = true
		matches, _ := filepath.Glob(filepath.Join(s.ws.Dir, filepath.FromSlash(dir), "*_test.go"))
		if len(matches) == 0 {
			out = append(out, "g notest "+dir)
		}
	}
	sort.Strings(out)
	return out
}

// grillRejections looks for similar past failures via the item's own
// title+body — the same signal draft's #rejections section searches for
// before the item existed.
func (s *Server) grillRejections(it item.Item) ([]string, error) {
	q := it.Title
	if it.Body != "" {
		q += " " + it.Body
	}
	docs, err := s.cache.Search(q, []string{"rejection"}, packHits)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, d := range docs {
		out = append(out, fmt.Sprintf("j %s %s :: %s", d.ID, d.Title, d.Body))
	}
	return out, nil
}

// grillQuestions shrank to the refs-only deliberation check
// (T-01KYD94KP4): the substring questions were padding-gameable, and even
// hasRecordedDeliberation's prose path was strings.Contains(body,
// "rejected") — a word-presence test of exactly the species this task
// removes. A weighed decision now counts only as a STRUCTURAL ref: an
// ADR-, R-, or rejection-tombstone citation. Prose never counts.
func grillQuestions(it item.Item) []string {
	if it.Kind != "proposal" {
		return nil
	}
	for _, r := range it.Refs {
		if strings.HasPrefix(r, "ADR-") || strings.HasPrefix(r, "R-") {
			return nil
		}
	}
	return []string{"q no deliberation recorded: no ADR or research ref"}
}

// ---- computed critique classes + the review verdict (T-01KYD94KP4) ----

// reviewHash is what verdicts and pack renders bind to: the judged
// SUBSTANCE — body AND targets, since four of the five computed classes
// (irreversible, blast, env-axes, research-needed) are functions of the
// target set, and a Body-only hash left a targets-only edit keeping a
// verdict fresh (cross-verification of this task). Edit either and every
// stamp computed against them expires by construction. The separators are
// unambiguous: neither byte occurs in bodies or target paths.
func reviewHash(it item.Item) string {
	h := sha256.New()
	h.Write([]byte(it.Body))
	for _, t := range it.Targets {
		h.Write([]byte{0})
		h.Write([]byte(t))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// reEphemeralAgent matches coord.GenName's exact output shape ("ag-%04x").
// Chosen over a per-event env-set boolean: the shape is fixed in one place
// (coord.go GenName), needs no journal schema change, and survives replay.
// STATED LIMIT: a caller who deliberately names itself ag-beef is
// indistinguishable from a generated identity and gets refused — a
// deliberate name should look deliberate.
var reEphemeralAgent = regexp.MustCompile(`^ag-[0-9a-f]{4}$`)

// knownBadVerify is the table of VERIFY-command shapes that have actually
// burned this repository — each entry cites its recorded failure. Seeded
// with exactly two; grown only by recorded incidents, never speculation.
var knownBadVerify = []struct {
	re   *regexp.Regexp
	name string
}{
	{regexp.MustCompile(`\blint\s+-root\b`), "lint -root"},                   // B-0010: lint takes a positional root
	{regexp.MustCompile(`\|[^\n;|]*;\s*echo\s+\$\?`), "pipe-then-exit-code"}, // $? after a pipe reads the LAST stage (false B-01KYED3E)
}

// reIrreversibleTarget names target substrings whose damage a revert cannot
// undo: append-only journals, the coordination DB, schema migrations.
var reIrreversibleTarget = regexp.MustCompile(`journal\.ndjson|coord\.db|SchemaStamp|migrat`)

// reRestoreHeading is the tripwire the irreversibility class demands.
// STATED LIMIT, verbatim per the brief: this is a TRIPWIRE against
// omission, not a verification of substance — T-0137 gamed the old
// word-check with a well-formed paragraph and a heading requirement is
// gameable the same way; substance is judged by the independent reviewer's
// verdict, and declared-vs-landed divergence is NOT detectable
// pre-implementation here — it is the validation phase's #diff computation.
var reRestoreHeading = regexp.MustCompile(`(?mi)^#*\s*(RESTORE|ROLLBACK)\b`)

// rePathToken finds repo-path-shaped tokens in a body for the
// path-existence class; the prefix list keeps URLs and flag args out.
var rePathToken = regexp.MustCompile(`(?:^|[\s"'` + "`" + `(])((?:internal|cmd|docs|scripts|\.spectackle|\.github)/[A-Za-z0-9_./\-]+)`)

// envAxes is the environment differential (class d): five axes, each
// anchored to a recorded defect. tests= comes from a static token scan of
// the target packages' _test.go files; an absent test counts as an OPEN
// finding only when the item's targets touch the axis's subsystem (dirs) —
// the per-axis scoping condition, stated here as the brief demands.
var envAxes = []struct {
	name   string
	defect string   // the recorded defect anchoring the axis
	dirs   []string // targets under these prefixes arm the axis
	tokens []string // any of these in a target package's tests = covered
}{
	{"branch-name", "B-0004", []string{"internal/wt", "internal/mcpserver", "cmd/"}, []string{"master", "gitBase", "DefaultBranch"}},
	{"git-dir-shape", "B-01KYD1G9K", []string{"internal/wt", "internal/mcpserver"}, []string{".git"}},
	{"root-kind", "B-0002", []string{"internal/wt", "internal/mcpserver", "internal/workspace"}, []string{"worktree"}},
	{"process-topology", "B-01KYD57F", []string{"internal/mcpserver", "internal/coord"}, []string{"SPECTACKLE_AGENT"}},
	{"path-normalization", "T-0136", []string{"internal/wt", "internal/workspace", "internal/mcpserver"}, []string{"EvalSymlinks", "ToSlash"}},
}

// grillComputed renders the five computed classes; every returned line is
// one OPEN finding counted into the open=<n> stamp. The classes are the
// half of review the author cannot fake — the server computes them, the
// reviewer judges them (rendered evidence, not a veto; the gate keys on
// the verdict).
func (s *Server) grillComputed(it item.Item, c *spec.Cascade, rejectionHits int) []string {
	var out []string
	// a. path-existence: named paths must exist in the working tree.
	seen := map[string]bool{}
	for _, m := range rePathToken.FindAllStringSubmatch(it.Body, -1) {
		tok := strings.TrimRight(m[1], ".,;:)")
		if seen[tok] {
			continue
		}
		seen[tok] = true
		if _, err := os.Stat(filepath.Join(s.ws.Dir, filepath.FromSlash(tok))); err != nil {
			out = append(out, "g nopath "+tok)
		}
	}
	// b. verify-executability: known-burned command shapes.
	for _, kb := range knownBadVerify {
		if kb.re.MatchString(it.Body) {
			out = append(out, "g badverify "+kb.name)
		}
	}
	// c. irreversibility: dangerous targets or a wide blast radius demand
	// a RESTORE/ROLLBACK section heading (tripwire — see reRestoreHeading).
	if !reRestoreHeading.MatchString(it.Body) {
		for _, t := range it.Targets {
			if reIrreversibleTarget.MatchString(t) {
				out = append(out, "g irreversible "+t)
			}
		}
		if len(it.Targets) >= 8 {
			out = append(out, fmt.Sprintf("g blast %d targets", len(it.Targets)))
		}
	}
	// d. environment differential: live value beside what the tests pin.
	out = append(out, s.grillEnvAxes(it.Targets)...)
	// e. research-demand: uncovered path + zero history/rejection signal =
	// unknown territory; the pack cannot substitute for a study. Closes on
	// a cited R-item (any state) — a structural ref, never semantic scoring.
	for _, r := range it.Refs {
		if strings.HasPrefix(r, "R-") {
			return out
		}
	}
	if rejectionHits == 0 {
		for _, t := range it.Targets {
			p, ok := targetPath(t)
			if !ok {
				continue
			}
			if len(c.ForPath(p)) == 0 {
				out = append(out, "g research-needed "+p)
				break
			}
		}
	}
	return out
}

// grillEnvAxes computes class d: for each armed axis (targets touch its
// dirs), the live environment value beside the token scan of the target
// packages' tests; absent coverage is the open finding.
func (s *Server) grillEnvAxes(targets []string) []string {
	dirs := map[string]bool{}
	for _, t := range targets {
		if p, ok := targetPath(t); ok {
			dirs[p] = true
		}
	}
	testBlob := s.targetTestBlob(dirs)
	var out []string
	for _, ax := range envAxes {
		armed := false
		for d := range dirs {
			for _, pre := range ax.dirs {
				if strings.HasPrefix(d, pre) {
					armed = true
				}
			}
		}
		if !armed {
			continue
		}
		covered := ""
		for _, tok := range ax.tokens {
			if strings.Contains(testBlob, tok) {
				covered = tok
				break
			}
		}
		if covered == "" {
			out = append(out, fmt.Sprintf("e %s live=%s tests=absent (%s)", ax.name, s.envAxisLive(ax.name), ax.defect))
		}
	}
	return out
}

// envAxisLive answers the LIVE value of one environment axis for this
// serving process — the concrete thing the item's tests should also pin.
func (s *Server) envAxisLive(axis string) string {
	switch axis {
	case "branch-name":
		if b, err := wt.CurrentBranch(s.main.Dir); err == nil {
			return b
		}
		return "unknown"
	case "git-dir-shape":
		if fi, err := os.Stat(filepath.Join(s.ws.Dir, ".git")); err == nil && !fi.IsDir() {
			return "file"
		}
		return "dir"
	case "root-kind":
		if s.ws.Dir != s.main.Dir {
			return "worktree"
		}
		return "checkout"
	case "process-topology":
		if reEphemeralAgent.MatchString(s.agent) {
			return "generated-identity"
		}
		return "env-identity"
	case "path-normalization":
		return runtime.GOOS
	}
	return "unknown"
}

// targetTestBlob concatenates the *_test.go contents of every target
// package once per render — the static scan class d reads.
func (s *Server) targetTestBlob(dirs map[string]bool) string {
	var b strings.Builder
	for d := range dirs {
		dir := d
		if strings.Contains(filepath.Base(d), ".") {
			dir = filepath.Dir(d)
		}
		matches, _ := filepath.Glob(filepath.Join(s.ws.Dir, filepath.FromSlash(dir), "*_test.go"))
		for _, m := range matches {
			if data, err := os.ReadFile(m); err == nil {
				b.Write(data)
			}
		}
	}
	return b.String()
}

// rejectSnapshot reconstructs a rejected item's pre-state from its latest
// reject event — enough for the review gate (kind, state, and the judged
// substance the hash binds). Rejected items leave work.md, so item.Get
// cannot answer for a revival move.
func (s *Server) rejectSnapshot(id string) (item.Item, bool) {
	events, err := journal.ReadAll(s.ws)
	if err != nil {
		return item.Item{}, false
	}
	var snap *journal.Event
	for i := range events {
		e := events[i]
		if e.ID != id {
			continue
		}
		switch e.Ev {
		case journal.EvReject:
			snap = &events[i]
		case journal.EvCreate, journal.EvMove, journal.EvArchive:
			// any later lifecycle event means the item was revived or
			// re-recorded — the snapshot no longer describes it
			if snap != nil && events[i].T.After(snap.T) {
				snap = nil
			}
		}
	}
	if snap == nil {
		return item.Item{}, false
	}
	return item.Item{
		ID: id, Kind: snap.K, State: item.StateRejected,
		Body: snap.Body, Targets: snap.Tg, Grilled: snap.Gr,
	}, true
}

// findingKey derives the stable key (class:subject) of one computed
// finding line — the unit of addressal (T-01KYD9J). The marker letter
// (g/v/e) is dropped; env-axis lines key as env:<axis>.
func findingKey(line string) string {
	f := strings.Fields(line)
	if len(f) < 2 {
		return line
	}
	if f[0] == "e" {
		return "env:" + f[1]
	}
	if len(f) < 3 {
		return f[1]
	}
	return f[1] + ":" + f[2]
}

func findingKeys(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		out = append(out, findingKey(l))
	}
	return out
}

// addressalGap answers whether a verdict addresses every open finding key:
// on pass every key needs a non-empty waiver reason; on fail every key's
// subject must appear in the findings text — the machine can verify only
// that the judge SAW everything, never that the judgment is right. Returns
// the refusal text or "" plus the journaled waiver lines.
func addressalGap(openN int, openKeys []string, pass bool, findings string, waivers map[string]string) (string, []string, []string) {
	// Legacy renders (pre-addressal binary: open count, no keys) must not
	// bypass the invariant — a bare pass cleared the gate over open
	// findings in exactly that shape (cross-verification, live repro
	// against this repo's own journal). Re-render first.
	if openN > 0 && len(openKeys) == 0 {
		return "legacy render (open findings, no keys) - re-render first", nil, nil
	}
	var missing []string
	var wv []string
	var ignored []string
	seen := map[string]bool{}
	for _, k := range openKeys {
		seen[k] = true
		if reason, ok := waivers[k]; ok {
			if strings.TrimSpace(reason) == "" {
				return "waiver without a reason: " + k, nil, nil
			}
			// Reasons render verbatim into the record stream: flatten
			// whitespace so a crafted reason cannot forge computed or
			// verdict lines in the next pack (cross-verification injection
			// repro), and cap it — an LLM-written field replayed forever
			// needs a ceiling.
			reason = strings.Join(strings.Fields(reason), " ")
			if len(reason) > 300 {
				reason = reason[:300] + "…"
			}
			wv = append(wv, k+" "+reason)
			continue
		}
		if !pass {
			subject := k
			if i := strings.Index(k, ":"); i >= 0 {
				subject = k[i+1:]
			}
			if strings.Contains(findings, subject) {
				continue
			}
		}
		missing = append(missing, k)
	}
	for k := range waivers {
		if !seen[k] {
			ignored = append(ignored, k)
		}
	}
	sort.Strings(ignored)
	if len(missing) > 0 {
		return "unaddressed findings: " + strings.Join(missing, " "), nil, nil
	}
	return "", wv, ignored
}

// reviewState scans the item's journal trail once: the author (create
// event's ag), the latest pack render (hash + open count), and the latest
// review verdict.
func (s *Server) reviewState(id string) (author, renderHash string, openN int, openKeys []string, rev *journal.Event, err error) {
	events, err := journal.ReadAll(s.ws)
	if err != nil {
		return "", "", 0, nil, nil, err
	}
	for i := range events {
		e := events[i]
		if e.ID != id {
			continue
		}
		switch e.Ev {
		case journal.EvCreate:
			if author == "" {
				author = e.Ag
			}
		case journal.EvGrill:
			renderHash, openN, openKeys = e.Hash, e.Open, e.Keys
		case journal.EvReview:
			rev = &events[i]
		}
	}
	return author, renderHash, openN, openKeys, rev, nil
}

// crossesApproval reports whether a move crosses the approval boundary —
// the edge the review gate guards. Forward skips are legal, so draft→active
// crosses it exactly like submitted→approved does — and so does a REJECTED
// revival straight to approved/active: the first draft's pre-set covered
// only draft|submitted, and move to=rejected followed by move to=approved
// reached approved with no grill and no verdict in two calls (cross-
// verification of this task; the pre-change stamp gate refused that lane).
// Anything not already post-approval crosses when the target is.
func crossesApproval(from, to string) bool {
	post := func(s string) bool {
		return s == item.StateApproved || s == item.StateActive ||
			s == item.StateDone || s == item.StateArchived
	}
	return !post(from) && post(to)
}

// reviewGateGap answers what still stands between a proposal and approval:
// "" when a passing, identity-valid, current-body review verdict exists.
func (s *Server) reviewGateGap(it item.Item) (string, error) {
	if it.Grilled == "" {
		return "ungrilled — grill first", nil
	}
	author, _, _, _, rev, err := s.reviewState(it.ID)
	if err != nil {
		return "", err
	}
	switch {
	case rev == nil:
		return "no review verdict — grill op=verdict from a second identity", nil
	case rev.Hash != reviewHash(it):
		return "stale review (body edited since) — re-grill, then re-verdict", nil
	case !rev.Pass:
		return "failing review — address its findings, re-grill, re-verdict", nil
	case author != "" && rev.Ag == author:
		return "review by the author — a second identity must judge it", nil
	case reEphemeralAgent.MatchString(rev.Ag):
		return "anonymous review — verdicts need a deliberate SPECTACKLE_AGENT", nil
	}
	return "", nil
}

// grillVerdict records the independent review verdict: a journal event
// bound to the reviewer's identity and the body it judged. The server
// cannot create independence — it refuses the ways independence is
// FORGOTTEN. IDENTITY LIMIT, stated per the brief: what the refusals
// compute is ag-string divergence, not independence. They defend against
// forgetting to use a separate reviewer — the failure mode R-0007
// documented — not against a driver minting a second name purely to clear
// the gate while sharing every blind spot. That residual is accepted and
// stated; process guidance (different model or fresh context for
// reviewers) lives in the workflow docs, outside what the server can
// verify. RESIDENT-SERVER corollary: a shared resident connection carries
// ONE identity for all its callers (B-0002 lineage), so verdicts are
// per-call stdio operations with an explicitly set SPECTACKLE_AGENT.
func (s *Server) grillVerdict(in grillIn) (*mcp.CallToolResult, any, error) {
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
		return refuse("! REVIEW E " + short + " anonymous reviewer - set SPECTACKLE_AGENT to a deliberate name")
	}
	if in.Pass == nil {
		return refuse("! ARG E - verdict requires pass")
	}
	author, renderHash, openN, openKeys, _, err := s.reviewState(it.ID)
	if err != nil {
		return nil, nil, err
	}
	if author != "" && author == s.agent {
		return refuse("! REVIEW E " + short + " reviewer is the author - use a fresh agent identity")
	}
	cur := reviewHash(it)
	if renderHash != cur {
		return refuse("! REVIEW E " + short + " no pack rendered for the current body - grill it first")
	}
	// A failing verdict must say why BEFORE addressal is judged — the
	// findings are the author's next brief, and an empty brief is its own
	// refusal, clearer than an unaddressed-keys list.
	if !*in.Pass && strings.TrimSpace(in.Findings) == "" {
		return refuse("! REVIEW E " + short + " a failing verdict must say why - the findings become the author's next brief")
	}
	// Per-finding addressal (T-01KYD9J): the verdict is authoritative, the
	// computed findings are evidence requiring addressal — fixed (absent
	// from the current render) or waived with a recorded reason; on fail,
	// naming the subject in the findings counts (the machine guarantees
	// the reviewer SAW everything; it never overrules).
	gap, wv, ignored := addressalGap(openN, openKeys, *in.Pass, in.Findings, in.Waivers)
	if gap != "" {
		return refuse("! REVIEW E " + short + " " + gap)
	}
	warnIgnored := ""
	for _, k := range ignored {
		warnIgnored += "! REVIEW W waiver for absent key " + k + " ignored (not in the current render)\n"
	}
	// The findings floor is deliberately ASYMMETRIC (stated per the
	// cross-verification): a fail must say why — the findings are the
	// author's next brief — and a thin fail draws the tripwire warn; a
	// pass needs no essay, because mandatory pass-findings would breed
	// ritual words of exactly the deleted species. The engagement-free
	// pass is covered by the sockpuppet residual in the IDENTITY LIMIT
	// comment, not by prose quotas.
	warn := ""
	if !*in.Pass {
		if len(in.Findings) < 80 {
			warn = "! REVIEW W findings under 80 chars - token-thin reviews are a known tell (tripwire, padding-gameable)\n"
		}
	}
	// Lens labels (T-01KYFXDC6): the sequential single-reviewer default —
	// one context walking the configured lenses with explicit perspective
	// resets — records WHICH lenses were walked; the names are the
	// reviewer's vocabulary, unvalidated, but an empty label is an error.
	var lenses []string
	if strings.TrimSpace(in.Lenses) != "" {
		for _, l := range strings.Split(in.Lenses, ",") {
			l = strings.TrimSpace(l)
			if l == "" {
				return refuse("! ARG E - empty lens label in lenses")
			}
			lenses = append(lenses, l)
		}
	}
	// Panel opt-in (per item, never per gate): a multi-agent panel is
	// evidence breadth, not consensus voting — the gate still needs one
	// passing verdict. Legal ONLY on a live risk signal; config CAPS the
	// size and can never raise a panel that was not item-justified.
	if in.Panel > 1 {
		risk := ""
		for _, k := range openKeys {
			if strings.HasPrefix(k, "irreversible:") || strings.HasPrefix(k, "blast:") {
				risk = k
				break
			}
		}
		if risk == "" && it.Override {
			risk = "override-once spent"
		}
		if risk == "" {
			return refuse("! REVIEW E " + short + " panel needs a live risk signal (open irreversible/blast finding or override-once spent) - none present")
		}
		max := s.ws.Cfg.Swarm.PanelMax
		if max <= 0 {
			max = 3
		}
		if in.Panel > max {
			return refuse(fmt.Sprintf("! REVIEW E %s panel=%d exceeds swarm.panel_max=%d - config caps, never raises", short, in.Panel, max))
		}
	}
	if err := journal.Append(s.ws, it.Dir, journal.Event{
		Ev: journal.EvReview, ID: it.ID, Dir: it.Dir,
		Pass: *in.Pass, Hash: cur, Note: in.Findings, Wv: wv, Ln: lenses,
	}); err != nil {
		return nil, nil, err
	}
	verdict := "fail"
	if *in.Pass {
		verdict = "pass"
	}
	_ = s.cd.Emit("review", it.ID, verdict+" by "+s.agent)
	s.markDirty()
	lensNote := ""
	if len(lenses) > 0 {
		lensNote = " lenses=" + strings.Join(lenses, ",")
	}
	return text(warnIgnored + warn + "ok review " + short + " " + verdict + " by " + s.agent + lensNote)
}

// grillEvidence runs the target-scoped sweeps with the item's unconsumed-ok
// suppressions parsed from its body: per-symbol, reasoned, visible in the
// pack — never a blanket toggle, and stale directives are flagged so a
// suppression cannot outlive its reason (T-01KYD88KE).
func (s *Server) grillEvidence(it item.Item) []string {
	var paths []string
	for _, t := range it.Targets {
		if p, ok := targetPath(t); ok {
			paths = append(paths, p)
		}
	}
	if len(paths) == 0 {
		return nil
	}
	suppressed := map[string]string{}
	for _, l := range strings.Split(it.Body, "\n") {
		l = strings.TrimSpace(l)
		if rest, ok := strings.CutPrefix(l, "unconsumed-ok: "); ok {
			sym, reason, _ := strings.Cut(rest, " ")
			suppressed[sym] = strings.TrimSpace(reason)
		}
	}
	out := evidence.Unconsumed(s.g, paths, suppressed)
	out = append(out, evidence.DivergentCallers(s.g, paths, func(rel string) []byte {
		data, err := os.ReadFile(filepath.Join(s.ws.Dir, filepath.FromSlash(rel)))
		if err != nil {
			return nil
		}
		return data
	})...)
	return out
}

// --- Ambiguity classes (T-01KYFXBY, P-01KYES / ADR-01KYES0TR) ---
//
// A vague requirement must force an interactive user round-trip or an
// accountable waiver, never a silent guess. All three classes compute from
// post-deletion signals — body size, coverage novelty, graph incoherence —
// never word presence, which T-01KYD94KP4 deleted as padding-gameable.

// ambThinFloor is the flattened-body byte floor for task/proposal drafts.
// Calibrated 2026-07-27 against the 22 task/proposal bodies in this repo's
// reject corpus: minimum 418 bytes, median ~4.4KB — the floor fires on
// none of them.
const ambThinFloor = 400

// grillAmbiguity computes the amb-* findings and resolves their ask
// closure: any cited decide-minted ADR- ref that is done or archived
// closes ALL ambiguity findings mechanically (the structural-ref
// precedent of class e); a cited but still-open ADR renders the findings
// as awaiting that decision. Waiver closure is the ordinary per-finding
// machinery and needs nothing here.
func (s *Server) grillAmbiguity(it item.Item, c *spec.Cascade, historyHits int) []string {
	if it.State != item.StateDraft || (it.Kind != "task" && it.Kind != "proposal") {
		return nil
	}
	var out []string
	if n := len([]byte(strings.Join(strings.Fields(it.Body), " "))); n < ambThinFloor {
		out = append(out, fmt.Sprintf("g amb-thin body %dB < %dB floor", n, ambThinFloor))
	}
	var paths []string
	for _, t := range it.Targets {
		if p, ok := targetPath(t); ok {
			paths = append(paths, p)
		}
	}
	if len(paths) > 0 && historyHits == 0 {
		uncovered := s.uncoveredPackages(c, "")
		isUncovered := func(p string) bool {
			for _, u := range uncovered {
				if p == u || strings.HasPrefix(p, u+"/") {
					return true
				}
			}
			return false
		}
		all := true
		for _, p := range paths {
			if !isUncovered(p) {
				all = false
				break
			}
		}
		if all {
			out = append(out, "g amb-novel all targets uncovered, zero prior art")
		}
	}
	if incoherentTargets(s.g, paths) {
		out = append(out, "g amb-incoherent targets span unlinked subtrees")
	}
	if len(out) == 0 {
		return nil
	}
	// Ask closure: resolve cited ADRs. Done/archived closes; open renders
	// awaiting. Archived ADRs tombstone out of work.md, so absence after a
	// lookup miss means archived-or-unknown — the tombstone check is the
	// history search the caller already ran for novelty; treat a cited but
	// unloadable ADR as closed only when the journal knows its archive.
	for _, r := range it.Refs {
		if !strings.HasPrefix(r, "ADR-") {
			continue
		}
		adr, ok, err := item.Get(s.ws, r)
		if err == nil && ok {
			if adr.State == item.StateDone || adr.State == item.StateArchived {
				return nil // decision landed — ambiguity resolved
			}
			for i := range out {
				out[i] += " awaiting " + r[:min(13, len(r))]
			}
			return out
		}
		if s.archivedInJournal(r) {
			return nil
		}
	}
	return out
}

// incoherentTargets: three or more target paths spanning three or more
// top-level dirs where no pair is connected in the graph (bounded BFS,
// depth 2, both directions). Two targets never fire it.
func incoherentTargets(g graph.Graph, paths []string) bool {
	if len(paths) < 3 {
		return false
	}
	tops := map[string]bool{}
	for _, p := range paths {
		tops[strings.SplitN(p, "/", 2)[0]] = true
	}
	if len(tops) < 3 {
		return false
	}
	under := func(file, p string) bool {
		return file == p || strings.HasPrefix(file, p+"/") || strings.HasPrefix(file, p+".")
	}
	seedsFor := func(p string) []graph.NodeID {
		base := path.Base(strings.TrimSuffix(p, ".go"))
		var ids []graph.NodeID
		for _, n := range g.Find(base, 25, graph.KUnknown) {
			if under(n.File, p) || under(n.File, path.Dir(p)) {
				ids = append(ids, n.ID)
			}
		}
		return ids
	}
	for i := 0; i < len(paths); i++ {
		seeds := seedsFor(paths[i])
		if len(seeds) == 0 {
			continue
		}
		nodes, _ := g.Impact(seeds, 2, graph.Both, nil)
		for j := 0; j < len(paths); j++ {
			if i == j {
				continue
			}
			for _, n := range nodes {
				if under(n.File, paths[j]) || under(n.File, path.Dir(paths[j])) {
					return false // one connected pair = coherent
				}
			}
		}
	}
	return true
}

// archivedInJournal reports whether the journal carries an archive event
// for id — the tombstone lookup for ADRs that left work.md.
func (s *Server) archivedInJournal(id string) bool {
	events, err := journal.ReadAll(s.ws)
	if err != nil {
		return false
	}
	for _, e := range events {
		if e.Ev == journal.EvArchive && e.ID == id {
			return true
		}
	}
	return false
}
