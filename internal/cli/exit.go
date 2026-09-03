package cli

import (
	"errors"

	"github.com/roman-16/proton-cli/internal/errs"
)

// exitCode classifies an error into the CLI's exit-code scheme:
//
//	0 success · 1 user error · 2 auth · 3 not-found · 4 conflict/ambiguous ·
//	5 network/server · 6 refused by the confirmation policy · 7 a bug.
//
// A refusal has a code of its own because a caller has to be able to tell it
// from a mistake: nothing about the command was wrong, and repeating it with
// different arguments will not help. A bug has one for the same reason from the
// other side: nothing about the command was wrong and nothing the caller does
// will help, so retrying is pointless and reporting it is not.
//
// Every error that knows what it is implements errs.ExitCoder and carries its
// own code - NotFound, Ambiguous, WrongTable, an explicit Exit wrap, an
// APIError, an expired session. What is left is an error nobody classified, and
// that is a user error only because cobra's complaints about a command line
// arrive that way. The ones from a command body are tagged as they pass through
// kit.Run, which is what makes the seventh code reachable - see errs.ExitBug.
func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var coder errs.ExitCoder
	if errors.As(err, &coder) {
		return coder.ExitCode()
	}
	return 1
}

// ourFault reports whether a failure is the CLI's rather than the caller's,
// which is what decides whether the screen asks them to report it.
//
// It is the same question exitCode answers, asked once so that the code and the
// invitation can never disagree - an error that prints "this is a bug" and exits
// 1 would be worse than either half alone.
func ourFault(err error) bool { return exitCode(err) == errs.ExitBug }
