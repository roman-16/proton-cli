package ui

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/goccy/go-yaml"
)

// A stream is a response with no end: one thing per line, printed the moment it
// happens rather than gathered and shown at once.
//
// It is a collection in every way but that, which is why it is a kind of its own
// rather than a mode of Table. A table measures its columns across every row it
// holds, and a stream has no every row: each line is drawn before the next thing
// exists. So the widths are declared instead of derived, there is no header rule
// to align against, and there is no footer, because the count is not known until
// the reader stops it.
//
// In a machine format the same difference decides the shape: an envelope keyed
// by a plural noun would have to be closed, so a stream writes one object per
// line - which is what jq reads without --slurp - and, in YAML, one document per
// thing.

// StreamColumn is one field of a streamed line.
type StreamColumn[T any] struct {
	// Cell extracts the text.
	Cell func(T) string
	// Width pads the cell to a fixed number of terminal cells, and truncates
	// anything longer. Zero lets the cell run at its natural width, which is
	// what the last column wants.
	Width int
	// ID marks the cell as a Proton reference: shortened on a terminal, painted
	// as one, and never truncated - a reference that cannot be pasted back is
	// worse than a ragged line.
	ID bool
	// Ref marks the cell as a reference to something in another collection, named
	// by the command line that lists it. It reads like ID; the difference is where
	// the reference belongs, which is what lets a message streamed by a watch be
	// found again under messages.
	Ref string
	// Handle marks the cell as the name a person would use for the thing, which
	// is a reference to it just as its ID is.
	Handle bool
	// Role says what the cell's value means. Nil is Plain.
	Role func(T) Role
}

// StreamSpec describes an unbounded response.
type StreamSpec[T any] struct {
	Columns []StreamColumn[T]
	// Object replaces the item in machine formats, when what goes on the wire
	// differs from what the columns read.
	Object func(T) any
	// Opening says what is being watched and how to stop it. It goes to Err
	// before the first line, and is suppressed by --quiet.
	Opening string
}

// Stream is an open response. Open it once, Emit for each thing.
type Stream[T any] struct {
	u    *UI
	spec StreamSpec[T]
}

// Open begins a stream, announcing what it is watching.
func Open[T any](u *UI, spec StreamSpec[T]) *Stream[T] {
	u.Hint(spec.Opening)
	return &Stream[T]{u: u, spec: spec}
}

// Emit writes one thing.
func (s *Stream[T]) Emit(item T) error {
	if s.u.Format.Machine() {
		return s.line(item)
	}
	s.draw(item)
	return nil
}

// line writes one thing as a machine-readable record: compact JSON on one line,
// or a YAML document of its own.
//
// It marshals rather than going through UI.encode because that one indents, and
// an indented object is not a line. Both encoders read the same `json` tags, so
// the two formats still cannot disagree about a field name.
func (s *Stream[T]) line(item T) error {
	var v any = item
	if s.spec.Object != nil {
		v = s.spec.Object(item)
	}
	if s.u.Format == FormatYAML {
		b, err := yaml.Marshal(v)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(s.u.Out, "---\n%s", b)
		return err
	}
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(s.u.Out, "%s\n", b)
	return err
}

func (s *Stream[T]) draw(item T) {
	short := s.u.ShortIDs()
	cells := make([]string, 0, len(s.spec.Columns))
	for _, c := range s.spec.Columns {
		text, role := c.Cell(item), Plain
		switch {
		case c.ID || c.Ref != "":
			text, role = Short(text, short), Accent
		case c.Role != nil:
			role = c.Role(item)
		case c.Width > 0:
			text = truncateCells(text, c.Width)
		}
		cells = append(cells, s.u.style.Paint(role, padCells(text, c.Width, false)))
	}
	_, _ = fmt.Fprintln(s.u.Out, strings.TrimRight(strings.Join(cells, "  "), " "))
}
