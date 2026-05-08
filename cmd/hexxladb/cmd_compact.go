package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/hexxla/hexxladb"
)

func cmdCompact(args []string) int {
	fs := flag.NewFlagSet("compact", flag.ContinueOnError)
	output := fs.String("o", "", "Destination database file (required)")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: hexxladb compact -o <dest> <src>")
		fmt.Fprintln(os.Stderr, "\nCopy-compact a database to a new file. The source is not modified.")
		fmt.Fprintln(os.Stderr, "All data including MVCC history is preserved. Reclaims freed overflow pages.")
		fmt.Fprintln(os.Stderr)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 || *output == "" {
		fs.Usage()
		return 2
	}
	srcPath := fs.Arg(0)
	dstPath := *output

	if _, err := os.Stat(srcPath); err != nil {
		fmt.Fprintf(os.Stderr, "hexxladb compact: source: %v\n", err)
		return 1
	}
	if _, err := os.Stat(dstPath); err == nil {
		fmt.Fprintf(os.Stderr, "hexxladb compact: destination already exists: %s\n", dstPath)
		return 1
	}

	srcFi, _ := os.Stat(srcPath)

	if err := hexxladb.CompactTo(context.Background(), srcPath, dstPath, nil); err != nil {
		fmt.Fprintf(os.Stderr, "hexxladb compact: %v\n", err)
		return 1
	}

	dstFi, err := os.Stat(dstPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "hexxladb compact: stat dest: %v\n", err)
		return 1
	}

	gain := float64(srcFi.Size()) / float64(max(1, dstFi.Size()))
	fmt.Printf("%s → %s  (%s → %s, gain=%.2fx)\n",
		srcPath, dstPath,
		humanBytes(srcFi.Size()), humanBytes(dstFi.Size()),
		gain,
	)
	return 0
}
