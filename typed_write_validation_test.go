package hexxladb_test

import (
	"errors"
	"math"
	"path/filepath"
	"testing"

	"github.com/hexxla/hexxladb"
)

func TestTypedWritesRejectInvalidCoordinatesAndNonFiniteMetadata(t *testing.T) {
	t.Parallel()
	db, err := hexxladb.Open(filepath.Join(t.TempDir(), "validation.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	valid, err := hexxladb.Pack(hexxladb.Coord{})
	if err != nil {
		t.Fatal(err)
	}
	invalid := hexxladb.PackedCoord{0, 1 << 63}
	validSeamID := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	tests := map[string]func(*hexxladb.Tx) error{
		"cell key": func(tx *hexxladb.Tx) error {
			return tx.PutCell(t.Context(), hexxladb.CellRecord{Key: invalid})
		},
		"cell cluster hint": func(tx *hexxladb.Tx) error {
			return tx.PutCell(t.Context(), hexxladb.CellRecord{Key: valid, ClusterHint: &invalid})
		},
		"cell confidence": func(tx *hexxladb.Tx) error {
			return tx.PutCell(t.Context(), hexxladb.CellRecord{
				Key: valid, Provenance: hexxladb.ProvenanceWire{Confidence: math.NaN()},
			})
		},
		"facet key": func(tx *hexxladb.Tx) error {
			return tx.PutFacet(hexxladb.FacetWalkRecord{Key: invalid})
		},
		"edge endpoint": func(tx *hexxladb.Tx) error {
			return tx.PutEdge(hexxladb.EdgeWalkRecord{From: invalid, To: valid, RelationType: "r", Weight: 1})
		},
		"edge provenance": func(tx *hexxladb.Tx) error {
			return tx.PutEdge(hexxladb.EdgeWalkRecord{
				From: valid, To: valid, RelationType: "r", Weight: 1,
				Provenance: hexxladb.ProvenanceWire{Confidence: math.Inf(1)},
			})
		},
		"seam endpoint": func(tx *hexxladb.Tx) error {
			return tx.PutSeam(t.Context(), hexxladb.SeamRecord{ID: validSeamID, CellA: invalid, CellB: valid})
		},
		"seam confidence delta": func(tx *hexxladb.Tx) error {
			return tx.PutSeam(t.Context(), hexxladb.SeamRecord{
				ID: validSeamID, CellA: valid, CellB: valid, ConfidenceDelta: math.Inf(-1),
			})
		},
		"embedding coordinate": func(tx *hexxladb.Tx) error {
			return tx.PutEmbedding(invalid, []float32{1, 0})
		},
	}

	for name, write := range tests {
		t.Run(name, func(t *testing.T) {
			err := db.Update(write)
			if !errors.Is(err, hexxladb.ErrInvalidArgument) {
				t.Fatalf("write error = %v, want ErrInvalidArgument", err)
			}
		})
	}
}
