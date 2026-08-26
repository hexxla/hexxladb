package hexxladb_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/hexxla/hexxladb"
)

func TestPreflightCompactToReportsCapacityWithoutCreatingDestination(t *testing.T) {
	t.Parallel()
	db, sourcePath := openCompactDB(t, nil)
	putCompactCell(t, db, 0, 0, "preflight")
	closeCompactDB(t, db)

	destinationPath := filepath.Join(t.TempDir(), "destination.db")
	plan, err := hexxladb.PreflightCompactTo(t.Context(), sourcePath, destinationPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	if plan.SourceStorage.PrimaryBytes == 0 || plan.SourceStorage.LiveBytes == 0 {
		t.Fatalf("source storage not populated: %#v", plan.SourceStorage)
	}
	if len(plan.Space) != 1 || plan.Space[0].RequiredBytes < plan.SourceStorage.PrimaryBytes {
		t.Fatalf("capacity plan=%#v", plan.Space)
	}
	if _, err := os.Stat(destinationPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("preflight created destination: %v", err)
	}
}

func TestPreflightCompactToPreservesDestinationComponents(t *testing.T) {
	t.Parallel()
	db, sourcePath := openCompactDB(t, nil)
	closeCompactDB(t, db)

	for _, suffix := range []string{"", "-wal", "-changelog"} {
		t.Run(suffix, func(t *testing.T) {
			destinationPath := filepath.Join(t.TempDir(), "destination.db")
			componentPath := destinationPath + suffix
			want := []byte("preserve-existing-component")
			if err := os.WriteFile(componentPath, want, 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := hexxladb.PreflightCompactTo(t.Context(), sourcePath, destinationPath, nil)
			if !errors.Is(err, os.ErrExist) {
				t.Fatalf("preflight error=%v, want os.ErrExist", err)
			}
			got, err := os.ReadFile(componentPath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("component changed: got %q want %q", got, want)
			}
		})
	}
}

func TestPreflightMigrateV1ToV2IsExactAndLeavesNoFiles(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "source.db")
	destinationPath := filepath.Join(directory, "destination.db")
	seedMigrationFixture(t, sourcePath, &hexxladb.Options{EmbeddingDimension: 3})
	sourceBefore, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatal(err)
	}

	plan, err := hexxladb.PreflightMigrateV1ToV2(t.Context(), sourcePath, destinationPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Resumable || plan.ProcessedKeys != 0 {
		t.Fatalf("unexpected resume plan: %#v", plan)
	}
	if plan.SourcePrimaryBytes == 0 || plan.SourceStorage.LiveBytes == 0 || len(plan.Space) != 1 {
		t.Fatalf("incomplete migration plan: %#v", plan)
	}
	if _, err := os.Stat(destinationPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("preflight created destination: %v", err)
	}
	sourceAfter, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(sourceAfter, sourceBefore) {
		t.Fatal("preflight changed source primary")
	}
	matches, err := filepath.Glob(filepath.Join(directory, ".hexxladb-migrate-v1-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("preflight left source snapshots: %v", matches)
	}
}

func TestPreflightMigrateV1ToV2RecognizesMatchingResumeWithoutAdvancing(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "source.db")
	destinationPath := filepath.Join(directory, "destination.db")
	seedMigrationFixture(t, sourcePath, &hexxladb.Options{EmbeddingDimension: 3})

	ctx, cancel := context.WithCancel(t.Context())
	err := hexxladb.MigrateV1ToV2(ctx, sourcePath, destinationPath, &hexxladb.MigrationOptions{
		BatchSize: 1,
		OnProgress: func(hexxladb.MigrationProgress) {
			cancel()
		},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel migration: got %v, want context.Canceled", err)
	}
	destinationBefore, err := os.ReadFile(destinationPath)
	if err != nil {
		t.Fatal(err)
	}

	plan, err := hexxladb.PreflightMigrateV1ToV2(t.Context(), sourcePath, destinationPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Resumable || plan.ProcessedKeys == 0 {
		t.Fatalf("resume plan=%#v", plan)
	}
	destinationAfter, err := os.ReadFile(destinationPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(destinationAfter, destinationBefore) {
		t.Fatal("preflight advanced resumable destination")
	}
	if _, err := hexxladb.Open(destinationPath, nil); !errors.Is(err, hexxladb.ErrMigrationIncomplete) {
		t.Fatalf("ordinary open error=%v, want ErrMigrationIncomplete", err)
	}
}

func TestMigrateV1ToV2PreflightCallbackRunsBeforeDestinationCreation(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "source.db")
	destinationPath := filepath.Join(directory, "destination.db")
	seedMigrationFixture(t, sourcePath, &hexxladb.Options{EmbeddingDimension: 3})
	callbackCalled := false
	if err := hexxladb.MigrateV1ToV2(t.Context(), sourcePath, destinationPath, &hexxladb.MigrationOptions{
		OnPreflight: func(plan hexxladb.MigrationPreflight) {
			callbackCalled = true
			if plan.SourceStorage.LiveBytes == 0 || len(plan.Space) == 0 {
				t.Fatalf("preflight plan=%#v", plan)
			}
			if _, err := os.Stat(destinationPath); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("destination exists during preflight callback: %v", err)
			}
			probe, err := hexxladb.Open(sourcePath, nil)
			if probe != nil {
				_ = probe.Close()
			}
			if !errors.Is(err, hexxladb.ErrDatabaseLocked) {
				t.Fatalf("source lock probe error=%v, want ErrDatabaseLocked", err)
			}
		},
	}); err != nil {
		t.Fatal(err)
	}
	if !callbackCalled {
		t.Fatal("preflight callback was not called")
	}
}

func TestPreflightMaintenanceRejectsSourceAlias(t *testing.T) {
	t.Parallel()
	db, sourcePath := openCompactDB(t, nil)
	closeCompactDB(t, db)
	aliasPath := filepath.Join(t.TempDir(), "source-alias.db")
	if err := os.Link(sourcePath, aliasPath); err != nil {
		t.Fatal(err)
	}

	if _, err := hexxladb.PreflightCompactTo(t.Context(), sourcePath, aliasPath, nil); !errors.Is(err, hexxladb.ErrInvalidArgument) {
		t.Fatalf("compact alias error=%v, want ErrInvalidArgument", err)
	}
	if _, err := hexxladb.PreflightMigrateV1ToV2(t.Context(), sourcePath, aliasPath, nil); !errors.Is(err, hexxladb.ErrInvalidArgument) {
		t.Fatalf("migration alias error=%v, want ErrInvalidArgument", err)
	}
}
