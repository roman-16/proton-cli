package offline

import (
	"strings"
	"testing"
)

// red is what a refusal opens with when it is painted at all.
const red = "\x1b[31m"

// Everything here runs down a pipe, which is what makes the pipe testable: a
// buffer collecting output is exactly the case colour must stay out of, and the
// case somebody overrides when what reads the output renders escapes itself - a
// pager, a CI log.
func TestColourThroughAPipe(t *testing.T) {
	// Two failures, reported from either side of the line where this program
	// knows what it was asked to do. A word naming no subcommand is answered
	// before the flags have parsed and so before there are settings; being signed
	// out is answered by the command itself.
	for _, tc := range []struct {
		what string
		args []string
	}{
		{what: "a word naming no subcommand", args: []string{"mail", "mesages"}},
		{what: "an unknown flag", args: []string{"mail", "messages", "list", "--bogus"}},
		{what: "no account to act as", args: []string{"mail", "messages", "list"}},
	} {
		t.Run(tc.what, func(t *testing.T) {
			for _, env := range []struct {
				what    string
				vars    map[string]string
				painted bool
			}{
				{what: "nothing said", vars: nil},
				{what: "NO_COLOR", vars: map[string]string{"NO_COLOR": "1"}},
				{what: "FORCE_COLOR", vars: map[string]string{"FORCE_COLOR": "1"}, painted: true},
				{
					what:    "FORCE_COLOR beside NO_COLOR",
					vars:    map[string]string{"FORCE_COLOR": "1", "NO_COLOR": "1"},
					painted: true,
				},
			} {
				_, stderr, _ := runWithEnv(t, env.vars, tc.args...)
				if painted := strings.Contains(stderr, red); painted != env.painted {
					t.Errorf("%s, painted=%v, want %v: %q", env.what, painted, env.painted, truncate(stderr))
				}
			}
		})
	}
}

// The flag says the same as the variable, for a shell that cannot put one in
// front of a single command.
func TestForceColorIsAlsoAFlag(t *testing.T) {
	_, stderr, _ := run(t, "--force-color", "mail", "messages", "list")
	if !strings.Contains(stderr, red) {
		t.Errorf("--force-color did not paint the refusal: %q", truncate(stderr))
	}
}

// Painting and not painting in the same command line is a sentence that
// contradicts itself, and resolving it either way would pass over half of what
// was typed in silence.
func TestPaintingAndNotPaintingIsRefused(t *testing.T) {
	refuses(t, 1, []string{"--force-color", "--no-color", "mail", "messages", "list"},
		"--no-color", "--force-color")
}
