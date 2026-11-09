package hnsw

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/hexxla/hexxladb/internal/lattice"
)

// Embedding pairs a coordinate with its authoritative vector.
type Embedding struct {
	Coord  lattice.PackedCoord
	Vector []float32
}

// BuiltGraph contains validated graph records ready for publication.
// Nodes are ordered by packed coordinate. An empty graph has nil Meta.
type BuiltGraph struct {
	Meta  *Meta
	Entry lattice.PackedCoord
	Nodes []*Node
}

// Build assembles an in-memory graph, inserting embeddings in input order.
func Build(ctx context.Context, embeddings []Embedding, score func(a, b []float32) float64) (BuiltGraph, error) {
	storage := newMemoryHNSWStorage(embeddings)
	graph := NewGraph(storage, score)
	for _, embedding := range embeddings {
		if err := ctx.Err(); err != nil {
			return BuiltGraph{}, err
		}
		if err := graph.Insert(embedding.Coord, embedding.Vector); err != nil {
			return BuiltGraph{}, fmt.Errorf("build embedding graph: %w", err)
		}
	}
	if err := storage.validate(); err != nil {
		return BuiltGraph{}, fmt.Errorf("validate embedding graph: %w", err)
	}

	nodes := slices.Collect(maps.Values(storage.nodes))
	slices.SortFunc(nodes, func(a, b *Node) int { return a.Coord.Compare(b.Coord) })
	return BuiltGraph{Meta: storage.meta, Entry: storage.entry, Nodes: nodes}, nil
}

type memoryHNSWStorage struct {
	embeddings map[lattice.PackedCoord][]float32
	meta       *Meta
	entry      lattice.PackedCoord
	entryFound bool
	nodes      map[lattice.PackedCoord]*Node
}

func newMemoryHNSWStorage(candidates []Embedding) *memoryHNSWStorage {
	embeddings := make(map[lattice.PackedCoord][]float32, len(candidates))
	for _, candidate := range candidates {
		embeddings[candidate.Coord] = candidate.Vector
	}
	return &memoryHNSWStorage{
		embeddings: embeddings,
		nodes:      make(map[lattice.PackedCoord]*Node, len(candidates)),
	}
}

func (s *memoryHNSWStorage) GetHNSWMeta() (*Meta, bool, error) {
	return s.meta, s.meta != nil, nil
}

func (s *memoryHNSWStorage) PutHNSWMeta(meta *Meta) error {
	s.meta = meta
	return nil
}

func (s *memoryHNSWStorage) GetHNSWEntry() (lattice.PackedCoord, bool, error) {
	return s.entry, s.entryFound, nil
}

func (s *memoryHNSWStorage) PutHNSWEntry(entry lattice.PackedCoord) error {
	s.entry = entry
	s.entryFound = true
	return nil
}

func (s *memoryHNSWStorage) DeleteHNSWEntry() error {
	s.entry = lattice.PackedCoord{}
	s.entryFound = false
	return nil
}

func (s *memoryHNSWStorage) GetHNSWNode(coord lattice.PackedCoord) (*Node, bool, error) {
	node, found := s.nodes[coord]
	return node, found, nil
}

func (s *memoryHNSWStorage) PutHNSWNode(node *Node) error {
	s.nodes[node.Coord] = node
	return nil
}

func (s *memoryHNSWStorage) DeleteHNSWNode(coord lattice.PackedCoord) error {
	delete(s.nodes, coord)
	return nil
}

func (s *memoryHNSWStorage) WalkHNSWNodes(visit func(*Node)) error {
	for _, node := range s.nodes {
		visit(node)
	}
	return nil
}

func (s *memoryHNSWStorage) GetEmbeddingVec(coord lattice.PackedCoord) ([]float32, bool, error) {
	vector, found := s.embeddings[coord]
	return vector, found, nil
}

func (s *memoryHNSWStorage) validate() error {
	if len(s.embeddings) == 0 {
		if s.meta != nil || s.entryFound || len(s.nodes) != 0 {
			return fmt.Errorf("empty embedding set produced graph records")
		}
		return nil
	}
	if s.meta == nil || !s.entryFound {
		return fmt.Errorf("non-empty graph has no meta or entry point")
	}
	if s.meta.Count != uint64(len(s.embeddings)) { //nolint:gosec // map length is non-negative.
		return fmt.Errorf("meta count %d does not match embeddings %d", s.meta.Count, len(s.embeddings))
	}
	if len(s.nodes) != len(s.embeddings) {
		return fmt.Errorf("node count %d does not match embeddings %d", len(s.nodes), len(s.embeddings))
	}
	if _, found := s.nodes[s.entry]; !found {
		return fmt.Errorf("entry point has no node")
	}
	for coord, node := range s.nodes {
		if node == nil || node.Coord != coord || len(node.Neighbors) != int(node.MaxLayer)+1 {
			return fmt.Errorf("invalid node structure")
		}
		for layer, neighbors := range node.Neighbors {
			limit := int(s.meta.M)
			if layer == 0 {
				limit *= 2
			}
			if len(neighbors) > limit {
				return fmt.Errorf("node degree exceeds layer limit")
			}
			for _, neighbor := range neighbors {
				if _, found := s.nodes[neighbor]; !found {
					return fmt.Errorf("node references missing neighbor")
				}
			}
		}
	}
	return nil
}
