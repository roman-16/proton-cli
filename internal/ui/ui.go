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
	"golang.org/x/term"
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

	// style paints Out, errStyle paints Err. Both are disabled unless that
	// stream is a terminal and the format is text, so piped bytes never carry
	// escape sequences.
	style    Style
	errStyle Style
}

// Options configures a UI. Out, Err and In default to the process streams.
type Options struct {
	Format   Format
	Out      io.Writer
	Err      io.Writer
	In       io.Reader
	LogLevel slog.Level
	Quiet    bool
	NoColor  bool
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
	var style, errStyle Style
	if opts.Format == FormatText && !opts.NoColor {
		style, errStyle = StyleFor(out), StyleFor(errw)
	}
	log, trace := newLoggers(errw, opts.LogLevel, opts.Salt, opts.Log, opts.Run)
	return &UI{
		Log:      log,
		Trace:    trace,
		Format:   opts.Format,
		Out:      out,
		Err:      errw,
		In:       in,
		Quiet:    opts.Quiet,
		NoInput:  opts.NoInput,
		FullIDs:  opts.FullIDs,
		Width:    opts.Width,
		style:    style,
		errStyle: errStyle,
	}
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
	f, ok := u.Out.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

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
	p.Out = u.Err
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
//
// Continuation lines are indented under the first, so a wrapped caveat stays
// visually attached to its mark.
func (u *UI) Warn(msg string) {
	if u.Quiet || msg == "" {
		return
	}
	lines := strings.Split(msg, "\n")
	_, _ = fmt.Fprintf(u.Err, "%s %s\n", u.errStyle.Paint(Caution, GlyphCaution), lines[0])
	for _, cont := range lines[1:] {
		_, _ = fmt.Fprintf(u.Err, "  %s\n", cont)
	}
}

// Warnf is Warn with formatting.
func (u *UI) Warnf(format string, a ...any) { u.Warn(fmt.Sprintf(format, a...)) }

// encode writes v to Out in the machine format. It is the single marshalling
// path, so JSON and YAML can never disagree about field names: goccy/go-yaml
// falls back to `json:"..."` tags when no `yaml:"..."` tag is present.
func (u *UI) encode(v any) error {
	v = spelledOut(v)
	if u.Format == FormatYAML {
		b, err := yaml.Marshal(v)
		if err != nil {
			return err
		}
		_, err = u.Out.Write(b)
		return err
	}
	enc := json.NewEncoder(u.Out)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
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

// Raw writes pre-formatted JSON bytes through, re-encoding for YAML. It exists
// for `proton api`, the one command whose contract is Proton's own shape.
//
// Numbers are decoded via json.Number so YAML keeps integer fields integral
// rather than rendering 1000 as 1000.0.
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
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		_, _ = u.Err.Write(raw)
		if raw[len(raw)-1] != '\n' {
			_, _ = fmt.Fprintln(u.Err)
		}
		return errs.Problemf("The API returned a body that is not JSON.").Exit(5)
	}
	return u.encode(plainNumbers(v))
}

// plainNumbers converts json.Number to int64 where it fits, else float64, so
// both encoders emit numbers rather than quoted strings.
func plainNumbers(v any) any {
	switch x := v.(type) {
	case json.Number:
		if i, err := x.Int64(); err == nil {
			return i
		}
		if f, err := x.Float64(); err == nil {
			return f
		}
		return x.String()
	case map[string]any:
		for k, vv := range x {
			x[k] = plainNumbers(vv)
		}
		return x
	case []any:
		for i, vv := range x {
			x[i] = plainNumbers(vv)
		}
		return x
	}
	return v
}
