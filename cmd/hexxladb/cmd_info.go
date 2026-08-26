package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/hexxla/hexxladb"
)

func cmdInfo(args []string) int {
	fs := flag.NewFlagSet("info", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: hexxladb info <database>")
		fmt.Fprintln(os.Stderr, "\nPrint database information: page size, value limit, commit seq, file size, and reclaimable storage.")
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return 2
	}
	dbPath := fs.Arg(0)

	fi, err := os.Stat(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "hexxladb info: %v\n", err)
		return 1
	}

	db, err := hexxladb.Open(dbPath, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "hexxladb info: open: %v\n", err)
		return 1
	}
	defer func() { _ = db.Close() }()

	stats, err := db.StatsMVCC()
	if err != nil {
		fmt.Fprintf(os.Stderr, "hexxladb info: stats: %v\n", err)
		return 1
	}
	storage, err := db.StorageStats()
	if err != nil {
		fmt.Fprintf(os.Stderr, "hexxladb info: storage: %v\n", err)
		return 1
	}

	fmt.Printf("Path:           %s\n", dbPath)
	fmt.Printf("Size:           %s (%d bytes)\n", humanBytes(fi.Size()), fi.Size())
	fmt.Printf("Page size:      %d bytes\n", db.PageSize())
	fmt.Printf("Max value:      %d bytes\n", db.MaxValueBytes())
	fmt.Printf("Commit seq:     %d\n", stats.CommitSeq)
	if stats.CommitSeq > 0 {
		fmt.Printf("Versioned rows: %d\n", stats.VersionedRows)
		fmt.Printf("Logical cells:  %d\n", stats.LogicalCells)
	}
	if storage.ReclaimableBytes > 0 {
		fmt.Printf("Reclaimable:    %s (compact recommended)\n", humanBytesUint(storage.ReclaimableBytes))
	}
	return 0
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.2f GiB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.2f MiB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.2f KiB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

func humanBytesUint(n uint64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.2f GiB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.2f MiB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.2f KiB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
