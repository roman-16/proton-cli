package render

import (
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"golang.org/x/term"
)

// Table prints a simple two-space-separated table with a header underline.
// Widths are computed from content and clamped to the terminal width.
func Table(w io.Writer, headers []string, rows [][]string) {
	if len(rows) == 0 {
		fmt.Fprintln(os.Stderr, "(no results)")
		return
	}

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

	termWidth := 120
	if tw, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && tw > 0 {
		termWidth = tw
	}
	total := len(widths) - 1
	for _, wv := range widths {
		total += wv + 2
	}
	if total > termWidth && len(widths) > 1 {
		excess := total - termWidth
		last := len(widths) - 1
		if widths[last] > excess+10 {
			widths[last] -= excess
		}
	}

	var head, sep strings.Builder
	for i, h := range headers {
		if i > 0 {
			head.WriteString("  ")
			sep.WriteString("  ")
		}
		head.WriteString(padRight(h, widths[i]))
		sep.WriteString(strings.Repeat("─", widths[i]))
	}
	_, _ = fmt.Fprintln(w, head.String())
	_, _ = fmt.Fprintln(w, sep.String())

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
		_, _ = fmt.Fprintln(w, line.String())
	}
}

func padRight(s string, width int) string {
	n := utf8.RuneCountInString(s)
	if n >= width {
		return s
	}
	return s + strings.Repeat(" ", width-n)
}

func truncate(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	if max <= 3 {
		return string(r[:max])
	}
	return string(r[:max-1]) + "…"
}
