package graph

// Impact runs a breadth-first traversal from seeds, bounded by depth, and
// returns the reachable subgraph in BFS order. Direction and edge-kind
// filters shape the radius: e.g. dir=Out, kinds=[ECgo, ELaunch, ECall]
// answers "what does a change to these symbols propagate into, across
// language boundaries".
func (g *memGraph) Impact(seeds []NodeID, depth int, dir Direction, kinds []EdgeKind) ([]Node, []Edge) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	type qitem struct {
		id   NodeID
		dist int
	}
	seen := map[NodeID]bool{}
	seenEdge := map[Edge]bool{}
	var nodes []Node
	var edges []Edge
	queue := make([]qitem, 0, len(seeds))
	for _, s := range seeds {
		if n, ok := g.nodes[s]; ok && !seen[s] {
			seen[s] = true
			nodes = append(nodes, n)
			queue = append(queue, qitem{s, 0})
		}
	}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if cur.dist >= depth {
			continue
		}
		for _, e := range g.neighborsLocked(cur.id, dir, kinds) {
			if !seenEdge[e] {
				seenEdge[e] = true
				edges = append(edges, e)
			}
			next := e.Dst
			if e.Dst == cur.id { // incoming edge during In/Both traversal
				next = e.Src
			}
			if seen[next] {
				continue
			}
			if n, ok := g.nodes[next]; ok {
				seen[next] = true
				nodes = append(nodes, n)
				queue = append(queue, qitem{next, cur.dist + 1})
			}
		}
	}
	return nodes, edges
}
