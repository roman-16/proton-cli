package ui

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/ref"
)

// tryLabel is the fixed heading for remedies. Its width sets the indent of
// continuation lines, so a multi-line remedy stays visually attached to it.
const tryLabel = "Try:   "

// WriteError renders an error the way every error is rendered: one line stating
// the problem, then an indented "Try:" block when the error knows a remedy.
//
//	Error: That ID is a conversation, not a message.
//	Try:   proton mail conversations get 5bH2mQxK
//
// Wrapped context ("upload x: open y: no such file") is left intact - it is the
// chain of what was being attempted, which is exactly what a reader needs.
func WriteError(w io.Writer, err error, style Style, short bool) {
	if err == nil {
		return
	}
	_, _ = fmt.Fprintf(w, "%s %s\n", style.Paint(Danger, "Error:"), errs.Shown(err))

	hints := remedies(err, short)
	if len(hints) == 0 {
		return
	}
	indent := strings.Repeat(" ", len(tryLabel))
	for i, h := range hints {
		prefix := indent
		if i == 0 {
			prefix = style.Paint(Muted, tryLabel)
		}
		_, _ = fmt.Fprintf(w, "%s%s\n", prefix, h)
	}
}

func remedies(err error, short bool) []string {
	var ambiguous *errs.Ambiguous
	if errors.As(err, &ambiguous) {
		return candidates(ambiguous, short)
	}
	var hinter errs.Hinter
	if errors.As(err, &hinter) {
		return hinter.Hints()
	}
	return nil
}

// candidates lists what the reference matched, so the reader can name one of
// them instead.
//
// A disambiguation list has to disambiguate, so the IDs are shortened only while
// shortening keeps them distinct - which it cannot when the reference that
// matched them was itself a short ID.
func candidates(e *errs.Ambiguous, short bool) []string {
	ids := make([]string, len(e.Candidates))
	for i, c := range e.Candidates {
		ids[i] = c.ID
	}
	if short {
		if shortened, ok := stillDistinct(ids); ok {
			ids = shortened
		}
	}
	out := make([]string, 0, len(ids)+1)
	out = append(out, "narrow the term, or use one of:")
	for i, c := range e.Candidates {
		line := "  " + ids[i]
		if c.Label != "" {
			line += "  " + c.Label
		}
		out = append(out, line)
	}
	return out
}

func stillDistinct(ids []string) ([]string, bool) {
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, len(ids))
	for i, id := range ids {
		s := ref.Shorten(id)
		if _, repeated := seen[s]; repeated {
			return nil, false
		}
		seen[s] = struct{}{}
		out[i] = s
	}
	return out, true
}

// Fail writes an error to the UI's stderr. It is the single exit path for a
// failed command, so no handler formats an error itself.
func (u *UI) Fail(err error) { WriteError(u.Err, err, u.errStyle, u.ShortIDs()) }
