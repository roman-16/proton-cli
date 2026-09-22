package ui

import (
	"bytes"
	"strings"
	"testing"
)

// The code a sign-in shows, which is the longest thing that is ever drawn as
// one: a user code, a 32-byte key in base64, and a client name.
const signInCode = "0:8FJ3K2QP:qm4Xb2v9tR1sLp0yZcHwKdNfEuAgJi7MoBxVn3Tl5Qs=:web-account"

func TestQRFoldsTwoModuleRowsIntoEachLine(t *testing.T) {
	lines := qrLines([][]bool{
		{true, true, false, false},
		{true, false, true, false},
		{false, true, false, true},
	})
	want := []string{"█▀▄ ", " ▀ ▀"}
	if len(lines) != len(want) {
		t.Fatalf("drew %d lines, want %d: %q", len(lines), len(want), lines)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, lines[i], want[i])
		}
	}
}

// The code has to fit the window somebody is signing in through, which is the
// smallest terminal anybody still has.
func TestQRFitsAnEightyByTwentyFourTerminal(t *testing.T) {
	var out bytes.Buffer
	u := New(Options{})
	u.Err = &out

	if err := u.QR(signInCode); err != nil {
		t.Fatalf("QR: %v", err)
	}
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) > 23 {
		t.Errorf("drew %d lines, which leaves nothing on screen beside it", len(lines))
	}
	for i, line := range lines {
		if width := len([]rune(line)); width > 80 {
			t.Errorf("line %d is %d columns wide", i, width)
		}
	}
}

// Every line is the same width, which is what makes the drawing a square rather
// than a shape a scanner gives up on.
func TestQRDrawsASquare(t *testing.T) {
	var out bytes.Buffer
	u := New(Options{})
	u.Err = &out

	if err := u.QR(signInCode); err != nil {
		t.Fatalf("QR: %v", err)
	}
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	for i, line := range lines {
		if len([]rune(line)) != len([]rune(lines[0])) {
			t.Fatalf("line %d is %d columns and line 0 is %d",
				i, len([]rune(line)), len([]rune(lines[0])))
		}
	}
}

// A code too long to draw is reported rather than left blank.
func TestQRSaysWhenItCannotDrawOne(t *testing.T) {
	var out bytes.Buffer
	u := New(Options{})
	u.Err = &out

	if err := u.QR(strings.Repeat("x", 8000)); err == nil {
		t.Error("a code that cannot be drawn was reported as drawn")
	}
}
