package hexxladb_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/hexxla/hexxladb"
	"github.com/hexxla/hexxladb/internal/index"
	"github.com/hexxla/hexxladb/internal/record"
)

const migrationSeamID = "01ARZ3NDEKTSV4RRFFQ69G5FAV"

type migrationFixture struct {
	first  hexxladb.PackedCoord
	second hexxladb.PackedCoord
	bucket int64
}

func seedMigrationFixture(t *testing.T, path string, opts *hexxladb.Options) migrationFixture {
	t.Helper()
	db, err := hexxladb.Open(path, opts)
	if err != nil {
		t.Fatal(err)
	}
	first, err := hexxladb.Pack(hexxladb.Coord{Q: 1, R: -1})
	if err != nil {
		t.Fatal(err)
	}
	second, err := hexxladb.Pack(hexxladb.Coord{Q: 2, R: -1})
	if err != nil {
		t.Fatal(err)
	}
	validFrom := int64(4 * index.WeekNanos)
	cell := hexxladb.CellRecord{
		Key:        first,
		RawContent: "migrate me",
		Provenance: hexxladb.ProvenanceWire{SourceID: "fixture-source", Confidence: 0.9},
		Validity:   hexxladb.ValidityWire{ValidFrom: &validFrom},
		Tags:       []string{"migration", "fixture"},
	}
	other := hexxladb.CellRecord{Key: second, RawContent: "related"}
	seam := hexxladb.SeamRecord{
		ID:         migrationSeamID,
		CellA:      first,
		CellB:      second,
		SeamType:   hexxladb.SeamTypeConflict,
		Reason:     "fixture",
		DetectedAt: validFrom,
		Validity:   hexxladb.ValidityWire{ValidFrom: &validFrom},
		Provenance: hexxladb.ProvenanceWire{SourceID: "fixture-source"},
	}
	err = db.Update(func(tx *hexxladb.Tx) error {
		if err := tx.PutCell(t.Context(), cell); err != nil {
			return err
		}
		if err := tx.PutCell(t.Context(), other); err != nil {
			return err
		}
		if err := tx.PutFacet(hexxladb.FacetWalkRecord{Key: first, FacetID: 2, DerivedContent: "facet"}); err != nil {
			return err
		}
		if err := tx.PutEdge(hexxladb.EdgeWalkRecord{From: first, To: second, RelationType: "related", Weight: 0.75}); err != nil {
			return err
		}
		if err := tx.PutSeam(t.Context(), seam); err != nil {
			return err
		}
		if err := tx.PutEmbedding(first, []float32{1, 0, 0}); err != nil {
			return err
		}
		return tx.Put([]byte("app/custom"), []byte("opaque-value"))
	})
	if err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	bucket, ok := index.WeekBucketFromValidity(cell.Validity)
	if !ok {
		t.Fatal("fixture validity did not produce a time bucket")
	}
	return migrationFixture{first: first, second: second, bucket: bucket}
}

func assertMigrationFixture(t *testing.T, path string, opts *hexxladb.Options, fixture migrationFixture) {
	t.Helper()
	db, err := hexxladb.Open(path, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	stats, err := db.StatsMVCC()
	if err != nil || stats.VersionedRows != 2 || stats.CommitSeq == 0 {
		t.Fatalf("destination is not MVCC-capable: stats=%#v err=%v", stats, err)
	}
	err = db.View(func(tx *hexxladb.Tx) error {
		cell, ok, err := tx.GetCell(fixture.first)
		if err != nil || !ok || cell.RawContent != "migrate me" {
			t.Fatalf("cell mismatch: cell=%#v ok=%v err=%v", cell, ok, err)
		}
		facet, ok, err := tx.GetFacet(fixture.first, 2)
		if err != nil || !ok || facet.DerivedContent != "facet" {
			t.Fatalf("facet mismatch: facet=%#v ok=%v err=%v", facet, ok, err)
		}
		edge, ok, err := tx.GetEdge(fixture.first, fixture.second, "related")
		if err != nil || !ok || edge.Weight != 0.75 {
			t.Fatalf("edge mismatch: edge=%#v ok=%v err=%v", edge, ok, err)
		}
		embedding, ok, err := tx.GetEmbedding(fixture.first)
		if err != nil || !ok || len(embedding) != 3 || embedding[0] != 1 {
			t.Fatalf("embedding mismatch: embedding=%v ok=%v err=%v", embedding, ok, err)
		}
		results, searchStats, err := tx.SearchByEmbeddingWithStats(
			[]float32{1, 0, 0},
			hexxladb.EmbeddingSearchConfig{MaxResults: 1},
		)
		if err != nil || len(results) != 1 || results[0].Coord != fixture.first || searchStats.Path != hexxladb.EmbeddingSearchPathHNSW {
			t.Fatalf("HNSW mismatch: results=%#v stats=%#v err=%v", results, searchStats, err)
		}
		raw, ok, err := tx.Get([]byte("app/custom"))
		if err != nil || !ok || !bytes.Equal(raw, []byte("opaque-value")) {
			t.Fatalf("raw value mismatch: value=%q ok=%v err=%v", raw, ok, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	assertMigrationIndexes(t, db, fixture)
}

func assertMigrationIndexes(t *testing.T, db *hexxladb.DB, fixture migrationFixture) {
	t.Helper()
	err := db.View(func(tx *hexxladb.Tx) error {
		checks := 0
		if err := tx.AscendCellsBySource(t.Context(), "fixture-source", func(hexxladb.CellRecord) bool {
			checks++
			return true
		}); err != nil {
			return err
		}
		if err := tx.AscendCellsByTag(t.Context(), "migration", func(hexxladb.CellRecord) bool {
			checks++
			return true
		}); err != nil {
			return err
		}
		if err := tx.AscendCellsInTimeBucket(t.Context(), fixture.bucket, func(hexxladb.CellRecord) bool {
			checks++
			return true
		}); err != nil {
			return err
		}
		if err := tx.AscendSeamsBySource(t.Context(), "fixture-source", func(hexxladb.SeamRecord) bool {
			checks++
			return true
		}); err != nil {
			return err
		}
		if err := tx.AscendSeamsInTimeBucket(t.Context(), fixture.bucket, func(hexxladb.SeamRecord) bool {
			checks++
			return true
		}); err != nil {
			return err
		}
		seams, err := tx.FindSeams(t.Context(), hexxladb.Coord{Q: 1, R: -1}, 0, false)
		if err != nil {
			return err
		}
		if len(seams) != 1 || seams[0].ID != migrationSeamID || checks != 5 {
			t.Fatalf("rebuilt indexes mismatch: checks=%d seams=%#v", checks, seams)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func readOptionalMigrationFile(t *testing.T, path string) ([]byte, bool) {
	t.Helper()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false
	}
	if err != nil {
		t.Fatal(err)
	}
	return data, true
}

func TestMigrateV1ToV2LogicalEquivalence(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "source.db")
	destinationPath := filepath.Join(dir, "destination.db")
	fixture := seedMigrationFixture(t, sourcePath, &hexxladb.Options{EmbeddingDimension: 3})
	hookCalls := 0
	destinationOptions := &hexxladb.Options{
		CellValidator: hexxladb.CellValidatorFunc(func(hexxladb.CellRecord) error {
			hookCalls++
			return errors.New("migration must not run application validation")
		}),
		AfterPutCell: hexxladb.AfterPutCellHookFunc(func(context.Context, hexxladb.CellRecord) error {
			hookCalls++
			return errors.New("migration must not run cell hooks")
		}),
		AfterPutSeam: hexxladb.AfterPutSeamHookFunc(func(context.Context, hexxladb.SeamRecord) error {
			hookCalls++
			return errors.New("migration must not run seam hooks")
		}),
	}
	if err := hexxladb.MigrateV1ToV2(t.Context(), sourcePath, destinationPath, &hexxladb.MigrationOptions{DestinationOptions: destinationOptions}); err != nil {
		t.Fatal(err)
	}
	if hookCalls != 0 {
		t.Fatalf("migration invoked %d application hooks", hookCalls)
	}
	assertMigrationFixture(t, destinationPath, nil, fixture)
}

func TestMigrateV1ToV2CancellationResumesAndPreservesSource(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "source.db")
	destinationPath := filepath.Join(dir, "destination.db")
	fixture := seedMigrationFixture(t, sourcePath, &hexxladb.Options{EmbeddingDimension: 3})
	sourceBefore, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	sourceWALBefore, sourceWALExisted := readOptionalMigrationFile(t, sourcePath+"-wal")
	ctx, cancel := context.WithCancel(t.Context())
	sourceStayedLocked := false
	err = hexxladb.MigrateV1ToV2(ctx, sourcePath, destinationPath, &hexxladb.MigrationOptions{
		BatchSize: 2,
		OnProgress: func(hexxladb.MigrationProgress) {
			probe, openErr := hexxladb.Open(sourcePath, nil)
			if probe != nil {
				_ = probe.Close()
			}
			sourceStayedLocked = errors.Is(openErr, hexxladb.ErrDatabaseLocked)
			cancel()
		},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled migration error=%v, want context.Canceled", err)
	}
	sourceAfter, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(sourceBefore, sourceAfter) {
		t.Fatal("interrupted migration changed the source primary")
	}
	sourceWALAfter, sourceWALStillExists := readOptionalMigrationFile(t, sourcePath+"-wal")
	if sourceWALExisted != sourceWALStillExists || !bytes.Equal(sourceWALBefore, sourceWALAfter) {
		t.Fatal("interrupted migration changed the source WAL")
	}
	if !sourceStayedLocked {
		t.Fatal("migration did not retain exclusive source ownership through the copy")
	}
	if _, err := hexxladb.Open(destinationPath, nil); !errors.Is(err, hexxladb.ErrMigrationIncomplete) {
		t.Fatalf("ordinary open error=%v, want ErrMigrationIncomplete", err)
	}
	resumed := false
	err = hexxladb.MigrateV1ToV2(t.Context(), sourcePath, destinationPath, &hexxladb.MigrationOptions{
		BatchSize: 2,
		OnProgress: func(progress hexxladb.MigrationProgress) {
			resumed = resumed || progress.Resumed
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !resumed {
		t.Fatal("resume did not report durable prior progress")
	}
	assertMigrationFixture(t, destinationPath, nil, fixture)
}

func TestMigrateV1ToV2EncryptedSourceAndDestination(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "source.db")
	destinationPath := filepath.Join(dir, "destination.db")
	fixture := seedMigrationFixture(t, sourcePath, &hexxladb.Options{Passphrase: "old-secret", EmbeddingDimension: 3})
	err := hexxladb.MigrateV1ToV2(t.Context(), sourcePath, destinationPath, &hexxladb.MigrationOptions{
		SourceOptions:      &hexxladb.Options{Passphrase: "old-secret"},
		DestinationOptions: &hexxladb.Options{Passphrase: "new-secret"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := hexxladb.Open(destinationPath, &hexxladb.Options{Passphrase: "old-secret"}); !errors.Is(err, hexxladb.ErrEncryptionKeyMismatch) {
		t.Fatalf("old destination credential error=%v, want ErrEncryptionKeyMismatch", err)
	}
	assertMigrationFixture(t, destinationPath, &hexxladb.Options{Passphrase: "new-secret"}, fixture)
}

func TestMigrateV1ToV2ChangelogResetIsExplicit(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "source.db")
	destinationPath := filepath.Join(dir, "destination.db")
	fixture := seedMigrationFixture(t, sourcePath, &hexxladb.Options{EmbeddingDimension: 3, ChangelogEnabled: true})
	if err := hexxladb.MigrateV1ToV2(t.Context(), sourcePath, destinationPath, nil); !errors.Is(err, hexxladb.ErrMigrationChangelogState) {
		t.Fatalf("migration error=%v, want ErrMigrationChangelogState", err)
	}
	if _, err := os.Stat(destinationPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("refused migration created destination: %v", err)
	}
	if err := hexxladb.MigrateV1ToV2(t.Context(), sourcePath, destinationPath, &hexxladb.MigrationOptions{ResetChangelog: true}); err != nil {
		t.Fatal(err)
	}
	assertMigrationFixture(t, destinationPath, nil, fixture)
}

func TestMigrateV1ToV2RefusesExistingDestination(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "source.db")
	destinationPath := filepath.Join(dir, "destination.db")
	seedMigrationFixture(t, sourcePath, &hexxladb.Options{EmbeddingDimension: 3})
	destination, err := hexxladb.Open(destinationPath, &hexxladb.Options{EnableMVCC: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := destination.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(destinationPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := hexxladb.MigrateV1ToV2(t.Context(), sourcePath, destinationPath, nil); !errors.Is(err, hexxladb.ErrInvalidArgument) {
		t.Fatalf("collision error=%v, want ErrInvalidArgument", err)
	}
	after, err := os.ReadFile(destinationPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("migration changed an unrelated existing destination")
	}
}

func TestOpenRefusesNewerFormatVersion(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "future.db")
	db, err := hexxladb.Open(path, &hexxladb.Options{EnableMVCC: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	binary.BigEndian.PutUint32(data[8:12], 3)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := hexxladb.Open(path, nil); !errors.Is(err, hexxladb.ErrUnsupportedFormatVersion) {
		t.Fatalf("future format error=%v, want ErrUnsupportedFormatVersion", err)
	}
}

func TestMigrationFixtureUsesV1RecordEncoding(t *testing.T) {
	// Keep the fixture tied to the currently supported record envelope while the engine
	// migration exercises format v1 to v2. This catches accidental fixture drift.
	encoded, err := record.EncodeCell(hexxladb.CellRecord{})
	if err != nil {
		t.Fatal(err)
	}
	version, _, err := record.ParseEnvelope(record.MagicCell, encoded)
	if err != nil {
		t.Fatal(err)
	}
	if version != record.FormatVersionV1 {
		t.Fatalf("record version=%d, want %d", version, record.FormatVersionV1)
	}
}
