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
	separatorStyle.Println(strings.Repeat("═", 60))
	headerStyle.Println("  ▶ " + title)
	separatorStyle.Println(strings.Repeat("═", 60))
}

func printSuccess(msg string) {
	successStyle.Println("  ✓ " + msg)
}

func printInfo(label, value string) {
	infoStyle.Printf("  %-20s ", label+":")
	dataStyle.Println(value)
}

func printMetric(name string, value any, unit string) {
	metricStyle.Printf("  📊 %-25s ", name+":")
	accentStyle.Printf("%v", value)
	if unit != "" {
		dimStyle.Println(" " + unit)
	} else {
		fmt.Println()
	}
}

func main() {
	log.SetFlags(0)
	if err := run(); err != nil {
		errorStyle.Println("\n  ✗ Error: " + err.Error())
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()
	dir := filepath.Join(".tmp", "conversational_memory")
	_ = os.MkdirAll(dir, 0750)

	printHeader("Conversational Memory Service")
	infoStyle.Println("  Building a production LLM memory system with HexxlaDB")
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

	infoStyle.Println("  Opening database with MVCC enabled...")
	db, err := hexxladb.Open(dbPath, opts)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	printSuccess("Database initialized")
	printInfo("Path", dbPath)
	printInfo("MVCC", "enabled (retain 100 commits)")
	fmt.Println()

	// ═══════════════════════════════════════════════════════════════
	// PHASE 2: Store Conversation Session
	// ═══════════════════════════════════════════════════════════════
	printHeader("Phase 2: Storing Conversation History")

	// Simulate a conversation with user preferences that change (to demo seams)
	conversation := []struct {
		role    string
		content string
		tags    []string
	}{
		{"user", "I prefer detailed technical explanations.", []string{"preferences", "user-123"}},
		{"assistant", "Understood. I'll provide detailed technical responses.", []string{"acknowledgment", "user-123"}},
		{"user", "Actually, keep it concise. I changed my mind.", []string{"preferences", "user-123", "contradiction"}},
		{"assistant", "Noted. I'll be concise going forward.", []string{"acknowledgment", "user-123"}},
		{"user", "What's the best database for LLM memory?", []string{"question", "user-123"}},
		{"assistant", "HexxlaDB is designed for spatial LLM memory...", []string{"answer", "user-123", "hexxladb"}},
	}

	infoStyle.Printf("  Processing %d conversation turns...\n", len(conversation))
	fmt.Println()

	var cells []lattice.Coord
	sessionID := fmt.Sprintf("session-%d", time.Now().Unix())

	for i, msg := range conversation {
		// Assign coordinates in a spiral pattern around center
		coord := spiralCoord(i)
		cells = append(cells, coord)

		pk, _ := lattice.Pack(coord)
		rec := record.CellRecord{
			Key:        pk,
			RawContent: msg.content,
			Tags:       msg.tags,
			Provenance: record.ProvenanceWire{
				SourceID:   fmt.Sprintf("%s/%s", sessionID, msg.role),
				Confidence: 1.0,
				CreatedAt:  time.Now().Add(time.Duration(i) * time.Second).UnixNano(),
				UpdatedAt:  time.Now().Add(time.Duration(i) * time.Second).UnixNano(),
			},
		}

		err := db.Update(func(tx *hexxladb.Tx) error {
			return tx.PutCell(ctx, rec)
		})
		if err != nil {
			return fmt.Errorf("store message %d: %w", i, err)
		}

		roleColor := dimStyle
		if msg.role == "user" {
			roleColor = color.New(color.FgGreen)
		} else {
			roleColor = color.New(color.FgBlue)
		}

		dimStyle.Printf("    [%d] ", i)
		roleColor.Printf("%-10s", msg.role)
		dataStyle.Printf(" %s\n", truncate(msg.content, 45))
	}

	fmt.Println()
	printSuccess(fmt.Sprintf("Stored %d conversation turns", len(conversation)))
	fmt.Println()

	// ═══════════════════════════════════════════════════════════════
	// PHASE 3: Detect Contradictions (Seams)
	// ═══════════════════════════════════════════════════════════════
	printHeader("Phase 3: Contradiction Detection")

	infoStyle.Println("  Checking for conflicting preferences...")
	fmt.Println()

	// User changed their preference - mark as conflict
	if len(cells) >= 3 {
		err := db.Update(func(tx *hexxladb.Tx) error {
			return tx.MarkConflict(cells[0], cells[2], "User preference changed: detailed vs concise")
		})
		if err != nil {
			return fmt.Errorf("mark conflict: %w", err)
		}

		warningStyle.Println("  ⚠ Detected contradiction:")
		infoStyle.Printf("    Between: ")
		dataStyle.Println(fmt.Sprintf("cell[0] (%d,%d) and cell[2] (%d,%d)", cells[0].Q, cells[0].R, cells[2].Q, cells[2].R))
		infoStyle.Printf("    Reason:  ")
		dataStyle.Println("User preference changed: detailed vs concise")
		fmt.Println()
		printSuccess("Seam created to track contradiction")
	}
	fmt.Println()

	// ═══════════════════════════════════════════════════════════════
	// PHASE 4: Context Assembly for LLM Prompt
	// ═══════════════════════════════════════════════════════════════
	printHeader("Phase 4: Context Assembly (Token-Budgeted)")

	center := cells[len(cells)-1] // Last message coordinate as context center

	infoStyle.Printf("  Assembling context around coordinate (%d,%d)...\n", center.Q, center.R)
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
	infoStyle.Printf("  Applying byte budget of %d...\n", budget)
	fmt.Println()

	opts2 := hexxladb.DefaultAssembleCellViewOpts()
	cfg := hexxladb.LoadContextBudgetConfig{
		Assemble:          opts2,
		MaxCandidateCells: 64,
		IncludeFacetText:  false,
		IncludeSeams:      true,
		SeamRadius:        2,
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
		warningStyle.Printf("  ⚠ %d seam(s) present in context:\n", len(pack.Seams))
		for _, seam := range pack.Seams {
			dimStyle.Printf("    • %v ↔ %v: ", seam.CellA, seam.CellB)
			dataStyle.Println(seam.Reason)
		}
	}

	fmt.Println()
	printSuccess("Context assembled within budget")
	fmt.Println()

	// ═══════════════════════════════════════════════════════════════
	// PHASE 5: Query Patterns
	// ═══════════════════════════════════════════════════════════════
	printHeader("Phase 5: Query Patterns")

	// Query by tag
	infoStyle.Println("  Query: All cells tagged 'preferences'...")
	var prefCells int
	err = db.View(func(tx *hexxladb.Tx) error {
		return tx.AscendCellsByTag(ctx, "preferences", func(rec record.CellRecord) bool {
			prefCells++
			return true
		})
	})
	if err != nil {
		return fmt.Errorf("query by tag: %w", err)
	}
	printMetric("Preferences found", prefCells, "cells")
	fmt.Println()

	// Query by source
	infoStyle.Println("  Query: All cells from this session...")
	var sessionCells int
	err = db.View(func(tx *hexxladb.Tx) error {
		return tx.AscendCellsBySource(ctx, sessionID, func(rec record.CellRecord) bool {
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
	// PHASE 6: MVCC Snapshot
	// ═══════════════════════════════════════════════════════════════
	printHeader("Phase 6: MVCC Time-Travel Query")

	stats, err := db.StatsMVCC()
	if err != nil {
		return fmt.Errorf("get mvcc stats: %w", err)
	}

	printMetric("Current commit seq", stats.CommitSeq, "")

	if stats.CommitSeq > 1 {
		infoStyle.Printf("  Querying at commit seq %d (after storing 3 messages)...\n", stats.CommitSeq-2)

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
	// COMPLETION
	// ═══════════════════════════════════════════════════════════════
	printHeader("Example Complete")
	successStyle.Println("  ✓ Conversational memory service demonstration finished")
	dimStyle.Printf("  Database: %s\n", dbPath)
	dimStyle.Printf("  Size: ~%.1f KB (depends on content)\n", float64(totalBytes)/1024)
	fmt.Println()
	infoStyle.Println("  Next steps:")
	dimStyle.Println("    • Integrate with your LLM client")
	dimStyle.Println("    • Implement your own TokenBudgeter (e.g., using tiktoken)")
	dimStyle.Println("    • Add encryption for production deployments")
	dimStyle.Println("    • See docs/hexxladb/API_REFERENCE.md for full API")
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
