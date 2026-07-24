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

	rs := Classify(ws, g, []Anchor{a}, exists)
	if rs[0].Class != OK {
		t.Fatalf("fresh anchor should be OK, got %s", rs[0].Class)
	}

	// pure line shift => Moved (silent refresh): same content hash, same rule hash
	os.WriteFile(filepath.Join(root, "pkg", "f.go"), []byte("package x\n\n// shifted\n\nfunc F() int {\n\treturn 1\n}\n"), 0o644)
	g2 := graph.NewMem()
	g2.Upsert([]graph.Node{{ID: "go:pkg.F", File: "pkg/f.go", Line: 5, EndLine: 7}}, nil)
	rs = Classify(ws, g2, []Anchor{a}, exists)
	if rs[0].Class != Moved {
		t.Fatalf("line shift should be Moved, got %s", rs[0].Class)
	}

	// code changed, rule sentence identical => Evolved (mechanically healable)
	os.WriteFile(filepath.Join(root, "pkg", "f.go"), []byte("package x\n\nfunc F() int {\n\treturn 2\n}\n"), 0o644)
	g3 := graph.NewMem()
	g3.Upsert([]graph.Node{{ID: "go:pkg.F", File: "pkg/f.go", Line: 3, EndLine: 5}}, nil)
	rs = Classify(ws, g3, []Anchor{a}, exists)
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
	rs = Classify(ws, g, []Anchor{a}, textOf("The function SHALL return 2 exactly."))
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
	rs = Classify(ws, g3, []Anchor{a}, textOf("The function SHALL return 2 exactly."))
	if rs[0].Class != Diverged {
		t.Fatalf("both-sides change should be Diverged, got %s", rs[0].Class)
	}

	// node vanished => Gone; rule vanished => Stale; empty graph => Pending
	g4 := graph.NewMem()
	g4.Upsert([]graph.Node{{ID: "go:pkg.Other", File: "pkg/f.go", Line: 1}}, nil)
	rs = Classify(ws, g4, []Anchor{a}, exists)
	if rs[0].Class != Gone {
		t.Fatalf("missing node should be Gone, got %s", rs[0].Class)
	}
	rs = Classify(ws, g3, []Anchor{a}, func(string) (string, bool) { return "", false })
	if rs[0].Class != Stale {
		t.Fatalf("missing rule should be Stale, got %s", rs[0].Class)
	}
	rs = Classify(ws, graph.NewMem(), []Anchor{a}, exists)
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
