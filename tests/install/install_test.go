// Package install holds what the install script answers before it reaches the
// network.
//
// It is delivered through a pipe, which is the one way it cannot read itself
// back off disk, and running it locally never reproduces that: `sh install.sh`
// and `curl … | sh -s --` disagree about what `$0` is, so a script that consults
// its own source is a different program in the flow its own help documents. That
// is checked here by running it both ways and comparing.
//
// What the awk that once printed the help bought - a help text that cannot drift
// out of step - is bought here instead, and wider: the options are read out of
// the parser and matched against the ones the help names, so a flag added to one
// and not the other fails.
package install

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"testing"
)

const script = "../../scripts/install.sh"

// delivery is how the script reaches the shell: the way the README tells people
// to run it, and the way somebody who downloaded it first would.
type delivery int

const (
	piped delivery = iota
	fromFile
)

func run(t *testing.T, how delivery, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Skipf("no sh to run the installer with: %v", err)
	}

	var cmd *exec.Cmd
	switch how {
	case piped:
		cmd = exec.Command(shell, append([]string{"-s", "--"}, args...)...)
		source, err := os.Open(script)
		if err != nil {
			t.Fatalf("could not open %s: %v", script, err)
		}
		defer func() { _ = source.Close() }()
		cmd.Stdin = source
	case fromFile:
		cmd = exec.Command(shell, append([]string{script}, args...)...)
	}

	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			t.Fatalf("could not run the installer: %v", err)
		}
	}
	return out.String(), errOut.String(), cmd.ProcessState.ExitCode()
}

// The help is asked for in the flow the script's own header documents, which is
// the one where there is no file to fall back on.
func TestHelpSurvivesThePipeItArrivesThrough(t *testing.T) {
	stdout, stderr, code := run(t, piped, "--help")
	if code != 0 {
		t.Errorf("exit %d, want 0\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "proton-cli installer.") {
		t.Errorf("help did not describe the installer\nstdout: %s", stdout)
	}
	// Help that was asked for is the answer, not a note about the run: every
	// other line this script writes goes to stderr, and this one does not.
	if stderr != "" {
		t.Errorf("help wrote to stderr: %s", stderr)
	}
}

func TestHelpIsTheSameTextHoweverTheScriptArrived(t *testing.T) {
	overPipe, _, _ := run(t, piped, "--help")
	fromDisk, _, _ := run(t, fromFile, "--help")
	if overPipe != fromDisk {
		t.Errorf("the help depends on how the script was delivered\npiped:\n%s\nfrom file:\n%s",
			overPipe, fromDisk)
	}
}

func TestAnOptionItCannotActOnEndsTheRun(t *testing.T) {
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"--nope"}, "unknown option"},
		{[]string{"--version"}, "--version requires an argument"},
		{[]string{"--install-dir"}, "--install-dir requires an argument"},
	} {
		stdout, stderr, code := run(t, piped, c.args...)
		if code != 1 {
			t.Errorf("%v: exit %d, want 1\nstdout: %s\nstderr: %s", c.args, code, stdout, stderr)
		}
		if !strings.Contains(stderr, c.want) {
			t.Errorf("%v: did not say %q\nstderr: %s", c.args, c.want, stderr)
		}
	}
}

var (
	parseArgs = regexp.MustCompile(`(?s)\nparse_args\(\) \{\n(.*?)\n\}\n`)
	caseLabel = regexp.MustCompile(`(?m)^\s*(-[^)\n]*)\)`)
	longFlag  = regexp.MustCompile(`--[a-z][a-z-]*`)
)

// The help is a literal, so nothing but this keeps it in step with the parser it
// describes.
func TestEveryOptionTheParserTakesIsInTheHelp(t *testing.T) {
	source, err := os.ReadFile(script)
	if err != nil {
		t.Fatalf("could not read %s: %v", script, err)
	}
	body := parseArgs.FindSubmatch(source)
	if body == nil {
		t.Fatal("parse_args is not in the installer under that name")
	}

	var parsed []string
	for _, label := range caseLabel.FindAllSubmatch(body[1], -1) {
		for _, flag := range longFlag.FindAll(label[1], -1) {
			parsed = append(parsed, string(flag))
		}
	}
	parsed = slices.Compact(slices.Sorted(slices.Values(parsed)))
	if len(parsed) == 0 {
		t.Fatal("no options found in parse_args")
	}

	help, _, _ := run(t, piped, "--help")
	documented := slices.Compact(slices.Sorted(slices.Values(longFlag.FindAllString(help, -1))))

	if !slices.Equal(parsed, documented) {
		t.Errorf("the parser takes %v, the help names %v", parsed, documented)
	}
}
