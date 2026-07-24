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
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jxsl13/spectacle/internal/graph"
	"github.com/jxsl13/spectacle/internal/ids"
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

// New wires the pipeline with a set of parsers and binding resolvers.
func New(g graph.Graph, s store.Store, parsers []LanguageParser, resolvers []resolve.BindingResolver) Indexer {
	byExt := map[string]LanguageParser{}
	for _, p := range parsers {
		for _, e := range p.Extensions() {
			byExt[e] = p
		}
	}
	return &indexer{g: g, s: s, byExt: byExt, resolvers: resolvers}
}

type indexer struct {
	g         graph.Graph
	s         store.Store
	byExt     map[string]LanguageParser
	resolvers []resolve.BindingResolver
}

// ignoreDirs are never descended into (cache, VCS, build output, vendored or
// generated trees). .spectacle is server-owned spec state, not source.
var ignoreDirs = map[string]bool{
	".git": true, ".spectacle": true, "node_modules": true,
	"testdata": true, "bin": true, "vendor": true,
}

func (ix *indexer) IndexAll(ctx context.Context, root string) (Stats, error) {
	var files []string
	byLang := map[graph.Lang][]string{}
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if ignoreDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(p))
		if _, ok := ix.byExt[ext]; !ok {
			return nil
		}
		rel, relErr := filepath.Rel(root, p)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		files = append(files, rel)
		if l := LangOf(rel); l != "" {
			byLang[l] = append(byLang[l], rel)
		}
		return nil
	})
	if err != nil {
		return Stats{}, err
	}
	// deterministic order: file path, so collision suffixes (~2) are stable.
	sort.Strings(files)

	st := Stats{Files: len(files)}
	var rawNodes []graph.Node
	var rawEdges []graph.Edge
	for _, rel := range files {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		src, rdErr := os.ReadFile(abs)
		if rdErr != nil {
			st.Skipped++
			continue
		}
		p := ix.byExt[strings.ToLower(filepath.Ext(rel))]
		pr, pErr := parseCached(ix.s, p, rel, src)
		if pErr != nil {
			// a single unparseable file must not sink the whole index.
			st.Skipped++
			continue
		}
		rawNodes = append(rawNodes, pr.Nodes...)
		rawEdges = append(rawEdges, pr.Edges...)
	}

	nodes, present, remap := disambiguate(rawNodes)
	ix.g.Upsert(nodes, nil)

	// same-language edges: re-home the Src of edges whose defining file lost
	// the base ID to a collision (~N), then keep only edges landing on a real
	// node (prunes heuristic call candidates whose target was never defined).
	var edges []graph.Edge
	for _, e := range rawEdges {
		if fid, ok := remap[siteKey{e.File, e.Src}]; ok {
			e.Src = fid
		}
		if present[e.Src] && present[e.Dst] {
			edges = append(edges, e)
		}
	}

	// cross-language binding edges; materialize stub nodes for any endpoint a
	// resolver names but no parser produced (unresolved C symbols etc.).
	fs := &diskFileSet{root: root, byLang: byLang}
	for _, r := range ix.resolvers {
		re, rErr := r.Resolve(ctx, ix.g, fs)
		if rErr != nil {
			return st, rErr
		}
		for _, e := range re {
			// resolvers mint base IDs too — re-home collision-renamed sources
			if fid, ok := remap[siteKey{e.File, e.Src}]; ok {
				e.Src = fid
			}
			edges = append(edges, e)
		}
	}
	stubs := stubNodes(edges, present)
	if len(stubs) > 0 {
		ix.g.Upsert(stubs, nil)
	}
	ix.g.Upsert(nil, edges)
	st.Nodes = len(nodes) + len(stubs)
	st.Edges = len(edges)

	rankByDegree(ix.g, append(append([]graph.Node{}, nodes...), stubs...))
	return st, nil
}

func (ix *indexer) IndexPaths(ctx context.Context, paths []string) (Stats, error) {
	// M2 stub: incremental re-index (hash fast-path via store, reparse only
	// changed files, rerun the resolvers whose Langs() intersect the changed
	// set) is not implemented yet — this is a documented no-op until then;
	// callers needing fresh state run IndexAll.
	return Stats{}, nil
}

// parseCached returns the parse result for a file, using the content-hash
// store to skip re-parsing unchanged files across runs.
func parseCached(s store.Store, p LanguageParser, rel string, src []byte) (ParseResult, error) {
	// The store round-trips gob blobs in M2; M0's in-memory store keeps parses
	// only per process. Either way the parser is the source of truth, so on any
	// miss (or a nil store) we simply parse.
	_ = s
	return p.Parse(rel, src)
}

// siteKey identifies a symbol by its defining file plus base ID — the unit
// the collision remap is keyed on (edges carry the defining file as their
// site, so this re-homes a renamed definition's outgoing edges).
type siteKey struct {
	File string
	Base graph.NodeID
}

// disambiguate assigns final IDs: the first node to claim a base ID keeps it;
// a later node with the SAME id but a different definition site gets a "~N"
// suffix in file-path order (SPX-GRA-001 stability). It returns the finalized
// nodes, the set of IDs present in the graph, and the (file, base ID) -> final
// ID remap for every renamed definition.
func disambiguate(raw []graph.Node) ([]graph.Node, map[graph.NodeID]bool, map[siteKey]graph.NodeID) {
	// stable order: file, then line, then id — so suffixes never shuffle.
	sort.SliceStable(raw, func(i, j int) bool {
		if raw[i].File != raw[j].File {
			return raw[i].File < raw[j].File
		}
		if raw[i].Line != raw[j].Line {
			return raw[i].Line < raw[j].Line
		}
		return raw[i].ID < raw[j].ID
	})
	present := map[graph.NodeID]bool{}
	claimed := map[graph.NodeID]graph.Node{} // base ID -> first definition
	seenN := map[graph.NodeID]int{}
	remap := map[siteKey]graph.NodeID{}
	var out []graph.Node
	for _, n := range raw {
		if first, ok := claimed[n.ID]; ok {
			if first.File == n.File && first.Line == n.Line {
				continue // exact duplicate (e.g. re-walk), drop
			}
			seenN[n.ID]++
			final := graph.NodeID(ids.WithCollision(string(n.ID), seenN[n.ID]+1))
			remap[siteKey{n.File, n.ID}] = final
			n.ID = final
		} else {
			claimed[n.ID] = n
		}
		present[n.ID] = true
		out = append(out, n)
	}
	return out, present, remap
}

// stubNodes materializes a minimal node for every edge endpoint that no parser
// produced, so the impact radius stays complete (e.g. a C symbol referenced
// across the cgo boundary before the C parser lands). Kind/Lang are inferred
// from the ID's language tag.
func stubNodes(edges []graph.Edge, present map[graph.NodeID]bool) []graph.Node {
	made := map[graph.NodeID]bool{}
	var out []graph.Node
	add := func(id graph.NodeID) {
		if id == "" || present[id] || made[id] {
			return
		}
		made[id] = true
		lang, _, _ := ids.Parse(string(id))
		out = append(out, graph.Node{ID: id, Kind: stubKind(graph.Lang(lang)), Lang: graph.Lang(lang)})
	}
	for _, e := range edges {
		add(e.Src)
		add(e.Dst)
	}
	return out
}

func stubKind(l graph.Lang) graph.NodeKind {
	switch l {
	case graph.LangCuda, graph.LangMSL:
		return graph.KKernel
	case graph.LangAsm:
		return graph.KAsmProc
	default:
		return graph.KFunc
	}
}

// rankByDegree sets Rank to the in+out degree of each node (M1 centrality;
// personalized PageRank arrives in M3). Nodes are re-upserted with their rank.
func rankByDegree(g graph.Graph, nodes []graph.Node) {
	ranked := make([]graph.Node, 0, len(nodes))
	for _, n := range nodes {
		deg := len(g.Neighbors(n.ID, graph.Both, nil))
		n.Rank = float32(deg)
		ranked = append(ranked, n)
	}
	g.Upsert(ranked, nil)
}

// diskFileSet gives resolvers read access to the walked source tree.
type diskFileSet struct {
	root   string
	byLang map[graph.Lang][]string
}

func (fs *diskFileSet) ByLang(l graph.Lang) []string { return fs.byLang[l] }

func (fs *diskFileSet) Read(p string) ([]byte, error) {
	return os.ReadFile(filepath.Join(fs.root, filepath.FromSlash(p)))
}

// MatchIgnore reports whether a repo-relative path matches any of the config
// ignore globs ("**/" prefixes match any directory depth, including zero).
func MatchIgnore(globs []string, rel string) bool {
	for _, g := range globs {
		if matchGlob(g, rel) {
			return true
		}
	}
	return false
}

func matchGlob(g, p string) bool {
	if g == "**" {
		return true
	}
	if rest, ok := strings.CutPrefix(g, "**/"); ok {
		for {
			if ok, _ := path.Match(rest, p); ok {
				return true
			}
			i := strings.IndexByte(p, '/')
			if i < 0 {
				return false
			}
			p = p[i+1:]
		}
	}
	// a trailing "/**" matches the dir and everything under it.
	if base, ok := strings.CutSuffix(g, "/**"); ok {
		if p == base || strings.HasPrefix(p, base+"/") {
			return true
		}
	}
	ok, _ := path.Match(g, p)
	return ok
}
