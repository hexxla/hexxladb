package hnsw

import (
	"math"
	"testing"

	"github.com/hexxla/hexxladb/internal/engine"
	"github.com/hexxla/hexxladb/internal/lattice"
)

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
	cp := *m.meta
	return &cp, true, nil
}

func (m *memStorage) PutHNSWMeta(meta *Meta) error {
	cp := *meta
	m.meta = &cp
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
	g := NewGraph(s, engine.DistanceCosine)

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
	g := NewGraph(s, engine.DistanceCosine)

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
	results, err := g.Search(query, 5, 50)
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
	g := NewGraph(s, engine.DistanceCosine)

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
	results, err := g.Search([]float32{1, 0, 0}, 3, 50)
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
	g := NewGraph(s, engine.DistanceCosine)

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
	g := NewGraph(s, engine.DistanceCosine)

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
	g := NewGraph(s, engine.DistanceCosine)

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
	results, err := g.Search([]float32{0, 1, 0}, 1, 10)
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
	g := NewGraph(s, engine.DistanceCosine)

	results, err := g.Search([]float32{1, 0, 0}, 5, 50)
	if err != nil {
		t.Fatal(err)
	}
	if results != nil {
		t.Fatalf("expected nil results, got %d", len(results))
	}
}
