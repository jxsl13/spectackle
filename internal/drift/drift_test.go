package drift

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jxsl13/spectacle/internal/graph"
	"github.com/jxsl13/spectacle/internal/workspace"
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

	exists := func(string) bool { return true }
	rs := Classify(ws, g, []Anchor{a}, exists)
	if rs[0].Class != OK {
		t.Fatalf("fresh anchor should be OK, got %s", rs[0].Class)
	}

	// pure line shift => Moved (silent refresh), same content hash
	os.WriteFile(filepath.Join(root, "pkg", "f.go"), []byte("package x\n\n// shifted\n\nfunc F() int {\n\treturn 1\n}\n"), 0o644)
	g2 := graph.NewMem()
	g2.Upsert([]graph.Node{{ID: "go:pkg.F", File: "pkg/f.go", Line: 5, EndLine: 7}}, nil)
	rs = Classify(ws, g2, []Anchor{a}, exists)
	if rs[0].Class != Moved {
		t.Fatalf("line shift should be Moved, got %s", rs[0].Class)
	}

	// content change => Changed
	os.WriteFile(filepath.Join(root, "pkg", "f.go"), []byte("package x\n\nfunc F() int {\n\treturn 2\n}\n"), 0o644)
	g3 := graph.NewMem()
	g3.Upsert([]graph.Node{{ID: "go:pkg.F", File: "pkg/f.go", Line: 3, EndLine: 5}}, nil)
	rs = Classify(ws, g3, []Anchor{a}, exists)
	if rs[0].Class != Changed {
		t.Fatalf("content change should be Changed, got %s", rs[0].Class)
	}

	// node vanished => Gone; rule vanished => Stale; empty graph => Pending
	g4 := graph.NewMem()
	g4.Upsert([]graph.Node{{ID: "go:pkg.Other", File: "pkg/f.go", Line: 1}}, nil)
	rs = Classify(ws, g4, []Anchor{a}, exists)
	if rs[0].Class != Gone {
		t.Fatalf("missing node should be Gone, got %s", rs[0].Class)
	}
	rs = Classify(ws, g3, []Anchor{a}, func(string) bool { return false })
	if rs[0].Class != Stale {
		t.Fatalf("missing rule should be Stale, got %s", rs[0].Class)
	}
	rs = Classify(ws, graph.NewMem(), []Anchor{a}, exists)
	if rs[0].Class != Pending {
		t.Fatalf("empty graph should degrade to Pending, got %s", rs[0].Class)
	}
}
