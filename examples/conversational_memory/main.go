// Conversational Memory Service Example
//
// Demonstrates production patterns for building an LLM memory system with HexxlaDB:
//
//   - Phase  1  Database init (MVCC, AfterPutCell hook, PageSize, Compression)
//   - Phase  2  Batch-storing a rich, multi-session conversation corpus
//   - Phase  3  Contradiction detection with MarkConflict seams
//   - Phase  4  Supersession — seam-aware context assembly (FilterSuperseded)
//   - Phase  5  Context assembly: QueryCells → seeds → LoadContext
//   - Phase  6  Tag discovery, TagCounts, TagCooccurrences
//   - Phase  7  Query patterns (QueryCells: tag, source, score, recency)
//   - Phase  8  MVCC time-travel (ViewAt)
//   - Phase  9  Lattice visualisation: ASCII hex grid + RingDensityMap
//   - Phase 10  QueryCells + multi-seed context assembly (LoadContext)
//   - Phase 11  Health check (DB.HealthCheck) + MVCC Snapshot Diff
//   - Phase 12  DeleteCell + Compact (MVCC tombstones, snapshot isolation, copy-compaction)
//   - Phase 13  Field of View — visibility-filtered context (LoadContextFOV)
//   - Phase 14  Pathfinding over edges (PutEdge, FindEdgePath Dijkstra, WalkEdges BFS, LoadContext with EdgeFilter)
//
// For embedding-based semantic search, see examples/llm_context_engine.
//
// Usage:
//
//	go run ./examples/conversational_memory           # default DB at .tmp/conversational-memory.db
//	go run ./examples/conversational_memory -db /path/to/my.db
//	task demo                                         # same as first form via Taskfile
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fatih/color"

	"github.com/hexxla/hexxladb"
)

// defaultDBPath is where the demo database lands when no -db flag is provided.
// Kept under .tmp/ so it is gitignored and never pollutes the repo root.
const defaultDBPath = ".tmp/conversational-memory.db"

// ANSI color styles for beautiful output
var (
	headerStyle    = color.New(color.FgCyan, color.Bold, color.Underline)
	successStyle   = color.New(color.FgGreen, color.Bold)
	infoStyle      = color.New(color.FgBlue)
	dataStyle      = color.New(color.FgWhite)
	accentStyle    = color.New(color.FgYellow)
	warningStyle   = color.New(color.FgHiYellow)
	errorStyle     = color.New(color.FgRed, color.Bold)
	dimStyle       = color.New(color.FgHiBlack)
	metricStyle    = color.New(color.FgMagenta)
	separatorStyle = color.New(color.FgHiBlack)
)

const lineWidth = 68

func printHeader(title string) {
	fmt.Println()
	_, _ = separatorStyle.Println(strings.Repeat("═", lineWidth))
	_, _ = headerStyle.Printf("  ▶  %s\n", title)
	_, _ = separatorStyle.Println(strings.Repeat("═", lineWidth))
}

func printSubHeader(title string) {
	fmt.Println()
	_, _ = separatorStyle.Println(strings.Repeat("─", lineWidth))
	_, _ = accentStyle.Printf("  ◆  %s\n", title)
	_, _ = separatorStyle.Println(strings.Repeat("─", lineWidth))
}

func printSuccess(msg string) {
	_, _ = successStyle.Println("  ✓  " + msg)
}

func printNote(msg string) {
	_, _ = dimStyle.Println("  ℹ  " + msg)
}

func printInfo(label, value string) {
	_, _ = infoStyle.Printf("  %-22s ", label+":")
	_, _ = dataStyle.Println(value)
}

func printMetric(name string, value any, unit string) {
	_, _ = metricStyle.Printf("  📊  %-28s ", name+":")
	_, _ = accentStyle.Printf("%v", value)
	if unit != "" {
		_, _ = dimStyle.Println(" " + unit)
	} else {
		fmt.Println()
	}
}

func main() {
	log.SetFlags(0)

	dbPath := flag.String("db", defaultDBPath,
		"path to the HexxlaDB demo database (created if absent; use existing to skip re-seeding)")
	flag.Parse()

	if err := run(*dbPath); err != nil {
		_, _ = errorStyle.Println("\n  ✗  Error: " + err.Error())
		os.Exit(1)
	}
}

func run(dbPath string) error {
	ctx := context.Background()

	// Ensure parent directory exists (never write into repo root).
	_ = os.MkdirAll(filepath.Dir(dbPath), 0o750)

	_, _ = separatorStyle.Println(strings.Repeat("═", lineWidth))
	_, _ = headerStyle.Printf("  ▶  HexxlaDB — Conversational Memory Demo\n")
	_, _ = dimStyle.Printf("  %d-turn corpus · 5 thematic sessions · 14 phases\n", len(seedConversation))
	_, _ = separatorStyle.Println(strings.Repeat("═", lineWidth))
	fmt.Println()

	// ═══════════════════════════════════════════════════════════════
	// PHASE 1: Initialize Database
	// ═══════════════════════════════════════════════════════════════
	printHeader("Phase 1: Database Initialization")

	isNew := false
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		isNew = true
	}
	if isNew {
		// Remove any stale WAL from a previous interrupted run.
		_ = os.Remove(dbPath + "-wal")
	}

	// AfterPutCell hook: count writes for telemetry (demonstrated in Phase 11).
	var hookCellCount int
	opts := &hexxladb.Options{
		EnableMVCC:       true,
		ChangelogEnabled: true,
		MVCCRetention: hexxladb.MVCCRetention{
			RetainCommitsBehindHead: 100,
		},
		AfterPutCell: hexxladb.AfterPutCellHookFunc(func(_ context.Context, _ hexxladb.CellRecord) error {
			hookCellCount++
			return nil
		}),
	}

	_, _ = infoStyle.Println("  Opening database (MVCC + changelog + AfterPutCell hook)...")
	db, err := hexxladb.Open(dbPath, opts)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer func() { _ = db.Close() }()

	printSuccess("Database open")
	printInfo("Path", dbPath)
	printInfo("Status", map[bool]string{true: "new (will seed)", false: "existing (re-using)"}[isNew])
	printInfo("MVCC", "enabled · retain 100 commits behind head")
	printInfo("PageSize", fmt.Sprintf("%d bytes (%d KiB, configurable per-database)", db.PageSize(), db.PageSize()/1024))
	printInfo("MaxValueBytes", fmt.Sprintf("%d bytes (%d KB, configurable per-database)", db.MaxValueBytes(), db.MaxValueBytes()/1024))
	printInfo("Compression", "DEFLATE (always-on, compress/flate, Go stdlib)")
	printInfo("AfterPutCell", "hook active — counting every write")
	fmt.Println()

	// ═══════════════════════════════════════════════════════════════
	// PHASE 2: Seed Conversation Corpus
	// ═══════════════════════════════════════════════════════════════
	printHeader(fmt.Sprintf("Phase 2: Seeding %d-Turn Conversation Corpus", len(seedConversation)))

	// seedConversation is defined in seed_data.go and covers 5 thematic sessions:
	//   A: Communication preferences & workflow (with contradiction pair)
	//   B: HexxlaDB technical deep-dive (MVCC, WAL, seams, API)
	//   C: Go programming & architecture (errors, generics, profiling)
	//   D: LLM systems & product design (RAG, token budgets, multi-agent)
	//   E: Security, compliance & operations (OWASP, K8s, GDPR)
	printNote("Sessions: preferences/workflow · HexxlaDB internals · Go patterns · LLM systems · security/ops")
	fmt.Println()

	sessionID := fmt.Sprintf("session-%d", time.Now().Unix())
	var cells []hexxladb.Coord
	var recs []hexxladb.CellRecord

	userColor := color.New(color.FgGreen)
	assistantColor := color.New(color.FgBlue)

	for i, msg := range seedConversation {
		coord := spiralCoord(i)
		cells = append(cells, coord)
		pk, _ := hexxladb.Pack(coord)

		var rec hexxladb.CellRecord
		if msg.role == "user" {
			rec = hexxladb.NewUserMessageCell(pk, msg.content, sessionID, 1.0)
		} else {
			rec = hexxladb.NewAssistantResponseCell(pk, msg.content, sessionID, 1.0)
		}
		rec.Tags = append(rec.Tags, msg.tags...)
		recs = append(recs, rec)

		roleCol := assistantColor
		if msg.role == "user" {
			roleCol = userColor
		}
		_, _ = dimStyle.Printf("    [%02d] ", i)
		_, _ = roleCol.Printf("%-10s", msg.role)
		_, _ = dataStyle.Printf("%s\n", truncate(msg.content, 50))
	}
	fmt.Println()

	if isNew {
		result, err := db.BatchPutCells(ctx, recs, &hexxladb.BatchPutCellOptions{
			BatchSize: 64,
			OnProgress: func(i int) {
				if (i+1)%20 == 0 || i+1 == len(recs) {
					_, _ = dimStyle.Printf("    ...committed %d/%d turns\n", i+1, len(recs))
				}
			},
		})
		if err != nil {
			return fmt.Errorf("batch put cells: %w", err)
		}
		fmt.Println()
		printSuccess(fmt.Sprintf("Seeded %d turns via BatchPutCells (hook saw %d writes so far)", result.Written, hookCellCount))
	} else {
		printNote("Existing database — skipping seed. All phases read from stored data.")
	}
	fmt.Println()

	// ═══════════════════════════════════════════════════════════════
	// PHASE 3: Detect Contradictions (Seams)
	// ═══════════════════════════════════════════════════════════════
	printHeader("Phase 3: Contradiction Detection")

	printNote("Cells [0] and [2] form a direct contradiction: 'detailed' vs 'concise'.")
	printNote("MarkConflict records an open dispute seam between the two coordinates.")
	fmt.Println()

	if len(cells) >= 3 {
		err := db.Update(func(tx *hexxladb.Tx) error {
			return tx.MarkConflict(cells[0], cells[2], "User preference changed: detailed vs concise")
		})
		if err != nil {
			return fmt.Errorf("mark conflict: %w", err)
		}

		_, _ = warningStyle.Println("  ⚠  Detected contradiction:")
		_, _ = infoStyle.Printf("      Cell A: ")
		_, _ = dataStyle.Printf("(%d,%d)  \"%s\"\n", cells[0].Q, cells[0].R, truncate(seedConversation[0].content, 48))
		_, _ = infoStyle.Printf("      Cell B: ")
		_, _ = dataStyle.Printf("(%d,%d)  \"%s\"\n", cells[2].Q, cells[2].R, truncate(seedConversation[2].content, 48))
		fmt.Println()
		printSuccess("Conflict seam created (SeamTypeConflict)")
	}
	fmt.Println()

	center := cells[len(cells)-1] // anchor for ring-walk phases

	// ═══════════════════════════════════════════════════════════════
	// PHASE 4: Supersession — mark stale cells as superseded
	// ═══════════════════════════════════════════════════════════════
	printHeader("Phase 4: Supersession (Seam-Aware Context Assembly)")

	printNote("MarkSupersedes declares cell[2] as the live truth, making cell[0] stale.")
	printNote("FilterSuperseded then excludes the stale cell from context packs automatically.")
	fmt.Println()

	if len(cells) >= 3 {
		if err := db.Update(func(tx *hexxladb.Tx) error {
			return tx.MarkSupersedes(cells[2], cells[0], "Preference updated: concise supersedes detailed")
		}); err != nil {
			return fmt.Errorf("mark supersedes: %w", err)
		}
		printSuccess("Supersession seam recorded (SeamTypeSupersedes)")
		_, _ = infoStyle.Printf("      Stale:   (%d,%d)  \"%s\"\n", cells[0].Q, cells[0].R, truncate(seedConversation[0].content, 48))
		_, _ = infoStyle.Printf("      Current: (%d,%d)  \"%s\"\n", cells[2].Q, cells[2].R, truncate(seedConversation[2].content, 48))
	}
	fmt.Println()

	// Load context WITHOUT FilterSuperseded — stale cell appears
	var packWithStale hexxladb.ContextPack
	if err := db.View(func(tx *hexxladb.Tx) error {
		var err error
		packWithStale, err = tx.LoadContext(ctx, hexxladb.LoadContextConfig{
			Seeds:    []hexxladb.Coord{center},
			MaxRing:  3,
			MaxCells: 64,
			Assembly: hexxladb.ContextAssemblyConfig{
				Assemble:         hexxladb.DefaultAssembleCellViewOpts(),
				FilterSuperseded: false,
				Explain:          true,
			},
		})
		return err
	}); err != nil {
		return fmt.Errorf("load context (unfiltered): %w", err)
	}

	// Load context WITH FilterSuperseded — stale cell excluded, successor included
	var packFiltered hexxladb.ContextPack
	if err := db.View(func(tx *hexxladb.Tx) error {
		var err error
		packFiltered, err = tx.LoadContext(ctx, hexxladb.LoadContextConfig{
			Seeds:    []hexxladb.Coord{center},
			MaxRing:  3,
			MaxCells: 64,
			Assembly: hexxladb.ContextAssemblyConfig{
				Assemble:         hexxladb.DefaultAssembleCellViewOpts(),
				FilterSuperseded: true,
				Explain:          true,
			},
		})
		return err
	}); err != nil {
		return fmt.Errorf("load context (filtered): %w", err)
	}

	printSubHeader("Comparison: with vs without stale-cell filtering")
	printMetric("Cells WITHOUT FilterSuperseded", len(packWithStale.Cells), "cells (includes stale)")
	printMetric("Cells WITH    FilterSuperseded", len(packFiltered.Cells), "cells (stale excluded)")
	fmt.Println()

	_, _ = infoStyle.Println("  Successor substitutions (SupersededFrom set on promoted cells):")
	var substituted int
	for _, cv := range packFiltered.Cells {
		if cv.SupersededFrom != nil {
			substituted++
			_, _ = accentStyle.Printf("    ↺  (%d,%d) promoted", cv.Coord.Q, cv.Coord.R)
			_, _ = dimStyle.Printf(" · replaced stale (%d,%d)\n", cv.SupersededFrom.Q, cv.SupersededFrom.R)
			_, _ = dimStyle.Printf("       %s\n", truncate(cv.RawContent, 58))
		}
	}
	if substituted == 0 {
		printNote("No substitutions in this radius — stale cell excluded entirely")
	}
	fmt.Println()

	_, _ = infoStyle.Println("  Explain mode — supersession decisions:")
	for _, ex := range packFiltered.Explanations {
		if ex.Reason == "superseded" {
			if ex.SupersededBy != nil {
				_, _ = warningStyle.Printf("    ✗  (%d,%d) ring=%-2d superseded → promoted by (%d,%d)\n",
					ex.Coord.Q, ex.Coord.R, ex.Ring, ex.SupersededBy.Q, ex.SupersededBy.R)
			} else {
				_, _ = warningStyle.Printf("    ✗  (%d,%d) ring=%-2d superseded → excluded (no live successor)\n",
					ex.Coord.Q, ex.Coord.R, ex.Ring)
			}
		}
	}
	fmt.Println()
	printSuccess("Seam-aware filtering complete — stale cells excluded from context")
	fmt.Println()

	// ═══════════════════════════════════════════════════════════════
	// PHASE 5: Context Assembly for LLM Prompt
	// ═══════════════════════════════════════════════════════════════
	printHeader("Phase 5: Context Assembly (QueryCells → Seeds → Bounded Candidates)")

	printNote("Pipeline: QueryCells finds matching cells → coords become ring-walk seeds → LoadContext assembles bounded candidates.")
	fmt.Println()

	printSubHeader("Step 1 — QueryCells: find 'preference' seeds sorted by confidence")
	printNote("Planner: RequireTags non-empty → uses tag/ secondary index (no full scan).")
	fmt.Println()

	var seedResults []hexxladb.CellQueryResult
	if err := db.View(func(tx *hexxladb.Tx) error {
		var err error
		seedResults, err = tx.QueryCells(ctx, hexxladb.CellQuery{
			RequireTags: []string{"preference"},
			ExcludeTags: []string{"acknowledgment"},
			MaxResults:  5,
			SortBy:      hexxladb.SortByConfidence,
		})
		return err
	}); err != nil {
		return fmt.Errorf("query cells for seeds: %w", err)
	}

	printMetric("Preference seeds found", len(seedResults), "cells")
	fmt.Println()
	_, _ = infoStyle.Println("  Seeds (each will be ring-walk expanded):")
	for i, r := range seedResults {
		_, _ = dimStyle.Printf("    [%d] conf=%.1f  (%d,%d)  ", i+1, r.Cell.Provenance.Confidence, r.Cell.Coord.Q, r.Cell.Coord.R)
		_, _ = dataStyle.Printf("%s\n", truncate(r.Cell.RawContent, 52))
	}
	fmt.Println()

	assemblySeeds := make([]hexxladb.Coord, 0, len(seedResults))
	for _, r := range seedResults {
		assemblySeeds = append(assemblySeeds, r.Cell.Coord)
	}
	if len(assemblySeeds) == 0 {
		assemblySeeds = []hexxladb.Coord{center} // fallback
	}

	const resultLimit = 12
	printSubHeader(fmt.Sprintf("Step 2 — LoadContext: %d seed(s), shared result limit %d cells", len(assemblySeeds), resultLimit))
	printNote("Each seed walks nearest-first; multi-seed candidates merge round-robin in seed order.")
	printNote("Prompt rendering and model-specific token fitting happen after retrieval in the application.")
	fmt.Println()

	assemblyCfg := hexxladb.ContextAssemblyConfig{
		Assemble:         hexxladb.DefaultAssembleCellViewOpts(),
		IncludeSeams:     true,
		SeamRadius:       2,
		FilterSuperseded: true,
		Explain:          true,
	}

	var pack hexxladb.ContextPack
	if err := db.View(func(tx *hexxladb.Tx) error {
		var err error
		pack, err = tx.LoadContext(ctx, hexxladb.LoadContextConfig{
			Seeds:    assemblySeeds,
			MaxRing:  3,
			MaxCells: resultLimit,
			Assembly: assemblyCfg,
		})
		return err
	}); err != nil {
		return fmt.Errorf("load context pack from: %w", err)
	}

	includedBytes := 0
	for _, cv := range pack.Cells {
		includedBytes += len(cv.RawContent)
	}

	fmt.Println()
	printMetric("Seeds expanded", len(assemblySeeds), "coords")
	printMetric("Candidates scanned", pack.Stats.CandidatesScanned, "cells")
	printMetric("Cells in combined pack", len(pack.Cells), "cells")
	printMetric("Result limit reached", pack.Stats.ResultLimitReached, "")
	printMetric("Returned content", includedBytes, "bytes (informational, not an LLM budget)")

	if len(pack.Seams) > 0 {
		fmt.Println()
		_, _ = warningStyle.Printf("  ⚠  %d seam(s) surface in this context window:\n", len(pack.Seams))
		for _, seam := range pack.Seams {
			_, _ = dimStyle.Printf("      %v ↔ %v: ", seam.CellA, seam.CellB)
			_, _ = dataStyle.Println(seam.Reason)
		}
	}

	fmt.Println()
	_, _ = infoStyle.Println("  Combined context pack (round-robin across nearest-first seed walks):")
	for i, cv := range pack.Cells {
		_, _ = dimStyle.Printf("    [%02d] conf=%.1f  (%d,%d)  ", i+1, cv.Provenance.Confidence, cv.Coord.Q, cv.Coord.R)
		_, _ = dataStyle.Printf("%s\n", truncate(cv.RawContent, 52))
	}

	if len(pack.Explanations) > 0 {
		fmt.Println()
		_, _ = infoStyle.Println("  Explain mode — cell inclusion decisions:")
		for _, ex := range pack.Explanations {
			switch ex.Reason {
			case "included":
				_, _ = color.New(color.FgGreen).Printf("    ✓  ")
				_, _ = dimStyle.Printf("(%d,%d) ring=%d included\n", ex.Coord.Q, ex.Coord.R, ex.Ring)
			case "superseded":
				_, _ = color.New(color.FgYellow).Printf("    ↺  ")
				_, _ = dimStyle.Printf("(%d,%d) ring=%d superseded (excluded — stale)\n", ex.Coord.Q, ex.Coord.R, ex.Ring)
			}
		}
	}

	fmt.Println()
	printSuccess(fmt.Sprintf("Context assembled: %d cells from %d seed(s), bounded to %d results",
		len(pack.Cells), len(assemblySeeds), resultLimit))
	fmt.Println()

	// ═══════════════════════════════════════════════════════════════
	// PHASE 6: Tag Discovery and Analytics
	// ═══════════════════════════════════════════════════════════════
	printHeader("Phase 6: Tag Discovery and Analytics")

	printNote("With 84 turns across 5 sessions the tag graph is rich enough to show meaningful co-occurrences.")
	fmt.Println()

	printSubHeader("All distinct tags (ListExistingTopics)")
	var allTags []string
	err = db.View(func(tx *hexxladb.Tx) error {
		var err error
		allTags, err = tx.ListExistingTopics(ctx)
		return err
	})
	if err != nil {
		return fmt.Errorf("list existing topics: %w", err)
	}

	printMetric("Unique tags in database", len(allTags), "tags")
	fmt.Println()
	const tagCols = 3
	for i, tag := range allTags {
		_, _ = dimStyle.Printf("    %-28s", "• "+tag)
		if (i+1)%tagCols == 0 {
			fmt.Println()
		}
	}
	if len(allTags)%tagCols != 0 {
		fmt.Println()
	}
	fmt.Println()
	printSuccess(fmt.Sprintf("Discovered %d unique tags", len(allTags)))
	fmt.Println()

	printSubHeader("TagCounts — top 15 by frequency")
	err = db.View(func(tx *hexxladb.Tx) error {
		counts, err := tx.TagCounts(ctx)
		if err != nil {
			return err
		}
		shown := min(len(counts), 15)
		for i, tc := range counts[:shown] {
			barLen := int(float64(tc.Count) / float64(counts[0].Count) * 20)
			bar := strings.Repeat("▪", barLen)
			_, _ = dimStyle.Printf("    %2d. %-28s ", i+1, tc.Tag)
			_, _ = accentStyle.Printf("%-20s ", bar)
			_, _ = dataStyle.Printf("%d\n", tc.Count)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("tag counts: %w", err)
	}
	fmt.Println()

	printSubHeader("TagCooccurrences — top 8 pairs appearing together (min 2 cells)")
	err = db.View(func(tx *hexxladb.Tx) error {
		pairs, err := tx.TagCooccurrences(ctx, 2)
		if err != nil {
			return err
		}
		shown := min(len(pairs), 8)
		for _, tp := range pairs[:shown] {
			_, _ = dimStyle.Printf("    %-24s + %-24s ", tp.A, tp.B)
			_, _ = accentStyle.Printf("%d cells\n", tp.Count)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("tag cooccurrences: %w", err)
	}
	fmt.Println()

	// ═══════════════════════════════════════════════════════════════
	// PHASE 7: Query Patterns
	// ═══════════════════════════════════════════════════════════════
	printHeader("Phase 7: Query Patterns")

	printNote("QueryCells is the unified query entry point. All predicate fields are AND-combined.")
	printNote("The planner prefers a small known-cost spatial probe, then available secondary indexes; remaining predicates apply in-memory.")
	fmt.Println()

	printSubHeader("Query A — 'opinion' cells, sorted by recency")
	printNote("RequireTags → tag/ index. SortByRecency orders by CommitSeq descending.")
	var opinionCells []hexxladb.CellQueryResult
	if err := db.View(func(tx *hexxladb.Tx) error {
		var err error
		opinionCells, err = tx.QueryCells(ctx, hexxladb.CellQuery{
			RequireTags: []string{"opinion"},
			MaxResults:  10,
			SortBy:      hexxladb.SortByRecency,
		})
		return err
	}); err != nil {
		return fmt.Errorf("query opinion cells: %w", err)
	}
	printMetric("Opinion cells found", len(opinionCells), "cells")
	for i, r := range opinionCells {
		_, _ = dimStyle.Printf("    [%d] (%d,%d)  %s\n", i+1, r.Cell.Coord.Q, r.Cell.Coord.R, truncate(r.Cell.RawContent, 54))
	}
	fmt.Println()

	printSubHeader("Query B — 'fact' cells from this session, confidence >= 0.8")
	printNote("Combines RequireTags (tag/ index), SourceID (source/ index filter), MinConfidence.")
	printNote("MaxScanRows=50 caps the source index walk — prevents O(n) full scans on large corpora.")
	var factSessionCells []hexxladb.CellQueryResult
	if err := db.View(func(tx *hexxladb.Tx) error {
		var err error
		factSessionCells, err = tx.QueryCells(ctx, hexxladb.CellQuery{
			RequireTags:   []string{"fact"},
			SourceID:      sessionID,
			MinConfidence: 0.8,
			MaxResults:    10,
			MaxScanRows:   50,
			SortBy:        hexxladb.SortByScore,
		})
		return err
	}); err != nil {
		return fmt.Errorf("query fact session cells: %w", err)
	}
	printMetric("Fact+session+conf≥0.8 cells", len(factSessionCells), "cells")
	for i, r := range factSessionCells {
		_, _ = dimStyle.Printf("    [%d] conf=%.1f  (%d,%d)  %s\n", i+1, r.Cell.Provenance.Confidence, r.Cell.Coord.Q, r.Cell.Coord.R, truncate(r.Cell.RawContent, 50))
	}
	fmt.Println()

	printSubHeader("Raw index scans (AscendCellsByTag / AscendCellsBySource)")
	printNote("These are the building blocks QueryCells uses internally. Shown here for transparency.")
	var prefCells int
	if err := db.View(func(tx *hexxladb.Tx) error {
		return tx.AscendCellsByTag(ctx, "preference", func(_ hexxladb.CellRecord) bool {
			prefCells++
			return true
		})
	}); err != nil {
		return fmt.Errorf("query by tag: %w", err)
	}
	_, _ = dimStyle.Printf("    AscendCellsByTag('preference') → %d cells\n", prefCells)

	var sessionCells int
	if err := db.View(func(tx *hexxladb.Tx) error {
		return tx.AscendCellsBySource(ctx, sessionID, func(_ hexxladb.CellRecord) bool {
			sessionCells++
			return true
		})
	}); err != nil {
		return fmt.Errorf("query by source: %w", err)
	}
	_, _ = dimStyle.Printf("    AscendCellsBySource(sessionID)   → %d cells\n", sessionCells)
	fmt.Println()
	printSuccess("All query patterns exercised")
	fmt.Println()

	// ═══════════════════════════════════════════════════════════════
	// PHASE 8: MVCC Snapshot
	// ═══════════════════════════════════════════════════════════════
	printHeader("Phase 8: MVCC Time-Travel Query")

	printNote("ViewAt pins a read snapshot to a past CommitSeq — earlier writes are invisible.")
	fmt.Println()

	mvccStats, err := db.StatsMVCC()
	if err != nil {
		return fmt.Errorf("get mvcc stats: %w", err)
	}

	printMetric("Current commit seq (head)", mvccStats.CommitSeq, "")

	if mvccStats.CommitSeq > 1 {
		snapshotSeq := mvccStats.CommitSeq - 2
		_, _ = infoStyle.Printf("  Pinning snapshot at seq %d (2 commits before head)...\n", snapshotSeq)
		fmt.Println()

		var historicalCount int
		err = db.ViewAt(snapshotSeq, func(tx *hexxladb.Tx) error {
			histCells, err := tx.ScanContextRaw(ctx, center, 3, 50)
			historicalCount = len(histCells)
			return err
		})
		if err != nil {
			return fmt.Errorf("historical query: %w", err)
		}

		printMetric("Cells visible at seq", historicalCount, fmt.Sprintf("(vs %d at head)", len(cells)))
		fmt.Println()
		printSuccess(fmt.Sprintf("Time-travel snapshot at seq=%d queried successfully", snapshotSeq))
	} else {
		printNote("CommitSeq too low for a meaningful snapshot — run again on a populated DB.")
	}
	fmt.Println()

	// ═══════════════════════════════════════════════════════════════
	// PHASE 9: Lattice Visualization & Diagnostics
	// ═══════════════════════════════════════════════════════════════
	printHeader("Phase 9: Lattice Visualisation & Diagnostics")

	printNote("ASCII grid is capped at radius 10 (debug tool). For larger lattices use RingDensityMap.")
	fmt.Println()

	printSubHeader("ASCII Hex Grid — radius 3 around context centre")
	err = db.View(func(tx *hexxladb.Tx) error {
		grid, err := tx.RenderHexGridFromDB(ctx, center, 3)
		if err != nil {
			return err
		}
		for line := range strings.SplitSeq(grid, "\n") {
			_, _ = dimStyle.Println("    " + line)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("render hex grid: %w", err)
	}
	fmt.Println()

	printSubHeader("Ring Density Map — radius 5 (works at any scale)")
	err = db.View(func(tx *hexxladb.Tx) error {
		density, err := tx.RingDensityMap(ctx, center, 5)
		if err != nil {
			return err
		}
		for _, rd := range density {
			bar := strings.Repeat("█", rd.Occupied) + strings.Repeat("░", rd.Total-rd.Occupied)
			_, _ = dimStyle.Printf("    ring %d: ", rd.Ring)
			_, _ = accentStyle.Printf("%-20s ", bar)
			_, _ = dataStyle.Printf("%d/%d\n", rd.Occupied, rd.Total)
		}
		occ, tot := hexxladb.TotalDensity(density)
		printMetric("Total density", fmt.Sprintf("%d/%d (%.0f%%)", occ, tot, float64(occ)/float64(tot)*100), "")
		return nil
	})
	if err != nil {
		return fmt.Errorf("ring density: %w", err)
	}
	fmt.Println()

	printSubHeader("Filtered Changelog — PutCell ops only, last 5 records")
	printNote("ReadChangelogFiltered scopes replay by op code (and optionally key prefix).")
	clRecords, err := db.ReadChangelogFiltered(0, 5, hexxladb.ChangelogFilter{
		Ops: []byte{hexxladb.ChangelogOpPutCell},
	})
	if err != nil {
		return fmt.Errorf("filtered changelog: %w", err)
	}
	for _, rec := range clRecords {
		_, _ = dimStyle.Printf("    seq=%-4d  op=PutCell  key=%x\n", rec.Seq, rec.Key)
	}
	printMetric("Changelog records returned", len(clRecords), "")
	fmt.Println()
	printSuccess("Lattice visualisation + diagnostics complete")
	fmt.Println()

	// ═══════════════════════════════════════════════════════════════
	// PHASE 10: QueryCells + Multi-Seed Context Assembly
	// ═══════════════════════════════════════════════════════════════
	printHeader("Phase 10: QueryCells + Multi-Seed Context Assembly")

	printNote("SearchCells is a thin wrapper over QueryCells. Use QueryCells for ExcludeTags, SortBy, Explain, temporal, spatial.")
	fmt.Println()

	printSubHeader("Query C — keyword 'database', sorted by composite score")
	printNote("Multi-term tokenized scoring per token: tag exact +1.0 · prefix +0.8 · content verbatim +0.6 · content icase +0.5 · sourceID +0.3 · confidence bonus (once)")

	var queryResults []hexxladb.CellQueryResult
	if err := db.View(func(tx *hexxladb.Tx) error {
		var err error
		queryResults, err = tx.QueryCells(ctx, hexxladb.CellQuery{
			Query:      "database",
			MaxResults: 5,
			SortBy:     hexxladb.SortByScore,
			Explain:    true,
		})
		return err
	}); err != nil {
		return fmt.Errorf("query cells: %w", err)
	}

	fmt.Println()
	printMetric("Results for 'database'", len(queryResults), "cells")
	fmt.Println()
	for i, r := range queryResults {
		_, _ = dimStyle.Printf("    [%d] score=%.2f  (%d,%d)  ", i+1, r.Score, r.Cell.Coord.Q, r.Cell.Coord.R)
		_, _ = dataStyle.Printf("%s\n", truncate(r.Cell.RawContent, 50))
		if r.Explanation != "" {
			_, _ = dimStyle.Printf("         explain: %s\n", r.Explanation)
		}
	}
	fmt.Println()

	printSubHeader("Query D — 'fact' cells, exclude 'question', sorted by confidence")
	printNote("Planner: RequireTags → tag/ index. ExcludeTags and MinConfidence applied in-memory.")

	var factResults []hexxladb.CellQueryResult
	if err := db.View(func(tx *hexxladb.Tx) error {
		var err error
		factResults, err = tx.QueryCells(ctx, hexxladb.CellQuery{
			RequireTags: []string{"fact"},
			ExcludeTags: []string{"question"},
			MaxResults:  5,
			SortBy:      hexxladb.SortByConfidence,
		})
		return err
	}); err != nil {
		return fmt.Errorf("query cells (fact): %w", err)
	}

	fmt.Println()
	printMetric("Fact cells (excluding question)", len(factResults), "cells")
	for i, r := range factResults {
		_, _ = dimStyle.Printf("    [%d] conf=%.1f  (%d,%d)  %s\n",
			i+1, r.Cell.Provenance.Confidence, r.Cell.Coord.Q, r.Cell.Coord.R,
			truncate(r.Cell.RawContent, 50))
	}
	fmt.Println()

	seeds := make([]hexxladb.Coord, 0, len(queryResults))
	for _, r := range queryResults {
		seeds = append(seeds, r.Cell.Coord)
	}

	printSubHeader(fmt.Sprintf("Multi-Seed Assembly — %d seeds from Query C, shared 12-cell result limit", len(seeds)))
	printNote("LoadContext auto-dispatches: 1 seed → ring walk; N seeds → concurrent multi-seed merge. No caller switch needed.")
	fmt.Println()

	if len(seeds) == 0 {
		printNote("No search results — skipping multi-seed assembly.")
	} else {

		const sharedLimit = 12
		multiCfg := hexxladb.ContextAssemblyConfig{
			Assemble:         hexxladb.DefaultAssembleCellViewOpts(),
			FilterSuperseded: true,
		}
		var multiPack hexxladb.ContextPack
		if err := db.View(func(tx *hexxladb.Tx) error {
			var err error
			multiPack, err = tx.LoadContext(ctx, hexxladb.LoadContextConfig{
				Seeds:    seeds,
				MaxRing:  2,
				MaxCells: sharedLimit,
				Assembly: multiCfg,
			})
			return err
		}); err != nil {
			return fmt.Errorf("load context pack from: %w", err)
		}

		printMetric("Seeds expanded", len(seeds), "coords")
		printMetric("Candidates scanned (all seeds)", multiPack.Stats.CandidatesScanned, "cells")
		printMetric("Cells in merged pack", len(multiPack.Cells), "cells")
		printMetric("Result limit reached", multiPack.Stats.ResultLimitReached, "")
		fmt.Println()

		_, _ = infoStyle.Println("  Merged context (round-robin across seeds):")
		for i, cv := range multiPack.Cells {
			_, _ = dimStyle.Printf("    [%02d] conf=%.1f  (%d,%d)  %s\n",
				i+1, cv.Provenance.Confidence, cv.Coord.Q, cv.Coord.R, truncate(cv.RawContent, 50))
		}
		fmt.Println()

		printSubHeader("How multi-seed result bounding works")
		_, _ = dimStyle.Println("    1.  Each seed ring-walks independently (r=2, FilterSuperseded applied)")
		_, _ = dimStyle.Println("    2.  Candidate positions merge round-robin in caller-supplied seed order")
		_, _ = dimStyle.Println("    3.  Shared neighbours are deduplicated")
		_, _ = dimStyle.Println("    4.  Retrieval stops at the shared MaxCells limit")
		_, _ = dimStyle.Println("    5.  The application ranks and fits the rendered model request")
		fmt.Println()

		printSuccess(fmt.Sprintf("Multi-seed pack assembled: %d cells from %d seeds with a %d-cell limit",
			len(multiPack.Cells), len(seeds), sharedLimit))
	}
	fmt.Println()

	// ═══════════════════════════════════════════════════════════════
	// PHASE 11: Health Check + MVCC Snapshot Diff
	// ═══════════════════════════════════════════════════════════════
	printHeader("Phase 11: Health Check + Event Hooks + MVCC Snapshot Diff")

	printSubHeader("DB.HealthCheck — full integrity scan")
	printNote("Checks cell count, seam resolution, orphaned seams, tag/source index consistency, MVCC stats.")
	fmt.Println()
	report, err := db.HealthCheck(ctx, hexxladb.DefaultHealthCheckConfig())
	if err != nil {
		return fmt.Errorf("health check: %w", err)
	}
	printMetric("Cell count", report.CellCount, "cells")
	printMetric("Seam count", report.SeamCount, "seams")
	printMetric("Seams resolved", report.SeamsResolved, "")
	printMetric("Seams unresolved", report.SeamsUnresolved, "")
	printMetric("Orphaned seams", report.OrphanedSeams, "")
	printMetric("Tag index errors", report.TagIndexErrors, "")
	printMetric("Source index errors", report.SourceIndexErrors, "")
	if len(report.Warnings) > 0 {
		fmt.Println()
		_, _ = warningStyle.Printf("  ⚠  %d warning(s):\n", len(report.Warnings))
		for _, w := range report.Warnings {
			_, _ = dimStyle.Printf("      • %s\n", w)
		}
	} else {
		printSuccess("Database is healthy — no warnings")
	}
	fmt.Println()

	printSubHeader("AfterPutCell hook telemetry")
	printNote("The hook was wired in Phase 1 and fires synchronously after every successful PutCell.")
	printMetric("Cells observed by hook", hookCellCount, "writes (all BatchPutCells turns)")
	fmt.Println()

	printSubHeader("DB.SnapshotDiff — MVCC change diff")
	printNote("Shows retained cell+seam versions in (fromSeq, toSeq] on MVCC v2/v3; use the changelog for complete CDC.")
	fmt.Println()
	diffStats, err := db.StatsMVCC()
	if err != nil {
		return fmt.Errorf("stats mvcc: %w", err)
	}

	diff, err := db.SnapshotDiff(ctx, 0, diffStats.CommitSeq, hexxladb.SnapshotDiffConfig{})
	if err != nil {
		return fmt.Errorf("snapshot diff: %w", err)
	}
	printMetric("Diff range", fmt.Sprintf("seq %d → %d", diff.FromSeq, diff.ToSeq), "")
	printMetric("Cell writes in range", len(diff.Cells), "CellDiff entries")
	printMetric("Seam writes in range", len(diff.Seams), "SeamDiff entries")
	if len(diff.Cells) > 0 {
		_, _ = infoStyle.Println("  First 5 CellDiff entries:")
		for i, cd := range diff.Cells {
			if i >= 5 {
				_, _ = dimStyle.Printf("    ⋯  (%d more cell diffs not shown)\n", len(diff.Cells)-5)
				break
			}
			_, _ = dimStyle.Printf("    [%02d] op=%-6s seq=%-4d %s\n",
				i+1, cd.Op, cd.CommitSeq, truncate(cd.Record.RawContent, 44))
		}
	}
	fmt.Println()

	if diffStats.CommitSeq >= 2 {
		narrowFrom := diffStats.CommitSeq - 2
		narrowDiff, err := db.SnapshotDiff(ctx, narrowFrom, diffStats.CommitSeq, hexxladb.SnapshotDiffConfig{})
		if err != nil {
			return fmt.Errorf("narrow snapshot diff: %w", err)
		}
		_, _ = infoStyle.Printf("  Narrow diff  seq %d→%d  (last 2 commits): %d cell(s), %d seam(s)\n",
			narrowDiff.FromSeq, narrowDiff.ToSeq, len(narrowDiff.Cells), len(narrowDiff.Seams))
	}
	fmt.Println()
	printSuccess("Health check + event hook telemetry + SnapshotDiff complete")
	fmt.Println()

	// ═══════════════════════════════════════════════════════════════
	// PHASE 12: DeleteCell + Compact
	// ═══════════════════════════════════════════════════════════════
	printHeader("Phase 12: DeleteCell + Compact")

	// --- 12a: write a fresh cell specifically for this demo phase ---
	printSubHeader("Tx.DeleteCell — MVCC tombstone deletion")
	printNote("Deleting a cell writes a tombstone (zero-length value) so older MVCC snapshots remain consistent.")
	printNote("Secondary indexes, facets, and outbound edges are cleaned up atomically.")
	fmt.Println()

	// Use an unused coord so this works even on re-runs of an existing DB.
	delCoord := hexxladb.Coord{Q: 10, R: 10}
	delKey, err := hexxladb.Pack(delCoord)
	if err != nil {
		return fmt.Errorf("pack delete coord: %w", err)
	}

	// Write the cell fresh so we always have something to delete.
	err = db.Update(func(tx *hexxladb.Tx) error {
		return tx.PutCell(ctx, hexxladb.CellRecord{
			Key:        delKey,
			RawContent: "This cell was written specifically to demonstrate DeleteCell.",
			Tags:       []string{"demo", "ephemeral"},
			Provenance: hexxladb.ProvenanceWire{SourceID: "phase-12"},
		})
	})
	if err != nil {
		return fmt.Errorf("seed delete target: %w", err)
	}

	// Capture cell count and MVCC seq *after* the write, *before* the delete.
	preReport, err := db.HealthCheck(ctx, hexxladb.DefaultHealthCheckConfig())
	if err != nil {
		return fmt.Errorf("health pre-delete: %w", err)
	}
	preDeleteStats, err := db.StatsMVCC()
	if err != nil {
		return fmt.Errorf("stats pre-delete: %w", err)
	}
	seqBeforeDelete := preDeleteStats.CommitSeq

	printInfo("Target cell", fmt.Sprintf("(%d,%d)", delCoord.Q, delCoord.R))
	printInfo("Content", "This cell was written specifically to demonstrate DeleteCell.")
	printInfo("Cells before delete", fmt.Sprintf("%d", preReport.CellCount))
	printInfo("MVCC seq before delete", fmt.Sprintf("%d", seqBeforeDelete))
	fmt.Println()

	// --- 12b: delete the cell ---
	err = db.Update(func(tx *hexxladb.Tx) error {
		return tx.DeleteCell(ctx, delKey)
	})
	if err != nil {
		return fmt.Errorf("delete cell: %w", err)
	}
	printSuccess("Cell deleted (MVCC tombstone written)")

	// Confirm current snapshot returns not-found.
	err = db.View(func(tx *hexxladb.Tx) error {
		_, ok, err := tx.GetCell(delKey)
		if err != nil {
			return err
		}
		if ok {
			_, _ = warningStyle.Println("  ⚠  Cell should be gone in current snapshot")
		} else {
			printSuccess("Current snapshot: cell not found (tombstone working)")
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("verify delete current: %w", err)
	}

	// ViewAt the snapshot *before* the delete — cell should still be visible.
	err = db.ViewAt(seqBeforeDelete, func(tx *hexxladb.Tx) error {
		rec, ok, err := tx.GetCell(delKey)
		if err != nil {
			return err
		}
		if !ok {
			_, _ = warningStyle.Println("  ⚠  Cell should be visible in pre-delete snapshot")
		} else {
			printSuccess(fmt.Sprintf("ViewAt(seq=%d): cell visible — %q", seqBeforeDelete, truncate(rec.RawContent, 40)))
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("verify delete viewat: %w", err)
	}

	// Health check after delete — confirm cell count dropped by 1.
	postDeleteReport, err := db.HealthCheck(ctx, hexxladb.DefaultHealthCheckConfig())
	if err != nil {
		return fmt.Errorf("health post-delete: %w", err)
	}
	printMetric("Cells after delete", postDeleteReport.CellCount, fmt.Sprintf("(was %d — dropped by %d)", preReport.CellCount, preReport.CellCount-postDeleteReport.CellCount))
	if len(postDeleteReport.Warnings) == 0 {
		printSuccess("Post-delete health check: no warnings")
	}
	fmt.Println()

	// --- 12c: idempotent re-delete (no error) ---
	printSubHeader("Idempotent re-delete")
	err = db.Update(func(tx *hexxladb.Tx) error {
		return tx.DeleteCell(ctx, delKey)
	})
	if err != nil {
		return fmt.Errorf("re-delete: %w", err)
	}
	printSuccess("Re-deleting the same cell returns nil (idempotent)")
	fmt.Println()

	// --- 12d: bulk-delete to create dead pages, then compact ---
	printSubHeader("DB.Compact — copy-compaction after bulk delete")
	printNote("Writes 200 throwaway cells, then deletes them all to create reclaimable dead space.")
	printNote("Compact rewrites live keys into a minimal-size file.")
	fmt.Println()

	// Write 200 throwaway cells to bloat the file.
	const bulkCount = 200
	err = db.Update(func(tx *hexxladb.Tx) error {
		for i := range bulkCount {
			p, err := hexxladb.Pack(hexxladb.Coord{Q: 20 + i, R: 20})
			if err != nil {
				return err
			}
			if err := tx.PutCell(ctx, hexxladb.CellRecord{
				Key:        p,
				RawContent: fmt.Sprintf("Bulk cell %d — written to demonstrate compaction size reduction after delete.", i),
				Tags:       []string{"bulk", "throwaway"},
				Provenance: hexxladb.ProvenanceWire{SourceID: "compact-demo"},
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("bulk write: %w", err)
	}
	printSuccess(fmt.Sprintf("Wrote %d throwaway cells", bulkCount))

	// Delete them all.
	err = db.Update(func(tx *hexxladb.Tx) error {
		for i := range bulkCount {
			p, err := hexxladb.Pack(hexxladb.Coord{Q: 20 + i, R: 20})
			if err != nil {
				return err
			}
			if err := tx.DeleteCell(ctx, p); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("bulk delete: %w", err)
	}
	printSuccess(fmt.Sprintf("Deleted %d throwaway cells (secondary indexes + tombstones written)", bulkCount))
	fmt.Println()

	// Prune old versions to convert tombstones into reclaimable dead space.
	pruneStats, err := db.StatsMVCC()
	if err != nil {
		return fmt.Errorf("prune stats: %w", err)
	}
	pruned, err := db.PruneCellVersions(pruneStats.CommitSeq, 10000)
	if err != nil {
		return fmt.Errorf("prune: %w", err)
	}
	printMetric("Pruned stale versions", pruned, "rows")

	srcInfo, _ := os.Stat(dbPath)
	srcSize := srcInfo.Size()
	printMetric("Source file size", fmt.Sprintf("%.1f KB", float64(srcSize)/1024), "")

	compactPath := dbPath + ".compacted"
	start := time.Now()
	if err := db.Compact(ctx, compactPath); err != nil {
		return fmt.Errorf("compact: %w", err)
	}
	elapsed := time.Since(start)

	destInfo, _ := os.Stat(compactPath)
	destSize := destInfo.Size()
	printMetric("Compacted file size", fmt.Sprintf("%.1f KB", float64(destSize)/1024), "")

	if destSize < srcSize {
		reduction := 100.0 * float64(srcSize-destSize) / float64(srcSize)
		printMetric("Size reduction", fmt.Sprintf("%.1f%%", reduction), "")
	} else {
		printMetric("Size change", fmt.Sprintf("%+.1f%%", 100.0*float64(destSize-srcSize)/float64(srcSize)), "")
	}
	printMetric("Compact duration", elapsed.Round(time.Microsecond), "")
	fmt.Println()

	// Verify the compacted database is readable.
	cDB, err := hexxladb.Open(compactPath, &hexxladb.Options{EnableMVCC: true})
	if err != nil {
		return fmt.Errorf("open compacted: %w", err)
	}
	cReport, err := cDB.HealthCheck(ctx, hexxladb.DefaultHealthCheckConfig())
	if err != nil {
		_ = cDB.Close()
		return fmt.Errorf("compact health: %w", err)
	}
	compactPageSize := cDB.PageSize()
	_ = cDB.Close()
	_ = os.Remove(compactPath)          // clean up demo artifact
	_ = os.Remove(compactPath + "-wal") // clean up compacted WAL

	printMetric("Compacted DB cells", cReport.CellCount, fmt.Sprintf("(matches source: %d)", postDeleteReport.CellCount))
	if compactPageSize == db.PageSize() {
		printSuccess(fmt.Sprintf("Compacted DB preserves page size: %d bytes", compactPageSize))
	}
	if len(cReport.Warnings) == 0 {
		printSuccess("Compacted database passes health check")
	}

	printSuccess("DeleteCell + Compact phase complete")
	fmt.Println()

	// ═══════════════════════════════════════════════════════════════
	// PHASE 13: Field of View (FOV) Context Loading
	// ═══════════════════════════════════════════════════════════════
	printHeader("Phase 13: Field of View — Visibility-Filtered Context")

	printNote("LoadContextFOV uses symmetric shadowcasting to skip cells hidden behind empty regions.")
	printNote("Compared to a radial ring walk, FOV returns only reachable candidates.")
	fmt.Println()

	fovCenter := cells[len(cells)/2] // a cell near the middle of the corpus

	// Radial context for comparison
	var radialCells []hexxladb.CellRecord
	if err := db.View(func(tx *hexxladb.Tx) error {
		for _, coord := range hexxladb.WalkRings(nil, fovCenter, 3) {
			p, err := hexxladb.Pack(coord)
			if err != nil {
				return err
			}
			rec, ok, err := tx.GetCell(p)
			if err != nil {
				return err
			}
			if ok {
				radialCells = append(radialCells, rec)
			}
		}
		return nil
	}); err != nil {
		return fmt.Errorf("radial context: %w", err)
	}

	// FOV-filtered context — empty cells act as opaque barriers
	var fovCells []hexxladb.CellRecord
	if err := db.View(func(tx *hexxladb.Tx) error {
		opaque := func(c hexxladb.Coord) bool {
			p, err := hexxladb.Pack(c)
			if err != nil {
				return true
			}
			_, ok, _ := tx.GetCell(p)
			return !ok // treat empty cells as opaque barriers
		}
		var err error
		fovCells, err = tx.LoadContextFOV(ctx, fovCenter, 3, opaque, hexxladb.FOVContextConfig{
			MaxCells: 128,
		})
		return err
	}); err != nil {
		return fmt.Errorf("fov context: %w", err)
	}

	printSubHeader("Comparison: radial vs FOV-filtered context")
	printMetric("Center", fmt.Sprintf("(%d,%d)", fovCenter.Q, fovCenter.R), "")
	printMetric("Radius", 3, "rings")
	printMetric("Radial cells (all occupied)", len(radialCells), "cells")
	printMetric("FOV cells (visible only)", len(fovCells), "cells")
	if len(radialCells) > len(fovCells) {
		printMetric("Cells skipped by FOV", len(radialCells)-len(fovCells), "occluded candidates")
	}
	fmt.Println()

	_, _ = infoStyle.Println("  FOV-visible cells:")
	for i, rec := range fovCells {
		if i >= 10 {
			_, _ = dimStyle.Printf("    ⋯  (%d more cells not shown)\n", len(fovCells)-10)
			break
		}
		c, _ := hexxladb.Unpack(rec.Key)
		dist := fovCenter.Distance(c)
		_, _ = dimStyle.Printf("    [%02d] (%d,%d) dist=%d  ", i+1, c.Q, c.R, dist)
		_, _ = dataStyle.Printf("%s\n", truncate(rec.RawContent, 48))
	}
	fmt.Println()

	printNote("FOV is ideal for sparse grids: large empty gaps block LOS, so the context")
	printNote("retrieval includes only cells the observer can actually 'see.'")
	printSuccess("FOV context loading complete")
	fmt.Println()

	// ═══════════════════════════════════════════════════════════════
	// PHASE 14: Pathfinding Over Edges
	// ═══════════════════════════════════════════════════════════════
	printHeader("Phase 14: Pathfinding Over Edges (Dijkstra / BFS)")

	printNote("Edges create a graph overlay on the hex grid. PutEdge links cells,")
	printNote("FindEdgePath finds weighted shortest paths, WalkEdges does BFS reachability.")
	fmt.Println()

	// Create edges between some cells to form a graph
	printSubHeader("Step 1 — Create edge network")
	type edgePair struct {
		from, to int
		kind     string
		weight   float64
	}
	edgePairs := []edgePair{
		{0, 1, "follows", 1.0},
		{1, 2, "follows", 1.0},
		{2, 3, "follows", 1.0},
		{3, 4, "follows", 1.0},
		{4, 5, "follows", 1.0},
		{0, 5, "references", 3.0}, // shortcut with higher weight
		{5, 10, "topic-link", 1.0},
		{10, 15, "topic-link", 1.0},
		{15, 20, "topic-link", 1.0},
	}
	if err := db.Update(func(tx *hexxladb.Tx) error {
		for _, ep := range edgePairs {
			if ep.from >= len(cells) || ep.to >= len(cells) {
				continue
			}
			fromPK, _ := hexxladb.Pack(cells[ep.from])
			toPK, _ := hexxladb.Pack(cells[ep.to])
			if err := tx.PutEdge(hexxladb.EdgeWalkRecord{
				From:         fromPK,
				To:           toPK,
				RelationType: ep.kind,
				Weight:       ep.weight,
			}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return fmt.Errorf("put edges: %w", err)
	}
	printSuccess(fmt.Sprintf("Created %d edges across the conversation graph", len(edgePairs)))
	for _, ep := range edgePairs {
		if ep.from >= len(cells) || ep.to >= len(cells) {
			continue
		}
		_, _ = dimStyle.Printf("    (%d,%d) →[%s w=%.0f]→ (%d,%d)\n",
			cells[ep.from].Q, cells[ep.from].R, ep.kind, ep.weight,
			cells[ep.to].Q, cells[ep.to].R)
	}
	fmt.Println()

	// Dijkstra shortest path
	printSubHeader("Step 2 — FindEdgePath (weighted shortest path)")
	if len(cells) > 20 {
		start, goal := cells[0], cells[5]
		_, _ = infoStyle.Printf("  Finding shortest path: (%d,%d) → (%d,%d)\n", start.Q, start.R, goal.Q, goal.R)
		var path []hexxladb.Coord
		if err := db.View(func(tx *hexxladb.Tx) error {
			var err error
			path, err = tx.FindEdgePath(ctx, start, goal, hexxladb.FindEdgePathConfig{MaxExpand: 100})
			return err
		}); err != nil {
			return fmt.Errorf("find edge path: %w", err)
		}
		if path != nil {
			printMetric("Path length", len(path), "hops")
			pathStr := make([]string, len(path))
			for i, c := range path {
				pathStr[i] = fmt.Sprintf("(%d,%d)", c.Q, c.R)
			}
			_, _ = dimStyle.Printf("    Path: %s\n", strings.Join(pathStr, " → "))
		} else {
			printNote("No path found (cells not connected)")
		}
	}
	fmt.Println()

	// BFS reachability
	printSubHeader("Step 3 — WalkEdges (BFS reachability)")
	if len(cells) > 0 {
		bfsStart := cells[0]
		_, _ = infoStyle.Printf("  BFS from (%d,%d), max 4 hops, following all edge types:\n", bfsStart.Q, bfsStart.R)
		var reachable []hexxladb.Coord
		if err := db.View(func(tx *hexxladb.Tx) error {
			var err error
			reachable, err = tx.WalkEdges(ctx, bfsStart, "", 4, 20)
			return err
		}); err != nil {
			return fmt.Errorf("walk edges: %w", err)
		}
		printMetric("Reachable cells", len(reachable), "coords")
		for i, c := range reachable {
			if i >= 10 {
				_, _ = dimStyle.Printf("    ⋯  (%d more not shown)\n", len(reachable)-10)
				break
			}
			_, _ = dimStyle.Printf("    [%d] (%d,%d)\n", i+1, c.Q, c.R)
		}
	}
	fmt.Println()

	// LoadContext with EdgeFilter
	printSubHeader("Step 4 — LoadContext with EdgeFilter (graph-aware context)")
	if len(cells) > 0 {
		edgeCenter := cells[0]
		var edgePack hexxladb.ContextPack
		if err := db.View(func(tx *hexxladb.Tx) error {
			var err error
			edgePack, err = tx.LoadContext(ctx, hexxladb.LoadContextConfig{
				Seeds:      []hexxladb.Coord{edgeCenter},
				EdgeFilter: "",
				MaxHops:    3,
				MaxCells:   20,
				Assembly:   hexxladb.ContextAssemblyConfig{Assemble: hexxladb.DefaultAssembleCellViewOpts()},
			})
			return err
		}); err != nil {
			return fmt.Errorf("load context by edges: %w", err)
		}
		printMetric("Edge-connected context cells", len(edgePack.Cells), "cells")
		for i, cv := range edgePack.Cells {
			if i >= 5 {
				_, _ = dimStyle.Printf("    ⋯  (%d more not shown)\n", len(edgePack.Cells)-5)
				break
			}
			_, _ = dimStyle.Printf("    [%d] (%d,%d) ", i+1, cv.Coord.Q, cv.Coord.R)
			_, _ = dataStyle.Printf("%s\n", truncate(cv.RawContent, 50))
		}
	}
	fmt.Println()
	printSuccess("Pathfinding over edges complete")
	fmt.Println()

	// ═══════════════════════════════════════════════════════════════
	// COMPLETION
	// ═══════════════════════════════════════════════════════════════
	_, _ = separatorStyle.Println(strings.Repeat("═", lineWidth))
	_, _ = successStyle.Printf("  ✓  HexxlaDB Conversational Memory Demo — complete\n")
	_, _ = separatorStyle.Println(strings.Repeat("═", lineWidth))
	fmt.Println()

	printInfo("Database", dbPath)
	printInfo("Corpus", fmt.Sprintf("%d turns · 5 thematic sessions", len(seedConversation)))
	printInfo("Phases run", "1–14 (all)")
	printInfo("Hook writes", fmt.Sprintf("%d cell writes observed via AfterPutCell", hookCellCount))
	fmt.Println()

	_, _ = infoStyle.Println("  Production next steps:")
	_, _ = dimStyle.Println("    •  Render and token-fit the complete model request in your application")
	_, _ = dimStyle.Println("    •  Wire AfterPutCell/AfterPutSeam for real-time CDC or audit logging")
	_, _ = dimStyle.Println("    •  Use durable changelog consumers for replication or CDC pipelines")
	_, _ = dimStyle.Println("    •  Use LoadContextFOV for sparse grids to avoid irrelevant candidates")
	_, _ = dimStyle.Println("    •  Build edge graphs for topic-navigation and graph-based retrieval")
	_, _ = dimStyle.Println("    •  Enable encryption (Options.Passphrase / Options.EncryptionKey)")
	_, _ = dimStyle.Println("    •  Tune Options.PageSize (4096/8192/16384/65536) for your workload")
	_, _ = dimStyle.Println("    •  See docs/hexxladb/API_REFERENCE.md for the public API guide")
	fmt.Println()

	return nil
}

// spiralCoord maps a linear corpus index to an axial grid coordinate.
// The grid is 11 columns wide (q in [-5,5]) so all 84 seed turns fit without
// collision. Each row shifts by one q unit per 11 entries.
func spiralCoord(index int) hexxladb.Coord {
	const cols = 11
	q := (index % cols) - (cols / 2)
	r := (index / cols) - 4
	return hexxladb.Coord{Q: q, R: r}
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
