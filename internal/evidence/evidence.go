// Package evidence computes review evidence the author cannot fake, scoped
// to an item's declared targets (T-01KYD88KE): the B-0009 class (exported,
// declared, never consumed) and the B-0003 class (one call site diverging
// from twenty siblings). The server surfaces, the agent judges — records
// render one line each, capped, deterministic, never a veto.
package evidence

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strings"

	"github.com/jxsl13/spectackle/internal/graph"
)

// recordCap bounds every sweep's output: ten findings orient a reviewer,
// thirty numb one — the tail count preserves the never-silent property.
const recordCap = 10

// minCallSites is the divergence sweep's floor: below five call sites a
// "minority shape" is indistinguishable from normal variation (B-0003 was
// one against twenty).
const minCallSites = 5

// minorityShare marks a call-shape group divergent: at or below 20% of
// sites (1 of 5, 4 of 20) the group is the odd one out worth a look.
const minorityShare = 0.20

func capped(recs []string) []string {
	if len(recs) <= recordCap {
		return recs
	}
	n := len(recs) - recordCap
	return append(recs[:recordCap], fmt.Sprintf("e +%d more", n))
}

// Unconsumed reports exported function-kind symbols under the target paths
// with zero inbound call/use edges from outside their own file — declared
// and never consumed. Suppressions (unconsumed-ok body directives) render
// visibly instead of counting; a directive naming an unflagged symbol is
// itself flagged stale, so suppressions never outlive their reason.
func Unconsumed(g graph.Graph, targets []string, suppressed map[string]string) []string {
	underTarget := func(file string) bool {
		for _, t := range targets {
			t = strings.TrimSuffix(t, "/")
			if file == t || strings.HasPrefix(file, t+"/") {
				return true
			}
		}
		return false
	}
	var flagged []string
	seen := map[string]bool{}
	for _, n := range g.Find("", 1<<20, graph.KFunc) {
		if !underTarget(n.File) || seen[string(n.ID)] {
			continue
		}
		seen[string(n.ID)] = true
		if !exportedNode(n) {
			continue
		}
		// Test and Benchmark functions are framework-invoked — no call
		// edge ever points at them, and flagging them is the same
		// false-positive family the nodocs class already learned.
		if strings.HasSuffix(n.File, "_test.go") {
			continue
		}
		consumed := false
		for _, e := range g.Neighbors(n.ID, graph.In, []graph.EdgeKind{graph.ECall, graph.EUse}) {
			if e.File != n.File {
				consumed = true
				break
			}
		}
		if !consumed {
			flagged = append(flagged, string(n.ID))
		}
	}
	sort.Strings(flagged)
	var out []string
	flaggedSet := map[string]bool{}
	for _, id := range flagged {
		flaggedSet[id] = true
		if reason, ok := suppressed[id]; ok {
			out = append(out, "e suppressed "+id+" "+reason)
			continue
		}
		out = append(out, "e unconsumed "+id)
	}
	// stale directives: suppressions must not outlive their reason
	var stale []string
	for id := range suppressed {
		if !flaggedSet[id] {
			stale = append(stale, "e stale-suppress "+id)
		}
	}
	sort.Strings(stale)
	return capped(append(out, stale...))
}

// exportedNode approximates Go exportedness from the node ID's final
// segment: an uppercase first rune after the last dot.
func exportedNode(n graph.Node) bool {
	id := string(n.ID)
	if i := strings.LastIndex(id, "."); i >= 0 && i+1 < len(id) {
		c := id[i+1]
		return c >= 'A' && c <= 'Z'
	}
	return false
}

// DivergentCallers reports call sites whose argument shape is in a small
// minority against their siblings — the B-0003 class. The graph carries no
// argument metadata (rejected: grows every edge for one consumer), so the
// sweep re-parses only the files the inbound call edges name, bounded.
func DivergentCallers(g graph.Graph, targets []string, load func(path string) []byte) []string {
	underTarget := func(file string) bool {
		for _, t := range targets {
			t = strings.TrimSuffix(t, "/")
			if file == t || strings.HasPrefix(file, t+"/") {
				return true
			}
		}
		return false
	}
	var out []string
	files := map[string]bool{}
	type callee struct {
		id    graph.NodeID
		sites []graph.Edge
	}
	var callees []callee
	for _, n := range g.Find("", 1<<20, graph.KFunc) {
		if !underTarget(n.File) {
			continue
		}
		sites := g.Neighbors(n.ID, graph.In, []graph.EdgeKind{graph.ECall})
		if len(sites) < minCallSites {
			continue
		}
		callees = append(callees, callee{n.ID, sites})
		for _, e := range sites {
			files[e.File] = true
		}
	}
	// AST-pass file guard: the cost ceiling is enforced, never assumed
	if len(files) > 50 {
		return []string{"e truncated ast >50 files"}
	}
	shapes := map[string]map[string][]graph.Edge{} // callee -> shape -> sites
	for f := range files {
		src := load(f)
		if src == nil {
			continue
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, f, src, 0)
		if err != nil {
			continue // non-Go or unparsable: skip cleanly
		}
		ast.Inspect(file, func(x ast.Node) bool {
			call, ok := x.(*ast.CallExpr)
			if !ok {
				return true
			}
			name := calleeName(call)
			if name == "" {
				return true
			}
			line := fset.Position(call.Pos()).Line
			for _, c := range callees {
				short := string(c.id)
				if i := strings.LastIndex(short, "."); i >= 0 {
					short = short[i+1:]
				}
				if name != short {
					continue
				}
				for _, site := range c.sites {
					if site.File == f && site.Line == line {
						key := string(c.id)
						if shapes[key] == nil {
							shapes[key] = map[string][]graph.Edge{}
						}
						sh := callShape(call)
						shapes[key][sh] = append(shapes[key][sh], site)
					}
				}
			}
			return true
		})
	}
	var keys []string
	for k := range shapes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		groups := shapes[k]
		total := 0
		for _, sites := range groups {
			total += len(sites)
		}
		if total < minCallSites {
			continue
		}
		var shs []string
		for sh := range groups {
			shs = append(shs, sh)
		}
		sort.Strings(shs)
		for _, sh := range shs {
			sites := groups[sh]
			if float64(len(sites))/float64(total) > minorityShare {
				continue
			}
			var locs []string
			for i, s := range sites {
				if i >= 3 {
					break
				}
				locs = append(locs, fmt.Sprintf("%s:%d", s.File, s.Line))
			}
			out = append(out, fmt.Sprintf("e divergent %s %d/%d sites differ: %s", k, len(sites), total, strings.Join(locs, " ")))
		}
	}
	sort.Strings(out)
	return capped(out)
}

func calleeName(call *ast.CallExpr) string {
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		return fn.Name
	case *ast.SelectorExpr:
		return fn.Sel.Name
	}
	return ""
}

// callShape classifies a call's arguments by count and per-argument kind —
// literal, identifier, call result, selector — the coarse shape classes
// whose minority groups B-0003 exemplified.
func callShape(call *ast.CallExpr) string {
	parts := make([]string, 0, len(call.Args))
	for _, a := range call.Args {
		switch a.(type) {
		case *ast.BasicLit:
			parts = append(parts, "lit")
		case *ast.Ident:
			parts = append(parts, "ident")
		case *ast.CallExpr:
			parts = append(parts, "call")
		case *ast.SelectorExpr:
			parts = append(parts, "sel")
		default:
			parts = append(parts, "expr")
		}
	}
	return fmt.Sprintf("%d:%s", len(call.Args), strings.Join(parts, ","))
}
