package hnsw

import (
	"math"
	"math/rand/v2"
	"slices"
	"sort"

	"github.com/hexxla/hexxladb/internal/engine"
	"github.com/hexxla/hexxladb/internal/lattice"
)

// Storage is the interface the HNSW graph uses to persist nodes/meta/entry.
// Implemented by the hexxladb Tx layer.
type Storage interface {
	GetHNSWMeta() (*Meta, bool, error)
	PutHNSWMeta(m *Meta) error
	GetHNSWEntry() (lattice.PackedCoord, bool, error)
	PutHNSWEntry(p lattice.PackedCoord) error
	DeleteHNSWEntry() error
	GetHNSWNode(p lattice.PackedCoord) (*Node, bool, error)
	PutHNSWNode(n *Node) error
	DeleteHNSWNode(p lattice.PackedCoord) error
	GetEmbeddingVec(p lattice.PackedCoord) ([]float32, bool, error)
}

// DefaultM is the default max bidirectional connections per layer.
const DefaultM = 16

// DefaultEfConstruction is the default build-time beam width.
const DefaultEfConstruction = 200

// Graph provides HNSW operations over a [Storage] backend.
type Graph struct {
	s      Storage
	metric engine.DistanceMetric
}

// NewGraph creates a graph handle for the given storage and metric.
func NewGraph(s Storage, metric engine.DistanceMetric) *Graph {
	return &Graph{s: s, metric: metric}
}

// Insert adds or updates a node in the HNSW graph for the given coord.
// vec is the embedding vector for the coord.
func (g *Graph) Insert(coord lattice.PackedCoord, vec []float32) error {
	meta, hasMeta, err := g.s.GetHNSWMeta()
	if err != nil {
		return err
	}
	if !hasMeta {
		return g.initSingleNodeGraph(coord, &Meta{M: DefaultM, EfC: DefaultEfConstruction, MaxLayer: 0, Count: 1})
	}

	entryCoord, hasEntry, err := g.s.GetHNSWEntry()
	if err != nil {
		return err
	}
	if !hasEntry {
		meta.Count = 1
		meta.MaxLayer = 0
		return g.initSingleNodeGraph(coord, meta)
	}

	// Handle update case: remove existing node first.
	entryCoord, meta, err = g.handleExistingNode(coord, entryCoord, meta)
	if err != nil {
		return err
	}
	if meta == nil {
		// Graph became empty after removing existing; re-initialize.
		return g.initSingleNodeGraph(coord, &Meta{M: DefaultM, EfC: DefaultEfConstruction, MaxLayer: 0, Count: 1})
	}

	nodeLayer := g.randomLayer(meta.M)
	newNode := &Node{Coord: coord, MaxLayer: nodeLayer, Neighbors: make([][]lattice.PackedCoord, int(nodeLayer)+1)}

	entryVec, ok, err := g.s.GetEmbeddingVec(entryCoord)
	if err != nil {
		return err
	}
	if !ok {
		// Entry point embedding missing — corrupted state; just store node.
		if err := g.s.PutHNSWNode(newNode); err != nil {
			return err
		}
		meta.Count++
		return g.s.PutHNSWMeta(meta)
	}

	// Phase 1: Greedy descent from top layer to nodeLayer+1.
	ep, _, err := g.greedyDescend(vec, entryCoord, entryVec, int(meta.MaxLayer), int(nodeLayer)+1)
	if err != nil {
		return err
	}

	// Phase 2: At insertion layer and below, search + connect.
	if err := g.insertConnectLayers(coord, vec, ep, newNode, meta); err != nil {
		return err
	}

	if err := g.s.PutHNSWNode(newNode); err != nil {
		return err
	}

	if nodeLayer > meta.MaxLayer {
		meta.MaxLayer = nodeLayer
		if err := g.s.PutHNSWEntry(coord); err != nil {
			return err
		}
	}
	meta.Count++
	return g.s.PutHNSWMeta(meta)
}

// initSingleNodeGraph stores a single node as the entry point and persists meta.
func (g *Graph) initSingleNodeGraph(coord lattice.PackedCoord, meta *Meta) error {
	node := &Node{Coord: coord, MaxLayer: 0, Neighbors: [][]lattice.PackedCoord{{}}}
	if err := g.s.PutHNSWNode(node); err != nil {
		return err
	}
	if err := g.s.PutHNSWEntry(coord); err != nil {
		return err
	}
	return g.s.PutHNSWMeta(meta)
}

// handleExistingNode removes an existing node at coord if present.
// Returns the (possibly updated) entry coord and meta. If the graph becomes empty,
// meta is returned as nil.
func (g *Graph) handleExistingNode(coord, entryCoord lattice.PackedCoord, meta *Meta) (lattice.PackedCoord, *Meta, error) {
	existing, ok, err := g.s.GetHNSWNode(coord)
	if err != nil {
		return entryCoord, meta, err
	}
	if !ok {
		return entryCoord, meta, nil
	}
	if err := g.removeNode(existing, meta, entryCoord); err != nil {
		return entryCoord, nil, err
	}
	if entryCoord == coord {
		entryCoord, ok, err = g.s.GetHNSWEntry()
		if err != nil {
			return entryCoord, nil, err
		}
		if !ok {
			return entryCoord, nil, nil // graph empty
		}
	}
	meta, _, err = g.s.GetHNSWMeta()
	if err != nil {
		return entryCoord, nil, err
	}
	return entryCoord, meta, nil
}

// randomLayer generates a random layer for a new node using the HNSW distribution.
func (g *Graph) randomLayer(m uint16) uint8 {
	mL := 1.0 / math.Log(float64(m))
	return uint8(min(int(-math.Log(rand.Float64())*mL), 255)) //nolint:gosec // G115
}

// greedyDescend performs greedy traversal from startLayer down to stopLayer (exclusive).
// Returns the best entry point and its distance to vec.
func (g *Graph) greedyDescend(vec []float32, ep lattice.PackedCoord, epVec []float32, startLayer, stopLayer int) (lattice.PackedCoord, float64, error) {
	epDist := engine.Similarity(vec, epVec, g.metric)
	for lc := startLayer; lc >= stopLayer; lc-- {
		changed := true
		for changed {
			changed = false
			epNode, epOk, epErr := g.s.GetHNSWNode(ep)
			if epErr != nil {
				return ep, epDist, epErr
			}
			if !epOk || lc >= len(epNode.Neighbors) {
				break
			}
			for _, nbr := range epNode.Neighbors[lc] {
				nbrVec, nOk, nErr := g.s.GetEmbeddingVec(nbr)
				if nErr != nil {
					return ep, epDist, nErr
				}
				if !nOk {
					continue
				}
				d := engine.Similarity(vec, nbrVec, g.metric)
				if d > epDist {
					ep = nbr
					epDist = d
					changed = true
				}
			}
		}
	}
	return ep, epDist, nil
}

// insertConnectLayers handles Phase 2 of HNSW insertion: search + bidirectional connect
// at each layer from min(nodeLayer, maxLayer) down to 0.
func (g *Graph) insertConnectLayers(coord lattice.PackedCoord, vec []float32, ep lattice.PackedCoord, newNode *Node, meta *Meta) error {
	mMax0 := meta.M * 2
	mMax := meta.M

	for lc := min(int(newNode.MaxLayer), int(meta.MaxLayer)); lc >= 0; lc-- {
		candidates := g.searchLayer(vec, ep, int(meta.EfC), lc)

		limit := mMax
		if lc == 0 {
			limit = mMax0
		}
		selected := g.selectNeighbors(candidates, int(limit))

		nbrCoords := make([]lattice.PackedCoord, len(selected))
		for i, s := range selected {
			nbrCoords[i] = s.coord
		}
		newNode.Neighbors[lc] = nbrCoords

		if err := g.connectBidirectional(coord, selected, lc, limit); err != nil {
			return err
		}

		if len(candidates) > 0 {
			ep = candidates[0].coord
		}
	}
	return nil
}

// connectBidirectional adds coord to each selected neighbor's list and prunes if needed.
func (g *Graph) connectBidirectional(coord lattice.PackedCoord, selected []scored, layer int, limit uint16) error {
	for _, sel := range selected {
		nbrNode, nOk, nErr := g.s.GetHNSWNode(sel.coord)
		if nErr != nil {
			return nErr
		}
		if !nOk {
			continue
		}
		for len(nbrNode.Neighbors) <= layer {
			nbrNode.Neighbors = append(nbrNode.Neighbors, nil)
		}
		nbrNode.Neighbors[layer] = append(nbrNode.Neighbors[layer], coord)
		if uint16(len(nbrNode.Neighbors[layer])) > limit { //nolint:gosec // bounded by uint16
			nbrNode.Neighbors[layer] = g.pruneNeighbors(sel.coord, nbrNode.Neighbors[layer], int(limit), layer)
		}
		if err := g.s.PutHNSWNode(nbrNode); err != nil {
			return err
		}
	}
	return nil
}

// Search returns the top-K nearest neighbors to vec using HNSW graph traversal.
func (g *Graph) Search(vec []float32, k, efSearch int) ([]SearchResult, error) {
	if efSearch <= 0 {
		efSearch = 100
	}
	if k <= 0 {
		k = 10
	}
	if efSearch < k {
		efSearch = k
	}

	meta, hasMeta, err := g.s.GetHNSWMeta()
	if err != nil {
		return nil, err
	}
	if !hasMeta || meta.Count == 0 {
		return nil, nil
	}
	entryCoord, hasEntry, err := g.s.GetHNSWEntry()
	if err != nil {
		return nil, err
	}
	if !hasEntry {
		return nil, nil
	}

	entryVec, ok, err := g.s.GetEmbeddingVec(entryCoord)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}

	// Greedy descent from top layer to layer 1.
	ep, _, err := g.greedyDescend(vec, entryCoord, entryVec, int(meta.MaxLayer), 1)
	if err != nil {
		return nil, err
	}

	// Layer 0: beam search.
	candidates := g.searchLayer(vec, ep, efSearch, 0)

	if len(candidates) > k {
		candidates = candidates[:k]
	}
	results := make([]SearchResult, len(candidates))
	for i, c := range candidates {
		results[i] = SearchResult{Coord: c.coord, Score: c.dist}
	}
	return results, nil
}

// SearchResult is a single HNSW search result.
type SearchResult struct {
	Coord lattice.PackedCoord
	Score float64
}

// Delete removes a node from the HNSW graph.
func (g *Graph) Delete(coord lattice.PackedCoord) error {
	meta, hasMeta, err := g.s.GetHNSWMeta()
	if err != nil {
		return err
	}
	if !hasMeta || meta.Count == 0 {
		return nil
	}
	node, ok, err := g.s.GetHNSWNode(coord)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	entryCoord, _, err := g.s.GetHNSWEntry()
	if err != nil {
		return err
	}
	return g.removeNode(node, meta, entryCoord)
}

// removeNode removes a node from the graph, repairs neighbors, and updates
// entry point and meta.
func (g *Graph) removeNode(node *Node, meta *Meta, entryCoord lattice.PackedCoord) error {
	coord := node.Coord

	// Remove coord from all neighbors' lists.
	for lc, nbrList := range node.Neighbors {
		for _, nbr := range nbrList {
			nbrNode, nOk, nErr := g.s.GetHNSWNode(nbr)
			if nErr != nil {
				return nErr
			}
			if !nOk || lc >= len(nbrNode.Neighbors) {
				continue
			}
			nbrNode.Neighbors[lc] = removeCoord(nbrNode.Neighbors[lc], coord)

			// Repair: try to connect this neighbor to former co-neighbors.
			for _, coNbr := range nbrList {
				if coNbr == nbr || coNbr == coord {
					continue
				}
				if containsCoord(nbrNode.Neighbors[lc], coNbr) {
					continue
				}
				var limit uint16
				if lc == 0 {
					limit = meta.M * 2
				} else {
					limit = meta.M
				}
				if uint16(len(nbrNode.Neighbors[lc])) < limit { //nolint:gosec // bounded
					nbrNode.Neighbors[lc] = append(nbrNode.Neighbors[lc], coNbr)
				}
			}
			if err := g.s.PutHNSWNode(nbrNode); err != nil {
				return err
			}
		}
	}

	// Delete the node itself.
	if err := g.s.DeleteHNSWNode(coord); err != nil {
		return err
	}

	meta.Count--
	if meta.Count == 0 {
		// Graph is now empty.
		meta.MaxLayer = 0
		if err := g.s.DeleteHNSWEntry(); err != nil {
			return err
		}
		return g.s.PutHNSWMeta(meta)
	}

	// If we deleted the entry point, promote a neighbor.
	if coord == entryCoord {
		// Pick the first neighbor at the highest layer.
		var newEntry lattice.PackedCoord
		found := false
		for lc := int(node.MaxLayer); lc >= 0 && !found; lc-- {
			for _, nbr := range node.Neighbors[lc] {
				newEntry = nbr
				found = true
				break
			}
		}
		if !found {
			// Shouldn't happen with count > 0; fall back to first existing node's layer.
			meta.MaxLayer = 0
		} else {
			newEntryNode, nOk, nErr := g.s.GetHNSWNode(newEntry)
			if nErr != nil {
				return nErr
			}
			if nOk {
				meta.MaxLayer = newEntryNode.MaxLayer
			} else {
				meta.MaxLayer = 0
			}
			if err := g.s.PutHNSWEntry(newEntry); err != nil {
				return err
			}
		}
	}

	return g.s.PutHNSWMeta(meta)
}

// searchLayer runs ef-bounded greedy search at the given layer starting from ep.
// Returns candidates sorted by descending similarity.
func (g *Graph) searchLayer(vec []float32, ep lattice.PackedCoord, ef, layer int) []scored {
	epVec, ok, err := g.s.GetEmbeddingVec(ep)
	if err != nil || !ok {
		return nil
	}
	epDist := engine.Similarity(vec, epVec, g.metric)

	visited := map[lattice.PackedCoord]bool{ep: true}
	candidates := []scored{{coord: ep, dist: epDist}} // sorted descending
	results := []scored{{coord: ep, dist: epDist}}    // sorted descending

	for len(candidates) > 0 {
		// Pop best candidate (highest similarity).
		best := candidates[0]
		candidates = candidates[1:]

		// Worst in results.
		worst := results[len(results)-1]
		if best.dist < worst.dist && len(results) >= ef {
			break
		}

		bestNode, bOk, bErr := g.s.GetHNSWNode(best.coord)
		if bErr != nil || !bOk {
			continue
		}
		if layer >= len(bestNode.Neighbors) {
			continue
		}
		for _, nbr := range bestNode.Neighbors[layer] {
			if visited[nbr] {
				continue
			}
			visited[nbr] = true

			nbrVec, nOk, nErr := g.s.GetEmbeddingVec(nbr)
			if nErr != nil || !nOk {
				continue
			}
			d := engine.Similarity(vec, nbrVec, g.metric)

			if len(results) < ef || d > results[len(results)-1].dist {
				// Insert into results maintaining descending order.
				results = insertSorted(results, scored{coord: nbr, dist: d})
				if len(results) > ef {
					results = results[:ef]
				}
				// Also add to candidates.
				candidates = insertSorted(candidates, scored{coord: nbr, dist: d})
			}
		}
	}
	return results
}

// selectNeighbors implements the diversity heuristic: prefer neighbors that
// are not too close to each other.
func (g *Graph) selectNeighbors(candidates []scored, limit int) []scored {
	if len(candidates) <= limit {
		return candidates
	}
	// Simple approach: return top-limit by score.
	// TODO(future): implement full diversity heuristic from Malkov & Yashunin.
	return candidates[:limit]
}

// pruneNeighbors reduces a neighbor list to the limit best by similarity to the owner.
func (g *Graph) pruneNeighbors(owner lattice.PackedCoord, neighbors []lattice.PackedCoord, limit, _ int) []lattice.PackedCoord {
	ownerVec, ok, err := g.s.GetEmbeddingVec(owner)
	if err != nil || !ok {
		// Can't score — just truncate.
		if len(neighbors) > limit {
			return neighbors[:limit]
		}
		return neighbors
	}
	type nbrScore struct {
		coord lattice.PackedCoord
		score float64
	}
	scored := make([]nbrScore, 0, len(neighbors))
	for _, nbr := range neighbors {
		nbrVec, nOk, nErr := g.s.GetEmbeddingVec(nbr)
		if nErr != nil || !nOk {
			continue
		}
		scored = append(scored, nbrScore{coord: nbr, score: engine.Similarity(ownerVec, nbrVec, g.metric)})
	}
	sort.Slice(scored, func(i, j int) bool { return scored[i].score > scored[j].score })
	if len(scored) > limit {
		scored = scored[:limit]
	}
	result := make([]lattice.PackedCoord, len(scored))
	for i, s := range scored {
		result[i] = s.coord
	}
	return result
}

type scored struct {
	coord lattice.PackedCoord
	dist  float64
}

// insertSorted inserts s into a descending-sorted slice.
func insertSorted(a []scored, s scored) []scored {
	i := sort.Search(len(a), func(j int) bool { return a[j].dist < s.dist })
	a = append(a, scored{})
	copy(a[i+1:], a[i:])
	a[i] = s
	return a
}

func removeCoord(list []lattice.PackedCoord, c lattice.PackedCoord) []lattice.PackedCoord {
	for i, v := range list {
		if v == c {
			return append(list[:i], list[i+1:]...)
		}
	}
	return list
}

func containsCoord(list []lattice.PackedCoord, c lattice.PackedCoord) bool {
	return slices.Contains(list, c)
}
