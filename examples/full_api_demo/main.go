// Full API demo: seeds a live HexxlaDB file (see seed.go for bulk data + MVCC parameters) and walks
// every exported hexxladb primitive
// (cells, seams, facets, edges, indexes, views, MVCC, changelog, encryption rotation, …)
// with kid-friendly explanations. For the smaller session-shaped example, see
// examples/live_session_demo. Gap analysis vs this tour: docs/hexxladb/API_REFERENCE.md
// ("Live demo — what it does not call").
//
// Run from repo root:
//
//	go run ./examples/full_api_demo
//
// Quiet machine-readable-ish output:
//
//	go run ./examples/full_api_demo -eli5=false
//
// Custom working directory (default ./.tmp/full_api_demo):
//
//	go run ./examples/full_api_demo -dir /tmp/hexxla_tour
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
)

func main() {
	log.SetFlags(0)
	dir := flag.String("dir", filepath.Join(".tmp", "full_api_demo"), "directory for database files")
	eli5 := flag.Bool("eli5", true, "print 'explain like I'm 5' story text for each section")
	skipEnc := flag.Bool("skip-encryption", false, "skip encrypted DB + RotateEncryption (faster)")
	flag.Parse()

	if err := os.MkdirAll(*dir, 0o750); err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()
	if err := runTour(ctx, *dir, *eli5, *skipEnc); err != nil {
		log.Fatal(err)
	}
	fmt.Println()
	fmt.Println("Full API tour finished OK — see docs/hexxladb/API_REFERENCE.md for symbol inventory.")
}
