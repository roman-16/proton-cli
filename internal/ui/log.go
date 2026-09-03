package ui

import (
	"context"
	"io"
	"log/slog"
	"strconv"

	"github.com/roman-16/proton-cli/internal/redact"
)

// undeclared is what stands in for an attribute nobody declared a policy for.
//
// A log line is written once and read by somebody who was not there, so the
// worst answer to an unknown attribute is to write it and hope. Refusing it
// leaves the name visible - which is what makes the omission obvious to whoever
// reads the log, and to the conformance test that fails the build over it -
// while the value stays where it was.
const undeclared = "<undeclared>"

// redacting is a handler that applies the declared policy to every attribute on
// its way through, and hands the result to another handler to format.
//
// It wraps rather than formats because the two destinations want different
// shapes - a person reading stderr wants the text form, a report wants JSON -
// and neither may see anything the other would not. Putting the redaction above
// both is what makes that true by construction instead of twice.
type redacting struct {
	inner    slog.Handler
	redactor *redact.Redactor
}

func (h redacting) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h redacting) Handle(ctx context.Context, r slog.Record) error {
	clean := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	r.Attrs(func(a slog.Attr) bool {
		if attr, ok := h.attr(a); ok {
			clean.AddAttrs(attr)
		}
		return true
	})
	return h.inner.Handle(ctx, clean)
}

// attr applies one attribute's policy.
//
// A group is walked rather than refused, so that a caller reaching for
// slog.Group is held to the same vocabulary as one passing a flat pair.
func (h redacting) attr(a slog.Attr) (slog.Attr, bool) {
	if a.Value.Kind() == slog.KindGroup {
		group := a.Value.Group()
		kept := make([]slog.Attr, 0, len(group))
		for _, inner := range group {
			if clean, ok := h.attr(inner); ok {
				kept = append(kept, clean)
			}
		}
		if len(kept) == 0 {
			return slog.Attr{}, false
		}
		return slog.Attr{Key: a.Key, Value: slog.GroupValue(kept...)}, true
	}
	if !redact.Declared(a.Key) {
		return slog.String(a.Key, undeclared), true
	}
	// A number, a boolean or a duration describes the machinery, and the policy
	// for such an attribute is Keep by declaration. Rendering it to a string to
	// pass it through the redactor and back would turn a JSON number into a
	// quoted one for nothing.
	switch a.Value.Kind() {
	case slog.KindBool, slog.KindDuration, slog.KindFloat64, slog.KindInt64, slog.KindTime, slog.KindUint64:
		if redact.Fields[a.Key] == redact.Keep {
			return a, true
		}
	}
	value, keep := h.redactor.Apply(a.Key, stringify(a.Value))
	if !keep {
		return slog.Attr{}, false
	}
	return slog.String(a.Key, value), true
}

// stringify renders a value for redaction. Anything that is not already text is
// asked for its own string form, which is what an error, a stringer and a
// number all answer to.
func stringify(v slog.Value) string {
	switch v.Kind() {
	case slog.KindString:
		return v.String()
	case slog.KindInt64:
		return strconv.FormatInt(v.Int64(), 10)
	case slog.KindAny:
		if err, ok := v.Any().(error); ok {
			if err == nil {
				return ""
			}
			return err.Error()
		}
	}
	return v.String()
}

func (h redacting) WithAttrs(attrs []slog.Attr) slog.Handler {
	kept := make([]slog.Attr, 0, len(attrs))
	for _, a := range attrs {
		if clean, ok := h.attr(a); ok {
			kept = append(kept, clean)
		}
	}
	return redacting{inner: h.inner.WithAttrs(kept), redactor: h.redactor}
}

func (h redacting) WithGroup(name string) slog.Handler {
	return redacting{inner: h.inner.WithGroup(name), redactor: h.redactor}
}

// fanout sends every record to each of several handlers, each with its own idea
// of what is worth reporting.
//
// The two levels are the whole point. What reaches stderr is a preference - most
// people want warnings and worse - and what reaches the file is not: a run that
// only recorded what was already on the screen would be no more use after the
// fact than the screen was.
type fanout []slog.Handler

func (f fanout) Enabled(ctx context.Context, level slog.Level) bool {
	for _, h := range f {
		if h.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (f fanout) Handle(ctx context.Context, r slog.Record) error {
	var err error
	for _, h := range f {
		if !h.Enabled(ctx, r.Level) {
			continue
		}
		if e := h.Handle(ctx, r.Clone()); e != nil && err == nil {
			err = e
		}
	}
	return err
}

func (f fanout) WithAttrs(attrs []slog.Attr) slog.Handler {
	out := make(fanout, len(f))
	for i, h := range f {
		out[i] = h.WithAttrs(attrs)
	}
	return out
}

func (f fanout) WithGroup(name string) slog.Handler {
	out := make(fanout, len(f))
	for i, h := range f {
		out[i] = h.WithGroup(name)
	}
	return out
}

// newLoggers builds the two an invocation has, behind one redactor so that a
// handle means the same thing in each.
//
// Log is for what happens while the work is being done - a retry, a signature
// that would not verify, an item that would not open - and goes to both: the
// screen at whatever verbosity was asked for, the file always and in full.
//
// Trace is for the run's own envelope: what was invoked, how it came out, the
// stack if it crashed. That goes to the file alone, because the screen has its
// own way of saying all three and saying them twice is how a failure comes to
// be reported to a person as two different-looking problems.
func newLoggers(errw io.Writer, level slog.Level, salt []byte, file io.Writer) (log, trace *slog.Logger) {
	redactor := redact.New(salt)
	screen := redacting{
		redactor: redactor,
		inner:    slog.NewTextHandler(errw, &slog.HandlerOptions{Level: level}),
	}
	if file == nil {
		return slog.New(screen), slog.New(slog.DiscardHandler)
	}
	recorded := redacting{
		redactor: redactor,
		inner:    slog.NewJSONHandler(file, &slog.HandlerOptions{Level: slog.LevelDebug}),
	}
	return slog.New(fanout{screen, recorded}), slog.New(recorded)
}
