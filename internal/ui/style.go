package ui

import (
	"encoding/hex"
	"io"
	"os"
	"strconv"
	"strings"

	"golang.org/x/term"
)

// Color is what the person running the command asked for, which is a different
// question from what their terminal can do with an escape sequence. This half is
// settled by flags, variables and the settings file in package config; the other
// half is asked of the destination here.
type Color uint8

const (
	// ColorAuto paints a terminal that can render an escape, and nothing else.
	ColorAuto Color = iota
	// ColorAlways paints whatever the answer is going to, a pipe included, which
	// is what carries colour into a pager or a log something else renders later.
	ColorAlways
	// ColorNever paints nothing.
	ColorNever
)

// depth is what the terminal behind a stream does with an escape sequence, which
// is the whole of what this package needs to know about it: whether one is acted
// on at all, and whether an exact colour survives.
type depth uint8

const (
	// depthNone prints an escape instead of acting on it.
	depthNone depth = iota
	// depthNamed resolves ECMA-48's colour names and the 256-colour cube.
	depthNamed
	// depthDirect takes 24-bit colour.
	depthDirect
)

// escapes reports whether a sequence written to such a terminal is acted on.
//
// Colour is not the only thing that asks. A transfer bar erases its line with an
// escape, and a terminal that acts on none cannot take back what it has drawn.
func (d depth) escapes() bool { return d != depthNone }

// Role is what a piece of output means, and the only vocabulary the CLI has for
// colour.
//
// A caller names a meaning and never a colour. That is not a convenience: it is
// what lets the reader's terminal decide how the meaning looks. ECMA-48 defines
// its eight colours by name alone - black, red, green, yellow, blue, magenta,
// cyan, white - and deliberately fixes no RGB value for any of them, so
// "magenta" is whatever purple the reader has configured. Naming a colour is
// how a program asks for the reader's palette; spelling one in hex is how it
// overrules them.
type Role uint8

const (
	// Plain is ordinary data, which is nearly all of it: the terminal's own
	// foreground, untouched.
	Plain Role = iota
	// Muted is context rather than content - headers, rules, field labels,
	// footers, the count beside a state.
	Muted
	// Accent is Proton's own mark: an ID, an unread message, a transfer in
	// flight.
	Accent
	// Success is a verdict in the reader's favour: a mutation that took, a
	// signature that verified.
	Success
	// Caution is true and worth noticing: a caveat, a star, a signature nobody
	// could check.
	Caution
	// Danger is a verdict against: an error, a signature that is wrong.
	Danger
)

// roles maps each meaning onto an ECMA-48 name.
//
// The choice of name comes from Proton - green where Proton signals success,
// magenta for the purple it brands with - and the shade comes from the reader.
// Both halves matter: a mapping computed from Proton's hexes at run time,
// against the palette the terminal actually holds, would paint "succeeded" cyan
// on the several popular themes whose cyan sits nearest Proton's teal, and the
// meaning would drift from terminal to terminal. The names are a semantic
// vocabulary, so they are assigned once, here, by hand.
//
// Muted is an intensity rather than a colour, because no colour can do the job:
// the slot themes usually put their grey in is also the slot several of them
// reserve for a background, and text painted in it disappears. Faint dims
// whatever the reader can already read, against whatever they read it on, and a
// terminal that does not implement it renders undimmed rather than invisible.
//
// The bright names and the four extremes - black, white and their bright twins
// - are deliberately unused: those are the ones a theme is free to collide with
// its own background.
var roles = [...]struct{ set, clear string }{
	Plain:   {"", ""},
	Muted:   {"\x1b[2m", "\x1b[22m"},  // faint       - text-hint, border-norm
	Accent:  {"\x1b[35m", "\x1b[39m"}, // magenta     - primary
	Success: {"\x1b[32m", "\x1b[39m"}, // green       - signal-success
	Caution: {"\x1b[33m", "\x1b[39m"}, // yellow      - signal-warning
	Danger:  {"\x1b[31m", "\x1b[39m"}, // red         - signal-danger
}

// Style paints one stream. It is deliberately narrow: colour is a courtesy for
// a human at a terminal, never part of the data. The zero Style is disabled and
// every method returns its input unchanged, so the bytes a pipe receives are
// identical either way.
type Style struct {
	enabled bool
	// direct is true for 24-bit terminals. Only a swatch needs it: every role is
	// a name, which any terminal that has colour at all can resolve.
	direct bool
}

// IsTerminal reports whether w is a real terminal, which is what separates
// someone reading the output from a file, a pipe or a scheduler collecting it.
func IsTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

// StyleFor returns the styling to use when writing to w.
//
// Auto asks the destination and paints only a terminal that acts on an escape,
// so a redirect or a pipe receives the same bytes without them.
// PROTON_CLI_FORCE_TTY deliberately does not apply, which keeps captured output
// plain unless colour was asked for by name.
func StyleFor(w io.Writer, want Color) Style {
	if want == ColorNever {
		return Style{}
	}
	d := depthNone
	if f, ok := w.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		d = terminalDepth(f)
	}
	if want == ColorAlways && d == depthNone {
		d = advertised()
	}
	if !d.escapes() {
		return Style{}
	}
	return Style{enabled: true, direct: d == depthDirect}
}

// advertised is the depth the environment claims, for a destination there is no
// terminal to ask.
//
// COLORTERM is the convention terminals actually advertise with; a TERM ending
// in -direct is terminfo's own spelling of the same capability. Neither says
// anything about names, which anything with colour at all resolves.
func advertised() depth {
	switch os.Getenv("COLORTERM") {
	case "truecolor", "24bit":
		return depthDirect
	}
	if strings.HasSuffix(os.Getenv("TERM"), "-direct") {
		return depthDirect
	}
	return depthNamed
}

// Enabled reports whether this styling emits escape sequences.
func (s Style) Enabled() bool { return s.enabled }

// Paint styles text as the given meaning.
func (s Style) Paint(r Role, text string) string {
	if !s.enabled || text == "" || int(r) >= len(roles) {
		return text
	}
	seq := roles[r]
	if seq.set == "" {
		return text
	}
	return seq.set + text + seq.clear
}

// Swatch draws glyph in a colour Proton stores rather than one this package
// owns: the hex a label, folder, calendar or contact group is kept as.
//
// It is the one place the CLI names an exact colour, and the reason is that the
// colour is the value. A swatch redrawn in the reader's palette would not be
// respecting their theme, it would be misreporting a field they set themselves.
// An unparseable hex is returned unpainted.
func (s Style) Swatch(hex, glyph string) string {
	c, ok := parseHex(hex)
	if !s.enabled || !ok || glyph == "" {
		return glyph
	}
	if s.direct {
		return "\x1b[38;2;" + strconv.Itoa(int(c.r)) + ";" +
			strconv.Itoa(int(c.g)) + ";" + strconv.Itoa(int(c.b)) + "m" + glyph + "\x1b[39m"
	}
	return "\x1b[38;5;" + strconv.Itoa(int(c.x256())) + "m" + glyph + "\x1b[39m"
}

func (s Style) paintMarks(m Marks) string {
	var b strings.Builder
	for _, mk := range m {
		b.WriteString(s.Paint(mk.Role, mk.Glyph))
	}
	return b.String()
}

type rgb struct{ r, g, b uint8 }

// parseHex reads the "#RRGGBB" Proton stores a label, folder, calendar or group
// colour as. Anything else reports false, so an unrecognised value is printed
// plainly rather than painted from nonsense.
func parseHex(s string) (rgb, bool) {
	b, err := hex.DecodeString(strings.TrimPrefix(strings.TrimSpace(s), "#"))
	if err != nil || len(b) != 3 {
		return rgb{}, false
	}
	return rgb{b[0], b[1], b[2]}, true
}

// x256 is the nearest index in the xterm-256 palette, computed from the 6×6×6
// colour cube and the 24-step grey ramp. The first sixteen entries are left out
// on purpose, for the same reason every role is one of them: a terminal is free
// to redefine them, so a swatch matched against them would come out as whatever
// the reader's theme says rather than as the colour Proton stores.
func (c rgb) x256() uint8 {
	best, bestDist := uint8(0), 1<<31-1
	consider := func(idx uint8, r, g, b int) {
		d := sq(int(c.r)-r) + sq(int(c.g)-g) + sq(int(c.b)-b)
		if d < bestDist {
			best, bestDist = idx, d
		}
	}
	levels := []int{0, 95, 135, 175, 215, 255}
	for r := 0; r < 6; r++ {
		for g := 0; g < 6; g++ {
			for b := 0; b < 6; b++ {
				consider(uint8(16+36*r+6*g+b), levels[r], levels[g], levels[b])
			}
		}
	}
	for i := 0; i < 24; i++ {
		v := 8 + 10*i
		consider(uint8(232+i), v, v, v)
	}
	return best
}

func sq(n int) int { return n * n }
