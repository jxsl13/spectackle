// Package journal is the append-only event log — one journal.ndjson per
// .spectackle folder, one JSON object per line. The journal is the historical
// source of truth: rejected and archived items leave work.md and live on
// here (searchable via the cache FTS as the rejection corpus).
//
// Append-only has one sanctioned exception: compaction may fold old events,
// but `reject`, `archive` and `compact` lines are always retained verbatim.
package journal

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"time"

	"github.com/jxsl13/spectackle/internal/workspace"
)

// mintEid returns 8 random bytes as hex — unique enough to identify an event
// across worktrees for replay deduplication.
func mintEid() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// Event kinds (Ev field).
const (
	EvCreate  = "create"
	EvMove    = "move"
	EvRule    = "rule"
	EvArchive = "archive"
	EvReject  = "reject"
	EvDrift   = "drift"
	EvCompact = "compact"
	EvStart   = "start"  // worktree opened for an item
	EvSubmit  = "submit" // worktree gated, merged and replayed onto main
	EvAbort   = "abort"  // worktree abandoned

	// SDD orchestration v2 (see internal/lifecycle: Move reopen counter,
	// Escalate, ResolveBlocked).
	EvGrill    = "grill"    // grill feedback recorded against an item
	EvDecide   = "decide"   // a blocked item's linked decision was resolved
	EvEscalate = "escalate" // an item exhausted its feedback rounds and was blocked
	EvReview   = "review"   // independent review verdict, identity- and body-hash-bound (T-01KYD94KP4)
	EvValidate = "validate" // post-implementation validation: Op=render (pack, diff hash) or Op=verdict (T-01KYD94M3)
	EvRevise   = "revise"   // draft-state body/targets revision through the draft tool (B-01KYER)
	EvGitSkip  = "gitskip"  // edge commit skipped after retries (index contention) — journal-only warning (T-01KYD94MG)
)

// Event is the single flat record type for all journal lines; unused fields
// are omitted. Keys are deliberately short — journals are read by machines
// and indexed, not read linearly by the LLM.
type Event struct {
	T    time.Time `json:"t"`
	Eid  string    `json:"eid,omitempty"` // unique event ID (minted on append) — the replay/idempotency backbone
	Ag   string    `json:"ag,omitempty"`  // agent that wrote the event
	Ev   string    `json:"ev"`
	ID   string    `json:"id,omitempty"`
	K    string    `json:"k,omitempty"`    // item kind
	Ti   string    `json:"ti,omitempty"`   // title
	Dir  string    `json:"dir,omitempty"`  // context dir
	Fr   string    `json:"fr,omitempty"`   // move: from state
	To   string    `json:"to,omitempty"`   // move: to state
	Note string    `json:"note,omitempty"` // move/reject/compact note
	Op   string    `json:"op,omitempty"`   // rule: add|edit|retire
	Rule string    `json:"rule,omitempty"` // rule/drift: rule ID
	Txt  string    `json:"txt,omitempty"`  // rule: full sentence (survives spec.md edits)
	Ap   []string  `json:"ap,omitempty"`   // rule: applies node IDs
	Sum  string    `json:"sum,omitempty"`  // archive/reject summary
	Body string    `json:"body,omitempty"` // reject: full item body (enables revocation)
	Tg   []string  `json:"tg,omitempty"`   // reject: item targets
	Refs []string  `json:"refs,omitempty"` // archive/reject: item refs — the research return path reads consumers from tombstones (T-01KYD88M)
	Par  string    `json:"par,omitempty"`  // reject: item parent
	Rls  []string  `json:"rules,omitempty"`
	Node string    `json:"node,omitempty"` // drift
	Cls  string    `json:"cls,omitempty"`  // drift: gone|changed|stale
	Oh   string    `json:"oh,omitempty"`   // drift: old hash
	Nh   string    `json:"nh,omitempty"`   // drift: new hash
	Item string    `json:"item,omitempty"` // drift: backprop item
	N    int       `json:"n,omitempty"`    // compact: events folded

	// SDD orchestration v2: reopen/grill/decide/escalate + reject snapshot.
	Rnd int      `json:"rnd,omitempty"` // move (reopen)/escalate/reject: Rounds counter
	Gr  string   `json:"gr,omitempty"`  // grill/reject: Grilled feedback
	Nd  []string `json:"nd,omitempty"`  // escalate/reject: Needs (blocking decision IDs)
	Ov  bool     `json:"ov,omitempty"`  // decide(override-once)/reject: Override spent

	// Review verdicts (EvReview) and the grill render they bind to
	// (T-01KYD94KP4): Hash is the sha256 of the item body at render/verdict
	// time — a body edit invalidates both by construction. Open is the
	// computed-finding count of the render; Pass the reviewer's verdict.
	Hash string   `json:"hash,omitempty"` // grill/review/validate: hash of the judged substance
	Open int      `json:"open,omitempty"` // grill/validate render: computed findings still open
	Pass bool     `json:"pass,omitempty"` // review/validate: the verdict
	Keys []string `json:"keys,omitempty"` // grill/validate render: the open finding keys (class:subject)
	Wv   []string `json:"wv,omitempty"`   // review/validate verdict: per-finding waivers, "key reason" (T-01KYD9J)
}

// Append writes one event to the journal of a context dir, creating the
// scaffold if needed. It mints a unique event ID and stamps the workspace's
// agent name unless the caller provided them (replay does, verbatim).
func Append(root workspace.Root, ctx string, e Event) error {
	if err := root.EnsureScaffold(ctx); err != nil {
		return err
	}
	if e.T.IsZero() {
		e.T = time.Now().UTC()
	}
	e.T = e.T.Truncate(time.Second)
	if e.Eid == "" {
		e.Eid = mintEid()
	}
	if e.Ag == "" {
		e.Ag = root.Agent
	}
	raw, err := json.Marshal(e)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(root.JournalPath(ctx), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err = f.Write(append(raw, '\n')); err != nil {
		return err
	}
	// The edge-commit engine's capture point (T-01KYD94MG): the sink sees
	// exactly what was appended, after it durably landed.
	if root.Sink != nil {
		root.Sink(root.JournalPath(ctx), raw)
	}
	return nil
}

// Read returns all events of one context dir (missing file = empty).
func Read(root workspace.Root, ctx string) ([]Event, error) {
	f, err := os.Open(root.JournalPath(ctx))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Event
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		var e Event
		if json.Unmarshal(sc.Bytes(), &e) == nil {
			out = append(out, e)
		}
	}
	return out, sc.Err()
}

// ReadAll returns the events of every context dir, in context order.
func ReadAll(root workspace.Root) ([]Event, error) {
	ctxs, err := root.ContextDirs()
	if err != nil {
		return nil, err
	}
	var out []Event
	for _, c := range ctxs {
		es, err := Read(root, c)
		if err != nil {
			return nil, err
		}
		for i := range es {
			if es[i].Dir == "" {
				es[i].Dir = c
			}
		}
		out = append(out, es...)
	}
	return out, nil
}

// Rewrite replaces a context dir's journal wholesale — used only by
// compaction (the sanctioned append-only exception).
func Rewrite(root workspace.Root, ctx string, events []Event) error {
	f, err := os.CreateTemp(root.SpectackleDir(ctx), "journal-*")
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	for _, e := range events {
		raw, err := json.Marshal(e)
		if err != nil {
			f.Close()
			os.Remove(f.Name())
			return err
		}
		w.Write(raw)
		w.WriteByte('\n')
	}
	if err := w.Flush(); err != nil {
		f.Close()
		os.Remove(f.Name())
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return err
	}
	return os.Rename(f.Name(), root.JournalPath(ctx))
}
