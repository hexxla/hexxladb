// LLM Context Engine — a realistic demonstration of HexxlaDB as an LLM memory backend.
//
// This example simulates what an LLM orchestrator (like LangChain, OpenAI Assistants,
// or a custom agent framework) would do on every turn of a conversation:
//
//  1. Store each user/assistant turn as a cell with an embedding
//  2. On new user input, retrieve the most relevant past context using embeddings
//  3. Combine semantic search with tag filters for precision
//  4. Assemble a token-budgeted context pack for the LLM prompt
//  5. Handle preference changes via supersession (not deletion)
//  6. Show how different retrieval strategies surface different knowledge
//
// Requires: Ollama running locally with the all-minilm model.
//
//	ollama pull all-minilm
//	go run ./examples/llm_context_engine       # default DB at .tmp/llm-context-engine.db
//	go run ./examples/llm_context_engine -db /path/to/my.db
//	make demo-llm                              # same as first form via Makefile
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fatih/color"

	"github.com/hexxla/hexxladb"
	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
)

// ── Styles ─────────────────────────────────────────────────────────────
var (
	headerStyle = color.New(color.FgHiCyan, color.Bold)
	stepStyle   = color.New(color.FgHiYellow)
	infoStyle   = color.New(color.FgCyan)
	dimStyle    = color.New(color.FgHiBlack)
	dataStyle   = color.New(color.FgGreen)
	warnStyle   = color.New(color.FgYellow)
	errStyle    = color.New(color.FgRed, color.Bold)
	sepStyle    = color.New(color.FgHiBlack)
	okStyle     = color.New(color.FgHiGreen, color.Bold)
)

const lineW = 72

// defaultLLMDBPath is where the LLM context engine demo database lands.
// Kept under .tmp/ so it is gitignored and never pollutes the repo root.
const defaultLLMDBPath = ".tmp/llm-context-engine.db"

func main() {
	dbPath := flag.String("db", defaultLLMDBPath,
		"path to the HexxlaDB demo database (always created fresh on each run)")
	flag.Parse()
	if err := run(*dbPath); err != nil {
		_, _ = errStyle.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

func run(dbPath string) error {
	ctx := context.Background()

	// ── Pre-flight: check Ollama ────────────────────────────────
	if !ollamaReachable() {
		_, _ = errStyle.Println("Ollama not reachable at localhost:11434.")
		_, _ = dimStyle.Println("Run:  ollama serve & ollama pull all-minilm")
		return fmt.Errorf("ollama required for this example")
	}

	// ── Open DB (always fresh — demo is self-contained) ──────────
	_ = os.MkdirAll(filepath.Dir(dbPath), 0o750)
	_ = os.Remove(dbPath)
	_ = os.Remove(dbPath + "-wal")

	db, err := hexxladb.Open(dbPath, &hexxladb.Options{
		EnableMVCC: true,
		PageSize:   65536,
	})
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	defer func() { _ = db.Close() }()

	banner()

	// ================================================================
	// SCENARIO 1: Ingest a conversation history
	// ================================================================
	//
	// An LLM orchestrator stores every turn with:
	//   - RawContent: the message text
	//   - Tags: structured metadata (role, topic, type)
	//   - SourceID: identifies the conversation/session
	//   - Confidence: how certain we are this is a fact vs opinion
	//   - Embedding: 384-dim vector from all-minilm
	//
	// This is what happens AFTER each API round-trip in production.

	printSection("Scenario 1: Ingest Conversation History")
	printNote("Storing 20 turns across 2 sessions with embeddings — simulates real LLM memory writes.")

	turns := conversationHistory()
	var nextQ, nextR int

	start := time.Now()
	for i, t := range turns {
		coord := lattice.Coord{Q: nextQ, R: nextR}
		pk, _ := lattice.Pack(coord)
		nextQ++
		if nextQ > 5 {
			nextQ = 0
			nextR++
		}

		vec, err := embed(t.content)
		if err != nil {
			return fmt.Errorf("embed %d: %w", i, err)
		}

		if err := db.Update(func(tx *hexxladb.Tx) error {
			if err := tx.PutCell(ctx, record.CellRecord{
				Key:        pk,
				RawContent: t.content,
				Tags:       t.tags,
				Provenance: record.ProvenanceWire{
					SourceID:   t.sourceID,
					Confidence: t.confidence,
				},
			}); err != nil {
				return fmt.Errorf("put cell: %w", err)
			}
			return tx.PutEmbedding(pk, vec)
		}); err != nil {
			return fmt.Errorf("ingest turn %d: %w", i, err)
		}
	}
	ingestDur := time.Since(start)

	printMetric("Turns stored", len(turns))
	printMetric("Ingest time", ingestDur.Round(time.Millisecond))
	printMetric("Avg/turn", (ingestDur / time.Duration(len(turns))).Round(time.Millisecond))
	fmt.Println()

	// ================================================================
	// SCENARIO 2: "The user just said something — what do I need to know?"
	// ================================================================
	//
	// This is THE core LLM memory operation. The user sends a new message,
	// and we need to find the most relevant past context to inject into
	// the system prompt.

	printSection("Scenario 2: Semantic Retrieval — 'What do I need to know?'")

	userQueries := []struct {
		message     string
		description string
	}{
		{
			"How should I handle the database connection pooling?",
			"Technical question about databases — should surface DB-related past turns",
		},
		{
			"Remember what I said about keeping things brief?",
			"Meta-question about preferences — should surface communication style turns",
		},
		{
			"What did we discuss about testing strategies?",
			"Recall question — should surface testing-related turns",
		},
	}

	for _, uq := range userQueries {
		printStep(uq.description)
		_, _ = infoStyle.Printf("  User says: %q\n", uq.message)
		fmt.Println()

		queryVec, err := embed(uq.message)
		if err != nil {
			return fmt.Errorf("embed query: %w", err)
		}

		// Pure embedding search — "give me the semantically closest turns"
		var annResults []hexxladb.EmbeddingSearchResult
		err = db.View(func(tx *hexxladb.Tx) error {
			var e error
			annResults, e = tx.SearchByEmbedding(queryVec, hexxladb.EmbeddingSearchConfig{MaxResults: 5})
			return e
		})
		if err != nil {
			return fmt.Errorf("search: %w", err)
		}

		_, _ = dimStyle.Println("  SearchByEmbedding (pure ANN, top 5):")
		err = db.View(func(tx *hexxladb.Tx) error {
			for i, hit := range annResults {
				rec, ok, _ := tx.GetCell(hit.Coord)
				if !ok {
					continue
				}
				c, _ := lattice.Unpack(rec.Key)
				_, _ = dimStyle.Printf("    [%d] sim=%.3f (%d,%d) ", i+1, hit.Score, c.Q, c.R)
				_, _ = dataStyle.Printf("%s\n", trunc(rec.RawContent, 55))
			}
			return nil
		})
		if err != nil {
			return err
		}
		fmt.Println()

		// QueryCells with embedding — combines ANN with composite scoring
		var qResults []hexxladb.CellQueryResult
		err = db.View(func(tx *hexxladb.Tx) error {
			var e error
			qResults, e = tx.QueryCells(ctx, hexxladb.CellQuery{
				Embedding:  queryVec,
				MaxResults: 5,
				SortBy:     hexxladb.SortByScore,
				Explain:    true,
			})
			return e
		})
		if err != nil {
			return fmt.Errorf("query cells: %w", err)
		}

		_, _ = dimStyle.Println("  QueryCells+Embedding (ANN + composite scoring + Explain):")
		for i, r := range qResults {
			_, _ = dimStyle.Printf("    [%d] score=%.3f (%d,%d) ", i+1, r.Score, r.Cell.Coord.Q, r.Cell.Coord.R)
			_, _ = dataStyle.Printf("%s\n", trunc(r.Cell.RawContent, 55))
			if r.Explanation != "" {
				_, _ = dimStyle.Printf("         → %s\n", r.Explanation)
			}
		}
		printSep()
	}

	// ================================================================
	// SCENARIO 3: Multi-signal retrieval
	// ================================================================
	//
	// Real LLM memory isn't just "find similar text." You combine:
	//   - Embedding similarity (semantic)
	//   - Tag filters (structured metadata)
	//   - Confidence thresholds (epistemic quality)
	//   - Source filters (which session/agent)

	printSection("Scenario 3: Multi-Signal Retrieval")
	printNote("Combining embeddings with tag filters and confidence thresholds.")
	fmt.Println()

	type retrieval struct {
		label       string
		query       hexxladb.CellQuery
		embedPrompt string
	}

	retrievals := []retrieval{
		{
			"High-confidence facts about architecture",
			hexxladb.CellQuery{
				RequireTags:   []string{"fact"},
				AnyTags:       []string{"architecture", "database"},
				MinConfidence: 0.8,
				MaxResults:    5,
				SortBy:        hexxladb.SortByScore,
			},
			"software architecture design patterns database",
		},
		{
			"User preferences only (for system prompt injection)",
			hexxladb.CellQuery{
				RequireTags: []string{"preference"},
				MaxResults:  10,
				SortBy:      hexxladb.SortByConfidence,
			},
			"user preferences communication style tooling",
		},
		{
			"Recent testing discussion (session-2 only)",
			hexxladb.CellQuery{
				RequireTags: []string{"testing"},
				SourceID:    "session-2",
				MaxResults:  5,
				SortBy:      hexxladb.SortByScore,
			},
			"testing strategy integration unit tests",
		},
	}

	for _, r := range retrievals {
		printStep(r.label)

		// Add embedding to query for ANN boost
		vec, err := embed(r.embedPrompt)
		if err != nil {
			return fmt.Errorf("embed: %w", err)
		}
		q := r.query
		q.Embedding = vec
		q.Explain = true

		var results []hexxladb.CellQueryResult
		err = db.View(func(tx *hexxladb.Tx) error {
			var e error
			results, e = tx.QueryCells(ctx, q)
			return e
		})
		if err != nil {
			return fmt.Errorf("query: %w", err)
		}

		if len(results) == 0 {
			_, _ = warnStyle.Println("    (no results)")
		}
		for i, res := range results {
			tags := strings.Join(res.Cell.Tags, ", ")
			_, _ = dimStyle.Printf("    [%d] score=%.3f conf=%.1f src=%-10s ",
				i+1, res.Score, res.Cell.Provenance.Confidence, res.Cell.Provenance.SourceID)
			_, _ = dataStyle.Printf("%s\n", trunc(res.Cell.RawContent, 45))
			_, _ = dimStyle.Printf("         tags=[%s]\n", tags)
		}
		fmt.Println()
	}

	// ================================================================
	// SCENARIO 4: Preference supersession
	// ================================================================
	//
	// When a user changes a preference, the old one shouldn't just disappear.
	// In production, you MarkSupersedes so:
	//   - The old preference is still visible in historical snapshots
	//   - FilterSuperseded automatically excludes it from new context packs
	//   - You can audit what changed and when

	printSection("Scenario 4: Preference Supersession")
	printNote("User changes their mind. Old preference is superseded, not deleted.")
	fmt.Println()

	// Find the "keep things brief" preference
	var briefCoord lattice.Coord
	var briefFound bool
	err = db.View(func(tx *hexxladb.Tx) error {
		results, e := tx.QueryCells(ctx, hexxladb.CellQuery{
			Query:       "brief",
			RequireTags: []string{"preference"},
			MaxResults:  1,
		})
		if e != nil {
			return e
		}
		if len(results) > 0 {
			briefCoord = results[0].Cell.Coord
			briefFound = true
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("find brief: %w", err)
	}

	if briefFound {
		_, _ = infoStyle.Printf("  Found old preference at (%d,%d): ", briefCoord.Q, briefCoord.R)
		_ = db.View(func(tx *hexxladb.Tx) error {
			pk, _ := lattice.Pack(briefCoord)
			rec, ok, _ := tx.GetCell(pk)
			if ok {
				_, _ = dataStyle.Printf("%s\n", trunc(rec.RawContent, 55))
			}
			return nil
		})

		// Write new preference
		newCoord := lattice.Coord{Q: nextQ, R: nextR}
		newPK, _ := lattice.Pack(newCoord)

		err = db.Update(func(tx *hexxladb.Tx) error {
			if err := tx.PutCell(ctx, record.CellRecord{
				Key:        newPK,
				RawContent: "Actually, I want detailed explanations with code examples for everything now. I'm learning a new codebase.",
				Tags:       []string{"preference", "communication-style", "user-request"},
				Provenance: record.ProvenanceWire{
					SourceID:   "session-3",
					Confidence: 1.0,
				},
			}); err != nil {
				return err
			}

			vec, err := embed("I want detailed explanations with code examples for everything now")
			if err != nil {
				return fmt.Errorf("embed new pref: %w", err)
			}
			if err := tx.PutEmbedding(newPK, vec); err != nil {
				return err
			}

			// Supersede the old preference
			return tx.MarkSupersedes(newCoord, briefCoord, "User changed communication preference")
		})
		if err != nil {
			return fmt.Errorf("supersede: %w", err)
		}

		_, _ = okStyle.Printf("  ✓  New preference at (%d,%d) supersedes old at (%d,%d)\n",
			newCoord.Q, newCoord.R, briefCoord.Q, briefCoord.R)
		fmt.Println()

		// Now show that context assembly respects supersession
		printStep("Context assembly with FilterSuperseded")
		printNote("LoadContext excludes superseded cells and substitutes successors.")

		var pack hexxladb.ContextPack
		err = db.View(func(tx *hexxladb.Tx) error {
			var e error
			pack, e = tx.LoadContext(ctx, hexxladb.LoadContextConfig{
				Seeds:     []hexxladb.Coord{briefCoord}, // start from the OLD coord — successor should appear instead
				MaxRing:   3,
				MaxTokens: 4096,
				Assembly: hexxladb.LoadContextBudgetConfig{
					FilterSuperseded: true,
					Explain:          true,
				},
			})
			return e
		})
		if err != nil {
			return fmt.Errorf("context pack: %w", err)
		}

		_, _ = infoStyle.Printf("  Context pack: %d cells, %d tokens\n", len(pack.Cells), pack.TotalTokens)
		for _, cv := range pack.Cells {
			marker := " "
			if cv.SupersededFrom != nil {
				marker = "→"
			}
			_, _ = dimStyle.Printf("    %s (%d,%d) conf=%.1f ",
				marker, cv.Coord.Q, cv.Coord.R, cv.Provenance.Confidence)
			_, _ = dataStyle.Printf("%s\n", trunc(cv.RawContent, 48))
		}
		if len(pack.Explanations) > 0 {
			fmt.Println()
			_, _ = dimStyle.Println("  Assembly decisions:")
			for _, ex := range pack.Explanations {
				_, _ = dimStyle.Printf("    (%d,%d) %s", ex.Coord.Q, ex.Coord.R, ex.Reason)
				if ex.SupersededBy != nil {
					_, _ = dimStyle.Printf(" → replaced by (%d,%d)", ex.SupersededBy.Q, ex.SupersededBy.R)
				}
				fmt.Println()
			}
		}
	}
	fmt.Println()

	// ================================================================
	// SCENARIO 5: The full LLM prompt assembly pipeline
	// ================================================================
	//
	// This is what runs on EVERY user message in production:
	//   1. Embed the user's new message
	//   2. Find semantically relevant past turns (ANN)
	//   3. Find preference cells (tag filter)
	//   4. Assemble a token-budgeted context window
	//   5. Format for the LLM prompt
	//
	// This is the payoff — everything working together.

	printSection("Scenario 5: Full LLM Prompt Assembly Pipeline")
	printNote("Simulates what runs on every user message in production.")
	fmt.Println()

	newUserMsg := "Can you help me set up integration tests for my Go HTTP handlers with a real database?"

	_, _ = infoStyle.Printf("  New user message:\n")
	_, _ = dataStyle.Printf("  %q\n\n", newUserMsg)

	msgVec, err := embed(newUserMsg)
	if err != nil {
		return fmt.Errorf("embed new msg: %w", err)
	}

	// Step 1: Find relevant technical context
	printStep("Step 1 — Semantic retrieval: relevant past discussion")
	var techContext []hexxladb.CellQueryResult
	err = db.View(func(tx *hexxladb.Tx) error {
		var e error
		techContext, e = tx.QueryCells(ctx, hexxladb.CellQuery{
			Embedding:     msgVec,
			ExcludeTags:   []string{"preference"}, // don't mix preferences into technical context
			MinConfidence: 0.5,
			MaxResults:    8,
			SortBy:        hexxladb.SortByScore,
		})
		return e
	})
	if err != nil {
		return fmt.Errorf("tech context: %w", err)
	}

	for i, r := range techContext {
		_, _ = dimStyle.Printf("    [%d] score=%.3f (%d,%d) ", i+1, r.Score, r.Cell.Coord.Q, r.Cell.Coord.R)
		_, _ = dataStyle.Printf("%s\n", trunc(r.Cell.RawContent, 52))
	}
	fmt.Println()

	// Step 2: Find user preferences (always injected into system prompt)
	printStep("Step 2 — Preference retrieval: system prompt injection")
	var prefs []hexxladb.CellQueryResult
	err = db.View(func(tx *hexxladb.Tx) error {
		var e error
		prefs, e = tx.QueryCells(ctx, hexxladb.CellQuery{
			RequireTags: []string{"preference"},
			MaxResults:  5,
			SortBy:      hexxladb.SortByConfidence,
		})
		return e
	})
	if err != nil {
		return fmt.Errorf("prefs: %w", err)
	}

	for i, r := range prefs {
		_, _ = dimStyle.Printf("    [%d] conf=%.1f ", i+1, r.Cell.Provenance.Confidence)
		_, _ = dataStyle.Printf("%s\n", trunc(r.Cell.RawContent, 58))
	}
	fmt.Println()

	// Step 3: Build the seed coords from search results
	printStep("Step 3 — Context assembly: token-budgeted neighbourhood walk")
	seeds := make([]hexxladb.Coord, 0, len(techContext))
	seen := map[hexxladb.Coord]bool{}
	for _, r := range techContext[:min(3, len(techContext))] {
		if !seen[r.Cell.Coord] {
			seeds = append(seeds, r.Cell.Coord)
			seen[r.Cell.Coord] = true
		}
	}

	var contextPack hexxladb.ContextPack
	err = db.View(func(tx *hexxladb.Tx) error {
		var e error
		contextPack, e = tx.LoadContext(ctx, hexxladb.LoadContextConfig{
			Seeds:     seeds,
			MaxRing:   2,
			MaxTokens: 2048,
			Assembly: hexxladb.LoadContextBudgetConfig{
				FilterSuperseded: true,
				IncludeSeams:     true,
				Explain:          true,
			},
		})
		return e
	})
	if err != nil {
		return fmt.Errorf("context pack: %w", err)
	}

	_, _ = infoStyle.Printf("    Seeds: %d coords → Context: %d cells, %d tokens (budget: 2048)\n",
		len(seeds), len(contextPack.Cells), contextPack.TotalTokens)
	_, _ = infoStyle.Printf("    Stats: scanned=%d, evicted=%d, max_ring=%d\n",
		contextPack.Stats.CandidatesScanned, contextPack.Stats.CellsEvicted, contextPack.Stats.MaxRingUsed)
	fmt.Println()

	// Step 4: Format the prompt
	printStep("Step 4 — Format LLM prompt")
	fmt.Println()
	_, _ = dimStyle.Println("  ┌─ SYSTEM PROMPT ─────────────────────────────────────────────┐")
	_, _ = dimStyle.Println("  │ You are a helpful coding assistant.                         │")
	_, _ = dimStyle.Println("  │                                                             │")
	_, _ = dimStyle.Println("  │ USER PREFERENCES:                                           │")
	for _, p := range prefs {
		line := fmt.Sprintf("  │   • %s", trunc(p.Cell.RawContent, 55))
		_, _ = dimStyle.Printf("%-64s│\n", line)
	}
	_, _ = dimStyle.Println("  │                                                             │")
	_, _ = dimStyle.Println("  │ RELEVANT CONTEXT FROM MEMORY:                               │")
	for i, cv := range contextPack.Cells {
		if i >= 5 {
			_, _ = dimStyle.Printf("  │   ... (%d more cells)%s│\n",
				len(contextPack.Cells)-5, strings.Repeat(" ", 64-25-len(fmt.Sprintf("%d", len(contextPack.Cells)-5))))
			break
		}
		line := fmt.Sprintf("  │   [%d] %s", i+1, trunc(cv.RawContent, 52))
		_, _ = dimStyle.Printf("%-64s│\n", line)
	}
	_, _ = dimStyle.Println("  └─────────────────────────────────────────────────────────────┘")
	fmt.Println()
	_, _ = dimStyle.Println("  ┌─ USER MESSAGE ────────────────────────────────────────────────┐")
	_, _ = dimStyle.Printf("  │ %s│\n", fmt.Sprintf("%-62s", trunc(newUserMsg, 62)))
	_, _ = dimStyle.Println("  └──────────────────────────────────────────────────────────────┘")
	fmt.Println()

	// ================================================================
	// SCENARIO 6: Show what an LLM CAN'T do without HexxlaDB
	// ================================================================

	printSection("Scenario 6: What HexxlaDB Enables vs. Stateless LLMs")
	fmt.Println()

	capabilities := []struct {
		feature string
		without string
		with    string
	}{
		{
			"Cross-session memory",
			"Each API call is stateless — context lost between sessions",
			"20 turns persisted, retrievable by semantic similarity across sessions",
		},
		{
			"Preference tracking",
			"User repeats preferences every session; contradictions undetected",
			"MarkSupersedes chains track preference evolution; FilterSuperseded auto-resolves",
		},
		{
			"Semantic + structured retrieval",
			"Either vector search OR keyword search, rarely both",
			"QueryCells combines ANN embeddings + tag filters + confidence + source in one call",
		},
		{
			"Token-budgeted context",
			"Naive truncation drops relevant context; overflow breaks generation",
			"LoadContext evicts low-confidence cells from outer rings first",
		},
		{
			"Auditable memory",
			"No history of what the LLM 'knew' at a given time",
			"MVCC ViewAt pins a snapshot; SnapshotDiff shows what changed between turns",
		},
	}

	for _, c := range capabilities {
		_, _ = infoStyle.Printf("  %s\n", c.feature)
		_, _ = dimStyle.Printf("    Without: %s\n", c.without)
		_, _ = dataStyle.Printf("    With:    %s\n", c.with)
		fmt.Println()
	}

	// ================================================================
	// SCENARIO 7: FOV-Filtered Retrieval
	// ================================================================
	//
	// LoadContextFOV uses symmetric shadowcasting on the hex grid.
	// Empty cells act as opaque barriers — only cells the "observer" can
	// see through populated neighborhoods are returned. This is smarter
	// than blind radial loading for sparse grids where large empty gaps
	// would waste the context budget on unreachable cells.

	printSection("Scenario 7: FOV-Filtered Context Loading")
	printNote("Visibility-based retrieval — only cells with clear line-of-sight from center are loaded.")
	printNote("Empty grid positions block vision, so occluded cells are skipped.")
	fmt.Println()

	// Use the first cell as the FOV center
	fovCenter := lattice.Coord{Q: 2, R: 1} // center of our 6-column grid

	// Standard radial loading for comparison
	var radialCount int
	_ = db.View(func(tx *hexxladb.Tx) error {
		packed := lattice.WalkRingsPacked(fovCenter, 3)
		for _, p := range packed {
			_, ok, _ := tx.GetCell(p)
			if ok {
				radialCount++
			}
		}
		return nil
	})

	// FOV-filtered loading — empty cells are opaque barriers
	var fovCells []record.CellRecord
	err = db.View(func(tx *hexxladb.Tx) error {
		opaque := func(c lattice.Coord) bool {
			p, pErr := lattice.Pack(c)
			if pErr != nil {
				return true
			}
			_, ok, _ := tx.GetCell(p)
			return !ok // treat empty cells as opaque barriers
		}
		var e error
		fovCells, e = tx.LoadContextFOV(ctx, fovCenter, 3, opaque, hexxladb.FOVContextConfig{
			MaxCells: 64,
		})
		return e
	})
	if err != nil {
		return fmt.Errorf("fov context: %w", err)
	}

	printStep(fmt.Sprintf("Center: (%d,%d), radius: 3", fovCenter.Q, fovCenter.R))
	printMetric("Radial cells (blind r=3)", radialCount)
	printMetric("FOV cells (visible only)", len(fovCells))
	if radialCount > len(fovCells) {
		printMetric("Cells saved by FOV", radialCount-len(fovCells))
	}
	fmt.Println()

	_, _ = dimStyle.Println("  Visible cells (with content):")
	for i, rec := range fovCells {
		if i >= 8 {
			_, _ = dimStyle.Printf("    ⋯  (%d more cells)\n", len(fovCells)-8)
			break
		}
		c, _ := lattice.Unpack(rec.Key)
		dist := fovCenter.Distance(c)
		_, _ = dimStyle.Printf("    [%d] (%d,%d) dist=%d conf=%.1f ",
			i+1, c.Q, c.R, dist, rec.Provenance.Confidence)
		_, _ = dataStyle.Printf("%s\n", trunc(rec.RawContent, 45))
	}
	fmt.Println()

	_, _ = dimStyle.Println("  Why FOV matters for LLM context:")
	_, _ = dimStyle.Println("    • Sparse grids: empty regions block LOS, preventing budget waste")
	_, _ = dimStyle.Println("    • Dense clusters: FOV naturally loads the connected neighborhood")
	_, _ = dimStyle.Println("    • Custom opacity: mark cells as opaque based on tags, confidence, etc.")
	fmt.Println()

	// ── Completion ──────────────────────────────────────────────
	_, _ = sepStyle.Println(strings.Repeat("═", lineW))
	_, _ = okStyle.Printf("  ✓  LLM Context Engine demo complete\n")
	_, _ = sepStyle.Println(strings.Repeat("═", lineW))
	fmt.Println()
	_, _ = dimStyle.Printf("  Database: %s\n", dbPath)
	_, _ = dimStyle.Printf("  Turns:    %d stored with 384-dim embeddings\n", len(turns))
	_, _ = dimStyle.Printf("  Queries:  %d semantic, %d multi-signal, 1 full pipeline, 1 FOV\n",
		len(userQueries), len(retrievals))
	fmt.Println()

	return nil
}

// ── Conversation data ──────────────────────────────────────────────────

type turn struct {
	content    string
	tags       []string
	sourceID   string
	confidence float64
}

func conversationHistory() []turn {
	return []turn{
		// Session 1: Getting started — preferences and Go basics
		{
			"I prefer concise responses. Bullet points, not paragraphs.",
			[]string{"preference", "communication-style", "user-request"},
			"session-1", 1.0,
		},
		{
			"Got it — concise bullet points going forward.",
			[]string{"acknowledgment", "communication-style", "assistant"},
			"session-1", 0.9,
		},
		{
			"I'm building a REST API in Go using chi router and sqlc for database access.",
			[]string{"fact", "project-context", "go", "architecture", "database"},
			"session-1", 0.95,
		},
		{
			"Good stack. chi is lightweight and composable. sqlc generates type-safe Go from SQL — great for maintainability.",
			[]string{"fact", "go", "architecture", "database", "assistant"},
			"session-1", 0.9,
		},
		{
			"How should I structure error handling across my HTTP handlers?",
			[]string{"question", "go", "errors", "http", "architecture"},
			"session-1", 0.7,
		},
		{
			"Use a central error handler middleware. Map domain errors to HTTP status codes. Never expose internal errors to clients. Log the full error; return a safe message.",
			[]string{"fact", "go", "errors", "http", "best-practice", "assistant"},
			"session-1", 0.95,
		},
		{
			"What about structured logging — should every handler log the request?",
			[]string{"question", "go", "logging", "observability"},
			"session-1", 0.6,
		},
		{
			"Use middleware for request logging (method, path, status, duration). Individual handlers should only log domain-specific events. Use slog with request_id correlation.",
			[]string{"fact", "go", "logging", "observability", "best-practice", "assistant"},
			"session-1", 0.9,
		},
		{
			"I always use table-driven tests. Please suggest test structures in that style.",
			[]string{"preference", "testing", "go", "user-request"},
			"session-1", 1.0,
		},
		{
			"Noted — will use table-driven test patterns with tt (test table) naming convention.",
			[]string{"acknowledgment", "testing", "go", "assistant"},
			"session-1", 0.9,
		},

		// Session 2: Deeper into testing and architecture
		{
			"I've set up the project. Now I need to write integration tests that hit a real Postgres database.",
			[]string{"fact", "testing", "database", "postgres", "integration"},
			"session-2", 0.9,
		},
		{
			"Use testcontainers-go to spin up a throwaway Postgres in each test. Combine with sqlc-generated queries for type-safe assertions. Isolate with per-test schemas or transactions.",
			[]string{"fact", "testing", "database", "postgres", "integration", "best-practice", "assistant"},
			"session-2", 0.95,
		},
		{
			"Should I mock the database layer or always use a real DB in tests?",
			[]string{"question", "testing", "database", "architecture"},
			"session-2", 0.7,
		},
		{
			"Both. Unit tests: mock the repository interface (gomock or hand-written). Integration tests: real DB via testcontainers. Never mock in integration tests — you're testing the integration.",
			[]string{"fact", "testing", "database", "architecture", "best-practice", "assistant"},
			"session-2", 0.95,
		},
		{
			"What about testing HTTP handlers specifically? I want to test the full request/response cycle.",
			[]string{"question", "testing", "http", "go"},
			"session-2", 0.7,
		},
		{
			"httptest.NewServer for full integration; httptest.NewRecorder for unit testing individual handlers. Inject mock services via dependency injection in the handler constructor.",
			[]string{"fact", "testing", "http", "go", "best-practice", "assistant"},
			"session-2", 0.95,
		},
		{
			"My CI is GitHub Actions. How should I run these integration tests there?",
			[]string{"question", "testing", "ci", "github-actions", "operations"},
			"session-2", 0.7,
		},
		{
			"Use service containers in your workflow YAML — Postgres as a sidecar. Set DATABASE_URL in env. Run go test -tags=integration ./... as a separate step from unit tests.",
			[]string{"fact", "testing", "ci", "github-actions", "operations", "best-practice", "assistant"},
			"session-2", 0.95,
		},
		{
			"I want to add OpenTelemetry tracing to my handlers. Does that affect test setup?",
			[]string{"question", "observability", "testing", "opentelemetry"},
			"session-2", 0.7,
		},
		{
			"In tests, use a no-op tracer provider so spans don't fail or add noise. For integration tests that verify tracing: use an in-memory exporter and assert on exported spans.",
			[]string{"fact", "observability", "testing", "opentelemetry", "best-practice", "assistant"},
			"session-2", 0.95,
		},
	}
}

// ── Ollama helpers ─────────────────────────────────────────────────────

const ollamaBase = "http://localhost:11434"

func ollamaReachable() bool {
	resp, err := http.Get(ollamaBase) //nolint:noctx // example code
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func embed(text string) ([]float32, error) {
	body, _ := json.Marshal(map[string]string{"model": "all-minilm", "prompt": text})
	resp, err := http.Post(ollamaBase+"/api/embeddings", "application/json", bytes.NewReader(body)) //nolint:noctx // example code
	if err != nil {
		return nil, fmt.Errorf("ollama: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama status %d", resp.StatusCode)
	}
	var result struct {
		Embedding []float64 `json:"embedding"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	out := make([]float32, len(result.Embedding))
	for i, v := range result.Embedding {
		out[i] = float32(v)
	}
	return out, nil
}

// ── Print helpers ──────────────────────────────────────────────────────

func banner() {
	_, _ = sepStyle.Println(strings.Repeat("═", lineW))
	_, _ = headerStyle.Println("  HexxlaDB — LLM Context Engine Demo")
	_, _ = dimStyle.Println("  Realistic memory retrieval workflow for LLM orchestration")
	_, _ = sepStyle.Println(strings.Repeat("═", lineW))
	fmt.Println()
}

func printSection(title string) {
	_, _ = sepStyle.Println(strings.Repeat("─", lineW))
	_, _ = headerStyle.Printf("  %s\n", title)
	_, _ = sepStyle.Println(strings.Repeat("─", lineW))
}

func printStep(s string) {
	_, _ = stepStyle.Printf("  ◆  %s\n", s)
}

func printNote(s string) {
	_, _ = dimStyle.Printf("  ℹ  %s\n", s)
}

func printMetric(label string, value any) {
	_, _ = infoStyle.Printf("  📊  %-24s %v\n", label+":", value)
}

func printSep() {
	fmt.Println()
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}
