package drift

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jxsl13/spectackle/internal/graph"
	"github.com/jxsl13/spectackle/internal/workspace"
)

func TestNormHashInvariance(t *testing.T) {
	a := NormHash([]byte("\nfunc F() {\n\treturn 1 \n}\n\n"))
	b := NormHash([]byte("func F() {\r\n\treturn 1\r\n}"))
	if a != b {
		t.Errorf("normalization not invariant: %s != %s", a, b)
	}
	c := NormHash([]byte("func F() {\n\treturn 2\n}"))
	if a == c {
		t.Errorf("different content must hash differently")
	}
}

func TestClassify(t *testing.T) {
	root := t.TempDir()
	ws := workspace.Root{Dir: root}
	src := "package x\n\nfunc F() int {\n\treturn 1\n}\n"
	os.MkdirAll(filepath.Join(root, "pkg"), 0o755)
	os.WriteFile(filepath.Join(root, "pkg", "f.go"), []byte(src), 0o644)

	g := graph.NewMem()
	g.Upsert([]graph.Node{{ID: "go:pkg.F", Kind: graph.KFunc, Lang: graph.LangGo,
		File: "pkg/f.go", Line: 3, EndLine: 5}}, nil)

	ruleText := "The function SHALL return 1 exactly."
	a := Stamp(ws, g, "X-TST-001", ruleText, "go:pkg.F")
	if a.CHash == "-" || a.File != "pkg/f.go" {
		t.Fatalf("stamp failed: %+v", a)
	}

	// textOf builds a ruleText func returning a fixed sentence, rule always present.
	textOf := func(text string) func(string) (string, bool) {
		return func(string) (string, bool) { return text, true }
	}
	exists := textOf(ruleText)

	rs := Classify(ws, g, []Anchor{a}, exists, nil)
	if rs[0].Class != OK {
		t.Fatalf("fresh anchor should be OK, got %s", rs[0].Class)
	}

	// pure line shift => Moved (silent refresh): same content hash, same rule hash
	os.WriteFile(filepath.Join(root, "pkg", "f.go"), []byte("package x\n\n// shifted\n\nfunc F() int {\n\treturn 1\n}\n"), 0o644)
	g2 := graph.NewMem()
	g2.Upsert([]graph.Node{{ID: "go:pkg.F", File: "pkg/f.go", Line: 5, EndLine: 7}}, nil)
	rs = Classify(ws, g2, []Anchor{a}, exists, nil)
	if rs[0].Class != Moved {
		t.Fatalf("line shift should be Moved, got %s", rs[0].Class)
	}

	// code changed, rule sentence identical => Evolved (mechanically healable)
	os.WriteFile(filepath.Join(root, "pkg", "f.go"), []byte("package x\n\nfunc F() int {\n\treturn 2\n}\n"), 0o644)
	g3 := graph.NewMem()
	g3.Upsert([]graph.Node{{ID: "go:pkg.F", File: "pkg/f.go", Line: 3, EndLine: 5}}, nil)
	rs = Classify(ws, g3, []Anchor{a}, exists, nil)
	if rs[0].Class != Evolved {
		t.Fatalf("code-only change should be Evolved, got %s", rs[0].Class)
	}
	if rs[0].NewHash == "" || rs[0].NewHash == a.CHash {
		t.Fatalf("Evolved result must carry a differing new code hash: %+v", rs[0])
	}
	if rs[0].NewRHash != a.RHash {
		t.Fatalf("Evolved result rule hash must still match the stamped one: %+v", rs[0])
	}

	// code identical, rule sentence changed => Tightened (never healed)
	os.WriteFile(filepath.Join(root, "pkg", "f.go"), []byte(src), 0o644)
	rs = Classify(ws, g, []Anchor{a}, textOf("The function SHALL return 2 exactly."), nil)
	if rs[0].Class != Tightened {
		t.Fatalf("rule-only change should be Tightened, got %s", rs[0].Class)
	}
	if rs[0].NewRHash == a.RHash {
		t.Fatalf("Tightened result must carry a differing rule hash: %+v", rs[0])
	}
	if rs[0].NewHash != a.CHash {
		t.Fatalf("Tightened result code hash must still match the stamped one: %+v", rs[0])
	}

	// both code and rule sentence changed => Diverged (never healed)
	os.WriteFile(filepath.Join(root, "pkg", "f.go"), []byte("package x\n\nfunc F() int {\n\treturn 2\n}\n"), 0o644)
	rs = Classify(ws, g3, []Anchor{a}, textOf("The function SHALL return 2 exactly."), nil)
	if rs[0].Class != Diverged {
		t.Fatalf("both-sides change should be Diverged, got %s", rs[0].Class)
	}

	// node vanished => Gone; rule vanished => Stale; empty graph => Pending
	g4 := graph.NewMem()
	g4.Upsert([]graph.Node{{ID: "go:pkg.Other", File: "pkg/f.go", Line: 1}}, nil)
	rs = Classify(ws, g4, []Anchor{a}, exists, nil)
	if rs[0].Class != Gone {
		t.Fatalf("missing node should be Gone, got %s", rs[0].Class)
	}
	rs = Classify(ws, g3, []Anchor{a}, func(string) (string, bool) { return "", false }, nil)
	if rs[0].Class != Stale {
		t.Fatalf("missing rule should be Stale, got %s", rs[0].Class)
	}
	rs = Classify(ws, graph.NewMem(), []Anchor{a}, exists, nil)
	if rs[0].Class != Pending {
		t.Fatalf("empty graph should degrade to Pending, got %s", rs[0].Class)
	}
}

// TestReconcile: DRF-001 regression — a rule edit (or retire) must drop the
// anchor rows of nodes no longer in its applies set, not just add the new
// ones, or a mistyped-then-corrected node leaves a permanently stale row.
func TestReconcile(t *testing.T) {
	mk := func(rule string, node graph.NodeID) Anchor {
		return Anchor{Rule: rule, Node: node, File: "-", CHash: "-", RHash: "x"}
	}
	cases := []struct {
		name    string
		anchors []Anchor
		rule    string
		keep    []graph.NodeID
		want    []Anchor
	}{
		{
			name: "drop one of two",
			anchors: []Anchor{
				mk("X-001", "go:pkg.A"),
				mk("X-001", "go:pkg.B"),
			},
			rule: "X-001",
			keep: []graph.NodeID{"go:pkg.B"},
			want: []Anchor{mk("X-001", "go:pkg.B")},
		},
		{
			name: "keep all",
			anchors: []Anchor{
				mk("X-001", "go:pkg.A"),
				mk("X-001", "go:pkg.B"),
			},
			rule: "X-001",
			keep: []graph.NodeID{"go:pkg.A", "go:pkg.B"},
			want: []Anchor{mk("X-001", "go:pkg.A"), mk("X-001", "go:pkg.B")},
		},
		{
			name: "rule absent leaves other rules untouched",
			anchors: []Anchor{
				mk("X-001", "go:pkg.A"),
				mk("Y-002", "go:pkg.C"),
			},
			rule: "X-001",
			keep: nil,
			want: []Anchor{mk("Y-002", "go:pkg.C")},
		},
		{
			name: "empty keep drops all rows of the rule",
			anchors: []Anchor{
				mk("X-001", "go:pkg.A"),
				mk("X-001", "go:pkg.B"),
				mk("Y-002", "go:pkg.C"),
			},
			rule: "X-001",
			keep: nil,
			want: []Anchor{mk("Y-002", "go:pkg.C")},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Reconcile(tc.anchors, tc.rule, tc.keep)
			if len(got) != len(tc.want) {
				t.Fatalf("got %d anchors, want %d: %+v", len(got), len(tc.want), got)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("anchor %d = %+v, want %+v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// ---- DRF-002: end-line comparison ----

// TestClassifyEndLineDrift (DRF-002, test 1 of T-0107): a stored anchor
// whose End disagrees with the node's current EndLine must never classify
// OK — before this fix, Classify's OK branch compared only File and Start,
// so Anchor.End (written by Stamp) was never read back by anything. This
// is the RSV-001 shape live in this repo: internal/resolve/resolver.go
// recorded 36-44 while go:resolve.Default actually ends at 45, content
// hash matching the real 36-45 span throughout, and check reported ok.
func TestClassifyEndLineDrift(t *testing.T) {
	root := t.TempDir()
	ws := workspace.Root{Dir: root}
	src := "package x\n\nfunc F() int {\n\treturn 1\n}\n"
	os.MkdirAll(filepath.Join(root, "pkg"), 0o755)
	os.WriteFile(filepath.Join(root, "pkg", "f.go"), []byte(src), 0o644)

	g := graph.NewMem()
	g.Upsert([]graph.Node{{ID: "go:pkg.F", File: "pkg/f.go", Line: 3, EndLine: 5}}, nil)
	exists := func(string) (string, bool) { return "The function SHALL return 1 exactly.", true }

	// Stamp at the node's real span (End=5, matching EndLine), then
	// hand-shrink the anchor's recorded End to 4 to simulate a row whose
	// end line drifted after stamping while its content hash — computed
	// from the node's REAL span at classification time, not the anchor's
	// recorded range — still matches exactly.
	a := Stamp(ws, g, "X-TST-002", "The function SHALL return 1 exactly.", "go:pkg.F")
	if a.End != 5 {
		t.Fatalf("setup: stamped End = %d, want 5", a.End)
	}
	a.End = 4

	rs := Classify(ws, g, []Anchor{a}, exists, nil)
	if rs[0].Class != Moved {
		t.Fatalf("end-line drift (anchor End=4, node EndLine=5, matching content) should be Moved, got %s", rs[0].Class)
	}
}

// TestClassifyExactEndMatchIsOK (DRF-002, test 2 of T-0107): file, start
// AND end all matching stays OK — the fix must narrow OK, not eliminate it.
func TestClassifyExactEndMatchIsOK(t *testing.T) {
	root := t.TempDir()
	ws := workspace.Root{Dir: root}
	src := "package x\n\nfunc F() int {\n\treturn 1\n}\n"
	os.MkdirAll(filepath.Join(root, "pkg"), 0o755)
	os.WriteFile(filepath.Join(root, "pkg", "f.go"), []byte(src), 0o644)

	g := graph.NewMem()
	g.Upsert([]graph.Node{{ID: "go:pkg.F", File: "pkg/f.go", Line: 3, EndLine: 5}}, nil)
	exists := func(string) (string, bool) { return "The function SHALL return 1 exactly.", true }

	a := Stamp(ws, g, "X-TST-003", "The function SHALL return 1 exactly.", "go:pkg.F")
	rs := Classify(ws, g, []Anchor{a}, exists, nil)
	if rs[0].Class != OK {
		t.Fatalf("exact file/start/end match should be OK, got %s", rs[0].Class)
	}
}

// ---- DRF-003: staleness guard ----

// TestClassifyStalePending (DRF-003, test 3 of T-0107): a staleness
// predicate reporting the node's file as newer than the graph must
// short-circuit to Pending BEFORE any hash comparison — proven by handing
// Classify an anchor whose CHash could not possibly match the real span
// (so a hash comparison, if one ran, would classify it Evolved) and
// confirming Pending anyway, with no NewHash recorded (i.e. SpanHash was
// never even called).
func TestClassifyStalePending(t *testing.T) {
	root := t.TempDir()
	ws := workspace.Root{Dir: root}
	src := "package x\n\nfunc F() int {\n\treturn 1\n}\n"
	os.MkdirAll(filepath.Join(root, "pkg"), 0o755)
	os.WriteFile(filepath.Join(root, "pkg", "f.go"), []byte(src), 0o644)

	g := graph.NewMem()
	g.Upsert([]graph.Node{{ID: "go:pkg.F", File: "pkg/f.go", Line: 3, EndLine: 5}}, nil)
	exists := func(string) (string, bool) { return "The function SHALL return 1 exactly.", true }

	a := Stamp(ws, g, "X-TST-004", "The function SHALL return 1 exactly.", "go:pkg.F")
	a.CHash = "0000000000000000" // deliberately wrong: would classify Evolved if a hash comparison ran

	staleCalls := 0
	stale := func(file string) bool {
		staleCalls++
		if file != "pkg/f.go" {
			t.Fatalf("stale predicate called with unexpected file %q", file)
		}
		return true
	}
	rs := Classify(ws, g, []Anchor{a}, exists, stale)
	if rs[0].Class != Pending {
		t.Fatalf("stale graph should degrade to Pending regardless of the (deliberately wrong) hash, got %s", rs[0].Class)
	}
	if rs[0].NewHash != "" {
		t.Fatalf("Pending from staleness must not carry a computed hash — no comparison should have run, got %q", rs[0].NewHash)
	}
	if staleCalls == 0 {
		t.Fatalf("stale predicate was never called")
	}
}

// TestClassifyNilStalePreservesOldBehavior (DRF-003, test 4 of T-0107,
// regression guard): a nil staleness predicate must behave exactly like
// Classify did before DRF-003 — hash-based classification, unchanged.
// internal/lifecycle's audit gate and state's read-only drift section both
// call Classify with nil (see their call sites) and depend on this.
func TestClassifyNilStalePreservesOldBehavior(t *testing.T) {
	root := t.TempDir()
	ws := workspace.Root{Dir: root}
	os.MkdirAll(filepath.Join(root, "pkg"), 0o755)
	os.WriteFile(filepath.Join(root, "pkg", "f.go"), []byte("package x\n\nfunc F() int {\n\treturn 2\n}\n"), 0o644)

	g := graph.NewMem()
	g.Upsert([]graph.Node{{ID: "go:pkg.F", File: "pkg/f.go", Line: 3, EndLine: 5}}, nil)
	exists := func(string) (string, bool) { return "The function SHALL return 1 exactly.", true }

	// Anchor stamped against the OLD content ("return 1"); the file on disk
	// now says "return 2" — a genuine code-side change that nil staleness
	// must still classify as Evolved, exactly as pre-DRF-003 Classify did.
	a := Anchor{Rule: "X-TST-005", Node: "go:pkg.F", File: "pkg/f.go", Start: 3, End: 5,
		CHash: NormHash([]byte("func F() int {\n\treturn 1\n}")),
		RHash: NormHash([]byte("The function SHALL return 1 exactly."))}

	rs := Classify(ws, g, []Anchor{a}, exists, nil)
	if rs[0].Class != Evolved {
		t.Fatalf("nil staleness should not change hash-based classification, got %s", rs[0].Class)
	}
}

// TestClassifyFalseHealGuard (DRF-003, test 5 of T-0107, false-heal
// reproduction end to end): the graph's node position is stale by four
// lines — as if four lines were inserted above the function without a
// reindex, the exact go:main.main shape measured twice in this repo
// (stored ff8dc53a80235df9, computed 2fd917f5bdf2bc9d over the stale
// range, classified Evolved, auto-healed). The function's own content is
// untouched at its real, shifted location. Without a staleness signal,
// Classify hashes the NEW file at the OLD stale range and gets a
// different hash — a false Evolved, reproduced here first. With the
// staleness predicate wired, the same inputs must classify Pending
// instead, and Pending must never be auto-healed (see check() in
// internal/mcpserver/tools.go, which only heals Evolved).
func TestClassifyFalseHealGuard(t *testing.T) {
	root := t.TempDir()
	ws := workspace.Root{Dir: root}
	os.MkdirAll(filepath.Join(root, "pkg"), 0o755)
	orig := "package x\n\nfunc F() int {\n\treturn 1\n}\n"
	os.WriteFile(filepath.Join(root, "pkg", "f.go"), []byte(orig), 0o644)

	g := graph.NewMem()
	g.Upsert([]graph.Node{{ID: "go:pkg.F", File: "pkg/f.go", Line: 3, EndLine: 5}}, nil)
	exists := func(string) (string, bool) { return "The function SHALL return 1 exactly.", true }

	a := Stamp(ws, g, "X-TST-006", "The function SHALL return 1 exactly.", "go:pkg.F")

	// Four lines inserted above the function (its own body byte-for-byte
	// unchanged) with NO reindex: g still says Line=3/EndLine=5, but F's
	// real body now lives at 7-9.
	shifted := "package x\n\n// one\n// two\n// three\n// four\n\nfunc F() int {\n\treturn 1\n}\n"
	os.WriteFile(filepath.Join(root, "pkg", "f.go"), []byte(shifted), 0o644)

	// Reproduction, no staleness signal: the stale graph range (3-5) read
	// over the new file content is a different span, hashes differently,
	// and mis-classifies as Evolved — the exact false heal DRF-003 fixes.
	rs := Classify(ws, g, []Anchor{a}, exists, nil)
	if rs[0].Class != Evolved {
		t.Fatalf("setup check: expected the unguarded call to reproduce the false Evolved heal, got %s", rs[0].Class)
	}

	// With the staleness predicate wired, the same inputs must not
	// classify Evolved.
	rs = Classify(ws, g, []Anchor{a}, exists, func(string) bool { return true })
	if rs[0].Class != Pending {
		t.Fatalf("stale graph over a real (but relocated) node must not classify Evolved, got %s", rs[0].Class)
	}
}

// TestRebindByHash pins B-01KYJB3SGK end to end at the Classify layer: the
// two-main-packages incident. Content is identity; tilde numerals and bare
// IDs are walk-order hints that must never cross files silently.
func TestRebindByHash(t *testing.T) {
	root := t.TempDir()
	ws := workspace.Root{Dir: root}
	cliSrc := "package main\n\nfunc main() {\n\tprintln(\"cli\")\n}\n"
	exSrc := "package main\n\nfunc main() {\n\tprintln(\"example\")\n}\n"
	os.MkdirAll(filepath.Join(root, "examples", "saxpy"), 0o755)
	os.WriteFile(filepath.Join(root, "main.go"), []byte(cliSrc), 0o644)
	os.WriteFile(filepath.Join(root, "examples", "saxpy", "main.go"), []byte(exSrc), 0o644)

	// era 1: the CLI main owns the bare ID, the example gets ~2
	g1 := graph.NewMem()
	g1.Upsert([]graph.Node{
		{ID: "go:main.main", Kind: graph.KFunc, File: "main.go", Line: 3, EndLine: 5},
		{ID: "go:main.main~2", Kind: graph.KFunc, File: "examples/saxpy/main.go", Line: 3, EndLine: 5},
	}, nil)
	ruleText := "The CLI entrypoint SHALL call println exactly once."
	a := Stamp(ws, g1, "CLI-TST-001", ruleText, "go:main.main")
	if a.File != "main.go" {
		t.Fatalf("stamp landed on the wrong file: %+v", a)
	}
	exists := func(string) (string, bool) { return ruleText, true }

	// era 2: walk order flips — the EXAMPLE now owns the bare ID, the CLI
	// main renumbered to ~2 (the exact PR 177 incident). The stored ID
	// resolves to an unrelated file; the true target's hash is unchanged.
	g2 := graph.NewMem()
	g2.Upsert([]graph.Node{
		{ID: "go:main.main", Kind: graph.KFunc, File: "examples/saxpy/main.go", Line: 3, EndLine: 5},
		{ID: "go:main.main~2", Kind: graph.KFunc, File: "main.go", Line: 3, EndLine: 5},
	}, nil)
	rs := Classify(ws, g2, []Anchor{a}, exists, nil)
	if rs[0].Class != Moved {
		t.Fatalf("(a) hash-matching candidate exists: must rebind and class Moved, got %s", rs[0].Class)
	}
	if rs[0].NewNode != "go:main.main~2" {
		t.Fatalf("(a) NewNode must carry the re-bound ID, got %q", rs[0].NewNode)
	}

	// (c) the mirror renumbering: an anchor stored with ~2 whose node now
	// owns the bare ID rebinds the same way
	a2 := Stamp(ws, g1, "CLI-TST-002", ruleText, "go:main.main~2")
	g3 := graph.NewMem()
	g3.Upsert([]graph.Node{
		{ID: "go:main.main", Kind: graph.KFunc, File: "examples/saxpy/main.go", Line: 3, EndLine: 5},
	}, nil)
	rs = Classify(ws, g3, []Anchor{a2}, exists, nil)
	if rs[0].Class != Moved || rs[0].NewNode != "go:main.main" {
		t.Fatalf("(c) tilde anchor must rebind to the bare ID by hash: %s %q", rs[0].Class, rs[0].NewNode)
	}

	// (b) impostor only: the true target is gone, the stored ID resolves
	// to the unrelated file, no hash match anywhere -> CrossFile audit
	g4 := graph.NewMem()
	g4.Upsert([]graph.Node{
		{ID: "go:main.main", Kind: graph.KFunc, File: "examples/saxpy/main.go", Line: 3, EndLine: 5},
	}, nil)
	rs = Classify(ws, g4, []Anchor{a}, exists, nil)
	if rs[0].Class != CrossFile {
		t.Fatalf("(b) no hash match across files must audit as CrossFile, got %s", rs[0].Class)
	}
	if rs[0].OtherFile != "examples/saxpy/main.go" {
		t.Fatalf("(b) the audit must name the impostor file, got %q", rs[0].OtherFile)
	}

	// (d) ambiguity: TWO hash-matching candidates under the stem — refuse
	// to guess. The stored ID is gone entirely, both copies identical.
	os.MkdirAll(filepath.Join(root, "cmd", "twin"), 0o755)
	os.WriteFile(filepath.Join(root, "cmd", "twin", "main.go"), []byte(cliSrc), 0o644)
	g5 := graph.NewMem()
	g5.Upsert([]graph.Node{
		{ID: "go:main.main~3", Kind: graph.KFunc, File: "main.go", Line: 3, EndLine: 5},
		{ID: "go:main.main~4", Kind: graph.KFunc, File: "cmd/twin/main.go", Line: 3, EndLine: 5},
	}, nil)
	rs = Classify(ws, g5, []Anchor{a}, exists, nil)
	if rs[0].Class != Gone {
		t.Fatalf("(d) two identical-hash candidates must refuse the rebind (Gone), got %s", rs[0].Class)
	}
	if rs[0].NewNode != "" {
		t.Fatalf("(d) an ambiguous rebind must not set NewNode: %q", rs[0].NewNode)
	}
}

// ---- B-01KYN5ZYM1FY2: unresolvable vs pending ----

// TestClassifyUnresolvableVsPending pins the distinction the bug report is
// about: an anchor naming a FILE PATH and an anchor naming a symbol that is
// merely not indexed yet used to classify identically (Pending), because
// the `CHash == "-"` arm ran first and swallowed both. They are different
// states — a reindex clears the second and can never clear the first — so
// they must classify differently, while a healthy stamped anchor stays OK.
func TestClassifyUnresolvableVsPending(t *testing.T) {
	root := t.TempDir()
	ws := workspace.Root{Dir: root}
	src := "package x\n\nfunc F() int {\n\treturn 1\n}\n"
	os.MkdirAll(filepath.Join(root, "pkg"), 0o755)
	os.WriteFile(filepath.Join(root, "pkg", "f.go"), []byte(src), 0o644)

	g := graph.NewMem()
	g.Upsert([]graph.Node{{ID: "go:pkg.F", File: "pkg/f.go", Line: 3, EndLine: 5}}, nil)
	sentence := "The function SHALL return 1 exactly."
	exists := func(string) (string, bool) { return sentence, true }

	// The exact argument shape from the incident: applies=[<a file path>].
	path := Anchor{Rule: "X-TST-010", Node: "pkg/f.go", File: "-", CHash: "-",
		RHash: NormHash([]byte(sentence))}
	// A real node ID for a symbol the indexer has not reached yet.
	notYet := Anchor{Rule: "X-TST-011", Node: "go:pkg.NotYet", File: "-", CHash: "-",
		RHash: NormHash([]byte(sentence))}
	healthy := Stamp(ws, g, "X-TST-012", sentence, "go:pkg.F")

	rs := Classify(ws, g, []Anchor{path, notYet, healthy}, exists, nil)
	if rs[0].Class != Unresolvable {
		t.Fatalf("a path-shaped anchor must classify Unresolvable, got %s", rs[0].Class)
	}
	if rs[1].Class != Pending {
		t.Fatalf("an un-indexed but well-shaped node ID must stay Pending, got %s", rs[1].Class)
	}
	if rs[2].Class != OK {
		t.Fatalf("a stamped anchor must stay OK, got %s", rs[2].Class)
	}
}

// TestClassifyPendingBindsWhenNodeAppears pins the other half of
// B-01KYN5ZYM1FY2: Pending is a WAIT, and the wait has to end. An anchor
// stamped before its node existed must become Bound — carrying a REAL
// NewHash for the caller to write back — the moment the node is in the
// graph. Without this the anchor keeps its "-" hash forever, because
// nothing else in the pipeline ever re-stamps one.
func TestClassifyPendingBindsWhenNodeAppears(t *testing.T) {
	root := t.TempDir()
	ws := workspace.Root{Dir: root}
	src := "package x\n\nfunc F() int {\n\treturn 1\n}\n"
	os.MkdirAll(filepath.Join(root, "pkg"), 0o755)
	os.WriteFile(filepath.Join(root, "pkg", "f.go"), []byte(src), 0o644)

	sentence := "The function SHALL return 1 exactly."
	exists := func(string) (string, bool) { return sentence, true }

	// Stamped against an EMPTY graph: pending, exactly as rule op=add
	// writes it when the symbol is not indexed yet.
	empty := graph.NewMem()
	a := Stamp(ws, empty, "X-TST-013", sentence, "go:pkg.F")
	if a.CHash != "-" {
		t.Fatalf("setup: stamping against an empty graph must yield a pending anchor, got CHash %q", a.CHash)
	}

	g := graph.NewMem()
	g.Upsert([]graph.Node{{ID: "go:pkg.F", File: "pkg/f.go", Line: 3, EndLine: 5}}, nil)
	rs := Classify(ws, g, []Anchor{a}, exists, nil)
	if rs[0].Class != Bound {
		t.Fatalf("a pending anchor whose node is now indexed must classify Bound, got %s", rs[0].Class)
	}
	want, err := SpanHash(ws, "pkg/f.go", 3, 5)
	if err != nil {
		t.Fatal(err)
	}
	if rs[0].NewHash != want {
		t.Fatalf("Bound must carry the node's real span hash %q, got %q", want, rs[0].NewHash)
	}

	// The staleness refusal still wins over binding (DRF-003): a graph
	// older than the file cannot be trusted for Line/EndLine, so no hash
	// is computed at all and the anchor keeps waiting.
	rs = Classify(ws, g, []Anchor{a}, exists, func(string) bool { return true })
	if rs[0].Class != Pending {
		t.Fatalf("a stale file must keep the anchor Pending, not bind it, got %s", rs[0].Class)
	}
	if rs[0].NewHash != "" {
		t.Fatalf("a stale-derived Pending must carry no hash, got %q", rs[0].NewHash)
	}
}
