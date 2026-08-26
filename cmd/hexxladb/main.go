// hexxladb — operator CLI for HexxlaDB database files.
//
// Usage: hexxladb <command> [flags] <database>
//
// Commands:
//
//	info     Print database info (page size, value limit, commit seq, file size)
//	check    Run a full integrity check and print the health report
//	compact  Copy-compact a database to a new file
//	migrate-v1-to-v2       Upgrade a closed v1 database into a distinct v2 file
//	migrate-to-authenticated  Upgrade a closed v1/v2 database into an encrypted v3 file
//	stats    Print MVCC and reclaimable-storage statistics
//	keys     Dump raw B+ tree keys (optional prefix filter)
//	cells    List visible cells (coord, source, tags, confidence)
//	get      Print a single cell by hex-lattice coordinate (Q,R)
package main

import (
	"fmt"
	"os"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		printUsage()
		return 0
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "info":
		return cmdInfo(rest)
	case "check":
		return cmdCheck(rest)
	case "compact":
		return cmdCompact(rest)
	case "migrate-v1-to-v2":
		return cmdMigrateV1ToV2(rest)
	case "migrate-to-authenticated":
		return cmdMigrateToAuthenticated(rest)
	case "stats":
		return cmdStats(rest)
	case "keys":
		return cmdKeys(rest)
	case "cells":
		return cmdCells(rest)
	case "get":
		return cmdGet(rest)
	case "help", "-h", "--help":
		printUsage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "hexxladb: unknown command %q\n\nRun 'hexxladb help' for usage.\n", cmd)
		return 2
	}
}

func printUsage() {
	fmt.Print(`hexxladb — operator CLI for HexxlaDB database files

Usage:
  hexxladb <command> [flags] <database>

Commands:
  info      Print database info: page size, value limit, commit seq, file size
  check     Run full integrity check; exits non-zero if errors are found
  compact   Copy-compact src to dest: hexxladb compact -o <dest> <src>
  migrate-v1-to-v2
            Migrate v1 src to a distinct v2 dest: hexxladb migrate-v1-to-v2 -o <dest> <src>
  migrate-to-authenticated
            Migrate v1/v2 src to encrypted v3: hexxladb migrate-to-authenticated -o <dest> <src>
  stats     Print MVCC and reclaimable-storage statistics
  keys      Dump raw B+ tree keys (optional -prefix filter)
  cells     List visible cells: coord, source, tags, confidence
  get       Print a single cell by coordinate: hexxladb get -q <Q> -r <R> <db>

  help      Print this help

Run 'hexxladb <command> -h' for command-specific flags.
`)
}
