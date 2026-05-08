package pathfind

import "github.com/hexxla/hexxladb/internal/lattice"

// pqItem is a node in the A* open set priority queue.
type pqItem struct {
	coord lattice.Coord
	g     float64 // cost from start
	f     float64 // g + heuristic estimate to goal
	index int     // heap index (managed by container/heap)
}

// priorityQueue implements heap.Interface for A* open set.
type priorityQueue []*pqItem

func (pq priorityQueue) Len() int           { return len(pq) }
func (pq priorityQueue) Less(i, j int) bool { return pq[i].f < pq[j].f }
func (pq priorityQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
	pq[i].index = i
	pq[j].index = j
}

func (pq *priorityQueue) Push(x any) {
	item := x.(*pqItem)
	item.index = len(*pq)
	*pq = append(*pq, item)
}

func (pq *priorityQueue) Pop() any {
	old := *pq
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	item.index = -1
	*pq = old[:n-1]
	return item
}
