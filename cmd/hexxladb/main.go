// hexxladb — operator CLI for HexxlaDB database files.
//
// Usage: hexxladb <command> [flags] <database>
//
// Commands:
//
//	info     Print header info (page size, format, commit seq, file size)
//	check    Run a full integrity check and print the health report
//	compact  Copy-compact a database to a new file
//	stats    Print MVCC statistics (versioned rows, logical cells, wasted bytes)
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
  info      Print header info: page size, format version, commit seq, file size
  check     Run full integrity check; exits non-zero if errors are found
  compact   Copy-compact src to dest: hexxladb compact -o <dest> <src>
  stats     Print MVCC statistics: versioned rows, logical cells, wasted bytes
  keys      Dump raw B+ tree keys (optional -prefix filter)
  cells     List visible cells: coord, source, tags, confidence
  get       Print a single cell by coordinate: hexxladb get -q <Q> -r <R> <db>

  help      Print this help

Run 'hexxladb <command> -h' for command-specific flags.
`)
}
