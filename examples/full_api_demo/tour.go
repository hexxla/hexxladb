package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/hexxla/hexxladb"
	"github.com/hexxla/hexxladb/internal/index"
	"github.com/hexxla/hexxladb/internal/record"
)

// runeCountBudgeter is a small [hexxladb.TokenBudgeter] for the full-API tour.
type runeCountBudgeter struct{}

func (runeCountBudgeter) CountTokens(s string) int { return len([]rune(s)) }

// runTour exercises the package root API against freshly created database files under dir.
func runTour(ctx context.Context, dir string, eli5, skipEnc bool) error {
	mainPath := filepath.Join(dir, "full_api_tour.db")
	for _, p := range []string{mainPath, mainPath + "-wal", mainPath + "-changelog"} {
		_ = os.Remove(p)
	}

	opts := &hexxladb.Options{
		EnableMVCC:       true,
		ChangelogEnabled: true,
		MVCCRetention: hexxladb.MVCCRetention{
			RetainCommitsBehindHead: 8,
		},
	}

	db, err := hexxladb.Open(mainPath, opts)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	ts, err := seedTourMain(ctx, db)
	if err != nil {
		return err
	}
	c0, c1, c2, cX := ts.c0, ts.c1, ts.c2, ts.cX
	p0, _, p2 := ts.p0, ts.p1, ts.p2
	seamResolvableID := ts.seamResolvableID
	seamSrc := ts.seamSrc
	markSeam := ts.markSeam
	vCell := ts.vCell

	fmt.Printf("Seed summary: lattice rings 0..%d (%d coords written), center MVCC → hello-version-%d\n",
		tourLatticeFillR, len(hexxladb.WalkRings(nil, c0, tourLatticeFillR)), tourMVCCLastVersion)

	if eli5 {
		eli5Banner(
			"The honeycomb toy box",
			"Imagine you have stickers on a honeycomb floor instead of one giant messy pile.\n"+
				"Each sticker sits on ONE hex so you always know WHERE a fact lives.\n"+
				"This program builds that floor in a real file on disk, then shows every toy tool HexxlaDB gives you.",
		)
	}

	if eli5 {
		eli5Banner(
			"Shapes and counting steps",
			"A Coord is where you stand on the honeycomb.\n"+
				"Distance counts how many steps along the hex paths.\n"+
				"Neighbors are the six stickers touching yours.",
			fmt.Sprintf("MaxAxialAbs=%d — the toy box only allows stickers this far from the middle.", hexxladb.MaxAxialAbs),
		)
	}
	cube := c0.Cube()
	nbc := len(hexxladb.Ring(c0, 1))
	fmt.Printf("coord c0=%+v cube=%+v neighbors=%d distance(c0,c1)=%d\n",
		c0, cube, nbc, c0.Distance(c1))

	fmt.Printf("Ring(0): %d cells; Ring(1): %d cells\n", len(hexxladb.Ring(c0, 0)), len(hexxladb.Ring(c0, 1)))
	wr := hexxladb.WalkRings(nil, c0, 2)
	fmt.Printf("WalkRings(center, maxR=2): %d coords\n", len(wr))

	if eli5 {
		eli5Banner(
			"Packing addresses",
			"PackedCoord is the sticker ID used inside the database.\n"+
				"Pack turns (q,r) into that ID; Unpack turns it back.",
		)
	}
	{
		back, err := hexxladb.Unpack(p0)
		if err != nil {
			return err
		}
		fmt.Printf("Unpack round-trip: equal=%v c0=%+v back=%+v\n", back.Q == c0.Q && back.R == c0.R, c0, back)
	}

	if eli5 {
		eli5Banner(
			"We wrote stickers while the camera rolled",
			"We saved cells (facts), facets (little summaries), edges (arrows between stickers),\n"+
				"seams (bright ribbons where two stickers disagree), and then bumped one sticker many times\n"+
				"so the grown-up MVCC camera can rewind old versions.",
		)
	}

	if err := db.View(func(tx *hexxladb.Tx) error {
		if tx.Writable() {
			return fmt.Errorf("writable in View")
		}
		v, ok, err := tx.Get([]byte("__demo__/scratch-kv"))
		if err != nil {
			return fmt.Errorf("Tx.Get scratch: %w", err)
		}
		if !ok || string(v) != "hello-raw-btree" {
			return errors.New("Tx.Get scratch: missing or unexpected value")
		}
		fmt.Printf("Tx.Get(scratch kv): %q\n", string(v))

		from := []byte(index.CellPrefix)
		to := []byte("celo")
		var nCellKeys int
		if err := tx.AscendRange(from, to, func(_, _ []byte) bool {
			nCellKeys++
			return true
		}); err != nil {
			return err
		}
		fmt.Printf("AscendRange(cell/…celo): %d physical cell btree keys (MVCC versions counted)\n", nCellKeys)

		if err := tx.WalkRing(ctx, c0, 1, func(_ hexxladb.Coord, _ []byte, _ bool) bool {
			return true
		}); err != nil {
			return err
		}
		fmt.Println("WalkRing ring=1 OK")

		asOf := time.Unix(0, 150).UTC()
		if err := tx.WalkRingAt(ctx, c0, 0, asOf, func(_ hexxladb.Coord, rec record.CellRecord) bool {
			fmt.Printf("WalkRingAt ring=0 raw prefix=%q…\n", truncate(rec.RawContent, 40))
			return false
		}); err != nil {
			return err
		}

		if err := tx.WalkRingFacets(ctx, c0, 0, 0x3, nil, func(_ hexxladb.Coord, _ record.CellRecord, fs []record.FacetRecord) bool {
			fmt.Printf("WalkRingFacets ring=0 facets_at_center=%d\n", len(fs))
			return true
		}); err != nil {
			return err
		}

		raws, err := tx.LoadContext(ctx, c0, 2, 500)
		if err != nil {
			return err
		}
		fmt.Printf("LoadContext(maxR=2 maxCells=500): %d wire cells\n", len(raws))

		asOfVC := time.Unix(0, 150).UTC()
		rawsAt, err := tx.LoadContextAt(ctx, c0, 2, 500, asOfVC)
		if err != nil {
			return err
		}
		fmt.Printf("LoadContextAt(seam-validity instant): %d wire cells\n", len(rawsAt))

		cv, err := tx.AssembleCellView(ctx, c0, nil, hexxladb.AssembleCellViewOpts{
			IncludeFacets:       true,
			IncludeEdges:        true,
			IncludeSeams:        true,
			SeamSearchRadius:    2,
			UnresolvedSeamsOnly: false,
		})
		if err != nil {
			return err
		}
		fmt.Printf("AssembleCellView: tags=%v facets=%d edges=%d seamRefs=%d\n",
			cv.Tags, len(cv.Facets), len(cv.Edges), len(cv.Seams))

		wire, okWire, err := tx.GetCell(p0)
		if err != nil {
			return fmt.Errorf("GetCell(center): %w", err)
		}
		if !okWire {
			return errors.New("GetCell(center): missing cell")
		}
		fmt.Printf("One cell — wire GetCell(center): raw=%q tags=%v source=%s confidence=%.2f clusterHintPacked=%v\n",
			truncate(wire.RawContent, 72), wire.Tags, wire.Provenance.SourceID, wire.Provenance.Confidence, wire.ClusterHint)
		fmt.Printf("One cell — validity (same record): ValidFrom=%v ValidTo=%v\n", wire.Validity.ValidFrom, wire.Validity.ValidTo)
		fmt.Printf("One cell — assembled CellView: coord=%+v raw=%q activeFacet=%d clusterHintCoord=%v\n",
			cv.Coord, truncate(cv.RawContent, 72), cv.ActiveFacet, cv.ClusterHint)
		for _, fv := range cv.Facets {
			fmt.Printf("  facet[%d]: %q… derivationHashPrefix=%s\n", fv.ID, truncate(fv.DerivedContent, 56), truncate(fv.DerivationHash, 16))
		}
		for _, ev := range cv.Edges {
			fmt.Printf("  edge → %+v relation=%s weight=%.2f\n", ev.To, ev.RelationType, ev.Weight)
		}
		for _, sr := range cv.Seams {
			fmt.Printf("  seamRef id=%s type=%s resolution=%q\n", truncate(sr.ID, 36), sr.SeamType, sr.ResolutionStatus)
		}

		explicitCoords := []hexxladb.Coord{
			c0, c1, c2, cX,
			{Q: -2, R: 2}, {Q: 3, R: -2}, {Q: -3, R: 1}, {Q: 2, R: 2},
			{Q: -1, R: -1}, {Q: 4, R: -3},
		}
		fmt.Printf("GetCell loop over explicit coords (%d): each Pack+GetCell — no batch read API required\n",
			len(explicitCoords))
		for _, ac := range explicitCoords {
			ap, perr := hexxladb.Pack(ac)
			if perr != nil {
				return perr
			}
			rec, ok, gerr := tx.GetCell(ap)
			if gerr != nil {
				return fmt.Errorf("GetCell %+v: %w", ac, gerr)
			}
			if !ok {
				fmt.Printf("  %+v: (missing)\n", ac)
				continue
			}
			fmt.Printf("  %+v: raw=%q src=%s tags=%v\n", ac, truncate(rec.RawContent, 52), rec.Provenance.SourceID, rec.Tags)
		}

		cfg := hexxladb.LoadContextBudgetConfig{
			Assemble: hexxladb.AssembleCellViewOpts{
				IncludeFacets:       true,
				IncludeEdges:        false,
				IncludeSeams:        true,
				SeamSearchRadius:    2,
				UnresolvedSeamsOnly: false,
			},
			MaxCandidateCells: 64,
			IncludeFacetText:  true,
			IncludeSeams:      true,
			SeamRadius:        2,
		}
		packBudget, err := tx.LoadContextWithBudgeting(ctx, c0, 2, 8000, hexxladb.ByteLenBudgeter{}, cfg)
		if err != nil {
			return err
		}
		packAlias, err := tx.LoadContextPack(ctx, c0, 2, 8000, hexxladb.ByteLenBudgeter{}, cfg)
		if err != nil {
			return err
		}
		fmt.Printf("LoadContextWithBudgeting cells=%d tokens=%d | LoadContextPack cells=%d (alias)\n",
			len(packBudget.Cells), packBudget.TotalTokens, len(packAlias.Cells))

		packRune, err := tx.LoadContextWithBudgeting(ctx, c0, 1, 400, runeCountBudgeter{}, cfg)
		if err != nil {
			return err
		}
		fmt.Printf("Custom TokenBudgeter (rune count): cells=%d tokens=%d\n", len(packRune.Cells), packRune.TotalTokens)

		filtered := hexxladb.FilterCellViews(packBudget.Cells, func(v hexxladb.CellView) bool {
			return slices.Contains(v.Tags, "demo/tag-a")
		})
		trimmed, tok := hexxladb.TruncateCellViewsToTokenBudget(filtered, hexxladb.ByteLenBudgeter{}, 200, true)
		fmt.Printf("FilterCellViews+TruncateCellViewsToTokenBudget: kept=%d totalTok=%d\n", len(trimmed), tok)

		topics, err := tx.ListExistingTopics(ctx)
		if err != nil {
			return err
		}
		fmt.Printf("ListExistingTopics: %v\n", topics)

		var distinct int
		if err := tx.AscendDistinctTags(ctx, func(string) bool {
			distinct++
			return true
		}); err != nil {
			return err
		}
		fmt.Printf("AscendDistinctTags visitor calls (distinct tags): %d\n", distinct)

		var nSrc int
		if err := tx.AscendCellsBySource(ctx, "demo/source-alpha", func(record.CellRecord) bool {
			nSrc++
			return true
		}); err != nil {
			return err
		}
		fmt.Printf("AscendCellsBySource(demo/source-alpha): %d\n", nSrc)

		bucket, ok := index.WeekBucketFromValidity(vCell)
		if !ok {
			return fmt.Errorf("WeekBucketFromValidity cell")
		}
		var nTime int
		if err := tx.AscendCellsInTimeBucket(ctx, bucket, func(record.CellRecord) bool {
			nTime++
			return true
		}); err != nil {
			return err
		}
		fmt.Printf("AscendCellsInTimeBucket(%d): %d\n", bucket, nTime)

		var nTag int
		if err := tx.AscendCellsByTag(ctx, "demo/tag-a", func(record.CellRecord) bool {
			nTag++
			return true
		}); err != nil {
			return err
		}
		fmt.Printf("AscendCellsByTag(demo/tag-a): %d\n", nTag)

		er, ok, err := tx.GetEdge(p0, p2, "cites")
		if err != nil {
			return fmt.Errorf("GetEdge cites: %w", err)
		}
		if !ok {
			return errors.New("GetEdge cites: missing")
		}
		fmt.Printf("GetEdge cites weight=%.2f\n", er.Weight)

		var nEd int
		if err := tx.AscendEdgesFrom(p0, func(record.EdgeRecord) bool {
			nEd++
			return true
		}); err != nil {
			return err
		}
		fmt.Printf("AscendEdgesFrom(center): %d edges\n", nEd)

		seams, err := tx.FindSeams(ctx, c0, 3, false)
		if err != nil {
			return err
		}
		fmt.Printf("FindSeams(radius=3): %d seams\n", len(seams))

		asOfSeam := time.Unix(0, 150).UTC()
		seamsAt, err := tx.FindSeamsAt(ctx, c0, 3, false, asOfSeam)
		if err != nil {
			return err
		}
		fmt.Printf("FindSeamsAt(as_of inside validity): %d seams\n", len(seamsAt))

		var nSS int
		if err := tx.AscendSeamsBySource(ctx, seamSrc, func(record.SeamRecord) bool {
			nSS++
			return true
		}); err != nil {
			return err
		}
		fmt.Printf("AscendSeamsBySource(%q): %d\n", seamSrc, nSS)

		sbucket, ok := index.WeekBucketFromValidity(markSeam.Validity)
		if !ok {
			return fmt.Errorf("WeekBucketFromValidity seam")
		}
		var nST int
		if err := tx.AscendSeamsInTimeBucket(ctx, sbucket, func(record.SeamRecord) bool {
			nST++
			return true
		}); err != nil {
			return err
		}
		fmt.Printf("AscendSeamsInTimeBucket(%d): %d\n", sbucket, nST)

		fr, ok, err := tx.GetFacet(p0, 1)
		if err != nil {
			return fmt.Errorf("GetFacet: %w", err)
		}
		if !ok {
			return errors.New("GetFacet: missing")
		}
		fmt.Printf("GetFacet(1): %q…\n", truncate(fr.DerivedContent, 40))

		var nFacetScan int
		if err := tx.AscendFacetsForCell(p0, func(record.FacetRecord) bool {
			nFacetScan++
			return true
		}); err != nil {
			return err
		}
		fmt.Printf("AscendFacetsForCell: %d facet rows visible\n", nFacetScan)

		return nil
	}); err != nil {
		return err
	}

	if err := db.Update(func(tx *hexxladb.Tx) error {
		return tx.ResolveSeam(seamResolvableID, "resolved-demo", "we picked a winner for the ELI5 tour")
	}); err != nil {
		return err
	}

	if eli5 {
		eli5Banner(
			"Time-travel goggles",
			"ViewAt rewinds to an old photo of the sticker box (a past commit).\n"+
				"ViewAtTime asks what the box knew at a clock time on the wall.",
		)
	}

	stats, err := db.StatsMVCC()
	if err != nil {
		return err
	}
	fmt.Printf("StatsMVCC: CommitSeq=%d versioned_rows=%d logical_cells=%d\n",
		stats.CommitSeq, stats.VersionedRows, stats.LogicalCells)

	if err := db.ViewAt(2, func(tx *hexxladb.Tx) error {
		rec, ok, err := tx.GetCell(p0)
		if err != nil || !ok {
			return fmt.Errorf("ViewAt(2) GetCell")
		}
		fmt.Printf("ViewAt(seq=2) center raw prefix=%q… (seq 1 was only scratch KV)\n", truncate(rec.RawContent, 28))
		return nil
	}); err != nil {
		return err
	}

	if err := db.ViewAtTime(time.Now().UTC().Add(time.Hour), func(tx *hexxladb.Tx) error {
		_, ok, err := tx.GetCell(p0)
		if err != nil || !ok {
			return fmt.Errorf("ViewAtTime future")
		}
		return nil
	}); err != nil {
		return err
	}

	badSeq := stats.CommitSeq + 1_000_000
	viewFutErr := db.ViewAt(badSeq, func(*hexxladb.Tx) error { return nil })
	if viewFutErr == nil {
		return errors.New("ViewAt future seq: got nil error")
	}
	if !errors.Is(viewFutErr, hexxladb.ErrReadSeqFuture) {
		return fmt.Errorf("ViewAt future seq: want ErrReadSeqFuture: %w", viewFutErr)
	}
	fmt.Println("ViewAt(far future seq): ErrReadSeqFuture (expected)")

	if eli5 {
		eli5Banner(
			"The diary tape on the side",
			"ChangelogEnabled keeps a side diary of writes — PutCell, seams, facets, edges.\n"+
				"Downstream robots can read it without replaying the whole floor.",
		)
	}
	recs, err := db.ReadChangelogSince(0, tourChangelogReadCap)
	if err != nil {
		return err
	}
	fmt.Printf("ReadChangelogSince(0,%d): %d records\n", tourChangelogReadCap, len(recs))
	if len(recs) > 0 {
		fmt.Printf("  first record Op=%d (constants: ChangelogOpPutCell=%d …)\n", recs[0].Op, hexxladb.ChangelogOpPutCell)
	}
	var nResolveSeamOps int
	for _, r := range recs {
		if r.Op == hexxladb.ChangelogOpResolveSeam {
			nResolveSeamOps++
		}
	}
	fmt.Printf("  OpResolveSeam records (from Tx.ResolveSeam): %d\n", nResolveSeamOps)

	before, okPrune, err := db.SuggestedPruneBeforeSeq()
	if err != nil {
		return err
	}
	fmt.Printf("SuggestedPruneBeforeSeq: before=%d ok=%v\n", before, okPrune)

	planBefore, maxDel, okPlan, err := db.MVCCPrunePlan(hexxladb.MVCCPruneBalanced)
	if err != nil {
		return err
	}
	fmt.Printf("MVCCPrunePlan(balanced): before=%d maxDelete=%d ok=%v\n", planBefore, maxDel, okPlan)

	if okPrune && okPlan {
		nPruned, err := db.PruneCellVersions(planBefore, maxDel)
		if err != nil {
			return err
		}
		fmt.Printf("PruneCellVersions: deleted %d stale rows\n", nPruned)

		sched := hexxladb.PruneScheduler{Profile: hexxladb.MVCCPruneLowLatency}
		nTick, err := sched.Tick(db)
		if err != nil {
			return err
		}
		fmt.Printf("PruneScheduler.Tick(low-latency): deleted %d\n", nTick)
	}

	stats2, err := db.StatsMVCC()
	if err != nil {
		return err
	}
	fmt.Printf("StatsMVCC after prune: versioned_rows=%d\n", stats2.VersionedRows)

	if !skipEnc {
		if eli5 {
			eli5Banner(
				"Secret lockbox (encryption)",
				"The next tiny database uses a password so the file looks like noise on disk.\n"+
					"RotateEncryption copies everything into a new lock with a new password — offline, safe swap.",
			)
		}
		if err := runEncryptionBranch(ctx, dir, eli5); err != nil {
			return err
		}
	}

	return nil
}

func runEncryptionBranch(ctx context.Context, dir string, _ bool) error {
	encPath := filepath.Join(dir, "encrypted_tour.db")
	for _, p := range []string{encPath, encPath + "-wal"} {
		_ = os.Remove(p)
	}

	salt := make([]byte, 16)
	for i := range salt {
		salt[i] = byte(i + 1)
	}
	dk, err := hexxladb.DeriveKeyFromPassphrase("eli5-demo-passphrase", salt)
	if err != nil {
		return err
	}
	fmt.Printf("DeriveKeyFromPassphrase: derived key length=%d bytes (used below as EncryptionKey)\n", len(dk))

	salt2 := make([]byte, 16)
	for i := range salt2 {
		salt2[i] = byte(255 - i)
	}
	dk2, err := hexxladb.DeriveKeyFromPassphrase("eli5-demo-second-key", salt2)
	if err != nil {
		return err
	}

	oldOpts := &hexxladb.Options{EncryptionKey: dk}
	db, err := hexxladb.Open(encPath, oldOpts)
	if err != nil {
		return err
	}

	c := hexxladb.Coord{Q: 5, R: -3}
	pk, err := hexxladb.Pack(c)
	if err != nil {
		_ = db.Close()
		return err
	}
	n := time.Now().UnixNano()
	prov := record.ProvenanceWire{SourceID: "enc-demo", Confidence: 1, CreatedAt: n, UpdatedAt: n}
	cell := record.CellRecord{
		Key:        pk,
		RawContent: "secret honeycomb fact",
		Provenance: prov,
	}
	if err := db.Update(func(tx *hexxladb.Tx) error { return tx.PutCell(ctx, cell) }); err != nil {
		_ = db.Close()
		return err
	}
	if err := db.Close(); err != nil {
		return err
	}

	newOpts := &hexxladb.Options{EncryptionKey: dk2}
	if err := hexxladb.RotateEncryptionWithOptions(encPath, oldOpts, newOpts, &hexxladb.RotateOptions{
		BatchSize: 128,
		OnProgress: func(copied int64) {
			if copied%512 == 0 && copied > 0 {
				fmt.Printf("  RotateEncryption copied=%d rows…\n", copied)
			}
		},
	}); err != nil {
		return err
	}
	fmt.Println("RotateEncryptionWithOptions: OK")

	db2, err := hexxladb.Open(encPath, newOpts)
	if err != nil {
		return err
	}
	defer func() { _ = db2.Close() }()

	return db2.View(func(tx *hexxladb.Tx) error {
		rec, ok, err := tx.GetCell(pk)
		if err != nil || !ok {
			return fmt.Errorf("reopen encrypted db GetCell")
		}
		fmt.Printf("Reopened encrypted DB: raw=%q\n", truncate(rec.RawContent, 50))
		return nil
	})
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
