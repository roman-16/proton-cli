package kit

import (
	"os/exec"
	"time"
)

// browserGrace is how long a launcher is given to say it failed before it is
// taken to have worked.
//
// Every one of them hands the address to something else and returns, so an exit
// arrives almost immediately or not at all. One still running after this has
// found a browser; one that was going to refuse - no display, nothing
// registered for https - has already done so.
const browserGrace = 2 * time.Second

// ShowInBrowser asks the desktop to open url, and reports whether it looks like
// it did.
//
// The answer chooses a word and nothing more. The address is printed either way,
// so a machine where this fails - a server, a container, a session with no
// desktop - loses a convenience rather than the ability to verify, and there is
// no need to work out in advance which kind of machine this is.
func ShowInBrowser(url string) bool {
	name, args := opener(url)
	if name == "" {
		return false
	}
	cmd := exec.Command(name, args...)
	if err := cmd.Start(); err != nil {
		return false
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return err == nil
	case <-time.After(browserGrace):
		return true
	}
}
