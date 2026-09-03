package ui

import (
	"strconv"
	"strings"
	"testing"
)

// message is the shape a mail list renders, reduced to what the table needs.
type message struct {
	id          string
	from        string
	subject     string
	date        string
	unread      bool
	starred     bool
	attachments int
}

// The fixture mirrors the real mail columns, status column included, so the
// golden files pin the shape the CLI actually prints.
func messageColumns() []Column[message] {
	return []Column[message]{
		{Header: "ID", ID: true, Cell: func(m message) string { return m.id }},
		{Header: "FROM", Flex: true, Cell: func(m message) string { return m.from }},
		{Header: "SUBJECT", Flex: true, Cell: func(m message) string { return m.subject }},
		{Header: "DATE", Cell: func(m message) string { return m.date }},
		{Header: "FLAGS", Marks: func(m message) Marks {
			var mk Marks
			if m.unread {
				mk = append(mk, Mark{GlyphUnread, Accent})
			}
			if m.starred {
				mk = append(mk, Mark{GlyphStarred, Caution})
			}
			if m.attachments > 0 {
				mk = append(mk, Mark{strconv.Itoa(m.attachments), Muted})
			}
			return mk
		}},
	}
}

func messages() []message {
	return []message{
		{"5bH2mQxKT9wLpN4vRs8kZc1yXd7fGh3jAe6bUi0oQm2nWr5tYv==", "Fastmail Billing", "Invoice #2291 is ready", "2026-04-15 14:32", true, true, 3},
		{"9xL4pQrTz2mKd8vBn6cXs1wYf5hJ3gAe7bUi0oQm4nWr2tYv==", "Trailhead Weekly", "The north trail is open again", "2026-04-15 09:02", true, false, 0},
		{"2mNp7RsVx8kLd4vZn1cQs6wYf9hJ5gAe3bUi0oQm7nWr4tYv==", "Jane Roe", "Re: Quarterly numbers", "2026-04-14 17:48", false, false, 0},
	}
}

func TestTablePaginated(t *testing.T) {
	u, out, errb := fixture(t, Options{})
	spec := TableSpec[message]{
		Noun: "messages", Columns: messageColumns(),
		Total: 312, Page: 0, PageSize: 3,
	}
	if err := Table(u, spec, messages()); err != nil {
		t.Fatal(err)
	}
	check(t, "table_paginated", out, errb)
}

func TestTableLastPage(t *testing.T) {
	u, out, errb := fixture(t, Options{})
	spec := TableSpec[message]{
		Noun: "messages", Columns: messageColumns(),
		Total: 6, Page: 1, PageSize: 3,
	}
	if err := Table(u, spec, messages()); err != nil {
		t.Fatal(err)
	}
	check(t, "table_last_page", out, errb)
}

// A short ID is the interactive form: the same table, shortened, is what a user
// copies from. The last row's ID begins with a dash, as about one Proton ID in
// sixty-four does, and the column has to show something a shell will hand back
// to the command rather than to the flag parser.
func TestTableShortIDs(t *testing.T) {
	t.Setenv("PROTON_CLI_FORCE_TTY", "1")
	u, out, errb := fixture(t, Options{})
	spec := TableSpec[message]{Noun: "messages", Columns: messageColumns(), Total: Unknown, Page: Unpaged}
	rows := append(messages(), message{
		"-Qt-s7R_oGCru5u3Kv6Y8QwYf9hJ5gAe3bUi0oQm7nWr4tYv==", "Alpine Club",
		"Hut booking confirmed", "2026-04-14 08:15", false, false, 1,
	})
	if err := Table(u, spec, rows); err != nil {
		t.Fatal(err)
	}
	check(t, "table_short_ids", out, errb)
}

// An empty collection writes nothing at all on stdout, so a redirect yields an
// empty file rather than a stray header.
func TestTableEmptyWritesNothingToStdout(t *testing.T) {
	u, out, errb := fixture(t, Options{})
	spec := TableSpec[message]{Noun: "messages", Columns: messageColumns(), Total: 0, Page: 0, PageSize: 25}
	if err := Table(u, spec, nil); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 {
		t.Errorf("stdout should be empty, got %q", out.String())
	}
	check(t, "table_empty", out, errb)
}

// When the table is too wide, the widest flexible column gives up room. Columns
// without Flex keep their natural width, so a date or an ID is never mangled.
//
// A narrow terminal is also where IDs are shortened, so that is the combination
// tested: full-length IDs plus a narrow budget is not a case that occurs.
func TestTableNarrowShrinksWidestFlexColumn(t *testing.T) {
	t.Setenv("PROTON_CLI_FORCE_TTY", "1")
	u, out, errb := fixture(t, Options{Width: 62})
	spec := TableSpec[message]{Noun: "messages", Columns: messageColumns(), Total: Unknown, Page: Unpaged}
	if err := Table(u, spec, messages()); err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimRight(out.String(), "\n"), "\n") {
		if n := len([]rune(line)); n > 62 {
			t.Errorf("line exceeds the budget (%d > 62): %q", n, line)
		}
	}
	// The DATE column is not flexible, so every row keeps its full timestamp.
	if !strings.Contains(out.String(), "2026-04-15 14:32") {
		t.Error("a non-flex column was truncated")
	}
	check(t, "table_narrow", out, errb)
}

func TestTableRightAlign(t *testing.T) {
	type row struct{ name, size string }
	u, out, errb := fixture(t, Options{})
	spec := TableSpec[row]{
		Noun: "items", Total: Unknown, Page: Unpaged,
		Columns: []Column[row]{
			{Header: "NAME", Flex: true, Cell: func(r row) string { return r.name }},
			{Header: "SIZE", Right: true, Cell: func(r row) string { return r.size }},
		},
	}
	rows := []row{{"report.pdf", "2.4 MB"}, {"notes.txt", "812 B"}, {"archive.tar.gz", "1.1 GB"}}
	if err := Table(u, spec, rows); err != nil {
		t.Fatal(err)
	}
	check(t, "table_right_align", out, errb)
}

// A collection that found nothing still answers with an array, so that reading
// the rows out of any envelope works without a special case for "none".
func TestTableEnvelopeIsAnArrayWhenEmpty(t *testing.T) {
	u, out, errb := fixture(t, Options{Format: FormatJSON})
	spec := TableSpec[message]{Noun: "messages", Columns: messageColumns(), Total: Unknown, Page: Unpaged}
	if err := Table(u, spec, nil); err != nil {
		t.Fatal(err)
	}
	check(t, "table_envelope_empty_json", out, errb)
}

// Every collection has the same envelope, so one consumer can read any list.
func TestTableEnvelope(t *testing.T) {
	u, out, errb := fixture(t, Options{Format: FormatJSON})
	spec := TableSpec[message]{
		Noun: "messages", Columns: messageColumns(),
		Total: 312, Page: 0, PageSize: 3,
		Rows: []map[string]any{{"id": "5bH2mQxK", "subject": "Invoice #2291 is ready"}},
	}
	if err := Table(u, spec, messages()); err != nil {
		t.Fatal(err)
	}
	check(t, "table_envelope_json", out, errb)
}

// An unpaginated collection omits the pagination fields rather than reporting
// them as zero, so a consumer can tell "page 0" from "not paginated".
func TestTableEnvelopeUnpaginated(t *testing.T) {
	u, out, errb := fixture(t, Options{Format: FormatJSON})
	spec := TableSpec[message]{Noun: "messages", Columns: messageColumns(), Total: Unknown, Page: Unpaged}
	if err := Table(u, spec, messages()[:1]); err != nil {
		t.Fatal(err)
	}
	for _, absent := range []string{"page", "page_size", "has_more", "total", "limited"} {
		if strings.Contains(out.String(), `"`+absent+`"`) {
			t.Errorf("unpaginated envelope should omit %q: %s", absent, out.String())
		}
	}
	check(t, "table_envelope_unpaginated_json", out, errb)
}

func TestTableEnvelopeSearchLimited(t *testing.T) {
	u, out, errb := fixture(t, Options{Format: FormatJSON})
	spec := TableSpec[message]{
		Noun: "messages", Columns: messageColumns(),
		Total: Unknown, Page: Unpaged, Limit: 3,
	}
	if err := Table(u, spec, messages()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"limited": true`) {
		t.Errorf("hitting the limit must be reported: %s", out.String())
	}
	check(t, "table_envelope_limited_json", out, errb)
}

func TestTableEnvelopeYAML(t *testing.T) {
	u, out, errb := fixture(t, Options{Format: FormatYAML})
	spec := TableSpec[message]{Noun: "messages", Columns: messageColumns(), Total: 3, Page: 0, PageSize: 25}
	if err := Table(u, spec, messages()[:1]); err != nil {
		t.Fatal(err)
	}
	check(t, "table_envelope_yaml", out, errb)
}

// Machine output is never shortened and never coloured, whatever the terminal is
// doing, because a program is reading it.
func TestTableMachineOutputIgnoresTTY(t *testing.T) {
	t.Setenv("PROTON_CLI_FORCE_TTY", "1")
	u, out, _ := fixture(t, Options{Format: FormatJSON})
	spec := TableSpec[message]{Noun: "messages", Columns: messageColumns(), Total: Unknown, Page: Unpaged}
	if err := Table(u, spec, messages()[:1]); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "\x1b[") {
		t.Error("machine output must carry no escape sequences")
	}
}

// Colour is applied after layout, so enabling it must not move a single column.
func TestTableColourDoesNotAffectLayout(t *testing.T) {
	plain, _, _ := fixture(t, Options{})
	u, out, _ := fixture(t, Options{})
	u.style = Style{enabled: true, direct: true}
	spec := TableSpec[message]{Noun: "messages", Columns: messageColumns(), Total: Unknown, Page: Unpaged}
	if err := Table(u, spec, messages()); err != nil {
		t.Fatal(err)
	}
	coloured := out.String()

	plainOut := &strings.Builder{}
	plain.Out = plainOut
	if err := Table(plain, spec, messages()); err != nil {
		t.Fatal(err)
	}
	if stripANSI(coloured) != plainOut.String() {
		t.Errorf("colour changed the layout\ncoloured (stripped):\n%s\nplain:\n%s",
			stripANSI(coloured), plainOut.String())
	}
}

// The palette itself is pinned, not just the layout.
//
// Every other golden file is captured with colour off, which means the escapes
// have never been reviewable in a diff: a wrong colour, or a colour applied to
// the wrong span, could only be found by looking at a terminal. This is the one
// file where changing a colour shows up as a change.
func TestTableColoured(t *testing.T) {
	u, out, errb := fixture(t, Options{})
	u.style, u.errStyle = Style{enabled: true, direct: true}, Style{enabled: true, direct: true}
	err := Table(u, TableSpec[message]{
		Noun: "messages", Columns: messageColumns(), Total: Unknown, Page: Unpaged,
	}, messages())
	if err != nil {
		t.Fatal(err)
	}
	check(t, "table_coloured", out, errb)
}

// A label's colour is shown rather than described. The dot is painted in the
// colour itself and the name beside it stays ordinary text, so a list of labels
// reads as a list rather than as twenty competing colours.
func TestTableSwatch(t *testing.T) {
	type label struct{ id, name, color string }
	labels := []label{
		{"kQ81mDx4T9wLpN4vRs8kZc==", "Work", "#8080FF"},
		{"7Kd91mQxT2wLpN8vRs4kZc==", "Personal", "#EC3E7C"},
		{"3Ns8pT2vX9kLd4vZn1cQs6==", "Receipts", "#179FD9"},
		{"9xL4pQrTz2mKd8vBn6cXs1==", "Imported", "#123456"},
	}
	cols := []Column[label]{
		{Header: "ID", ID: true, Cell: func(l label) string { return l.id }},
		{Header: "NAME", Flex: true, Cell: func(l label) string { return l.name }},
		{
			Header: "COLOR",
			Swatch: func(l label) string { return l.color },
			Cell: func(l label) string {
				// Mirrors kit.ColorColumn: the palette's name, or the hex when
				// Proton has no name for it.
				if l.color == "#8080FF" {
					return "purple"
				}
				if l.color == "#EC3E7C" {
					return "strawberry"
				}
				if l.color == "#179FD9" {
					return "pacific"
				}
				return l.color
			},
		},
	}

	t.Run("plain", func(t *testing.T) {
		u, out, errb := fixture(t, Options{FullIDs: true})
		if err := Table(u, TableSpec[label]{Noun: "labels", Columns: cols, Total: Unknown, Page: Unpaged}, labels); err != nil {
			t.Fatal(err)
		}
		check(t, "table_swatch", out, errb)
	})

	t.Run("the dot is painted, the name is not", func(t *testing.T) {
		u, out, _ := fixture(t, Options{})
		u.style = Style{enabled: true, direct: true}
		if err := Table(u, TableSpec[label]{Noun: "labels", Columns: cols, Total: Unknown, Page: Unpaged}, labels); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "\x1b[38;2;128;128;255m"+GlyphSwatch+"\x1b[39m") {
			t.Errorf("the swatch should be painted in the colour it names:\n%s", out.String())
		}
		if strings.Contains(out.String(), "\x1b[38;2;128;128;255mWork") {
			t.Errorf("the name should stay ordinary text:\n%s", out.String())
		}
	})
}

// stripANSI removes escape sequences so output can be measured as it is seen.
// A sequence ends at its final byte, anywhere in @ to ~, so this handles the
// colour codes and the progress line's clear-to-end-of-line alike.
func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			for i < len(s) && (s[i] < '@' || s[i] > '~' || s[i] == '[') {
				i++
			}
			i++
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// A listing that is short says so, on the commentary stream and in the
// envelope: the warning is for the person reading and the field is for the
// script that never sees a warning.
func TestTableThatIsShortSaysSo(t *testing.T) {
	u, out, errb := fixture(t, Options{})
	spec := TableSpec[message]{
		Noun: "messages", Columns: messageColumns(), Total: Unknown, Page: Unpaged,
		Skipped: IncompleteSpec{
			Count: 1, Kind: "message",
			Remedy: "This is a bug or damaged data - `proton report` has the details.",
		},
	}
	if err := Table(u, spec, messages()); err != nil {
		t.Fatal(err)
	}
	check(t, "table_incomplete", out, errb)
}

func TestTableThatLostAContainerSaysWhatWentWithIt(t *testing.T) {
	u, out, errb := fixture(t, Options{})
	spec := TableSpec[message]{
		Noun: "messages", Columns: messageColumns(), Total: Unknown, Page: Unpaged,
		Skipped: IncompleteSpec{
			Count: 1, Kind: "folder", Hides: true,
			Remedy: "This is a bug or damaged data - `proton report` has the details.",
		},
	}
	if err := Table(u, spec, messages()); err != nil {
		t.Fatal(err)
	}
	check(t, "table_incomplete_container", out, errb)
}

func TestTheEnvelopeSaysHowManyItCouldNotShow(t *testing.T) {
	u, out, errb := fixture(t, Options{Format: FormatJSON})
	spec := TableSpec[message]{
		Noun: "messages", Columns: messageColumns(), Total: 312, Page: 0, PageSize: 3,
		Skipped: IncompleteSpec{Count: 1, Kind: "message"},
	}
	if err := Table(u, spec, messages()[:1]); err != nil {
		t.Fatal(err)
	}
	check(t, "table_envelope_incomplete_json", out, errb)
}
