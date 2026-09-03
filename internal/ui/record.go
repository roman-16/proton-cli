package ui

import (
	"fmt"
	"strings"
)

// Field is one labelled value in a record.
//
// Labels are Title Case words, spelled out: "Signature", never "Sig" or
// "signature". The label column is measured at render time from the fields
// actually present, so no view carries a hand-tuned width that a longer label
// can silently break.
type Field struct {
	Label string
	Value string
	// Always keeps the field even when Value is empty, for facts whose absence
	// is itself information ("Signature: (none)").
	Always bool
	// ID paints the value as a Proton reference to the record itself and shortens
	// it on a terminal.
	ID bool
	// Ref paints the value as a reference to something in another collection,
	// named by the command line that lists it. It reads like ID; the difference is
	// where the reference belongs, which is what lets the item a link points at be
	// found again under items.
	Ref string
	// Handle marks the value as the name a person would use for the record - a
	// subject, a title, an address - which is a reference to it just as its ID is.
	Handle bool
	// Role says what the value means, for the fields that carry a verdict rather
	// than plain data.
	Role Role
	// Swatch is the hex the value names, drawn as a dot in front of it.
	Swatch string
}

// RecordSpec describes a single object.
type RecordSpec struct {
	Fields []Field
	// Object replaces the field list in machine formats. It should be the
	// service's own struct, so JSON keeps its snake_case tags rather than
	// echoing display labels.
	Object any
}

// Record renders one object as an aligned label/value block on Out, or as the
// object itself in a machine format.
func Record(u *UI, spec RecordSpec) error {
	if u.Format.Machine() {
		return u.encode(spec.Object)
	}
	writeFields(u, spec.Fields, "")
	return nil
}

// writeFields draws a label/value block, prefixed by indent. Empty fields are
// dropped unless marked Always. A value containing newlines continues in the
// value column, so a wrapped address or a short signature stays visually inside
// its field.
func writeFields(u *UI, fields []Field, indent string) {
	visible := make([]Field, 0, len(fields))
	for _, f := range fields {
		if f.Value == "" && !f.Always {
			continue
		}
		visible = append(visible, f)
	}
	if len(visible) == 0 {
		return
	}

	width := 0
	for _, f := range visible {
		if n := Cells(f.Label); n > width {
			width = n
		}
	}
	width++ // the colon

	short := u.ShortIDs()
	style := u.style
	for _, f := range visible {
		label := pad(f.Label+":", width, false)
		value := f.Value
		switch {
		case f.ID || f.Ref != "":
			value = style.Paint(Accent, Short(value, short))
		case f.Swatch != "":
			value = style.Swatch(f.Swatch, GlyphSwatch) + " " + value
		default:
			value = style.Paint(f.Role, value)
		}
		lines := strings.Split(value, "\n")
		_, _ = fmt.Fprintf(u.Out, "%s%s  %s\n", indent, style.Paint(Muted, label), lines[0])
		for _, cont := range lines[1:] {
			_, _ = fmt.Fprintf(u.Out, "%s%s  %s\n", indent, spaces(width), cont)
		}
	}
}
