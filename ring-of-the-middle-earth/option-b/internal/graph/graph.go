package graph

import "container/heap"

// Edge in the game graph
type Edge struct {
	To   string
	Cost int
}

// Graph is an adjacency list representation of the Middle Earth map
type Graph struct {
	adj map[string][]Edge
}

func New() *Graph {
	return &Graph{adj: make(map[string][]Edge)}
}

func (g *Graph) AddEdge(from, to string, cost int) {
	g.adj[from] = append(g.adj[from], Edge{To: to, Cost: cost})
	g.adj[to] = append(g.adj[to], Edge{To: from, Cost: cost})
}

// Distance returns the minimum number of hops (ignoring cost) from src to dst
func (g *Graph) Distance(src, dst string) int {
	if src == dst {
		return 0
	}
	visited := map[string]bool{src: true}
	queue := []struct {
		node  string
		depth int
	}{{src, 0}}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, e := range g.adj[cur.node] {
			if e.To == dst {
				return cur.depth + 1
			}
			if !visited[e.To] {
				visited[e.To] = true
				queue = append(queue, struct {
					node  string
					depth int
				}{e.To, cur.depth + 1})
			}
		}
	}
	return 999
}

// ShortestPath returns the minimum cost path using Dijkstra
func (g *Graph) ShortestPath(src, dst string) int {
	if src == dst {
		return 0
	}
	dist := map[string]int{}
	pq := &priorityQueue{}
	heap.Push(pq, &item{node: src, cost: 0})
	for pq.Len() > 0 {
		cur := heap.Pop(pq).(*item)
		if cur.node == dst {
			return cur.cost
		}
		if d, ok := dist[cur.node]; ok && d <= cur.cost {
			continue
		}
		dist[cur.node] = cur.cost
		for _, e := range g.adj[cur.node] {
			heap.Push(pq, &item{node: e.To, cost: cur.cost + e.Cost})
		}
	}
	return 999
}

// Neighbors returns all directly adjacent nodes
func (g *Graph) Neighbors(node string) []string {
	edges := g.adj[node]
	out := make([]string, 0, len(edges))
	for _, e := range edges {
		out = append(out, e.To)
	}
	return out
}

// NodesWithinHops returns all nodes reachable within `hops` BFS steps
func (g *Graph) NodesWithinHops(src string, hops int) []string {
	visited := map[string]bool{src: true}
	queue := []struct {
		node  string
		depth int
	}{{src, 0}}
	var result []string
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if cur.depth > 0 {
			result = append(result, cur.node)
		}
		if cur.depth >= hops {
			continue
		}
		for _, e := range g.adj[cur.node] {
			if !visited[e.To] {
				visited[e.To] = true
				queue = append(queue, struct {
					node  string
					depth int
				}{e.To, cur.depth + 1})
			}
		}
	}
	return result
}

// priority queue implementation for Dijkstra
type item struct {
	node string
	cost int
	idx  int
}

type priorityQueue []*item

func (pq priorityQueue) Len() int            { return len(pq) }
func (pq priorityQueue) Less(i, j int) bool  { return pq[i].cost < pq[j].cost }
func (pq priorityQueue) Swap(i, j int)       { pq[i], pq[j] = pq[j], pq[i]; pq[i].idx = i; pq[j].idx = j }
func (pq *priorityQueue) Push(x interface{}) { it := x.(*item); it.idx = len(*pq); *pq = append(*pq, it) }
func (pq *priorityQueue) Pop() interface{}   { old := *pq; n := len(old); it := old[n-1]; *pq = old[:n-1]; return it }
