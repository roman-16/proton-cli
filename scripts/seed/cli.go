package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/roman-16/proton-cli/tests/fixture"
)

// passwordFiles is where each profile's account password was written, for the
// one thing Proton will not do on a session alone. A session cannot carry that
// elevation: the key blob sealed at login is a one-way derivation of the
// password rather than the password itself.
var passwordFiles = map[string]string{}

func writePasswordFiles(work string) error {
	for _, a := range accounts {
		file := filepath.Join(work, a.Profile+".password")
		if err := os.WriteFile(file, []byte(os.Getenv(a.Password)), 0o600); err != nil {
			return err
		}
		passwordFiles[a.Profile] = file
	}
	return nil
}

// binary is the proton to drive. `just` builds it first; PROTON_CLI points
// somewhere else when the integration suite runs its own build.
func binary() string {
	if v := os.Getenv("PROTON_CLI"); v != "" {
		return v
	}
	return "./proton"
}

// command builds a CLI invocation as one profile.
//
// Nothing the seed runs may stop to ask a question: it fills accounts
// unattended, and a prompt nobody is watching is a run that hangs until
// somebody notices. Signing in is the exception, and asks for the terminal
// itself - see attended.
func command(profile string, args ...string) *exec.Cmd {
	cmd := exec.Command(binary(), withPassword(profile, args)...)
	cmd.Env = append(os.Environ(), "PROTON_PROFILE="+profile, "PROTON_NO_INPUT=1")
	return cmd
}

// guarded are the commands Proton answers only for a session that has just
// proved the password again. Deleting a calendar is one, and the seed does it
// whenever a run left one behind.
var guarded = [][]string{
	{"calendar", "settings", "calendars", "delete"},
}

// withPassword hands such a command the profile's password file.
//
// It goes directly after the command's own words: the flag belongs to that
// command rather than to the root, and anything after a positional would be read
// as another one.
func withPassword(profile string, args []string) []string {
	file := passwordFiles[profile]
	if file == "" {
		return args
	}
	for _, cmd := range guarded {
		at := indexOfRun(args, cmd...)
		if at < 0 {
			continue
		}
		out := make([]string, 0, len(args)+2)
		out = append(out, args[:at+len(cmd)]...)
		out = append(out, "--password-file", file)
		return append(out, args[at+len(cmd):]...)
	}
	return args
}

// indexOfRun reports where args holds the words in order and adjacent, or -1.
func indexOfRun(args []string, run ...string) int {
	for i := 0; i+len(run) <= len(args); i++ {
		if slices.Equal(args[i:i+len(run)], run) {
			return i
		}
	}
	return -1
}

// run invokes the CLI as one profile and returns stdout. stderr is folded into
// the error, because that is where the CLI explains itself.
func run(profile string, args ...string) (string, error) {
	var out, errb bytes.Buffer
	cmd := command(profile, args...)
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return out.String(), fmt.Errorf("%s: %s", strings.Join(args, " "), firstLine(msg))
	}
	return out.String(), nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// seedRunner is how the fixture package runs the CLI as one account.
var seedRunner fixture.Runner = func(profile string, args ...string) (string, error) {
	return run(profile, args...)
}
