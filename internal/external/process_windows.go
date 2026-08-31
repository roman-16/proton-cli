//go:build windows

package external

import "os/exec"

// CommandContext's default cancellation terminates the process on Windows.
func configureProcess(*exec.Cmd) {}
