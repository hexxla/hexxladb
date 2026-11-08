// Quickstart stores observations, searches them, and assembles related context.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/hexxla/hexxladb"
)

func main() {
	if err := run(context.Background()); err != nil {
		slog.Error("quickstart failed", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) (err error) {
	dir, err := os.MkdirTemp("", "hexxladb-quickstart-*")
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, os.RemoveAll(dir)) }()
	db, err := hexxladb.Open(filepath.Join(dir, "memory.db"), &hexxladb.Options{EnableMVCC: true})
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, db.Close()) }()

	if err := storeObservations(ctx, db); err != nil {
		return err
	}
	const question = "How should we plan the migration?"
	return db.View(func(tx *hexxladb.Tx) error {
		hits, err := tx.QueryCells(ctx, hexxladb.CellQuery{
			Query: "migration", MaxResults: 5, MaxScanRows: 100,
		})
		if err != nil {
			return err
		}
		if len(hits) == 0 {
			return errors.New("migration observation is missing")
		}
		pack, err := tx.LoadContext(ctx, hexxladb.LoadContextConfig{
			Seeds: []hexxladb.Coord{hits[0].Cell.Coord}, EdgeFilter: "requires",
			MaxHops: 2, MaxCells: 5,
		})
		if err != nil {
			return err
		}
		_, prompt, words, err := fitRenderedPrompt(nil, rankByConfidence(pack.Cells), question, 80, demoWordCounter)
		if err != nil {
			return err
		}
		fmt.Printf("Lexical hits: %d | Graph context cells: %d | Rendered words: %d\n\n%s\n", len(hits), len(pack.Cells), words, prompt)
		return nil
	})
}

func storeObservations(ctx context.Context, db *hexxladb.DB) error {
	return db.Update(func(tx *hexxladb.Tx) error {
		observations := []string{
			"Schedule the migration after the staging review.",
			"Verify the backup by restoring it in staging.",
		}
		keys := make([]hexxladb.PackedCoord, len(observations))
		now := time.Now().UnixNano()
		for i, content := range observations {
			placement, err := tx.FindUnusedCellPlacement(ctx, hexxladb.Coord{}, 2)
			if err != nil {
				return err
			}
			keys[i] = placement.Key
			if err := tx.PutCell(ctx, hexxladb.CellRecord{
				Key: placement.Key, RawContent: content, Tags: []string{"operations"},
				Provenance: hexxladb.ProvenanceWire{
					SourceID: "quickstart", Confidence: 0.9, CreatedAt: now, UpdatedAt: now,
				},
			}); err != nil {
				return err
			}
		}
		return tx.PutEdge(hexxladb.EdgeWalkRecord{
			From: keys[0], To: keys[1], RelationType: "requires", Weight: 1,
		})
	})
}
