// Spatial Algorithms Example
//
// Demonstrates HexxlaDB's spatial algorithms on the hex grid:
//
//   - Phase 1  Grid setup: seed cells in a spiral pattern with edges
//   - Phase 2  Field of View (LoadContextFOV) — symmetric shadowcasting visibility filtering
//   - Phase 3  Large-radius context (LoadContext) — bounded nearest-first loading
//   - Phase 4  Voronoi Partitioning (LoadContextVoronoi) — fair, non-overlapping regions
//   - Phase 5  Pathfinding (FindEdgePath Dijkstra, WalkEdges BFS, LoadContext with EdgeFilter)
//   - Phase 6  Comparison — radial vs large-radius vs FOV vs Voronoi vs edge-based
//
// No external dependencies required — runs on a self-contained in-memory-like DB.
//
// Usage:
//
//	go run ./examples/spatial_algorithms           # default DB at .tmp/spatial-algorithms.db
//	go run ./examples/spatial_algorithms -db /path/to/my.db
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/fatih/color"

	"github.com/hexxla/hexxladb"
)

const defaultDBPath = ".tmp/spatial-algorithms.db"

// Styles
var (
	headerStyle = color.New(color.FgCyan, color.Bold, color.Underline)
	stepStyle   = color.New(color.FgYellow, color.Bold)
	infoStyle   = color.New(color.FgBlue)
	dataStyle   = color.New(color.FgGreen)
	dimStyle    = color.New(color.FgHiBlack)
	okStyle     = color.New(color.FgGreen, color.Bold)
	sepStyle    = color.New(color.FgHiBlack)
	metricStyle = color.New(color.FgMagenta)
	accentStyle = color.New(color.FgYellow)
)

const lineW = 72

func printHeader(title string) {
	fmt.Println()
	_, _ = sepStyle.Println(strings.Repeat("═", lineW))
	_, _ = headerStyle.Printf("  ▶  %s\n", title)
	_, _ = sepStyle.Println(strings.Repeat("═", lineW))
}

func printStep(s string) {
	fmt.Println()
	_, _ = sepStyle.Println(strings.Repeat("─", lineW))
	_, _ = stepStyle.Printf("  ◆  %s\n", s)
	_, _ = sepStyle.Println(strings.Repeat("─", lineW))
}

func printNote(msg string) {
	_, _ = dimStyle.Println("  ℹ  " + msg)
}

func printMetric(label string, value any, unit string) {
	_, _ = metricStyle.Printf("  📊  %-28s ", label+":")
	_, _ = accentStyle.Printf("%v", value)
	if unit != "" {
		_, _ = dimStyle.Println(" " + unit)
	} else {
		fmt.Println()
	}
}

func printSuccess(msg string) {
	_, _ = okStyle.Println("  ✓  " + msg)
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}

func main() {
	dbPath := flag.String("db", defaultDBPath,
		"path to HexxlaDB (created fresh on each run)")
	flag.Parse()
	if err := run(*dbPath); err != nil {
		_, _ = color.New(color.FgRed, color.Bold).Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

func run(dbPath string) error {
	ctx := context.Background()

	_ = os.MkdirAll(filepath.Dir(dbPath), 0o750)
	_ = os.Remove(dbPath)
	_ = os.Remove(dbPath + "-wal")

	db, err := hexxladb.Open(dbPath, &hexxladb.Options{EnableMVCC: true})
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	defer func() { _ = db.Close() }()

	_, _ = sepStyle.Println(strings.Repeat("═", lineW))
	_, _ = headerStyle.Println("  HexxlaDB — Spatial Algorithms Demo")
	_, _ = dimStyle.Println("  FOV · radial context · Voronoi · Pathfinding on the hex grid")
	_, _ = sepStyle.Println(strings.Repeat("═", lineW))
	fmt.Println()

	// ════════════════════════════════════════════════════════════════
	// PHASE 1: Seed grid with cells and edges
	// ════════════════════════════════════════════════════════════════
	printHeader("Phase 1: Seed Grid")
	printNote("Creating a 7-ring spiral of cells with edges connecting sequential turns.")
	fmt.Println()

	type cellSeed struct {
		coord   hexxladb.Coord
		content string
		tags    []string
	}

	// Generate cells in a spiral pattern filling rings 0-6
	var seeds []cellSeed
	idx := 0
	for r := range 7 {
		ring := hexxladb.Ring(hexxladb.Coord{}, r)
		for _, c := range ring {
			topic := []string{"spatial", "demo"}
			switch {
			case r <= 1:
				topic = append(topic, "core", "important")
			case r <= 3:
				topic = append(topic, "context", "nearby")
			default:
				topic = append(topic, "distant", "background")
			}
			seeds = append(seeds, cellSeed{
				coord:   c,
				content: fmt.Sprintf("Cell at (%d,%d) ring=%d — %s region content #%d", c.Q, c.R, r, topic[2], idx),
				tags:    topic,
			})
			idx++
		}
	}

	// Also add some intentional gaps (skip every 5th cell in outer rings)
	var cells []hexxladb.Coord
	err = db.Update(func(tx *hexxladb.Tx) error {
		for i, s := range seeds {
			// Create gaps in outer rings to make FOV interesting
			if s.coord.Distance(hexxladb.Coord{}) >= 4 && i%5 == 0 {
				continue
			}
			pk, pErr := hexxladb.Pack(s.coord)
			if pErr != nil {
				continue
			}
			if err := tx.PutCell(ctx, hexxladb.CellRecord{
				Key:        pk,
				RawContent: s.content,
				Tags:       s.tags,
				Provenance: hexxladb.ProvenanceWire{
					SourceID:   "spatial-demo",
					Confidence: 1.0 - float64(s.coord.Distance(hexxladb.Coord{}))*0.1,
				},
			}); err != nil {
				return err
			}
			cells = append(cells, s.coord)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("seed cells: %w", err)
	}
	printMetric("Cells created", len(cells), "")
	printMetric("Grid extent", "7 rings (0-6)", "")

	// Create edges: connect sequential cells and add some cross-links
	var edgeCount int
	err = db.Update(func(tx *hexxladb.Tx) error {
		for i := 1; i < len(cells); i++ {
			fromPK, _ := hexxladb.Pack(cells[i-1])
			toPK, _ := hexxladb.Pack(cells[i])
			if err := tx.PutEdge(hexxladb.EdgeWalkRecord{
				From:         fromPK,
				To:           toPK,
				RelationType: "sequence",
				Weight:       1.0,
			}); err != nil {
				return err
			}
			edgeCount++
		}
		// Add some cross-links between distant cells
		crossLinks := [][2]int{{0, 20}, {5, 30}, {10, 40}, {15, 50}}
		for _, cl := range crossLinks {
			if cl[0] >= len(cells) || cl[1] >= len(cells) {
				continue
			}
			fromPK, _ := hexxladb.Pack(cells[cl[0]])
			toPK, _ := hexxladb.Pack(cells[cl[1]])
			if err := tx.PutEdge(hexxladb.EdgeWalkRecord{
				From:         fromPK,
				To:           toPK,
				RelationType: "cross-ref",
				Weight:       2.0,
			}); err != nil {
				return err
			}
			edgeCount++
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("create edges: %w", err)
	}
	printMetric("Edges created", edgeCount, "(sequence + cross-ref)")

	// Show grid
	_ = db.View(func(tx *hexxladb.Tx) error {
		grid, err := tx.RenderHexGridFromDB(ctx, hexxladb.Coord{}, 6)
		if err != nil {
			return nil
		}
		fmt.Println()
		for line := range strings.SplitSeq(grid, "\n") {
			_, _ = dimStyle.Println("    " + line)
		}
		return nil
	})
	fmt.Println()
	printSuccess("Grid seeded")
	fmt.Println()

	center := hexxladb.Coord{Q: 0, R: 0}

	// ════════════════════════════════════════════════════════════════
	// PHASE 2: Field of View
	// ════════════════════════════════════════════════════════════════
	printHeader("Phase 2: Field of View (LoadContextFOV)")
	printNote("Symmetric shadowcasting FOV (Albert Ford 2021 adaptation) from center.")
	printNote("Opaque function: empty grid positions = walls. Guaranteed symmetric: if A sees B, B sees A.")
	fmt.Println()

	var fovCells []hexxladb.CellRecord
	err = db.View(func(tx *hexxladb.Tx) error {
		opaque := func(c hexxladb.Coord) bool {
			p, e := hexxladb.Pack(c)
			if e != nil {
				return true
			}
			_, ok, _ := tx.GetCell(p)
			return !ok
		}
		var e error
		fovCells, e = tx.LoadContextFOV(ctx, center, 5, opaque, hexxladb.FOVContextConfig{
			MaxCells: 200,
		})
		return e
	})
	if err != nil {
		return fmt.Errorf("fov: %w", err)
	}

	// Count total cells in radius for comparison
	var totalInRadius int
	_ = db.View(func(tx *hexxladb.Tx) error {
		for _, coord := range hexxladb.WalkRings(nil, center, 5) {
			p, pErr := hexxladb.Pack(coord)
			if pErr != nil {
				return pErr
			}
			_, ok, _ := tx.GetCell(p)
			if ok {
				totalInRadius++
			}
		}
		return nil
	})

	printMetric("Radius", 5, "rings")
	printMetric("Total cells in radius", totalInRadius, "")
	printMetric("FOV-visible cells", len(fovCells), "")
	if totalInRadius > len(fovCells) {
		printMetric("Occluded (skipped)", totalInRadius-len(fovCells), "cells hidden behind gaps")
	}
	fmt.Println()

	_, _ = infoStyle.Println("  Sample visible cells:")
	for i, rec := range fovCells {
		if i >= 8 {
			_, _ = dimStyle.Printf("    ⋯  (%d more)\n", len(fovCells)-8)
			break
		}
		c, _ := hexxladb.Unpack(rec.Key)
		_, _ = dimStyle.Printf("    [%d] (%d,%d) dist=%d  ", i+1, c.Q, c.R, center.Distance(c))
		_, _ = dataStyle.Printf("%s\n", trunc(rec.RawContent, 45))
	}
	fmt.Println()
	printSuccess("FOV loading complete")
	fmt.Println()

	// ════════════════════════════════════════════════════════════════
	// PHASE 3: Large-radius context
	// ════════════════════════════════════════════════════════════════
	printHeader("Phase 3: Large-radius radial context (LoadContext)")
	printNote("LoadContext scans occupied cells nearest-first and stops at MaxCells.")
	printNote("Coordinates are never substituted or implicitly coarsened.")
	fmt.Println()

	var largeRadiusPack hexxladb.ContextPack
	err = db.View(func(tx *hexxladb.Tx) error {
		var e error
		largeRadiusPack, e = tx.LoadContext(ctx, hexxladb.LoadContextConfig{
			Seeds:    []hexxladb.Coord{center},
			MaxRing:  12,
			MaxCells: 100,
			Assembly: hexxladb.ContextAssemblyConfig{
				Assemble: hexxladb.DefaultAssembleCellViewOpts(),
			},
		})
		return e
	})
	if err != nil {
		return fmt.Errorf("large-radius context: %w", err)
	}

	printMetric("MaxRing", 12, "rings")
	printMetric("Cells loaded", len(largeRadiusPack.Cells), "")
	fmt.Println()

	_, _ = infoStyle.Println("  Loaded cells by distance from center:")
	ringBuckets := map[int]int{}
	for _, cv := range largeRadiusPack.Cells {
		d := center.Distance(cv.Coord)
		ringBuckets[d]++
	}
	for d := range 7 {
		if ringBuckets[d] > 0 {
			bar := strings.Repeat("▪", ringBuckets[d])
			_, _ = dimStyle.Printf("    dist=%d  %-20s %d cells\n", d, bar, ringBuckets[d])
		}
	}
	fmt.Println()
	printSuccess("Large-radius context loading complete")
	fmt.Println()

	// ════════════════════════════════════════════════════════════════
	// PHASE 4: Voronoi Partitioning
	// ════════════════════════════════════════════════════════════════
	printHeader("Phase 4: Voronoi Partitioning (LoadContextVoronoi)")
	printNote("Multi-source Dijkstra assigns each coordinate to the lowest-cost seed.")
	printNote("No overlap between regions — fair budget allocation per seed. Supports optional WeightFunc for non-uniform cost.")
	fmt.Println()

	voronoiSeeds := []hexxladb.Coord{
		{Q: -3, R: 0},
		{Q: 3, R: 0},
		{Q: 0, R: -3},
		{Q: 0, R: 3},
	}

	var voronoiRegions map[int][]hexxladb.CellRecord
	err = db.View(func(tx *hexxladb.Tx) error {
		var e error
		voronoiRegions, e = tx.LoadContextVoronoi(ctx, voronoiSeeds, hexxladb.VoronoiContextConfig{
			MaxRadius:       4,
			MaxCellsPerSeed: 32,
		})
		return e
	})
	if err != nil {
		return fmt.Errorf("voronoi: %w", err)
	}

	printMetric("Seeds", len(voronoiSeeds), "")
	totalVoronoi := 0
	for i, seed := range voronoiSeeds {
		region := voronoiRegions[i]
		totalVoronoi += len(region)
		_, _ = infoStyle.Printf("  Seed %d (%d,%d): %d cells\n", i+1, seed.Q, seed.R, len(region))
		for j, rec := range region {
			if j >= 3 {
				_, _ = dimStyle.Printf("      ⋯  (%d more)\n", len(region)-3)
				break
			}
			c, _ := hexxladb.Unpack(rec.Key)
			_, _ = dimStyle.Printf("      [%d] (%d,%d) ", j+1, c.Q, c.R)
			_, _ = dataStyle.Printf("%s\n", trunc(rec.RawContent, 42))
		}
	}
	printMetric("Total Voronoi cells", totalVoronoi, "(no overlaps)")
	fmt.Println()
	printSuccess("Voronoi partitioning complete")
	fmt.Println()

	// ════════════════════════════════════════════════════════════════
	// PHASE 5: Pathfinding
	// ════════════════════════════════════════════════════════════════
	printHeader("Phase 5: Pathfinding (Dijkstra / BFS / Edge Context)")
	printNote("Edges form a graph overlay. Dijkstra finds weighted shortest paths; BFS discovers reachable sets.")
	fmt.Println()

	// Dijkstra path
	printStep("FindEdgePath — weighted shortest path")
	if len(cells) > 50 {
		start, goal := cells[0], cells[50]
		_, _ = infoStyle.Printf("  Start: (%d,%d)  Goal: (%d,%d)\n", start.Q, start.R, goal.Q, goal.R)
		var path []hexxladb.Coord
		err = db.View(func(tx *hexxladb.Tx) error {
			var e error
			path, e = tx.FindEdgePath(ctx, start, goal, hexxladb.FindEdgePathConfig{MaxExpand: 500})
			return e
		})
		if err != nil {
			return fmt.Errorf("pathfind: %w", err)
		}
		if path != nil {
			printMetric("Path length", len(path), "hops")
			pathStr := make([]string, 0, len(path))
			for i, c := range path {
				if i > 8 {
					pathStr = append(pathStr, fmt.Sprintf("⋯ (%d more)", len(path)-9))
					break
				}
				pathStr = append(pathStr, fmt.Sprintf("(%d,%d)", c.Q, c.R))
			}
			_, _ = dimStyle.Printf("    %s\n", strings.Join(pathStr, " → "))
		} else {
			printNote("No edge path found between these cells")
		}
	}
	fmt.Println()

	// BFS reachability
	printStep("WalkEdges — BFS reachability")
	if len(cells) > 0 {
		bfsStart := cells[0]
		_, _ = infoStyle.Printf("  BFS from (%d,%d), max 3 hops:\n", bfsStart.Q, bfsStart.R)
		var reachable []hexxladb.Coord
		err = db.View(func(tx *hexxladb.Tx) error {
			var e error
			reachable, e = tx.WalkEdges(ctx, bfsStart, "", 3, 30)
			return e
		})
		if err != nil {
			return fmt.Errorf("bfs: %w", err)
		}
		printMetric("Reachable in 3 hops", len(reachable), "cells")
		for i, c := range reachable {
			if i >= 8 {
				_, _ = dimStyle.Printf("    ⋯  (%d more)\n", len(reachable)-8)
				break
			}
			_, _ = dimStyle.Printf("    [%d] (%d,%d)\n", i+1, c.Q, c.R)
		}
	}
	fmt.Println()

	// Edge-based context loading
	printStep("LoadContext with EdgeFilter — graph-aware context")
	if len(cells) > 0 {
		var edgePack hexxladb.ContextPack
		err = db.View(func(tx *hexxladb.Tx) error {
			var e error
			edgePack, e = tx.LoadContext(ctx, hexxladb.LoadContextConfig{
				Seeds:      []hexxladb.Coord{center},
				EdgeFilter: "",
				MaxHops:    4,
				MaxCells:   30,
				Assembly:   hexxladb.ContextAssemblyConfig{Assemble: hexxladb.DefaultAssembleCellViewOpts()},
			})
			return e
		})
		if err != nil {
			return fmt.Errorf("edge context: %w", err)
		}
		printMetric("Edge-connected cells", len(edgePack.Cells), "")
		for i, cv := range edgePack.Cells {
			if i >= 5 {
				_, _ = dimStyle.Printf("    ⋯  (%d more)\n", len(edgePack.Cells)-5)
				break
			}
			_, _ = dimStyle.Printf("    [%d] (%d,%d) ", i+1, cv.Coord.Q, cv.Coord.R)
			_, _ = dataStyle.Printf("%s\n", trunc(cv.RawContent, 45))
		}
	}
	fmt.Println()
	printSuccess("Pathfinding complete")
	fmt.Println()

	// ════════════════════════════════════════════════════════════════
	// PHASE 6: Comparison
	// ════════════════════════════════════════════════════════════════
	printHeader("Phase 6: Strategy Comparison")
	printNote("Same center, same radius — different algorithms, different results.")
	fmt.Println()

	// Radial
	var radialCells []hexxladb.CellRecord
	_ = db.View(func(tx *hexxladb.Tx) error {
		for _, coord := range hexxladb.WalkRings(nil, center, 5) {
			p, pErr := hexxladb.Pack(coord)
			if pErr != nil {
				return pErr
			}
			rec, ok, _ := tx.GetCell(p)
			if ok {
				radialCells = append(radialCells, rec)
			}
		}
		return nil
	})

	// Edge context
	var edgeCtxPack hexxladb.ContextPack
	_ = db.View(func(tx *hexxladb.Tx) error {
		edgeCtxPack, _ = tx.LoadContext(ctx, hexxladb.LoadContextConfig{
			Seeds:      []hexxladb.Coord{center},
			EdgeFilter: "",
			MaxHops:    5,
			MaxCells:   200,
			Assembly:   hexxladb.ContextAssemblyConfig{Assemble: hexxladb.DefaultAssembleCellViewOpts()},
		})
		return nil
	})

	_, _ = infoStyle.Println("  Retrieval strategy results (center=(0,0), r=5):")
	fmt.Println()

	strategies := []struct {
		name  string
		count int
		desc  string
	}{
		{"Radial (blind)", len(radialCells), "All occupied cells within radius — baseline"},
		{"FOV (visibility)", len(fovCells), "Shadowcast-filtered — skips occluded cells; symmetric"},
		{"Large-radius radial", len(largeRadiusPack.Cells), "Nearest-first with an explicit result limit"},
		{"Voronoi (4 seeds)", totalVoronoi, "Dijkstra regions — non-overlapping, cost-aware"},
		{"Edge-walk (graph)", len(edgeCtxPack.Cells), "BFS over edges — follows connections"},
	}

	maxCount := 0
	for _, s := range strategies {
		if s.count > maxCount {
			maxCount = s.count
		}
	}

	for _, s := range strategies {
		barLen := 0
		if maxCount > 0 {
			barLen = s.count * 30 / maxCount
		}
		bar := strings.Repeat("▪", barLen)
		_, _ = dimStyle.Printf("    %-22s ", s.name)
		_, _ = accentStyle.Printf("%-30s ", bar)
		_, _ = dataStyle.Printf("%3d cells\n", s.count)
		_, _ = dimStyle.Printf("    %24s%s\n", "", s.desc)
	}
	fmt.Println()

	_, _ = infoStyle.Println("  When to use each strategy:")
	_, _ = dimStyle.Println("    • Radial:  dense grids, all neighbors equally important")
	_, _ = dimStyle.Println("    • FOV:     sparse grids, symmetric shadowcasting skips occluded regions")
	_, _ = dimStyle.Println("    • Large:   larger radius with a strict nearest-first result limit")
	_, _ = dimStyle.Println("    • Voronoi: multi-seed Dijkstra, no duplicate context; supports weighted cost")
	_, _ = dimStyle.Println("    • Edges:   graph-structured data, follow relationships not proximity")
	fmt.Println()

	printSuccess("All strategies compared")
	fmt.Println()

	// ── Completion ──────────────────────────────────────────────
	_, _ = sepStyle.Println(strings.Repeat("═", lineW))
	_, _ = okStyle.Println("  ✓  HexxlaDB Spatial Algorithms Demo — complete")
	_, _ = sepStyle.Println(strings.Repeat("═", lineW))
	fmt.Println()
	_, _ = dimStyle.Printf("  Database:   %s\n", dbPath)
	_, _ = dimStyle.Printf("  Cells:      %d across 7 rings\n", len(cells))
	_, _ = dimStyle.Printf("  Edges:      %d (sequence + cross-ref)\n", edgeCount)
	_, _ = dimStyle.Printf("  Algorithms: FOV, radial context, Voronoi, Dijkstra, BFS, edge-context\n")
	_, _ = dimStyle.Println("  See docs/hexxladb/API_REFERENCE.md for the public API guide")
	fmt.Println()

	return nil
}
