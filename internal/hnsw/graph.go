package hnsw

import (
	"cmp"
	"context"
	"fmt"
	"math"
	"slices"
	"sort"

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
	WalkHNSWNodes(visit func(*Node)) error
	GetEmbeddingVec(p lattice.PackedCoord) ([]float32, bool, error)
}

// DefaultM is the default max bidirectional connections per layer.
const DefaultM = 16

// DefaultEfConstruction is the default build-time beam width.
const DefaultEfConstruction = 200

// Graph provides HNSW operations over a [Storage] backend.
type Graph struct {
	s     Storage
	score func(a, b []float32) float64
}

// NewGraph creates a graph handle for the given storage and similarity scorer. Higher scores rank first.
func NewGraph(s Storage, score func(a, b []float32) float64) *Graph {
	return &Graph{s: s, score: score}
}

// Insert adds or updates a node in the HNSW graph for the given coord.
// vec is the embedding vector for the coord.
func (g *Graph) Insert(coord lattice.PackedCoord, vec []float32) error {
	return g.insert(coord, vec, true)
}

// Build validates its final graph, so its intermediate inserts can skip the
// per-mutation reachability walk.
func (g *Graph) insert(coord lattice.PackedCoord, vec []float32, checkReachability bool) error {
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
		if meta.Count != 0 {
			return fmt.Errorf("%w: non-empty graph has no entry point", ErrCorruptGraph)
		}
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

	nodeLayer := layerForCoord(coord, meta.M)
	newNode := &Node{Coord: coord, MaxLayer: nodeLayer, Neighbors: make([][]lattice.PackedCoord, int(nodeLayer)+1)}

	entryVec, ok, err := g.s.GetEmbeddingVec(entryCoord)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%w: entry embedding is missing", ErrCorruptGraph)
	}

	// Phase 1: Greedy descent from top layer to nodeLayer+1.
	ep, _, err := g.greedyDescend(context.Background(), vec, entryCoord, entryVec, int(meta.MaxLayer), int(nodeLayer)+1)
	if err != nil {
		return err
	}

	// Phase 2: At insertion layer and below, search + connect.
	linked, removed, err := g.insertConnectLayers(coord, vec, ep, newNode, meta, checkReachability)
	if err != nil {
		return err
	}

	if err := g.s.PutHNSWNode(newNode); err != nil {
		return err
	}

	newEntry := nodeLayer > meta.MaxLayer
	if newEntry {
		meta.MaxLayer = nodeLayer
		if checkReachability {
			removed = append(removed, layerZeroArc{from: coord, to: entryCoord})
		}
		entryCoord = coord
		if err := g.s.PutHNSWEntry(coord); err != nil {
			return err
		}
	}
	meta.Count++
	if err := g.s.PutHNSWMeta(meta); err != nil {
		return err
	}
	if !linked && !newEntry {
		return fmt.Errorf("%w: inserted node has no incoming layer-zero link", ErrDisconnectedGraph)
	}
	// If each removed edge has a replacement path, every previous path
	// still exists. A new entry must also be able to reach the old one.
	for _, arc := range removed {
		reachable, err := g.hasShortPath(arc)
		if err != nil {
			return err
		}
		if !reachable {
			return g.validateStoredReachability(entryCoord, meta.Count)
		}
	}
	return nil
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

// layerForCoord deterministically maps a coordinate onto the HNSW exponential
// layer distribution. Persisted graph construction is therefore repeatable for
// the same insertion order without storing PRNG state.
func layerForCoord(coord lattice.PackedCoord, m uint16) uint8 {
	x := coord[0] ^ (coord[1]<<32 | coord[1]>>32)
	x += 0x9e3779b97f4a7c15
	x = (x ^ (x >> 30)) * 0xbf58476d1ce4e5b9
	x = (x ^ (x >> 27)) * 0x94d049bb133111eb
	x ^= x >> 31
	// Use the high 53 bits and add one so log never sees zero.
	u := float64((x>>11)+1) / float64((uint64(1)<<53)+1)
	mL := 1.0 / math.Log(float64(m))
	return uint8(min(int(-math.Log(u)*mL), 255)) //nolint:gosec // bounded to uint8
}

// greedyDescend performs greedy traversal from startLayer down to stopLayer (exclusive).
// Returns the best entry point and its distance to vec.
func (g *Graph) greedyDescend(ctx context.Context, vec []float32, ep lattice.PackedCoord, epVec []float32, startLayer, stopLayer int) (lattice.PackedCoord, float64, error) {
	epDist := g.score(vec, epVec)
	for lc := startLayer; lc >= stopLayer; lc-- {
		changed := true
		for changed {
			changed = false
			epNode, epOk, epErr := g.s.GetHNSWNode(ep)
			if epErr != nil {
				return ep, epDist, epErr
			}
			if !epOk {
				return ep, epDist, fmt.Errorf("%w: descent node is missing", ErrCorruptGraph)
			}
			if lc >= len(epNode.Neighbors) {
				return ep, epDist, fmt.Errorf("%w: descent node is missing layer %d", ErrCorruptGraph, lc)
			}
			for _, nbr := range epNode.Neighbors[lc] {
				if err := ctx.Err(); err != nil {
					return ep, epDist, err
				}
				nbrVec, nOk, nErr := g.s.GetEmbeddingVec(nbr)
				if nErr != nil {
					return ep, epDist, nErr
				}
				if !nOk {
					return ep, epDist, fmt.Errorf("%w: descent neighbor embedding is missing", ErrCorruptGraph)
				}
				d := g.score(vec, nbrVec)
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
func (g *Graph) insertConnectLayers(coord lattice.PackedCoord, vec []float32, ep lattice.PackedCoord, newNode *Node, meta *Meta, track bool) (linked bool, removed []layerZeroArc, err error) {
	mMax0 := meta.M * 2
	mMax := meta.M

	for lc := min(int(newNode.MaxLayer), int(meta.MaxLayer)); lc >= 0; lc-- {
		candidates, err := g.searchLayer(context.Background(), vec, ep, int(meta.EfC), lc)
		if err != nil {
			return false, nil, err
		}

		limit := mMax
		if lc == 0 {
			limit = mMax0
		}
		selected, err := g.selectNeighbors(candidates, int(limit))
		if err != nil {
			return false, nil, err
		}
		newNode.Neighbors[lc] = selected

		linked, removed, err = g.connectBidirectional(coord, selected, lc, limit, track && lc == 0)
		if err != nil {
			return false, nil, err
		}

		if len(candidates) > 0 {
			ep = candidates[0].coord
		}
	}
	return linked, removed, nil
}

// connectBidirectional adds coord to each selected neighbor's list and prunes if needed.
func (g *Graph) connectBidirectional(coord lattice.PackedCoord, selected []lattice.PackedCoord, layer int, limit uint16, track bool) (linked bool, removed []layerZeroArc, err error) {
	for _, sel := range selected {
		nbrNode, nOk, nErr := g.s.GetHNSWNode(sel)
		if nErr != nil {
			return false, nil, nErr
		}
		if !nOk {
			return false, nil, fmt.Errorf("%w: selected neighbor is missing", ErrCorruptGraph)
		}
		if len(nbrNode.Neighbors) <= layer {
			return false, nil, fmt.Errorf("%w: selected neighbor is missing layer %d", ErrCorruptGraph, layer)
		}
		nbrNode.Neighbors[layer] = append(nbrNode.Neighbors[layer], coord)
		if len(nbrNode.Neighbors[layer]) > int(limit) {
			before := nbrNode.Neighbors[layer]
			pruned, err := g.pruneNeighbors(sel, before, int(limit))
			if err != nil {
				return false, nil, err
			}
			nbrNode.Neighbors[layer] = pruned
			if track {
				for _, old := range before[:len(before)-1] {
					if !slices.Contains(pruned, old) {
						removed = append(removed, layerZeroArc{from: sel, to: old})
					}
				}
			}
		}
		linked = linked || slices.Contains(nbrNode.Neighbors[layer], coord)
		if err := g.s.PutHNSWNode(nbrNode); err != nil {
			return false, nil, err
		}
	}
	return linked, removed, nil
}

type layerZeroArc struct {
	from lattice.PackedCoord
	to   lattice.PackedCoord
}

// hasShortPath looks for a direct or two-hop replacement in the final graph.
// A negative result requires a full reachability check.
func (g *Graph) hasShortPath(arc layerZeroArc) (bool, error) {
	neighbors, err := g.neighborsAtLayer(arc.from, 0)
	if err != nil {
		return false, err
	}
	if slices.Contains(neighbors, arc.to) {
		return true, nil
	}
	for _, middle := range neighbors {
		next, err := g.neighborsAtLayer(middle, 0)
		if err != nil {
			return false, err
		}
		if slices.Contains(next, arc.to) {
			return true, nil
		}
	}
	return false, nil
}

func (g *Graph) validateStoredReachability(entry lattice.PackedCoord, count uint64) error {
	layerZero := make(map[lattice.PackedCoord][]lattice.PackedCoord)
	if err := g.s.WalkHNSWNodes(func(node *Node) {
		layerZero[node.Coord] = node.Neighbors[0]
	}); err != nil {
		return err
	}
	if uint64(len(layerZero)) != count {
		return fmt.Errorf("%w: layer-zero node count does not match metadata", ErrCorruptGraph)
	}
	return validateReachability(context.Background(), entry, len(layerZero), func(coord lattice.PackedCoord) ([]lattice.PackedCoord, bool) {
		neighbors, found := layerZero[coord]
		return neighbors, found
	})
}

// Search returns the top-K nearest neighbors to vec using HNSW graph traversal.
func (g *Graph) Search(ctx context.Context, vec []float32, k, efSearch int) ([]SearchResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if efSearch <= 0 {
		efSearch = 100
	}
	if k <= 0 {
		k = 10
	}
	efSearch = max(efSearch, k)

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
		return nil, fmt.Errorf("%w: non-empty graph has no entry point", ErrCorruptGraph)
	}

	entryVec, ok, err := g.s.GetEmbeddingVec(entryCoord)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("%w: entry embedding is missing", ErrCorruptGraph)
	}

	// Greedy descent from top layer to layer 1.
	ep, _, err := g.greedyDescend(ctx, vec, entryCoord, entryVec, int(meta.MaxLayer), 1)
	if err != nil {
		return nil, err
	}

	// Layer 0: beam search.
	candidates, err := g.searchLayer(ctx, vec, ep, efSearch, 0)
	if err != nil {
		return nil, err
	}

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
	entryCoord, hasEntry, err := g.s.GetHNSWEntry()
	if err != nil {
		return err
	}
	if !hasEntry {
		return fmt.Errorf("%w: non-empty graph has no entry point", ErrCorruptGraph)
	}
	return g.removeNode(node, meta, entryCoord)
}

// removeNode removes a node from the graph, repairs neighbors, and updates
// entry point and meta.
func (g *Graph) removeNode(node *Node, meta *Meta, entryCoord lattice.PackedCoord) error {
	coord := node.Coord

	replacement, layerZero, err := g.repairIncomingLinks(node, meta, coord == entryCoord)
	if err != nil {
		return err
	}
	if err := g.s.DeleteHNSWNode(coord); err != nil {
		return err
	}

	meta.Count--
	if meta.Count == 0 {
		meta.MaxLayer = 0
		if err := g.s.DeleteHNSWEntry(); err != nil {
			return err
		}
	} else if coord == entryCoord {
		if replacement == nil {
			return fmt.Errorf("%w: no surviving entry point", ErrCorruptGraph)
		}
		meta.MaxLayer = replacement.MaxLayer
		entryCoord = replacement.Coord
		if err := g.s.PutHNSWEntry(entryCoord); err != nil {
			return err
		}
	}
	if err := g.s.PutHNSWMeta(meta); err != nil {
		return err
	}
	if uint64(len(layerZero)) != meta.Count { //nolint:gosec // map length is non-negative.
		return fmt.Errorf("%w: layer-zero node count does not match metadata", ErrCorruptGraph)
	}
	if meta.Count == 0 {
		return nil
	}
	return validateReachability(context.Background(), entryCoord, len(layerZero), func(coord lattice.PackedCoord) ([]lattice.PackedCoord, bool) {
		neighbors, found := layerZero[coord]
		return neighbors, found
	})
}

// Neighbor pruning can make links directional. Find actual incoming links
// before removing a node, and stage rewrites only after finishing the scan.
func (g *Graph) repairIncomingLinks(removed *Node, meta *Meta, replaceEntry bool) (*Node, map[lattice.PackedCoord][]lattice.PackedCoord, error) {
	var replacement *Node
	var changed []*Node
	layerZero := make(map[lattice.PackedCoord][]lattice.PackedCoord)
	err := g.s.WalkHNSWNodes(func(node *Node) {
		if node.Coord == removed.Coord {
			return
		}
		if replaceEntry && betterEntry(node, replacement) {
			replacement = node
		}
		if g.removeIncomingLinks(node, removed, meta) {
			changed = append(changed, node)
		}
		layerZero[node.Coord] = node.Neighbors[0]
	})
	if err != nil {
		return nil, nil, err
	}
	for _, node := range changed {
		if err := g.s.PutHNSWNode(node); err != nil {
			return nil, nil, err
		}
	}
	return replacement, layerZero, nil
}

func betterEntry(node, best *Node) bool {
	return best == nil || node.MaxLayer > best.MaxLayer ||
		(node.MaxLayer == best.MaxLayer && node.Coord.Less(best.Coord))
}

func (g *Graph) removeIncomingLinks(node, removed *Node, meta *Meta) bool {
	changed := false
	for layer, links := range node.Neighbors {
		remaining := slices.DeleteFunc(links, func(coord lattice.PackedCoord) bool { return coord == removed.Coord })
		if len(remaining) == len(links) {
			continue
		}
		changed = true
		node.Neighbors[layer] = remaining
		if layer < len(removed.Neighbors) {
			g.repairWithCoNeighbors(node, layer, removed.Neighbors[layer], removed.Coord, node.Coord, g.layerLimit(meta, layer))
		}
	}
	return changed
}

// repairWithCoNeighbors adds former co-neighbors of the removed node to nbrNode
// at layer lc, up to the capacity limit.
func (g *Graph) repairWithCoNeighbors(nbrNode *Node, lc int, nbrList []lattice.PackedCoord, removedCoord, self lattice.PackedCoord, limit uint16) {
	for _, coNbr := range nbrList {
		if len(nbrNode.Neighbors[lc]) >= int(limit) {
			break
		}
		if coNbr != self && coNbr != removedCoord && !slices.Contains(nbrNode.Neighbors[lc], coNbr) {
			nbrNode.Neighbors[lc] = append(nbrNode.Neighbors[lc], coNbr)
		}
	}
}

// layerLimit returns the maximum neighbor count for the given layer.
func (g *Graph) layerLimit(meta *Meta, lc int) uint16 {
	if lc == 0 {
		return meta.M * 2
	}
	return meta.M
}

// searchLayer runs ef-bounded greedy search at the given layer starting from ep.
// Returns candidates sorted by descending similarity.
func (g *Graph) searchLayer(ctx context.Context, vec []float32, ep lattice.PackedCoord, ef, layer int) ([]scored, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	epVec, ok, err := g.s.GetEmbeddingVec(ep)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("%w: candidate embedding is missing", ErrCorruptGraph)
	}
	epDist := g.score(vec, epVec)

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

		neighbors, err := g.neighborsAtLayer(best.coord, layer)
		if err != nil {
			return nil, err
		}
		candidates, results, err = g.expandSearchNeighbors(ctx, vec, neighbors, ef, visited, candidates, results)
		if err != nil {
			return nil, err
		}
	}
	return results, ctx.Err()
}

func (g *Graph) neighborsAtLayer(coord lattice.PackedCoord, layer int) ([]lattice.PackedCoord, error) {
	node, ok, err := g.s.GetHNSWNode(coord)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("%w: candidate node is missing", ErrCorruptGraph)
	}
	if layer >= len(node.Neighbors) {
		return nil, fmt.Errorf("%w: candidate node is missing layer %d", ErrCorruptGraph, layer)
	}
	return node.Neighbors[layer], nil
}

func (g *Graph) expandSearchNeighbors(
	ctx context.Context,
	vec []float32,
	neighbors []lattice.PackedCoord,
	ef int,
	visited map[lattice.PackedCoord]bool,
	candidates, results []scored,
) ([]scored, []scored, error) {
	for _, nbr := range neighbors {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		if visited[nbr] {
			continue
		}
		visited[nbr] = true

		nbrVec, ok, err := g.s.GetEmbeddingVec(nbr)
		if err != nil {
			return nil, nil, err
		}
		if !ok {
			return nil, nil, fmt.Errorf("%w: neighbor embedding is missing", ErrCorruptGraph)
		}
		distance := g.score(vec, nbrVec)

		if len(results) < ef || distance > results[len(results)-1].dist {
			result := scored{coord: nbr, dist: distance}
			results = insertSorted(results, result)
			if len(results) > ef {
				results = results[:ef]
			}
			candidates = insertSorted(candidates, result)
		}
	}
	return candidates, results, nil
}

// selectNeighbors implements SELECT-NEIGHBORS-HEURISTIC (Algorithm 4) from
// https://arxiv.org/abs/1603.09320 with no candidate extension and with refill.
// Candidates arrive in descending similarity order. A closer selected neighbor
// occludes a candidate when the two are at least as similar as the candidate
// and the owner. Refill uses the closest occluded candidates.
func (g *Graph) selectNeighbors(candidates []scored, limit int) ([]lattice.PackedCoord, error) {
	selected := make([]lattice.PackedCoord, 0, min(limit, len(candidates)))
	if len(candidates) <= limit {
		// All candidates would be retained by the refill step.
		for _, candidate := range candidates {
			selected = append(selected, candidate.coord)
		}
		return selected, nil
	}
	vectors := make([][]float32, 0, limit)
	discarded := make([]lattice.PackedCoord, 0, limit)
	for _, candidate := range candidates {
		if len(selected) == limit {
			break
		}
		vector, ok, err := g.s.GetEmbeddingVec(candidate.coord)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("%w: selection candidate embedding is missing", ErrCorruptGraph)
		}
		if slices.ContainsFunc(vectors, func(other []float32) bool { return g.score(vector, other) >= candidate.dist }) {
			if len(discarded) < limit {
				discarded = append(discarded, candidate.coord)
			}
			continue
		}
		selected = append(selected, candidate.coord)
		vectors = append(vectors, vector)
	}
	return append(selected, discarded[:min(len(discarded), limit-len(selected))]...), nil
}

// pruneNeighbors reruns neighbor selection against the owner when the degree limit is exceeded.
func (g *Graph) pruneNeighbors(owner lattice.PackedCoord, neighbors []lattice.PackedCoord, limit int) ([]lattice.PackedCoord, error) {
	ownerVec, ok, err := g.s.GetEmbeddingVec(owner)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("%w: pruning owner embedding is missing", ErrCorruptGraph)
	}
	candidates := make([]scored, 0, len(neighbors))
	for _, nbr := range neighbors {
		nbrVec, nOk, nErr := g.s.GetEmbeddingVec(nbr)
		if nErr != nil {
			return nil, nErr
		}
		if !nOk {
			return nil, fmt.Errorf("%w: pruning neighbor embedding is missing", ErrCorruptGraph)
		}
		candidates = append(candidates, scored{coord: nbr, dist: g.score(ownerVec, nbrVec)})
	}
	slices.SortStableFunc(candidates, func(a, b scored) int { return cmp.Compare(b.dist, a.dist) })
	return g.selectNeighbors(candidates, limit)
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
