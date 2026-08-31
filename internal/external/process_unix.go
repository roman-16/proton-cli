//go:build !windows

package external

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// configureProcess gives the invocation its own process group. CommandContext
// can then stop the whole tree rather than only the executable it started;
// otherwise a helper's child may keep captured pipes open past the deadline.
func configureProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
}
