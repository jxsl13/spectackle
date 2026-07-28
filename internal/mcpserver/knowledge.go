package mcpserver

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jxsl13/spectackle/internal/drift"
	"github.com/jxsl13/spectackle/internal/ears"
	"github.com/jxsl13/spectackle/internal/item"
	"github.com/jxsl13/spectackle/internal/journal"
	"github.com/jxsl13/spectackle/internal/knowledge"
	"github.com/jxsl13/spectackle/internal/lifecycle"
	"github.com/jxsl13/spectackle/internal/spec"
	"github.com/jxsl13/spectackle/internal/wt"
)

// knowledge exposes internal/knowledge (a finished, standalone package —
// this file consumes it, it does not redesign it) as one MCP tool with
// three operations, matching how decide/rule/lease/work already group
// several ops under one noun instead of growing the tool count (SPX-ARC-004
// / MCP-009's standing constraint).
//
// export produces a portable artifact for THIS workspace: either lifted
// from the live cascade+items (knowledge.Extract), or — for a repository
// with no .spectackle bundle at all — assembled from caller-authored
// entries, each validated and content-keyed by knowledge.NewEntry so a
// caller-supplied key is impossible (NewEntry does not even accept one).
//
// merge condenses N artifacts and reports every conflict as a dense
// record — never auto-resolved (that curation step is deliberately left to
// a human or a later tool; see knowledge.Resolve/Apply for when it lands).
//
// apply is the only writing operation: it folds one artifact into this
// workspace, additively and idempotently, through the composers that
// already exist for rules (rule op=add's spec.AddRule) and ADRs (the same
// Draft/Upsert/journal primitives decide.go's own resolveDecision uses) —
// never a new write path onto .spectackle files.
func (s *Server) knowledge(in knowledgeIn) (*mcp.CallToolResult, any, error) {
	switch in.Op {
	case "export":
		return s.knowledgeExport(in)
	case "merge":
		return s.knowledgeMerge(in)
	case "apply":
		return s.knowledgeApply(in)
	}
	return refuse("! ARG E - op must be export|merge|apply")
}

// sourceLabel identifies THE WORKSPACE BEING EXPORTED — the provenance
// knowledge.Extract/NewEntry record on every entry.
//
// It used to reuse moduleRepoURL (debug.ReadBuildInfo), which names the
// RUNNING BINARY's module: every repo a shared spectackle installation
// touched exported the identical label, so a merge of two repositories
// reported sources=1 and unionProvenance — keyed on (Source, Dir) —
// collapsed the same rule found independently in both into count 1,
// silently undercounting the recurrence rank that IS the artifact's
// headline signal. Two conflicting decisions also rendered with the same
// src=, leaving a human unable to tell which repo said what
// (B-01KYMCHNJCFBP, measured by an independent gap hunt).
//
// Order: the git remote (the durable cross-machine identity), else the
// workspace directory name, else the build-info module path — a repo with
// no remote and no name is the only case where the old behavior remains.
func (s *Server) sourceLabel() string {
	if u, err := wt.RemoteURL(s.ws.Dir, s.effectiveGit().Remote); err == nil {
		if lbl := normalizeRepoLabel(u); lbl != "" {
			return lbl
		}
	}
	if base := filepath.Base(strings.TrimRight(s.ws.Dir, "/")); base != "" && base != "." && base != "/" {
		return base
	}
	return strings.TrimPrefix(moduleRepoURL(), "https://")
}

// normalizeRepoLabel turns a git remote URL into a stable host/path label:
// scheme, credentials, the scp-style colon and any .git suffix all drop,
// so https://github.com/o/r.git, git@github.com:o/r.git and
// ssh://git@github.com/o/r all label identically — two clones of one
// repository must never look like two sources.
func normalizeRepoLabel(u string) string {
	u = strings.TrimSpace(u)
	if u == "" {
		return ""
	}
	if i := strings.Index(u, "://"); i >= 0 {
		u = u[i+3:]
	}
	if i := strings.LastIndex(u, "@"); i >= 0 {
		u = u[i+1:] // strip credentials or the scp-style user
	}
	u = strings.Replace(u, ":", "/", 1) // scp-style host:path
	u = strings.TrimSuffix(strings.TrimRight(u, "/"), ".git")
	return u
}

// ---- export ----

func (s *Server) knowledgeExport(in knowledgeIn) (*mcp.CallToolResult, any, error) {
	var a knowledge.Artifact
	if len(in.Entries) == 0 {
		// mode (a): walk this workspace's own cascade + items.
		c, err := spec.Load(s.ws.Dir)
		if err != nil {
			return nil, nil, err
		}
		items, err := item.LoadAll(s.ws)
		if err != nil {
			return nil, nil, err
		}
		a, err = knowledge.Extract(c, items, s.sourceLabel())
		if err != nil {
			return nil, nil, err
		}
	} else {
		// mode (b), brownfield: caller-authored entries, no bundle to walk.
		// Every entry is routed through knowledge.NewEntry — validated and
		// content-keyed identically to an Extracted one; there is no key
		// field on knowledgeEntryIn for a caller to even attempt supplying.
		source := s.sourceLabel()
		entries := make([]knowledge.Entry, 0, len(in.Entries))
		for i, e := range in.Entries {
			entry, err := knowledgeEntryFromIn(e, source)
			if err != nil {
				return refuse(fmt.Sprintf("! ARG E - entries[%d]: %s\n", i, err.Error()))
			}
			entries = append(entries, entry)
		}
		a = knowledge.Artifact{Sources: []string{source}, Entries: entries}
	}

	out, err := knowledge.Marshal(a)
	if err != nil {
		return nil, nil, err
	}
	if in.Path != "" {
		if err := os.WriteFile(in.Path, out, 0o644); err != nil {
			return refuse("! IO E - " + err.Error())
		}
	}

	var b strings.Builder
	b.Write(out)
	if !strings.HasSuffix(b.String(), "\n") {
		b.WriteString("\n")
	}
	counts := knowledgeCounts(a.Entries)
	fmt.Fprintf(&b, "ok export entries=%d rule=%d adr=%d intent=%d",
		len(a.Entries), counts[knowledge.KindRule], counts[knowledge.KindADR], counts[knowledge.KindIntent])
	if in.Path != "" {
		fmt.Fprintf(&b, " written=%s", in.Path)
	}
	b.WriteString("\n")
	return text(b.String())
}

// knowledgeEntryFromIn maps one caller-supplied brownfield entry onto
// knowledge.NewEntry's call shape: only the payload fields relevant to Kind
// matter (NewEntry itself ignores the rest), and this entry is always
// asserted-by (never derived-from) source — a brownfield survey of THIS
// repository's own code/tests/docs is exactly the "this repository's files
// assert it" case, not an LLM generalizing across several repositories
// (that is Merge's job, later, on artifacts this op produces).
func knowledgeEntryFromIn(e knowledgeEntryIn, source string) (knowledge.Entry, error) {
	kind := knowledge.EntryKind(strings.ToLower(strings.TrimSpace(e.Kind)))
	payload := knowledge.Entry{
		Text: e.Text, Rationale: e.Rationale,
		Question: e.Question, Context: e.Context, Decision: e.Decision,
		Consequences: e.Consequences, Status: e.Status, Options: e.Options,
		Prose: e.Prose,
	}
	prov := []knowledge.Provenance{{Source: source, Dir: dirOf(e.Dir)}}
	return knowledge.NewEntry(kind, payload, prov, nil)
}

func knowledgeCounts(entries []knowledge.Entry) map[knowledge.EntryKind]int {
	out := map[knowledge.EntryKind]int{}
	for _, e := range entries {
		out[e.Kind]++
	}
	return out
}

// ---- merge ----

func (s *Server) knowledgeMerge(in knowledgeIn) (*mcp.CallToolResult, any, error) {
	artifacts, err := knowledgeGatherArtifacts(in.Paths, in.Body)
	if err != nil {
		return refuse("! ARG E - " + err.Error())
	}
	if len(artifacts) == 0 {
		return refuse("! ARG E - merge requires paths and/or body (at least one artifact)")
	}

	merged, conflicts, err := knowledge.Merge(artifacts...)
	if err != nil {
		return nil, nil, err
	}
	out, err := knowledge.Marshal(merged)
	if err != nil {
		return nil, nil, err
	}

	var b strings.Builder
	b.Write(out)
	if !strings.HasSuffix(b.String(), "\n") {
		b.WriteString("\n")
	}
	// conflicts are reported, never auto-resolved (MCP-009 spirit for
	// apply carries over to merge too): one `x` record per competing
	// entry, grouped by their shared (kind, key) — a human reads both
	// sides intact and picks, exactly like knowledge.Conflict itself never
	// synthesizes a winner.
	for _, cf := range conflicts {
		for _, e := range cf.Entries {
			fmt.Fprintf(&b, "x %s %s src=%s %s\n",
				cf.Kind, cf.Key, strings.Join(provenanceSources(e), ","), knowledgeEntrySummary(e))
		}
	}
	fmt.Fprintf(&b, "ok merge sources=%d entries=%d conflicts=%d\n",
		len(merged.Sources), len(merged.Entries), len(conflicts))
	return text(b.String())
}

// knowledgeGatherArtifacts reads and parses every merge input: each of
// paths (a file per artifact — the fleet-transport shape export writes),
// plus body as one more inline artifact when non-empty. Order is
// paths-then-body, but Merge itself is order-independent (see its doc
// comment), so this only affects Conflict.Entries ordering, not correctness.
func knowledgeGatherArtifacts(paths []string, body string) ([]knowledge.Artifact, error) {
	var out []knowledge.Artifact
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", p, err)
		}
		a, err := knowledge.Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", p, err)
		}
		out = append(out, a)
	}
	if strings.TrimSpace(body) != "" {
		a, err := knowledge.Parse([]byte(body))
		if err != nil {
			return nil, fmt.Errorf("parse body: %w", err)
		}
		out = append(out, a)
	}
	return out, nil
}

// provenanceSources collapses one entry's Sources+DerivedFrom into a
// deduped, sorted list of repository labels — the compact `src=` field on
// an `x` conflict record; which of the two provenance kinds each label came
// from is already visible in the full artifact body printed above it.
func provenanceSources(e knowledge.Entry) []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range e.Sources {
		if !seen[p.Source] {
			seen[p.Source] = true
			out = append(out, p.Source)
		}
	}
	for _, p := range e.DerivedFrom {
		if !seen[p.Source] {
			seen[p.Source] = true
			out = append(out, p.Source)
		}
	}
	sort.Strings(out)
	return out
}

// knowledgeEntrySummary renders the substance that actually differs between
// two entries sharing a (kind, key) identity — see knowledge.Conflict's doc
// comment: for a rule/intent this never fires in practice (their Key
// already covers the whole payload), so Text/Prose in full is fine; for an
// ADR (the realistic case, Key == hash(Question) only) Decision/Status are
// exactly the fields substanceEqual compares.
func knowledgeEntrySummary(e knowledge.Entry) string {
	switch e.Kind {
	case knowledge.KindRule:
		return e.Text
	case knowledge.KindADR:
		return fmt.Sprintf("decision=%q status=%s", e.Decision, orDash(e.Status))
	case knowledge.KindIntent:
		return e.Prose
	default:
		return ""
	}
}

// ---- apply ----

// knowledgeImportStem is the rule-ID stem apply mints new rules under.
// spec.AddRule needs a stem to compose an ID from, and a portable entry
// carries none — rule-ID prefixes are repository-local, deliberately
// stripped by Extract (see internal/knowledge/extract.go). AddRule's own
// default (the stem of the last rule already in the target file) only
// resolves when that file already has a rule; an empty root spec.md — the
// common case for a workspace's first-ever apply — has none, so AddRule
// would otherwise refuse with "no ID stem". A fixed, distinct stem also
// makes imported rules recognizable at a glance (`find scope=rule q=KB`) as
// the ones still needing the dir/applies review check's coverage gaps
// exist to prompt (MCP-009).
const knowledgeImportStem = "KB"

func (s *Server) knowledgeApply(in knowledgeIn) (*mcp.CallToolResult, any, error) {
	var raw []byte
	switch {
	case in.Path != "":
		b, err := os.ReadFile(in.Path)
		if err != nil {
			return refuse("! ARG E - " + err.Error())
		}
		raw = b
	case strings.TrimSpace(in.Body) != "":
		raw = []byte(in.Body)
	default:
		return refuse("! ARG E - apply requires path or body")
	}
	incoming, err := knowledge.Parse(raw)
	if err != nil {
		return refuse("! ARG E - " + err.Error())
	}

	c, err := spec.Load(s.ws.Dir)
	if err != nil {
		return nil, nil, err
	}
	items, err := item.LoadAll(s.ws)
	if err != nil {
		return nil, nil, err
	}
	current, err := knowledge.Extract(c, items, s.sourceLabel())
	if err != nil {
		return nil, nil, err
	}

	// the additive delta, computed purely (internal/knowledge/apply.go):
	// every incoming entry this workspace's own artifact does not already
	// carry under the same (kind, key) identity. Applying the same
	// artifact twice sees an empty delta the second time — current, by
	// then, already reflects the first apply's writes.
	toAdd := knowledge.FoldInto(current, incoming)

	var b strings.Builder
	added := 0
	for _, e := range toAdd.Entries {
		var line string
		var ok bool
		var werr error
		switch e.Kind {
		case knowledge.KindRule:
			line, ok, werr = s.applyRuleEntry(e)
		case knowledge.KindADR:
			line, ok, werr = s.applyADREntry(e)
		case knowledge.KindIntent:
			line, ok, werr = s.applyIntentEntry(e)
		default:
			line = fmt.Sprintf("! ARG W - apply: unknown entry kind %q skipped\n", e.Kind)
		}
		if werr != nil {
			return nil, nil, werr
		}
		b.WriteString(line)
		if ok {
			added++
		}
	}

	// gaps are counted AFTER the writes land, from the same two
	// gap-producing computations `check` itself runs (coverageGaps +
	// orphan applies), so apply's own trailer is provably the same number
	// a standalone `check` call would report — never a guess.
	c2, err := spec.Load(s.ws.Dir)
	if err != nil {
		return nil, nil, err
	}
	gaps, err := s.knowledgeGapCount(c2)
	if err != nil {
		return nil, nil, err
	}

	fmt.Fprintf(&b, "ok applied added=%d gaps=%d\n", added, gaps)
	if added > 0 {
		s.markDirty()
	}
	return text(b.String())
}

// applyRuleEntry adds one rule entry through spec.AddRule — the exact
// composer rule op=add's own ruleAdd calls (tools.go) — at root scope with
// no applies binding: a portable entry carries neither a context dir nor
// node anchors (both are repository-local, stripped by Extract), so the
// rule lands universally scoped and unanchored. check's coverage-gap
// machinery (`g orphan`) then correctly has nothing to report for THIS
// rule specifically (no applies means no orphan pair), but the rule is
// real and lintable immediately — the anchoring itself is exactly the
// adoption work check's gap list exists to worklist (MCP-009). Journals
// through the same journalRule helper ruleAdd uses, so `find scope=history`
// and sibling swarm learnings see it identically to a hand-authored rule.
func (s *Server) applyRuleEntry(e knowledge.Entry) (string, bool, error) {
	c, err := spec.Load(s.ws.Dir)
	if err != nil {
		return "", false, err
	}
	res, err := spec.AddRule(s.ws, c, spec.AuthorReq{
		Dir: "", Stem: knowledgeImportStem, Mint: s.ruleMinter(),
		Sentence: e.Text, Rationale: e.Rationale,
	})
	if err != nil {
		return fmt.Sprintf("! ARG E - apply rule %s: %s\n", e.Key, err.Error()), false, nil
	}
	if !res.Written {
		var lb strings.Builder
		for _, f := range res.Findings {
			lb.WriteString(f.String() + "\n")
		}
		lb.WriteString("! REJECTED E - apply rule " + e.Key + " failed lint; skipped\n")
		return lb.String(), false, nil
	}
	s.journalRule("add", res.ID, e.Text, nil, "", "")
	p := ears.Classify(e.Text)
	return fmt.Sprintf("ok %s %s\nr %s %s . %s\n", res.ID, res.Path, res.ID, p, e.Text), true, nil
}

// applyADREntry adds one ADR entry through the same primitives decide.go's
// own decideAsk+resolveDecision use — lifecycle.Draft to mint the item,
// item.Upsert to persist its ADR fields, journal.Append(EvDecide) for
// history — rather than the decide tool's op=ask RPC itself: op=ask always
// attempts live MCP elicitation (req.Session.Elicit), which is exactly
// right for asking a human a question it does not yet know the answer to,
// and exactly wrong here — an applied ADR entry already carries its
// Context/Decision/Consequences/Status, so driving it through op=ask would
// pop an elicitation prompt at the user for a decision that has already
// been made (and, for a multi-entry apply, one prompt per ADR). Landing the
// item straight at state=done mirrors resolveDecision's own final step
// exactly (it also sets State directly + item.Upsert, not a lifecycle.Move
// call) — this is the decide path's own established pattern, not a new one.
func (s *Server) applyADREntry(e knowledge.Entry) (string, bool, error) {
	var bodyLines []string
	for _, o := range e.Options {
		bodyLines = append(bodyLines, "option: "+o)
	}
	d, err := lifecycle.Draft(s.ws, s.minter(), "adr", e.Question, strings.Join(bodyLines, "\n"), "", "", nil)
	if err != nil {
		return fmt.Sprintf("! ARG E - apply adr %s: %s\n", e.Key, err.Error()), false, nil
	}
	d.Context = e.Context
	d.Decision = e.Decision
	d.Consequences = e.Consequences
	d.Status = e.Status
	if d.Status == "" {
		d.Status = "accepted" // an applied ADR already has a settled decision
	}
	d.State = item.StateDone
	if err := item.Upsert(s.ws, d); err != nil {
		return "", false, err
	}
	if err := journal.Append(s.ws, "", journal.Event{
		Ev: journal.EvDecide, ID: d.ID, Dir: "", Note: "apply: " + e.Decision,
	}); err != nil {
		return "", false, err
	}
	_ = s.cd.Emit("decide", d.ID, "apply "+e.Decision)
	sc, err := s.idScope()
	if err != nil {
		return "", false, err
	}
	return sc.record(d) + "\n", true, nil
}

// applyIntentEntry adds one intent/prose entry through spec.AppendIntent —
// the same append-only `## intent` writer lifecycle's archive() already
// uses to merge a proposal's delta into spec.md. The brief's apply section
// names only the rule and ADR routes explicitly; intent entries are a third
// of Extract's own output (internal/knowledge/extract.go), so silently
// dropping them here would break apply's "add what the workspace lacks"
// promise for a third of what an artifact can carry. Routing through the
// one write path this repository already has for `## intent` prose keeps
// the "no new write path" invariant intact either way.
func (s *Server) applyIntentEntry(e knowledge.Entry) (string, bool, error) {
	if err := spec.AppendIntent(s.ws, "", e.Prose); err != nil {
		return "", false, err
	}
	return fmt.Sprintf("ok intent %s\ns sec:.#intent %s\n", e.Key, e.Prose), true, nil
}

// knowledgeGapCount mirrors check's two gap-producing computations exactly
// (tools.go's coverageGaps, and the orphan-applies loop check itself
// runs) without check's side effects (anchor healing/audit, drift.Save) —
// apply must report an accurate gap count, but must not itself perform
// check's mutating drift classification as a byproduct of adding entries.
func (s *Server) knowledgeGapCount(c *spec.Cascade) (int, error) {
	n := len(s.coverageGaps(c, ""))
	anchors, err := drift.Load(s.ws)
	if err != nil {
		return 0, err
	}
	anchored := map[string]bool{}
	for _, a := range anchors {
		anchored[a.Rule+"\x00"+string(a.Node)] = true
	}
	for _, f := range c.All() {
		for _, r := range f.Rules {
			for _, node := range r.Applies {
				if !anchored[r.ID+"\x00"+node] {
					n++
				}
			}
		}
	}
	return n, nil
}
