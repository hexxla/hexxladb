package main

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/hexxla/hexxladb"
	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
)

// simulateHexxlaService approximates what a HEXXLA gateway would measure in production:
// token-budget behavior, secondary-index recall, seam visibility, and read-path latency.
// Numbers are illustrative (single-shot timings); run `make bench` for rigorous stats.
func simulateHexxlaService(ctx context.Context, db *hexxladb.DB, center lattice.Coord, loadR, seamScanR, nTurns int) error {
	candidateCap := max(512, nTurns+128)
	cfgBase := hexxladb.LoadContextBudgetConfig{
		Assemble:          hexxladb.DefaultAssembleCellViewOpts(),
		MaxCandidateCells: candidateCap,
		IncludeFacetText:  true,
		IncludeSeams:      true,
		SeamRadius:        seamScanR,
	}

	fmt.Println()
	fmt.Println("=== HEXXLA service simulation (effectiveness grasp) ===")
	fmt.Println("How a product layer might probe the same primitives you ship in library form:")
	fmt.Println()

	// 1) Budget ladder — mimics tightening context windows for smaller models.
	fmt.Println("1) Token budget ladder (LoadContextPack — eviction outer ring / lowest confidence first)")
	budgets := []int{900, 4500, 18000, 120000}
	fmt.Printf("%-10s %-8s %-14s %-8s %-10s\n", "budget_B", "cells", "pack_byte_sum", "seams", "wall_us")
	fmt.Println(strings.Repeat("-", 54))
	for _, b := range budgets {
		start := time.Now()
		var pack hexxladb.ContextPack
		err := db.View(func(tx *hexxladb.Tx) error {
			var e error
			pack, e = tx.LoadContextPack(ctx, hexxladb.Coord(center), loadR, b, hexxladb.ByteLenBudgeter{}, cfgBase)
			return e
		})
		if err != nil {
			return err
		}
		us := time.Since(start).Microseconds()
		fmt.Printf("%-10d %-8d %-14d %-8d %-10d\n", b, len(pack.Cells), pack.TotalTokens, len(pack.Seams), us)
	}

	// 2) Tag recall — proves tag secondaries match what you wrote for the first n turns.
	fmt.Println()
	fmt.Println("2) Tag-index recall vs scripted expectations (first n turns)")
	showTags := []string{
		tagTopicPrefs, tagTopicSecurity, tagTopicIncident, tagTopicQuota, tagRetrievalKB, tagTopicProject,
	}
	fmt.Printf("%-26s %-10s %-10s\n", "tag", "scripted", "indexed")
	fmt.Println(strings.Repeat("-", 50))
	for _, tg := range showTags {
		want := 0
		for i := range nTurns {
			if i >= len(sessionScript) {
				break
			}
			if slices.Contains(sessionScript[i].Tags, tg) {
				want++
			}
		}
		var got int
		err := db.View(func(tx *hexxladb.Tx) error {
			return tx.AscendCellsByTag(ctx, tg, func(record.CellRecord) bool {
				got++
				return true
			})
		})
		if err != nil {
			return err
		}
		ok := "ok"
		if got != want {
			ok = " MISMATCH"
		}
		fmt.Printf("%-26s %-10d %-10d%s\n", tg, want, got, ok)
	}

	// 3) Source sweep latency — typical “hydrate dashboard” pattern.
	fmt.Println()
	fmt.Println("3) Secondary index sweep latency (single View; not warmed/GC isolated)")
	t0 := time.Now()
	var sources int
	err := db.View(func(tx *hexxladb.Tx) error {
		for _, src := range allSourceIDs() {
			if err := tx.AscendCellsBySource(ctx, src, func(record.CellRecord) bool {
				sources++
				return true
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	fmt.Printf("  AscendCellsBySource all roles: %d cell visits in %s\n", sources, time.Since(t0).Truncate(time.Microsecond))

	// 4) Seam visibility — contradictions must surface in FindSeams within geometric radius.
	var seamCount int
	t1 := time.Now()
	err = db.View(func(tx *hexxladb.Tx) error {
		var e error
		var seams []record.SeamRecord
		seams, e = tx.FindSeams(ctx, center, seamScanR, false)
		seamCount = len(seams)
		return e
	})
	if err != nil {
		return err
	}
	fmt.Printf("  FindSeams(r≤%d): %d seam(s) in %s\n", seamScanR, seamCount, time.Since(t1).Truncate(time.Microsecond))

	// 5) Neighborhood coverage — wire-first LoadContext should recover all scripted cells when uncapped.
	var wireCells int
	err = db.View(func(tx *hexxladb.Tx) error {
		var e error
		var cells []record.CellRecord
		cells, e = tx.LoadContext(ctx, center, loadR, max(nTurns+50, 800))
		wireCells = len(cells)
		return e
	})
	if err != nil {
		return err
	}
	fmt.Printf("  LoadContext(maxR=%d, generous maxCells): %d cells materialized (want ≥ %d scripted)\n",
		loadR, wireCells, nTurns)
	if wireCells < nTurns {
		fmt.Fprintf(os.Stderr, "  note: geometric cap may omit distant rings if maxCells too low — tune for demos.\n")
	}

	fmt.Println()
	fmt.Println("Interpretation: tightening budgets trims outer/confident-low cells first; tags/sources answer")
	fmt.Println("'what belong to topic X?' without walking the lattice; seams stay visible when radius covers endpoints.")
	fmt.Println("=== End simulation ===")
	fmt.Println()
	return nil
}

func allSourceIDs() []string {
	seen := make(map[string]struct{})
	var order []string
	for _, row := range sessionScript {
		if _, ok := seen[row.SourceID]; ok {
			continue
		}
		seen[row.SourceID] = struct{}{}
		order = append(order, row.SourceID)
	}
	return order
}
