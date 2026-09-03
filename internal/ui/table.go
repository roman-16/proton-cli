package ui

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"
)

// Column describes one column of a collection. Cell extracts the text; the
// flags say how it is treated.
type Column[T any] struct {
	// Header is the column title: one uppercase word, or SNAKE_CASE. Never a
	// glyph - a reader should be able to say the name out loud.
	Header string
	// Cell extracts the text. It is what every column but a status column uses;
	// see Marks for the exception.
	Cell func(T) string
	// ID marks the cell as a Proton reference to the row itself: shortened on a
	// terminal unless full IDs were asked for, and painted as one.
	ID bool
	// Ref marks the cell as a reference to something in another collection,
	// named by the command line that lists it. It is painted and shortened like
	// ID; the difference is where the reference belongs, which is what lets a
	// message ID shown beside an attachment be found again under messages.
	Ref string
	// Handle marks the cell as the name a person would use for the row - a
	// subject, a title, an address - which is a reference to it just as its ID is.
	Handle bool
	// Role says what the cell's value means, for the columns that carry a verdict
	// or a marker rather than plain data. Nil is Plain throughout.
	Role func(T) Role
	// Marks replaces Cell for a status column, whose glyphs each mean something
	// different and are therefore painted one by one.
	Marks func(T) Marks
	// Swatch returns the hex colour the cell's value names, drawn as a dot in
	// front of it. It is how a label, folder, calendar or group shows the colour
	// Proton gave it, instead of printing a hex code at a person.
	Swatch func(T) string
	// Flex allows the column to be narrowed when the table is wider than the
	// terminal. Columns without it keep their natural width.
	Flex bool
	// Right aligns the cell to the right, for counts and sizes.
	Right bool
}

// role is the cell's meaning, or Plain when the column declares none.
func (c Column[T]) role(row T) Role {
	if c.Role == nil {
		return Plain
	}
	return c.Role(row)
}

// TableSpec describes a collection: the columns to draw, and the facts the
// footer and the JSON envelope need.
type TableSpec[T any] struct {
	// Noun is the collection's plural name. It is the JSON envelope key and the
	// word the footer uses, so the two can never disagree.
	Noun    string
	Columns []Column[T]

	// Total, Page, PageSize, Limit and Filtered describe the request; see
	// FooterSpec.
	Total    int
	Page     int
	PageSize int
	Limit    int
	Filtered bool

	// Rows, when set, replaces the marshalled items in machine formats. Use it
	// when the wire shape differs from the table's row type.
	Rows any
	// Extra adds fields to the JSON envelope.
	Extra map[string]any

	// Skipped is what this listing could not show; see IncompleteSpec. It is
	// filled in by kit.List from the invocation's tally rather than by the
	// command, so that an incomplete answer says so everywhere instead of
	// wherever somebody remembered to check.
	Skipped IncompleteSpec
}

// flexFloor is the narrowest a flexible column may be squeezed before the table
// is allowed to overflow instead. Below this, truncation destroys more than it
// saves.
const flexFloor = 12

// Table renders a collection. In text mode it draws an aligned table on Out and
// a one-line summary on Err; in a machine format it writes the envelope on Out.
//
// An empty collection produces no bytes on Out at all - just the summary on Err
// - so a redirect yields an empty file rather than a stray header.
func Table[T any](u *UI, spec TableSpec[T], items []T) error {
	if u.Format.Machine() {
		return u.encode(envelope(spec, items))
	}

	if len(items) > 0 {
		writeTable(u, spec, items)
	}
	u.Hint(Footer(FooterSpec{
		Noun: spec.Noun, Count: len(items), Total: spec.Total,
		Page: spec.Page, PageSize: spec.PageSize, Limit: spec.Limit,
		Filtered: spec.Filtered,
	}))
	u.Incomplete(spec.Skipped)
	return nil
}

// envelope builds the machine-format object. Every collection has the same
// shape: the rows under their plural noun, plus the facts that were actually
// established. Fields the request did not involve are omitted rather than
// reported as zero.
// An empty collection is an empty array, never null: a nil slice is how Go
// spells "none", not how the contract does, and `.items[]` has to keep working.
func envelope[T any](spec TableSpec[T], items []T) map[string]any {
	if items == nil {
		items = []T{}
	}
	var rows any = items
	if spec.Rows != nil {
		rows = spec.Rows
	}
	env := map[string]any{
		spec.Noun: rows,
		"count":   len(items),
	}
	if spec.Total != Unknown {
		env["total"] = spec.Total
	}
	if spec.Page != Unpaged && spec.PageSize > 0 {
		env["page"] = spec.Page
		env["page_size"] = spec.PageSize
		env["has_more"] = spec.Total != Unknown && (spec.Page+1)*spec.PageSize < spec.Total
	}
	if spec.Limit > 0 {
		env["limited"] = len(items) >= spec.Limit
	}
	// Omitted when nothing was skipped, so a listing that is complete has exactly
	// the shape it has always had. A consumer that never sees the key is reading a
	// whole answer; one that sees it has been told the answer is short, which a
	// warning on the commentary stream could never tell it.
	if spec.Skipped.Count > 0 {
		env["skipped"] = spec.Skipped.Count
	}
	for k, v := range spec.Extra {
		env[k] = v
	}
	return env
}

func writeTable[T any](u *UI, spec TableSpec[T], items []T) {
	cols := spec.Columns
	short := u.ShortIDs()

	rows := make([][]string, 0, len(items))
	paints := make([][]cellPaint, 0, len(items))
	for _, it := range items {
		row := make([]string, len(cols))
		paint := make([]cellPaint, len(cols))
		for i, c := range cols {
			var v string
			switch {
			case c.Marks != nil:
				paint[i].marks = c.Marks(it)
				v = paint[i].marks.String()
			case c.ID || c.Ref != "":
				v = Short(c.Cell(it), short)
				paint[i].role = Accent
			default:
				v = c.Cell(it)
				if c.Swatch != nil && v != "" {
					// The dot is part of the cell's text, so it is measured and
					// truncated with it rather than smuggled in at draw time.
					paint[i].swatch = c.Swatch(it)
					v = GlyphSwatch + " " + v
				}
				paint[i].role = c.role(it)
			}
			row[i] = v
		}
		rows = append(rows, row)
		paints = append(paints, paint)
	}

	widths := layout(cols, rows, u.width())
	style := u.style

	heads := make([]string, len(cols))
	rules := make([]string, len(cols))
	for i, c := range cols {
		heads[i] = pad(c.Header, widths[i], c.Right)
		rules[i] = strings.Repeat(GlyphRule, widths[i])
	}
	_, _ = fmt.Fprintln(u.Out, style.Paint(Muted, strings.TrimRight(strings.Join(heads, "  "), " ")))
	_, _ = fmt.Fprintln(u.Out, style.Paint(Muted, strings.Join(rules, "  ")))

	for r, row := range rows {
		// Cells are assembled first so trailing empty ones can be dropped whole.
		// Trimming the finished line instead would be wrong: styling wraps a cell,
		// so padding can end up inside an escape sequence where no trim reaches it.
		cells := make([]string, len(cols))
		last := -1
		for i, c := range cols {
			if row[i] == "" {
				cells[i] = spaces(widths[i])
				continue
			}
			last = i
			cells[i] = style.cell(pad(truncate(row[i], widths[i]), widths[i], c.Right), paints[r][i])
		}
		if last < 0 {
			_, _ = fmt.Fprintln(u.Out)
			continue
		}
		// The rightmost populated cell needs no padding after it.
		if !cols[last].Right {
			bare := strings.TrimRight(truncate(row[last], widths[last]), " ")
			cells[last] = style.cell(bare, paints[r][last])
		}
		_, _ = fmt.Fprintln(u.Out, strings.Join(cells[:last+1], "  "))
	}
}

// cellPaint is how one cell is painted, decided while the row is built so the
// drawing loop has nothing left to work out.
type cellPaint struct {
	// swatch is the hex whose dot opens the cell, when the column names a colour.
	swatch string
	// marks are the individually painted glyphs of a status cell.
	marks Marks
	role  Role
}

// cell paints one finished cell.
//
// A swatch colours only its leading dot, and a status run only its glyphs: the
// text beside them is ordinary, and painting that too would make every row a
// different colour instead of pointing at the one thing that has one.
//
// Both forms keep whatever padding follows outside the escapes, and both fall
// back to a plain cell if truncation has eaten the prefix they describe.
func (s Style) cell(text string, p cellPaint) string {
	switch {
	case p.swatch != "" && strings.HasPrefix(text, GlyphSwatch):
		return s.Swatch(p.swatch, GlyphSwatch) + text[len(GlyphSwatch):]
	case len(p.marks) > 0:
		if plain := p.marks.String(); strings.HasPrefix(text, plain) {
			return s.paintMarks(p.marks) + text[len(plain):]
		}
		return text
	}
	return s.Paint(p.role, text)
}

// layout sizes every column to its content, then, while the table is wider than
// maxWidth, shaves a character off the widest flexible column. Narrowing the
// widest column keeps the loss where there is most to spare, unlike shrinking a
// fixed position such as the last column.
//
// maxWidth <= 0 means unlimited: nothing is truncated.
//
// Widths are counted in terminal cells, so a subject in Japanese and a subject
// in German each end where the column does.
func layout[T any](cols []Column[T], rows [][]string, maxWidth int) []int {
	widths := make([]int, len(cols))
	for i, c := range cols {
		widths[i] = Cells(c.Header)
	}
	for _, row := range rows {
		for i, cell := range row {
			if n := Cells(cell); n > widths[i] {
				widths[i] = n
			}
		}
	}
	if maxWidth <= 0 {
		return widths
	}

	total := func() int {
		n := 2 * (len(widths) - 1)
		for _, w := range widths {
			n += w
		}
		return n
	}
	for total() > maxWidth {
		victim, best := -1, flexFloor
		for i, c := range cols {
			if c.Flex && widths[i] > best {
				victim, best = i, widths[i]
			}
		}
		if victim < 0 {
			break
		}
		widths[victim]--
	}
	return widths
}

// width is the column budget for a table, or 0 for no budget at all.
//
// Truncation is a courtesy for a human whose terminal would otherwise wrap. It
// is never applied to a pipe or a file: there, a shortened subject is not a
// tidier table, it is corrupted data. So a non-terminal destination gets its
// full natural width, and only an explicit --width or a real terminal imposes
// one.
func (u *UI) width() int {
	if u.Width > 0 {
		return u.Width
	}
	f, ok := u.Out.(*os.File)
	if !ok {
		return 0
	}
	cols, _, err := term.GetSize(int(f.Fd()))
	if err != nil || cols <= 0 {
		return 0
	}
	return cols
}

func pad(s string, width int, right bool) string { return padCells(s, width, right) }

func truncate(s string, max int) string { return truncateCells(s, max) }
