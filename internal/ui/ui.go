// Package ui owns every byte the CLI shows a human or a program. Commands
// describe what they produced - a collection, a record, a document, or the
// result of a mutation - and this package decides how it looks in text and how
// it is shaped in JSON or YAML. Nothing outside this package writes to the
// process streams.
//
// The package knows nothing about Proton's service: no IDs are resolved here, no
// caches are written, no API types are imported. That keeps it testable against
// golden files with no fixtures.
//
// It does know the notation a reference is written in, because it prints them -
// but it borrows that from internal/ref rather than restating it, so a reference
// this package renders is by construction one the cli layer will read back.
package ui

import (
	"bytes"
	"encoding"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"reflect"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/roman-16/proton-cli/internal/errs"
)

// Format is how the answer is serialised.
type Format string

const (
	FormatText Format = "text"
	FormatJSON Format = "json"
	FormatYAML Format = "yaml"
)

// ParseFormat maps the --output value to a Format.
func ParseFormat(s string) (Format, error) {
	switch s {
	case "", "text":
		return FormatText, nil
	case "json":
		return FormatJSON, nil
	case "yaml", "yml":
		return FormatYAML, nil
	}
	return "", fmt.Errorf("--output accepts: text, json, yaml")
}

// Machine reports whether the format is for a program rather than a person.
func (f Format) Machine() bool { return f != FormatText }

// LogLevels is the domain of --log-level, in increasing severity.
var LogLevels = []string{"debug", "info", "warn", "error"}

// ParseLogLevel resolves how noisy the logger should be, defaulting to warnings
// and worse.
//
// An unrecognised value is refused rather than quietly becoming the default. A
// mistyped --log-level used to produce silence, which looks exactly like the
// logging working and there being nothing to say.
func ParseLogLevel(value string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return slog.LevelWarn, nil
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	}
	return slog.LevelWarn, fmt.Errorf("--log-level accepts: %s", strings.Join(LogLevels, ", "))
}

// UI is the renderer for one invocation.
//
// Out carries the answer; Err carries everything about producing it. That split
// is absolute: a redirect of stdout must yield exactly the data and nothing
// else.
type UI struct {
	Format Format
	Out    io.Writer
	Err    io.Writer
	In     io.Reader
	// Log records what happened while the work was done, on the commentary
	// stream and in the run's file.
	Log *slog.Logger
	// Trace records the run's own envelope, in the file alone. See newLoggers.
	Trace *slog.Logger

	// Quiet suppresses confirmations, notes and progress on Err.
	Quiet bool
	// NoInput forbids prompting, so a missing credential is an error instead of
	// a question.
	NoInput bool
	// FullIDs keeps IDs unshortened even on a terminal.
	FullIDs bool
	// Width overrides the assumed terminal width for table layout. Zero measures
	// the output stream, falling back to a sane default when it is not a
	// terminal.
	Width int

	// style paints Out, errStyle paints Err. A machine format is never painted,
	// and a stream with no terminal behind it only when colour was asked for by
	// name, so piped bytes carry an escape sequence only where somebody said they
	// should.
	style    Style
	errStyle Style

	// What each stream is, asked once. Every renderer reads these rather than
	// asking a writer whether it is a terminal: Out and Err are wrapped while a
	// sign of life is on the screen, and a wrapper is not a file to ask.
	out, err measured
	in       bool

	// sp is the sign of life, shared by every clone of this UI so that a frame is
	// retired by whichever of them writes first. Nil when this run may not draw
	// one.
	sp *spinner
}

// Options configures a UI. Out, Err and In default to the process streams.
type Options struct {
	Format   Format
	Out      io.Writer
	Err      io.Writer
	In       io.Reader
	LogLevel slog.Level
	Quiet    bool
	Color    Color
	NoInput  bool
	FullIDs  bool
	Width    int

	// Log is the run's diagnostic file, which receives every record at full
	// detail whatever LogLevel says. Nil writes no file.
	Log io.Writer
	// Run names the invocation in every record that reaches the file, which is
	// what lets a day be read back as the runs that made it up.
	Run string
	// Salt keys the handles that stand in for addresses and IDs in both the file
	// and the commentary stream. Nil makes them stable for this process only.
	Salt []byte
}

func New(opts Options) *UI {
	out, errw, in := opts.Out, opts.Err, opts.In
	if out == nil {
		out = os.Stdout
	}
	if errw == nil {
		errw = os.Stderr
	}
	if in == nil {
		in = os.Stdin
	}
	// A machine format carries data, and an escape sequence is not data - so JSON
	// is plain however loudly colour was asked for.
	want := opts.Color
	if opts.Format.Machine() {
		want = ColorNever
	}
	outStream, errStream := measure(out), measure(errw)
	u := &UI{
		Format:   opts.Format,
		Out:      out,
		Err:      errw,
		In:       in,
		Quiet:    opts.Quiet,
		NoInput:  opts.NoInput,
		FullIDs:  opts.FullIDs,
		Width:    opts.Width,
		style:    styleFrom(outStream, want),
		errStyle: styleFrom(errStream, want),
		out:      outStream,
		err:      errStream,
		in:       reading(in),
	}
	// The sign of life draws on the raw stream and every other line goes through
	// a wrapper that takes it back, so nothing this run writes can land beside a
	// frame - whichever stream it was written to, and whichever clone wrote it.
	if u.animates() {
		u.sp = newSpinner(errw, u.errStyle)
		u.Out, u.Err = yielding{to: out, sp: u.sp}, yielding{to: errw, sp: u.sp}
	}
	u.Log, u.Trace = newLoggers(u.Err, u.errStyle, opts.LogLevel, opts.Salt, opts.Log, opts.Run)
	return u
}

// animates reports whether this run may draw something that erases itself
// again: a transfer bar, a sign of life.
//
// Drawing in place is an escape sequence like any other, so it needs a terminal
// that acts on one; and it is commentary, so it needs a run that has not been
// quietened and an answer meant for a person rather than a program.
func (u *UI) animates() bool {
	return !u.Quiet && !u.Format.Machine() && u.err.terminal && u.err.depth.escapes()
}

// Working draws a sign of life on the commentary stream while the run has
// nothing to show yet, and returns the way to take it back.
//
// A command that has printed nothing is indistinguishable from one that has
// hung, and the first request of an ordinary listing routinely outlasts the
// moment a reader starts to wonder. The frame is retired by the first byte the
// run writes to either stream, so it is never on the screen beside an answer, a
// question or a bar.
func (u *UI) Working() func() {
	if u.sp == nil {
		return func() {}
	}
	return u.sp.start()
}

// Style returns the styling for Out.
func (u *UI) Style() Style { return u.style }

// ErrStyle returns the styling for Err.
func (u *UI) ErrStyle() Style { return u.errStyle }

// IsTTY reports whether the answer is going to a terminal, which is what
// decides ID shortening: a pipe wants full IDs even when the user's stderr is
// still a terminal.
//
// PROTON_CLI_FORCE_TTY=1 overrides the check so tests can exercise interactive
// rendering without a pty. It deliberately does not enable colour.
func (u *UI) IsTTY() bool {
	if os.Getenv("PROTON_CLI_FORCE_TTY") == "1" {
		return true
	}
	return u.out.terminal
}

// ErrIsTTY reports whether somebody is watching the commentary stream, which is
// what separates a person at a keyboard from a scheduler collecting a log.
func (u *UI) ErrIsTTY() bool { return u.err.terminal }

// InIsTTY reports whether standard input is a terminal, so a read that is about
// to wait for typing can say so rather than look like a hang.
func (u *UI) InIsTTY() bool { return u.in }

// ShortIDs reports whether IDs should be shortened for display.
func (u *UI) ShortIDs() bool { return u.IsTTY() && !u.FullIDs }

// preview returns a UI whose answer stream is this one's commentary stream.
//
// A dry run explains rather than answers: it produces no data, so everything it
// draws - the table of what would be affected included - belongs on stderr,
// where it survives a redirect of stdout. The clone is quiet because the count
// has already been stated in the line above the table.
func (u *UI) preview() *UI {
	p := *u
	p.Out, p.out = u.Err, u.err
	p.style = u.errStyle
	p.Quiet = true
	return &p
}

// silent returns a clone that writes no commentary, for a renderer nested
// inside a larger response whose surroundings already say what it is.
func (u *UI) silent() *UI {
	s := *u
	s.Quiet = true
	return &s
}

// Note writes an incidental line to Err. Suppressed by --quiet.
func (u *UI) Note(msg string) {
	if u.Quiet || msg == "" {
		return
	}
	_, _ = fmt.Fprintln(u.Err, msg)
}

// Notef is Note with formatting.
func (u *UI) Notef(format string, a ...any) { u.Note(fmt.Sprintf(format, a...)) }

// Break sets a remark apart from whatever the command has already written, for
// the one line that is not about the work just done.
func (u *UI) Break() {
	if u.Quiet {
		return
	}
	_, _ = fmt.Fprintln(u.Err)
}

// Hint writes a dimmed incidental line to Err.
func (u *UI) Hint(msg string) {
	if u.Quiet || msg == "" {
		return
	}
	_, _ = fmt.Fprintln(u.Err, u.errStyle.Paint(Muted, msg))
}

// Instruct writes something the person has to do before the command can carry
// on: touch the key, follow the dialog that just opened.
//
// It is dimmed like a hint and, unlike one, survives --quiet. An instruction is
// part of the question, not commentary on it - and a run sitting there waiting
// for a finger, having said nothing about why, is indistinguishable from one
// that has hung.
func (u *UI) Instruct(msg string) {
	if msg == "" {
		return
	}
	_, _ = fmt.Fprintln(u.Err, u.errStyle.Paint(Muted, msg))
}

// Warn reports something that is true, is not a failure, and is worth noticing:
// a file that arrived but could not be attributed, a change that was saved but
// whose notification bounced, a filter about to cover more than the reader
// probably means.
//
// It is the third severity, and the CLI needs exactly three. Without it every
// caveat prints as flat commentary in the same colour as "Downloading…", which
// is how a warning about an unverifiable signature ends up sitting invisibly
// above a green tick.
func (u *UI) Warn(msg string) {
	if u.Quiet || msg == "" {
		return
	}
	writeCaution(u.Err, u.errStyle, Caution, msg)
}

// writeCaution draws the third severity wherever it is raised: a command's own
// caveat, or a record the run logged while the work was going on.
//
// Continuation lines are indented under the first, so a wrapped caveat stays
// visually attached to its mark.
func writeCaution(w io.Writer, style Style, role Role, msg string) {
	lines := strings.Split(msg, "\n")
	_, _ = fmt.Fprintf(w, "%s %s\n", style.Paint(role, GlyphCaution), lines[0])
	for _, cont := range lines[1:] {
		_, _ = fmt.Fprintf(w, "  %s\n", cont)
	}
}

// Warnf is Warn with formatting.
func (u *UI) Warnf(format string, a ...any) { u.Warn(fmt.Sprintf(format, a...)) }

// encode writes v to Out in the machine format, with what the answer could not
// include attached.
//
// There is one marshalling, and YAML is a rendering of its bytes rather than a
// second pass over the value. That is what makes "the two formats cannot
// disagree" true by construction instead of true as long as every struct is
// shaped in the way both encoders happen to read alike - a promise an embedded
// struct quietly breaks, since one of them flattens it and the other files it
// under its type name.
func (u *UI) encode(v any, skipped int) error {
	b, err := json.Marshal(spelledOut(v))
	if err != nil {
		return err
	}
	b = withSkipped(b, skipped)
	if u.Format == FormatYAML {
		y, err := yaml.JSONToYAML(b)
		if err != nil {
			return err
		}
		_, err = u.Out.Write(y)
		return err
	}
	var out bytes.Buffer
	if err := json.Indent(&out, b, "", "  "); err != nil {
		return err
	}
	out.WriteByte('\n')
	_, err = u.Out.Write(out.Bytes())
	return err
}

// withSkipped adds the count of what could not be read to an object that is
// about to be written.
//
// A record and a document carry it for the reason a listing's envelope does: a
// consumer that never sees the key read a whole answer, and one that does has
// been told the answer is short - which the warning on the commentary stream
// could never tell it. An answer that is not an object has nowhere to put it,
// and the only such answers are `api`'s, which are Proton's own shape.
func withSkipped(b []byte, skipped int) []byte {
	if skipped == 0 || len(b) < 2 || b[0] != '{' || b[len(b)-1] != '}' {
		return b
	}
	key := fmt.Sprintf(`"skipped":%d}`, skipped)
	if bytes.Equal(bytes.TrimSpace(b), []byte("{}")) {
		return []byte("{" + key)
	}
	return append(b[:len(b)-1:len(b)-1], []byte(","+key)...)
}

// spelledOut is v with every empty list and map written out as one.
//
// A machine format promises that a list is always a list, and a nil slice in Go
// marshals to null - which is not an empty answer to a consumer but an error, so
// `jq '.attendees[]'` stops rather than yielding nothing. Keeping the promise
// here, at the boundary that owns the machine format, is what makes it true of
// every view struct in the CLI instead of true of the ones somebody remembered
// to build an empty slice in.
func spelledOut(v any) any {
	if v == nil {
		return nil
	}
	return spellOut(reflect.ValueOf(v)).Interface()
}

var (
	jsonMarshaler = reflect.TypeOf((*json.Marshaler)(nil)).Elem()
	textMarshaler = reflect.TypeOf((*encoding.TextMarshaler)(nil)).Elem()
)

func spellOut(v reflect.Value) reflect.Value {
	// A type that writes itself is not a shape to walk into. time.Time is the one
	// that matters: it carries nothing exported, so rebuilding it field by field
	// would hand back the zero instant.
	if t := v.Type(); t.Implements(jsonMarshaler) || t.Implements(textMarshaler) {
		return v
	}
	switch v.Kind() {
	case reflect.Slice:
		// A byte slice is a string in JSON rather than something to iterate, so an
		// absent one stays absent instead of becoming an empty string.
		if v.Type().Elem().Kind() == reflect.Uint8 {
			return v
		}
		out := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		for i := range v.Len() {
			out.Index(i).Set(spellOut(v.Index(i)))
		}
		return out
	case reflect.Map:
		out := reflect.MakeMapWithSize(v.Type(), v.Len())
		iter := v.MapRange()
		for iter.Next() {
			out.SetMapIndex(iter.Key(), spellOut(iter.Value()))
		}
		return out
	case reflect.Pointer:
		if v.IsNil() {
			return v
		}
		out := reflect.New(v.Type().Elem())
		out.Elem().Set(spellOut(v.Elem()))
		return out
	case reflect.Interface:
		if v.IsNil() {
			return v
		}
		inner := spellOut(v.Elem())
		if !inner.Type().AssignableTo(v.Type()) {
			return v
		}
		out := reflect.New(v.Type()).Elem()
		out.Set(inner)
		return out
	case reflect.Struct:
		out := reflect.New(v.Type()).Elem()
		for i := range v.NumField() {
			if !out.Field(i).CanSet() {
				continue
			}
			out.Field(i).Set(spellOut(v.Field(i)))
		}
		return out
	}
	return v
}

// Raw writes pre-formatted JSON bytes through, re-rendering them for YAML. It
// exists for `proton api`, the one command whose contract is Proton's own shape.
//
// The bytes are reformatted rather than decoded and marshalled again, so the
// answer keeps the order Proton wrote its keys in and every number the width it
// was sent with. Decoding into a map would sort the keys and round the integers,
// neither of which is the shape the command promises.
//
// A body that is not JSON is not that shape, so it never reaches Out: a proxy's
// error page landing where jq is waiting is a broken pipeline reported as a
// success. It goes to Err, where it is still the thing a reader needs, and the
// call fails as the server trouble it is. A body with nothing in it is an empty
// answer rather than a broken one, which is what a HEAD returns.
func Raw(u *UI, raw []byte) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	if !json.Valid(raw) {
		_, _ = u.Err.Write(raw)
		if raw[len(raw)-1] != '\n' {
			_, _ = fmt.Fprintln(u.Err)
		}
		return errs.Problemf("The API returned a body that is not JSON.").Exit(5)
	}
	if u.Format == FormatYAML {
		y, err := yaml.JSONToYAML(raw)
		if err != nil {
			return err
		}
		_, err = u.Out.Write(y)
		return err
	}
	var out bytes.Buffer
	if err := json.Indent(&out, bytes.TrimSpace(raw), "", "  "); err != nil {
		return err
	}
	out.WriteByte('\n')
	_, err := u.Out.Write(out.Bytes())
	return err
}
