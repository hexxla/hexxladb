package hnsw

import (
	"testing"

	"github.com/hexxla/hexxladb/internal/lattice"
)

func mustPackNodeTest(t *testing.T, q, r int) lattice.PackedCoord {
	t.Helper()
	coord, err := lattice.Pack(lattice.Coord{Q: q, R: r})
	if err != nil {
		t.Fatal(err)
	}
	return coord
}

func TestNode_EncodeDecodeRoundTrip(t *testing.T) {
	t.Parallel()
	coord, err := lattice.Pack(lattice.Coord{Q: 42, R: 99})
	if err != nil {
		t.Fatal(err)
	}
	n := &Node{
		Coord:    coord,
		MaxLayer: 2,
		Neighbors: [][]lattice.PackedCoord{
			{mustPackNodeTest(t, 1, 0), mustPackNodeTest(t, 2, 0), mustPackNodeTest(t, 3, 0)},
			{mustPackNodeTest(t, 10, 0), mustPackNodeTest(t, 20, 0)},
			{mustPackNodeTest(t, 100, 0)},
		},
	}
	data := EncodeNode(n)
	got, err := DecodeNode(coord, data)
	if err != nil {
		t.Fatalf("DecodeNode: %v", err)
	}
	if got.Coord != coord {
		t.Fatalf("coord: want %v, got %v", coord, got.Coord)
	}
	if got.MaxLayer != n.MaxLayer {
		t.Fatalf("maxLayer: want %d, got %d", n.MaxLayer, got.MaxLayer)
	}
	if len(got.Neighbors) != len(n.Neighbors) {
		t.Fatalf("layers: want %d, got %d", len(n.Neighbors), len(got.Neighbors))
	}
	for i := range n.Neighbors {
		if len(got.Neighbors[i]) != len(n.Neighbors[i]) {
			t.Fatalf("layer %d: want %d neighbors, got %d", i, len(n.Neighbors[i]), len(got.Neighbors[i]))
		}
		for j := range n.Neighbors[i] {
			if got.Neighbors[i][j] != n.Neighbors[i][j] {
				t.Fatalf("layer %d neighbor %d: want %v, got %v", i, j, n.Neighbors[i][j], got.Neighbors[i][j])
			}
		}
	}
}

func TestNode_EncodeDecodeEmpty(t *testing.T) {
	t.Parallel()
	coord, err := lattice.Pack(lattice.Coord{Q: 1, R: 2})
	if err != nil {
		t.Fatal(err)
	}
	n := &Node{
		Coord:     coord,
		MaxLayer:  0,
		Neighbors: [][]lattice.PackedCoord{{}}, // layer 0, no neighbors
	}
	data := EncodeNode(n)
	got, err := DecodeNode(coord, data)
	if err != nil {
		t.Fatalf("DecodeNode: %v", err)
	}
	if got.MaxLayer != 0 {
		t.Fatalf("maxLayer: want 0, got %d", got.MaxLayer)
	}
	if len(got.Neighbors[0]) != 0 {
		t.Fatalf("layer 0: want 0 neighbors, got %d", len(got.Neighbors[0]))
	}
}

func TestNode_DecodeTruncated(t *testing.T) {
	t.Parallel()
	if _, err := DecodeNode(lattice.PackedCoord{}, nil); err == nil {
		t.Fatal("expected error for nil data")
	}
	if _, err := DecodeNode(lattice.PackedCoord{}, []byte{2}); err == nil {
		t.Fatal("expected error for truncated layer count")
	}
	// MaxLayer=0, count=1 but no coord bytes.
	if _, err := DecodeNode(lattice.PackedCoord{}, []byte{0, 0, 1}); err == nil {
		t.Fatal("expected error for truncated neighbor data")
	}
}

func TestNode_DecodeTrailingBytes(t *testing.T) {
	t.Parallel()
	coord, err := lattice.Pack(lattice.Coord{Q: 1, R: 2})
	if err != nil {
		t.Fatal(err)
	}
	n := &Node{Coord: coord, MaxLayer: 0, Neighbors: [][]lattice.PackedCoord{{}}}
	data := EncodeNode(n)
	data = append(data, 0xFF) // trailing junk
	if _, err := DecodeNode(coord, data); err == nil {
		t.Fatal("expected error for trailing bytes")
	}
}

func TestMeta_EncodeDecodeRoundTrip(t *testing.T) {
	t.Parallel()
	m := &Meta{M: 16, EfC: 200, MaxLayer: 5, Count: 12345}
	data := EncodeMeta(m)
	got, err := DecodeMeta(data)
	if err != nil {
		t.Fatalf("DecodeMeta: %v", err)
	}
	if got.M != m.M || got.EfC != m.EfC || got.MaxLayer != m.MaxLayer || got.Count != m.Count {
		t.Fatalf("mismatch: want %+v, got %+v", m, got)
	}
}

func TestMeta_DecodeTooShort(t *testing.T) {
	t.Parallel()
	if _, err := DecodeMeta(make([]byte, 5)); err == nil {
		t.Fatal("expected error for short meta data")
	}
}

func TestMeta_DecodeRejectsTrailingBytesAndInvalidSearchParameters(t *testing.T) {
	t.Parallel()
	valid := EncodeMeta(&Meta{M: 16, EfC: 200, Count: 1})
	for name, data := range map[string][]byte{
		"trailing":             append(valid, 0),
		"m-too-small":          EncodeMeta(&Meta{M: 1, EfC: 200, Count: 1}),
		"zero-ef-construction": EncodeMeta(&Meta{M: 16, EfC: 0, Count: 1}),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := DecodeMeta(data); err == nil {
				t.Fatal("DecodeMeta accepted corrupt metadata")
			}
		})
	}
}

func BenchmarkEncodeNode_M16_L2(b *testing.B) {
	// Typical node: M=16 neighbors at layer 0, 8 at layer 1, 4 at layer 2.
	n := &Node{
		Coord:    lattice.PackedCoord{42, 99},
		MaxLayer: 2,
		Neighbors: [][]lattice.PackedCoord{
			make([]lattice.PackedCoord, 32), // M_max0 = 2*M
			make([]lattice.PackedCoord, 16),
			make([]lattice.PackedCoord, 4),
		},
	}
	for b.Loop() {
		EncodeNode(n)
	}
}

func BenchmarkDecodeNode_M16_L2(b *testing.B) {
	coord, err := lattice.Pack(lattice.Coord{Q: 42, R: 99})
	if err != nil {
		b.Fatal(err)
	}
	n := &Node{
		Coord:    coord,
		MaxLayer: 2,
		Neighbors: [][]lattice.PackedCoord{
			make([]lattice.PackedCoord, 32),
			make([]lattice.PackedCoord, 16),
			make([]lattice.PackedCoord, 4),
		},
	}
	data := EncodeNode(n)
	coord = n.Coord
	for b.Loop() {
		_, _ = DecodeNode(coord, data)
	}
}
