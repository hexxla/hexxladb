//go:build integration

package hexxladb_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/hexxla/hexxladb"
	"github.com/hexxla/hexxladb/internal/record"
)

// engineMaxCellBlob is the engine B+ tree max value size ([internal/engine/btree_page.go] maxValBytes).
// Encoded cells must fit; see docs/hexxladb/API_REFERENCE.md "Storage limits".
const engineMaxCellBlob = 512

func mustEncodeCellFitsEngine(t *testing.T, rec record.CellRecord, ctx string) {
	t.Helper()
	b, err := record.EncodeCell(rec)
	if err != nil {
		t.Fatalf("%s: EncodeCell: %v", ctx, err)
	}
	if len(b) > engineMaxCellBlob {
		t.Fatalf("%s: encoded cell %d bytes > engine max %d — shrink RawContent/tags or see API_REFERENCE storage limits", ctx, len(b), engineMaxCellBlob)
	}
}

// TestIntegration_MVCC_latticeAndHighChurnPrune seeds a dense lattice with demo-like cell payloads
// (provenance, tags, validity where applicable), applies many MVCC updates to the center cell,
// then reclaims with PruneCellVersions. Regresses B+ tree delete/rebalance (see TestBTree_hexxladbLatticeChurnPruneShape).
func TestIntegration_MVCC_latticeAndHighChurnPrune(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in -short")
	}
	// R=5 (~91 coords): more leaf pressure than tourLatticeFillR=4; seed matches full_api_demo richness.
	const fillR = 5
	// 250 version bumps: matches full_api_demo center churn; must not corrupt on prune.
	const centerVersions = 250
	const retainBehind uint64 = 8

	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "lattice_prune.db")
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

	c0 := hexxladb.Coord{Q: 0, R: 0}
	c2 := hexxladb.Coord{Q: 0, R: 1}
	p0, err := hexxladb.Pack(c0)
	if err != nil {
		t.Fatal(err)
	}
	p2, err := hexxladb.Pack(c2)
	if err != nil {
		t.Fatal(err)
	}
	coords := hexxladb.WalkRings(nil, c0, fillR)
	ctx := context.Background()

	tagsRot := []string{"demo/tag-a", "demo/tag-b", "demo/tag-c", "demo/tag-d", "demo/tag-e"}
	srcRot := []string{
		"demo/source-alpha", "demo/source-beta", "demo/source-gamma", "demo/source-delta",
		"demo/source-epsilon", "demo/source-zeta", "demo/source-eta", "demo/source-theta",
	}
	vFrom := int64(100)
	vTo := int64(200)
	vCell := record.ValidityWire{ValidFrom: &vFrom, ValidTo: &vTo}
	prov := func(src string) record.ProvenanceWire {
		n := time.Now().UnixNano()
		return record.ProvenanceWire{
			SourceID: src, Confidence: 1, CreatedAt: n, UpdatedAt: n,
		}
	}

	if err := db.Batch(func(tx *hexxladb.Tx) error {
		for i, coord := range coords {
			packed, perr := hexxladb.Pack(coord)
			if perr != nil {
				return perr
			}
			raw := fmt.Sprintf("seed-q%d-r%d-%05d-body=%020d", coord.Q, coord.R, i, i)
			atCenter := coord.Q == c0.Q && coord.R == c0.R
			if atCenter {
				raw = "hello-version-1"
			}
			cell := record.CellRecord{
				Key:        packed,
				RawContent: raw,
				Provenance: prov(srcRot[i%len(srcRot)]),
				Tags: []string{
					tagsRot[i%len(tagsRot)],
					tagsRot[(i+2)%len(tagsRot)],
				},
			}
			if atCenter {
				cell.Tags = []string{"demo/tag-a", "demo/tag-b"}
				cell.Provenance = prov("demo/source-alpha")
				cell.ClusterHint = &p2
				cell.Validity = vCell
			} else if i%5 == 0 {
				cell.Validity = vCell
			}
			mustEncodeCellFitsEngine(t, cell, fmt.Sprintf("coord i=%d", i))
			if err := tx.PutCell(ctx, cell); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("initial lattice: %v", err)
	}

	for v := 2; v <= centerVersions; v++ {
		v := v
		if err := db.Update(func(tx *hexxladb.Tx) error {
			rec, ok, err := tx.GetCell(p0)
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("center cell missing at v=%d", v)
			}
			rec.RawContent = fmt.Sprintf("hello-version-%d", v)
			return tx.PutCell(ctx, rec)
		}); err != nil {
			t.Fatalf("bump version %d: %v", v, err)
		}
	}

	before, ok, err := db.SuggestedPruneBeforeSeq()
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected SuggestedPruneBeforeSeq after MVCCRetention")
	}

	for {
		deleted, err := db.PruneCellVersions(before, 2048)
		if err != nil {
			t.Fatalf("PruneCellVersions: %v", err)
		}
		if deleted == 0 {
			break
		}
	}

	if err := db.View(func(tx *hexxladb.Tx) error {
		rec, ok, err := tx.GetCell(p0)
		if err != nil {
			return err
		}
		if !ok {
			t.Fatal("center cell missing after prune")
		}
		want := fmt.Sprintf("hello-version-%d", centerVersions)
		if rec.RawContent != want {
			t.Fatalf("center content: got %q want %q", rec.RawContent, want)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	_, err = db.StatsMVCC()
	if err != nil {
		t.Fatalf("StatsMVCC: %v", err)
	}

}
