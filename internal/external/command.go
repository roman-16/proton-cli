// Package external runs optional command-line tools behind a small, testable
// boundary. Product integrations use it instead of starting processes directly
// so discovery, time limits and failures have one meaning throughout the CLI.
package external

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"
)

const DefaultTimeout = 30 * time.Second

// Runner discovers and executes one external program.
type Runner struct {
	Name     string
	Timeout  time.Duration
	LookPath func(string) (string, error)
}

// Result is the complete output of a successful invocation. Keeping stderr
// separate matters: many tools use it for progress even when they succeed.
type Result struct {
	Stdout string
	Stderr string
}

// MissingError means the optional executable could not be found on PATH.
type MissingError struct{ Name string }

func (e *MissingError) Error() string {
	return fmt.Sprintf("required executable %q was not found on PATH", e.Name)
}

// ExitError is a completed invocation rejected by the external program.
type ExitError struct {
	Name   string
	Code   int
	Stderr string
}

func (e *ExitError) Error() string {
	if e.Stderr == "" {
		return fmt.Sprintf("%s exited with status %d", e.Name, e.Code)
	}
	return fmt.Sprintf("%s exited with status %d: %s", e.Name, e.Code, e.Stderr)
}

// TimeoutError means the configured execution deadline elapsed.
type TimeoutError struct {
	Name    string
	Timeout time.Duration
}

func (e *TimeoutError) Error() string {
	return fmt.Sprintf("%s did not finish within %s", e.Name, e.Timeout)
}

// Run executes args without a shell and returns captured output. The caller's
// cancellation always wins over the runner's own timeout.
func (r Runner) Run(ctx context.Context, args ...string) (Result, error) {
	return r.run(ctx, nil, nil, args...)
}

// RunWithInput is Run with an attached input stream. It exists for external
// programs that own an interactive credential exchange. Output remains captured
// so it cannot contaminate this CLI's stdout; callers render only normalized
// results and errors after the process finishes.
func (r Runner) RunWithInput(ctx context.Context, input io.Reader, args ...string) (Result, error) {
	return r.run(ctx, input, nil, args...)
}

// RunInteractive is RunWithInput with the child process's output relayed to
// terminal as it arrives. Both child streams go to that one writer: callers
// pass their commentary/error stream so an upstream password or two-factor
// prompt can never contaminate machine-readable stdout. A real terminal file
// is attached directly so interactive children retain their TTY descriptors;
// other writers are relayed and captured, primarily for tests. Exit failures
// and timeouts remain typed in either case.
func (r Runner) RunInteractive(ctx context.Context, input io.Reader, terminal io.Writer, args ...string) (Result, error) {
	return r.run(ctx, input, terminal, args...)
}

func (r Runner) run(ctx context.Context, input io.Reader, terminal io.Writer, args ...string) (Result, error) {
	lookPath := r.LookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	path, err := lookPath(r.Name)
	if err != nil {
		return Result{}, &MissingError{Name: r.Name}
	}

	timeout := r.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(runCtx, path, args...)
	cmd.Stdin = input
	if terminal == nil {
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
	} else if terminalFile, ok := terminal.(*os.File); ok && term.IsTerminal(int(terminalFile.Fd())) {
		// Giving os/exec the file itself avoids its intermediary pipes. Console
		// programs such as Bridge use isatty/readline and may refuse or degrade
		// their shell if handed a writer that merely forwards to a terminal.
		cmd.Stdout, cmd.Stderr = terminalFile, terminalFile
	} else {
		relay := &lockedWriter{writer: terminal}
		cmd.Stdout = captureAndRelay{capture: &stdout, relay: relay}
		cmd.Stderr = captureAndRelay{capture: &stderr, relay: relay}
	}
	configureProcess(cmd)
	err = cmd.Run()
	result := Result{Stdout: stdout.String(), Stderr: stderr.String()}
	if err == nil {
		return result, nil
	}
	if errors.Is(ctx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return result, ctx.Err()
	}
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		return result, &TimeoutError{Name: r.Name, Timeout: timeout}
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return result, &ExitError{
			Name: r.Name, Code: exitErr.ExitCode(), Stderr: clean(stderr.String()),
		}
	}
	return result, fmt.Errorf("run %s: %w", r.Name, err)
}

// lockedWriter keeps stdout and stderr relays from writing through a shared
// terminal concurrently. A terminal is best-effort commentary: a closed relay
// must not turn an otherwise valid child invocation into a broken-pipe failure.
type lockedWriter struct {
	mu     sync.Mutex
	writer io.Writer
}

func (w *lockedWriter) write(p []byte) {
	w.mu.Lock()
	defer w.mu.Unlock()
	_, _ = w.writer.Write(p)
}

type captureAndRelay struct {
	capture *bytes.Buffer
	relay   *lockedWriter
}

func (w captureAndRelay) Write(p []byte) (int, error) {
	_, _ = w.capture.Write(p)
	w.relay.write(p)
	return len(p), nil
}

// DecodeJSON parses a tool's machine-readable stdout into the integration's
// declared response type. Unknown fields are intentionally accepted so an
// additive tool upgrade does not break an older proton binary; malformed or
// concatenated documents are rejected.
func DecodeJSON[T any](result Result) (T, error) {
	var value T
	dec := json.NewDecoder(strings.NewReader(result.Stdout))
	if err := dec.Decode(&value); err != nil {
		return value, fmt.Errorf("decode external JSON: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return value, errors.New("decode external JSON: multiple documents")
		}
		return value, fmt.Errorf("decode external JSON: %w", err)
	}
	return value, nil
}

func clean(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
