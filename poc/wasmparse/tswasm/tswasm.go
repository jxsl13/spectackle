// Package tswasm wraps github.com/malivvan/tree-sitter (tree-sitter core +
// tree-sitter-c statically compiled to one WASI wasm blob, executed via
// tetratelabs/wazero) and reduces its parse tree to the same (name, kind,
// line) triple.Triple shape internal/langspec's cSpec oracle produces, so
// T-0040 can diff the two directly.
//
// malivvan/tree-sitter's Go binding (v0.0.1) is a thin, low-level shim: no
// field-based child access (e.g. "give me the declarator field"), only
// positional Child(i)/ChildCount plus Kind() per node (see the vendored
// node.go/tree.go read at poc/wasmparse setup time). Symbol extraction here
// therefore walks the raw child list and pattern-matches by node kind
// string, mirroring what a hand-written cgo tree-sitter walker in
// internal/index would have to do too (see docs/design-wasm-parsers.md §3).
package tswasm

import (
	"context"
	"fmt"
	"sort"

	sitter "github.com/malivvan/tree-sitter"

	"github.com/jxsl13/spectacle/poc/wasmparse/triple"
)

// Extractor owns one wazero-backed TreeSitter engine plus one reusable
// parser instance. Compiling+instantiating the wasm module (New) is the
// PoC's "cold" cost; ParseFile calls on an existing Extractor are "warm".
type Extractor struct {
	ts     sitter.TreeSitter
	parser sitter.Parser
	lang   sitter.Language
}

// New compiles and instantiates the embedded tree-sitter-core+C wasm module
// and prepares a single reusable C parser. This is the expensive,
// once-per-process step (wazero compiler-mode JIT of a 4.2MB module).
func New(ctx context.Context) (*Extractor, error) {
	ts, err := sitter.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("tswasm: instantiating tree-sitter wasm engine: %w", err)
	}
	lang, err := ts.LanguageC(ctx)
	if err != nil {
		return nil, fmt.Errorf("tswasm: loading C grammar: %w", err)
	}
	p, err := ts.NewParser(ctx)
	if err != nil {
		return nil, fmt.Errorf("tswasm: creating parser: %w", err)
	}
	if err := p.SetLanguage(ctx, lang); err != nil {
		return nil, fmt.Errorf("tswasm: setting C language: %w", err)
	}
	return &Extractor{ts: ts, parser: p, lang: lang}, nil
}

// ParseFile parses one file's source with the shared parser and reduces the
// resulting tree to triple.Triple values.
func (e *Extractor) ParseFile(ctx context.Context, src []byte) ([]triple.Triple, error) {
	tree, err := e.parser.ParseString(ctx, string(src))
	if err != nil {
		return nil, fmt.Errorf("parsing: %w", err)
	}
	root, err := tree.RootNode(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting root node: %w", err)
	}

	lo := newLineOffsets(src)
	w := &walker{ctx: ctx, src: src, lo: lo}
	if err := w.walk(root); err != nil {
		return nil, err
	}
	return w.out, nil
}

type walker struct {
	ctx context.Context
	src []byte
	lo  lineOffsets
	out []triple.Triple
}

// walk is a full pre-order DFS over the parse tree. It does not prune
// subtrees after a match: a struct tag referenced in a function's return
// type or parameter list is a distinct AST node from the function itself
// and is visited (and, if named, emitted) independently — this is
// deliberate and mirrors how cSpec's per-line regex scan can also emit two
// triples from one physical line (see internal/langspec/c.go's doc comment
// on `struct Point *make_point(void) {`).
func (w *walker) walk(n sitter.Node) error {
	kind, err := n.Kind(w.ctx)
	if err != nil {
		return fmt.Errorf("node kind: %w", err)
	}

	switch kind {
	case "function_definition", "declaration":
		if err := w.emitFunction(n); err != nil {
			return err
		}
	case "struct_specifier", "union_specifier", "enum_specifier":
		if err := w.emitType(n); err != nil {
			return err
		}
	case "preproc_function_def":
		if err := w.emitMacro(n); err != nil {
			return err
		}
	}

	cnt, err := n.ChildCount(w.ctx)
	if err != nil {
		return fmt.Errorf("child count: %w", err)
	}
	for i := uint64(0); i < cnt; i++ {
		c, err := n.Child(w.ctx, i)
		if err != nil {
			return fmt.Errorf("child %d: %w", i, err)
		}
		if err := w.walk(c); err != nil {
			return err
		}
	}
	return nil
}

// emitFunction handles function_definition and declaration nodes: only
// mints a triple.KindFunc if a function_declarator is found in the
// subtree (a plain "int x;" declaration node has no function_declarator
// and is correctly skipped, matching cSpec which also requires "(...)").
func (w *walker) emitFunction(n sitter.Node) error {
	fd, ok, err := findKind(w.ctx, n, "function_declarator")
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	nameNode, ok, err := directChildOfKind(w.ctx, fd, "identifier")
	if err != nil {
		return err
	}
	if !ok {
		return nil // e.g. function pointer declarators — out of PoC scope
	}
	name, err := w.text(nameNode)
	if err != nil {
		return err
	}
	line, err := w.lineOf(nameNode)
	if err != nil {
		return err
	}
	w.out = append(w.out, triple.Triple{Name: name, Kind: triple.KindFunc, Line: line})
	return nil
}

func (w *walker) emitType(n sitter.Node) error {
	nameNode, ok, err := directChildOfKind(w.ctx, n, "type_identifier")
	if err != nil {
		return err
	}
	if !ok {
		return nil // anonymous struct/union/enum — cSpec's regex also requires a name
	}
	name, err := w.text(nameNode)
	if err != nil {
		return err
	}
	line, err := w.lineOf(nameNode)
	if err != nil {
		return err
	}
	w.out = append(w.out, triple.Triple{Name: name, Kind: triple.KindType, Line: line})
	return nil
}

func (w *walker) emitMacro(n sitter.Node) error {
	nameNode, ok, err := directChildOfKind(w.ctx, n, "identifier")
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	name, err := w.text(nameNode)
	if err != nil {
		return err
	}
	line, err := w.lineOf(nameNode)
	if err != nil {
		return err
	}
	w.out = append(w.out, triple.Triple{Name: name, Kind: triple.KindFunc, Line: line})
	return nil
}

func (w *walker) text(n sitter.Node) (string, error) {
	start, err := n.StartByte(w.ctx)
	if err != nil {
		return "", err
	}
	end, err := n.EndByte(w.ctx)
	if err != nil {
		return "", err
	}
	if end > uint64(len(w.src)) || start > end {
		return "", fmt.Errorf("node byte range [%d,%d) out of bounds for %d-byte source", start, end, len(w.src))
	}
	return string(w.src[start:end]), nil
}

func (w *walker) lineOf(n sitter.Node) (int, error) {
	start, err := n.StartByte(w.ctx)
	if err != nil {
		return 0, err
	}
	return w.lo.lineAt(start), nil
}

// findKind does a pre-order DFS from (and including) n looking for the
// first node whose Kind() equals target. Pre-order means an outer
// function_declarator (the one directly naming the function) is always
// found before any nested one (e.g. inside a function-pointer parameter).
func findKind(ctx context.Context, n sitter.Node, target string) (sitter.Node, bool, error) {
	kind, err := n.Kind(ctx)
	if err != nil {
		return sitter.Node{}, false, err
	}
	if kind == target {
		return n, true, nil
	}
	cnt, err := n.ChildCount(ctx)
	if err != nil {
		return sitter.Node{}, false, err
	}
	for i := uint64(0); i < cnt; i++ {
		c, err := n.Child(ctx, i)
		if err != nil {
			return sitter.Node{}, false, err
		}
		if found, ok, err := findKind(ctx, c, target); err != nil {
			return sitter.Node{}, false, err
		} else if ok {
			return found, true, nil
		}
	}
	return sitter.Node{}, false, nil
}

// directChildOfKind returns n's first direct child whose Kind() equals
// target (non-recursive — used to pull a declarator/tag identifier out of
// its immediate parent without descending into nested constructs).
func directChildOfKind(ctx context.Context, n sitter.Node, target string) (sitter.Node, bool, error) {
	cnt, err := n.ChildCount(ctx)
	if err != nil {
		return sitter.Node{}, false, err
	}
	for i := uint64(0); i < cnt; i++ {
		c, err := n.Child(ctx, i)
		if err != nil {
			return sitter.Node{}, false, err
		}
		kind, err := c.Kind(ctx)
		if err != nil {
			return sitter.Node{}, false, err
		}
		if kind == target {
			return c, true, nil
		}
	}
	return sitter.Node{}, false, nil
}

// lineOffsets maps a byte offset into a 1-based line number via binary
// search over precomputed newline positions.
type lineOffsets []int // byte offset of each '\n' in the source, ascending

func newLineOffsets(src []byte) lineOffsets {
	var lo lineOffsets
	for i, b := range src {
		if b == '\n' {
			lo = append(lo, i)
		}
	}
	return lo
}

// lineAt returns the 1-based line number containing byte offset pos.
func (lo lineOffsets) lineAt(pos uint64) int {
	// Number of newlines strictly before pos == 0-based line index.
	idx := sort.Search(len(lo), func(i int) bool { return uint64(lo[i]) >= pos })
	return idx + 1
}
