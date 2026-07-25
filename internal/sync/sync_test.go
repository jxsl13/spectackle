package sync

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/jxsl13/spectackle/internal/cache"
	"github.com/jxsl13/spectackle/internal/journal"
	"github.com/jxsl13/spectackle/internal/workspace"
)

func scaffold(t *testing.T) (workspace.Root, *Scanner) {
	t.Helper()
	root := t.TempDir()
	ws := workspace.Root{Dir: root}
	if err := ws.EnsureScaffold(""); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ws.SpecPath(""), []byte(`---
schema: v0
prefix: TST
---
## intent
Testing ground.

## TST-ARC-001
The scanner SHALL index every rule sentence into the FTS cache.

## TST-ARC-003
The scanner SHALL treat specoldmarker content as the freshness authority.
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ws.WorkPath(""), []byte(`---
schema: v0
---

## P-0001 make sync observable
kind: proposal
state: draft
created: 2026-07-24

Body text about observability and workoldmarker payloads.
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := journal.Append(ws, "", journal.Event{Ev: journal.EvReject, ID: "B-0001", K: "bug", Ti: "flaky launch", Note: "not reproducible"}); err != nil {
		t.Fatal(err)
	}
	if err := journal.Append(ws, "", journal.Event{Ev: journal.EvReject, ID: "B-0002", K: "bug", Ti: "marker carrier", Note: "jrnloldmarker"}); err != nil {
		t.Fatal(err)
	}
	c, err := cache.Open(ws.CacheDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return ws, &Scanner{Root: ws, Cache: c}
}

func TestRefreshFeedsAllBundleKinds(t *testing.T) {
	_, s := scaffold(t)
	if err := s.Refresh(); err != nil {
		t.Fatal(err)
	}
	for q, kind := range map[string]string{
		"FTS cache":    "rule",
		"Testing":      "section",
		"observable":   "proposal",
		"reproducible": "rejection",
	} {
		docs, err := s.Cache.Search(q, []string{kind}, 3)
		if err != nil || len(docs) == 0 {
			t.Errorf("kind %s not indexed (q=%q): %v %v", kind, q, docs, err)
		}
	}
}

func TestRefreshPicksUpChangesAndDebounces(t *testing.T) {
	ws, s := scaffold(t)
	if err := s.Refresh(); err != nil {
		t.Fatal(err)
	}
	// change spec.md: within the debounce window nothing is re-read...
	spec, _ := os.ReadFile(ws.SpecPath(""))
	spec = append(spec, []byte("\n## TST-ARC-002\nThe scanner SHALL notice appended rules within 300 milliseconds.\n")...)
	if err := os.WriteFile(ws.SpecPath(""), spec, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.Refresh(); err != nil { // debounced no-op
		t.Fatal(err)
	}
	if docs, _ := s.Cache.Search("appended", []string{"rule"}, 3); len(docs) != 0 {
		t.Fatalf("debounce window should have skipped the rescan, found %+v", docs)
	}
	// ...MarkDirty voids the window
	s.MarkDirty()
	if err := s.Refresh(); err != nil {
		t.Fatal(err)
	}
	if docs, _ := s.Cache.Search("appended", []string{"rule"}, 3); len(docs) != 1 {
		t.Fatalf("changed spec not re-indexed: %+v", docs)
	}
}

func TestNestedContextIndexed(t *testing.T) {
	ws, s := scaffold(t)
	if err := ws.EnsureScaffold("gpu"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws.SpectackleDir("gpu"), "spec.md"), []byte(`---
schema: v0
---
## GPU-KRN-001
The kernel SHALL guard bounds with an explicit check of i against n.
`), 0o644); err != nil {
		t.Fatal(err)
	}
	s.MarkDirty()
	if err := s.Refresh(); err != nil {
		t.Fatal(err)
	}
	docs, _ := s.Cache.Search("guard bounds", []string{"rule"}, 3)
	if len(docs) != 1 || docs[0].Dir != "gpu" {
		t.Fatalf("nested context not indexed: %+v", docs)
	}
}

// ---------------------------------------------------------------------------
// B-0009 class: freshness must follow content, not metadata.
//
// The Scanner used to decide "this bundle is unchanged" from os.Stat mtime and
// size alone, so any write that preserved both — coarse mtime granularity, or
// tooling that restores timestamps (rsync --times, cp -p, tar -p, a restored
// CI cache, an image layer) — left every FTS-backed surface answering from the
// pre-change docs. The invariant below is asserted for EVERY bundle kind the
// Scanner feeds rather than for one file, and it is driven through the real
// Scanner and the real Cache, because the defect lived in the interaction
// between the two, not inside either one.
// ---------------------------------------------------------------------------

// bundleCase describes one .spectackle bundle: where it lives, which doc kinds
// it owns in the FTS index, and a marker word planted in it whose replacement
// is exactly as long, so the rewrite cannot be detected by size.
type bundleCase struct {
	name  string
	path  func(workspace.Root) string
	kinds []string // every kind this bundle owns; ReplaceDocs' unit of work
	kind  string   // the kind the marker is searchable under
	old   string
	new   string
}

var bundleCases = []bundleCase{
	{
		name:  "spec",
		path:  func(ws workspace.Root) string { return ws.SpecPath("") },
		kinds: []string{"rule", "section"},
		kind:  "rule",
		old:   "specoldmarker",
		new:   "specnewmarker",
	},
	{
		name:  "work",
		path:  func(ws workspace.Root) string { return ws.WorkPath("") },
		kinds: []string{"proposal", "task", "bug", "research", "adr"},
		kind:  "proposal",
		old:   "workoldmarker",
		new:   "worknewmarker",
	},
	{
		name:  "journal",
		path:  func(ws workspace.Root) string { return ws.JournalPath("") },
		kinds: []string{"journal", "rejection"},
		kind:  "rejection",
		old:   "jrnloldmarker",
		new:   "jrnlnewmarker",
	},
}

// rewriteSameSizeSameMTime swaps oldWord for newWord (equal byte length) in
// place and puts the original modification time back to the nanosecond, so
// after it returns the only thing about the file that differs is its content.
// It fails the test if the filesystem cannot reproduce that premise, rather
// than silently degrading into a test that no longer exercises the defect.
func rewriteSameSizeSameMTime(t *testing.T, path, oldWord, newWord string) {
	t.Helper()
	if len(oldWord) != len(newWord) {
		t.Fatalf("markers must be equal length: %q (%d) vs %q (%d)",
			oldWord, len(oldWord), newWord, len(newWord))
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte(oldWord)) {
		t.Fatalf("%s carries no marker %q — scaffold and case disagree", path, oldWord)
	}
	if err := os.WriteFile(path, bytes.ReplaceAll(raw, []byte(oldWord), []byte(newWord)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, before.ModTime(), before.ModTime()); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if after.Size() != before.Size() || after.ModTime().UnixNano() != before.ModTime().UnixNano() {
		t.Fatalf("premise broken, metadata not preserved: size %d->%d, mtime %d->%d",
			before.Size(), after.Size(), before.ModTime().UnixNano(), after.ModTime().UnixNano())
	}
}

func hits(t *testing.T, s *Scanner, q, kind string) int {
	t.Helper()
	docs, err := s.Cache.Search(q, []string{kind}, 5)
	if err != nil {
		t.Fatalf("Search(%q, %q): %v", q, kind, err)
	}
	return len(docs)
}

// TestFreshnessFollowsContentPerBundleKind is the B-0009 regression, asserted
// once per bundle kind: a same-size, mtime-preserving rewrite must become
// searchable and the content it replaced must stop being searchable.
func TestFreshnessFollowsContentPerBundleKind(t *testing.T) {
	for _, bc := range bundleCases {
		t.Run(bc.name, func(t *testing.T) {
			ws, s := scaffold(t)
			if err := s.Refresh(); err != nil {
				t.Fatal(err)
			}
			if n := hits(t, s, bc.old, bc.kind); n != 1 {
				t.Fatalf("premise: marker %q not indexed as %s, got %d hits", bc.old, bc.kind, n)
			}

			rewriteSameSizeSameMTime(t, bc.path(ws), bc.old, bc.new)
			s.MarkDirty()
			if err := s.Refresh(); err != nil {
				t.Fatal(err)
			}

			if n := hits(t, s, bc.new, bc.kind); n != 1 {
				t.Errorf("stale cache: content on disk (%q) is not searchable, got %d hits", bc.new, n)
			}
			if n := hits(t, s, bc.old, bc.kind); n != 0 {
				t.Errorf("stale cache: content no longer on disk (%q) still answers, got %d hits", bc.old, n)
			}
		})
	}
}

// TestUnchangedBundleIsNotRefed is the other direction of the same invariant:
// the fix must not degrade every scan into a full rebuild. Measured by effect,
// not by timing — the bundle's doc set is poisoned with a sentinel that only
// this test could have written, and re-feeding the bundle necessarily calls
// ReplaceDocs(dir, kinds, ...), which deletes it. A surviving sentinel is a
// direct observation that the feed function never ran.
func TestUnchangedBundleIsNotRefed(t *testing.T) {
	const sentinel = "sentinelnotrefed"
	for _, bc := range bundleCases {
		t.Run(bc.name, func(t *testing.T) {
			_, s := scaffold(t)
			if err := s.Refresh(); err != nil {
				t.Fatal(err)
			}
			if err := s.Cache.ReplaceDocs("", bc.kinds, []cache.Doc{
				{Kind: bc.kind, ID: "SENTINEL", Dir: "", Title: "sentinel", Body: sentinel},
			}); err != nil {
				t.Fatal(err)
			}

			// nothing on disk is touched between the two refreshes
			s.MarkDirty()
			if err := s.Refresh(); err != nil {
				t.Fatal(err)
			}

			if n := hits(t, s, sentinel, bc.kind); n != 1 {
				t.Errorf("untouched %s bundle was re-fed: sentinel gone (%d hits)", bc.name, n)
			}
			if n := hits(t, s, bc.old, bc.kind); n != 0 {
				t.Errorf("untouched %s bundle was re-fed: real docs restored (%d hits for %q)",
					bc.name, n, bc.old)
			}
		})
	}
}
