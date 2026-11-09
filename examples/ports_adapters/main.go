package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/hexxla/hexxladb"
	hexxladbout "github.com/hexxla/hexxladb/examples/ports_adapters/internal/adapters/out/hexxladb"
	"github.com/hexxla/hexxladb/examples/ports_adapters/internal/app"
	"github.com/hexxla/hexxladb/examples/ports_adapters/internal/domain"
)

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context) (err error) {
	dir, err := os.MkdirTemp("", "hexxladb-contradiction-")
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, os.RemoveAll(dir)) }()
	db, err := hexxladb.Open(filepath.Join(dir, "example.db"), &hexxladb.Options{})
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, db.Close()) }()

	service := app.New(hexxladbout.New(db))
	left := domain.Observation{Text: "The valve is open."}
	right := domain.Observation{At: domain.Position{Q: 1}, Text: "The valve is closed."}
	id, err := service.Record(ctx, left, right, "Conflicting inspections.")
	if err != nil {
		return err
	}
	if err := service.Resolve(ctx, id, "Verified the valve is closed."); err != nil {
		return err
	}
	fmt.Printf("Recorded and resolved contradiction %s\n", id)
	return nil
}
