// Package render owns all presentation logic: table/JSON/YAML output,
// progress bars, and the user-visible logger. Commands should never format
// output themselves.
package render

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/goccy/go-yaml"
	"golang.org/x/term"
)

// Format is the output mode selected via --output.
type Format string

const (
	FormatText Format = "text"
	FormatJSON Format = "json"
	FormatYAML Format = "yaml"
)

// ParseFormat returns the canonical Format for a user-supplied value.
func ParseFormat(s string) (Format, error) {
	switch s {
	case "", "text":
		return FormatText, nil
	case "json":
		return FormatJSON, nil
	case "yaml", "yml":
		return FormatYAML, nil
	}
	return "", fmt.Errorf("unknown output format %q (want text|json|yaml)", s)
}

// Renderer centralises all output formatting.
type Renderer struct {
	Format Format
	Stdout io.Writer
	Stderr io.Writer
	Log    *slog.Logger
	Quiet  bool
}

// New constructs a Renderer writing to the given streams.
func New(format Format, stdout, stderr io.Writer, level slog.Level, quiet bool) *Renderer {
	if stdout == nil {
		stdout = os.Stdout
	}
	if stderr == nil {
		stderr = os.Stderr
	}
	h := slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: level})
	return &Renderer{
		Format: format,
		Stdout: stdout,
		Stderr: stderr,
		Log:    slog.New(h),
		Quiet:  quiet,
	}
}

// Object marshals v to the configured output format when text mode is not
// applicable (e.g. a single object with no table representation).
//
// YAML output uses goccy/go-yaml, which falls back to `json:"..."` struct
// tags when no `yaml:"..."` tag is set, so field names match JSON output.
func (r *Renderer) Object(v any) error {
	switch r.Format {
	case FormatYAML:
		b, err := yaml.Marshal(v)
		if err != nil {
			return err
		}
		_, err = r.Stdout.Write(b)
		return err
	default: // FormatJSON and FormatText fallback
		enc := json.NewEncoder(r.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(v)
	}
}

// JSON writes raw pre-formatted JSON bytes to stdout (fallback for pass-through
// API responses). When format=yaml, it re-encodes via Object.
//
// For YAML we parse JSON numbers via json.Number so goccy/go-yaml preserves
// them as ints rather than float64 (which would render `1000` as `1000.0`).
func (r *Renderer) JSON(raw []byte) error {
	if r.Format == FormatYAML {
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		var v any
		if err := dec.Decode(&v); err != nil {
			_, _ = r.Stdout.Write(raw)
			return nil
		}
		b, err := yaml.Marshal(convertJSONNumbers(v))
		if err != nil {
			return err
		}
		_, err = r.Stdout.Write(b)
		return err
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		_, _ = r.Stdout.Write(raw)
		return nil
	}
	enc := json.NewEncoder(r.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// convertJSONNumbers walks v and converts json.Number to int64 when it fits,
// else float64. This keeps integer API fields rendering as ints in YAML.
func convertJSONNumbers(v any) any {
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
			x[k] = convertJSONNumbers(vv)
		}
		return x
	case []any:
		for i, vv := range x {
			x[i] = convertJSONNumbers(vv)
		}
		return x
	}
	return v
}

// TableOrObject prints a table when format=text, else marshals v.
func (r *Renderer) TableOrObject(headers []string, rows [][]string, v any) error {
	if r.Format == FormatText {
		Table(r.Stdout, headers, rows)
		return nil
	}
	return r.Object(v)
}

// ID prints just the ID on stdout (for scripts that want to capture it) and a
// success message on stderr. Use after a creating/mutating call.
func (r *Renderer) ID(id, msg string) {
	if id != "" {
		_, _ = fmt.Fprintln(r.Stdout, id)
	}
	if !r.Quiet && msg != "" {
		_, _ = fmt.Fprintln(r.Stderr, "✓ "+msg)
	}
}

// Success prints a success notice to stderr (no stdout).
func (r *Renderer) Success(msg string) {
	if r.Quiet || msg == "" {
		return
	}
	_, _ = fmt.Fprintln(r.Stderr, "✓ "+msg)
}

// Info prints a neutral progress notice to stderr.
func (r *Renderer) Info(msg string) {
	if r.Quiet || msg == "" {
		return
	}
	_, _ = fmt.Fprintln(r.Stderr, msg)
}

// IsTTY reports whether stderr is a terminal.
func (r *Renderer) IsTTY() bool {
	if f, ok := r.Stderr.(*os.File); ok {
		return term.IsTerminal(int(f.Fd()))
	}
	return false
}
