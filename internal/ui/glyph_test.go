package ui

import (
	"testing"
	"unicode/utf8"
)

// vocabulary is every glyph the CLI draws, by what it means.
func vocabulary() map[string]string {
	return map[string]string{
		"success": GlyphSuccess, "caution": GlyphCaution, "flagged": GlyphFlagged,
		"unread": GlyphUnread, "starred": GlyphStarred, "expiring": GlyphExpiring,
		"swatch": GlyphSwatch, "rule": GlyphRule, "bar filled": GlyphBarFilled,
	}
}

// Every glyph carries exactly one meaning, and no two meanings share a glyph.
func TestGlyphVocabularyIsDistinct(t *testing.T) {
	glyphs := vocabulary()
	seen := map[string]string{}
	for meaning, g := range glyphs {
		if g == "" {
			t.Errorf("%s has no glyph", meaning)
		}
		if prev, dup := seen[g]; dup {
			t.Errorf("%q means both %q and %q", g, prev, meaning)
		}
		seen[g] = meaning
	}
	// The progress bar's remaining segment reuses the rule glyph deliberately:
	// both are "a line", drawn in different weights.
	if GlyphBarPending != GlyphRule {
		t.Errorf("the bar's pending segment should be the rule glyph, got %q", GlyphBarPending)
	}
}

// Every glyph is one code point of one terminal cell, which is what keeps a
// column a column.
//
// The rule exists because an emoji cannot satisfy it. No monospace font carries
// one, so a terminal asked to draw an emoji substitutes a colour emoji font, and
// those glyphs are two cells wide - enough to push every column after them out
// of line. A paperclip was the CLI's one emoji, and working around its width is
// why the status column had to be pinned last; the attachment marker is a count
// now, and this test is what stops the next one arriving.
func TestEveryGlyphIsOneNarrowCell(t *testing.T) {
	for meaning, g := range vocabulary() {
		if n := utf8.RuneCountInString(g); n != 1 {
			t.Errorf("%s is %q, which is %d code points; a glyph is one", meaning, g, n)
			continue
		}
		r, _ := utf8.DecodeRuneInString(g)
		if r > 0xFFFF {
			t.Errorf("%s is %q (U+%04X), which is outside the BMP and so an emoji", meaning, g, r)
		}
		if w := Cells(g); w != 1 {
			t.Errorf("%s is %q, which occupies %d cells; a glyph occupies one", meaning, g, w)
		}
	}
}

// A run of marks measures as its plain text, so a status column is laid out from
// what it draws rather than from what it draws it with.
func TestMarksMeasureAsPlainText(t *testing.T) {
	m := Marks{{GlyphUnread, Accent}, {GlyphStarred, Caution}, {"3", Muted}}
	if got := m.String(); got != GlyphUnread+GlyphStarred+"3" {
		t.Errorf("marks rendered as %q", got)
	}
	if got := Cells(m.String()); got != 3 {
		t.Errorf("three marks should occupy three cells, got %d", got)
	}
}
