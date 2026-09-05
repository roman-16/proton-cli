//go:build windows

package ui

import (
	"os"

	"golang.org/x/sys/windows"
)

// terminalDepth is what the console behind f can render.
//
// A Windows console prints an escape sequence instead of acting on it until a
// program asks for the parser, which is what the mode below turns on. A console
// hosted by a pseudoconsole - a Windows Terminal tab, an SSH session - is given
// it already; a conhost window is not. So asking for it is both the switch and
// the check: the mode is refused by a handle that has no console to render one.
//
// Ordinary output processing rides along because the documentation for the flag
// asks for the two together.
//
// A console that takes the mode takes 24-bit colour with it, and nothing here
// reads COLORTERM: Windows sets neither that nor TERM, and exact colour has been
// in the console since Windows 10 1703, which is older than any build Go itself
// runs on.
func terminalDepth(f *os.File) depth {
	handle := windows.Handle(f.Fd())
	var mode uint32
	if err := windows.GetConsoleMode(handle, &mode); err != nil {
		return depthNone
	}
	want := mode | windows.ENABLE_PROCESSED_OUTPUT | windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING
	if want != mode && windows.SetConsoleMode(handle, want) != nil {
		return depthNone
	}
	return depthDirect
}
