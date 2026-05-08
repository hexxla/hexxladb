package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/hexxla/hexxladb"
)

func cmdCells(args []string) int {
	fs := flag.NewFlagSet("cells", flag.ContinueOnError)
	limit := fs.Int("n", 50, "Maximum number of cells to print (0 = all)")
	tag := fs.String("tag", "", "Filter: only cells with this tag")
	source := fs.String("source", "", "Filter: only cells from this source ID")
	query := fs.String("q", "", "Filter: cells matching this query string")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: hexxladb cells [flags] <database>")
		fmt.Fprintln(os.Stderr, "\nList visible cells with coord, source, tags, and confidence.")
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
		fmt.Fprintf(os.Stderr, "hexxladb cells: open: %v\n", err)
		return 1
	}
	defer func() { _ = db.Close() }()

	var requireTags []string
	if *tag != "" {
		requireTags = []string{*tag}
	}

	maxResults := *limit
	if maxResults == 0 {
		maxResults = 1<<31 - 1
	}

	q := hexxladb.CellQuery{
		Query:       *query,
		RequireTags: requireTags,
		SourceID:    *source,
		MaxResults:  maxResults,
	}

	viewErr := db.View(func(tx *hexxladb.Tx) error {
		results, err := tx.QueryCells(context.Background(), q)
		if err != nil {
			return err
		}
		for _, r := range results {
			c := r.Cell
			tags := strings.Join(c.Tags, ", ")
			fmt.Printf("(%d,%d)  src=%-20s  conf=%.2f  tags=[%s]\n",
				c.Coord.Q, c.Coord.R, truncStr(c.Provenance.SourceID, 20), c.Provenance.Confidence, tags)
			if c.RawContent != "" {
				fmt.Printf("        %s\n", truncStr(c.RawContent, 80))
			}
		}
		fmt.Fprintf(os.Stderr, "(%d cells)\n", len(results))
		return nil
	})
	if viewErr != nil {
		fmt.Fprintf(os.Stderr, "hexxladb cells: %v\n", viewErr)
		return 1
	}
	return 0
}

func truncStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
