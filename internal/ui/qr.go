package ui

import (
	"fmt"
	"strings"

	qrcode "github.com/skip2/go-qrcode"
)

// A code for a camera to read.
//
// It is the one thing this package draws that is not writing: what reaches the
// screen is an image, and whether it works is decided by a phone rather than by
// a reader. That changes two of the usual rules. It is drawn in the extremes -
// black on white - because the colours are the picture and not a theme's to
// choose, the same reason a swatch names an exact hex. And it is drawn two
// module rows to a line, in half blocks, because a terminal cell is twice as
// tall as it is wide and a code drawn a row to a line is an oblong no scanner
// will read.

// half blocks: one cell carries the two module rows a line covers.
const (
	glyphBoth  = "█"
	glyphUpper = "▀"
	glyphLower = "▄"
	glyphNone  = " "
)

// correction is how much of the code is redundancy. The low level is what a
// screen wants: nothing is smudged or folded here, and every level above it
// makes the code wider, which is what decides whether it fits the window at all.
const correction = qrcode.Low

// QR draws text as a code to scan, on the commentary stream.
//
// A run that cannot draw one says so rather than leaving a person waiting at a
// screen with nothing on it to scan.
func (u *UI) QR(text string) error {
	code, err := qrcode.New(text, correction)
	if err != nil {
		return fmt.Errorf("draw a code for %d characters: %w", len(text), err)
	}
	for _, line := range qrLines(code.Bitmap()) {
		_, _ = fmt.Fprintln(u.Err, u.errStyle.Scannable(line))
	}
	return nil
}

// qrLines folds a module grid into lines of half blocks, a dark module drawn as
// ink. An odd number of rows leaves the last line's lower half blank, which the
// quiet zone around the code makes indistinguishable from more quiet zone.
func qrLines(modules [][]bool) []string {
	lines := make([]string, 0, (len(modules)+1)/2)
	for y := 0; y < len(modules); y += 2 {
		var b strings.Builder
		for x := range modules[y] {
			upper := modules[y][x]
			lower := y+1 < len(modules) && modules[y+1][x]
			switch {
			case upper && lower:
				b.WriteString(glyphBoth)
			case upper:
				b.WriteString(glyphUpper)
			case lower:
				b.WriteString(glyphLower)
			default:
				b.WriteString(glyphNone)
			}
		}
		lines = append(lines, b.String())
	}
	return lines
}
