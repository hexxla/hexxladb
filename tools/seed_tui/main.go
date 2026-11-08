// seed_tui creates synthetic data for exploring the TUI in an unused directory.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/hexxla/hexxladb"
)

func main() {
	dir := flag.String("dir", ".tmp/tui-sample", "new directory for the fixture database")
	flag.Parse()
	if err := seed(context.Background(), *dir); err != nil {
		slog.Error("seed TUI fixture", "error", err)
		os.Exit(1)
	}
	fmt.Println(filepath.Join(*dir, "fixture.db"))
}

func seed(ctx context.Context, dir string) error {
	if err := os.Mkdir(dir, 0o700); err != nil {
		return err
	}
	db, err := hexxladb.Open(filepath.Join(dir, "fixture.db"), &hexxladb.Options{EnableMVCC: true})
	if err != nil {
		return err
	}
	writeErr := seedCells(ctx, db)
	return errors.Join(writeErr, db.Close())
}

func seedCells(ctx context.Context, db *hexxladb.DB) error {
	topics := []string{"backups", "migration", "monitoring"}
	actions := []string{"inventory", "review", "test", "approval", "runbook"}
	now := time.Now().UnixNano()
	keys := make([]hexxladb.PackedCoord, 0, len(topics)*len(actions))
	if err := db.Update(func(tx *hexxladb.Tx) error {
		for i, topic := range topics {
			for _, action := range actions {
				placement, err := tx.FindUnusedCellPlacement(ctx, hexxladb.Coord{Q: i*3 - 3}, 2)
				if err != nil {
					return err
				}
				keys = append(keys, placement.Key)
				if err := tx.PutCell(ctx, hexxladb.CellRecord{
					Key: placement.Key, RawContent: topic + " " + action, Tags: []string{topic, action},
					Provenance: hexxladb.ProvenanceWire{
						SourceID: "fixture", Confidence: 0.8, CreatedAt: now, UpdatedAt: now,
					},
				}); err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		return err
	}
	return db.Update(func(tx *hexxladb.Tx) error {
		if err := tx.PutFacet(hexxladb.FacetWalkRecord{
			Key: keys[0], DerivedContent: "Inventory backup destinations.", LastRotated: now,
		}); err != nil {
			return err
		}
		if err := tx.PutEdge(hexxladb.EdgeWalkRecord{
			From: keys[0], To: keys[1], RelationType: "requires", Weight: 1,
		}); err != nil {
			return err
		}
		return tx.PutSeam(ctx, hexxladb.SeamRecord{
			ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", CellA: keys[0], CellB: keys[1],
			SeamType: hexxladb.SeamTypeConflict, Reason: "Review and inventory disagree on the backup schedule.",
			DetectedAt: now,
		})
	})
}
