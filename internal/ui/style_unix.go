//go:build !windows

package ui

import "os"

// terminalDepth is what the terminal behind f can render.
//
// TERM is the name a terminal is known by, set by whatever opened it, and the
// terminfo entry it names is what says how to talk to it. Nothing, or the name
// dumb, says there is no such entry: a build system capturing output through a
// pty, an editor's shell buffer, a serial console. Any other name has an entry,
// and every entry there is has colour.
//
// The descriptor is not consulted. A terminal is whatever the session is
// attached to, so both streams reach the same one.
func terminalDepth(*os.File) depth {
	switch os.Getenv("TERM") {
	case "", "dumb":
		return depthNone
	}
	return advertised()
}
