// Package errs defines the cross-cutting error types that drive the CLI's exit
// codes and its user-facing wording. Domain code returns these instead of
// formatting strings; the ui layer turns them into bytes, and the cli layer
// classifies them via errors.As rather than by matching message text.
//
// Every message here is a sentence: it starts with a capital and ends with a
// full stop, because it is printed to a person as-is. Remedies are separate,
// carried by Hinter, so the problem and the fix never run together in one line.
package errs

import (
	"errors"
	"fmt"
	"strings"
)

// ExitCoder is implemented by errors that carry a specific process exit code.
//
//	1 user error · 2 auth · 3 not found · 4 conflict or ambiguity · 5 network
//	6 refused by policy · 7 a bug · 8 not supported yet
type ExitCoder interface {
	ExitCode() int
}

// ExitBug is what a failure nobody phrased exits with.
//
// Every failure a user can cause is given words and a code by whatever raised
// it. A failure that comes out of a command's own work still wearing whatever Go
// wrote is, by that fact alone, one this CLI did not anticipate - so it is not
// the caller's mistake, and reporting it as one costs somebody an afternoon
// looking for the argument they got wrong.
//
// It is applied by the seam every command body passes through rather than by
// the classifier at the top, because the distinction is about where an error
// came from and only that seam knows: everything cobra raises about a command
// line happens before a body runs, and is a user error however bare it looks.
const ExitBug = 7

// Bug marks a failure as this CLI's own, unless something has already said what
// it is. Returns nil for nil.
func Bug(err error) error {
	if err == nil {
		return nil
	}
	var coder ExitCoder
	if errors.As(err, &coder) {
		return err
	}
	return &Exit{Code: ExitBug, Err: err}
}

// ExitUnsupported is what a Proton feature this build does not implement exits
// with.
//
// Nothing about the command was wrong and there is nothing to report: what it
// reached for is a gap somebody has already written down, so neither a retry nor
// different arguments changes the answer. That is what separates it from the
// mistake exit 1 describes and the bug exit 7 asks to be told about.
const ExitUnsupported = 8

// Unsupportedf starts a refusal for something this build does not implement
// (exit 8). Remedies are added with Hint, as with any other problem.
func Unsupportedf(format string, a ...any) *Problem {
	return &Problem{Msg: Sentence(fmt.Sprintf(format, a...)), Code: ExitUnsupported}
}

// Hinter is implemented by errors that know how the user might fix them. The ui
// layer renders the lines under a "Try:" heading.
type Hinter interface {
	Hints() []string
}

// Problem is a plain user-facing error: what went wrong, and optionally what to
// do about it.
type Problem struct {
	Msg  string
	Try  []string
	Code int
}

// Problemf starts a user error (exit 1). Add remedies with Hint and change the
// exit code with Exit.
//
//	return errs.Problemf("--format accepts: text, html, raw")
//	return errs.Problemf("Nothing selected.").Hint("pass a REF, or a filter")
func Problemf(format string, a ...any) *Problem {
	return &Problem{Msg: Sentence(fmt.Sprintf(format, a...)), Code: 1}
}

// Hint appends remedy lines.
func (p *Problem) Hint(lines ...string) *Problem {
	p.Try = append(p.Try, lines...)
	return p
}

// Exit overrides the process exit code.
func (p *Problem) Exit(code int) *Problem {
	p.Code = code
	return p
}

func (p *Problem) Error() string { return p.Msg }
func (p *Problem) Hints() []string {
	return p.Try
}
func (p *Problem) ExitCode() int {
	if p.Code == 0 {
		return 1
	}
	return p.Code
}

// NotFound means a reference matched nothing. Exit 3.
type NotFound struct {
	// Kind is the singular noun for what was looked for: "message", "contact".
	Kind string
	Ref  string
	// Try holds what to do about it. It is how a search that was itself
	// incomplete admits as much: something that could not be decrypted was also
	// not searched, and "no item matching that" is the wrong answer if the item
	// was there and unreadable.
	Try []string
}

func (e *NotFound) Error() string {
	if e.Ref == "" {
		return fmt.Sprintf("No %s found.", e.Kind)
	}
	return fmt.Sprintf("No %s matching %q.", e.Kind, e.Ref)
}
func (e *NotFound) ExitCode() int   { return 3 }
func (e *NotFound) Hints() []string { return e.Try }

// Exists means the name is already taken where it was going to be written. Exit
// 4, the same code as an ambiguous reference, because both are the answer that
// what was named matches something already there rather than being wrong.
type Exists struct {
	// Kind is the singular noun for what is in the way: "file", "folder".
	Kind string
	Name string
	// Where is the container that already holds it.
	Where string
	// Answers are the ways the command offers to go ahead anyway, which only the
	// command knows: they are its own flags.
	Answers []string
}

func (e *Exists) Hints() []string { return e.Answers }

func (e *Exists) Error() string {
	return fmt.Sprintf("%s already has a %s called %q.", e.Where, e.Kind, e.Name)
}
func (e *Exists) ExitCode() int { return 4 }

// Candidate is one entry in an Ambiguous error's disambiguation list.
type Candidate struct {
	ID string
	// Label is a human-readable hint - a sender, a subject, an email address -
	// so the user can tell the candidates apart without looking them up.
	Label string
}

// Ambiguous means a reference matched more than one thing. Exit 4.
//
// The candidates stay data rather than becoming lines, because how much of an ID
// to show is a question about the screen: internal/ui shortens them where
// shortening still tells them apart, and prints them whole where it would not.
type Ambiguous struct {
	Kind       string
	Ref        string
	Candidates []Candidate
}

func (e *Ambiguous) Error() string {
	return fmt.Sprintf("%q matches %d %s.", e.Ref, len(e.Candidates), plural(e.Kind, len(e.Candidates)))
}

func (e *Ambiguous) ExitCode() int { return 4 }

// Exit wraps an error with an explicit process exit code, for cases where the
// cause is already well phrased.
type Exit struct {
	Code int
	Err  error
}

func (e *Exit) Error() string { return e.Err.Error() }
func (e *Exit) Unwrap() error { return e.Err }
func (e *Exit) ExitCode() int { return e.Code }

// WithExit wraps err so it exits with the given code. Returns nil for nil err.
func WithExit(code int, err error) error {
	if err == nil {
		return nil
	}
	return &Exit{Code: code, Err: err}
}

// Sentence normalises a message for direct display: a capital first letter and
// one trailing full stop. Text already ending in punctuation keeps it, so a
// message closing with "?" or ":" is left alone.
//
// A message opening with an identifier is not capitalised. "--format accepts: …"
// and "week-start accepts: …" both name something the user typed, and changing
// its case would misquote it.
func Sentence(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	r := []rune(s)
	if r[0] >= 'a' && r[0] <= 'z' && !opensWithIdentifier(s) {
		r[0] -= 'a' - 'A'
	}
	s = string(r)
	switch s[len(s)-1] {
	case '.', '!', '?', ':':
		return s
	}
	return s + "."
}

// opensWithIdentifier reports whether the first word looks like something the
// user typed rather than the first word of a sentence: a flag, a setting key, a
// path, anything carrying a character prose does not use inside a word.
func opensWithIdentifier(s string) bool {
	if strings.HasPrefix(s, "-") {
		return true
	}
	first, _, _ := strings.Cut(s, " ")
	return strings.ContainsAny(first, "-_/=.@0123456789")
}

func plural(noun string, n int) string {
	if n == 1 {
		return noun
	}
	return noun + "s"
}
