package hexxladb_test

import (
	"bytes"
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
}
