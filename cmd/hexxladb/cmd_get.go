package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/hexxla/hexxladb"
)

func cmdGet(args []string) int {
	fs := flag.NewFlagSet("get", flag.ContinueOnError)
	q := fs.Int("q", 0, "Q axial coordinate")
	r := fs.Int("r", 0, "R axial coordinate")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: hexxladb get -q <Q> -r <R> <database>")
		fmt.Fprintln(os.Stderr, "\nPrint the visible cell at a hex coordinate.")
		fmt.Fprintln(os.Stderr)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return 2
	}
	dbPath := fs.Arg(0)

	db, err := hexxladb.Open(dbPath, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "hexxladb get: open: %v\n", err)
		return 1
	}
	defer func() { _ = db.Close() }()

	coord := hexxladb.Coord{Q: *q, R: *r}
	packed, err := hexxladb.Pack(coord)
	if err != nil {
		fmt.Fprintf(os.Stderr, "hexxladb get: invalid coordinate (%d,%d): %v\n", *q, *r, err)
		return 1
	}

	var found bool
	viewErr := db.View(func(tx *hexxladb.Tx) error {
		cell, ok, err := tx.GetCell(packed)
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
		found = true
		fmt.Printf("Coord:      (%d, %d)\n", *q, *r)
		fmt.Printf("Source:     %s\n", cell.Provenance.SourceID)
		fmt.Printf("Confidence: %.4f\n", cell.Provenance.Confidence)
		fmt.Printf("Tags:       [%s]\n", strings.Join(cell.Tags, ", "))
		if cell.RawContent != "" {
			fmt.Printf("Content:    %s\n", cell.RawContent)
		}
		if cell.Provenance.CreatedAt > 0 {
			fmt.Printf("Created:    %d (unix ns)\n", cell.Provenance.CreatedAt)
		}
		if cell.Provenance.UpdatedAt > 0 {
			fmt.Printf("Updated:    %d (unix ns)\n", cell.Provenance.UpdatedAt)
		}
		return nil
	})
	if viewErr != nil {
		fmt.Fprintf(os.Stderr, "hexxladb get: %v\n", viewErr)
		return 1
	}
	if !found {
		fmt.Fprintf(os.Stderr, "no visible cell at (%d, %d)\n", *q, *r)
		return 1
	}
	return 0
}
