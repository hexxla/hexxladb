package hexxladb_test

import (
	"context"
	"errors"
	"fmt"
	"math"
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

func TestFindEdgePathValidatesEndpointsAndStopsCallbacks(t *testing.T) {
	t.Parallel()
	db := openPathfindTestDB(t)
	start := hexxladb.Coord{}
	if err := db.Update(func(tx *hexxladb.Tx) error {
		for i := 1; i <= 128; i++ {
			if err := tx.LinkCells(start, hexxladb.Coord{Q: i}, "test", 1, record.ProvenanceWire{}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.View(func(tx *hexxladb.Tx) error {
		invalid := hexxladb.Coord{Q: hexxladb.MaxAxialAbs, R: 1}
		for _, pair := range [][2]hexxladb.Coord{{invalid, invalid}, {invalid, start}, {start, invalid}} {
			if _, err := tx.FindEdgePath(t.Context(), pair[0], pair[1], hexxladb.FindEdgePathConfig{}); !errors.Is(err, hexxladb.ErrInvalidArgument) {
				t.Fatalf("invalid path endpoints %v: got %v", pair, err)
			}
		}
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		calls := 0
		_, err := tx.FindEdgePath(ctx, start, hexxladb.Coord{Q: 128}, hexxladb.FindEdgePathConfig{
			CostFunc: func(_, _ hexxladb.Coord) float64 {
				calls++
				cancel()
				return 1
			},
		})
		if !errors.Is(err, context.Canceled) || calls != 1 {
			t.Fatalf("cancelled path: %d callback calls, error %v", calls, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
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
		path, err := tx.FindEdgePath(t.Context(), coords[0], coords[3], hexxladb.FindEdgePathConfig{})
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

func TestFindEdgePath_WorkBudgets(t *testing.T) {
	t.Parallel()
	db := openPathfindTestDB(t)
	start, via, goal := hexxladb.Coord{}, hexxladb.Coord{Q: 1}, hexxladb.Coord{Q: 2}
	if err := db.Update(func(tx *hexxladb.Tx) error {
		for _, edge := range []struct {
			from, to hexxladb.Coord
			relation string
		}{{start, goal, "other"}, {start, via, "route"}, {via, goal, "route"}} {
			if err := tx.LinkCells(edge.from, edge.to, edge.relation, 1, record.ProvenanceWire{}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	for _, tt := range []struct {
		name string
		cfg  hexxladb.FindEdgePathConfig
		want []hexxladb.Coord
		err  error
	}{
		{"expansion limit", hexxladb.FindEdgePathConfig{Filter: "route", MaxExpand: 1}, nil, hexxladb.ErrSpatialScanLimit},
		{"filtered row counts", hexxladb.FindEdgePathConfig{Filter: "route", MaxEdgeRows: 2}, nil, hexxladb.ErrSpatialScanLimit},
		{"exact boundary", hexxladb.FindEdgePathConfig{Filter: "route", MaxExpand: 2, MaxEdgeRows: 3}, []hexxladb.Coord{start, via, goal}, nil},
		{"unreachable", hexxladb.FindEdgePathConfig{Filter: "absent", MaxExpand: 1, MaxEdgeRows: 2}, nil, nil},
		{"negative expansion limit", hexxladb.FindEdgePathConfig{MaxExpand: -1}, nil, hexxladb.ErrInvalidArgument},
		{"negative row limit", hexxladb.FindEdgePathConfig{MaxEdgeRows: -1}, nil, hexxladb.ErrInvalidArgument},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := db.View(func(tx *hexxladb.Tx) error {
				got, err := tx.FindEdgePath(t.Context(), start, goal, tt.cfg)
				if !errors.Is(err, tt.err) || !slices.Equal(got, tt.want) {
					t.Errorf("FindEdgePath = (%v, %v), want (%v, %v)", got, err, tt.want, tt.err)
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		})
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

func TestFindEdgePath_Overflow(t *testing.T) {
	t.Parallel()
	db := openPathfindTestDB(t)
	start := hexxladb.Coord{}
	first := hexxladb.Coord{Q: 1}
	second := hexxladb.Coord{R: 1}
	goal := hexxladb.Coord{Q: 1, R: 1}
	if err := db.Update(func(tx *hexxladb.Tx) error {
		for _, edge := range []struct {
			from, to hexxladb.Coord
			cost     float64
		}{
			{start, first, 0.75 * math.MaxFloat64},
			{first, goal, math.MaxFloat64},
			{start, second, 0.9 * math.MaxFloat64},
			{second, goal, 0.25 * math.MaxFloat64},
		} {
			if err := tx.LinkCells(edge.from, edge.to, "weighted", edge.cost, record.ProvenanceWire{}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if err := db.View(func(tx *hexxladb.Tx) error {
		path, err := tx.FindEdgePath(t.Context(), start, goal, hexxladb.FindEdgePathConfig{})
		if path != nil || !errors.Is(err, hexxladb.ErrInvalidArgument) {
			t.Fatalf("only overflowing routes: got %v, %v; want nil, ErrInvalidArgument", path, err)
		}
		path, err = tx.FindEdgePath(t.Context(), start, goal, hexxladb.FindEdgePathConfig{
			CostFunc: func(from, to hexxladb.Coord) float64 {
				switch {
				case from == first:
					return math.MaxFloat64
				case from == start && to == second:
					return 0.5 * math.MaxFloat64
				default:
					return 0.25 * math.MaxFloat64
				}
			},
		})
		if want := []hexxladb.Coord{start, second, goal}; err != nil || !slices.Equal(path, want) {
			t.Fatalf("finite alternative: got %v, %v; want %v, nil", path, err, want)
		}
		path, err = tx.FindEdgePath(t.Context(), start, hexxladb.Coord{Q: 100}, hexxladb.FindEdgePathConfig{})
		if path != nil || err != nil {
			t.Fatalf("disconnected goal: got %v, %v; want nil, nil", path, err)
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
		path, err := tx.FindEdgePath(t.Context(), lattice.Coord{Q: 0, R: 0}, lattice.Coord{Q: 5, R: 5}, hexxladb.FindEdgePathConfig{})
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
		path, err := tx.FindEdgePath(t.Context(), a, c, hexxladb.FindEdgePathConfig{Filter: "related"})
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
		path, err := tx.FindEdgePath(t.Context(), coords[0], coords[3], hexxladb.FindEdgePathConfig{
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
		path, err := tx.FindEdgePath(t.Context(), coords[0], coords[3], hexxladb.FindEdgePathConfig{})
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
		result, err := tx.WalkEdges(t.Context(), coords[0], "", 2, 0)
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
		pack, err := tx.LoadContext(t.Context(), hexxladb.LoadContextConfig{
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
		bounded, err := tx.LoadContext(t.Context(), hexxladb.LoadContextConfig{
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
		ringOnly, err := tx.LoadContext(t.Context(), hexxladb.LoadContextConfig{
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

func TestLoadContext_ByMultipleRelations(t *testing.T) {
	t.Parallel()
	db := openPathfindTestDB(t)
	seed, required, caused, other := hexxladb.Coord{}, hexxladb.Coord{Q: 1}, hexxladb.Coord{Q: 2}, hexxladb.Coord{R: 1}
	if err := db.Update(func(tx *hexxladb.Tx) error {
		for _, coord := range []hexxladb.Coord{seed, required, caused, other} {
			if err := putCell(tx, coord, "cell"); err != nil {
				return err
			}
		}
		for _, edge := range []struct {
			from, to hexxladb.Coord
			relation string
		}{
			{seed, required, "requires"},
			{required, caused, "causes"},
			{seed, other, "requires,causes"},
		} {
			if err := tx.LinkCells(edge.from, edge.to, edge.relation, 1, record.ProvenanceWire{}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if err := db.View(func(tx *hexxladb.Tx) error {
		for _, filter := range []string{"requires,causes", "requires, causes, requires,"} {
			pack, err := tx.LoadContext(t.Context(), hexxladb.LoadContextConfig{
				Seeds: []hexxladb.Coord{seed}, EdgeFilter: filter, MaxHops: 2,
			})
			if err != nil {
				return err
			}
			var got []hexxladb.Coord
			for _, cell := range pack.Cells {
				got = append(got, cell.Coord)
			}
			if want := []hexxladb.Coord{seed, required, caused}; !slices.Equal(got, want) {
				t.Fatalf("filter %q returned %v, want %v", filter, got, want)
			}
		}
		got, err := tx.WalkEdges(t.Context(), seed, "requires,causes", 2, 10)
		if err != nil {
			return err
		}
		if want := []hexxladb.Coord{seed, other}; !slices.Equal(got, want) {
			t.Fatalf("exact relation walk returned %v, want %v", got, want)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
