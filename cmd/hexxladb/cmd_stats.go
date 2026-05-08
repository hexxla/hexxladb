package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/hexxla/hexxladb"
)

func cmdStats(args []string) int {
	fs := flag.NewFlagSet("stats", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: hexxladb stats <database>")
		fmt.Fprintln(os.Stderr, "\nPrint MVCC statistics: versioned rows, logical cells, commit seq, wasted bytes.")
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
		fmt.Fprintf(os.Stderr, "hexxladb stats: open: %v\n", err)
		return 1
	}
	defer func() { _ = db.Close() }()

	stats, err := db.StatsMVCC()
	if err != nil {
		fmt.Fprintf(os.Stderr, "hexxladb stats: %v\n", err)
		return 1
	}

	fmt.Printf("Commit seq:     %d\n", stats.CommitSeq)
	fmt.Printf("Versioned rows: %d\n", stats.VersionedRows)
	fmt.Printf("Logical cells:  %d\n", stats.LogicalCells)
	fmt.Printf("Wasted bytes:   %s", humanBytes(int64(stats.WastedBytes))) //nolint:gosec // display only
	if stats.WastedBytes > 0 {
		fmt.Printf("  (compact recommended)")
	}
	fmt.Println()
	return 0
}
