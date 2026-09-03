package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/proton"
	"strings"
)

func TestExitCode(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"nil is success", nil, 0},
		{"generic user error", errors.New("bad flag"), 1},
		{"unauthorized", proton.ErrUnauthorized, 2},
		{"not found", &errs.NotFound{Kind: "message"}, 3},
		{"ambiguous", &errs.Ambiguous{Kind: "message"}, 4},
		{"network failure", &proton.NetworkError{Err: errors.New("connection refused")}, 5},
		{"api 404", &proton.APIError{HTTPStatus: 404}, 3},
		{"api 500", &proton.APIError{HTTPStatus: 500}, 5},
		{"explicit exit wrap", errs.WithExit(4, errors.New("x")), 4},
	}
	for _, tc := range cases {
		if got := exitCode(tc.err); got != tc.want {
			t.Errorf("%s: exitCode = %d, want %d", tc.name, got, tc.want)
		}
	}
}

// Running out of time is a network failure, not the user changing their mind.
// The two used to share an exit code, which told anyone whose connection stalled
// that they had cancelled the command themselves.
func TestTimeoutIsNotCancellation(t *testing.T) {
	timedOut := &proton.NetworkError{Err: fmt.Errorf("awaiting headers: %w", context.DeadlineExceeded)}

	if errors.Is(timedOut, context.Canceled) {
		t.Error("a deadline must not read as a cancellation")
	}
	if got := exitCode(timedOut); got != 5 {
		t.Errorf("exitCode = %d, want 5 (network)", got)
	}
}

// A failure nobody phrased is this CLI's own, and says so with a code of its
// own. The distinction cannot be made at this level - cobra's complaints about
// a command line are just as bare and are nobody's bug - so it is made by the
// seam every command body passes through, and this checks both halves agree.
func TestAFailureNobodyPhrasedIsABug(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"a bare error out of a command body", errs.Bug(errors.New("failed to unlock keys")), errs.ExitBug},
		{"a phrased user error keeps its code", errs.Bug(errs.Problemf("--repeat takes no --start")), 1},
		{"a reference that matched nothing", errs.Bug(&errs.NotFound{Kind: "item"}), 3},
		{"an expired session", errs.Bug(proton.ErrUnauthorized), 2},
		{"a network failure", errs.Bug(&proton.NetworkError{Err: errors.New("refused")}), 5},
		{"cobra's own, which never reached a body", errors.New("unknown flag: --bogus"), 1},
	}
	for _, tc := range cases {
		got := exitCode(tc.err)
		if got != tc.want {
			t.Errorf("%s: exitCode = %d, want %d", tc.name, got, tc.want)
		}
		if ourFault(tc.err) != (tc.want == errs.ExitBug) {
			t.Errorf("%s: the invitation to report disagrees with exit %d", tc.name, got)
		}
	}
}

// A crash says one sentence and points at the report. The stack goes to the
// log, where it is of use to somebody; on the screen it is forty lines that
// tell the person who hit it nothing at all.
func TestACrashSaysOneSentenceAndNotAStack(t *testing.T) {
	var screen bytes.Buffer
	root := newRoot()
	code := crashed(&screen, root, "runtime error: index out of range [3] with length 2",
		[]byte("goroutine 1 [running]:\ngithub.com/roman-16/proton-cli/internal/service/drive...\n"))

	if code != errs.ExitBug {
		t.Errorf("a crash exited %d, want %d", code, errs.ExitBug)
	}
	got := screen.String()
	for _, want := range []string{"crashed", "This is a bug", "proton report"} {
		if !strings.Contains(got, want) {
			t.Errorf("the crash screen does not say %q:\n%s", want, got)
		}
	}
	for _, unwanted := range []string{"goroutine", "index out of range", "internal/service"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("the crash screen shows %q:\n%s", unwanted, got)
		}
	}
}

// The invitation appears for a bug and for nothing else. A line saying "this
// might be a bug" under every mistyped flag is a line nobody reads twice.
func TestOnlyABugIsWorthReporting(t *testing.T) {
	for _, c := range []struct {
		err     error
		invited bool
	}{
		{errs.Bug(errors.New("failed to unlock any address keys")), true},
		{errs.Problemf("Profile %q is not signed in.", "work").Exit(2), false},
		{&errs.NotFound{Kind: "message", Ref: "x"}, false},
		{errors.New("unknown flag: --bogus"), false},
	} {
		var screen bytes.Buffer
		invite(&screen, newRoot(), c.err)
		if invited := strings.Contains(screen.String(), "report"); invited != c.invited {
			t.Errorf("%v: invited=%v, want %v", c.err, invited, c.invited)
		}
	}
}
