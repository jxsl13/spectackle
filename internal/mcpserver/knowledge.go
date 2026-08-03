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
// knowledgeConflictMarker records, in the ADR body, the merge-conflict key
// an imported decision came from — provenance for a later reader, and the
// one line that distinguishes it from a question a human asked. It is
// deliberately NOT the answer path's hook: decide op=answer stays fully
// generic over ADRs, and the duplicate-suppression check keys off the
// workspace's own artifact (knowledge.Extract's Key) rather than this
// string, so a decision reached without an import counts just the same.
const knowledgeConflictMarker = "knowledge-conflict:"

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
	// host:path is scp style and becomes host/path; host:PORT/path is a
	// URL port and drops entirely, so the same self-hosted remote reached
	// with and without an explicit port still collapses to one source
	// (cross-val-prov WARN).
	if i := strings.IndexByte(u, ':'); i >= 0 {
		rest := u[i+1:]
		if j := strings.IndexByte(rest, '/'); j > 0 && isAllDigits(rest[:j]) {
			u = u[:i] + rest[j:]
		} else {
			u = u[:i] + "/" + rest
		}
	}
	u = strings.TrimSuffix(strings.TrimRight(u, "/"), ".git")
	return u
}

func isAllDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

// rootPath resolves an artifact path the way every other spectackle path
// works: RELATIVE to the workspace, absolute taken verbatim.
//
// It used to go straight to os.ReadFile/os.WriteFile, so a relative path
// landed in whatever directory the SERVER PROCESS was started from — an
// independent gap hunt exporting with path=kb-export.md wrote the file
// into the spectackle source repo instead of the workspace
// (B-01KYMCJG8HFYW). A long-lived MCP server has one cwd and serves many
// roots over its lifetime, so cwd-relative was never the useful reading.
func (s *Server) rootPath(p string) string {
	if p == "" || filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(s.ws.Dir, filepath.FromSlash(p))
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
		if err := os.WriteFile(s.rootPath(in.Path), out, 0o644); err != nil {
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
	// The other untrusted status entry point: a caller composing entries for
	// a repository with no bundle. The enum lived only in this field's
	// jsonschema DESCRIPTION, which validates nothing (B-01KYNA4PJNF5K).
	//
	// ValidStatus only — "superseded" IS allowed here, and the asymmetry with
	// applyADREntry is the point rather than an oversight. Exporting says
	// "this repository's decision is superseded", a true historical fact
	// about the source, recorded against Entry.Sources. Importing the same
	// value would say "this workspace's decision is superseded", which the
	// importer cannot know: it holds no replacement record. Describing your
	// own history and adopting someone else's claim are different acts.
	if !item.ValidStatus(e.Status) {
		return knowledge.Entry{}, fmt.Errorf("status %q invalid (want %s)",
			e.Status, strings.Join(item.Statuses(), "|"))
	}
	// The entry's dir is caller-supplied and lands verbatim in the
	// Provenance of every exported entry, which is a rendered line in the
	// artifact — the same class as draft's and bench's dir
	// (B-01KYRN4VBEEXQ). Unrelated to the spec.AuthorReq{Dir: ""} call this
	// file makes further down: that one is internal and supplies its own
	// value.
	if err := item.CheckDir(e.Dir); err != nil {
		return knowledge.Entry{}, err
	}
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
	artifacts, err := s.knowledgeGatherArtifacts(in.Paths, in.Body)
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
func (s *Server) knowledgeGatherArtifacts(paths []string, body string) ([]knowledge.Artifact, error) {
	var out []knowledge.Artifact
	for _, p := range paths {
		raw, err := os.ReadFile(s.rootPath(p))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", p, err)
		}
		if err := recordBlockRefusal(p, string(raw)); err != nil {
			return nil, err
		}
		a, err := knowledge.Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", p, err)
		}
		out = append(out, a)
	}
	if strings.TrimSpace(body) != "" {
		if err := recordBlockRefusal("body", body); err != nil {
			return nil, err
		}
		a, err := knowledge.Parse([]byte(body))
		if err != nil {
			return nil, fmt.Errorf("parse body: %w", err)
		}
		out = append(out, a)
	}
	return out, nil
}

// recordBlockRefusal diagnoses the one wrong call the knowledge tool's own
// output shape invites, and it must run BEFORE knowledge.Parse
// (B-01KYRVXQ02FDH). `export` and `merge` print their dense record lines
// AFTER the artifact on the SAME text result, so the obvious composition —
// feed what one op printed into the next — hands those record lines to the
// artifact parser. Both outcomes of that were worse than a refusal:
//
//   - where a `## ` heading precedes them, yaml chokes on a plain scalar
//     with no colon and the caller gets a raw parser complaint about a
//     coordinate it has to map itself;
//   - where none does — merge's own output, whose trailing block follows the
//     last entry of a condensate, or any artifact whose records land outside
//     every entry — Parse's heading loop `continue`s straight past them and
//     reports NOTHING. Measured: piping a conflicting merge's output back
//     into merge exited 0 with `entries=0 conflicts=0`, dropping two
//     conflict records a human still had to adjudicate.
//
// The silent face is why this is a pre-Parse check rather than a nicer
// error message: there is no error to improve.
//
// label names the input in the caller's own vocabulary — the path it
// passed, or "body".
func recordBlockRefusal(label, s string) error {
	block, firstN := trailingRecordBlock(s)
	if len(block) == 0 {
		return nil
	}
	return fmt.Errorf("%s lines %d-%d are record lines, not artifact content: %q — knowledge export/merge print their record lines after the artifact on the same stream; drop the trailing record block, or export with path= and apply with path=",
		label, firstN, firstN+len(block)-1, strings.Join(block, "\n"))
}

// trailingRecordBlock walks BACKWARDS from the end of s over the final run
// of non-blank lines for as long as they are dense records (`ok ` or `x `,
// the convention every tool here shares — docs/tools.md), and returns that
// run together with the 1-based line number of its FIRST line in s.
//
// The whole RUN matters, not merely the last line: a merge with conflicts
// emits one `x` record per competing entry BEFORE its `ok` trailer, so a
// message naming only the final line would have the caller strip one line,
// call again, and hit a second, differently-worded failure. One refusal has
// to describe the entire thing to be dropped.
//
// Returns (nil, 0) when nothing trails — a well-formed artifact ends in
// entry yaml, and this check must be invisible on it.
func trailingRecordBlock(s string) (block []string, firstN int) {
	lines := strings.Split(s, "\n")
	i := len(lines) - 1
	for i >= 0 && strings.TrimSpace(lines[i]) == "" {
		i--
	}
	last := i
	for i >= 0 && (strings.HasPrefix(lines[i], "ok ") || strings.HasPrefix(lines[i], "x ")) {
		i--
	}
	if i == last {
		return nil, 0
	}
	return lines[i+1 : last+1], i + 2
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
	// Apply takes the SAME inputs merge does, and ALWAYS merges them —
	// however many arrived and by whichever field. Conflicts become open
	// decisions (ADR-01KYMKEG7YE2P: decide-integration) instead of
	// vanishing from the target as they did before (P-01KYMCKE8DEW7).
	//
	// Merging unconditionally is the load-bearing part. An artifact count
	// is NOT a conflict count: knowledge.Merge buckets entries across AND
	// within artifacts, and `knowledge export` of a workspace that answered
	// the same question twice emits ONE artifact carrying both answers — so
	// gating detection on the number of artifacts (either shape of that
	// guard) let the single-artifact route drop a side silently, which is
	// this feature's own bug reproduced through its own tool.
	//
	// It is backward compatible by construction rather than by promise: a
	// conflict-free artifact merges to itself. Parse already canonicalizes
	// entry order with sortEntries, the same comparator Merge re-applies,
	// and groupBySubstance only folds entries that FoldInto would dedupe by
	// identity anyway — so the delta, the added count and the rendered
	// lines are unchanged.
	paths := in.Paths
	if in.Path != "" {
		paths = append([]string{in.Path}, paths...)
	}
	artifacts, err := s.knowledgeGatherArtifacts(paths, in.Body)
	if err != nil {
		return refuse("! ARG E - " + err.Error())
	}
	if len(artifacts) == 0 {
		return refuse("! ARG E - apply requires path, paths or body")
	}
	incoming, conflicts, err := knowledge.Merge(artifacts...)
	if err != nil {
		return nil, nil, err
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

	// FoldInto skips an incoming entry whose (kind, key) identity this
	// workspace already carries — correct precedence (local wins, apply
	// stays additive and idempotent) but, on its own, indistinguishable from
	// agreement. An import that DISAGREES with a decision already held was
	// dropped as silently as one that merely repeated it, which is the same
	// shape as the loss this whole feature exists to close, just on the
	// one-artifact side of it. Report the disagreements; do not adopt them.
	diverged := knowledge.Diverging(current, incoming)

	var b strings.Builder
	// `refused` is the exit-code input, counted alongside `added` rather
	// than derived from it: an entry can be neither (an unknown kind is a
	// skip, not a write), and the two counts answer different questions —
	// `added` is what landed, `refused` is what the caller asked for and did
	// not get. See the return at the bottom for why ANY refusal exits
	// non-zero (B-01KYRN43FQFZ4).
	added, refused := 0, 0
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
		} else {
			// Every non-ok arm above already wrote a `! ... E` (or, for the
			// unknown kind, a `! ... W`) record into b — the five producers
			// are applyRuleEntry's AddRule error and lint rejection, and
			// applyADREntry's status guard, header check and Draft error.
			// The unknown-kind arm is unreachable from Parse (the section
			// regex in internal/knowledge/artifact.go constrains the kind),
			// but counting it refused is the safe side if that regex ever
			// widens: an entry that was asked for and not written is a
			// refusal whatever the reason.
			refused++
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

	// Each conflict becomes an OPEN DECISION in this workspace rather than
	// a dropped entry: the ADR carries both sides with their sources, so
	// the loser survives in the record whatever the answer turns out to
	// be, and answering it through decide op=answer is what lands a
	// winner. Never auto-resolved (the merge contract), never silently
	// discarded (the gap this closes).
	//
	// Minting is idempotent under FoldInto's own rule: a conflict whose
	// (KindADR, Key) identity this workspace already carries is settled
	// here and skipped, exactly as a non-conflicting entry with a known
	// identity is skipped. knowledge.Extract emits an entry for every adr
	// item at any state with Key=NormHash(question), so that one check
	// covers all three ways an opinion can already exist: a decision minted
	// by an earlier apply and still open, one that has since been answered,
	// and one this repository reached on its own. Without it a re-apply
	// minted a second ADR asking a question already on the board.
	held := map[string]bool{}
	for _, e := range current.Entries {
		if e.Kind == knowledge.KindADR {
			held[e.Key] = true
		}
	}
	// Archived decisions count as held too. Extract reads work.md, and an
	// answered ADR's normal end is to leave it — so without this the ONLY
	// workspace that gets asked its curated questions again is the one that
	// ran the lifecycle properly all the way to archived.
	archived, aerr := s.archivedDecisionKeys()
	if aerr != nil {
		return nil, nil, aerr
	}
	for k := range archived {
		held[k] = true
	}
	// The settle check and the mint must be ONE step. `held` is derived from
	// an Extract taken before any writing, so two agents applying the same
	// pair at once both saw an empty board and both minted — two unlinked
	// ADRs asking the identical question, neither authoritative. Under the
	// lock, the second one re-reads work.md, finds the first, and reports it
	// instead of opening a rival. Named per conflict key, so applies over
	// disjoint conflicts still run concurrently.
	open, settled := 0, 0
	for _, cf := range conflicts {
		if held[cf.Key] {
			settled++
			continue
		}
		var id string
		var mErr error
		lErr := s.cd.WithLock("knowledge-conflict:"+cf.Key, func() error {
			if existing, eErr := s.existingDecisionFor(cf); eErr != nil {
				return eErr
			} else if existing != "" {
				id = ""
				return nil
			}
			id, mErr = s.mintConflictDecision(cf)
			return nil
		})
		if lErr != nil {
			// A conflict whose lock or mint failed is a decision this
			// workspace was supposed to open and did not — the same class of
			// loss as a refused entry, and it rode under `ok` too. The
			// settled= comment below already argues a failed mint must not
			// fold into that counter; the identical reasoning keeps it out of
			// the success exit code (B-01KYRN43FQFZ4).
			refused++
			fmt.Fprintf(&b, "! ARG E - conflict %s: %s\n", cf.Key, lErr.Error())
			continue
		}
		if mErr != nil {
			refused++
			fmt.Fprintf(&b, "! ARG E - conflict %s: %s\n", cf.Key, mErr.Error())
			continue
		}
		if id == "" {
			settled++ // a concurrent apply opened it first
			continue
		}
		open++
		fmt.Fprintf(&b, "need decision %s %s\n", id, conflictQuestion(cf))
	}
	// One `x` line per divergence, the same record `merge` uses for a
	// conflict between artifacts — this is the same disagreement, with the
	// workspace as one of the two sides. Kept out of the mint loop: the
	// local position stands, so there is nothing to decide, only something
	// the caller must not be allowed to miss.
	for _, dv := range diverged {
		ours, theirs := divergedValue(dv.Ours), divergedValue(dv.Then)
		if ours == theirs {
			// The headline values agree, so the disagreement is in a field
			// this line does not quote — rendering it anyway printed the
			// same string twice for a real, correctly detected divergence.
			// Name the fields instead; the record itself carries the text.
			fmt.Fprintf(&b, "x %s %s same %q, differs in %s (kept ours)\n",
				dv.Kind, shortKey(dv.Key), ours, strings.Join(divergedFields(dv), ","))
			continue
		}
		fmt.Fprintf(&b, "x %s %s ours=%q theirs=%q (kept ours)\n",
			dv.Kind, shortKey(dv.Key), ours, theirs)
	}

	// The counters go into their OWN builder, unprefixed, so the `ok ` that
	// has always led them is applied at the return below only when nothing
	// was refused. Composing them into b — the same builder carrying every
	// per-entry `! ... E` record — is what let a refusal ship under a
	// success-shaped trailer (B-01KYRN43FQFZ4).
	var t strings.Builder
	fmt.Fprintf(&t, "applied added=%d gaps=%d", added, gaps)
	if open > 0 {
		fmt.Fprintf(&t, " conflicts=%d", open)
	}
	if settled > 0 {
		// Counted, never derived: the sources still disagree, this workspace
		// simply already holds an answer — silence would read as "no
		// conflict", and a second `conflicts=` would read as "decide again".
		// `len(conflicts) - open` looked equivalent and was not: it folded
		// every conflict whose mint FAILED (a lock timeout, a write error)
		// into the count, reporting a conflict that still needs a decision
		// as one already settled.
		fmt.Fprintf(&t, " settled=%d", settled)
	}
	if len(diverged) > 0 {
		fmt.Fprintf(&t, " diverged=%d", len(diverged))
	}
	t.WriteString("\n")
	// A partial apply really did write, so the dirty flag keys on `added`,
	// not on the exit code.
	if added > 0 {
		s.markDirty()
	}
	// ANY refusal exits non-zero. This is not a fresh judgement call:
	// SRF-001 says the refused operation SHALL exit non-zero, and a refused
	// entry IS a refused operation. The partial-success reading — exit
	// non-zero only when nothing landed — would make the exit code depend on
	// UNRELATED entries in the same artifact, so an import that dropped a
	// decision would still exit 0 as long as some other entry happened to
	// apply. That invisible refusal is precisely the defect (B-01KYRN43FQFZ4).
	//
	// The headline leads with what did NOT happen; the per-entry `! ... E`
	// lines already in b say which entry and why, so nothing new is
	// rendered. The `ok <ID> <path>` / `r <ID>` lines for entries that DID
	// land stay — SRF-001 forbids a success line for a state the caller did
	// not request, and those states were requested and reached.
	if refused > 0 {
		return refuse(fmt.Sprintf("! REJECTED E - apply refused %d of %d entries; they were NOT applied\n",
			refused, len(toAdd.Entries)) + b.String() + t.String())
	}
	return text(b.String() + "ok " + t.String())
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
	// Validate BEFORE minting. lifecycle.Draft persists the item and journals
	// a create event, so a check placed after it left a permanent,
	// content-less ADR behind on every refusal — and an export of that
	// workspace re-emitted the stray as an ordinary entry, which a third
	// workspace then promoted to a full accepted ADR. The guard meant to keep
	// bad data out was manufacturing worse data than it rejected.
	//
	// UNTRUSTED input: this status was authored by another repository. The
	// asymmetry with the export path is deliberate — see
	// knowledgeEntryFromIn.
	if !item.ValidStatus(e.Status) || e.Status == "superseded" {
		return fmt.Sprintf("! ARG E - apply adr %s: status %q not adoptable from an artifact (want %s; adopting superseded would assert a replacement this workspace does not have)\n",
			e.Key, e.Status, strings.Join(item.Statuses(), "|")), false, nil
	}
	// Same ordering, same reason, for the values rather than the status: the
	// fields below are written by the Upsert AFTER the mint, so a paragraph
	// break in an imported Context left a content-less ADR behind that an
	// export then re-emitted as an ordinary entry.
	if err := item.CheckHeader(item.Item{
		Title: e.Question, Context: e.Context, Decision: e.Decision,
		Consequences: e.Consequences, Status: e.Status,
	}); err != nil {
		return fmt.Sprintf("! ARG E - apply adr %s: %s\n", e.Key, err.Error()), false, nil
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

// archivedDecisionKeys is the content-key set of every decision this
// workspace has already made AND archived. knowledge.Extract answers the
// same question for live records by reading work.md, but an answered ADR's
// normal end is to leave work.md for a journal tombstone — so a workspace
// that curated its conflicts and closed them out properly would otherwise
// look, to the next import, like one that had never decided anything.
//
// Built as one journal pass, mirroring exactly what lifecycle.Tombstone
// itself looks for, in the same shape knownRefIDs uses rather than probing
// per candidate. The key comes from knowledge.ADRKey so the hash has one
// definition (KN-001), never a second one spelled out here.
func (s *Server) archivedDecisionKeys() (map[string]bool, error) {
	events, err := journal.ReadAll(s.ws)
	if err != nil {
		return nil, err
	}
	keys := map[string]bool{}
	for _, e := range events {
		if e.Ev != journal.EvArchive || e.K != "adr" {
			continue
		}
		if q := strings.TrimSpace(e.Ti); q != "" {
			keys[knowledge.ADRKey(q)] = true
		}
	}
	return keys, nil
}

// shortKey trims a content hash to the same width the `x` merge-conflict
// record uses, so the two disagreement renders read alike.
func shortKey(k string) string {
	if len(k) > 8 {
		return k[:8]
	}
	return k
}

// divergedValue is the one field of an entry that carries the disagreement:
// a decision for an ADR, the sentence itself for a rule or intent. Capped,
// because a divergence line is a pointer to go read the record, not the
// record.
func divergedValue(e knowledge.Entry) string {
	v := strings.TrimSpace(e.Decision)
	if v == "" {
		v = strings.TrimSpace(e.Text)
	}
	if v == "" {
		v = strings.TrimSpace(e.Prose)
	}
	v = strings.ReplaceAll(v, "\n", " ")
	if len(v) > 60 {
		v = v[:60] + "…"
	}
	return v
}

// divergedFields names the entry fields that actually differ, for the case
// where the quoted headline value does not — the only way an `x` line can
// say something true when ours and theirs read alike.
func divergedFields(dv knowledge.Divergence) []string {
	var out []string
	for _, f := range []struct {
		name string
		a, b string
	}{
		{"text", dv.Ours.Text, dv.Then.Text},
		{"prose", dv.Ours.Prose, dv.Then.Prose},
		{"context", dv.Ours.Context, dv.Then.Context},
		{"decision", dv.Ours.Decision, dv.Then.Decision},
		{"consequences", dv.Ours.Consequences, dv.Then.Consequences},
		// status is compared RAW, matching substanceEqual, which uses ==
		// on Status where it NormHashes the others. Trimming here made a
		// whitespace-only status difference — flagged as a divergence by
		// substanceEqual — invisible to this list, degrading a namable
		// field to the generic "formatting" fallback.
		{"status", dv.Ours.Status, dv.Then.Status},
	} {
		differs := strings.TrimSpace(f.a) != strings.TrimSpace(f.b)
		if f.name == "status" {
			differs = f.a != f.b
		}
		if differs {
			out = append(out, f.name)
		}
	}
	if len(out) == 0 {
		// substanceEqual normalizes (NormHash folds case and whitespace)
		// where this compares raw — say so rather than print nothing
		out = append(out, "formatting")
	}
	return out
}

// conflictQuestion renders the decision a conflict poses.
func conflictQuestion(cf knowledge.Conflict) string {
	for _, e := range cf.Entries {
		if strings.TrimSpace(e.Question) != "" {
			return strings.TrimSpace(e.Question)
		}
	}
	return string(cf.Kind) + " " + cf.Key + ": sources disagree"
}

// mintConflictDecision opens one ADR for one merge conflict, through the
// SAME primitives decide op=ask uses — no new write path. The options are
// the competing decisions labeled by source, so an answer is unambiguous
// about WHOSE decision won; the body keeps every side verbatim so the
// loser stays readable in the record and its journal tombstone.
func (s *Server) mintConflictDecision(cf knowledge.Conflict) (string, error) {
	body := []string{"kind: radio", knowledgeConflictMarker + " " + cf.Key}
	var opts []string
	for _, e := range cf.Entries {
		src := "unknown"
		if len(e.Sources) > 0 {
			src = e.Sources[0].Source
		}
		decision := strings.TrimSpace(e.Decision)
		if decision == "" {
			decision = strings.TrimSpace(e.Text)
		}
		if decision == "" {
			// A source that asked the question but never answered it is
			// still a side Merge distinguished, so it must stay visible —
			// but rendered bare it produced "option: <src>:" and
			// decide op=answer accepted that empty string as the winning
			// decision. Naming the absence keeps the side and makes
			// choosing it a deliberate, legible act.
			decision = "(no decision recorded)"
		}
		opt := src + ": " + decision
		opts = append(opts, opt)
		body = append(body, "option: "+opt)
	}
	if len(opts) < 2 {
		return "", fmt.Errorf("conflict %s has fewer than two sides", cf.Key)
	}
	d, err := lifecycle.Draft(s.ws, s.minter(), "adr", conflictQuestion(cf), strings.Join(body, "\n"), "", "", nil)
	if err != nil {
		return "", err
	}
	d.Context = "knowledge merge conflict: the sources above disagree; answering selects the decision this workspace adopts, and the others stay recorded here"
	d.Status = "proposed"
	if err := item.Upsert(s.ws, d); err != nil {
		return "", err
	}
	if _, err := lifecycle.Move(s.ws, d.ID, item.StateSubmitted, ""); err != nil {
		return "", err
	}
	_ = s.cd.Emit("decide", d.ID, "ask "+conflictQuestion(cf))
	return shortDisplayID(d.ID), nil
}

// existingDecisionFor returns the ID of a live decision already asking this
// question, or "" when none does. It is the inside-the-lock half of the
// settle check: the outside-the-lock one reads knowledge.Extract, which is
// computed before any minting and therefore cannot see a decision a
// concurrent apply opened a millisecond ago.
func (s *Server) existingDecisionFor(cf knowledge.Conflict) (string, error) {
	items, err := item.LoadAll(s.ws)
	if err != nil {
		return "", err
	}
	key := knowledge.ADRKey(conflictQuestion(cf))
	for _, it := range items {
		if it.Kind == "adr" && it.State != item.StateArchived && knowledge.ADRKey(it.Title) == key {
			return it.ID, nil
		}
	}
	return "", nil
}
