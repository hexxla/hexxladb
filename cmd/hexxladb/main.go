// Command hexxladb is the composition root for this repository and a reference for
// embedding HexxlaDB in other programs. A typical consumer binary can follow
// the same structure:
//
//  1. Load process configuration (environment, flags).
//  2. Configure structured logging (e.g. log/slog).
//  3. Open the database: db, err := hexxladb.Open(path, opts); defer db.Close().
//  4. Build your application service with constructor injection; secondary
//     adapters should use only the public hexxladb API (see docs).
//  5. Exit, or run your own long-lived work — this repo is library-first, so
//     this command finishes after wiring unless you add servers or workers.
//
// Copy or adapt this file when integrating hexxladb into another module; keep
// business rules in internal/ (or your own packages), not in main.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/hexxla/hexxladb"
	hexxladbout "github.com/hexxla/hexxladb/internal/adapters/out/hexxladb"
	"github.com/hexxla/hexxladb/internal/app"
	"github.com/hexxla/hexxladb/internal/config"
	"github.com/hexxla/hexxladb/internal/domain"
	"github.com/hexxla/hexxladb/internal/index"
	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
)

func main() {
	os.Exit(run())
}

func run() int {
	demo := flag.Bool("demo", false, "write a grid of cells, scan by source, reopen, and verify reads (requires -path or HEXXLA_DB_PATH)")
	pathFlag := flag.String("path", "", "database file path (overrides HEXXLA_DB_PATH when set)")
	cells := flag.Int("cells", 500, "number of grid cells for -demo (1..50000)")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		slog.Error("config", "err", err)
		return 1
	}

	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(log)

	dbPath := *pathFlag
	if dbPath == "" {
		dbPath = os.Getenv("HEXXLA_DB_PATH")
	}

	if *demo {
		if dbPath == "" {
			log.Error("demo requires -path or HEXXLA_DB_PATH")
			return 1
		}
		if *cells < 1 || *cells > 50000 {
			log.Error("demo -cells out of range (1..50000)", "cells", *cells)
			return 1
		}
		if err := runDemo(context.Background(), dbPath, *cells); err != nil {
			log.Error("demo", "err", err)
			return 1
		}
		log.Info("hexxladb", "event", "demo_ok", "path", dbPath, "cells", *cells)
		return 0
	}

	// Database + hexagonal wiring: DB → outbound adapter → app service
	var svc *app.Service
	if dbPath != "" {
		db, err := hexxladb.Open(dbPath, nil)
		if err != nil {
			log.Error("hexxladb open", "path", dbPath, "err", err)
			return 1
		}
		defer func() { _ = db.Close() }()
		log.Info("hexxladb", "event", "db_opened", "path", dbPath)

		storage := hexxladbout.NewStorage(db)
		svc = app.NewWithStorage(storage)

		if err := runStorageSmoke(context.Background(), storage); err != nil {
			log.Error("storage_smoke", "err", err)
			return 1
		}
		log.Info("hexxladb", "event", "storage_smoke_ok")
	} else {
		svc = app.New()
	}

	_ = svc
	if dbPath != "" {
		log.Info("hexxladb", "event", "ready", "path", dbPath, "storage_wired", true)
	} else {
		log.Info("hexxladb", "event", "ready", "storage_wired", false, "hint", "set HEXXLA_DB_PATH or use -path / -demo (see README)")
	}

	return 0
}

// runDemo exercises public PutCell, secondary scan, GetCell, Close, Open — orchestration only.
func runDemo(ctx context.Context, path string, n int) error {
	nq := 80
	nr := (n + nq - 1) / nq
	if nq*nr < n {
		return fmt.Errorf("internal grid too small nq=%d nr=%d n=%d", nq, nr, n)
	}

	db, err := hexxladb.Open(path, nil)
	if err != nil {
		return err
	}

	vf := int64(5) * index.WeekNanos
	src := "cmd-hexxladb-demo"

	const batch = 200
	for base := 0; base < n; base += batch {
		end := min(base+batch, n)
		err = db.Update(func(tx *hexxladb.Tx) error {
			for i := base; i < end; i++ {
				q := i % nq
				r := i / nq
				p, err := lattice.Pack(lattice.Coord{Q: q, R: r})
				if err != nil {
					return err
				}
				rec := record.CellRecord{
					Key:        p,
					RawContent: fmt.Sprintf("demo-%d", i),
					Provenance: record.ProvenanceWire{
						SourceID:   src,
						Confidence: 1,
						CreatedAt:  int64(i),
						UpdatedAt:  int64(i),
					},
					Validity: record.ValidityWire{ValidFrom: &vf},
				}
				if err := tx.PutCell(context.Background(), rec); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			_ = db.Close()
			return err
		}
	}

	var bySource int
	err = db.View(func(tx *hexxladb.Tx) error {
		return tx.AscendCellsBySource(ctx, src, func(record.CellRecord) bool {
			bySource++
			return true
		})
	})
	if err != nil {
		_ = db.Close()
		return err
	}
	if bySource != n {
		_ = db.Close()
		return fmt.Errorf("AscendCellsBySource: want %d got %d", n, bySource)
	}

	p0, err := lattice.Pack(lattice.Coord{Q: 0, R: 0})
	if err != nil {
		_ = db.Close()
		return err
	}
	err = db.View(func(tx *hexxladb.Tx) error {
		got, ok, err := tx.GetCell(p0)
		if err != nil {
			return err
		}
		if !ok || got.RawContent != "demo-0" {
			return fmt.Errorf("GetCell(0,0): ok=%v raw=%q", ok, got.RawContent)
		}
		return nil
	})
	if err != nil {
		_ = db.Close()
		return err
	}

	if err := db.Close(); err != nil {
		return err
	}

	db2, err := hexxladb.Open(path, nil)
	if err != nil {
		return err
	}
	defer func() { _ = db2.Close() }()

	err = db2.View(func(tx *hexxladb.Tx) error {
		got, ok, err := tx.GetCell(p0)
		if err != nil {
			return err
		}
		if !ok || got.RawContent != "demo-0" {
			return fmt.Errorf("after reopen GetCell: ok=%v raw=%q", ok, got.RawContent)
		}
		return nil
	})
	return err
}

// runStorageSmoke writes and reads a minimal cell via the domain port (end-to-end through package hexxladb).
func runStorageSmoke(ctx context.Context, st domain.Storage) error {
	key, err := lattice.Pack(lattice.Coord{Q: 0, R: 0})
	if err != nil {
		return err
	}
	rec := record.CellRecord{
		Key:        key,
		RawContent: "hexxladb-cli-smoke",
		Provenance: record.ProvenanceWire{SourceID: "cmd/hexxladb", Confidence: 1, CreatedAt: 1, UpdatedAt: 1},
		Validity:   record.ValidityWire{},
	}
	if err := st.PutCell(ctx, rec); err != nil {
		return err
	}
	got, ok, err := st.GetCell(ctx, key)
	if err != nil {
		return err
	}
	if !ok || got.RawContent != rec.RawContent {
		return fmt.Errorf("get cell: ok=%v raw_content=%q", ok, got.RawContent)
	}
	return nil
}
