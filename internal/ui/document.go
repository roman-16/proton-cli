package ui

import (
	"fmt"
	"strings"
)

// Part is one body inside a document: a message in a thread, or the whole of a
// standalone message.
type Part struct {
	// Divider labels the part when a document has more than one, e.g. "2/5".
	Divider string
	// Header is the part's own label/value block.
	Header []Field
	Body   string
	// TrailerTitle labels the block Trailer draws, e.g. "Attachments".
	TrailerTitle string
	// Trailer renders anything that follows the body, such as the attachment
	// table. It is part of the document rather than a separate call, so a
	// handler still produces exactly one response.
	//
	// The UI it receives is silent: the block is already introduced by its title,
	// so a nested table's own footer would only repeat it.
	Trailer func(*UI) error
}

// DocumentSpec describes decrypted content meant to be read: a message, or a
// whole thread.
type DocumentSpec struct {
	// Header is the envelope shared by every part, used when a document has
	// more than one (a thread's subject and message count).
	Header []Field
	Parts  []Part
	// BodyOnly suppresses the envelope, the dividers, the per-part headers and
	// the trailers, leaving just the bodies. It is what a redirect into a file
	// wants.
	BodyOnly bool
	// Object replaces the rendering in machine formats.
	Object any
	// Skipped is what this document could not include; see IncompleteSpec. A
	// thread is a collection of bodies, and one of them that will not open is a
	// message missing from what is on the screen.
	Skipped IncompleteSpec
}

// dividerWidth is the length of the rule that separates messages in a thread.
// Narrow enough to read as a separator rather than as a table.
const dividerWidth = 56

// Document renders readable content: a header block, a blank line, the body,
// and whatever trails it. In a machine format it writes the object instead, so
// the body arrives as a field rather than as loose text.
func Document(u *UI, spec DocumentSpec) error {
	if u.Format.Machine() {
		return u.encode(spec.Object, spec.Skipped.Count)
	}

	if !spec.BodyOnly && len(spec.Header) > 0 {
		writeFields(u, spec.Header, "")
		_, _ = fmt.Fprintln(u.Out)
	}

	for i, p := range spec.Parts {
		if !spec.BodyOnly {
			if p.Divider != "" {
				_, _ = fmt.Fprintf(u.Out, "%s\n",
					u.style.Paint(Muted, strings.Repeat(GlyphRule, 3)+" "+p.Divider+" "+
						strings.Repeat(GlyphRule, dividerWidth)))
			}
			if len(p.Header) > 0 {
				writeFields(u, p.Header, "")
				_, _ = fmt.Fprintln(u.Out)
			}
		}

		_, _ = fmt.Fprintln(u.Out, p.Body)

		if !spec.BodyOnly && p.Trailer != nil {
			_, _ = fmt.Fprintln(u.Out)
			if p.TrailerTitle != "" {
				_, _ = fmt.Fprintln(u.Out, u.style.Paint(Muted, p.TrailerTitle))
			}
			if err := p.Trailer(u.silent()); err != nil {
				return err
			}
		}
		if i < len(spec.Parts)-1 {
			_, _ = fmt.Fprintln(u.Out)
		}
	}
	u.Incomplete(spec.Skipped)
	return nil
}
