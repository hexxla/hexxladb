package main

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/hexxla/hexxladb"
	"github.com/hexxla/hexxladb/internal/lattice"
)

// dashboardView shows database overview and quick stats
type dashboardView struct {
	db *hexxladb.DB
}

func newDashboardView(db *hexxladb.DB) view {
	return dashboardView{db: db}
}

func (v dashboardView) Update(msg tea.Msg) (view, tea.Cmd) {
	return v, nil
}

func (v dashboardView) View() string {
	var content strings.Builder

	// Get database stats
	stats, _ := v.db.StatsMVCC()

	content.WriteString("HexxlaDB Dashboard\n\n")

	// Database info
	content.WriteString("Database Information\n")
	content.WriteString("===================\n")
	content.WriteString(fmt.Sprintf("MVCC Enabled: %s\n", yesNo(stats.CommitSeq > 0)))
	content.WriteString(fmt.Sprintf("Commit Seq: %d\n", stats.CommitSeq))

	// Cell count
	var cellCount int
	_ = v.db.View(func(tx *hexxladb.Tx) error {
		return tx.AscendRange(nil, []byte("cell\xff"), func(k, v []byte) bool {
			cellCount++
			return cellCount < 10000
		})
	})
	content.WriteString(fmt.Sprintf("Cell Count: %d\n", cellCount))

	content.WriteString("\nQuick Actions\n")
	content.WriteString("============\n")
	content.WriteString("F2 - Open Hex Grid\n")
	content.WriteString("F3 - Cell Inspector\n")
	content.WriteString("F4 - Analytics\n")
	content.WriteString("F5 - Search\n")

	return content.String()
}

// hexGridView displays the hexagonal lattice
type hexGridView struct {
	db     *hexxladb.DB
	center hexxladb.Coord
	radius int
}

func newHexGridView(db *hexxladb.DB) view {
	return hexGridView{
		db:     db,
		center: hexxladb.Coord{Q: 0, R: 0},
		radius: 3,
	}
}

func (v hexGridView) Update(msg tea.Msg) (view, tea.Cmd) {
	return v, nil
}

func (v hexGridView) View() string {
	content := "Hex Grid View\n"
	content += "============\n\n"
	content += fmt.Sprintf("Center: (%d,%d) | Radius: %d\n", v.center.Q, v.center.R, v.radius)
	content += "\nControls:\n"
	content += "Arrow/WASD - Move center\n"
	content += "+/- - Zoom in/out\n"
	content += "Space - Inspect cell\n"
	content += "g - Go to coordinates\n"

	// Try to render a small hex grid
	_ = v.db.View(func(tx *hexxladb.Tx) error {
		grid, err := tx.RenderHexGridFromDB(context.Background(), v.center, v.radius)
		if err == nil {
			content += "\n" + grid
		}
		return nil
	})

	return content
}

// cellInspectorView shows detailed cell information
type cellInspectorView struct {
	db    *hexxladb.DB
	cell  hexxladb.Coord
	found bool
	data  hexxladb.CellView
}

func newCellInspectorView(db *hexxladb.DB) view {
	return cellInspectorView{
		db:   db,
		cell: hexxladb.Coord{Q: 0, R: 0},
	}
}

func (v cellInspectorView) Update(msg tea.Msg) (view, tea.Cmd) {
	return v, nil
}

func (v cellInspectorView) View() string {
	content := "Cell Inspector\n"
	content += "==============\n\n"

	// Load cell data
	if !v.found {
		pk, err := lattice.Pack(v.cell)
		if err == nil {
			_ = v.db.View(func(tx *hexxladb.Tx) error {
				cell, ok, err := tx.GetCell(pk)
				if err == nil && ok {
					v.data = hexxladb.CellView{
						Coord:      v.cell,
						RawContent: cell.RawContent,
						Tags:       cell.Tags,
						Provenance: cell.Provenance,
						Validity:   cell.Validity,
					}
					v.found = true
				}
				return nil
			})
		}
	}

	if v.found {
		content += fmt.Sprintf("Coordinate: (%d,%d)\n", v.cell.Q, v.cell.R)
		content += fmt.Sprintf("Content: %s\n", truncate(v.data.RawContent, 100))
		content += fmt.Sprintf("Tags: %s\n", strings.Join(v.data.Tags, ", "))
		content += fmt.Sprintf("Source: %s\n", v.data.Provenance.SourceID)
		content += fmt.Sprintf("Confidence: %.2f\n", v.data.Provenance.Confidence)
	} else {
		content += fmt.Sprintf("No cell at (%d,%d)\n", v.cell.Q, v.cell.R)
	}

	content += "\nControls:\n"
	content += "Arrow/WASD - Navigate\n"
	content += "g - Go to coordinates\n"

	return content
}

// analyticsView shows statistics and analytics
type analyticsView struct {
	db *hexxladb.DB
}

func newAnalyticsView(db *hexxladb.DB) view {
	return analyticsView{db: db}
}

func (v analyticsView) Update(msg tea.Msg) (view, tea.Cmd) {
	return v, nil
}

func (v analyticsView) View() string {
	content := "Analytics\n"
	content += "=========\n\n"

	// Tag counts
	content += "Top Tags:\n"
	_ = v.db.View(func(tx *hexxladb.Tx) error {
		counts, err := tx.TagCounts(context.Background())
		if err == nil && len(counts) > 0 {
			for i, tc := range counts {
				if i >= 10 {
					break
				}
				content += fmt.Sprintf("  %s: %d\n", tc.Tag, tc.Count)
			}
		}
		return nil
	})

	content += "\nRing Density (radius 5):\n"
	_ = v.db.View(func(tx *hexxladb.Tx) error {
		density, err := tx.RingDensityMap(context.Background(), hexxladb.Coord{Q: 0, R: 0}, 5)
		if err == nil {
			for _, rd := range density {
				bar := strings.Repeat("█", rd.Occupied) + strings.Repeat("░", rd.Total-rd.Occupied)
				content += fmt.Sprintf("  Ring %d: %s %d/%d\n", rd.Ring, bar, rd.Occupied, rd.Total)
			}
		}
		return nil
	})

	return content
}

// searchView allows searching and filtering cells
type searchView struct {
	db      *hexxladb.DB
	query   string
	results []hexxladb.CellView
}

func newSearchView(db *hexxladb.DB) view {
	return searchView{db: db}
}

func (v searchView) Update(msg tea.Msg) (view, tea.Cmd) {
	return v, nil
}

func (v searchView) View() string {
	content := "Search\n"
	content += "======\n\n"

	if v.query == "" {
		content += "Enter search term...\n"
		content += "\nControls:\n"
		content += "Type - Search\n"
		content += "Enter - Execute search\n"
		content += "Tab - Next result\n"
	} else {
		content += fmt.Sprintf("Query: %s\n", v.query)
		content += fmt.Sprintf("Results: %d\n", len(v.results))

		if len(v.results) > 0 {
			content += "\nTop Results:\n"
			for i, cell := range v.results {
				if i >= 5 {
					break
				}
				content += fmt.Sprintf("  (%d,%d) %s\n", cell.Coord.Q, cell.Coord.R, truncate(cell.RawContent, 50))
			}
		}
	}

	return content
}

// Helper functions
func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
