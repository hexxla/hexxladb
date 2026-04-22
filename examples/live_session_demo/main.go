// Live session demo: seed a HexxlaDB file with realistic LLM-session-shaped cells
// (user / assistant / tool roles, provenance, validity, tags), optional facets, edges,
// and a contradiction seam — then read back via the same primitives a product would use.
//
// Run from repo root (creates ./.tmp/live_session.db by default; not system /tmp):
//
//	go run ./examples/live_session_demo
//
// Run checks only (requires existing DB):
//
//	go run ./examples/live_session_demo -verify-only
//
// Flags: -turns N (0=all scripted rows), -report, -simulate (HEXXLA effectiveness ladder),
// -mvcc, -keep, -path (default ./.tmp/live_session.db).
package main

import (
	"context"
	"crypto/rand"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/hexxla/hexxladb"
	"github.com/hexxla/hexxladb/internal/index"
	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
)

func main() {
	log.SetFlags(0)
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	path := flag.String("path", "", "database file (default: .tmp/live_session.db under cwd)")
	turns := flag.Int("turns", 0, "simulated conversation turns (caps sessionScript; 0 = full script)")
	mvcc := flag.Bool("mvcc", false, "create format v2 DB with MVCC (new file only)")
	verifyOnly := flag.Bool("verify-only", false, "skip seeding; run read checks only")
	noCleanup := flag.Bool("keep", false, "do not delete DB at path before seed (default deletes when path was auto-set or you want fresh run)")
	report := flag.Bool("report", true, "after verify, print query results for live demos (-report=false for quiet)")
	simulate := flag.Bool("simulate", true, "print HEXXLA effectiveness simulation (budget ladder, recall, timings; -simulate=false to skip)")
	flag.Parse()

	p := *path
	if p == "" {
		p = filepath.Join(".tmp", "live_session.db")
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		return err
	}
	if !*verifyOnly && !*noCleanup {
		_ = os.Remove(p)
		_ = os.Remove(p + "-wal")
	}

	opts := (*hexxladb.Options)(nil)
	if *mvcc {
		opts = &hexxladb.Options{
			EnableMVCC: true,
			MVCCRetention: hexxladb.MVCCRetention{
				RetainCommitsBehindHead: 256,
			},
		}
	}

	db, err := hexxladb.Open(p, opts)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	sessionStart := time.Now().UTC().Truncate(time.Minute)

	nt := *turns
	if nt <= 0 || nt > len(sessionScript) {
		nt = len(sessionScript)
	}

	if !*verifyOnly {
		if err := seedSession(ctx, db, sessionStart, nt); err != nil {
			return err
		}
		fmt.Printf("Seeded %q (%d turns + facet/edges/seam).\n", p, nt)
	}

	if err := verifySession(ctx, db, sessionStart, nt); err != nil {
		return err
	}
	fmt.Println("Read checks OK — session-shaped data round-trips as expected.")

	_, loadR, seamScanR, err := sessionGeometry(nt)
	if err != nil {
		return err
	}
	if *report {
		if err := printDemoReport(ctx, db, p, nt); err != nil {
			return err
		}
	}
	if *simulate {
		if err := simulateHexxlaService(ctx, db, lattice.Coord{Q: 0, R: 0}, loadR, seamScanR, nt); err != nil {
			return err
		}
	}
	return nil
}

func turnCoordinates(n int) ([]lattice.Coord, error) {
	if n < 1 {
		return nil, fmt.Errorf("turns must be >= 1")
	}
	center := lattice.Coord{Q: 0, R: 0}
	for maxR := 1; maxR <= 256; maxR++ {
		dst := hexxladb.WalkRings(nil, center, maxR)
		if len(dst) >= n {
			return dst[:n], nil
		}
	}
	return nil, fmt.Errorf("ring span exceeded for %d coords", n)
}

// sessionGeometry returns walk coords for seam endpoints and radii for neighborhood reads.
func sessionGeometry(n int) (coords []lattice.Coord, loadR, seamScanR int, err error) {
	coords, err = turnCoordinates(n + 2)
	if err != nil {
		return nil, 0, 0, err
	}
	center := lattice.Coord{Q: 0, R: 0}
	for i := range n {
		if i >= len(coords) {
			break
		}
		d := center.Distance(coords[i])
		if d > loadR {
			loadR = d
		}
	}
	seamScanR = loadR
	for _, idx := range []int{n, n + 1} {
		if idx < len(coords) {
			d := center.Distance(coords[idx])
			if d > seamScanR {
				seamScanR = d
			}
		}
	}
	return coords, loadR, seamScanR, nil
}

func unixNanoAt(base time.Time, step int) int64 {
	return base.Add(time.Duration(step) * time.Second).UnixNano()
}

// seedSession writes one cell per scripted turn (separate Update per turn ≈ discrete commits).
// Then adds a derived facet, ring edges, and a seam between two contradictory beliefs.
func seedSession(ctx context.Context, db *hexxladb.DB, sessionStart time.Time, turns int) error {
	n := min(turns, len(sessionScript))
	coords, err := turnCoordinates(n + 2) // extra coords for contradiction pair + seam endpoints
	if err != nil {
		return err
	}

	for i := range n {
		t := unixNanoAt(sessionStart, i+1)
		vf := t
		row := sessionScript[i]
		rec := record.CellRecord{
			Key:        mustPack(coords[i]),
			RawContent: row.rawContent(),
			Provenance: provenanceForRole(row.SourceID, t),
			Validity:   record.ValidityWire{ValidFrom: &vf},
			Tags:       row.Tags,
		}
		if err := db.Update(func(tx *hexxladb.Tx) error {
			return tx.PutCell(ctx, rec)
		}); err != nil {
			return fmt.Errorf("turn %d PutCell: %w", i, err)
		}
	}

	// Contradiction cells (explicit seam demo): Aurora vs Nova at dedicated coords.
	tA := unixNanoAt(sessionStart, n+10)
	tB := unixNanoAt(sessionStart, n+11)
	cellA := record.CellRecord{
		Key:        mustPack(coords[n]),
		RawContent: "Fact store: project codename is Aurora.",
		Provenance: provenanceForRole(sourceUser, tA),
		Validity:   record.ValidityWire{ValidFrom: &tA},
		Tags:       []string{tagRoleUser, "fact/codename"},
	}
	cellB := record.CellRecord{
		Key:        mustPack(coords[n+1]),
		RawContent: "Fact store: project codename is Nova (rename approved).",
		Provenance: provenanceForRole(sourceUser, tB),
		Validity:   record.ValidityWire{ValidFrom: &tB},
		Tags:       []string{tagRoleUser, "fact/codename"},
	}
	if err := db.Update(func(tx *hexxladb.Tx) error {
		if err := tx.PutCell(ctx, cellA); err != nil {
			return err
		}
		return tx.PutCell(ctx, cellB)
	}); err != nil {
		return err
	}

	// Facet on session center: derived summary (hash-bound to raw).
	centerPK := mustPack(lattice.Coord{Q: 0, R: 0})
	rawCenter, _, err := cellRawAt(db, centerPK)
	if err != nil {
		return err
	}
	hash := record.HashRawContent(rawCenter)
	facet := record.FacetRecord{
		Key:            centerPK,
		FacetID:        0,
		DerivedContent: "Rolling facet: prefs + security + incidents + quota seam risk + HEX-442 + freeze + Nova rename + observability deltas.",
		LastRotated:    time.Now().UnixNano(),
		DerivationHash: hash,
	}

	sid := ulid.MustNew(ulid.Timestamp(time.Now()), rand.Reader).String()
	seam := record.SeamRecord{
		ID: sid, CellA: cellA.Key, CellB: cellB.Key,
		SeamType: "fact_drift", Reason: "codename Aurora vs Nova",
		ConfidenceDelta: 0.15, DetectedAt: time.Now().UnixNano(),
		Provenance: record.ProvenanceWire{
			SourceID: sourceAssistant, Confidence: 0.9,
			CreatedAt: time.Now().UnixNano(), UpdatedAt: time.Now().UnixNano(),
		},
	}

	if err := db.Update(func(tx *hexxladb.Tx) error {
		if err := tx.PutFacet(facet); err != nil {
			return err
		}
		for i := range n - 1 {
			fromC := coords[i]
			toC := coords[i+1]
			if err := tx.LinkCells(fromC, toC, "conversation_turn", 1.0,
				record.ProvenanceWire{SourceID: "session/graph", Confidence: 1, CreatedAt: time.Now().UnixNano(), UpdatedAt: time.Now().UnixNano()},
			); err != nil {
				return err
			}
		}
		return tx.PutSeam(ctx, seam)
	}); err != nil {
		return err
	}

	return nil
}

func mustPack(c lattice.Coord) lattice.PackedCoord {
	p, err := lattice.Pack(c)
	if err != nil {
		panic(err)
	}
	return p
}

func cellRawAt(db *hexxladb.DB, pk lattice.PackedCoord) (raw []byte, ok bool, err error) {
	err = db.View(func(tx *hexxladb.Tx) error {
		rec, o, e := tx.GetCell(pk)
		if e != nil || !o {
			ok = o
			return e
		}
		ok = true
		raw = []byte(rec.RawContent)
		return nil
	})
	return raw, ok, err
}

func verifySession(ctx context.Context, db *hexxladb.DB, sessionStart time.Time, turns int) error {
	n := min(turns, len(sessionScript))
	coords, loadR, seamScanR, err := sessionGeometry(n)
	if err != nil {
		return err
	}
	center := lattice.Coord{Q: 0, R: 0}

	wantBySource := map[string]int{}
	for i := range n {
		id := sessionScript[i].SourceID
		wantBySource[id]++
	}
	wantBySource[sourceUser] += 2 // contradiction pair uses session/user provenance

	for src, want := range wantBySource {
		var got int
		err = db.View(func(tx *hexxladb.Tx) error {
			return tx.AscendCellsBySource(ctx, src, func(record.CellRecord) bool {
				got++
				return true
			})
		})
		if err != nil {
			return err
		}
		if got != want {
			return fmt.Errorf("AscendCellsBySource(%q): want %d got %d", src, want, got)
		}
	}

	wantPrefs := 0
	for i := range n {
		if slices.Contains(sessionScript[i].Tags, tagTopicPrefs) {
			wantPrefs++
		}
	}
	var tagPrefs int
	err = db.View(func(tx *hexxladb.Tx) error {
		return tx.AscendCellsByTag(ctx, tagTopicPrefs, func(record.CellRecord) bool {
			tagPrefs++
			return true
		})
	})
	if err != nil {
		return err
	}
	if tagPrefs != wantPrefs {
		return fmt.Errorf("AscendCellsByTag(%q): want %d got %d", tagTopicPrefs, wantPrefs, tagPrefs)
	}

	maxCellsWire := max(n+100, 800)
	var cells []record.CellRecord
	err = db.View(func(tx *hexxladb.Tx) error {
		var e error
		cells, e = tx.LoadContext(ctx, center, loadR, maxCellsWire)
		return e
	})
	if err != nil {
		return err
	}
	if len(cells) < n {
		return fmt.Errorf("LoadContext: want at least %d cells in neighborhood got %d (maxR=%d maxCells=%d)", n, len(cells), loadR, maxCellsWire)
	}

	candidateCap := max(512, n+128)
	var pack hexxladb.ContextPack
	err = db.View(func(tx *hexxladb.Tx) error {
		cfg := hexxladb.LoadContextBudgetConfig{
			Assemble:          hexxladb.DefaultAssembleCellViewOpts(),
			MaxCandidateCells: candidateCap,
			IncludeFacetText:  true,
			IncludeSeams:      true,
			SeamRadius:        seamScanR,
		}
		var e error
		pack, e = tx.LoadContextPack(ctx, hexxladb.Coord(center), loadR, 120000, hexxladb.ByteLenBudgeter{}, cfg)
		return e
	})
	if err != nil {
		return err
	}
	if len(pack.Cells) == 0 {
		return fmt.Errorf("LoadContextPack: expected non-empty CellView slice")
	}
	if len(pack.Seams) < 1 {
		return fmt.Errorf("LoadContextPack seams: want >=1 got %d", len(pack.Seams))
	}

	// Time bucket scan (same week as sessionStart validity)
	vf := unixNanoAt(sessionStart, 1)
	vw := record.ValidityWire{ValidFrom: &vf}
	bucket, ok := index.WeekBucketFromValidity(vw)
	if !ok {
		return fmt.Errorf("WeekBucketFromValidity")
	}
	var inBucket int
	err = db.View(func(tx *hexxladb.Tx) error {
		return tx.AscendCellsInTimeBucket(ctx, bucket, func(record.CellRecord) bool {
			inBucket++
			return true
		})
	})
	if err != nil {
		return err
	}
	totalCells := n + 2 // script + contradiction pair
	if inBucket < totalCells {
		return fmt.Errorf("AscendCellsInTimeBucket(%d): want >= %d got %d", bucket, totalCells, inBucket)
	}

	var hasFacet bool
	err = db.View(func(tx *hexxladb.Tx) error {
		var e error
		_, hasFacet, e = tx.GetFacet(mustPack(center), 0)
		return e
	})
	if err != nil {
		return err
	}
	if !hasFacet {
		return fmt.Errorf("GetFacet(center,0): expected facet")
	}

	var seams []record.SeamRecord
	err = db.View(func(tx *hexxladb.Tx) error {
		var e error
		seams, e = tx.FindSeams(ctx, center, seamScanR, false)
		return e
	})
	if err != nil {
		return err
	}
	if len(seams) < 1 {
		return fmt.Errorf("FindSeams: want >=1 seam, got %d (radius %d)", len(seams), seamScanR)
	}
	_ = coords

	maxAt := max(n, 120)
	asOf := time.Unix(0, unixNanoAt(sessionStart, n)).UTC()
	var atCells []record.CellRecord
	err = db.View(func(tx *hexxladb.Tx) error {
		var e error
		atCells, e = tx.LoadContextAt(ctx, center, loadR, maxAt, asOf)
		return e
	})
	if err != nil {
		return err
	}
	if len(atCells) == 0 {
		return fmt.Errorf("LoadContextAt: expected some cells at asOf")
	}

	_ = db.ViewAtTime(sessionStart, func(tx *hexxladb.Tx) error {
		_, err := tx.LoadContext(ctx, center, min(loadR, 4), min(maxCellsWire, 64))
		return err
	})

	return nil
}

func preview(s string, limit int) string {
	if limit <= 0 || len(s) <= limit {
		return s
	}
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit]) + "…"
}

// printDemoReport runs the same style of reads a product would use and prints
// human-readable output for live demos (tag scan, context pack, facet).
func printDemoReport(ctx context.Context, db *hexxladb.DB, dbPath string, nTurns int) error {
	center := lattice.Coord{Q: 0, R: 0}
	fmt.Println()
	fmt.Println("=== Live demo — query report ===")
	fmt.Printf("Database file: %s\n", dbPath)

	for _, src := range allSourceIDs() {
		var n int
		err := db.View(func(tx *hexxladb.Tx) error {
			return tx.AscendCellsBySource(ctx, src, func(record.CellRecord) bool {
				n++
				return true
			})
		})
		if err != nil {
			return err
		}
		fmt.Printf("AscendCellsBySource(%q): %d cells\n", src, n)
	}

	fmt.Printf("\nAscendCellsByTag(%q) (snippets):\n", tagTopicPrefs)
	err := db.View(func(tx *hexxladb.Tx) error {
		return tx.AscendCellsByTag(ctx, tagTopicPrefs, func(cr record.CellRecord) bool {
			fmt.Printf("  [%s] %s\n", cr.Provenance.SourceID, preview(cr.RawContent, 160))
			return true
		})
	})
	if err != nil {
		return err
	}

	_, loadR, seamR, geomErr := sessionGeometry(nTurns)
	if geomErr != nil {
		return geomErr
	}
	candidateCap := max(512, nTurns+128)

	var pack hexxladb.ContextPack
	err = db.View(func(tx *hexxladb.Tx) error {
		cfg := hexxladb.LoadContextBudgetConfig{
			Assemble:          hexxladb.DefaultAssembleCellViewOpts(),
			MaxCandidateCells: candidateCap,
			IncludeFacetText:  true,
			IncludeSeams:      true,
			SeamRadius:        seamR,
		}
		var e error
		pack, e = tx.LoadContextPack(ctx, hexxladb.Coord(center), loadR, 120000, hexxladb.ByteLenBudgeter{}, cfg)
		return e
	})
	if err != nil {
		return err
	}
	fmt.Printf("\nLoadContextPack(center=(0,0), maxR=%d, budget=120000 bytes): %d cell views, TotalTokens=%d (byte tally), %d seams\n",
		loadR, len(pack.Cells), pack.TotalTokens, len(pack.Seams))
	for i := range pack.Seams {
		s := pack.Seams[i]
		fmt.Printf("  seam %s: %s (confidence Δ %.2f)\n", s.SeamType, preview(s.Reason, 120), s.ConfidenceDelta)
	}

	var fr record.FacetRecord
	var hasFacet bool
	err = db.View(func(tx *hexxladb.Tx) error {
		var e error
		fr, hasFacet, e = tx.GetFacet(mustPack(center), 0)
		return e
	})
	if err != nil {
		return err
	}
	if hasFacet {
		fmt.Printf("\nGetFacet(center, id=0): %s\n", preview(fr.DerivedContent, 220))
		fmt.Printf("  derivation hash prefix: %x…\n", fr.DerivationHash[:8])
	}

	fmt.Println("=== End query report ===")
	fmt.Println()
	return nil
}
