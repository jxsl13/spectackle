// Package index defines the indexing pipeline: walk -> hash -> parse (per
// language) -> resolve cross-language bindings -> populate the graph.
//
// Parser backends are pluggable behind LanguageParser. The target picture is
// cgo-free: Go is parsed with the stdlib go/parser (never tree-sitter), Plan 9
// asm with a custom scanner (this package), and C/C++/CUDA start on cgo
// tree-sitter bindings in M1/M2 with a planned migration to tree-sitter
// grammars compiled to WASM executed via wazero (pure Go). See
// docs/architecture.md §2 and docs/roadmap.md.
package index

import (
	"context"

	"github.com/jxsl13/spectacle/internal/graph"
	"github.com/jxsl13/spectacle/internal/resolve"
	"github.com/jxsl13/spectacle/internal/store"
)

// ParseResult is the per-file output of a LanguageParser, cacheable by
// content hash.
type ParseResult struct {
	Nodes []graph.Node
	Edges []graph.Edge
	Hash  [32]byte
}

// LanguageParser turns one source file into nodes and same-language edges.
// Implementations must be deterministic: identical bytes yield identical
// results (SPX-GRA-001 depends on it).
type LanguageParser interface {
	Lang() graph.Lang
	Extensions() []string // e.g. [".cu", ".cuh"]
	Parse(path string, src []byte) (ParseResult, error)
}

// Stats summarizes one indexing run.
type Stats struct {
	Files, Nodes, Edges, Skipped int
}

// Indexer builds and refreshes the graph.
type Indexer interface {
	// IndexAll walks root, parses every recognized file (cache-accelerated)
	// and runs all binding resolvers.
	IndexAll(ctx context.Context, root string) (Stats, error)
	// IndexPaths re-indexes only the given files and re-runs the resolvers
	// whose languages intersect the changed set.
	IndexPaths(ctx context.Context, paths []string) (Stats, error)
}

// New wires the pipeline. M0 ships the wiring but no tree-sitter parsers;
// IndexAll indexes nothing until M1 registers real parsers.
func New(g graph.Graph, s store.Store, parsers []LanguageParser, resolvers []resolve.BindingResolver) Indexer {
	return &indexer{g: g, s: s, parsers: parsers, resolvers: resolvers}
}

type indexer struct {
	g         graph.Graph
	s         store.Store
	parsers   []LanguageParser
	resolvers []resolve.BindingResolver
}

func (ix *indexer) IndexAll(ctx context.Context, root string) (Stats, error) {
	// M1: walk root honoring .spectacle/config.yaml ignore globs, dispatch
	// files to parsers by extension (parallel workers, single graph writer),
	// then run resolvers over the fresh graph.
	return Stats{}, nil
}

func (ix *indexer) IndexPaths(ctx context.Context, paths []string) (Stats, error) {
	// M2: hash fast-path via store, reparse only changed files, rerun the
	// resolvers whose Langs() intersect the changed languages.
	return Stats{}, nil
}
