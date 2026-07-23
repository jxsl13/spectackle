// Package graph holds the cross-language symbol graph: nodes are symbols
// (functions, types, kernels, asm procedures), edges are structural or
// cross-language binding relations. The graph is the structural pillar of
// spectacle — it lets an LLM compute the impact radius of a change across
// language boundaries (Go -> C -> CUDA) without reading file contents.
package graph

import (
	"sort"
	"strings"
	"sync"
)

// NodeID is a stable short identifier, "<lang>:<qualified-name>",
// e.g. "go:saxpy.Saxpy", "cu:saxpy_kernel". See internal/ids.
type NodeID string

// Lang tags the language a node belongs to.
type Lang string

const (
	LangGo   Lang = "go"
	LangC    Lang = "c"
	LangCpp  Lang = "cpp"
	LangCuda Lang = "cu"
	LangAsm  Lang = "asm"
	LangObjC Lang = "objc"
	LangMSL  Lang = "msl"
)

// NodeKind classifies a node.
type NodeKind uint8

const (
	KUnknown NodeKind = iota
	KFunc             // function
	KMethod           // method (receiver-bound)
	KType             // type / struct / class
	KVar              // package/global variable
	KKernel           // CUDA __global__ / Metal kernel / compute entry point
	KAsmProc          // asm TEXT procedure
	KFile             // file-level node
	KDir              // directory-level node
)

var nodeKindNames = [...]string{"?", "fn", "method", "type", "var", "kernel", "asm", "file", "dir"}

func (k NodeKind) String() string {
	if int(k) < len(nodeKindNames) {
		return nodeKindNames[k]
	}
	return "?"
}

// EdgeKind classifies an edge.
type EdgeKind uint8

const (
	EUnknown EdgeKind = iota
	EDef              // container defines symbol (file -> func, type -> method)
	ECall             // same-language call
	EIncl             // #include / import
	ECgo              // Go <-> C boundary (import "C", //export)
	EAsm              // Go decl <-> asm TEXT implementation
	ELaunch           // host code launches GPU kernel (<<<>>>, pipeline creation)
	EUse              // reference that is not a call (type use, var read)
	ESpecLink         // EARS rule bound to node (from .spectacle/links.tsv)
)

var edgeKindNames = [...]string{"?", "def", "call", "incl", "cgo", "asm", "launch", "use", "link"}

func (k EdgeKind) String() string {
	if int(k) < len(edgeKindNames) {
		return edgeKindNames[k]
	}
	return "?"
}

// EdgeKindFromString is the inverse of EdgeKind.String; EUnknown for no match.
func EdgeKindFromString(s string) EdgeKind {
	for i, n := range edgeKindNames {
		if n == s {
			return EdgeKind(i)
		}
	}
	return EUnknown
}

// Direction selects edge traversal direction.
type Direction uint8

const (
	Out Direction = iota
	In
	Both
)

// Node is a symbol in the cross-language graph.
type Node struct {
	ID   NodeID
	Kind NodeKind
	Lang Lang
	File string // repo-relative path
	Line int    // 1-based definition line
	Sig  string // compact signature, e.g. "(n int,a float32)error"
	Rank float32
}

// Edge is a directed relation between two nodes. File:Line locate the edge
// site (e.g. the call or launch statement), not the definitions.
type Edge struct {
	Src, Dst NodeID
	Kind     EdgeKind
	File     string
	Line     int
}

// Graph is the queryable symbol graph. Implementations must be safe for
// concurrent readers with a single writer.
type Graph interface {
	Upsert(nodes []Node, edges []Edge)
	Node(id NodeID) (Node, bool)
	// Find returns up to k nodes whose ID or name contains q (case-insensitive),
	// filtered by kind (KUnknown = any), best matches first.
	Find(q string, k int, kind NodeKind) []Node
	// Neighbors returns edges incident to id in the given direction, filtered
	// by edge kinds (empty = all).
	Neighbors(id NodeID, dir Direction, kinds []EdgeKind) []Edge
	// Impact runs a bounded BFS from seeds and returns the reachable subgraph.
	// Each node appears exactly once, at its minimum BFS distance (SPX-GRA-002).
	Impact(seeds []NodeID, depth int, dir Direction, kinds []EdgeKind) ([]Node, []Edge)
}

// memGraph is the in-memory implementation. The persistent cache
// (internal/store) only accelerates re-indexing; queries always run here.
type memGraph struct {
	mu    sync.RWMutex
	nodes map[NodeID]Node
	out   map[NodeID][]Edge
	in    map[NodeID][]Edge
}

// NewMem returns an empty in-memory graph.
func NewMem() Graph {
	return &memGraph{
		nodes: map[NodeID]Node{},
		out:   map[NodeID][]Edge{},
		in:    map[NodeID][]Edge{},
	}
}

func (g *memGraph) Upsert(nodes []Node, edges []Edge) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, n := range nodes {
		g.nodes[n.ID] = n
	}
	for _, e := range edges {
		g.out[e.Src] = append(g.out[e.Src], e)
		g.in[e.Dst] = append(g.in[e.Dst], e)
	}
}

func (g *memGraph) Node(id NodeID) (Node, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	n, ok := g.nodes[id]
	return n, ok
}

func (g *memGraph) Find(q string, k int, kind NodeKind) []Node {
	g.mu.RLock()
	defer g.mu.RUnlock()
	q = strings.ToLower(q)
	var hits []Node
	for _, n := range g.nodes {
		if kind != KUnknown && n.Kind != kind {
			continue
		}
		if strings.Contains(strings.ToLower(string(n.ID)), q) {
			hits = append(hits, n)
		}
	}
	sort.Slice(hits, func(i, j int) bool {
		// exact suffix match ranks first, then higher rank, then stable ID order
		si := strings.HasSuffix(strings.ToLower(string(hits[i].ID)), q)
		sj := strings.HasSuffix(strings.ToLower(string(hits[j].ID)), q)
		if si != sj {
			return si
		}
		if hits[i].Rank != hits[j].Rank {
			return hits[i].Rank > hits[j].Rank
		}
		return hits[i].ID < hits[j].ID
	})
	if k > 0 && len(hits) > k {
		hits = hits[:k]
	}
	return hits
}

func (g *memGraph) Neighbors(id NodeID, dir Direction, kinds []EdgeKind) []Edge {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.neighborsLocked(id, dir, kinds)
}

func (g *memGraph) neighborsLocked(id NodeID, dir Direction, kinds []EdgeKind) []Edge {
	var es []Edge
	if dir == Out || dir == Both {
		es = append(es, g.out[id]...)
	}
	if dir == In || dir == Both {
		es = append(es, g.in[id]...)
	}
	if len(kinds) == 0 {
		return es
	}
	want := map[EdgeKind]bool{}
	for _, k := range kinds {
		want[k] = true
	}
	var filtered []Edge
	for _, e := range es {
		if want[e.Kind] {
			filtered = append(filtered, e)
		}
	}
	return filtered
}
