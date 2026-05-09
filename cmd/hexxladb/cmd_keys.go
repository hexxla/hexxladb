package main

import (
	"flag"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/hexxla/hexxladb"
)

func cmdKeys(args []string) int {
	fs := flag.NewFlagSet("keys", flag.ContinueOnError)
	prefix := fs.String("prefix", "", "Only show keys with this prefix (e.g. cell/, tag/, source/)")
	limit := fs.Int("n", 0, "Stop after N keys (0 = all)")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: hexxladb keys [flags] <database>")
		fmt.Fprintln(os.Stderr, "\nDump raw B+ tree keys. Useful for debugging index structure.")
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
		fmt.Fprintf(os.Stderr, "hexxladb keys: open: %v\n", err)
		return 1
	}
	defer func() { _ = db.Close() }()

	count := 0
	scanErr := db.View(func(tx *hexxladb.Tx) error {
		from := []byte(*prefix)
		var to []byte
		if *prefix != "" {
			// Upper bound: increment the last byte of the prefix so we stay within the prefix.
			to = prefixUpperBound(*prefix)
		}
		return tx.AscendRange(from, to, func(k, _ []byte) bool {
			if *limit > 0 && count >= *limit {
				return false
			}
			fmt.Printf("%s\n", sanitizeKey(k))
			count++
			return true
		})
	})
	if scanErr != nil {
		fmt.Fprintf(os.Stderr, "hexxladb keys: scan: %v\n", scanErr)
		return 1
	}
	fmt.Fprintf(os.Stderr, "(%d keys)\n", count)
	return 0
}

// prefixUpperBound returns the lexicographic successor of prefix for range scans.
func prefixUpperBound(prefix string) []byte {
	b := []byte(prefix)
	for i := range slices.Backward(b) {
		if b[i] < 0xff {
			b[i]++
			return b[:i+1]
		}
	}
	return nil // overflow: no upper bound
}

// sanitizeKey replaces non-printable bytes with dots for safe terminal output.
func sanitizeKey(k []byte) string {
	var sb strings.Builder
	sb.Grow(len(k))
	for _, b := range k {
		if b >= 0x20 && b < 0x7f {
			sb.WriteByte(b)
		} else {
			sb.WriteByte('.')
		}
	}
	return sb.String()
}
