//go:build linux

package external

import (
	"context"
	"errors"
	"fmt"
	"os"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestRunnerInteractivePreservesRealTerminalDescriptors(t *testing.T) {
	_, terminal := openTestPTY(t)
	r := fixture(t, `[ -t 0 ] && [ -t 1 ] && [ -t 2 ]`)
	result, err := r.RunInteractive(context.Background(), terminal, terminal)
	if err != nil {
		t.Fatalf("interactive child did not receive terminal descriptors: %v", err)
	}
	if result.Stdout != "" || result.Stderr != "" {
		t.Fatalf("direct terminal output was unexpectedly captured: %#v", result)
	}
}

func TestRunnerInteractiveRealTerminalStillHasTypedTimeout(t *testing.T) {
	_, terminal := openTestPTY(t)
	r := fixture(t, `sleep 5`)
	r.Timeout = 20 * time.Millisecond
	_, err := r.RunInteractive(context.Background(), terminal, terminal)
	var timeout *TimeoutError
	if !errors.As(err, &timeout) {
		t.Fatalf("RunInteractive error = %T %v", err, err)
	}
}

func openTestPTY(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("open PTY master: %v", err)
	}
	t.Cleanup(func() { _ = master.Close() })
	if err := unix.IoctlSetPointerInt(int(master.Fd()), unix.TIOCSPTLCK, 0); err != nil {
		t.Skipf("unlock PTY: %v", err)
	}
	number, err := unix.IoctlGetInt(int(master.Fd()), unix.TIOCGPTN)
	if err != nil {
		t.Skipf("find PTY slave: %v", err)
	}
	slave, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", number), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("open PTY slave: %v", err)
	}
	t.Cleanup(func() { _ = slave.Close() })
	return master, slave
}
