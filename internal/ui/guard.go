package ui

import (
	"bytes"
	"fmt"
	"io"
	"unicode/utf8"
)

// What a terminal is allowed to act on.
//
// A subject, a sender's name and a message body are written by whoever sent the
// mail, and a terminal cannot tell text this CLI meant to show from an
// instruction buried inside it. The instructions are the interesting part: move
// the cursor, erase the line, open a hyperlink, write the clipboard, reverse the
// reading order. Any of them lets a sender decide what a trusted tool appears to
// say.
//
// So a terminal is given the styling this CLI emits and nothing else, and
// everything it would otherwise act on is shown to the reader instead. A stream
// with no terminal behind it carries its bytes exactly: a redirect is data, and
// `--body-only > body.txt` has to be the body.
//
// The guard sits on the stream rather than on each value because the stream is
// one place and the values are every renderer there will ever be. The two things
// that draw in place - the sign of life and a transfer bar - write underneath it,
// which is what keeps this CLI's own cursor movement out of the allowance.

const escape = 0x1b

// guard is w with nothing on it a terminal acts on but this CLI's own styling.
// A stream that is not a terminal is itself.
func guard(w io.Writer, stream measured) io.Writer {
	if !stream.terminal {
		return w
	}
	return guarded{to: w}
}

type guarded struct{ to io.Writer }

func (g guarded) Write(p []byte) (int, error) {
	var out bytes.Buffer
	out.Grow(len(p))
	for i := 0; i < len(p); {
		if p[i] == escape {
			if n := styling(p[i:]); n > 0 {
				out.Write(p[i : i+n])
				i += n
				continue
			}
		}
		r, n := utf8.DecodeRune(p[i:])
		if shown := shown(r, p[i+n:]); shown != "" {
			out.WriteString(shown)
		} else {
			out.Write(p[i : i+n])
		}
		i += n
	}
	if _, err := g.to.Write(out.Bytes()); err != nil {
		return 0, err
	}
	return len(p), nil
}

// styling is the length of the style sequence at the start of p, and 0 for
// anything else.
//
// Select Graphic Rendition is the whole of what this CLI asks a terminal for: a
// colour, a weight, and the reset that ends them. Every other sequence a control
// introducer can open moves the cursor, erases, reports or redefines, and none of
// those is styling under any name.
func styling(p []byte) int {
	if len(p) < 3 || p[1] != '[' {
		return 0
	}
	for i := 2; i < len(p); i++ {
		switch {
		case p[i] == 'm':
			return i + 1
		case p[i] >= '0' && p[i] <= '9', p[i] == ';':
		default:
			return 0
		}
	}
	return 0
}

// shown is what a rune is written as when a terminal would act on it, and "" for
// one it may have as it stands.
//
// A carriage return is the exception worth making: on its own it returns to the
// start of the line, which is how a line is overwritten, but in front of a
// newline it is only how most of the world ends one - and a message body that
// arrived over SMTP ends every line that way.
func shown(r rune, rest []byte) string {
	switch {
	case r == '\n', r == '\t':
		return ""
	case r == '\r':
		if len(rest) > 0 && rest[0] == '\n' {
			return ""
		}
		return "^M"
	case r < 0x20:
		return "^" + string('@'+r)
	case r == 0x7f:
		return "^?"
	case r >= 0x80 && r <= 0x9f, // C1, which introduces the same sequences as ESC
		r >= 0x202a && r <= 0x202e, // bidirectional embedding and override
		r >= 0x2066 && r <= 0x2069: // bidirectional isolate
		return fmt.Sprintf("\\u%04x", r)
	}
	return ""
}
