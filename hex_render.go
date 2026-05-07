package hexxladb

import (
	"context"
	"fmt"
	"strings"
)

// MaxRenderRadius is the largest maxR accepted by [RenderHexGrid] and [RenderHexGridFromDB].
// At radius 10 the grid is 21 rows × ~21 columns (331 cells); beyond that the ASCII output
// becomes impractical. For larger lattices use [RingDensityMap] for aggregate stats instead.
const MaxRenderRadius = 10

// HexGridCell holds the label for one cell position in [RenderHexGrid].
type HexGridCell struct {
	Coord Coord
	Label string // short label (truncated to fit cell width)
}

// RenderHexGrid returns an ASCII hex grid centered on center extending to maxR rings.
// maxR is clamped to [MaxRenderRadius] (10); use [RingDensityMap] for larger areas.
// labelFn is called for each coordinate; return a short string (≤5 chars) or "" for empty.
// If labelFn is nil, all positions show ".".
func RenderHexGrid(ctx context.Context, center Coord, maxR int, labelFn func(Coord) string) string {
	if maxR < 0 {
		return ""
	}
	if maxR > MaxRenderRadius {
		maxR = MaxRenderRadius
	}

	cells, bounds, cancelled := collectHexPositions(ctx, center, maxR, labelFn)
	if cancelled {
		return ""
	}
	return renderGridLines(cells, bounds)
}

// hexPos is a labelled cell with its offset-grid position.
type hexPos struct {
	col, row int
	label    string
}

// hexBounds tracks the min/max column and row extents.
type hexBounds struct {
	minCol, maxCol, minRow, maxRow int
}

// collectHexPositions gathers labelled positions and their bounding box.
func collectHexPositions(ctx context.Context, center Coord, maxR int, labelFn func(Coord) string) ([]hexPos, hexBounds, bool) {
	coords := WalkRings(nil, center, maxR)
	cells := make([]hexPos, 0, len(coords))
	var b hexBounds
	for _, c := range coords {
		if ctx != nil {
			if err := ctx.Err(); err != nil {
				return nil, b, true
			}
		}
		dq := c.Q - center.Q
		dr := c.R - center.R
		col := dq + dr/2
		row := dr

		label := "."
		if labelFn != nil {
			label = labelFn(c)
		}
		if len(label) > 5 {
			label = label[:5]
		}
		cells = append(cells, hexPos{col: col, row: row, label: label})
		if col < b.minCol {
			b.minCol = col
		}
		if col > b.maxCol {
			b.maxCol = col
		}
		if row < b.minRow {
			b.minRow = row
		}
		if row > b.maxRow {
			b.maxRow = row
		}
	}
	return cells, b, false
}

// renderGridLines converts hex positions into an ASCII grid string.
func renderGridLines(cells []hexPos, b hexBounds) string {
	const cellW = 7

	gridW := b.maxCol - b.minCol + 1
	gridH := b.maxRow - b.minRow + 1
	grid := make([][]string, gridH)
	for i := range grid {
		grid[i] = make([]string, gridW)
	}
	for _, c := range cells {
		grid[c.row-b.minRow][c.col-b.minCol] = c.label
	}

	lines := make([]string, gridH)
	for i := range grid {
		actualRow := i + b.minRow
		indent := ""
		if actualRow%2 != 0 {
			indent = strings.Repeat(" ", cellW/2)
		}
		var parts []string
		for j := range grid[i] {
			label := grid[i][j]
			if label == "" {
				parts = append(parts, strings.Repeat(" ", cellW))
			} else {
				padded := fmt.Sprintf("%-*s", cellW, fmt.Sprintf("[%s]", label))
				parts = append(parts, padded)
			}
		}
		lines[i] = indent + strings.Join(parts, "")
	}
	return strings.Join(lines, "\n")
}

// RenderHexGridFromDB produces an ASCII hex grid showing which cells are occupied in the database.
// Occupied cells show "•", empty positions show ".".
func (tx *Tx) RenderHexGridFromDB(ctx context.Context, center Coord, maxR int) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if tx == nil || tx.db == nil {
		return "", ErrClosed
	}
	occupied := make(map[Coord]bool)
	coords := WalkRings(nil, center, maxR)
	for _, c := range coords {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		p := mustPack(c)
		_, ok, err := tx.GetCell(p)
		if err != nil {
			return "", err
		}
		if ok {
			occupied[c] = true
		}
	}
	result := RenderHexGrid(ctx, center, maxR, func(c Coord) string {
		if occupied[c] {
			return "*"
		}
		return "."
	})
	return result, nil
}
