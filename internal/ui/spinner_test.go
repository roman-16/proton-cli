package ui

import (
	"bytes"
	"strings"
	"testing"
)

// The frames are drawn in one cell, in place, so what is on the screen at any
// moment is one glyph and never a trail of them.
func TestEachFrameReplacesTheOneBeforeIt(t *testing.T) {
	var screen bytes.Buffer
	s := newSpinner(&screen, Style{})
	s.draw()
	s.draw()

	frames := []rune(GlyphWorking)
	want := "\r" + clearToEOL + string(frames[0]) + "\r" + clearToEOL + string(frames[1])
	if screen.String() != want {
		t.Errorf("drew %q, want %q", screen.String(), want)
	}
}

// Whatever the run has to say, the sign of life is off the screen before the
// first byte of it lands - and it is the same sign of life whichever stream is
// written to, because the answer and the commentary share a terminal.
func TestTheFirstThingWrittenTakesTheScreenBack(t *testing.T) {
	for _, stream := range []string{"the answer", "the commentary"} {
		t.Run(stream, func(t *testing.T) {
			var screen bytes.Buffer
			s := newSpinner(&screen, Style{})
			out := yielding{to: &screen, sp: s}
			s.draw()
			if _, err := out.Write([]byte("a row\n")); err != nil {
				t.Fatal(err)
			}
			s.draw()

			written := screen.String()
			erased := "\r" + clearToEOL + "a row\n"
			if !strings.HasSuffix(written, erased) {
				t.Errorf("the frame was left beside the output: %q", written)
			}
			if strings.Count(written, string([]rune(GlyphWorking)[0])) != 1 {
				t.Errorf("it went on drawing after the run had spoken: %q", written)
			}
		})
	}
}

// Nothing was drawn, so there is nothing to erase: a run quick enough to answer
// inside the first tenth of a second leaves the screen exactly as it found it.
func TestARunThatNeverWaitedWritesNothing(t *testing.T) {
	var screen bytes.Buffer
	newSpinner(&screen, Style{}).stop()
	if screen.Len() != 0 {
		t.Errorf("a spinner that never drew wrote %q", screen.String())
	}
}

// Only a terminal that is being watched gets one: a pipe, a machine format and
// a quietened run each keep the commentary stream exactly as it was.
func TestNoSignOfLifeWhereNobodyIsWatching(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts Options
	}{
		{name: "a pipe", opts: Options{}},
		{name: "quietened", opts: Options{Quiet: true}},
		{name: "for a machine", opts: Options{Format: FormatJSON}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out, errb bytes.Buffer
			opts := tc.opts
			opts.Out, opts.Err = &out, &errb
			u := New(opts)
			if u.sp != nil {
				t.Error("a stream nobody is watching was given a sign of life")
			}
			u.Working()()
			if errb.Len() != 0 {
				t.Errorf("the commentary stream was written to: %q", errb.String())
			}
		})
	}
}
