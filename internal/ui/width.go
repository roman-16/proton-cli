package ui

import (
	"sort"
	"unicode"
	"unicode/utf8"
)

// Text is laid out in terminal cells, not in runes.
//
// A table that counts runes aligns only for data that happens to be Latin. A
// Japanese subject, a Chinese filename or an emoji in a folder name each occupy
// two cells per rune, so every column after them sits too far right - by as many
// cells as the row had wide characters. Counting cells is what makes a column a
// column.

// Cells is how many terminal columns s occupies.
//
// Three widths exist. Combining marks, joiners and other formatting carry no
// cell of their own. East Asian Wide and Fullwidth characters, and the emoji
// that terminals render from a colour font, take two. Everything else takes one.
func Cells(s string) int {
	total, last := 0, 0
	absorb := false
	for _, r := range s {
		switch {
		// A zero-width joiner fuses what follows into the preceding glyph, so the
		// whole sequence occupies one glyph's worth of cells however long it is.
		case r == zeroWidthJoiner:
			absorb = true
		case absorb:
			absorb = false
		// The emoji presentation selector asks for the colour form of the
		// character before it, which is drawn two cells wide. It is only ever a
		// promotion: a character already wide stays wide.
		case r == variationSelector16:
			if last == 1 {
				total++
				last = 2
			}
		default:
			w := runeCells(r)
			total += w
			last = w
		}
	}
	return total
}

const (
	variationSelector16 = '\uFE0F'
	zeroWidthJoiner     = '\u200D'
)

func runeCells(r rune) int {
	// A rune a terminal would act on is drawn as what it is rather than obeyed, so
	// it is measured as what it is drawn as. The two have to agree: a row carrying
	// one would otherwise be laid out to a width it does not occupy. See guard.go.
	if drawn := shown(r, nil); drawn != "" {
		return len(drawn)
	}
	switch {
	case r < 0x7F:
		return 1
	case unicode.In(r, unicode.Mn, unicode.Me, unicode.Cf):
		return 0
	case inRanges(r, wide):
		return 2
	}
	return 1
}

// interval is an inclusive range of code points, kept sorted for binary search.
type interval struct{ lo, hi rune }

func inRanges(r rune, rs []interval) bool {
	i := sort.Search(len(rs), func(i int) bool { return r <= rs[i].hi })
	return i < len(rs) && r >= rs[i].lo
}

// wide is every code point a terminal draws two cells wide: the East Asian Wide
// and Fullwidth classes, plus the pictographic planes that no monospace font
// carries and every terminal therefore renders from a colour emoji font.
//
// Regional indicators are listed as one cell each, which is what makes a flag -
// always a pair of them - come out as two.
var wide = []interval{
	{0x1100, 0x115F},   // Hangul Jamo, initial consonants
	{0x2E80, 0x303E},   // CJK radicals, Kangxi, CJK symbols and punctuation
	{0x3041, 0x33FF},   // kana, Bopomofo, Hangul compatibility jamo, enclosed CJK
	{0x3400, 0x4DBF},   // CJK unified ideographs extension A
	{0x4E00, 0x9FFF},   // CJK unified ideographs
	{0xA000, 0xA4CF},   // Yi syllables and radicals
	{0xA960, 0xA97F},   // Hangul jamo extended-A
	{0xAC00, 0xD7A3},   // Hangul syllables
	{0xF900, 0xFAFF},   // CJK compatibility ideographs
	{0xFE10, 0xFE19},   // vertical forms
	{0xFE30, 0xFE6F},   // CJK compatibility forms, small form variants
	{0xFF00, 0xFF60},   // fullwidth ASCII and punctuation
	{0xFFE0, 0xFFE6},   // fullwidth currency and signs
	{0x16FE0, 0x16FE4}, // Tangut and Nushu marks
	{0x17000, 0x18CD5}, // Tangut, Khitan
	{0x1B000, 0x1B2FB}, // kana supplement and extended, Nushu
	{0x1F004, 0x1F004}, // mahjong red dragon
	{0x1F0CF, 0x1F0CF}, // playing card black joker
	{0x1F18E, 0x1F18E}, // negative squared AB
	{0x1F191, 0x1F19A}, // squared CL through squared VS
	{0x1F200, 0x1F2FF}, // enclosed ideographic supplement
	{0x1F300, 0x1F9FF}, // pictographs, emoticons, transport, supplemental symbols
	{0x1FA00, 0x1FAFF}, // chess symbols, symbols and pictographs extended-A
	{0x20000, 0x2FFFD}, // CJK unified ideographs extensions B onwards
	{0x30000, 0x3FFFD}, // CJK unified ideographs extension G onwards
}

// padCells extends s with spaces to width cells, on whichever side leaves the
// text where the column wants it.
func padCells(s string, width int, right bool) string {
	fill := width - Cells(s)
	if fill <= 0 {
		return s
	}
	pad := spaces(fill)
	if right {
		return pad + s
	}
	return s + pad
}

func spaces(n int) string {
	const run = "                                                                "
	if n <= len(run) {
		return run[:n]
	}
	out := make([]byte, n)
	for i := range out {
		out[i] = ' '
	}
	return string(out)
}

// truncateCells shortens s to at most max cells, marking the cut with an
// ellipsis. The ellipsis is itself a cell, so it replaces content rather than
// being appended to a full line.
//
// A cut never lands inside a character: a two-cell rune that would straddle the
// limit is dropped whole and the gap it leaves is padded, so the column keeps
// its width either way.
func truncateCells(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if Cells(s) <= max {
		return s
	}
	if max == 1 {
		return "…"
	}
	budget, end := max-1, 0
	for i, r := range s {
		w := runeCells(r)
		if budget-w < 0 {
			break
		}
		budget -= w
		end = i + utf8.RuneLen(r)
	}
	return s[:end] + spaces(budget) + "…"
}
