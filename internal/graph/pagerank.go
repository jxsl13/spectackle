package graph

import "sort"

// PersonalizedRank computes personalized PageRank over the subgraph within
// depth hops of the focus seeds (SPX-GRA-004). Relevance in a call graph
// flows both ways — a callee matters to its caller and vice versa — so the
// adjacency is undirected. Restart mass is split uniformly over the seeds;
// nodes outside the subgraph score 0. Deterministic: fixed iteration count
// and float sums accumulated in sorted node order (map iteration order
// would flip last bits).
func PersonalizedRank(g Graph, focus []NodeID, depth, iters int, damping float64) map[NodeID]float64 {
	if len(focus) == 0 || iters <= 0 {
		return nil
	}
	nodes, edges := g.Impact(focus, depth, Both, nil)
	if len(nodes) == 0 {
		return nil
	}
	order := make([]NodeID, 0, len(nodes))
	for _, n := range nodes {
		order = append(order, n.ID)
	}
	sort.Slice(order, func(i, j int) bool { return order[i] < order[j] })
	inSub := make(map[NodeID]bool, len(order))
	for _, id := range order {
		inSub[id] = true
	}
	adj := make(map[NodeID][]NodeID, len(order))
	for _, e := range edges {
		if !inSub[e.Src] || !inSub[e.Dst] || e.Src == e.Dst {
			continue
		}
		adj[e.Src] = append(adj[e.Src], e.Dst)
		adj[e.Dst] = append(adj[e.Dst], e.Src)
	}

	restart := make(map[NodeID]float64, len(focus))
	for _, f := range focus {
		if inSub[f] {
			restart[f] += 1 / float64(len(focus))
		}
	}
	score := make(map[NodeID]float64, len(order))
	for id, w := range restart {
		score[id] = w
	}
	for i := 0; i < iters; i++ {
		next := make(map[NodeID]float64, len(order))
		dangling := 0.0
		for _, id := range order {
			r := score[id]
			if r == 0 {
				continue
			}
			ns := adj[id]
			if len(ns) == 0 {
				dangling += r // no neighbors: mass returns via restart
				continue
			}
			share := r / float64(len(ns))
			for _, n := range ns {
				next[n] += share
			}
		}
		score = make(map[NodeID]float64, len(order))
		for _, id := range order {
			s := damping * next[id]
			if w, ok := restart[id]; ok {
				s += (1 - damping + damping*dangling) * w
			}
			if s != 0 {
				score[id] = s
			}
		}
	}
	return score
}
