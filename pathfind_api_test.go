package hexxladb_test

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"testing"

	"github.com/hexxla/hexxladb"
	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
)

func openPathfindTestDB(t *testing.T) *hexxladb.DB {
	t.Helper()
	dir := t.TempDir()
	db, err := hexxladb.Open(filepath.Join(dir, "pf.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func putCell(tx *hexxladb.Tx, c lattice.Coord, content string) error {
	p, err := lattice.Pack(c)
	if err != nil {
		return err
	}
	return tx.PutCell(context.Background(), record.CellRecord{
		Key:        p,
		RawContent: content,
		Tags:       []string{"test"},
	})
}

func TestFindEdgePath_basic(t *testing.T) {
	t.Parallel()
	db := openPathfindTestDB(t)

	coords := []lattice.Coord{{Q: 0, R: 0}, {Q: 1, R: 0}, {Q: 2, R: 0}, {Q: 3, R: 0}}
	err := db.Update(func(tx *hexxladb.Tx) error {
		for _, c := range coords {
			if err := putCell(tx, c, "cell"); err != nil {
				return err
			}
		}
		for i := 0; i < len(coords)-1; i++ {
			if err := tx.LinkCells(coords[i], coords[i+1], "related", 1.0, record.ProvenanceWire{}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	err = db.View(func(tx *hexxladb.Tx) error {
		path, err := tx.FindEdgePath(context.Background(), coords[0], coords[3], hexxladb.FindEdgePathConfig{})
		if err != nil {
			return err
		}
		if len(path) != 4 {
			t.Fatalf("expected path len=4, got %d: %v", len(path), path)
		}
		if path[0] != coords[0] || path[3] != coords[3] {
			t.Fatalf("endpoints wrong: %v", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestFindEdgePath_SubunitWeightsRemainOptimal(t *testing.T) {
	t.Parallel()
	db := openPathfindTestDB(t)
	start := lattice.Coord{Q: 0, R: 0}
	via := lattice.Coord{Q: 0, R: 1}
	goal := lattice.Coord{Q: 10, R: 0}

	if err := db.Update(func(tx *hexxladb.Tx) error {
		for _, edge := range []struct {
			from, to lattice.Coord
			weight   float64
		}{{start, goal, 2}, {start, via, 0.1}, {via, goal, 0.1}} {
			if err := tx.LinkCells(edge.from, edge.to, "weighted", edge.weight, record.ProvenanceWire{}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if err := db.View(func(tx *hexxladb.Tx) error {
		path, err := tx.FindEdgePath(t.Context(), start, goal, hexxladb.FindEdgePathConfig{})
		if err != nil {
			return err
		}
		want := []hexxladb.Coord{start, via, goal}
		if !slices.Equal(path, want) {
			t.Fatalf("weighted shortest path: got %v, want %v", path, want)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestFindEdgePath_UnfilteredUsesCheapestRelation(t *testing.T) {
	t.Parallel()
	db := openPathfindTestDB(t)
	start := lattice.Coord{Q: 0, R: 0}
	via := lattice.Coord{Q: 1, R: 0}
	goal := lattice.Coord{Q: 2, R: 0}

	if err := db.Update(func(tx *hexxladb.Tx) error {
		for _, c := range []lattice.Coord{start, via, goal} {
			if err := putCell(tx, c, "cell"); err != nil {
				return err
			}
		}
		// Relation types are distinct edge identities. The shorter name sorts
		// first in the edge key but is deliberately more expensive.
		for _, edge := range []struct {
			from, to lattice.Coord
			relation string
			weight   float64
		}{
			{start, goal, "x", 10},
			{start, goal, "cheapest", 1},
			{start, via, "route", 2},
			{via, goal, "route", 2},
		} {
			if err := tx.LinkCells(edge.from, edge.to, edge.relation, edge.weight, record.ProvenanceWire{}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if err := db.View(func(tx *hexxladb.Tx) error {
		path, err := tx.FindEdgePath(t.Context(), start, goal, hexxladb.FindEdgePathConfig{})
		if err != nil {
			return err
		}
		want := []hexxladb.Coord{start, goal}
		if !slices.Equal(path, want) {
			t.Fatalf("unfiltered path: got %v, want cheapest direct edge %v", path, want)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestFindEdgePath_noEdges(t *testing.T) {
	t.Parallel()
	db := openPathfindTestDB(t)

	err := db.Update(func(tx *hexxladb.Tx) error {
		for _, c := range []lattice.Coord{{Q: 0, R: 0}, {Q: 5, R: 5}} {
			if err := putCell(tx, c, "cell"); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	err = db.View(func(tx *hexxladb.Tx) error {
		path, err := tx.FindEdgePath(context.Background(), lattice.Coord{Q: 0, R: 0}, lattice.Coord{Q: 5, R: 5}, hexxladb.FindEdgePathConfig{})
		if err != nil {
			return err
		}
		if path != nil {
			t.Fatalf("expected nil path with no edges, got %v", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestFindEdgePath_filtered(t *testing.T) {
	t.Parallel()
	db := openPathfindTestDB(t)

	a := lattice.Coord{Q: 0, R: 0}
	b := lattice.Coord{Q: 1, R: 0}
	c := lattice.Coord{Q: 2, R: 0}

	err := db.Update(func(tx *hexxladb.Tx) error {
		for _, coord := range []lattice.Coord{a, b, c} {
			if err := putCell(tx, coord, "cell"); err != nil {
				return err
			}
		}
		if err := tx.LinkCells(a, b, "related", 1.0, record.ProvenanceWire{}); err != nil {
			return err
		}
		if err := tx.LinkCells(b, c, "derived", 1.0, record.ProvenanceWire{}); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	err = db.View(func(tx *hexxladb.Tx) error {
		path, err := tx.FindEdgePath(context.Background(), a, c, hexxladb.FindEdgePathConfig{Filter: "related"})
		if err != nil {
			return err
		}
		if path != nil {
			t.Fatalf("expected nil path with filter=related (can't reach c), got %v", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestFindEdgePath_customCostFunc(t *testing.T) {
	t.Parallel()
	db := openPathfindTestDB(t)

	// Graph: A─B─C─D (linear chain, all weight=1).
	// CostFunc marks B→C as impassable (cost < 0); path must go A→B then fail,
	// proving CostFunc overrides the stored edge weight.
	coords := []lattice.Coord{{Q: 0, R: 0}, {Q: 1, R: 0}, {Q: 2, R: 0}, {Q: 3, R: 0}}
	err := db.Update(func(tx *hexxladb.Tx) error {
		for _, c := range coords {
			if err := putCell(tx, c, "cell"); err != nil {
				return err
			}
		}
		for i := range len(coords) - 1 {
			if err := tx.LinkCells(coords[i], coords[i+1], "seq", 1.0, record.ProvenanceWire{}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	b, c := coords[1], coords[2]
	err = db.View(func(tx *hexxladb.Tx) error {
		// Block B→C so path from A to D is impossible.
		path, err := tx.FindEdgePath(context.Background(), coords[0], coords[3], hexxladb.FindEdgePathConfig{
			CostFunc: func(from, to hexxladb.Coord) float64 {
				if from == b && to == c {
					return -1 // impassable
				}
				return 1.0
			},
		})
		if err != nil {
			return err
		}
		if path != nil {
			t.Fatalf("expected nil path with B→C blocked, got %v", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Without blocking, path must exist.
	err = db.View(func(tx *hexxladb.Tx) error {
		path, err := tx.FindEdgePath(context.Background(), coords[0], coords[3], hexxladb.FindEdgePathConfig{})
		if err != nil {
			return err
		}
		if len(path) != 4 {
			t.Fatalf("expected len=4 without CostFunc block, got %d", len(path))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestWalkEdges_basic(t *testing.T) {
	t.Parallel()
	db := openPathfindTestDB(t)

	coords := []lattice.Coord{{Q: 0, R: 0}, {Q: 1, R: 0}, {Q: 2, R: 0}}
	err := db.Update(func(tx *hexxladb.Tx) error {
		for _, c := range coords {
			if err := putCell(tx, c, "cell"); err != nil {
				return err
			}
		}
		if err := tx.LinkCells(coords[0], coords[1], "next", 1.0, record.ProvenanceWire{}); err != nil {
			return err
		}
		if err := tx.LinkCells(coords[1], coords[2], "next", 1.0, record.ProvenanceWire{}); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	err = db.View(func(tx *hexxladb.Tx) error {
		result, err := tx.WalkEdges(context.Background(), coords[0], "", 2, 0)
		if err != nil {
			return err
		}
		if len(result) != 3 {
			t.Fatalf("expected 3 reachable coords, got %d: %v", len(result), result)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestLoadContext_ByEdges_basic(t *testing.T) {
	t.Parallel()
	db := openPathfindTestDB(t)

	coords := []lattice.Coord{{Q: 0, R: 0}, {Q: 1, R: 0}, {Q: 2, R: 0}, {Q: 10, R: 10}}
	err := db.Update(func(tx *hexxladb.Tx) error {
		for i, c := range coords {
			if err := putCell(tx, c, fmt.Sprintf("content-%d", i)); err != nil {
				return err
			}
		}
		if err := tx.LinkCells(coords[0], coords[1], "seq", 1.0, record.ProvenanceWire{}); err != nil {
			return err
		}
		if err := tx.LinkCells(coords[1], coords[2], "seq", 1.0, record.ProvenanceWire{}); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	err = db.View(func(tx *hexxladb.Tx) error {
		pack, err := tx.LoadContext(context.Background(), hexxladb.LoadContextConfig{
			Seeds:      []hexxladb.Coord{coords[0]},
			EdgeFilter: "seq",
			MaxHops:    5,
			MaxCells:   10,
			Assembly:   hexxladb.ContextAssemblyConfig{Assemble: hexxladb.DefaultAssembleCellViewOpts()},
		})
		if err != nil {
			return err
		}
		if len(pack.Cells) != 3 {
			t.Fatalf("expected 3 cells via edges, got %d", len(pack.Cells))
		}
		bounded, err := tx.LoadContext(context.Background(), hexxladb.LoadContextConfig{
			Seeds:      []hexxladb.Coord{coords[0]},
			EdgeFilter: "seq",
			MaxHops:    5,
			MaxCells:   2,
			Assembly:   hexxladb.ContextAssemblyConfig{Assemble: hexxladb.DefaultAssembleCellViewOpts()},
		})
		if err != nil {
			return err
		}
		if len(bounded.Cells) != 2 || bounded.Cells[0].Coord != coords[0] || bounded.Cells[1].Coord != coords[1] {
			t.Fatalf("bounded edge context = %+v, want first two BFS cells", bounded.Cells)
		}
		if !bounded.Stats.ResultLimitReached {
			t.Fatal("bounded edge context did not report result limit")
		}
		ringOnly, err := tx.LoadContext(context.Background(), hexxladb.LoadContextConfig{
			Seeds:    []hexxladb.Coord{coords[0]},
			MaxRing:  1,
			MaxHops:  5,
			MaxCells: 10,
			Assembly: hexxladb.ContextAssemblyConfig{Assemble: hexxladb.DefaultAssembleCellViewOpts()},
		})
		if err != nil {
			return err
		}
		if len(ringOnly.Cells) != 2 {
			t.Fatalf("MaxHops without EdgeFilter loaded %d cells, want 2 from ring radius 1", len(ringOnly.Cells))
		}
		for _, cell := range ringOnly.Cells {
			if cell.Coord == coords[2] {
				t.Fatal("MaxHops without EdgeFilter unexpectedly selected graph traversal")
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
