//go:build !windows

package ui

import (
	"os"
	"testing"
)

// What a terminal can render is read from the name it is known by, and the two
// names that mean there is nothing to render to are the ones that switch every
// escape off - the erase a transfer bar draws with as much as the colour.
//
// 24-bit is the second question, and it decides how faithfully a swatch is drawn
// and nothing else: every role is a colour name, which anything with colour at
// all resolves.
func TestTerminalDepthIsReadFromTheTerminalsOwnName(t *testing.T) {
	for _, tc := range []struct {
		term, colorterm string
		want            depth
	}{
		{"", "", depthNone},
		{"dumb", "", depthNone},
		{"dumb", "truecolor", depthNone},
		{"xterm-256color", "", depthNamed},
		{"screen", "8bit", depthNamed},
		{"xterm-256color", "truecolor", depthDirect},
		{"xterm-256color", "24bit", depthDirect},
		{"xterm-direct", "", depthDirect},
	} {
		t.Setenv("TERM", tc.term)
		t.Setenv("COLORTERM", tc.colorterm)
		got := terminalDepth(os.Stdout)
		if got != tc.want {
			t.Errorf("TERM=%q COLORTERM=%q: got depth %d, want %d", tc.term, tc.colorterm, got, tc.want)
		}
		if got.escapes() != (tc.want != depthNone) {
			t.Errorf("TERM=%q COLORTERM=%q: escapes() disagrees with the depth", tc.term, tc.colorterm)
		}
	}
}
