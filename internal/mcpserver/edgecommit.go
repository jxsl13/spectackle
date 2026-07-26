package mcpserver

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jxsl13/spectackle/internal/journal"
	"github.com/jxsl13/spectackle/internal/wt"
)

// The edge-commit engine (T-01KYD94MG, P-01KYD8H: the commit log is the
// decision log): every tool call that writes .spectackle state commits it
// with a structured message derived from the call's own journal events —
// the two logs agree by construction, and the driving LLM issues no git
// command. Capture is EXACT: a per-call sink installed on the serving
// roots observes precisely the events the call appended (chosen over a
// pre/post journal diff, which costs a full read per call, and over
// wrapping every Append call site, which is broad and easy to miss —
// the sink rides the one function all appends already pass through).

// beginEdgeCapture installs the per-call event sink and returns the finish
// step: called after the handler with its failure verdict, it commits the
// touched bundles — or nothing, when the call failed, wrote nothing, or
// the knob is off (byte-identical behavior, zero commits).
func (s *Server) beginEdgeCapture() func(failed bool) {
	if !s.ws.Cfg.Git.EdgeCommits() {
		return func(bool) {}
	}
	type hit struct {
		e    journal.Event
		path string
	}
	var hits []hit
	sink := func(path string, raw []byte) {
		var e journal.Event
		if json.Unmarshal(raw, &e) == nil {
			hits = append(hits, hit{e, path})
		}
	}
	s.ws.Sink, s.main.Sink = sink, sink
	flushed := false
	fin := func(failed bool) {
		if flushed {
			return
		}
		flushed = true
		s.ws.Sink, s.main.Sink = nil, nil
		s.edgeFlush = nil
		if failed || len(hits) == 0 {
			return
		}
		// group by owning root: work ops append to main while serving a
		// worktree; each repo gets its own commit
		byRoot := map[string][]hit{}
		for _, h := range hits {
			root := s.ws.Dir
			if !strings.HasPrefix(h.path, s.ws.Dir+string(filepath.Separator)) &&
				strings.HasPrefix(h.path, s.main.Dir+string(filepath.Separator)) {
				root = s.main.Dir
			}
			byRoot[root] = append(byRoot[root], h)
		}
		for root, group := range byRoot {
			events := make([]journal.Event, len(group))
			paths := make([]string, len(group))
			for i, h := range group {
				events[i], paths[i] = h.e, h.path
			}
			// The cross-process edge lock: two servers on one checkout
			// race the git index exactly like they race work.md — a
			// sibling's concurrent commit would sweep this call's appends
			// and leave one of the two with nothing to commit (the NEEDS
			// clause of T-01KYD94MG, observed as 6 commits from 8
			// concurrent drafts). Append-to-commit is not atomic across
			// the two processes, so the loser RE-CHECKS for its own eid
			// in the log and skips silently when the sibling's sweep
			// already carried it.
			_ = s.cd.WithLock("edge-commit:"+root, func() error {
				s.commitEdge(root, events, paths)
				return nil
			})
		}
	}
	s.edgeFlush = fin
	return fin
}

// commitEdge commits the touched .spectackle bundles of one repo with the
// structured decision message. Every skip is deliberate and cheap: a
// checkpoint is a bonus, never a failure mode — no repo, detached HEAD,
// mid-rebase, or persistent index contention all leave the tool call
// untouched.
func (s *Server) commitEdge(root string, events []journal.Event, journalPaths []string) {
	if !wt.IsRepo(root) {
		return
	}
	git := func(args ...string) (string, error) {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	if br, err := git("rev-parse", "--abbrev-ref", "HEAD"); err != nil || strings.TrimSpace(br) == "HEAD" {
		return // unborn/detached
	}
	if p, err := git("rev-parse", "--git-path", "rebase-merge"); err == nil {
		if _, statErr := os.Stat(filepath.Join(root, strings.TrimSpace(p))); statErr == nil {
			return // mid-rebase
		}
	}
	// pathspec: the touched bundle DIRS, relative — exact to this call's
	// writes because the gate serializes the whole call; the bundle's
	// sibling files (work.md beside journal.ndjson) are the same logical
	// write. Explicit pathspecs restrict both add and commit, so a user's
	// concurrently staged bystander is never swept (git commit with a
	// pathspec implies --only).
	specSet := map[string]bool{}
	for _, jp := range journalPaths {
		rel, err := filepath.Rel(root, filepath.Dir(jp))
		if err != nil || strings.HasPrefix(rel, "..") {
			continue
		}
		specSet[filepath.ToSlash(rel)] = true
	}
	if len(specSet) == 0 {
		return
	}
	// A sibling process's sweep may have carried these events already —
	// their eids would sit in its commit trailers. One log probe keeps
	// one-commit-per-call truthful instead of minting an empty duplicate.
	if len(events) > 0 && events[0].Eid != "" {
		if out, err := git("log", "-8", "--format=%B"); err == nil && strings.Contains(out, events[0].Eid) {
			return
		}
	}
	specs := make([]string, 0, len(specSet))
	for sp := range specSet {
		specs = append(specs, sp)
	}
	sort.Strings(specs)
	msg := edgeCommitMessage(events)
	addArgs := append([]string{"add", "-A", "--"}, specs...)
	commitArgs := append([]string{"commit", "-q", "-m", msg, "--"}, specs...)
	for attempt := 0; attempt < 3; attempt++ {
		if out, err := git(addArgs...); err != nil {
			if strings.Contains(out, "index.lock") {
				time.Sleep(75 * time.Millisecond)
				continue
			}
			return
		}
		out, err := git(commitArgs...)
		if err == nil {
			return
		}
		if strings.Contains(out, "index.lock") {
			time.Sleep(75 * time.Millisecond)
			continue
		}
		if strings.Contains(out, "nothing to commit") || strings.Contains(out, "no changes added") {
			// A sibling process's sweep carried this call's bytes before we
			// took the lock: the DECISION still gets its own log entry — an
			// empty commit with the structured message and an explicit
			// marker — so one call remains one greppable edge forever,
			// while the tree content lives one commit over.
			empty := append([]string{"commit", "-q", "--allow-empty",
				"-m", msg + "\nSpectackle-Content: carried-by-sibling-commit", "--"}, specs...)
			_, _ = git(empty...)
			return
		}
		return
	}
	// persistent contention: journal-only warning, never a tool error —
	// the sink is already cleared, so this append cannot recurse
	_ = journal.Append(s.ws, "", journal.Event{
		Ev: journal.EvGitSkip, Note: "edge commit skipped: index contention after retries",
	})
}

// edgeCommitMessage renders the structured, immutable-safe message: the
// primary event in the subject, the decision note verbatim in the body,
// machine-parseable trailers with FULL IDs (a display-short prefix is
// unambiguous only at that instant; commits are forever), sibling events
// as additional Spectackle-Eid trailers.
// edgePriority ranks event kinds for subject selection: the most decisive
// event of a call names the commit — a rejection's EvMove sibling must not
// out-rank the EvReject that carries the decision.
var edgePriority = map[string]int{
	journal.EvReject: 0, journal.EvArchive: 0, journal.EvEscalate: 0,
	journal.EvCreate: 1, journal.EvRevise: 1, journal.EvGrill: 1,
	journal.EvReview: 1, journal.EvValidate: 1, journal.EvRule: 1,
	journal.EvDecide: 1, journal.EvStart: 1, journal.EvSubmit: 1, journal.EvAbort: 1,
	journal.EvMove: 2,
}

func edgeCommitMessage(events []journal.Event) string {
	p := events[0]
	best := 99
	for _, e := range events {
		rank, ok := edgePriority[e.Ev]
		if !ok {
			rank = 3
		}
		if rank < best {
			best, p = rank, e
		}
	}
	// a reject subject still shows the traversal: borrow Fr/To from the
	// sibling move event when the primary lacks them
	if p.Fr == "" && p.To == "" {
		for _, e := range events {
			if e.Ev == journal.EvMove && e.ID == p.ID {
				p.Fr, p.To = e.Fr, e.To
				break
			}
		}
	}
	var sub strings.Builder
	sub.WriteString("spectackle(" + p.Ev + "):")
	if p.ID != "" {
		sub.WriteString(" " + p.ID)
	} else if p.Rule != "" {
		sub.WriteString(" " + p.Rule)
	}
	if p.Fr != "" || p.To != "" {
		sub.WriteString(" " + p.Fr + "->" + p.To)
	}
	body := strings.TrimSpace(p.Note)
	if body == "" {
		body = strings.TrimSpace(p.Ti)
	}
	var b strings.Builder
	b.WriteString(sub.String())
	if body != "" {
		b.WriteString("\n\n" + body)
	}
	b.WriteString("\n")
	tr := func(k, v string) {
		if v != "" {
			b.WriteString("\n" + k + ": " + v)
		}
	}
	tr("Spectackle-Ev", p.Ev)
	if p.ID != "" {
		tr("Spectackle-Item", p.ID)
	} else {
		tr("Spectackle-Item", p.Rule)
	}
	tr("Spectackle-From", p.Fr)
	tr("Spectackle-To", p.To)
	tr("Spectackle-Agent", p.Ag)
	for _, e := range events {
		tr("Spectackle-Eid", e.Eid)
	}
	return b.String()
}
