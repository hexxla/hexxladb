package pathfind

import "github.com/hexxla/hexxladb/internal/lattice"

// pqItem is a node in the A* open set.
type pqItem struct {
	coord lattice.Coord
	g     float64 // cost from start
	f     float64 // g + heuristic estimate to goal
}

// priorityQueue is a min-heap. Store entries by value so each relaxation
// does not allocate its own heap object.
type priorityQueue []pqItem

func (pq *priorityQueue) push(item pqItem) {
	queue := append(*pq, item)
	i := len(queue) - 1
	for i > 0 {
		parent := (i - 1) / 2
		if !(item.f < queue[parent].f) {
			break
		}
		queue[i] = queue[parent]
		i = parent
	}
	queue[i] = item
	*pq = queue
}

func (pq *priorityQueue) pop() pqItem {
	queue := *pq
	best := queue[0]
	last := queue[len(queue)-1]
	queue = queue[:len(queue)-1]
	if len(queue) != 0 {
		i := 0
		for i < len(queue)/2 {
			child := 2*i + 1
			if child+1 < len(queue) && queue[child+1].f < queue[child].f {
				child++
			}
			if !(queue[child].f < last.f) {
				break
			}
			queue[i] = queue[child]
			i = child
		}
		queue[i] = last
	}
	*pq = queue
	return best
}
