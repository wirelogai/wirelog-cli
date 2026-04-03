package output

import (
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
)

const (
	minColWidth = 4
	maxColWidth = 60
	colPadding  = 2
)

func printTable(w io.Writer, headers []string, rows [][]string, noColor bool) error {
	if len(headers) == 0 {
		return nil
	}

	termWidth := 120
	if fd := int(os.Stdout.Fd()); term.IsTerminal(fd) {
		if tw, _, err := term.GetSize(fd); err == nil && tw > 0 {
			termWidth = tw
		}
	}

	// Calculate column widths
	colWidths := make([]int, len(headers))
	for i, h := range headers {
		colWidths[i] = utf8.RuneCountInString(h)
	}
	for _, row := range rows {
		for i := range headers {
			if i < len(row) {
				cw := utf8.RuneCountInString(row[i])
				if cw > colWidths[i] {
					colWidths[i] = cw
				}
			}
		}
	}

	// Cap columns and distribute width
	for i := range colWidths {
		if colWidths[i] < minColWidth {
			colWidths[i] = minColWidth
		}
		if colWidths[i] > maxColWidth {
			colWidths[i] = maxColWidth
		}
	}

	// Shrink columns if total exceeds terminal width
	totalWidth := len(headers) * colPadding
	for _, cw := range colWidths {
		totalWidth += cw
	}
	if totalWidth > termWidth && len(headers) > 0 {
		excess := totalWidth - termWidth
		for excess > 0 {
			widest := 0
			for i, cw := range colWidths {
				if cw > colWidths[widest] {
					widest = i
				}
			}
			if colWidths[widest] <= minColWidth {
				break
			}
			colWidths[widest]--
			excess--
		}
	}

	// Styles
	headerStyle := lipgloss.NewStyle().Bold(true)
	dimStyle := lipgloss.NewStyle()
	if !noColor {
		headerStyle = headerStyle.Foreground(lipgloss.Color("#00ff88"))
		dimStyle = dimStyle.Foreground(lipgloss.Color("#555555"))
	}

	pad := strings.Repeat(" ", colPadding)

	// Render header
	var headerParts []string
	for i, h := range headers {
		cell := padOrTruncate(h, colWidths[i])
		headerParts = append(headerParts, headerStyle.Render(cell))
	}
	if _, err := fmt.Fprintln(w, strings.Join(headerParts, pad)); err != nil {
		return err
	}

	// Separator
	var sepParts []string
	for _, cw := range colWidths {
		sepParts = append(sepParts, dimStyle.Render(strings.Repeat("─", cw)))
	}
	if _, err := fmt.Fprintln(w, strings.Join(sepParts, pad)); err != nil {
		return err
	}

	// Rows
	for _, row := range rows {
		var parts []string
		for i := range headers {
			val := ""
			if i < len(row) {
				val = row[i]
			}
			cell := padOrTruncate(val, colWidths[i])
			parts = append(parts, cell)
		}
		if _, err := fmt.Fprintln(w, strings.Join(parts, pad)); err != nil {
			return err
		}
	}

	return nil
}

func padOrTruncate(s string, width int) string {
	runes := []rune(s)
	if len(runes) > width {
		if width > 1 {
			return string(runes[:width-1]) + "…"
		}
		return string(runes[:width])
	}
	return s + strings.Repeat(" ", width-len(runes))
}

// Color helpers

// GreenText renders text in WireLog green.
func GreenText(s string, noColor bool) string {
	if noColor {
		return s
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color("#00ff88")).Render(s)
}

// DimText renders text in dim gray.
func DimText(s string, noColor bool) string {
	if noColor {
		return s
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color("#7a8a7a")).Render(s)
}

// WarnText renders text in warning red.
func WarnText(s string, noColor bool) string {
	if noColor {
		return s
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color("#ff6b6b")).Render(s)
}

// Internal aliases for use within the package.
func green(s string, noColor bool) string { return GreenText(s, noColor) }
func dim(s string, noColor bool) string   { return DimText(s, noColor) }
