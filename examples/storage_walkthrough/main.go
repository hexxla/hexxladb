// Storage walkthrough: exercises [domain.Storage] via outbound adapter, same
// surface a Hexxla-style service uses. Run from repo root:
//
//	go run ./examples/storage_walkthrough -path .tmp/walkthrough.db
//
// This is orchestration and I/O only — no business rules (see HEXAGONAL_ARCHITECTURE.md).
package main

import (
	"context"
	"crypto/rand"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/hexxla/hexxladb"
	hexxladbout "github.com/hexxla/hexxladb/internal/adapters/out/hexxladb"
	"github.com/hexxla/hexxladb/internal/index"
	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	path := flag.String("path", "", "database file (default: temp file under os.TempDir)")
	flag.Parse()

	p := *path
	if p == "" {
		p = filepath.Join(os.TempDir(), "hexxla-storage-walkthrough.db")
	}
	_ = os.Remove(p)

	db, err := hexxladb.Open(p, nil)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	st := hexxladbout.NewStorage(db)
	ctx := context.Background()

	c0 := lattice.Coord{Q: 0, R: 0}
	c1 := lattice.Coord{Q: 1, R: 0}
	p0, err := lattice.Pack(c0)
	if err != nil {
		return err
	}
	p1, err := lattice.Pack(c1)
	if err != nil {
		return err
	}

	vf := int64(2) * index.WeekNanos
	validity := record.ValidityWire{ValidFrom: &vf}
	src := "walkthrough"
	prov := record.ProvenanceWire{SourceID: src, Confidence: 1, CreatedAt: 1, UpdatedAt: 1}

	fmt.Println("PutCell (two adjacent cells)")
	for _, rec := range []record.CellRecord{
		{Key: p0, RawContent: "alpha", Provenance: prov, Validity: validity},
		{Key: p1, RawContent: "beta", Provenance: prov, Validity: validity},
	} {
		if err := st.PutCell(ctx, rec); err != nil {
			return err
		}
	}

	fmt.Println("GetCell")
	got, ok, err := st.GetCell(ctx, p0)
	if err != nil {
		return err
	}
	if !ok || got.RawContent != "alpha" {
		return errors.New("GetCell: unexpected result")
	}

	fmt.Println("AscendCellsBySource")
	var nCell int
	if err := st.AscendCellsBySource(ctx, src, func(record.CellRecord) bool {
		nCell++
		return true
	}); err != nil {
		return err
	}
	if nCell != 2 {
		return fmt.Errorf("AscendCellsBySource: want 2 got %d", nCell)
	}

	fmt.Println("AscendCellsInTimeBucket")
	bucket, ok := index.WeekBucketFromValidity(validity)
	if !ok {
		return errors.New("WeekBucketFromValidity")
	}
	var nTime int
	if err := st.AscendCellsInTimeBucket(ctx, bucket, func(record.CellRecord) bool {
		nTime++
		return true
	}); err != nil {
		return err
	}
	if nTime != 2 {
		return fmt.Errorf("AscendCellsInTimeBucket: want 2 got %d", nTime)
	}

	fmt.Println("WalkRing")
	var ringVisits int
	if err := st.WalkRing(ctx, c0, 1, func(lattice.Coord, []byte, bool) bool {
		ringVisits++
		return true
	}); err != nil {
		return err
	}
	if ringVisits != 6 {
		return fmt.Errorf("WalkRing: want 6 visits got %d", ringVisits)
	}

	asOf := time.Unix((vf+1)/1e9, (vf+1)%1e9).UTC()
	fmt.Println("WalkRingAt")
	var ringAt int
	if err := st.WalkRingAt(ctx, c0, 1, asOf, func(lattice.Coord, record.CellRecord) bool {
		ringAt++
		return true
	}); err != nil {
		return err
	}
	if ringAt == 0 {
		return errors.New("WalkRingAt: expected at least one cell")
	}

	fmt.Println("LoadContext")
	cells, err := st.LoadContext(ctx, c0, 2, 20)
	if err != nil {
		return err
	}
	if len(cells) < 1 {
		return errors.New("LoadContext: expected cells")
	}

	fmt.Println("LoadContextAt")
	cellsAt, err := st.LoadContextAt(ctx, c0, 2, 20, asOf)
	if err != nil {
		return err
	}
	if len(cellsAt) < 1 {
		return errors.New("LoadContextAt: expected cells")
	}

	fmt.Println("PutFacet / GetFacet / AscendFacetsForCell")
	facetRec := record.FacetRecord{
		Key: p0, FacetID: 0, DerivedContent: "facet-demo", LastRotated: 1,
	}
	if err := st.PutFacet(ctx, facetRec); err != nil {
		return err
	}
	fr, ok, err := st.GetFacet(ctx, p0, 0)
	if err != nil {
		return err
	}
	if !ok || fr.DerivedContent != "facet-demo" {
		return errors.New("GetFacet: unexpected result")
	}
	var nFacet int
	if err := st.AscendFacetsForCell(ctx, p0, func(record.FacetRecord) bool {
		nFacet++
		return true
	}); err != nil {
		return err
	}
	if nFacet != 1 {
		return fmt.Errorf("AscendFacetsForCell: want 1 got %d", nFacet)
	}

	fmt.Println("WalkRingFacets")
	var facetRing int
	if err := st.WalkRingFacets(ctx, c0, 0, 0x1, nil, func(_ lattice.Coord, _ record.CellRecord, fs []record.FacetRecord) bool {
		if len(fs) > 0 {
			facetRing++
		}
		return true
	}); err != nil {
		return err
	}
	if facetRing != 1 {
		return fmt.Errorf("WalkRingFacets: want 1 ring hit got %d", facetRing)
	}

	fmt.Println("UpdateFacet")
	facetRec.DerivationHash = record.HashRawContent([]byte("alpha"))
	facetRec.DerivedContent = "facet-updated"
	if err := st.UpdateFacet(ctx, facetRec); err != nil {
		return err
	}

	fmt.Println("LinkCells / GetEdge / AscendEdgesFrom")
	if err := st.LinkCells(ctx, c0, c1, "adjacent", 1, prov); err != nil {
		return err
	}
	edge, ok, err := st.GetEdge(ctx, p0, p1, "adjacent")
	if err != nil {
		return err
	}
	if !ok || edge.Weight != 1 {
		return errors.New("GetEdge: unexpected result")
	}
	var nEdge int
	if err := st.AscendEdgesFrom(ctx, p0, func(record.EdgeRecord) bool {
		nEdge++
		return true
	}); err != nil {
		return err
	}
	if nEdge != 1 {
		return fmt.Errorf("AscendEdgesFrom: want 1 got %d", nEdge)
	}

	fmt.Println("PutSeam / FindSeams / seam secondaries / ResolveSeam")
	var lo int64 = 100
	var hi int64 = 200
	seamID := ulid.MustNew(ulid.Now(), rand.Reader).String()
	seamSrc := "walkthrough-seam"
	seam := record.SeamRecord{
		ID:               seamID,
		CellA:            p0,
		CellB:            p1,
		SeamType:         "demo",
		Reason:           "walkthrough",
		ConfidenceDelta:  0.1,
		DetectedAt:       1,
		ResolutionStatus: "",
		ResolutionNote:   "",
		Validity:         record.ValidityWire{ValidFrom: &lo, ValidTo: &hi},
		Provenance:       record.ProvenanceWire{SourceID: seamSrc, Confidence: 1, CreatedAt: 1, UpdatedAt: 1},
	}
	if err := st.PutSeam(ctx, seam); err != nil {
		return err
	}
	seams, err := st.FindSeams(ctx, c0, 2, true)
	if err != nil {
		return err
	}
	if len(seams) != 1 {
		return fmt.Errorf("FindSeams: want 1 got %d", len(seams))
	}
	asOfSeam := time.Unix(0, 150).UTC()
	seamsAt, err := st.FindSeamsAt(ctx, c0, 2, true, asOfSeam)
	if err != nil {
		return err
	}
	if len(seamsAt) != 1 {
		return fmt.Errorf("FindSeamsAt: want 1 got %d", len(seamsAt))
	}
	var nSeamSrc int
	if err := st.AscendSeamsBySource(ctx, seamSrc, func(record.SeamRecord) bool {
		nSeamSrc++
		return true
	}); err != nil {
		return err
	}
	if nSeamSrc != 1 {
		return fmt.Errorf("AscendSeamsBySource: want 1 got %d", nSeamSrc)
	}
	sbucket, ok := index.WeekBucketFromValidity(seam.Validity)
	if !ok {
		return errors.New("seam WeekBucketFromValidity")
	}
	var nSeamTime int
	if err := st.AscendSeamsInTimeBucket(ctx, sbucket, func(record.SeamRecord) bool {
		nSeamTime++
		return true
	}); err != nil {
		return err
	}
	if nSeamTime != 1 {
		return fmt.Errorf("AscendSeamsInTimeBucket: want 1 got %d", nSeamTime)
	}
	if err := st.ResolveSeam(ctx, seamID, "resolved", "walkthrough ok"); err != nil {
		return err
	}

	fmt.Println("MarkConflict")
	cExtra := lattice.Coord{Q: -1, R: 1}
	if err := st.MarkConflict(ctx, c0, cExtra, "walkthrough mark_conflict"); err != nil {
		return err
	}

	fmt.Printf("done — database path: %s\n", p)
	return nil
}
