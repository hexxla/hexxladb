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
	"fmt"
	"log/slog"
	"os"

	"github.com/hexxla/hexxladb"
	hexxladbout "github.com/hexxla/hexxladb/internal/adapters/out/hexxladb"
	"github.com/hexxla/hexxladb/internal/app"
	"github.com/hexxla/hexxladb/internal/config"
	"github.com/hexxla/hexxladb/internal/domain"
	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
)

func main() {
	os.Exit(run())
}

func run() int {
	// 1. Config
	cfg, err := config.Load()
	if err != nil {
		slog.Error("config", "err", err)
		return 1
	}

	// 2. Logging (JSON to stdout; replace or wrap with your ops sink if needed)
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(log)

	// 3. Database + hexagonal wiring: DB → outbound adapter → app service
	var svc *app.Service
	if dbPath := os.Getenv("HEXXLA_DB_PATH"); dbPath != "" {
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
	if dbPath := os.Getenv("HEXXLA_DB_PATH"); dbPath != "" {
		log.Info("hexxladb", "event", "ready", "path", dbPath, "storage_wired", true)
	} else {
		log.Info("hexxladb", "event", "ready", "storage_wired", false, "hint", "set HEXXLA_DB_PATH to open DB and run PutCell/GetCell smoke test")
	}

	return 0
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
