package live

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

// A command that stays attached until it is told to stop.
//
// It records what it asked Proton for, the same as runAs does: a watch is the
// only way two of the requests this CLI can send are ever sent, so a helper that
// skipped the trace would have `just coverage` record a suite that never reached
// them.

// watch is a long-running subprocess under the same environment runAs builds,
// for the commands that stay attached until told to stop.
type watch struct {
	cmd  *exec.Cmd
	errb *bytes.Buffer

	// lines carries each stdout line as it is written, and buf keeps the whole
	// run for a report when a timeout needs one.
	lines chan string
	buf   *bytes.Buffer
	// ready closes once the opening line has reached stderr, which is how a
	// test knows the watch has authenticated and begun.
	ready chan struct{}
	once  sync.Once
	done  chan error

	// profile, args and started are what the trace needs to record the
	// invocation once the watch has stopped.
	profile string
	args    []string
	started time.Time
	traced  sync.Once
}

// record hands the watch's stderr to the trace, so the requests it made count
// towards what the live suite reached. It runs once, whenever the watch ends.
func (w *watch) record(exitCode int) {
	w.traced.Do(func() {
		_ = trace(w.profile, w.args, time.Since(w.started), exitCode, w.errb.String())
	})
}

// watchAs starts the command as the named profile and returns once it begins.
//
// It records what the watch asked Proton for, the same as runAs does. A watch is
// the only way two of the requests this CLI can send are ever sent, so a helper
// that skipped the trace would have `just coverage` record a suite that never
// reached them - which is exactly what happened until this did it too.
func watchAs(profile string, args ...string) (*watch, error) {
	a, ok := accounts[profile]
	if !ok {
		return nil, fmt.Errorf("unknown test profile %q", profile)
	}
	args = withPassword(a, args)

	w := &watch{
		cmd:     exec.Command(binaryPath, args...),
		errb:    &bytes.Buffer{},
		lines:   make(chan string),
		buf:     &bytes.Buffer{},
		ready:   make(chan struct{}),
		done:    make(chan error, 1),
		profile: profile,
		args:    args,
		started: time.Now(),
	}
	w.cmd.Env = childEnv(profile)
	if tracingRequests() {
		w.cmd.Env = append(w.cmd.Env, "PROTON_LOG_LEVEL=debug")
	}

	outPipe, err := w.cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	errPipe, err := w.cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := w.cmd.Start(); err != nil {
		return nil, err
	}
	go func() { w.done <- w.cmd.Wait() }()
	go func() {
		scanner := bufio.NewScanner(outPipe)
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)
		for scanner.Scan() {
			w.buf.WriteString(scanner.Text() + "\n")
			w.lines <- scanner.Text()
		}
	}()
	go func() {
		scanner := bufio.NewScanner(errPipe)
		for scanner.Scan() {
			w.errb.WriteString(scanner.Text() + "\n")
			if strings.HasPrefix(scanner.Text(), "Watching ") {
				w.once.Do(func() { close(w.ready) })
			}
		}
	}()
	return w, nil
}

// waitReady blocks until the watch has begun streaming, or fails on timeout.
func (w *watch) waitReady(t *testing.T, timeout time.Duration) {
	t.Helper()
	select {
	case <-w.ready:
	case err := <-w.done:
		t.Fatalf("watch exited before it began: %v\nstderr:\n%s", err, w.errb.String())
	case <-time.After(timeout):
		t.Fatalf("watch did not begin within %s\nstderr:\n%s", timeout, w.errb.String())
	}
}

// waitForLine returns the first streamed line satisfying check, or fails after
// the timeout. The watch keeps running either way.
func (w *watch) waitForLine(t *testing.T, timeout time.Duration, check func(string) bool) string {
	t.Helper()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case line := <-w.lines:
			if check(line) {
				return line
			}
		case err := <-w.done:
			t.Fatalf("watch ended before the expected line: %v\nreceived:\n%s\nstderr:\n%s",
				err, w.buf.String(), w.errb.String())
		case <-timer.C:
			t.Fatalf("watch produced no matching line within %s\nreceived:\n%s\nstderr:\n%s",
				timeout, w.buf.String(), w.errb.String())
		}
	}
}

// stop interrupts the watch and expects a clean exit, which is what Ctrl+C
// means to it.
func (w *watch) stop(t *testing.T) {
	t.Helper()
	if w.cmd.Process == nil {
		return
	}
	_ = w.cmd.Process.Signal(os.Interrupt)
	select {
	case err := <-w.done:
		w.record(w.cmd.ProcessState.ExitCode())
		if err != nil {
			t.Fatalf("watch did not stop cleanly: %v\nstderr:\n%s", err, w.errb.String())
		}
	case <-time.After(5 * time.Second):
		w.record(-1)
		t.Fatalf("watch did not stop after SIGINT\nstderr:\n%s", w.errb.String())
	}
}
