package mcpserver

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jxsl13/spectackle/internal/ears"
	"github.com/jxsl13/spectackle/internal/item"
	"github.com/jxsl13/spectackle/internal/journal"
	"github.com/jxsl13/spectackle/internal/knowledge"
	"github.com/jxsl13/spectackle/internal/lifecycle"
	"github.com/jxsl13/spectackle/internal/spec"
)

// knowledge — the fleet-portable-knowledge tool: one noun, three ops
// (export|merge|apply), the same shape find/rule/lease/work already use
// instead of three separate tools (SPX-ARC-004's "few tools" principle).
// internal/knowledge does the actual lifting (Extract/Merge/NewEntry); this
// file only wires it to a workspace, exactly the way commands.go wires
// internal/mcpserver/templates to disk. See knowledgeIn's field docs for the
// three ops' distinct parameter subsets.

// knowledgeIn is registered in tools.go's registerTools() (the "input
// struct" lives here, in the sibling-file pattern decide.go/commands.go
// already use — decideIn and commandsIn are not in tools.go either).
type knowledgeIn struct {
	Op string `json:"op" jsonschema:"export|merge|apply"`

	// export
	Source  string              `json:"source,omitempty" jsonschema:"export: repository label recorded as provenance; default this binary's own module path"`
	Entries []knowledgeEntryIn  `json:"entries,omitempty" jsonschema:"export brownfield path: caller-authored entries (no .spectackle bundle to extract from) — routed through NewEntry, keyed and validated identically to an extracted entry; never a caller-chosen key"`
	Out     string              `json:"out,omitempty" jsonschema:"export: also write the marshaled artifact to this path (fleet workflows need a file to move between repos), in addition to returning it inline"`

	// merge | apply
	Paths     []string `json:"paths,omitempty" jsonschema:"merge|apply: artifact file paths to read"`
	Artifacts []string `json:"artifacts,omitempty" jsonschema:"merge|apply: inline marshaled artifacts (as returned by export/merge)"`

	// apply
	Dir  string `json:"dir,omitempty" jsonschema:"apply: context dir every added entry lands in, default root"`
	Stem string `json:"stem,omitempty" jsonschema:"apply: rule ID stem for added rules; default derived from the artifact's first source label"`
}

// knowledgeEntryIn is the wire shape of one caller-authored Entry for
// export's brownfield path — a flat superset of every kind's payload
// fields (only the ones matching Kind are read), plus the two provenance
// lists NewEntry requires. There is deliberately no key/id field: NewEntry
// computes the content key itself, so a caller cannot supply one even by
// mistake.
type knowledgeEntryIn struct {
	Kind string `json:"kind" jsonschema:"rule|adr|intent"`

	Text      string `json:"text,omitempty" jsonschema:"rule: the EARS sentence, verbatim"`
	Rationale string `json:"rationale,omitempty" jsonschema:"rule: optional rationale"`

	Question     string   `json:"question,omitempty" jsonschema:"adr: the question decided"`
	Context      string   `json:"context,omitempty" jsonschema:"adr: forces and constraints behind the decision"`
	Decision     string   `json:"decision,omitempty" jsonschema:"adr: the chosen option"`
	Consequences string   `json:"consequences,omitempty" jsonschema:"adr: trade-offs and follow-on effects"`
	Status       string   `json:"status,omitempty" jsonschema:"adr: proposed|accepted|superseded|deprecated"`
	Options      []string `json:"options,omitempty" jsonschema:"adr: rejected alternatives"`

	Prose string `json:"prose,omitempty" jsonschema:"intent: prose section text, verbatim"`

	AssertedBy  []string `json:"asserted_by,omitempty" jsonschema:"repo labels whose own files literally assert this entry"`
	DerivedFrom []string `json:"derived_from,omitempty" jsonschema:"repo labels an LLM drew on to generalize this entry (no single repo asserts it verbatim)"`
}

// toEntry routes one wire entry through knowledge.NewEntry — the same
// validating, key-computing constructor an LLM-authored generalization
// uses — so a caller-supplied entry is keyed identically to an Extracted
// one (see TestKnowledgeExportBrownfieldKeyMatchesExtract).
func (e knowledgeEntryIn) toEntry() (knowledge.Entry, error) {
	payload := knowledge.Entry{
		Text: e.Text, Rationale: e.Rationale,
		Question: e.Question, Context: e.Context, Decision: e.Decision,
		Consequences: e.Consequences, Status: e.Status, Options: e.Options,
		Prose: e.Prose,
	}
	return knowledge.NewEntry(knowledge.EntryKind(e.Kind), payload, provenanceOf(e.AssertedBy), provenanceOf(e.DerivedFrom))
}

func provenanceOf(sources []string) []knowledge.Provenance {
	if len(sources) == 0 {
		return nil
	}
	out := make([]knowledge.Provenance, len(sources))
	for i, s := range sources {
		out[i] = knowledge.Provenance{Source: s}
	}
	return out
}

func (s *Server) knowledgeTool(in knowledgeIn) (*mcp.CallToolResult, any, error) {
	switch in.Op {
	case "export":
		return s.knowledgeExport(in)
	case "merge":
		return s.knowledgeMerge(in)
	case "apply":
		return s.knowledgeApply(in)
	}
	return text("! ARG E - op must be export|merge|apply")
}

// ---- export ----

// exportSourceDefault derives the default `source` label for op=export the
// same way moduleRepoURL derives its https:// URL — debug.ReadBuildInfo,
// modulePath fallback (server.go) — reused directly rather than
// re-implementing the ReadBuildInfo dance, per the task brief ("reuse that
// approach, do not hardcode"). An artifact's Source is a bare repo label
// like a Provenance.Source string, not a clickable URL, so the "https://"
// moduleRepoURL adds for humans is stripped back off here.
func exportSourceDefault() string {
	return strings.TrimPrefix(moduleRepoURL(), "https://")
}

func entryCounts(entries []knowledge.Entry) map[knowledge.EntryKind]int {
	counts := map[knowledge.EntryKind]int{}
	for _, e := range entries {
		counts[e.Kind]++
	}
	return counts
}

func (s *Server) knowledgeExport(in knowledgeIn) (*mcp.CallToolResult, any, error) {
	source := strings.TrimSpace(in.Source)
	if source == "" {
		source = exportSourceDefault()
	}

	var art knowledge.Artifact
	if len(in.Entries) > 0 {
		// brownfield path: no .spectackle bundle to walk, the caller (an LLM
		// that surveyed code/tests/docs) authored entries directly. Every
		// one is routed through NewEntry — validated and keyed exactly like
		// an Extracted entry, never trusting a caller-supplied identity.
		var entries []knowledge.Entry
		for i, ei := range in.Entries {
			e, err := ei.toEntry()
			if err != nil {
				return text(fmt.Sprintf("! ARG E - entries[%d]: %s", i, err.Error()))
			}
			entries = append(entries, e)
		}
		art = knowledge.Artifact{Sources: []string{source}, Entries: entries}
	} else {
		c, err := spec.Load(s.ws.Dir)
		if err != nil {
			return nil, nil, err
		}
		items, err := item.LoadAll(s.ws)
		if err != nil {
			return nil, nil, err
		}
		art, err = knowledge.Extract(c, items, source)
		if err != nil {
			return nil, nil, err
		}
	}

	out, err := knowledge.Marshal(art)
	if err != nil {
		return nil, nil, err
	}

	counts := entryCounts(art.Entries)
	var b strings.Builder
	fmt.Fprintf(&b, "ok export %s entries=%d rule=%d adr=%d intent=%d\n",
		source, len(art.Entries), counts[knowledge.KindRule], counts[knowledge.KindADR], counts[knowledge.KindIntent])

	if in.Out != "" {
		if err := s.writeArtifactOut(in.Out, out); err != nil {
			return text("! ARG E - " + err.Error())
		}
		fmt.Fprintf(&b, "ok wrote %s\n", in.Out)
	}
	b.WriteString("\n")
	b.Write(out)
	return text(b.String())
}

// writeArtifactOut refuses a destination inside .spectackle: those folders
// are server territory (SPX-ARC-005) written only through the versioned
// lifecycle paths, and an exported artifact is neither a spec nor an item —
// it is a plain file meant to travel between repositories, so it belongs
// anywhere else on disk (matching how commands op=gen writes generated
// repo files straight to disk, outside .spectackle, per its own doc).
func (s *Server) writeArtifactOut(path string, data []byte) error {
	abs := path
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(s.ws.Dir, path)
	}
	rel, err := filepath.Rel(s.ws.Dir, abs)
	if err == nil {
		rel = filepath.ToSlash(rel)
		if rel == ".spectackle" || strings.HasPrefix(rel, ".spectackle/") {
			return fmt.Errorf("out must not be inside .spectackle")
		}
	}
	return os.WriteFile(abs, data, 0o644)
}

// ---- merge ----

func (s *Server) knowledgeMerge(in knowledgeIn) (*mcp.CallToolResult, any, error) {
	arts, err := loadArtifacts(in.Paths, in.Artifacts)
	if err != nil {
		return text("! ARG E - " + err.Error())
	}
	if len(arts) == 0 {
		return text("! ARG E - merge requires paths or artifacts")
	}

	merged, conflicts, err := knowledge.Merge(arts...)
	if err != nil {
		return nil, nil, err
	}
	out, err := knowledge.Marshal(merged)
	if err != nil {
		return nil, nil, err
	}

	counts := entryCounts(merged.Entries)
	var b strings.Builder
	fmt.Fprintf(&b, "ok merge sources=%d entries=%d rule=%d adr=%d intent=%d conflicts=%d\n",
		len(merged.Sources), len(merged.Entries), counts[knowledge.KindRule], counts[knowledge.KindADR], counts[knowledge.KindIntent], len(conflicts))
	for _, cf := range conflicts {
		fmt.Fprintf(&b, "cf %s %s n=%d\n", cf.Kind, cf.Key, len(cf.Entries))
		for _, e := range cf.Entries {
			fmt.Fprintf(&b, "cf> count=%d %s\n", e.Count, conflictPreview(e))
		}
	}
	b.WriteString("\n")
	b.Write(out)
	return text(b.String())
}

// conflictPreview renders one conflicting entry's substance densely — never
// the full entry as a JSON blob (SPX-MCP-002) — so a human curating a merge
// result can tell the two answers apart without a separate get call.
func conflictPreview(e knowledge.Entry) string {
	switch e.Kind {
	case knowledge.KindADR:
		return fmt.Sprintf("decision=%q sources=%s", e.Decision, sourceLabels(e.Sources, e.DerivedFrom))
	case knowledge.KindRule:
		return fmt.Sprintf("text=%q sources=%s", e.Text, sourceLabels(e.Sources, e.DerivedFrom))
	default:
		return fmt.Sprintf("prose=%q sources=%s", e.Prose, sourceLabels(e.Sources, e.DerivedFrom))
	}
}

func sourceLabels(pss ...[]knowledge.Provenance) string {
	seen := map[string]bool{}
	var out []string
	for _, ps := range pss {
		for _, p := range ps {
			if !seen[p.Source] {
				seen[p.Source] = true
				out = append(out, p.Source)
			}
		}
	}
	if len(out) == 0 {
		return "-"
	}
	return strings.Join(out, ",")
}

// loadArtifacts parses artifacts from file paths and/or inline marshaled
// text, in that order — shared by merge and apply, neither of which needs
// server/workspace state to do it.
func loadArtifacts(paths, inline []string) ([]knowledge.Artifact, error) {
	var out []knowledge.Artifact
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", p, err)
		}
		a, err := knowledge.Parse(data)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", p, err)
		}
		out = append(out, a)
	}
	for i, raw := range inline {
		a, err := knowledge.Parse([]byte(raw))
		if err != nil {
			return nil, fmt.Errorf("parse artifacts[%d]: %w", i, err)
		}
		out = append(out, a)
	}
	return out, nil
}

// ---- apply ----

var reStemJunk = regexp.MustCompile(`[^A-Z0-9]+`)

// defaultStem derives a rule ID stem from an artifact's first source label
// when the caller does not pass one — the last path segment (so
// "github.com/acme/repo" becomes "REPO"), uppercased, non-alnum stripped,
// capped at 8 chars; "KNOW" when that yields nothing usable (no sources, or
// a label with no alnum content).
func defaultStem(sources []string) string {
	if len(sources) == 0 {
		return "KNOW"
	}
	s := sources[0]
	if i := strings.LastIndexByte(s, '/'); i >= 0 {
		s = s[i+1:]
	}
	s = reStemJunk.ReplaceAllString(strings.ToUpper(s), "")
	if s == "" {
		return "KNOW"
	}
	if len(s) > 8 {
		s = s[:8]
	}
	return s
}

// knowledgeADRBody renders an applied ADR entry's Options the same way
// decideAsk writes them (`option: <text>` lines) so item.ParseOptions —
// and, on a later export, knowledge.Extract itself — reads them back
// byte-identically. There is no `kind:`/`blocks:` line: those describe an
// interactive decide op=ask session this entry never went through.
func knowledgeADRBody(e knowledge.Entry) string {
	if len(e.Options) == 0 {
		return ""
	}
	lines := make([]string, len(e.Options))
	for i, o := range e.Options {
		lines[i] = "option: " + o
	}
	return strings.Join(lines, "\n")
}

func (s *Server) knowledgeApply(in knowledgeIn) (*mcp.CallToolResult, any, error) {
	arts, err := loadArtifacts(in.Paths, in.Artifacts)
	if err != nil {
		return text("! ARG E - " + err.Error())
	}
	if len(arts) != 1 {
		return text("! ARG E - apply requires exactly one artifact (path or inline)")
	}
	incoming := arts[0]

	dir := dirOf(in.Dir)
	if s.wtItem == "" {
		if res, out, err := s.blockedByLease([]string{dir}); res != nil || err != nil {
			return res, out, err
		}
	}

	c, err := spec.Load(s.ws.Dir)
	if err != nil {
		return nil, nil, err
	}
	items, err := item.LoadAll(s.ws)
	if err != nil {
		return nil, nil, err
	}
	// current is THIS workspace's own knowledge, extracted exactly like
	// export's default path would — the additive-diff baseline. source
	// (s.ws.Dir) is immaterial here: FoldInto only compares (Kind, Key)
	// identity, never Sources.
	current, err := knowledge.Extract(c, items, s.ws.Dir)
	if err != nil {
		return nil, nil, err
	}

	plan := knowledge.FoldInto(current, incoming)

	stem := strings.TrimSpace(in.Stem)
	if stem == "" {
		stem = defaultStem(incoming.Sources)
	}

	defer s.scan.MarkDirty()
	var b strings.Builder
	added, unsupported := 0, 0
	for _, e := range plan.Entries {
		switch e.Kind {
		case knowledge.KindRule:
			res, aerr := spec.AddRule(s.ws, c, spec.AuthorReq{
				Dir: dir, Stem: stem, Mint: s.ruleMinter(),
				Sentence: e.Text, Rationale: e.Rationale,
				// Applies is deliberately omitted: an applied rule arrives
				// with no binding to this repo's code — check's coverage
				// pass is exactly the adoption worklist that leaves open
				// (see the gaps= trailer below, MCP-009).
			})
			if aerr != nil {
				return text("! ARG E - " + aerr.Error())
			}
			if !res.Written {
				var fb strings.Builder
				for _, f := range res.Findings {
					fb.WriteString(f.String() + "\n")
				}
				fmt.Fprintf(&fb, "! REJECTED E - fold stopped before entry %s: lint failed\n", e.Key)
				return text(fb.String())
			}
			s.journalRule("add", res.ID, e.Text, nil, "", dir)
			fmt.Fprintf(&b, "r %s %s %s %s\n", res.ID, ears.Classify(e.Text), orDot(dir), e.Text)
			added++
		case knowledge.KindADR:
			it, derr := lifecycle.Draft(s.ws, s.minter(), "adr", e.Question, knowledgeADRBody(e), dir, "", nil)
			if derr != nil {
				return text("! ARG E - " + derr.Error())
			}
			it.Context = e.Context
			it.Decision = e.Decision
			it.Consequences = e.Consequences
			it.Status = e.Status
			if it.Status == "" {
				it.Status = "accepted"
			}
			// The decision already happened, in the repo this artifact came
			// from — apply persists its outcome, it does not re-ask the
			// question (no Session available in a fleet/apply call, and
			// nothing to elicit: see resolveDecision in decide.go, whose
			// direct state=done + item.Upsert this mirrors instead of
			// routing through lifecycle.Move).
			it.State = item.StateDone
			if err := item.Upsert(s.ws, it); err != nil {
				return nil, nil, err
			}
			_ = journal.Append(s.ws, dir, journal.Event{
				Ev: journal.EvDecide, ID: it.ID, Dir: dir, Note: "knowledge apply",
			})
			_ = s.cd.Emit("decide", it.ID, "knowledge apply: "+e.Question)
			b.WriteString(item.Record(it) + "\n")
			added++
		default:
			// KindIntent: Extract keys a whole prose SECTION as one entry
			// (drift.NormHash of the section's full text), but the only
			// existing write path (spec.AppendIntent) is additive AT THE
			// LINE level into `## intent` specifically. Folding a
			// whole-section blob through a line-additive API would break
			// FoldInto's own idempotence contract on the next apply
			// (re-extracting the now-combined section produces a different
			// key than the standalone entry that was folded in) — see
			// apply_test.go / knowledge_test.go and the task report for the
			// full reasoning. Deliberately unsupported for now rather than
			// silently wrong: surfaced as its own count, not dropped mute.
			unsupported++
		}
	}

	// gaps: check's own coverage-gap count (g uncovered — see
	// coverageGaps/check in tools.go, untouched by this task), recomputed
	// against the now-written cascade and reported in the same call so the
	// caller does not need a separate check round trip to see the adoption
	// worklist this fold leaves open (MCP-009). Whole-workspace, not scoped
	// to dir: an applied rule only ever closes coverage for the exact
	// directory it lands in (it has no applies binding to open anything
	// there), so the number worth surfacing is what is STILL uncovered
	// repo-wide, not a delta this call caused.
	gaps := 0
	if c2, cerr := spec.Load(s.ws.Dir); cerr == nil {
		gaps = len(s.coverageGaps(c2, ""))
	}

	fmt.Fprintf(&b, "ok added=%d skipped=%d gaps=%d", added, plan.Skipped, gaps)
	if unsupported > 0 {
		fmt.Fprintf(&b, " unsupported=%d", unsupported)
	}
	b.WriteString("\n")
	return text(b.String())
}
