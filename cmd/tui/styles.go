package main

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/hexxla/hexxladb"
)

var (
	styleAccent = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#6442A5", Dark: "#BEA4FF"}).Bold(true)
	styleActive = styleAccent.Background(lipgloss.AdaptiveColor{Light: "#EEE7F9", Dark: "#382D50"})
	styleDim    = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#555961", Dark: "#A4A9B4"})
	styleBad    = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#A51D34", Dark: "#FF9AA8"})
)

func coordText(c hexxladb.Coord) string { return fmt.Sprintf("(%d,%d)", c.Q, c.R) }

func helpItem(key, action string) string {
	return styleAccent.Render(key) + " " + styleDim.Render(action)
}

func helpLine(items ...string) string { return strings.Join(items, "  ") }

// terminalText discards terminal escape sequences and normalizes control
// characters from database and user text before applying trusted styles.
func terminalText(s string) string {
	return strings.ReplaceAll(terminalBody(s), "\n", " ")
}

func terminalBody(s string) string {
	return strings.Map(func(r rune) rune {
		if r != '\n' && unicode.IsControl(r) {
			return ' '
		}
		return r
	}, ansi.Strip(s))
}

func truncStr(s string, width int) string {
	return ansi.Truncate(terminalText(s), max(0, width), "…")
}

// clipLines clips styled text to the screen without stripping trusted styles.
func clipLines(text string, width, height int) string {
	var lines []string
	for line := range strings.SplitSeq(text, "\n") {
		if len(lines) == height {
			break
		}
		lines = append(lines, ansi.Truncate(line, width, ""))
	}
	return strings.Join(lines, "\n")
}

type column struct {
	label string
	width int // a zero-width column receives the remaining space
	right bool
}

// compactTable uses one line per record. The cursor occupies the leading
// character of the first field and shares the selected row's highlight.
type compactTable struct {
	columns []column
	width   int
}

func (t compactTable) render(count, cursor, room int, row func(int) []string) string {
	widths := make([]int, len(t.columns))
	headers := make([]string, len(t.columns))
	spare := t.width - 1 - 2*(len(t.columns)-1)
	for i, col := range t.columns {
		widths[i] = col.width
		headers[i] = col.label
		spare -= col.width
	}
	for i := range widths {
		if widths[i] == 0 {
			widths[i] = max(1, spare)
			break
		}
	}

	var out strings.Builder
	out.WriteString(styleDim.Bold(true).Render(" " + t.line(headers, widths)))
	start, end := visibleRows(count, cursor, room)
	for i := start; i < end; i++ {
		line := t.line(row(i), widths)
		out.WriteByte('\n')
		if i == cursor {
			out.WriteString(styleActive.Render("›" + line))
		} else {
			out.WriteString(" " + line)
		}
	}
	return out.String()
}

func (t compactTable) line(values []string, widths []int) string {
	parts := make([]string, len(values))
	for i, value := range values {
		part := truncStr(value, widths[i])
		pad := strings.Repeat(" ", widths[i]-ansi.StringWidth(part))
		if t.columns[i].right {
			part = pad + part
		} else {
			part += pad
		}
		parts[i] = part
	}
	return strings.Join(parts, "  ")
}

func visibleRows(count, cursor, room int) (int, int) {
	room = min(count, max(0, room))
	start := min(max(0, cursor-room/2), count-room)
	return start, start + room
}

func rowPosition(cursor, count int) string {
	if count == 0 {
		return "0 rows"
	}
	return fmt.Sprintf("%d / %d", cursor+1, count)
}
