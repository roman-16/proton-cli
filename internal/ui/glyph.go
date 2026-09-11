package ui

import "strings"

// The glyph vocabulary. One symbol per meaning, used everywhere that meaning
// appears, so a reader learns each mark once.
//
// Every glyph is one code point of one terminal cell. That is a rule, not a
// coincidence: no monospace font carries an emoji, so a terminal asked to draw
// one substitutes a colour emoji font, and those are two cells wide. A single
// such glyph is enough to push every column after it out of line, which is why
// the attachment marker is a count rather than a paperclip.
const (
	GlyphSuccess    = "✓" // a mutation succeeded
	GlyphCaution    = "!" // it worked, and something about it is worth knowing
	GlyphFlagged    = "✗" // a thing the service itself distrusts
	GlyphUnread     = "●" // an unread message
	GlyphStarred    = "★" // a starred message
	GlyphSwatch     = "■" // the colour a label, folder or calendar is shown in
	GlyphRule       = "─" // a horizontal rule, under table headers
	GlyphBarFilled  = "━" // progress, done
	GlyphBarPending = "─" // progress, remaining
)

// GlyphWorking is one turn of the sign of life a run draws while it waits, one
// frame at a time in the same cell. The one-cell rule holds frame by frame.
const GlyphWorking = "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏"

// Mark is one glyph in a status column, paired with what it means.
type Mark struct {
	Glyph string
	Role  Role
}

// Marks is the run of glyphs a status column draws as a single cell.
//
// They are a list rather than a string because each mark means something
// different, and a cell painted one colour throughout cannot say so. A star is
// the caution colour wherever it appears; the count beside it is context, not a
// state, and is muted accordingly.
type Marks []Mark

// String is the plain text of the run, which is what the table measures,
// truncates and hands to a pipe.
func (m Marks) String() string {
	var b strings.Builder
	for _, mk := range m {
		b.WriteString(mk.Glyph)
	}
	return b.String()
}
