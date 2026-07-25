// Package sync keeps the non-versioned cache consistent with the versioned
// .spectackle files (which are the source of truth). A debounced Refresh runs
// before every tool call: per bundle file, an mtime+size gate decides cheaply
// that a file changed, and a sha256 of the content decides whether a file the
// gate calls unchanged really is; on change the file is re-parsed and its doc
// kinds replaced in the FTS index.
package sync

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/jxsl13/spectackle/internal/cache"
	"github.com/jxsl13/spectackle/internal/ears"
	"github.com/jxsl13/spectackle/internal/item"
	"github.com/jxsl13/spectackle/internal/journal"
	"github.com/jxsl13/spectackle/internal/workspace"
)

// Scanner drives cache refreshes.
type Scanner struct {
	Root  workspace.Root
	Cache *cache.Cache

	last time.Time
}

const debounce = 300 * time.Millisecond

// MarkDirty voids the debounce window so the next Refresh runs immediately —
// every server-side write calls this, keeping its effects visible to the
// very next tool call.
func (s *Scanner) MarkDirty() { s.last = time.Time{} }

// Refresh re-syncs changed .spectackle bundle files into the cache. Calls
// within the debounce window are no-ops.
func (s *Scanner) Refresh() error {
	if time.Since(s.last) < debounce {
		return nil
	}
	s.last = time.Now()

	ctxs, err := s.Root.ContextDirs()
	if err != nil {
		return err
	}
	for _, ctx := range ctxs {
		type bundle struct {
			path  string
			kinds []string
			feed  func(ctx, path string) ([]cache.Doc, error)
		}
		for _, b := range []bundle{
			{s.Root.SpecPath(ctx), []string{"rule", "section"}, s.feedSpec},
			{s.Root.WorkPath(ctx), []string{"proposal", "task", "bug", "research", "adr"}, s.feedWork},
			{s.Root.JournalPath(ctx), []string{"journal", "rejection"}, s.feedJournal},
		} {
			st, err := os.Stat(b.path)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return err
			}
			mtime, size := st.ModTime().UnixNano(), st.Size()
			mt, sz, sha, ok := s.Cache.FileStat(b.path)

			// Two-stage freshness. Differing mtime/size is proof of change,
			// so re-feed straight away and never pay for a hash the feed's
			// own read would duplicate. Matching mtime/size proves nothing:
			// a same-size, timestamp-preserving write (coarse mtime
			// granularity on HFS+ and several network/container
			// filesystems, or rsync --times, cp -p, tar -p, restored CI
			// caches) leaves both untouched. So when the cheap gate says
			// "unchanged", the recorded content hash gets the last word —
			// freshness follows content, not metadata.
			var sum string
			if ok && mt == mtime && sz == size && sha != "" {
				if sum, err = fileSHA(b.path); err != nil {
					return err
				}
				if sum == sha {
					continue
				}
			}
			if sum == "" {
				if sum, err = fileSHA(b.path); err != nil {
					return err
				}
			}

			docs, err := b.feed(ctx, b.path)
			if err != nil {
				return fmt.Errorf("sync: %s: %w", b.path, err)
			}
			if err := s.Cache.ReplaceDocs(ctx, b.kinds, docs); err != nil {
				return err
			}
			if err := s.Cache.PutFileStat(b.path, mtime, size, sum); err != nil {
				return err
			}
		}
	}
	return nil
}

// fileSHA is the content fingerprint freshness is decided on. Streamed
// rather than slurped: spec.md and work.md are small, but journal.ndjson is
// append-only and unbounded, and the freshness check must not scale its
// memory with the history.
func fileSHA(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (s *Scanner) feedSpec(ctx, path string) ([]cache.Doc, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	body, start := ears.StripFrontMatter(string(raw))
	rules, _ := ears.ParseRules(body, path, start)
	secs := ears.ParseSections(body)
	var docs []cache.Doc
	for _, r := range rules {
		docs = append(docs, cache.Doc{Kind: "rule", ID: r.ID, Dir: ctx,
			Title: r.Text, Body: r.Text + " " + r.Rationale})
	}
	for _, sec := range secs {
		docs = append(docs, cache.Doc{Kind: "section", ID: "sec:" + ctx + "#" + sec.Name, Dir: ctx,
			Title: sec.Name, Body: sec.Text})
	}
	return docs, nil
}

func (s *Scanner) feedWork(ctx, path string) ([]cache.Doc, error) {
	items, err := item.LoadWork(path, ctx)
	if err != nil {
		return nil, err
	}
	var docs []cache.Doc
	for _, it := range items {
		docs = append(docs, cache.Doc{Kind: it.Kind, ID: it.ID, Dir: ctx,
			Title: it.Title,
			Body:  it.Title + " " + it.State + " " + strings.Join(it.Targets, " ") + " " + it.Body})
	}
	return docs, nil
}

func (s *Scanner) feedJournal(ctx, path string) ([]cache.Doc, error) {
	events, err := journal.Read(s.Root, ctx)
	if err != nil {
		return nil, err
	}
	var docs []cache.Doc
	for i, e := range events {
		kind := "journal"
		if e.Ev == journal.EvReject {
			kind = "rejection"
		}
		docs = append(docs, cache.Doc{
			Kind:  kind,
			ID:    fmt.Sprintf("j:%s#%d", orDot(ctx), i+1),
			Dir:   ctx,
			Title: strings.TrimSpace(strings.Join(fields(e.Ev, e.ID, e.Rule, e.Ti), " ")),
			Body: strings.TrimSpace(strings.Join([]string{
				e.Ev, e.ID, e.K, e.Ti, e.To, e.Note, e.Sum, e.Rule, e.Txt, e.Cls,
			}, " ")),
		})
	}
	return docs, nil
}

func fields(vals ...string) []string {
	var out []string
	for _, v := range vals {
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

func orDot(ctx string) string {
	if ctx == "" {
		return "."
	}
	return ctx
}
