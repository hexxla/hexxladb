// Package storagecontract provides reusable contract tests for [domain.Storage]
// implementations. Any adapter can import this package and call [RunAll] from
// its own *testing.T to validate behavioral contracts.
package storagecontract

import (
	"context"
	"crypto/sha256"
	"sort"
	"testing"
	"time"

	"github.com/hexxla/hexxladb/internal/domain"
	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
)

// Factory creates a fresh, empty Storage for each sub-test.
type Factory func(t *testing.T) domain.Storage

// RunAll exercises every contract in the [domain.Storage] interface against the
// implementation returned by factory. Each sub-test gets a fresh instance.
func RunAll(t *testing.T, factory Factory) {
	t.Helper()

	t.Run("PutCell_GetCell_roundTrip", func(t *testing.T) {
		t.Parallel()
		s := factory(t)
		ctx := context.Background()
		key := mustPack(t, 1, 2)
		want := record.CellRecord{
			Key:        key,
			RawContent: "hello",
			Tags:       []string{"go", "test"},
			Provenance: record.ProvenanceWire{SourceID: "src-1", Confidence: 0.9},
		}
		if err := s.PutCell(ctx, want); err != nil {
			t.Fatal(err)
		}
		got, ok, err := s.GetCell(ctx, key)
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			t.Fatal("GetCell: expected ok=true")
		}
		if got.RawContent != want.RawContent {
			t.Fatalf("RawContent: got %q want %q", got.RawContent, want.RawContent)
		}
		if got.Provenance.SourceID != want.Provenance.SourceID {
			t.Fatalf("SourceID: got %q want %q", got.Provenance.SourceID, want.Provenance.SourceID)
		}
	})

	t.Run("GetCell_missing", func(t *testing.T) {
		t.Parallel()
		s := factory(t)
		ctx := context.Background()
		key := mustPack(t, 99, 99)
		_, ok, err := s.GetCell(ctx, key)
		if err != nil {
			t.Fatal(err)
		}
		if ok {
			t.Fatal("GetCell: expected ok=false for missing key")
		}
	})

	t.Run("PutCell_overwrite", func(t *testing.T) {
		t.Parallel()
		s := factory(t)
		ctx := context.Background()
		key := mustPack(t, 3, 4)
		rec1 := record.CellRecord{Key: key, RawContent: "v1"}
		rec2 := record.CellRecord{Key: key, RawContent: "v2"}
		if err := s.PutCell(ctx, rec1); err != nil {
			t.Fatal(err)
		}
		if err := s.PutCell(ctx, rec2); err != nil {
			t.Fatal(err)
		}
		got, ok, err := s.GetCell(ctx, key)
		if err != nil || !ok {
			t.Fatalf("GetCell: ok=%v err=%v", ok, err)
		}
		if got.RawContent != "v2" {
			t.Fatalf("overwrite: got %q want v2", got.RawContent)
		}
	})

	t.Run("DeleteCell_removes_and_idempotent", func(t *testing.T) {
		t.Parallel()
		s := factory(t)
		ctx := context.Background()
		key := mustPack(t, 5, 6)
		rec := record.CellRecord{Key: key, RawContent: "to-delete", Tags: []string{"del"}}
		if err := s.PutCell(ctx, rec); err != nil {
			t.Fatal(err)
		}
		if err := s.DeleteCell(ctx, key); err != nil {
			t.Fatal(err)
		}
		_, ok, err := s.GetCell(ctx, key)
		if err != nil {
			t.Fatal(err)
		}
		if ok {
			t.Fatal("GetCell after delete: expected ok=false")
		}
		// Idempotent: second delete must not error.
		if err := s.DeleteCell(ctx, key); err != nil {
			t.Fatalf("idempotent delete: %v", err)
		}
	})

	t.Run("AscendCellsBySource", func(t *testing.T) {
		t.Parallel()
		s := factory(t)
		ctx := context.Background()
		for i, src := range []string{"alpha", "beta", "alpha"} {
			key := mustPack(t, i+10, 0)
			if err := s.PutCell(ctx, record.CellRecord{
				Key:        key,
				RawContent: src + "-content",
				Provenance: record.ProvenanceWire{SourceID: src},
			}); err != nil {
				t.Fatal(err)
			}
		}
		var found []string
		if err := s.AscendCellsBySource(ctx, "alpha", func(r record.CellRecord) bool {
			found = append(found, r.RawContent)
			return true
		}); err != nil {
			t.Fatal(err)
		}
		if len(found) != 2 {
			t.Fatalf("AscendCellsBySource: got %d results want 2", len(found))
		}
	})

	t.Run("AscendCellsByTag", func(t *testing.T) {
		t.Parallel()
		s := factory(t)
		ctx := context.Background()
		for i, tags := range [][]string{{"go", "db"}, {"rust"}, {"go", "api"}} {
			key := mustPack(t, i+20, 0)
			if err := s.PutCell(ctx, record.CellRecord{
				Key: key, RawContent: "c", Tags: tags,
			}); err != nil {
				t.Fatal(err)
			}
		}
		var count int
		if err := s.AscendCellsByTag(ctx, "go", func(_ record.CellRecord) bool {
			count++
			return true
		}); err != nil {
			t.Fatal(err)
		}
		if count != 2 {
			t.Fatalf("AscendCellsByTag(go): got %d want 2", count)
		}
	})

	t.Run("AscendDistinctTags", func(t *testing.T) {
		t.Parallel()
		s := factory(t)
		ctx := context.Background()
		for i, tags := range [][]string{{"go", "db"}, {"rust", "db"}, {"go"}} {
			key := mustPack(t, i+30, 0)
			if err := s.PutCell(ctx, record.CellRecord{
				Key: key, RawContent: "c", Tags: tags,
			}); err != nil {
				t.Fatal(err)
			}
		}
		var tags []string
		if err := s.AscendDistinctTags(ctx, func(tag string) bool {
			tags = append(tags, tag)
			return true
		}); err != nil {
			t.Fatal(err)
		}
		sort.Strings(tags)
		if len(tags) < 3 {
			t.Fatalf("AscendDistinctTags: got %v, want at least [db go rust]", tags)
		}
	})

	t.Run("ListExistingTopics", func(t *testing.T) {
		t.Parallel()
		s := factory(t)
		ctx := context.Background()
		for i, tags := range [][]string{{"x", "y"}, {"y", "z"}} {
			key := mustPack(t, i+40, 0)
			if err := s.PutCell(ctx, record.CellRecord{
				Key: key, RawContent: "c", Tags: tags,
			}); err != nil {
				t.Fatal(err)
			}
		}
		topics, err := s.ListExistingTopics(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(topics) < 3 {
			t.Fatalf("ListExistingTopics: got %v, want at least [x y z]", topics)
		}
		// Must be sorted.
		if !sort.StringsAreSorted(topics) {
			t.Fatalf("ListExistingTopics: not sorted: %v", topics)
		}
	})

	t.Run("PutSeam_FindSeams", func(t *testing.T) {
		t.Parallel()
		s := factory(t)
		ctx := context.Background()
		cellA := mustPack(t, 0, 0)
		cellB := mustPack(t, 1, 0)
		// Put cells first so FindSeams has something to scan.
		if err := s.PutCell(ctx, record.CellRecord{Key: cellA, RawContent: "a"}); err != nil {
			t.Fatal(err)
		}
		if err := s.PutCell(ctx, record.CellRecord{Key: cellB, RawContent: "b"}); err != nil {
			t.Fatal(err)
		}
		seam := record.SeamRecord{
			ID:    "01ARZ3NDEKTSV4RRFFQ69G5FAV",
			CellA: cellA, CellB: cellB,
			SeamType: "contradiction", Reason: "test",
		}
		if err := s.PutSeam(ctx, seam); err != nil {
			t.Fatal(err)
		}
		center := lattice.Coord{Q: 0, R: 0}
		found, err := s.FindSeams(ctx, center, 3, false)
		if err != nil {
			t.Fatal(err)
		}
		if len(found) == 0 {
			t.Fatal("FindSeams: expected at least 1 seam")
		}
	})

	t.Run("ResolveSeam", func(t *testing.T) {
		t.Parallel()
		s := factory(t)
		ctx := context.Background()
		cellA := mustPack(t, 50, 0)
		cellB := mustPack(t, 50, 1)
		if err := s.PutCell(ctx, record.CellRecord{Key: cellA, RawContent: "a"}); err != nil {
			t.Fatal(err)
		}
		if err := s.PutCell(ctx, record.CellRecord{Key: cellB, RawContent: "b"}); err != nil {
			t.Fatal(err)
		}
		seamID := "01ARZ3NDEKTSV4RRFFQ69G5FA2"
		seam := record.SeamRecord{
			ID: seamID, CellA: cellA, CellB: cellB,
			SeamType: "contradiction", Reason: "resolve-test",
		}
		if err := s.PutSeam(ctx, seam); err != nil {
			t.Fatal(err)
		}
		if err := s.ResolveSeam(ctx, seamID, "accepted", "resolved in test"); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("MarkConflict", func(t *testing.T) {
		t.Parallel()
		s := factory(t)
		ctx := context.Background()
		a := lattice.Coord{Q: 60, R: 0}
		b := lattice.Coord{Q: 60, R: 1}
		keyA := mustPack(t, 60, 0)
		keyB := mustPack(t, 60, 1)
		if err := s.PutCell(ctx, record.CellRecord{Key: keyA, RawContent: "a"}); err != nil {
			t.Fatal(err)
		}
		if err := s.PutCell(ctx, record.CellRecord{Key: keyB, RawContent: "b"}); err != nil {
			t.Fatal(err)
		}
		if err := s.MarkConflict(ctx, a, b, "disagreement"); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("PutFacet_GetFacet", func(t *testing.T) {
		t.Parallel()
		s := factory(t)
		ctx := context.Background()
		key := mustPack(t, 70, 0)
		if err := s.PutCell(ctx, record.CellRecord{Key: key, RawContent: "cell"}); err != nil {
			t.Fatal(err)
		}
		facet := record.FacetRecord{Key: key, FacetID: 0, DerivedContent: "facet-0"}
		if err := s.PutFacet(ctx, facet); err != nil {
			t.Fatal(err)
		}
		got, ok, err := s.GetFacet(ctx, key, 0)
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			t.Fatal("GetFacet: expected ok=true")
		}
		if got.DerivedContent != "facet-0" {
			t.Fatalf("DerivedContent: got %q", got.DerivedContent)
		}
	})

	t.Run("GetFacet_missing", func(t *testing.T) {
		t.Parallel()
		s := factory(t)
		ctx := context.Background()
		key := mustPack(t, 71, 0)
		_, ok, err := s.GetFacet(ctx, key, 0)
		if err != nil {
			t.Fatal(err)
		}
		if ok {
			t.Fatal("GetFacet: expected ok=false for missing")
		}
	})

	t.Run("AscendFacetsForCell", func(t *testing.T) {
		t.Parallel()
		s := factory(t)
		ctx := context.Background()
		key := mustPack(t, 72, 0)
		if err := s.PutCell(ctx, record.CellRecord{Key: key, RawContent: "cell"}); err != nil {
			t.Fatal(err)
		}
		for i := range 3 {
			if err := s.PutFacet(ctx, record.FacetRecord{Key: key, FacetID: byte(i), DerivedContent: "f"}); err != nil {
				t.Fatal(err)
			}
		}
		var count int
		if err := s.AscendFacetsForCell(ctx, key, func(_ record.FacetRecord) bool {
			count++
			return true
		}); err != nil {
			t.Fatal(err)
		}
		if count != 3 {
			t.Fatalf("AscendFacetsForCell: got %d want 3", count)
		}
	})

	t.Run("PutEdge_GetEdge", func(t *testing.T) {
		t.Parallel()
		s := factory(t)
		ctx := context.Background()
		from := mustPack(t, 80, 0)
		to := mustPack(t, 80, 1)
		edge := record.EdgeRecord{
			From: from, To: to, RelationType: "related", Weight: 1.0,
			Provenance: record.ProvenanceWire{SourceID: "edge-src"},
		}
		if err := s.PutEdge(ctx, edge); err != nil {
			t.Fatal(err)
		}
		got, ok, err := s.GetEdge(ctx, from, to, "related")
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			t.Fatal("GetEdge: expected ok=true")
		}
		if got.Weight != 1.0 {
			t.Fatalf("Weight: got %f want 1.0", got.Weight)
		}
	})

	t.Run("GetEdge_missing", func(t *testing.T) {
		t.Parallel()
		s := factory(t)
		ctx := context.Background()
		from := mustPack(t, 81, 0)
		to := mustPack(t, 81, 1)
		_, ok, err := s.GetEdge(ctx, from, to, "nope")
		if err != nil {
			t.Fatal(err)
		}
		if ok {
			t.Fatal("GetEdge: expected ok=false for missing")
		}
	})

	t.Run("LinkCells", func(t *testing.T) {
		t.Parallel()
		s := factory(t)
		ctx := context.Background()
		from := lattice.Coord{Q: 90, R: 0}
		to := lattice.Coord{Q: 90, R: 1}
		fromKey := mustPack(t, 90, 0)
		toKey := mustPack(t, 90, 1)
		if err := s.PutCell(ctx, record.CellRecord{Key: fromKey, RawContent: "f"}); err != nil {
			t.Fatal(err)
		}
		if err := s.PutCell(ctx, record.CellRecord{Key: toKey, RawContent: "t"}); err != nil {
			t.Fatal(err)
		}
		if err := s.LinkCells(ctx, from, to, "link", 0.5, record.ProvenanceWire{SourceID: "lnk"}); err != nil {
			t.Fatal(err)
		}
		got, ok, err := s.GetEdge(ctx, fromKey, toKey, "link")
		if err != nil || !ok {
			t.Fatalf("GetEdge after LinkCells: ok=%v err=%v", ok, err)
		}
		if got.Weight != 0.5 {
			t.Fatalf("Weight: got %f want 0.5", got.Weight)
		}
	})

	t.Run("AscendEdgesFrom", func(t *testing.T) {
		t.Parallel()
		s := factory(t)
		ctx := context.Background()
		from := mustPack(t, 100, 0)
		for i := range 3 {
			to := mustPack(t, 100, i+1)
			if err := s.PutEdge(ctx, record.EdgeRecord{
				From: from, To: to, RelationType: "r", Weight: float64(i),
			}); err != nil {
				t.Fatal(err)
			}
		}
		var count int
		if err := s.AscendEdgesFrom(ctx, from, func(_ record.EdgeRecord) bool {
			count++
			return true
		}); err != nil {
			t.Fatal(err)
		}
		if count != 3 {
			t.Fatalf("AscendEdgesFrom: got %d want 3", count)
		}
	})

	t.Run("WalkRing_ring0", func(t *testing.T) {
		t.Parallel()
		s := factory(t)
		ctx := context.Background()
		center := lattice.Coord{Q: 0, R: 0}
		key := mustPack(t, 0, 0)
		if err := s.PutCell(ctx, record.CellRecord{Key: key, RawContent: "center"}); err != nil {
			t.Fatal(err)
		}
		var visited int
		if err := s.WalkRing(ctx, center, 0, func(_ lattice.Coord, data []byte, ok bool) bool {
			visited++
			if !ok {
				t.Fatal("WalkRing ring=0: center cell should be present")
			}
			return true
		}); err != nil {
			t.Fatal(err)
		}
		if visited != 1 {
			t.Fatalf("WalkRing ring=0: visited %d want 1", visited)
		}
	})

	t.Run("LoadContext_basic", func(t *testing.T) {
		t.Parallel()
		s := factory(t)
		ctx := context.Background()
		key := mustPack(t, 0, 0)
		if err := s.PutCell(ctx, record.CellRecord{Key: key, RawContent: "ctx-cell"}); err != nil {
			t.Fatal(err)
		}
		center := lattice.Coord{Q: 0, R: 0}
		cells, err := s.ScanContextRaw(ctx, center, 1, 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(cells) == 0 {
			t.Fatal("ScanContextRaw: expected at least 1 cell")
		}
	})

	t.Run("AscendCellsInTimeBucket", func(t *testing.T) {
		t.Parallel()
		s := factory(t)
		ctx := context.Background()
		nowNano := time.Now().UnixNano()
		bucket := nowNano / int64(time.Second) / (7 * 24 * 3600)
		key := mustPack(t, 110, 0)
		if err := s.PutCell(ctx, record.CellRecord{
			Key: key, RawContent: "time-cell",
			Validity: record.ValidityWire{ValidFrom: &nowNano},
		}); err != nil {
			t.Fatal(err)
		}
		var count int
		if err := s.AscendCellsInTimeBucket(ctx, bucket, func(_ record.CellRecord) bool {
			count++
			return true
		}); err != nil {
			t.Fatal(err)
		}
		if count < 1 {
			t.Fatalf("AscendCellsInTimeBucket: got %d want >=1", count)
		}
	})

	t.Run("UpdateFacet", func(t *testing.T) {
		t.Parallel()
		s := factory(t)
		ctx := context.Background()
		key := mustPack(t, 120, 0)
		cellContent := "update-facet-cell"
		if err := s.PutCell(ctx, record.CellRecord{Key: key, RawContent: cellContent}); err != nil {
			t.Fatal(err)
		}
		// DerivationHash must be SHA-256 of the cell's RawContent for UpdateFacet to succeed.
		hash := sha256.Sum256([]byte(cellContent))
		if err := s.PutFacet(ctx, record.FacetRecord{Key: key, FacetID: 1, DerivedContent: "old", DerivationHash: hash}); err != nil {
			t.Fatal(err)
		}
		if err := s.UpdateFacet(ctx, record.FacetRecord{Key: key, FacetID: 1, DerivedContent: "new", DerivationHash: hash}); err != nil {
			t.Fatal(err)
		}
		got, ok, err := s.GetFacet(ctx, key, 1)
		if err != nil || !ok {
			t.Fatalf("GetFacet after update: ok=%v err=%v", ok, err)
		}
		if got.DerivedContent != "new" {
			t.Fatalf("DerivedContent: got %q want new", got.DerivedContent)
		}
	})

	t.Run("AscendSeamsBySource", func(t *testing.T) {
		t.Parallel()
		s := factory(t)
		ctx := context.Background()
		cellA := mustPack(t, 130, 0)
		cellB := mustPack(t, 130, 1)
		if err := s.PutCell(ctx, record.CellRecord{Key: cellA, RawContent: "a"}); err != nil {
			t.Fatal(err)
		}
		if err := s.PutCell(ctx, record.CellRecord{Key: cellB, RawContent: "b"}); err != nil {
			t.Fatal(err)
		}
		seam := record.SeamRecord{
			ID: "01ARZ3NDEKTSV4RRFFQ69G5FA3", CellA: cellA, CellB: cellB,
			SeamType: "contradiction", Reason: "src-test",
			Provenance: record.ProvenanceWire{SourceID: "seam-src"},
		}
		if err := s.PutSeam(ctx, seam); err != nil {
			t.Fatal(err)
		}
		var count int
		if err := s.AscendSeamsBySource(ctx, "seam-src", func(_ record.SeamRecord) bool {
			count++
			return true
		}); err != nil {
			t.Fatal(err)
		}
		if count < 1 {
			t.Fatalf("AscendSeamsBySource: got %d want >=1", count)
		}
	})
}

// mustPack packs a coordinate or fails the test.
func mustPack(t *testing.T, q, r int) lattice.PackedCoord {
	t.Helper()
	p, err := lattice.Pack(lattice.Coord{Q: q, R: r})
	if err != nil {
		t.Fatalf("Pack(%d,%d): %v", q, r, err)
	}
	return p
}
