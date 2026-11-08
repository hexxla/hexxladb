package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/hexxla/hexxladb/internal/cmdconfig"
)

func cmdPrune(args []string) int {
	fs := flag.NewFlagSet("prune", flag.ContinueOnError)
	before := fs.Uint64("before-seq", 0, "historical cutoff (required)")
	limit := fs.Int("max-delete", 2048, "maximum number of cell versions to remove")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: hexxladb prune --before-seq <sequence> [--max-delete <count>] <database>")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 || *before == 0 || *limit < 1 {
		fs.Usage()
		return 2
	}
	db, err := cmdconfig.OpenExisting(fs.Arg(0))
	if err != nil {
		fmt.Fprintf(os.Stderr, "hexxladb prune: open: %v\n", err)
		return 1
	}
	defer func() { _ = db.Close() }()

	if _, err := db.OldestSnapshotSeq(); err != nil {
		fmt.Fprintf(os.Stderr, "hexxladb prune: %v\n", err)
		return 1
	}
	deleted, err := db.PruneCellVersions(*before, *limit)
	if err != nil {
		fmt.Fprintf(os.Stderr, "hexxladb prune: after %d confirmed deletions: %v\n", deleted, err)
		return 1
	}
	oldest, err := db.OldestSnapshotSeq()
	if err != nil {
		fmt.Fprintf(os.Stderr, "hexxladb prune: retention: %v\n", err)
		return 1
	}
	fmt.Printf("Versions removed: %d\nOldest snapshot:  %d\n", deleted, oldest)
	return 0
}

func cmdReclaimTail(args []string) int {
	fs := flag.NewFlagSet("reclaim-tail", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: hexxladb reclaim-tail <database>")
		fmt.Fprintln(os.Stderr, "Truncate the free file suffix on authenticated format v3.")
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return 2
	}
	db, err := cmdconfig.OpenExisting(fs.Arg(0))
	if err != nil {
		fmt.Fprintf(os.Stderr, "hexxladb reclaim-tail: open: %v\n", err)
		return 1
	}
	defer func() { _ = db.Close() }()

	reclaimed, err := db.ReclaimTail()
	if err != nil {
		fmt.Fprintf(os.Stderr, "hexxladb reclaim-tail: %v\n", err)
		return 1
	}
	fmt.Printf("Bytes reclaimed: %d\n", reclaimed)
	return 0
}
