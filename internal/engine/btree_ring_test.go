package engine_test

import (
	"bytes"
	"path/filepath"
	"slices"
	"testing"

	"github.com/hexxla/hexxladb/internal/engine"
	"github.com/hexxla/hexxladb/internal/index"
	"github.com/hexxla/hexxladb/internal/lattice"
)

func TestBTree_ringWalkRangeScanMatchesMortonOrder(t *testing.T) {
	t.Parallel()
	center := lattice.Coord{Q: 0, R: 0}
	var coords []lattice.Coord
	coords = lattice.WalkRings(coords, center, 2)

	var packed []lattice.PackedCoord
	for _, c := range coords {
		p, err := lattice.Pack(c)
		if err != nil {
			t.Fatal(err)
		}
		packed = append(packed, p)
	}
	wantOrder := append([]lattice.PackedCoord(nil), packed...)
	slices.SortFunc(wantOrder, func(a, b lattice.PackedCoord) int {
		return a.Compare(b)
	})

	dir := t.TempDir()
	path := filepath.Join(dir, "ring.db")
	e, err := engine.Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = e.Close() }()
	bt := engine.OpenBTree(e)

	for i := len(packed) - 1; i >= 0; i-- {
		k := index.CellKey(packed[i])
		if err := bt.Put(k, []byte{byte(i)}); err != nil {
			t.Fatal(err)
		}
	}

	minK := index.CellKey(wantOrder[0])
	maxK := index.CellKey(wantOrder[len(wantOrder)-1])
	var got []lattice.PackedCoord
	err = bt.AscendRange(minK, maxK, func(k, _ []byte) bool {
		p, err := index.ParseCellKey(k)
		if err != nil {
			t.Errorf("parse: %v", err)
			return false
		}
		got = append(got, p)
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(wantOrder) {
		t.Fatalf("len got %d want %d", len(got), len(wantOrder))
	}
	for i := range wantOrder {
		if got[i].Compare(wantOrder[i]) != 0 {
			t.Fatalf("at %d: got %+v want %+v", i, got[i], wantOrder[i])
		}
	}
}

func TestBTree_ring2OrderMatchesGoldenRingSequence(t *testing.T) {
	t.Parallel()
	center := lattice.Coord{Q: 1, R: -1}
	k := 2
	ring := lattice.Ring(center, k)
	var keys [][]byte
	for _, c := range ring {
		p, err := lattice.Pack(c)
		if err != nil {
			t.Fatal(err)
		}
		keys = append(keys, index.CellKey(p))
	}
	slices.SortFunc(keys, bytes.Compare)

	dir := t.TempDir()
	e, err := engine.Open(filepath.Join(dir, "g.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = e.Close() }()
	bt := engine.OpenBTree(e)
	for i := range keys {
		if err := bt.Put(keys[i], []byte{byte(i)}); err != nil {
			t.Fatal(err)
		}
	}
	var scan [][]byte
	err = bt.AscendRange(keys[0], keys[len(keys)-1], func(k, _ []byte) bool {
		scan = append(scan, append([]byte(nil), k...))
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(scan) != len(keys) {
		t.Fatalf("scan len %d want %d", len(scan), len(keys))
	}
	for i := range keys {
		if !bytes.Equal(scan[i], keys[i]) {
			t.Fatalf("i=%d scan %v want %v", i, scan[i], keys[i])
		}
	}
}
