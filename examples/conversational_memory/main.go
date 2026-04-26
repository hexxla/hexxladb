// Conversational Memory Service Example
//
// Demonstrates production patterns for building an LLM memory system:
// - Storing conversations with proper provenance
// - Context assembly with token budgets
// - Contradiction detection and tracking
// - Session management and cleanup
//
// Run: go run ./examples/conversational_memory
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fatih/color"

	"github.com/hexxla/hexxladb"
	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
)

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

func printHeader(title string) {
	fmt.Println()
	_, _ = separatorStyle.Println(strings.Repeat("═", 60))
	_, _ = headerStyle.Println("  ▶ " + title)
	_, _ = separatorStyle.Println(strings.Repeat("═", 60))
}

func printSuccess(msg string) {
	_, _ = successStyle.Println("  ✓ " + msg)
}

func printInfo(label, value string) {
	_, _ = infoStyle.Printf("  %-20s ", label+":")
	_, _ = dataStyle.Println(value)
}

func printMetric(name string, value any, unit string) {
	_, _ = metricStyle.Printf("  📊 %-25s ", name+":")
	_, _ = accentStyle.Printf("%v", value)
	if unit != "" {
		_, _ = dimStyle.Println(" " + unit)
	} else {
		fmt.Println()
	}
}

func main() {
	log.SetFlags(0)
	if err := run(); err != nil {
		_, _ = errorStyle.Println("\n  ✗ Error: " + err.Error())
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()
	dir := filepath.Join(".tmp", "conversational_memory")
	_ = os.MkdirAll(dir, 0o750)

	printHeader("Conversational Memory Service")
	_, _ = infoStyle.Println("  Building a production LLM memory system with HexxlaDB")
	fmt.Println()

	// ═══════════════════════════════════════════════════════════════
	// PHASE 1: Initialize Database
	// ═══════════════════════════════════════════════════════════════
	printHeader("Phase 1: Database Initialization")

	dbPath := filepath.Join(dir, "memory.db")
	_ = os.Remove(dbPath)
	_ = os.Remove(dbPath + "-wal")

	opts := &hexxladb.Options{
		EnableMVCC:       true,
		ChangelogEnabled: true,
		MVCCRetention: hexxladb.MVCCRetention{
			RetainCommitsBehindHead: 100,
		},
	}

	_, _ = infoStyle.Println("  Opening database with MVCC enabled...")
	db, err := hexxladb.Open(dbPath, opts)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer func() { _ = db.Close() }()

	printSuccess("Database initialized")
	printInfo("Path", dbPath)
	printInfo("MVCC", "enabled (retain 100 commits)")
	fmt.Println()

	// ═══════════════════════════════════════════════════════════════
	// PHASE 2: Store Conversation Session
	// ═══════════════════════════════════════════════════════════════
	printHeader("Phase 2: Storing Conversation History")

	// Rich conversation history with semantically-aligned tags
	// Tags categorize content type (fact, opinion, preference) and domain (tech, workflow, api)
	conversation := []struct {
		role    string
		content string
		tags    []string
	}{
		// User preferences and workflow
		{"user", "I prefer detailed technical explanations with code examples.", []string{"preference", "communication-style", "user-123"}},
		{"assistant", "Understood. I'll provide detailed technical responses with code examples.", []string{"acknowledgment", "commitment", "user-123"}},
		{"user", "Actually, keep it concise. I changed my mind.", []string{"preference", "communication-style", "contradiction", "user-123"}},
		{"assistant", "Noted. I'll be concise going forward.", []string{"acknowledgment", "commitment", "user-123"}},

		// Technical facts about the system
		{"user", "What's the best database for LLM memory systems?", []string{"question", "database", "recommendation", "user-123"}},
		{"assistant", "HexxlaDB is designed specifically for spatial LLM memory with hexagonal coordinate addressing.", []string{"fact", "product", "hexagonal-addressing", "user-123"}},
		{"user", "How does the hexagonal grid work?", []string{"question", "architecture", "explanation", "user-123"}},
		{"assistant", "Each memory cell has axial coordinates (q, r). Six neighbors form a ring around any cell for context assembly.", []string{"fact", "technical", "coordinates", "ring-neighbors", "user-123"}},

		// User opinions and experiences
		{"user", "I think traditional vector databases are too complex for conversational memory.", []string{"opinion", "vector-databases", "criticism", "user-123"}},
		{"assistant", "Many users find vector DBs require significant tuning for conversational retrieval.", []string{"fact", "industry-trend", "user-feedback", "user-123"}},
		{"user", "I really like the idea of storing contradictions as seams.", []string{"opinion", "feature", "seams", "enthusiasm", "user-123"}},

		// API and integration questions
		{"user", "Can I query cells by tags?", []string{"question", "api", "tags", "query", "user-123"}},
		{"assistant", "Yes. Use AscendCellsByTag to scan the tag secondary index. ListExistingTopics returns all distinct tags.", []string{"fact", "api", "example-code", "secondary-index", "user-123"}},
		{"user", "What's the maximum cell content size?", []string{"question", "limits", "configuration", "user-123"}},
		{"assistant", "Default is 8KB per cell, configurable per-database. Handles most prompts and conversation turns.", []string{"fact", "configuration", "8kb-limit", "user-123"}},

		// Workflow and preferences
		{"user", "I want to organize my project memories by topic.", []string{"goal", "workflow", "organization", "user-123"}},
		{"assistant", "Use tags like 'frontend', 'backend', 'database' to categorize. Query by tag for focused context.", []string{"recommendation", "best-practice", "tag-strategy", "user-123"}},
		{"user", "I work on multiple projects. Can I filter by project?", []string{"question", "multi-tenant", "filtering", "user-123"}},
		{"assistant", "Yes. Use source_id for project isolation or project-specific tags like 'project-alpha', 'project-beta'.", []string{"fact", "multi-tenant", "source-id", "tag-pattern", "user-123"}},

		// Contradiction demo (preference changed again)
		{"user", "On second thought, I do want detailed explanations for complex topics.", []string{"preference", "communication-style", "contradiction", "conditional", "user-123"}},
		{"assistant", "I'll adapt: concise for simple topics, detailed for complex ones.", []string{"acknowledgment", "conditional-logic", "adaptation", "user-123"}},

		// More facts and features
		{"user", "Does it support encryption?", []string{"question", "security", "encryption", "compliance", "user-123"}},
		{"assistant", "Yes. AES-256-XTS at the page level. Optional passphrase with Argon2id key derivation.", []string{"fact", "security", "aes-xts", "argon2id", "user-123"}},
		{"user", "Can I export my memory data?", []string{"question", "export", "portability", "user-123"}},
		{"assistant", "The changefeed provides logical change stream. Secondary indexes support bulk export patterns.", []string{"fact", "export", "changefeed", "bulk-operations", "user-123"}},
	}

	_, _ = infoStyle.Printf("  Processing %d conversation turns...\n", len(conversation))
	fmt.Println()

	var cells []lattice.Coord
	sessionID := fmt.Sprintf("session-%d", time.Now().Unix())

	// Build cells using template factories instead of manual record construction.
	var recs []record.CellRecord
	for i, msg := range conversation {
		coord := spiralCoord(i)
		cells = append(cells, coord)
		pk, _ := lattice.Pack(coord)

		var rec record.CellRecord
		if msg.role == "user" {
			rec = hexxladb.NewUserMessageCell(pk, msg.content, sessionID, 1.0)
		} else {
			rec = hexxladb.NewAssistantResponseCell(pk, msg.content, sessionID, 1.0)
		}
		rec.Tags = append(rec.Tags, msg.tags...) // merge template tags with per-message tags
		recs = append(recs, rec)

		var roleColor *color.Color
		if msg.role == "user" {
			roleColor = color.New(color.FgGreen)
		} else {
			roleColor = color.New(color.FgBlue)
		}

		_, _ = dimStyle.Printf("    [%d] ", i)
		_, _ = roleColor.Printf("%-10s", msg.role)
		_, _ = dataStyle.Printf(" %s\n", truncate(msg.content, 45))
	}

	// Batch-write all cells with progress tracking.
	result, err := db.BatchPutCells(ctx, recs, &hexxladb.BatchPutCellOptions{
		BatchSize: 64,
		OnProgress: func(i int) {
			if (i+1)%10 == 0 || i+1 == len(recs) {
				_, _ = dimStyle.Printf("    ...committed %d/%d\n", i+1, len(recs))
			}
		},
	})
	if err != nil {
		return fmt.Errorf("batch put cells: %w", err)
	}

	fmt.Println()
	printSuccess(fmt.Sprintf("Stored %d conversation turns (BatchPutCells)", result.Written))
	fmt.Println()

	// ═══════════════════════════════════════════════════════════════
	// PHASE 3: Detect Contradictions (Seams)
	// ═══════════════════════════════════════════════════════════════
	printHeader("Phase 3: Contradiction Detection")

	_, _ = infoStyle.Println("  Checking for conflicting preferences...")
	fmt.Println()

	// User changed their preference - mark as conflict
	if len(cells) >= 3 {
		err := db.Update(func(tx *hexxladb.Tx) error {
			return tx.MarkConflict(cells[0], cells[2], "User preference changed: detailed vs concise")
		})
		if err != nil {
			return fmt.Errorf("mark conflict: %w", err)
		}

		_, _ = warningStyle.Println("  ⚠ Detected contradiction:")
		_, _ = infoStyle.Printf("    Between: ")
		_, _ = dataStyle.Println(fmt.Sprintf("cell[0] (%d,%d) and cell[2] (%d,%d)", cells[0].Q, cells[0].R, cells[2].Q, cells[2].R))
		_, _ = infoStyle.Printf("    Reason:  ")
		_, _ = dataStyle.Println("User preference changed: detailed vs concise")
		fmt.Println()
		printSuccess("Seam created to track contradiction")
	}
	fmt.Println()

	center := cells[len(cells)-1] // Last message coordinate as context center

	// ═══════════════════════════════════════════════════════════════
	// PHASE 4: Supersession — mark stale cells as superseded
	// ═══════════════════════════════════════════════════════════════
	printHeader("Phase 4: Supersession (Seam-Aware Context Assembly)")

	_, _ = infoStyle.Println("  Marking preference cell[0] as superseded by cell[2]...")
	_, _ = dimStyle.Println("  (User changed from 'detailed' to 'concise' — old cell is stale)")
	fmt.Println()

	// cells[2] is the "keep it concise" message — the current truth supersedes cells[0]
	if len(cells) >= 3 {
		if err := db.Update(func(tx *hexxladb.Tx) error {
			return tx.MarkSupersedes(cells[2], cells[0], "Preference updated: concise supersedes detailed")
		}); err != nil {
			return fmt.Errorf("mark supersedes: %w", err)
		}
		_, _ = successStyle.Println("  ✓ Supersession seam recorded")
		_, _ = infoStyle.Printf("    Stale:   cell[0] (%d,%d) — 'I prefer detailed technical explanations'\n", cells[0].Q, cells[0].R)
		_, _ = infoStyle.Printf("    Current: cell[2] (%d,%d) — 'Actually, keep it concise'\n", cells[2].Q, cells[2].R)
	}
	fmt.Println()

	// Load context WITHOUT FilterSuperseded — stale cell appears
	var packWithStale hexxladb.ContextPack
	if err := db.View(func(tx *hexxladb.Tx) error {
		var err error
		packWithStale, err = tx.LoadContextPack(ctx, center, 3, 10000, hexxladb.ByteLenBudgeter{}, hexxladb.LoadContextBudgetConfig{
			Assemble:         hexxladb.DefaultAssembleCellViewOpts(),
			FilterSuperseded: false,
			Explain:          true,
		})
		return err
	}); err != nil {
		return fmt.Errorf("load context (unfiltered): %w", err)
	}

	// Load context WITH FilterSuperseded — stale cell excluded, successor included
	var packFiltered hexxladb.ContextPack
	if err := db.View(func(tx *hexxladb.Tx) error {
		var err error
		packFiltered, err = tx.LoadContextPack(ctx, center, 3, 10000, hexxladb.ByteLenBudgeter{}, hexxladb.LoadContextBudgetConfig{
			Assemble:         hexxladb.DefaultAssembleCellViewOpts(),
			FilterSuperseded: true,
			Explain:          true,
		})
		return err
	}); err != nil {
		return fmt.Errorf("load context (filtered): %w", err)
	}

	printMetric("Cells WITHOUT FilterSuperseded", len(packWithStale.Cells), "cells")
	printMetric("Cells WITH FilterSuperseded", len(packFiltered.Cells), "cells")
	fmt.Println()

	// Show which cells are successors (SupersededFrom is set)
	var substituted int
	for _, cv := range packFiltered.Cells {
		if cv.SupersededFrom != nil {
			substituted++
			_, _ = accentStyle.Printf("  ↺ Successor cell (%d,%d)", cv.Coord.Q, cv.Coord.R)
			_, _ = dimStyle.Printf(" replaced stale cell (%d,%d)\n", cv.SupersededFrom.Q, cv.SupersededFrom.R)
			_, _ = dimStyle.Printf("    Content: %s\n", truncate(cv.RawContent, 55))
		}
	}
	if substituted == 0 {
		_, _ = dimStyle.Println("  (no substitutions in this radius — stale cell excluded entirely)")
	}
	fmt.Println()

	// Show superseded explanations from Explain mode
	_, _ = infoStyle.Println("  Supersession decisions (Explain mode):")
	for _, ex := range packFiltered.Explanations {
		if ex.Reason == "superseded" {
			if ex.SupersededBy != nil {
				_, _ = warningStyle.Printf("  ✗ (%d,%d) ring=%-2d superseded → replaced by (%d,%d)\n",
					ex.Coord.Q, ex.Coord.R, ex.Ring, ex.SupersededBy.Q, ex.SupersededBy.R)
			} else {
				_, _ = warningStyle.Printf("  ✗ (%d,%d) ring=%-2d superseded → excluded (no live successor)\n",
					ex.Coord.Q, ex.Coord.R, ex.Ring)
			}
		}
	}
	fmt.Println()
	printSuccess("Seam-aware filtering complete: stale cells removed from context")
	fmt.Println()

	// ═══════════════════════════════════════════════════════════════
	// PHASE 5: Context Assembly for LLM Prompt
	// ═══════════════════════════════════════════════════════════════
	printHeader("Phase 5: Context Assembly (Token-Budgeted)")

	_, _ = infoStyle.Printf("  Assembling context around coordinate (%d,%d)...\n", center.Q, center.R)
	fmt.Println()

	// First, load without budget constraint to see total size
	var allCells []record.CellRecord
	err = db.View(func(tx *hexxladb.Tx) error {
		var err error
		allCells, err = tx.LoadContext(ctx, center, 3, 100)
		return err
	})
	if err != nil {
		return fmt.Errorf("load context: %w", err)
	}

	totalBytes := 0
	for _, c := range allCells {
		totalBytes += len(c.RawContent)
	}

	printMetric("Cells in radius-3", len(allCells), "cells")
	printMetric("Total content size", totalBytes, "bytes")
	fmt.Println()

	// Now load with byte budget (simulating token limit)
	budget := 150 // bytes
	_, _ = infoStyle.Printf("  Applying byte budget of %d...\n", budget)
	fmt.Println()

	opts2 := hexxladb.DefaultAssembleCellViewOpts()
	cfg := hexxladb.LoadContextBudgetConfig{
		Assemble:          opts2,
		MaxCandidateCells: 64,
		IncludeFacetText:  false,
		IncludeSeams:      true,
		SeamRadius:        2,
		Explain:           true, // populate per-cell inclusion/eviction reasons
	}

	var pack hexxladb.ContextPack
	err = db.View(func(tx *hexxladb.Tx) error {
		var err error
		pack, err = tx.LoadContextPack(ctx, center, 3, budget, hexxladb.ByteLenBudgeter{}, cfg)
		return err
	})
	if err != nil {
		return fmt.Errorf("load context pack: %w", err)
	}

	includedBytes := 0
	for _, cv := range pack.Cells {
		includedBytes += len(cv.RawContent)
	}

	printMetric("Cells in context pack", len(pack.Cells), "cells")
	printMetric("Included content", includedBytes, "bytes")
	printMetric("Budget utilization", fmt.Sprintf("%.1f%%", float64(includedBytes)/float64(budget)*100), "")

	if len(pack.Seams) > 0 {
		fmt.Println()
		_, _ = warningStyle.Printf("  ⚠ %d seam(s) present in context:\n", len(pack.Seams))
		for _, seam := range pack.Seams {
			_, _ = dimStyle.Printf("    • %v ↔ %v: ", seam.CellA, seam.CellB)
			_, _ = dataStyle.Println(seam.Reason)
		}
	}

	// QueryStats: show assembly statistics
	fmt.Println()
	_, _ = infoStyle.Println("  Assembly Statistics (ContextPackStats):")
	printMetric("Candidates scanned", pack.Stats.CandidatesScanned, "cells")
	printMetric("Cells evicted", pack.Stats.CellsEvicted, "cells")
	printMetric("Max ring used", pack.Stats.MaxRingUsed, "")

	// Explain mode: show per-cell decisions
	if len(pack.Explanations) > 0 {
		fmt.Println()
		_, _ = infoStyle.Println("  Cell Explanations (Explain mode):")
		for _, ex := range pack.Explanations {
			marker := "✓"
			c := color.New(color.FgGreen)
			if ex.Reason != "included" {
				marker = "✗"
				c = color.New(color.FgRed)
			}
			_, _ = c.Printf("    %s ", marker)
			_, _ = dimStyle.Printf("(%d,%d) ring=%d tokens=%d ", ex.Coord.Q, ex.Coord.R, ex.Ring, ex.Tokens)
			_, _ = dataStyle.Println(ex.Reason)
		}
	}

	fmt.Println()
	printSuccess("Context assembled within budget")
	fmt.Println()

	// ═══════════════════════════════════════════════════════════════
	// PHASE 6: Tag Discovery and Analytics
	// ═══════════════════════════════════════════════════════════════
	printHeader("Phase 6: Tag Discovery and Analytics")

	// Discover all unique tags in the database
	_, _ = infoStyle.Println("  Discovering all tags in the database...")
	var allTags []string
	err = db.View(func(tx *hexxladb.Tx) error {
		var err error
		allTags, err = tx.ListExistingTopics(ctx)
		return err
	})
	if err != nil {
		return fmt.Errorf("list existing topics: %w", err)
	}

	printMetric("Existing tags", len(allTags), "tags")
	fmt.Println()

	_, _ = infoStyle.Println("  Tags in database:")
	for _, tag := range allTags {
		_, _ = dimStyle.Printf("    • %s\n", tag)
	}
	fmt.Println()
	printSuccess(fmt.Sprintf("Discovered %d unique tags", len(allTags)))
	fmt.Println()

	// Tag analytics: counts and co-occurrences
	_, _ = infoStyle.Println("  Tag Counts (top 10):")
	err = db.View(func(tx *hexxladb.Tx) error {
		counts, err := tx.TagCounts(ctx)
		if err != nil {
			return err
		}
		shown := min(len(counts), 10)
		for _, tc := range counts[:shown] {
			_, _ = dimStyle.Printf("    %-30s ", tc.Tag)
			_, _ = accentStyle.Printf("%d cells\n", tc.Count)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("tag counts: %w", err)
	}
	fmt.Println()

	_, _ = infoStyle.Println("  Tag Co-occurrences (top 5, min 3):")
	err = db.View(func(tx *hexxladb.Tx) error {
		pairs, err := tx.TagCooccurrences(ctx, 3)
		if err != nil {
			return err
		}
		shown := min(len(pairs), 5)
		for _, tp := range pairs[:shown] {
			_, _ = dimStyle.Printf("    %-20s + %-20s ", tp.A, tp.B)
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

	// Query by tag
	_, _ = infoStyle.Println("  Query: All cells tagged 'preference'...")
	var prefCells int
	err = db.View(func(tx *hexxladb.Tx) error {
		return tx.AscendCellsByTag(ctx, "preference", func(_ record.CellRecord) bool {
			prefCells++
			return true
		})
	})
	if err != nil {
		return fmt.Errorf("query by tag: %w", err)
	}
	printMetric("Preference cells found", prefCells, "cells")
	fmt.Println()

	// Query by source
	_, _ = infoStyle.Println("  Query: All cells from this session...")
	var sessionCells int
	err = db.View(func(tx *hexxladb.Tx) error {
		return tx.AscendCellsBySource(ctx, sessionID, func(_ record.CellRecord) bool {
			sessionCells++
			return true
		})
	})
	if err != nil {
		return fmt.Errorf("query by source: %w", err)
	}
	printMetric("Session cells found", sessionCells, "cells")
	fmt.Println()
	printSuccess("Queries completed")
	fmt.Println()

	// ═══════════════════════════════════════════════════════════════
	// PHASE 8: MVCC Snapshot
	// ═══════════════════════════════════════════════════════════════
	printHeader("Phase 8: MVCC Time-Travel Query")

	stats, err := db.StatsMVCC()
	if err != nil {
		return fmt.Errorf("get mvcc stats: %w", err)
	}

	printMetric("Current commit seq", stats.CommitSeq, "")

	if stats.CommitSeq > 1 {
		_, _ = infoStyle.Printf("  Querying at commit seq %d (after storing 3 messages)...\n", stats.CommitSeq-2)

		var historicalCount int
		err = db.ViewAt(stats.CommitSeq-2, func(tx *hexxladb.Tx) error {
			cells, err := tx.LoadContext(ctx, center, 3, 50)
			historicalCount = len(cells)
			return err
		})
		if err != nil {
			return fmt.Errorf("historical query: %w", err)
		}

		printMetric("Cells at that point", historicalCount, "cells")
		fmt.Println()
		printSuccess("Time-travel query completed")
	}
	fmt.Println()

	// ═══════════════════════════════════════════════════════════════
	// PHASE 9: Lattice Visualization & Diagnostics
	// ═══════════════════════════════════════════════════════════════
	printHeader("Phase 9: Lattice Visualization & Diagnostics")

	// ASCII hex grid — debug tool for small radii (clamped to MaxRenderRadius=10).
	// For large lattices, use RingDensityMap for aggregate stats instead.
	_, _ = infoStyle.Println("  ASCII Hex Grid (radius 3 around center):")
	fmt.Println()
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

	// Ring density — works for any radius, unlike ASCII renderer
	_, _ = infoStyle.Println("  Ring Density Map (radius 5):")
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

	// Filtered changelog — watch only cell writes
	_, _ = infoStyle.Println("  Filtered Changelog (cell writes only, last 5):")
	clRecords, err := db.ReadChangelogFiltered(0, 5, hexxladb.ChangelogFilter{
		Ops: []byte{hexxladb.ChangelogOpPutCell},
	})
	if err != nil {
		return fmt.Errorf("filtered changelog: %w", err)
	}
	for _, rec := range clRecords {
		_, _ = dimStyle.Printf("    seq=%d op=PutCell key=%x\n", rec.Seq, rec.Key)
	}
	printMetric("Filtered changelog records", len(clRecords), "")
	fmt.Println()
	printSuccess("Visualization & diagnostics complete")
	fmt.Println()

	// ═══════════════════════════════════════════════════════════════
	// PHASE 10: QueryCells + Multi-Seed Context Assembly
	// ═══════════════════════════════════════════════════════════════
	printHeader("Phase 10: QueryCells + Multi-Seed Context Assembly")

	// ── 10a: lexical query with score ranking ─────────────────────
	_, _ = infoStyle.Println("  Query 1 — keyword 'database', sorted by score:")
	_, _ = dimStyle.Println("  (Uses full-scan planner path; scores: tag exact +1.0, prefix +0.8,")
	_, _ = dimStyle.Println("   content verbatim +0.6, content icase +0.5, sourceID +0.3, confidence bonus)")
	fmt.Println()

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

	printMetric("Results for 'database'", len(queryResults), "cells")
	fmt.Println()
	_, _ = infoStyle.Println("  Results (score desc):")
	for i, r := range queryResults {
		_, _ = dimStyle.Printf("    [%d] score=%.2f coord=(%d,%d) ", i+1, r.Score, r.Cell.Coord.Q, r.Cell.Coord.R)
		_, _ = dataStyle.Printf("%s\n", truncate(r.Cell.RawContent, 48))
		if r.Explanation != "" {
			_, _ = dimStyle.Printf("         explain: %s\n", r.Explanation)
		}
	}
	fmt.Println()

	// ── 10b: tag + ExcludeTags + SortByConfidence ─────────────────
	_, _ = infoStyle.Println("  Query 2 — tag:'fact', exclude:'question', sort by confidence:")
	_, _ = dimStyle.Println("  (Uses tag/ secondary index as primary scan; ExcludeTags applied in-memory)")
	fmt.Println()

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

	printMetric("fact cells (excluding question)", len(factResults), "cells")
	for i, r := range factResults {
		_, _ = dimStyle.Printf("    [%d] conf=%.1f coord=(%d,%d) %s\n",
			i+1, r.Cell.Provenance.Confidence, r.Cell.Coord.Q, r.Cell.Coord.R,
			truncate(r.Cell.RawContent, 48))
	}
	fmt.Println()

	// ── multi-seed assembly from query 1 results ──────────────────
	seeds := make([]hexxladb.Coord, 0, len(queryResults))
	for _, r := range queryResults {
		seeds = append(seeds, r.Cell.Coord)
	}

	if len(seeds) == 0 {
		_, _ = dimStyle.Println("  (no search results — skipping multi-seed assembly)")
	} else {
		_, _ = infoStyle.Printf("  Assembling merged context from %d seeds (shared budget: 400 bytes)...\n", len(seeds))
		_, _ = dimStyle.Println("  Budget eviction: merged pool re-ranked by confidence; highest-confidence cells kept first.")
		fmt.Println()

		sharedBudget := 400
		assemblyCfg := hexxladb.LoadContextBudgetConfig{
			Assemble:         hexxladb.DefaultAssembleCellViewOpts(),
			FilterSuperseded: true,
		}
		var multiPack hexxladb.ContextPack
		if err := db.View(func(tx *hexxladb.Tx) error {
			var err error
			// LoadContextPackFrom dispatches to LoadContextPack (1 seed) or
			// LoadMultiContextPack (N seeds) automatically — no API switch needed.
			multiPack, err = tx.LoadContextPackFrom(ctx, 1, sharedBudget,
				hexxladb.ByteLenBudgeter{}, assemblyCfg, seeds...)
			return err
		}); err != nil {
			return fmt.Errorf("load context pack from: %w", err)
		}

		usedBytes := 0
		for _, cv := range multiPack.Cells {
			usedBytes += hexxladb.ByteLenBudgeter{}.CountTokens(cv.RawContent)
		}

		printMetric("Seeds expanded", len(seeds), "coords")
		printMetric("Candidates scanned (all seeds)", multiPack.Stats.CandidatesScanned, "cells")
		printMetric("Cells evicted (budget)", multiPack.Stats.CellsEvicted, "cells")
		printMetric("Cells in merged pack", len(multiPack.Cells), "cells")
		printMetric("Budget used", usedBytes, fmt.Sprintf("/ %d bytes (%.0f%%)", sharedBudget, float64(usedBytes)/float64(sharedBudget)*100))
		fmt.Println()

		_, _ = infoStyle.Println("  Merged context (highest-confidence cells first):")
		for i, cv := range multiPack.Cells {
			conf := cv.Provenance.Confidence
			_, _ = dimStyle.Printf("    [%d] conf=%.1f coord=(%d,%d) %s\n",
				i+1, conf, cv.Coord.Q, cv.Coord.R, truncate(cv.RawContent, 45))
		}
		fmt.Println()

		_, _ = infoStyle.Println("  Token budget breakdown:")
		_, _ = dimStyle.Printf("    Shared budget:    %d bytes\n", sharedBudget)
		_, _ = dimStyle.Printf("    Used by pack:     %d bytes\n", usedBytes)
		_, _ = dimStyle.Printf("    Remaining:        %d bytes\n", sharedBudget-usedBytes)
		_, _ = dimStyle.Println()
		_, _ = dimStyle.Println("  How budget works across seeds:")
		_, _ = dimStyle.Println("    1. Each seed expands independently (ring walk, FilterSuperseded)")
		_, _ = dimStyle.Println("    2. All candidate cells merged into one pool")
		_, _ = dimStyle.Println("    3. Pool re-ranked by Confidence descending (cross-seed fair ranking)")
		_, _ = dimStyle.Println("    4. Greedy fill: keep cells from highest confidence until budget exhausted")
		_, _ = dimStyle.Println("    5. DeduplicateCoords: shared neighbours counted only once (seed order priority)")
		fmt.Println()

		printSuccess(fmt.Sprintf("Multi-seed pack assembled: %d cells from %d search seeds within %d-byte budget",
			len(multiPack.Cells), len(seeds), sharedBudget))
	}
	fmt.Println()

	// ═══════════════════════════════════════════════════════════════
	// COMPLETION
	// ═══════════════════════════════════════════════════════════════
	printHeader("Example Complete")
	_, _ = successStyle.Println("  ✓ Conversational memory service demonstration finished")
	_, _ = dimStyle.Printf("  Database: %s\n", dbPath)
	_, _ = dimStyle.Printf("  Size: ~%.1f KB (depends on content)\n", float64(totalBytes)/1024)
	fmt.Println()
	_, _ = infoStyle.Println("  Next steps:")
	_, _ = dimStyle.Println("    • Integrate with your LLM client")
	_, _ = dimStyle.Println("    • Implement your own TokenBudgeter (e.g., using tiktoken)")
	_, _ = dimStyle.Println("    • Add encryption for production deployments")
	_, _ = dimStyle.Println("    • Use SearchCells → LoadMultiContextPack for semantic seed selection")
	_, _ = dimStyle.Println("    • See docs/hexxladb/API_REFERENCE.md for full API")
	fmt.Println()

	return nil
}

// spiralCoord generates coordinates in a spiral pattern from center
func spiralCoord(index int) lattice.Coord {
	if index == 0 {
		return lattice.Coord{Q: 0, R: 0}
	}

	// Simple grid layout for demo
	n := index - 1
	q := (n % 7) - 3
	r := (n / 7) - 3
	return lattice.Coord{Q: q, R: r}
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
