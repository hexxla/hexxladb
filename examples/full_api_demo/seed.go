package main

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/hexxla/hexxladb"
	"github.com/hexxla/hexxladb/internal/record"
)

// Tour seed tuning — see seedTourMain for every write into full_api_tour.db (MVCC lattice + bumps).
// Cell blobs must stay within the engine ordered-store value limit (512 bytes encoded; see docs/hexxladb/API_REFERENCE.md);
// this tour keeps RawContent + metadata within that budget.
const (
	// tourLatticeFillR places one cell at every axial coordinate in rings [0,tourLatticeFillR] around the origin (~61 hexes when 4).
	tourLatticeFillR = 4
	// tourMVCCLastVersion is the final hello-version-N on the center cell (loops 2..N).
	// Stress: lattice + many center versions + prune is covered by TestIntegration_MVCC_latticeAndHighChurnPrune.
	tourMVCCLastVersion = 250
	// tourChangelogReadCap must cover all mutation records written by this tour or ReadChangelogSince truncates.
	tourChangelogReadCap = 500000
)

type tourSeed struct {
	seamResolvableID string
	seamSrc          string
	markSeam         record.SeamRecord
	vCell            record.ValidityWire
	c0, c1, c2, cX   hexxladb.Coord
	p0, p1, p2       hexxladb.PackedCoord
}

// seedTourMain performs all btree writes for the main tour database file (scratch KV, dense lattice,
// MVCC history on the center hex, facets, edges, seams, MarkConflict). It does not open the
// optional encrypted DB — that stays in runEncryptionBranch in tour.go.
func seedTourMain(ctx context.Context, db *hexxladb.DB) (*tourSeed, error) {
	c0 := hexxladb.Coord{Q: 0, R: 0}
	c1 := hexxladb.Coord{Q: 1, R: 0}
	c2 := hexxladb.Coord{Q: 0, R: 1}
	cX := hexxladb.Coord{Q: -2, R: 3}

	p0, err := hexxladb.Pack(c0)
	if err != nil {
		return nil, err
	}
	p1, err := hexxladb.Pack(c1)
	if err != nil {
		return nil, err
	}
	p2, err := hexxladb.Pack(c2)
	if err != nil {
		return nil, err
	}
	hint := p2

	vFrom := int64(100)
	vTo := int64(200)
	vCell := record.ValidityWire{ValidFrom: &vFrom, ValidTo: &vTo}

	prov := func(src string) record.ProvenanceWire {
		n := time.Now().UnixNano()
		return record.ProvenanceWire{
			SourceID: src, Confidence: 1, CreatedAt: n, UpdatedAt: n,
		}
	}

	seamA, seamB := record.CanonicalCellPair(p0, p1)
	seamResolvableID := ulid.MustNew(ulid.Timestamp(time.Now()), rand.Reader).String()
	seamSrc := "demo/seam-source"

	markSeam := record.SeamRecord{
		ID:               seamResolvableID,
		CellA:            seamA,
		CellB:            seamB,
		SeamType:         "fact_drift",
		Reason:           "demo seam for ResolveSeam",
		ConfidenceDelta:  0.12,
		DetectedAt:       time.Now().UnixNano(),
		ResolutionStatus: "",
		ResolutionNote:   "",
		Validity: record.ValidityWire{
			ValidFrom: &vFrom,
			ValidTo:   &vTo,
		},
		Provenance: prov(seamSrc),
	}

	tEdge := time.Now().UnixNano()
	edgeExplicit := record.EdgeRecord{
		From:         p0,
		To:           p2,
		RelationType: "cites",
		Weight:       0.75,
		Provenance:   record.ProvenanceWire{SourceID: "edge-writer", Confidence: 1, CreatedAt: tEdge, UpdatedAt: tEdge},
	}

	tagsRot := []string{"demo/tag-a", "demo/tag-b", "demo/tag-c", "demo/tag-d", "demo/tag-e"}
	srcRot := []string{
		"demo/source-alpha", "demo/source-beta", "demo/source-gamma", "demo/source-delta",
		"demo/source-epsilon", "demo/source-zeta", "demo/source-eta", "demo/source-theta",
	}

	if err := db.Update(func(tx *hexxladb.Tx) error {
		return tx.Put([]byte("__demo__/scratch-kv"), []byte("hello-raw-btree"))
	}); err != nil {
		return nil, err
	}

	coords := hexxladb.WalkRings(nil, c0, tourLatticeFillR)

	if err := db.Batch(func(tx *hexxladb.Tx) error {
		if !tx.Writable() {
			return errors.New("writable: want true inside Batch")
		}
		for i, coord := range coords {
			packed, perr := hexxladb.Pack(coord)
			if perr != nil {
				return perr
			}
			raw := fmt.Sprintf("seed-q%d-r%d-%05d", coord.Q, coord.R, i)
			atCenter := coord.Q == c0.Q && coord.R == c0.R
			if atCenter {
				raw = "hello-version-1"
			}
			cell := record.CellRecord{
				Key:        packed,
				RawContent: raw,
				Provenance: prov(srcRot[i%len(srcRot)]),
				Tags: []string{
					tagsRot[i%len(tagsRot)],
					tagsRot[(i+2)%len(tagsRot)],
				},
			}
			if atCenter {
				cell.Tags = []string{"demo/tag-a", "demo/tag-b"}
				cell.Provenance = prov("demo/source-alpha")
				cell.ClusterHint = &hint
				cell.Validity = vCell
			} else if i%5 == 0 {
				cell.Validity = vCell
			}
			if err := tx.PutCell(ctx, cell); err != nil {
				return err
			}
		}

		f0 := record.FacetRecord{
			Key:            p0,
			FacetID:        0,
			DerivedContent: "short summary of hello",
			LastRotated:    time.Now().UnixNano(),
			DerivationHash: record.HashRawContent([]byte("hello-version-1")),
		}
		if err := tx.PutFacet(f0); err != nil {
			return err
		}
		if err := tx.PutEdge(edgeExplicit); err != nil {
			return err
		}
		if err := tx.LinkCells(c0, c1, "next", 1, prov("linker")); err != nil {
			return err
		}
		for _, nc := range hexxladb.Ring(c0, 1) {
			if err := tx.LinkCells(c0, nc, "hub", 0.25, prov("demo/hub")); err != nil {
				return err
			}
		}
		if err := tx.PutSeam(ctx, markSeam); err != nil {
			return err
		}
		return nil
	}); err != nil {
		return nil, err
	}

	for i := 2; i <= tourMVCCLastVersion; i++ {
		if err := db.Update(func(tx *hexxladb.Tx) error {
			rec, ok, err := tx.GetCell(p0)
			if err != nil {
				return fmt.Errorf("get cell: %w", err)
			}
			if !ok {
				return fmt.Errorf("get cell: missing")
			}
			rec.RawContent = fmt.Sprintf("hello-version-%d", i)
			rec.Provenance = prov("demo/source-alpha")
			return tx.PutCell(ctx, rec)
		}); err != nil {
			return nil, err
		}
	}

	if err := db.Update(func(tx *hexxladb.Tx) error {
		rec, ok, err := tx.GetCell(p0)
		if err != nil || !ok {
			return err
		}
		hash := record.HashRawContent([]byte(rec.RawContent))
		f1 := record.FacetRecord{
			Key:            p0,
			FacetID:        1,
			DerivedContent: "sticky note facet-1",
			LastRotated:    time.Now().UnixNano(),
			DerivationHash: hash,
		}
		return tx.PutFacet(f1)
	}); err != nil {
		return nil, err
	}

	if err := db.Update(func(tx *hexxladb.Tx) error {
		rec, ok, err := tx.GetCell(p0)
		if err != nil || !ok {
			return err
		}
		up := record.FacetRecord{
			Key:            p0,
			FacetID:        1,
			DerivedContent: "sticky note facet-1 UPDATED",
			LastRotated:    time.Now().UnixNano(),
			DerivationHash: record.HashRawContent([]byte(rec.RawContent)),
		}
		return tx.UpdateFacet(up)
	}); err != nil {
		return nil, err
	}

	if err := db.Update(func(tx *hexxladb.Tx) error {
		return tx.MarkConflict(c1, cX, "demo MarkConflict sugar")
	}); err != nil {
		return nil, err
	}

	return &tourSeed{
		seamResolvableID: seamResolvableID,
		seamSrc:          seamSrc,
		markSeam:         markSeam,
		vCell:            vCell,
		c0:               c0,
		c1:               c1,
		c2:               c2,
		cX:               cX,
		p0:               p0,
		p1:               p1,
		p2:               p2,
	}, nil
}
