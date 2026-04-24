package hexxladb

import (
	"context"
	"fmt"
	"strings"
)

// HexGridCell holds the label for one cell position in [RenderHexGrid].
type HexGridCell struct {
	Coord Coord
	Label string // short label (truncated to fit cell width)
}

// RenderHexGrid returns an ASCII hex grid centered on center extending to maxR rings.
// labelFn is called for each coordinate; return a short string (≤5 chars) or "" for empty.
// If labelFn is nil, occupied cells show "•" and empty cells show ".".
func RenderHexGrid(ctx context.Context, center Coord, maxR int, labelFn func(Coord) string) string {
	if maxR < 0 {
		return ""
	}
	const cellW = 7 // width per cell including spacing

	// Collect all coords with their offset positions.
	// Offset layout: even-r with center at (0,0).
	type pos struct {
		col, row int
		label    string
	}
	var cells []pos
	minCol, maxCol := 0, 0
	minRow, maxRow := 0, 0

	coords := WalkRings(nil, center, maxR)
	for _, c := range coords {
		if ctx != nil {
			if err := ctx.Err(); err != nil {
				return ""
			}
		}
		// Relative axial coords from center.
		dq := c.Q - center.Q
		dr := c.R - center.R
		// Convert axial to offset grid position for ASCII rendering.
		// col = dq + dr/2 (shifted), row = dr.
		col := dq + dr/2
		row := dr

		label := ""
		if labelFn != nil {
			label = labelFn(c)
		} else {
			label = "."
		}
		if len(label) > 5 {
			label = label[:5]
		}
		cells = append(cells, pos{col: col, row: row, label: label})
		if col < minCol {
			minCol = col
		}
		if col > maxCol {
			maxCol = col
		}
		if row < minRow {
			minRow = row
		}
		if row > maxRow {
			maxRow = row
		}
	}

	// Build grid lines.
	gridW := (maxCol - minCol + 1)
	gridH := (maxRow - minRow + 1)
	// Each row: indent (for hex stagger) + cells.
	lines := make([]string, gridH)
	grid := make([][]string, gridH)
	for i := range grid {
		grid[i] = make([]string, gridW)
		for j := range grid[i] {
			grid[i][j] = ""
		}
	}

	for _, c := range cells {
		gi := c.row - minRow
		gj := c.col - minCol
		grid[gi][gj] = c.label
	}

	for i := range grid {
		actualRow := i + minRow
		// Hex stagger: odd rows get half-cell indent.
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
