package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/term"
)

// printTable prints a formatted table to stdout.
func printTable(headers []string, rows [][]string) {
	if len(rows) == 0 {
		fmt.Fprintln(os.Stderr, "(no results)")
		return
	}

	// Calculate column widths.
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = utf8.RuneCountInString(h)
	}
	for _, row := range rows {
		for i, cell := range row {
			if i < len(widths) {
				n := utf8.RuneCountInString(cell)
				if n > widths[i] {
					widths[i] = n
				}
			}
		}
	}

	// Cap to terminal width if possible.
	termWidth := 120
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 0 {
		termWidth = w
	}

	// Shrink last column if total exceeds terminal width.
	totalWidth := len(widths) - 1
	for _, w := range widths {
		totalWidth += w + 2
	}
	if totalWidth > termWidth && len(widths) > 1 {
		excess := totalWidth - termWidth
		last := len(widths) - 1
		if widths[last] > excess+10 {
			widths[last] -= excess
		}
	}

	// Print header.
	var headerLine strings.Builder
	var sepLine strings.Builder
	for i, h := range headers {
		if i > 0 {
			headerLine.WriteString("  ")
			sepLine.WriteString("  ")
		}
		headerLine.WriteString(padRight(h, widths[i]))
		sepLine.WriteString(strings.Repeat("─", widths[i]))
	}
	fmt.Println(headerLine.String())
	fmt.Println(sepLine.String())

	// Print rows.
	for _, row := range rows {
		var line strings.Builder
		for i := 0; i < len(headers); i++ {
			if i > 0 {
				line.WriteString("  ")
			}
			cell := ""
			if i < len(row) {
				cell = row[i]
			}
			line.WriteString(padRight(truncate(cell, widths[i]), widths[i]))
		}
		fmt.Println(line.String())
	}
}

func padRight(s string, width int) string {
	n := utf8.RuneCountInString(s)
	if n >= width {
		return s
	}
	return s + strings.Repeat(" ", width-n)
}

func truncate(s string, maxWidth int) string {
	if utf8.RuneCountInString(s) <= maxWidth {
		return s
	}
	runes := []rune(s)
	if maxWidth <= 3 {
		return string(runes[:maxWidth])
	}
	return string(runes[:maxWidth-1]) + "…"
}

// parseICalField extracts a field value from iCal/vCard text.
func parseICalField(text, field string) string {
	prefix := field + ":"
	prefixParam := field + ";"
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, prefix) {
			return strings.TrimPrefix(line, prefix)
		}
		if strings.HasPrefix(line, prefixParam) {
			idx := strings.Index(line, ":")
			if idx >= 0 {
				return line[idx+1:]
			}
		}
		if strings.Contains(line, "."+field+";") || strings.Contains(line, "."+field+":") {
			idx := strings.Index(line, ":")
			if idx >= 0 {
				return line[idx+1:]
			}
		}
	}
	return ""
}

// isLikelyID returns true if s looks like a Proton base64 ID.
// Proton IDs are base64-encoded, ~88 chars, and end with ==.
func isLikelyID(s string) bool {
	return len(s) > 60 && strings.HasSuffix(s, "==")
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if h == 0 {
		return fmt.Sprintf("%dm", m)
	}
	if m == 0 {
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dh%dm", h, m)
}

func formatSize(bytes int64) string {
	switch {
	case bytes >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(1<<20))
	case bytes >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
