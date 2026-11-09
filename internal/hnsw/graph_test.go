package hnsw

import (
	"context"
	"errors"
	"math"
	"math/rand/v2"
	"testing"

	"github.com/hexxla/hexxladb/internal/lattice"
)

type failingEmbeddingRead struct {
	*memStorage
	target lattice.PackedCoord
	err    error
}

func (s failingEmbeddingRead) GetEmbeddingVec(coord lattice.PackedCoord) ([]float32, bool, error) {
	if coord == s.target {
		return nil, false, s.err
	}
	return s.memStorage.GetEmbeddingVec(coord)
}

func TestGraphPruningPropagatesStorageFailures(t *testing.T) {
	t.Parallel()
	storage := newMemStorage()
	owner, a, b := coord(1), coord(2), coord(3)
	for _, key := range []lattice.PackedCoord{owner, a, b} {
		storage.SetVec(key, []float32{1, 0})
	}
	readErr := errors.New("storage read failed")
	for _, target := range []lattice.PackedCoord{owner, b} {
		for _, fault := range []error{readErr, nil} {
			want := fault
			if want == nil {
				want = ErrCorruptGraph
			}
			graph := NewGraph(failingEmbeddingRead{memStorage: storage, target: target, err: fault}, testSimilarity)
			if _, err := graph.pruneNeighbors(owner, []lattice.PackedCoord{a, b}, 1); !errors.Is(err, want) {
				t.Errorf("pruning error = %v, want %v", err, want)
			}
		}
	}
}

func TestGraphSearchCancelsDuringBeam(t *testing.T) {
	t.Parallel()
	storage := newMemStorage()
	graph := NewGraph(storage, testSimilarity)
	for _, key := range []lattice.PackedCoord{coord(1), coord(2), coord(3)} {
		vector := []float32{1, 0}
		storage.SetVec(key, vector)
		if err := graph.Insert(key, vector); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	scores := 0
	graph = NewGraph(storage, func(a, b []float32) float64 {
		scores++
		if scores == 3 {
			cancel()
		}
		return testSimilarity(a, b)
	})
	if _, err := graph.Search(ctx, []float32{1, 0}, 3, 3); !errors.Is(err, context.Canceled) || scores != 3 {
		t.Fatalf("Search error = %v after %d scores", err, scores)
	}
}

func TestBuildReachesSeparatedVectorClusters(t *testing.T) {
	t.Parallel()
	const count = 512
	rng := rand.New(rand.NewPCG(1, 43))
	embeddings := make([]Embedding, count)
	for i := range embeddings {
		vector := make([]float32, 16)
		for j := range vector {
			vector[j] = float32(0.05 * rng.NormFloat64())
		}
		vector[i%len(vector)]++
		embeddings[i] = Embedding{Coord: coord(uint64(i + 1)), Vector: vector}
	}
	if graph, err := Build(t.Context(), embeddings, testSimilarity); err != nil || len(graph.Nodes) != count {
		t.Fatalf("built %d nodes, want %d: %v", len(graph.Nodes), count, err)
	}
}

// memStorage is an in-memory implementation of Storage for testing.
type memStorage struct {
	meta  *Meta
	entry *lattice.PackedCoord
	nodes map[lattice.PackedCoord]*Node
	vecs  map[lattice.PackedCoord][]float32
}

func newMemStorage() *memStorage {
	return &memStorage{
		nodes: make(map[lattice.PackedCoord]*Node),
		vecs:  make(map[lattice.PackedCoord][]float32),
	}
}

func (m *memStorage) GetHNSWMeta() (*Meta, bool, error) {
	if m.meta == nil {
		return nil, false, nil
	}
	return new(*m.meta), true, nil
}

func (m *memStorage) PutHNSWMeta(meta *Meta) error {
	m.meta = new(*meta)
	return nil
}

func (m *memStorage) GetHNSWEntry() (lattice.PackedCoord, bool, error) {
	if m.entry == nil {
		return lattice.PackedCoord{}, false, nil
	}
	return *m.entry, true, nil
}

func (m *memStorage) PutHNSWEntry(p lattice.PackedCoord) error {
	m.entry = &p
	return nil
}

func (m *memStorage) DeleteHNSWEntry() error {
	m.entry = nil
	return nil
}

func (m *memStorage) GetHNSWNode(p lattice.PackedCoord) (*Node, bool, error) {
	n, ok := m.nodes[p]
	if !ok {
		return nil, false, nil
	}
	// Deep copy.
	cp := &Node{Coord: n.Coord, MaxLayer: n.MaxLayer}
	cp.Neighbors = make([][]lattice.PackedCoord, len(n.Neighbors))
	for i, layer := range n.Neighbors {
		cp.Neighbors[i] = append([]lattice.PackedCoord(nil), layer...)
	}
	return cp, true, nil
}

func (m *memStorage) PutHNSWNode(n *Node) error {
	cp := &Node{Coord: n.Coord, MaxLayer: n.MaxLayer}
	cp.Neighbors = make([][]lattice.PackedCoord, len(n.Neighbors))
	for i, layer := range n.Neighbors {
		cp.Neighbors[i] = append([]lattice.PackedCoord(nil), layer...)
	}
	m.nodes[n.Coord] = cp
	return nil
}

func (m *memStorage) DeleteHNSWNode(p lattice.PackedCoord) error {
	delete(m.nodes, p)
	return nil
}

func (m *memStorage) WalkHNSWNodes(visit func(*Node)) error {
	for _, node := range m.nodes {
		visit(node)
	}
	return nil
}

func (m *memStorage) GetEmbeddingVec(p lattice.PackedCoord) (vec []float32, found bool, _ error) {
	vec, found = m.vecs[p]
	return vec, found, nil
}

func (m *memStorage) SetVec(p lattice.PackedCoord, v []float32) {
	m.vecs[p] = v
}

func coord(i uint64) lattice.PackedCoord { return lattice.PackedCoord{i, 0} }

func TestGraph_InsertSingle(t *testing.T) {
	t.Parallel()
	s := newMemStorage()
	g := NewGraph(s, testSimilarity)

	c := coord(1)
	s.SetVec(c, []float32{1, 0, 0})
	if err := g.Insert(c, []float32{1, 0, 0}); err != nil {
		t.Fatal(err)
	}
	meta, ok, _ := s.GetHNSWMeta()
	if !ok || meta.Count != 1 {
		t.Fatalf("meta: want count=1, got %+v", meta)
	}
	entry, ok, _ := s.GetHNSWEntry()
	if !ok || entry != c {
		t.Fatalf("entry: want %v, got %v", c, entry)
	}
}

func TestLayerForCoordIsDeterministicAndDistributed(t *testing.T) {
	t.Parallel()
	var aboveBase int
	for i := range 10_000 {
		c := coord(uint64(i + 1))
		first := layerForCoord(c, DefaultM)
		if second := layerForCoord(c, DefaultM); second != first {
			t.Fatalf("coord %d layer changed: %d then %d", i, first, second)
		}
		if first > 0 {
			aboveBase++
		}
	}
	if aboveBase < 500 || aboveBase > 750 {
		t.Fatalf("higher-layer nodes=%d, want distribution near 1/%d", aboveBase, DefaultM)
	}
}

func TestGraph_InsertMultiple_SearchRecall(t *testing.T) {
	t.Parallel()
	s := newMemStorage()
	g := NewGraph(s, testSimilarity)

	// Insert 20 vectors in 3D.
	n := 20
	for i := range n {
		c := coord(uint64(i + 1))
		// Spread vectors around: use angle.
		angle := float64(i) * 2 * math.Pi / float64(n)
		v := []float32{float32(math.Cos(angle)), float32(math.Sin(angle)), 0}
		s.SetVec(c, v)
		if err := g.Insert(c, v); err != nil {
			t.Fatalf("Insert %d: %v", i, err)
		}
	}

	meta, ok, _ := s.GetHNSWMeta()
	if !ok {
		t.Fatal("no meta")
	}
	if meta.Count != uint64(n) {
		t.Fatalf("count: want %d, got %d", n, meta.Count)
	}

	// Search for vec close to (1,0,0) — should find coord(1).
	query := []float32{1, 0, 0}
	results, err := g.Search(t.Context(), query, 5, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("no results")
	}
	// The top result should be coord(1) since its vector is exactly (1,0,0).
	if results[0].Coord != coord(1) {
		t.Fatalf("top result: want %v, got %v (score=%f)", coord(1), results[0].Coord, results[0].Score)
	}
	if math.Abs(results[0].Score-1.0) > 1e-6 {
		t.Fatalf("top score: want ~1.0, got %f", results[0].Score)
	}

	// Scores descending.
	for i := 1; i < len(results); i++ {
		if results[i].Score > results[i-1].Score+1e-9 {
			t.Fatalf("results not descending at %d: %f > %f", i, results[i].Score, results[i-1].Score)
		}
	}
}

func TestGraph_Delete(t *testing.T) {
	t.Parallel()
	s := newMemStorage()
	g := NewGraph(s, testSimilarity)

	// Insert 5 nodes.
	vecs := [][]float32{
		{1, 0, 0},
		{0, 1, 0},
		{0, 0, 1},
		{0.7, 0.7, 0},
		{0.5, 0.5, 0.5},
	}
	for i, v := range vecs {
		c := coord(uint64(i + 1))
		s.SetVec(c, v)
		if err := g.Insert(c, v); err != nil {
			t.Fatal(err)
		}
	}

	// Delete coord(1).
	if err := g.Delete(coord(1)); err != nil {
		t.Fatal(err)
	}
	meta, ok, _ := s.GetHNSWMeta()
	if !ok || meta.Count != 4 {
		t.Fatalf("after delete: want count=4, got %+v", meta)
	}
	// Node should be gone.
	_, nodeOk, _ := s.GetHNSWNode(coord(1))
	if nodeOk {
		t.Fatal("node(1) should be deleted")
	}

	// Search should still work.
	results, err := g.Search(t.Context(), []float32{1, 0, 0}, 3, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("no results after delete")
	}
	// coord(1) should not appear.
	for _, r := range results {
		if r.Coord == coord(1) {
			t.Fatal("deleted coord(1) appeared in search results")
		}
	}
}

func TestGraph_DeleteAll(t *testing.T) {
	t.Parallel()
	s := newMemStorage()
	g := NewGraph(s, testSimilarity)

	for i := range 3 {
		c := coord(uint64(i + 1))
		v := []float32{float32(i + 1), 0, 0}
		s.SetVec(c, v)
		if err := g.Insert(c, v); err != nil {
			t.Fatal(err)
		}
	}
	for i := range 3 {
		if err := g.Delete(coord(uint64(i + 1))); err != nil {
			t.Fatal(err)
		}
	}
	meta, ok, _ := s.GetHNSWMeta()
	if !ok || meta.Count != 0 {
		t.Fatalf("after delete all: want count=0, got %+v", meta)
	}
	_, entryOk, _ := s.GetHNSWEntry()
	if entryOk {
		t.Fatal("entry point should be cleared")
	}
}

func TestGraph_DeleteEntryPoint(t *testing.T) {
	t.Parallel()
	s := newMemStorage()
	g := NewGraph(s, testSimilarity)

	// Insert 3 nodes.
	for i := range 3 {
		c := coord(uint64(i + 1))
		v := []float32{float32(i + 1), 0, 0}
		s.SetVec(c, v)
		if err := g.Insert(c, v); err != nil {
			t.Fatal(err)
		}
	}
	entry, _, _ := s.GetHNSWEntry()

	// Delete the entry point.
	if err := g.Delete(entry); err != nil {
		t.Fatal(err)
	}
	newEntry, ok, _ := s.GetHNSWEntry()
	if !ok {
		t.Fatal("expected new entry point")
	}
	if newEntry == entry {
		t.Fatal("entry point should have changed")
	}
}

func TestGraph_UpdateExisting(t *testing.T) {
	t.Parallel()
	s := newMemStorage()
	g := NewGraph(s, testSimilarity)

	c := coord(1)
	s.SetVec(c, []float32{1, 0, 0})
	if err := g.Insert(c, []float32{1, 0, 0}); err != nil {
		t.Fatal(err)
	}

	// Update the same coord with a new vector.
	s.SetVec(c, []float32{0, 1, 0})
	if err := g.Insert(c, []float32{0, 1, 0}); err != nil {
		t.Fatal(err)
	}

	meta, ok, _ := s.GetHNSWMeta()
	if !ok || meta.Count != 1 {
		t.Fatalf("after update: want count=1, got %+v", meta)
	}

	// Search should find the updated vector.
	results, err := g.Search(t.Context(), []float32{0, 1, 0}, 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Coord != c {
		t.Fatalf("unexpected: %+v", results)
	}
}

func TestGraph_SearchEmpty(t *testing.T) {
	t.Parallel()
	s := newMemStorage()
	g := NewGraph(s, testSimilarity)

	results, err := g.Search(t.Context(), []float32{1, 0, 0}, 5, 50)
	if err != nil {
		t.Fatal(err)
	}
	if results != nil {
		t.Fatalf("expected nil results, got %d", len(results))
	}
}

func TestGraph_SearchRejectsIncompleteGraph(t *testing.T) {
	t.Parallel()
	entry := coord(1)
	for name, removePart := range map[string]func(*memStorage){
		"entry":  func(s *memStorage) { s.entry = nil },
		"vector": func(s *memStorage) { delete(s.vecs, entry) },
		"node":   func(s *memStorage) { delete(s.nodes, entry) },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := newMemStorage()
			s.meta = &Meta{M: DefaultM, EfC: DefaultEfConstruction, Count: 1}
			s.entry = new(entry)
			s.vecs[entry] = []float32{1, 0}
			s.nodes[entry] = &Node{Coord: entry, Neighbors: [][]lattice.PackedCoord{{}}}
			removePart(s)

			_, err := NewGraph(s, testSimilarity).Search(t.Context(), []float32{1, 0}, 1, 10)
			if !errors.Is(err, ErrCorruptGraph) {
				t.Fatalf("Search error = %v, want ErrCorruptGraph", err)
			}
		})
	}
}

// testSimilarity uses negative squared distance shifted so identical vectors score 1.
func testSimilarity(a, b []float32) float64 {
	score := 1.0
	for i, value := range a {
		delta := float64(value) - float64(b[i])
		score -= delta * delta
	}
	return score
}

func TestGraphRepairsOneWayLinks(t *testing.T) {
	t.Parallel()
	for _, replace := range []bool{false, true} {
		name := "delete"
		if replace {
			name = "replace"
		}
		t.Run(name, func(t *testing.T) {
			s := newMemStorage()
			removed, survivor := coord(1), coord(2)
			s.meta = &Meta{M: DefaultM, EfC: DefaultEfConstruction, Count: 2}
			s.entry = &removed
			s.nodes[removed] = &Node{Coord: removed, Neighbors: [][]lattice.PackedCoord{nil}}
			s.nodes[survivor] = &Node{Coord: survivor, Neighbors: [][]lattice.PackedCoord{{removed}}}
			s.SetVec(removed, []float32{1, 0})
			s.SetVec(survivor, []float32{0, 1})
			graph := NewGraph(s, testSimilarity)
			if replace {
				if err := graph.Insert(removed, []float32{1, 1}); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := graph.Delete(removed); err != nil {
					t.Fatal(err)
				}
				if s.entry == nil || *s.entry != survivor {
					t.Fatalf("entry = %v, want %v", s.entry, survivor)
				}
				if links := s.nodes[survivor].Neighbors[0]; len(links) != 0 {
					t.Fatalf("survivor still points to removed node: %v", links)
				}
			}
			hits, err := graph.Search(t.Context(), []float32{0, 1}, 1, 10)
			if err != nil || len(hits) != 1 || hits[0].Coord != survivor {
				t.Fatalf("search = %v, %v, want %v", hits, err, survivor)
			}
		})
	}
}
