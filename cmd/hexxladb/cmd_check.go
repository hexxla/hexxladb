package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/hexxla/hexxladb"
)

func cmdCheck(args []string) int {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	noOrphans := fs.Bool("no-orphans", false, "Skip orphaned seam detection")
	noTagIndex := fs.Bool("no-tag-index", false, "Skip tag index consistency check")
	noSrcIndex := fs.Bool("no-source-index", false, "Skip source index consistency check")
	maxErrors := fs.Int("max-errors", 20, "Maximum index errors to report per check")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: hexxladb check [flags] <database>")
		fmt.Fprintln(os.Stderr, "\nRun a full integrity check. Exits 1 if errors are found.")
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
		fmt.Fprintf(os.Stderr, "hexxladb check: open: %v\n", err)
		return 1
	}
	defer func() { _ = db.Close() }()

	cfg := hexxladb.HealthCheckConfig{
		CheckOrphans:     !*noOrphans,
		CheckTagIndex:    !*noTagIndex,
		CheckSourceIndex: !*noSrcIndex,
		MaxErrors:        *maxErrors,
	}

	report, err := db.HealthCheck(context.Background(), cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "hexxladb check: %v\n", err)
		return 1
	}

	fmt.Printf("Cells:             %d\n", report.CellCount)
	fmt.Printf("Seams:             %d (%d resolved, %d unresolved)\n",
		report.SeamCount, report.SeamsResolved, report.SeamsUnresolved)
	fmt.Printf("Orphaned seams:    %d\n", len(report.OrphanedSeams))
	fmt.Printf("Tag index errors:  %d\n", report.TagIndexErrors)
	fmt.Printf("Source idx errors: %d\n", report.SourceIndexErrors)
	fmt.Printf("Versioned rows:    %d\n", report.MVCCStats.VersionedRows)
	fmt.Printf("Logical cells:     %d\n", report.MVCCStats.LogicalCells)
	if report.MVCCStats.WastedBytes > 0 {
		fmt.Printf("Wasted bytes:      %s (compact recommended)\n", humanBytes(int64(report.MVCCStats.WastedBytes))) //nolint:gosec // display only
	}
	for _, w := range report.Warnings {
		fmt.Printf("WARNING: %s\n", w)
	}

	hasErrors := report.TagIndexErrors > 0 || report.SourceIndexErrors > 0 || len(report.OrphanedSeams) > 0
	if hasErrors {
		fmt.Fprintln(os.Stderr, "\ncheck FAILED")
		return 1
	}
	fmt.Println("\ncheck OK")
	return 0
}
