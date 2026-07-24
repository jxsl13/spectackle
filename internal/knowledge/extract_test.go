package knowledge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jxsl13/spectackle/internal/item"
	"github.com/jxsl13/spectackle/internal/spec"
)

// buildExtractTree mirrors the shape of internal/spec/cascade_test.go's
// buildTree: a root spec bundle (with an intent AND a notes prose section,
// plus one rule carrying an `applies` anchor) and a nested gpu/ bundle.
func buildExtractTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		".spectackle/spec.md": `---
schema: v0
prefix: GLB
---
## intent
Root intent prose, verbatim.

## notes
Root notes must not travel — only intent does.

## GLB-ARC-001 {applies: cu:pkg.kernel}
The system SHALL log to ` + "`stderr`" + ` only.
`,
		"gpu/.spectackle/spec.md": `---
schema: v0
prefix: GPU
---
## GPU-KRN-001 {applies: cu:pkg.kernel,cu:pkg.other}
WHEN a kernel is launched, the wrapper SHALL check cudaGetLastError.
`,
	}
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func testItems() []item.Item {
	return []item.Item{
		{ID: "ADR-0001", Kind: "adr", State: item.StateActive, Title: "How should retries work?",
			Context: "Jobs fail transiently under load.", Decision: "Retry up to 3 times with backoff.",
			Consequences: "Slightly higher tail latency.", Status: "accepted", Dir: ""},
		{ID: "P-0001", Kind: "proposal", State: item.StateDraft, Title: "strided access", Dir: ""},
		{ID: "T-0001", Kind: "task", State: item.StateActive, Title: "wire up drift", Dir: "gpu"},
		{ID: "B-0001", Kind: "bug", State: item.StateActive, Title: "off-by-one in kernel", Dir: "gpu"},
		{ID: "R-0001", Kind: "research", State: item.StateActive, Title: "survey retry strategies", Dir: ""},
	}
}

func TestExtractRulesADRsIntent(t *testing.T) {
	c, err := spec.Load(buildExtractTree(t))
	if err != nil {
		t.Fatal(err)
	}
	if fs := c.Findings(); len(fs) != 0 {
		t.Fatalf("tree should lint clean, got %v", fs)
	}

	a, err := Extract(c, testItems(), "github.com/acme/repoA")
	if err != nil {
		t.Fatal(err)
	}

	var rules, adrs, intents int
	for _, e := range a.Entries {
		switch e.Kind {
		case KindRule:
			rules++
		case KindADR:
			adrs++
		case KindIntent:
			intents++
		}
	}
	if rules != 2 {
		t.Fatalf("want 2 rule entries (GLB-ARC-001, GPU-KRN-001), got %d: %+v", rules, a.Entries)
	}
	if adrs != 1 {
		t.Fatalf("want 1 adr entry, got %d: %+v", adrs, a.Entries)
	}
	if intents != 1 {
		t.Fatalf("want 1 intent entry (notes must not travel), got %d: %+v", intents, a.Entries)
	}
	if len(a.Entries) != rules+adrs+intents {
		t.Fatalf("unexpected extra entries: %+v", a.Entries)
	}
	if len(a.Sources) != 1 || a.Sources[0] != "github.com/acme/repoA" {
		t.Fatalf("Sources = %v, want [github.com/acme/repoA]", a.Sources)
	}
}

// TestExtractExcludesTransientKinds: proposals, tasks, bugs and research
// describe one repository's transient state, not knowledge, and must never
// appear in an extracted artifact — including them would drown the
// condensate other repositories are meant to curate from.
func TestExtractExcludesTransientKinds(t *testing.T) {
	c, err := spec.Load(buildExtractTree(t))
	if err != nil {
		t.Fatal(err)
	}
	a, err := Extract(c, testItems(), "github.com/acme/repoA")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range a.Entries {
		if e.Kind != KindADR {
			continue
		}
		if e.Question == "strided access" || e.Question == "wire up drift" ||
			e.Question == "off-by-one in kernel" || e.Question == "survey retry strategies" {
			t.Fatalf("proposal/task/bug/research leaked into extracted knowledge: %+v", e)
		}
	}
	out, err := Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, leaked := range []string{"strided access", "wire up drift", "off-by-one in kernel", "survey retry strategies"} {
		if strings.Contains(s, leaked) {
			t.Fatalf("marshaled artifact leaked transient item %q:\n%s", leaked, s)
		}
	}
}

// TestExtractStripsAnchorsAndIDs: applies node-ID anchors and rule-ID
// prefixes/numbers cannot mean anything outside the repository that minted
// them and must not survive extraction, in the struct or on the wire.
func TestExtractStripsAnchorsAndIDs(t *testing.T) {
	c, err := spec.Load(buildExtractTree(t))
	if err != nil {
		t.Fatal(err)
	}
	a, err := Extract(c, nil, "github.com/acme/repoA")
	if err != nil {
		t.Fatal(err)
	}

	out, err := Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, mustNotAppear := range []string{"GLB-ARC-001", "GPU-KRN-001", "cu:pkg.kernel", "cu:pkg.other", "applies"} {
		if strings.Contains(s, mustNotAppear) {
			t.Fatalf("marshaled artifact leaked repo-local identity %q:\n%s", mustNotAppear, s)
		}
	}

	var sawRule bool
	for _, e := range a.Entries {
		if e.Kind != KindRule {
			continue
		}
		sawRule = true
		if strings.Contains(e.Text, "GLB-ARC") || strings.Contains(e.Text, "GPU-KRN") {
			t.Fatalf("rule Text carries an ID: %q", e.Text)
		}
	}
	if !sawRule {
		t.Fatal("expected at least one rule entry")
	}
}

// TestExtractRecordsContextDir: the originating context dir travels as
// provenance metadata (a portability hint), never as part of merge identity.
func TestExtractRecordsContextDir(t *testing.T) {
	c, err := spec.Load(buildExtractTree(t))
	if err != nil {
		t.Fatal(err)
	}
	a, err := Extract(c, nil, "github.com/acme/repoA")
	if err != nil {
		t.Fatal(err)
	}
	var gotRootRule, gotGPURule bool
	for _, e := range a.Entries {
		if e.Kind != KindRule || len(e.Sources) != 1 {
			continue
		}
		switch e.Sources[0].Dir {
		case "":
			gotRootRule = true
		case "gpu":
			gotGPURule = true
		}
	}
	if !gotRootRule || !gotGPURule {
		t.Fatalf("expected one rule provenance Dir=='' and one Dir=='gpu', entries: %+v", a.Entries)
	}
}
