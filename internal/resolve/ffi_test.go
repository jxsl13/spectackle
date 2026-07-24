package resolve

import (
	"context"
	"errors"
	"testing"

	"github.com/jxsl13/spectackle/internal/graph"
)

// ffiFS is a tiny in-memory FileSet exposing .c files under LangC and .cpp
// files under LangCpp.
type ffiFS struct {
	c   map[string]string
	cpp map[string]string
}

func (f ffiFS) ByLang(l graph.Lang) []string {
	var m map[string]string
	switch l {
	case graph.LangC:
		m = f.c
	case graph.LangCpp:
		m = f.cpp
	default:
		return nil
	}
	var paths []string
	for p := range m {
		paths = append(paths, p)
	}
	return paths
}

func (f ffiFS) Read(path string) ([]byte, error) {
	if src, ok := f.c[path]; ok {
		return []byte(src), nil
	}
	if src, ok := f.cpp[path]; ok {
		return []byte(src), nil
	}
	return nil, errors.New("ffiFS: no such file")
}

// resolveFFI runs FFIResolver over an in-memory file set and fails the test
// on error.
func resolveFFI(t *testing.T, g graph.Graph, fs FileSet) []graph.Edge {
	t.Helper()
	edges, err := (FFIResolver{}).Resolve(context.Background(), g, fs)
	if err != nil {
		t.Fatalf("FFIResolver.Resolve returned error: %v", err)
	}
	return edges
}

// TestFFIResolverBridgesDanglingCppToC is the task-brief acceptance case:
// cpp:engine.Render calls a dangling cpp:str_copy, but c:str_copy exists
// (declared in a header, defined in C) — the resolver must bridge
// cpp:engine.Render -> c:str_copy while leaving the original dangling
// cpp:engine.Render -> cpp:str_copy edge (minted by the primary langspec
// pass) untouched.
func TestFFIResolverBridgesDanglingCppToC(t *testing.T) {
	cppSrc := `void Engine::Render() {
    str_copy(dst, src);
}
`
	g := graph.NewMem()
	g.Upsert([]graph.Node{
		{ID: "cpp:Render", Kind: graph.KFunc, Lang: graph.LangCpp, File: "engine.cpp", Line: 1, EndLine: 3},
		{ID: "c:str_copy", Kind: graph.KFunc, Lang: graph.LangC, File: "strutil.c", Line: 1, EndLine: 3},
	}, []graph.Edge{
		{Src: "cpp:Render", Dst: "cpp:str_copy", Kind: graph.ECall, File: "engine.cpp", Line: 2},
	})

	fs := ffiFS{cpp: map[string]string{"engine.cpp": cppSrc}}
	edges := resolveFFI(t, g, fs)

	if len(edges) != 1 {
		t.Fatalf("want exactly 1 bridged edge, got %d: %+v", len(edges), edges)
	}
	e := edges[0]
	if e.Src != "cpp:Render" || e.Dst != "c:str_copy" || e.Kind != graph.ECall {
		t.Errorf("edge = %+v, want cpp:Render -ECall-> c:str_copy", e)
	}
	if e.File != "engine.cpp" || e.Line != 2 {
		t.Errorf("edge File/Line = %q/%d, want engine.cpp/2", e.File, e.Line)
	}

	// the original dangling cpp:Render -> cpp:str_copy edge from the primary
	// pass must remain untouched — resolvers only add (RSV-001).
	orig := g.Neighbors("cpp:Render", graph.Out, []graph.EdgeKind{graph.ECall})
	foundOrig := false
	for _, oe := range orig {
		if oe.Dst == "cpp:str_copy" {
			foundOrig = true
		}
	}
	if !foundOrig {
		t.Errorf("original dangling cpp:Render -> cpp:str_copy edge was removed, resolvers must only add: %+v", orig)
	}
}

// TestFFIResolverBridgesAllmanCppToC is T-0054's acceptance case: before
// this task, ffi.go's body-span scanning (ffiBraceSpan) was an independent,
// unfixed, K&R-only copy of internal/langspec's pre-T-0053 braceSpan, so an
// Allman-bodied method def (the opening '{' on its own following line, the
// universal style in real-world codebases like ddnet — see
// docs/validation-ddnet.md) was never scanned at all: ffiBraceSpan bailed
// immediately because the def line itself had no '{'. T-0054 switched
// ffi.go to internal/cspan.Span (the same, already-Allman-aware scanner the
// primary langspec pass uses), so this now bridges exactly like the
// existing K&R fixture above. cpp:Engine.Render's body is Allman
// (`Render()` then `{` on the next line); c:str_copy is itself defined
// Allman-style too (included for realism, though the resolver only needs
// the pre-minted c:str_copy graph node, not to re-derive it from strutil.c
// — same as every other fixture in this file).
func TestFFIResolverBridgesAllmanCppToC(t *testing.T) {
	cppSrc := `void Engine::Render()
{
    str_copy(dst, src);
}
`
	cSrc := `void str_copy(char *dst, const char *src)
{
    dst[0] = src[0];
}
`
	g := graph.NewMem()
	g.Upsert([]graph.Node{
		{ID: "cpp:Render", Kind: graph.KFunc, Lang: graph.LangCpp, File: "engine.cpp", Line: 1, EndLine: 4},
		{ID: "c:str_copy", Kind: graph.KFunc, Lang: graph.LangC, File: "strutil.c", Line: 1, EndLine: 3},
	}, []graph.Edge{
		{Src: "cpp:Render", Dst: "cpp:str_copy", Kind: graph.ECall, File: "engine.cpp", Line: 3},
	})

	fs := ffiFS{
		cpp: map[string]string{"engine.cpp": cppSrc},
		c:   map[string]string{"strutil.c": cSrc},
	}
	edges := resolveFFI(t, g, fs)

	if len(edges) != 1 {
		t.Fatalf("want exactly 1 bridged edge, got %d: %+v", len(edges), edges)
	}
	e := edges[0]
	if e.Src != "cpp:Render" || e.Dst != "c:str_copy" || e.Kind != graph.ECall {
		t.Errorf("edge = %+v, want cpp:Render -ECall-> c:str_copy", e)
	}
	if e.File != "engine.cpp" || e.Line != 3 {
		t.Errorf("edge File/Line = %q/%d, want engine.cpp/3", e.File, e.Line)
	}
}

// TestFFIResolverBridgesDanglingCToCpp mirrors the above for the c: -> cpp:
// direction.
func TestFFIResolverBridgesDanglingCToCpp(t *testing.T) {
	cSrc := `void c_entry(void) {
    cpp_handler(1);
}
`
	g := graph.NewMem()
	g.Upsert([]graph.Node{
		{ID: "c:c_entry", Kind: graph.KFunc, Lang: graph.LangC, File: "bridge.c", Line: 1, EndLine: 3},
		{ID: "cpp:cpp_handler", Kind: graph.KFunc, Lang: graph.LangCpp, File: "handler.cpp", Line: 1, EndLine: 3},
	}, nil)

	fs := ffiFS{c: map[string]string{"bridge.c": cSrc}}
	edges := resolveFFI(t, g, fs)

	if len(edges) != 1 {
		t.Fatalf("want exactly 1 bridged edge, got %d: %+v", len(edges), edges)
	}
	e := edges[0]
	if e.Src != "c:c_entry" || e.Dst != "cpp:cpp_handler" || e.Kind != graph.ECall {
		t.Errorf("edge = %+v, want c:c_entry -ECall-> cpp:cpp_handler", e)
	}
	if e.File != "bridge.c" || e.Line != 2 {
		t.Errorf("edge File/Line = %q/%d, want bridge.c/2", e.File, e.Line)
	}
}

// TestFFIResolverNoOpWhenRealNodeExists: when the callee already resolves
// within its own language, the resolver must not add a bridge edge — the
// primary pass already connected it.
func TestFFIResolverNoOpWhenRealNodeExists(t *testing.T) {
	cppSrc := `void Engine::Render() {
    str_copy(dst, src);
}
`
	g := graph.NewMem()
	g.Upsert([]graph.Node{
		{ID: "cpp:Render", Kind: graph.KFunc, Lang: graph.LangCpp, File: "engine.cpp", Line: 1, EndLine: 3},
		{ID: "cpp:str_copy", Kind: graph.KFunc, Lang: graph.LangCpp, File: "engine.cpp", Line: 10, EndLine: 12},
		{ID: "c:str_copy", Kind: graph.KFunc, Lang: graph.LangC, File: "strutil.c", Line: 1, EndLine: 3},
	}, []graph.Edge{
		{Src: "cpp:Render", Dst: "cpp:str_copy", Kind: graph.ECall, File: "engine.cpp", Line: 2},
	})

	fs := ffiFS{cpp: map[string]string{"engine.cpp": cppSrc}}
	edges := resolveFFI(t, g, fs)

	if len(edges) != 0 {
		t.Fatalf("want no bridge edge when cpp:str_copy already exists, got %d: %+v", len(edges), edges)
	}
}

// TestFFIResolverNoOpWhenSiblingMissing: dangling in both languages stays
// dangling — no fabricated bridge.
func TestFFIResolverNoOpWhenSiblingMissing(t *testing.T) {
	cppSrc := `void Engine::Render() {
    totally_unknown(dst, src);
}
`
	g := graph.NewMem()
	g.Upsert([]graph.Node{
		{ID: "cpp:Render", Kind: graph.KFunc, Lang: graph.LangCpp, File: "engine.cpp", Line: 1, EndLine: 3},
	}, []graph.Edge{
		{Src: "cpp:Render", Dst: "cpp:totally_unknown", Kind: graph.ECall, File: "engine.cpp", Line: 2},
	})

	fs := ffiFS{cpp: map[string]string{"engine.cpp": cppSrc}}
	edges := resolveFFI(t, g, fs)

	if len(edges) != 0 {
		t.Fatalf("want no bridge edge when neither language has the callee, got %d: %+v", len(edges), edges)
	}
}

// TestFFIResolverIgnoresStopWords: `if (`/`sizeof(` inside a def's body must
// never be treated as a callee candidate, mirroring cSpec/cppSpec's Stop
// list (LSP-001 PART 2).
func TestFFIResolverIgnoresStopWords(t *testing.T) {
	cppSrc := `void Engine::Render() {
    if (ready) {
        return;
    }
    int sz = sizeof(int);
}
`
	g := graph.NewMem()
	g.Upsert([]graph.Node{
		{ID: "cpp:Render", Kind: graph.KFunc, Lang: graph.LangCpp, File: "engine.cpp", Line: 1, EndLine: 6},
		{ID: "c:if", Kind: graph.KFunc, Lang: graph.LangC, File: "junk.c", Line: 1, EndLine: 1},
		{ID: "c:sizeof", Kind: graph.KFunc, Lang: graph.LangC, File: "junk.c", Line: 2, EndLine: 2},
	}, nil)

	fs := ffiFS{cpp: map[string]string{"engine.cpp": cppSrc}}
	edges := resolveFFI(t, g, fs)

	if len(edges) != 0 {
		t.Fatalf("want no bridge edges for Stop-listed if/sizeof, got %d: %+v", len(edges), edges)
	}
}

// TestFFIResolverNilFileSet mirrors the other resolvers' fs=nil contract.
func TestFFIResolverNilFileSet(t *testing.T) {
	edges, err := (FFIResolver{}).Resolve(context.Background(), graph.NewMem(), nil)
	if err != nil {
		t.Fatalf("fs=nil must not error, got %v", err)
	}
	if edges != nil {
		t.Fatalf("fs=nil must yield nil edges, got %+v", edges)
	}
}

// TestFFIResolverSkipsUnreadableFiles mirrors the other resolvers' contract:
// a file that fails to read is skipped, not fatal to the whole run.
func TestFFIResolverSkipsUnreadableFiles(t *testing.T) {
	g := graph.NewMem()
	g.Upsert([]graph.Node{
		{ID: "c:c_entry", Kind: graph.KFunc, Lang: graph.LangC, File: "good.c", Line: 1, EndLine: 3},
		{ID: "cpp:cpp_handler", Kind: graph.KFunc, Lang: graph.LangCpp, File: "handler.cpp", Line: 1, EndLine: 3},
	}, nil)

	fs := ffiFS{c: map[string]string{
		"good.c": "void c_entry(void) {\n    cpp_handler(1);\n}\n",
	}}
	// unreadable.c is listed by ByLang but Read fails for it — simulate via
	// a dedicated FileSet wrapper.
	edges := resolveFFI(t, g, unreadableWrap{fs, "unreadable.c"})
	if len(edges) != 1 {
		t.Fatalf("want exactly 1 edge (from good.c), got %d: %+v", len(edges), edges)
	}
}

// unreadableWrap adds one always-failing path to an underlying FileSet's
// ByLang(LangC) listing.
type unreadableWrap struct {
	FileSet
	extra string
}

func (w unreadableWrap) ByLang(l graph.Lang) []string {
	paths := w.FileSet.ByLang(l)
	if l == graph.LangC {
		paths = append(paths, w.extra)
	}
	return paths
}

func (w unreadableWrap) Read(path string) ([]byte, error) {
	if path == w.extra {
		return nil, errors.New("unreadableWrap: injected read error")
	}
	return w.FileSet.Read(path)
}
