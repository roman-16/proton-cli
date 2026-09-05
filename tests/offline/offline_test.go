// Package offline runs the real binary against no account at all.
//
// The interface promises that anything judgeable from the command line alone is
// judged before the network: an unknown setting key, a value outside a declared
// domain, a colour off Proton's palette, a reference shaped like nothing. Those
// answers cost a process and nothing else, so paying a round trip to Proton for
// each of them - and a set of credentials to reach them - is a cost with no
// evidence behind it.
//
// So this suite is the same binary, the same argv and the same exit codes as the
// live one, with two things taken away: there is no session, and the API is
// pointed at a port nothing listens on. A test that passes here therefore proves
// its answer needed neither.
package offline

import (
	"bytes"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

var (
	binaryPath string
	configDir  string
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "proton-cli-offline-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create temp dir: %v\n", err)
		os.Exit(1)
	}
	binaryPath = filepath.Join(dir, "proton")
	build := exec.Command("go", "build", "-o", binaryPath, "../../cmd/proton")
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		_ = os.RemoveAll(dir)
		fmt.Fprintf(os.Stderr, "failed to build binary: %v\n", err)
		os.Exit(1)
	}
	// An empty config directory of our own: no session to find, and no chance of
	// touching the one the developer is signed in with.
	configDir = filepath.Join(dir, "config")
	if err := os.MkdirAll(configDir, 0700); err != nil {
		fmt.Fprintf(os.Stderr, "failed to create the config dir: %v\n", err)
		os.Exit(1)
	}

	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// run executes the CLI with nothing to act as and nowhere to connect to.
func run(t *testing.T, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	return runIn(t, "", nil, args...)
}

// runWithStdin is run with something on standard input, for the answers that are
// about the stream itself.
func runWithStdin(t *testing.T, stdin string, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	return runIn(t, stdin, nil, args...)
}

// runWithEnv is run with more in the environment, for the answers that the
// environment is what settles.
func runWithEnv(t *testing.T, env map[string]string, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	return runIn(t, "", env, args...)
}

// runIn starts the binary, and is the only thing here that does.
//
// The environment is built from scratch rather than inherited, for the same
// reason the live suite does it: whatever a developer happens to have exported,
// the binary under test sees a stated environment. Here that environment states
// that there is no account and no API, plus whatever the test is about.
func runIn(t *testing.T, stdin string, env map[string]string, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	var outBuf, errBuf bytes.Buffer
	cmd := exec.Command(binaryPath, args...)
	cmd.Stdin = strings.NewReader(stdin)
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	cmd.Env = []string{
		"PROTON_PROFILE=nobody",
		"PROTON_NO_INPUT=1",
		"XDG_CONFIG_HOME=" + configDir,
		// Nothing listens here. A test that reaches the network fails with a
		// connection error instead of quietly passing for the wrong reason.
		"PROTON_API_URL=http://127.0.0.1:1",
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + configDir,
	}
	for _, name := range slices.Sorted(maps.Keys(env)) {
		cmd.Env = append(cmd.Env, name+"="+env[name])
	}
	if err := cmd.Run(); err != nil {
		exitErr, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("failed to run %v: %v", args, err)
		}
		exitCode = exitErr.ExitCode()
	}
	return outBuf.String(), errBuf.String(), exitCode
}

// refuses runs the command and asserts it was refused with the given exit code
// and that every phrase appears in the answer.
//
// The whole accepted domain belongs in the message: somebody who guessed wrong
// needs the list, not the news that they were wrong.
func refuses(t *testing.T, wantExit int, args []string, phrases ...string) {
	t.Helper()
	stdout, stderr, code := run(t, args...)
	if code != wantExit {
		t.Errorf("%v: exit %d, want %d\nstderr: %s", args, code, wantExit, truncate(stderr))
	}
	if strings.Contains(stderr, "request failed") || strings.Contains(stderr, "connection refused") {
		t.Errorf("%v: reached the network, so this answer was not judged from the command line\nstderr: %s",
			args, truncate(stderr))
	}
	if stdout != "" {
		t.Errorf("%v: wrote to stdout while refusing: %q", args, truncate(stdout))
	}
	for _, want := range phrases {
		if !strings.Contains(stderr, want) {
			t.Errorf("%v: stderr does not mention %q\nstderr: %s", args, want, truncate(stderr))
		}
	}
}

func truncate(s string) string {
	if len(s) > 400 {
		return s[:400] + "...(truncated)"
	}
	return s
}

// TestEveryInvocationGoesThroughTheRunner keeps the binary from being spawned
// anywhere but here, so there is one place that states what a command runs
// without - as in the live suite, where the same rule keeps a test from acting as
// the wrong account.
func TestEveryInvocationGoesThroughTheRunner(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read the test directory: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), "_test.go") || e.Name() == "offline_test.go" {
			continue
		}
		src, err := os.ReadFile(e.Name())
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		if strings.Contains(string(src), "exec.Command") {
			t.Errorf("%s starts the binary itself; go through run so the missing account and API are stated in one place", e.Name())
		}
	}
}
