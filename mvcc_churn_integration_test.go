//go:build integration

package hexxladb_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/hexxla/hexxladb"
	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
)

// TestIntegration_MVCC_sustainedPutCellSameKey exercises many MVCC commits updating one logical cell,
// then reclaims stale versions using several prune batches ([DB.PruneCellVersions] batch size 2048).
// Heavy pruning mid-workload is covered by unit tests in mvcc_test.go; operators should run bounded prune
// passes during maintenance windows (see docs/hexxladb/OPERATIONS.md § MVCC retention).
func TestIntegration_MVCC_sustainedPutCellSameKey(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration churn in -short")
	}
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "churn.db")
	const iterations = 6000
	const retainBehind uint64 = 400
	db, err := hexxladb.Open(path, &hexxladb.Options{
		EnableMVCC: true,
		MVCCRetention: hexxladb.MVCCRetention{
			RetainCommitsBehindHead: retainBehind,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	p := lattice.PackedCoord{3, 9}
	ctx := context.Background()
	vFrom := int64(1e12)
	vTo := int64(2e12)
	for i := range iterations {
		n := time.Now().UnixNano() + int64(i)
		rec := record.CellRecord{
			Key:        p,
			RawContent: fmt.Sprintf("payload-%08d-churn=%06d", i, i%100000),
			Provenance: record.ProvenanceWire{
				SourceID: fmt.Sprintf("churn/source-%03d", i%200), Confidence: 1, CreatedAt: n, UpdatedAt: n,
			},
			Validity: record.ValidityWire{ValidFrom: &vFrom, ValidTo: &vTo},
			Tags:     []string{"churn/tag-a", "churn/tag-b"},
		}
		if encoded, err := record.EncodeCell(rec); err != nil {
			t.Fatalf("EncodeCell iter %d: %v", i, err)
		} else if len(encoded) > 512 {
			t.Fatalf("encoded cell iter %d is %d bytes (>512 engine limit)", i, len(encoded))
		}
		if err := db.Update(func(tx *hexxladb.Tx) error {
			return tx.PutCell(ctx, rec)
		}); err != nil {
			t.Fatal(err)
		}
	}
	stats, err := db.StatsMVCC()
	if err != nil {
		t.Fatal(err)
	}
	if stats.LogicalCells != 1 {
		t.Fatalf("logical cells: %d", stats.LogicalCells)
	}
	if stats.VersionedRows != int64(iterations) {
		t.Fatalf("versioned rows: got %d want %d", stats.VersionedRows, iterations)
	}
	if stats.CommitSeq != uint64(iterations) {
		t.Fatalf("commit seq: got %d want %d", stats.CommitSeq, iterations)
	}
	bs, ok, err := db.SuggestedPruneBeforeSeq()
	if err != nil || !ok {
		t.Fatalf("SuggestedPruneBeforeSeq ok=%v err=%v", ok, err)
	}
	if stats.CommitSeq > retainBehind {
		want := stats.CommitSeq - retainBehind
		if bs != want {
			t.Fatalf("beforeSeq: got %d want %d", bs, want)
		}
	} else if bs != 0 {
		t.Fatalf("beforeSeq: got %d want 0 when commit seq <= retention window", bs)
	}
	var totalDeleted int
	for {
		deleted, err := db.PruneCellVersions(bs, 2048)
		if err != nil {
			t.Fatal(err)
		}
		totalDeleted += deleted
		if deleted == 0 {
			break
		}
	}
	if totalDeleted < 1 {
		t.Fatalf("expected prune to reclaim stale rows, deleted=%d", totalDeleted)
	}
	stats2, err := db.StatsMVCC()
	if err != nil {
		t.Fatal(err)
	}
	if stats2.VersionedRows >= stats.VersionedRows {
		t.Fatalf("rows should shrink after prune: before=%d after=%d", stats.VersionedRows, stats2.VersionedRows)
	}
}
