package sync

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jxsl13/spectacle/internal/cache"
	"github.com/jxsl13/spectacle/internal/journal"
	"github.com/jxsl13/spectacle/internal/workspace"
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

Body text about observability.
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := journal.Append(ws, "", journal.Event{Ev: journal.EvReject, ID: "B-0001", K: "bug", Ti: "flaky launch", Note: "not reproducible"}); err != nil {
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
	if err := os.WriteFile(filepath.Join(ws.SpectacleDir("gpu"), "spec.md"), []byte(`---
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
