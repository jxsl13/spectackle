package resolve

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"

	"github.com/jxsl13/spectackle/internal/graph"
)

// memFS is a tiny in-memory FileSet: every entry in files is treated as a Go
// source file; paths listed in readErr are reported by ByLang but fail Read.
type memFS struct {
	files   map[string]string
	readErr map[string]bool
}

func (m memFS) ByLang(l graph.Lang) []string {
	if l != graph.LangGo {
		return nil
	}
	var paths []string
	for p := range m.files {
		paths = append(paths, p)
	}
	for p := range m.readErr {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return paths
}

func (m memFS) Read(path string) ([]byte, error) {
	if m.readErr[path] {
		return nil, errors.New("memFS: injected read error")
	}
	src, ok := m.files[path]
	if !ok {
		return nil, errors.New("memFS: no such file")
	}
	return []byte(src), nil
}

// resolveCgo runs CgoResolver over an in-memory file set and fails the test on
// error (the resolver contract is: skip bad files, never error).
func resolveCgo(t *testing.T, fs FileSet) []graph.Edge {
	t.Helper()
	edges, err := (CgoResolver{}).Resolve(context.Background(), graph.NewMem(), fs)
	if err != nil {
		t.Fatalf("CgoResolver.Resolve returned error: %v", err)
	}
	return edges
}

// lineOf returns the 1-based line of the first occurrence of substr in src.
func lineOf(t *testing.T, src, substr string) int {
	t.Helper()
	i := strings.Index(src, substr)
	if i < 0 {
		t.Fatalf("substring %q not found in source", substr)
	}
	return 1 + strings.Count(src[:i], "\n")
}

func TestCgoResolverForwardCall(t *testing.T) {
	src := `package saxpy

/*
#include "saxpy.h"
*/
import "C"

func Saxpy(n int, a float32, x, y []float32) {
	C.launch_saxpy(C.int(n), C.float(a), nil, nil)
}
`
	edges := resolveCgo(t, memFS{files: map[string]string{"gpu/saxpy.go": src}})
	if len(edges) != 1 {
		t.Fatalf("want exactly 1 edge, got %d: %+v", len(edges), edges)
	}
	e := edges[0]
	if e.Src != "go:saxpy.Saxpy" || e.Dst != "c:launch_saxpy" {
		t.Errorf("edge endpoints = %q -> %q, want go:saxpy.Saxpy -> c:launch_saxpy", e.Src, e.Dst)
	}
	if e.Kind != graph.ECgo {
		t.Errorf("edge kind = %v, want ECgo", e.Kind)
	}
	if e.File != "gpu/saxpy.go" {
		t.Errorf("edge file = %q, want gpu/saxpy.go", e.File)
	}
	if want := lineOf(t, src, "C.launch_saxpy"); e.Line != want {
		t.Errorf("edge line = %d, want %d (line of the call expression)", e.Line, want)
	}
}

func TestCgoResolverPrimitiveConversionsAreNotEdges(t *testing.T) {
	src := `package conv

import "C"

import "unsafe"

func Convert(n int, a float32, p unsafe.Pointer) {
	_ = C.int(n)
	_ = C.float(a)
	_ = (*C.float)(unsafe.Pointer(p))
}
`
	edges := resolveCgo(t, memFS{files: map[string]string{"conv.go": src}})
	if len(edges) != 0 {
		t.Fatalf("primitive conversions must not create edges, got %+v", edges)
	}
}

func TestCgoResolverExportDirective(t *testing.T) {
	src := `package cb

import "C"

//export MyCallback
func MyCallback(v C.int) {
	_ = v
}
`
	edges := resolveCgo(t, memFS{files: map[string]string{"cb.go": src}})
	if len(edges) != 1 {
		t.Fatalf("want exactly 1 reverse edge, got %d: %+v", len(edges), edges)
	}
	e := edges[0]
	if e.Src != "c:MyCallback" || e.Dst != "go:cb.MyCallback" {
		t.Errorf("edge endpoints = %q -> %q, want c:MyCallback -> go:cb.MyCallback", e.Src, e.Dst)
	}
	if e.Kind != graph.ECgo {
		t.Errorf("edge kind = %v, want ECgo", e.Kind)
	}
	if e.File != "cb.go" {
		t.Errorf("edge file = %q, want cb.go", e.File)
	}
	if want := lineOf(t, src, "func MyCallback"); e.Line != want {
		t.Errorf("edge line = %d, want %d (line of the function decl)", e.Line, want)
	}
}

func TestCgoResolverNonCgoFileIgnored(t *testing.T) {
	// No import "C" anywhere; "C.x" only appears inside a string literal.
	src := `package pure

const note = "call C.x here has no meaning"

func Nothing() {
	_ = note
}
`
	edges := resolveCgo(t, memFS{files: map[string]string{"pure.go": src}})
	if len(edges) != 0 {
		t.Fatalf("file without import \"C\" must yield no edges, got %+v", edges)
	}
}

func TestCgoResolverMethodReceiver(t *testing.T) {
	src := `package dev

import "C"

type Dev struct{}

func (d *Dev) Close() {
	C.dev_close(nil)
}
`
	edges := resolveCgo(t, memFS{files: map[string]string{"dev.go": src}})
	if len(edges) != 1 {
		t.Fatalf("want exactly 1 edge, got %d: %+v", len(edges), edges)
	}
	e := edges[0]
	if e.Src != "go:dev.Dev.Close" || e.Dst != "c:dev_close" {
		t.Errorf("edge endpoints = %q -> %q, want go:dev.Dev.Close -> c:dev_close", e.Src, e.Dst)
	}
}

func TestCgoResolverDedupesCallsWithinFunction(t *testing.T) {
	src := `package dup

import "C"

func Twice() {
	C.do_work(nil)
	C.do_work(nil)
}
`
	edges := resolveCgo(t, memFS{files: map[string]string{"dup.go": src}})
	if len(edges) != 1 {
		t.Fatalf("duplicate calls to the same C symbol must dedupe to 1 edge, got %d: %+v", len(edges), edges)
	}
	e := edges[0]
	if e.Src != "go:dup.Twice" || e.Dst != "c:do_work" {
		t.Errorf("edge endpoints = %q -> %q, want go:dup.Twice -> c:do_work", e.Src, e.Dst)
	}
	if want := lineOf(t, src, "C.do_work"); e.Line != want {
		t.Errorf("edge line = %d, want %d (first call site)", e.Line, want)
	}
}

func TestCgoResolverBodylessDeclaration(t *testing.T) {
	// Regression: a bodyless declaration (asm stub / linkname target) in a
	// cgo file used to panic ast.Inspect with a nil *ast.BlockStmt.
	src := `package stub

import "C"

//go:noescape
func ceil(x float64) float64

//export Exported
func Exported() {
	C.real_call(nil)
}
`
	edges := resolveCgo(t, memFS{files: map[string]string{"stub.go": src}})
	// the bodyless decl yields nothing; Exported yields its forward call
	// edge plus the //export reverse edge.
	if len(edges) != 2 {
		t.Fatalf("want 2 edges (forward + reverse for Exported), got %d: %+v", len(edges), edges)
	}
	for _, e := range edges {
		if e.Src == "go:stub.ceil" || e.Dst == "go:stub.ceil" {
			t.Errorf("bodyless declaration must not produce edges, got %+v", e)
		}
	}
}

func TestCgoResolverNilFileSet(t *testing.T) {
	edges, err := (CgoResolver{}).Resolve(context.Background(), graph.NewMem(), nil)
	if err != nil {
		t.Fatalf("fs=nil must not error, got %v", err)
	}
	if edges != nil {
		t.Fatalf("fs=nil must yield nil edges, got %+v", edges)
	}
}

func TestCgoResolverSkipsUnparsableAndUnreadableFiles(t *testing.T) {
	good := `package ok

import "C"

func Fine() {
	C.fine_call(nil)
}
`
	broken := `package broken

import "C"

func oops( {
`
	fs := memFS{
		files: map[string]string{
			"a_broken.go": broken,
			"b_good.go":   good,
		},
		readErr: map[string]bool{"c_unreadable.go": true},
	}
	edges := resolveCgo(t, fs)
	if len(edges) != 1 {
		t.Fatalf("broken/unreadable files must be skipped without aborting, got %d edges: %+v", len(edges), edges)
	}
	if e := edges[0]; e.Src != "go:ok.Fine" || e.Dst != "c:fine_call" {
		t.Errorf("edge endpoints = %q -> %q, want go:ok.Fine -> c:fine_call", e.Src, e.Dst)
	}
}
