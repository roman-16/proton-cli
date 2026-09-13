package ui

import (
	"bytes"
	"strings"
	"testing"
)

func guardedString(t *testing.T, s string) string {
	t.Helper()
	var out bytes.Buffer
	n, err := guarded{to: &out}.Write([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	if n != len(s) {
		t.Fatalf("reported %d of %d bytes written", n, len(s))
	}
	return out.String()
}

// Everything a sender can put in a subject that a terminal would act on rather
// than draw. None of it reaches the terminal, and all of it reaches the reader.
func TestNothingASenderWroteReachesTheTerminalAsAnInstruction(t *testing.T) {
	for _, c := range []struct{ name, in, want string }{
		{"erase the line and rewrite it", "\x1b[2K\rInvoice paid", "^[[2K^MInvoice paid"},
		{"move the cursor up", "\x1b[3AInvoice paid", "^[[3AInvoice paid"},
		{"ring the bell", "urgent\x07", "urgent^G"},
		{"open a hyperlink", "\x1b]8;;https://evil.test\x1b\\click\x1b]8;;\x1b\\", "^[]8;;https://evil.test^[\\click^[]8;;^[\\"},
		{"write the clipboard", "\x1b]52;c;cGF5\x07", "^[]52;c;cGF5^G"},
		{"reverse the reading order", "invoice\u202egpj.exe", `invoice\u202egpj.exe`},
		{"introduce a sequence with C1", "pay\u009b2K", `pay\u009b2K`},
		{"delete", "pay\x7f\x7f", "pay^?^?"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := guardedString(t, c.in); got != c.want {
				t.Errorf("\n got %q\nwant %q", got, c.want)
			}
		})
	}
}

// The CLI's own styling is the one thing a terminal is asked to act on, so it
// goes through whole - including the colour a swatch mixes, which is not from
// any fixed list.
func TestTheCLIsOwnStylingIsLeftAlone(t *testing.T) {
	for _, s := range []string{
		"\x1b[2mmuted\x1b[22m",
		"\x1b[35maccent\x1b[39m",
		"\x1b[38;2;120;200;80m●\x1b[39m",
		"\x1b[0m",
	} {
		if got := guardedString(t, s); got != s {
			t.Errorf("styling did not survive:\n got %q\nwant %q", got, s)
		}
	}
}

// A body that arrived over SMTP ends every line with a carriage return, so one
// in front of a newline is how a line ends rather than how one is overwritten.
func TestALineEndingSurvivesAndALoneCarriageReturnDoesNot(t *testing.T) {
	if got, want := guardedString(t, "first\r\nsecond\r\n"), "first\r\nsecond\r\n"; got != want {
		t.Errorf("mangled a body's line endings:\n got %q\nwant %q", got, want)
	}
	if got, want := guardedString(t, "shown\rhidden"), "shown^Mhidden"; got != want {
		t.Errorf("\n got %q\nwant %q", got, want)
	}
}

// A redirect is data. `--body-only > body.txt` has to be the body, and a pipe
// into another program has to be what that program was promised.
func TestAStreamWithNoTerminalBehindItCarriesItsBytes(t *testing.T) {
	var out bytes.Buffer
	w := guard(&out, measured{})
	if w != (&out) {
		t.Fatal("wrapped a stream that is not a terminal")
	}
}

// What is drawn and what is measured have to be the same, or the row after a
// cell holding one sits in the wrong column.
func TestAShownRuneIsMeasuredAsWhatIsDrawn(t *testing.T) {
	for _, c := range []struct {
		in   string
		want int
	}{
		{"\r", 2}, {"\x07", 2}, {"\x1b", 2}, {"\x7f", 2}, {"\u202e", 6},
	} {
		if got := Cells(c.in); got != c.want {
			t.Errorf("%q measures %d, drawn as %q", c.in, got, guardedString(t, c.in))
		}
	}
}

// A row is one line, whatever the sender put in the subject.
func TestACellIsFoldedOntoOneLine(t *testing.T) {
	got := oneLine("Invoice\nID        FROM      SUBJECT\n5bH2mQxK  Proton    Reset your password")
	if strings.Contains(got, "\n") {
		t.Errorf("a cell can still end its row: %q", got)
	}
}
