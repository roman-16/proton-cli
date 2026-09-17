package ui

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/roman-16/proton-cli/internal/progress"
)

// A progress bar is for a human watching a terminal. Everywhere else it must
// vanish, or it corrupts a log file and a JSON stream alike.
func TestNewProgressIsNopWhereItWouldBeNoise(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts Options
	}{
		{"stderr is not a terminal", Options{}},
		{"quiet", Options{Quiet: true}},
		{"machine output", Options{Format: FormatJSON}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u, _, errb := fixture(t, tc.opts)
			p := NewProgress(u)
			if _, isNop := p.(progress.Nop); !isNop {
				t.Errorf("want a no-op sink, got %T", p)
			}
			p.Start(100, "Uploading")
			p.Add(50)
			p.Done()
			if errb.Len() != 0 {
				t.Errorf("a no-op sink wrote %q", errb.String())
			}
		})
	}
}

// bar returns a Progress drawing into a buffer, redrawing on every call so the
// frames are deterministic, and laid out for a terminal of the given width.
func bar(width int) (*Progress, *bytes.Buffer) {
	var buf bytes.Buffer
	return &Progress{w: &buf, active: true, width: func() int { return width }}, &buf
}

// frames splits what was drawn into the successive states of the line.
func frames(buf *bytes.Buffer) []string {
	out := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\r")
	for i, f := range out {
		out[i] = strings.ReplaceAll(f, clearToEOL, "")
	}
	return out[1:] // the leading field before the first carriage return is empty
}

func TestProgressFrames(t *testing.T) {
	p, buf := bar(80)
	p.Start(1000, "Uploading report.pdf")
	p.Add(500)
	p.Add(500)
	p.Done()

	got := frames(buf)
	if len(got) != 4 { // start, two additions, and the final frame Done draws
		t.Fatalf("want 4 frames, got %d: %q", len(got), buf.String())
	}
	for i, want := range []string{
		"  0%  0 B / 1000 B",
		" 50%  500 B / 1000 B",
		"100%  1000 B / 1000 B",
		"100%  1000 B / 1000 B",
	} {
		if !strings.Contains(got[i], want) {
			t.Errorf("frame %d: want %q in %q", i, want, got[i])
		}
		if !strings.HasPrefix(got[i], "Uploading report.pdf") {
			t.Errorf("frame %d: the label should lead the line: %q", i, got[i])
		}
	}
	if !strings.HasSuffix(buf.String(), "\n") {
		t.Error("Done must close the line so the next output starts fresh")
	}
}

// Work measured in things counts them, in the same line a transfer draws: the
// bar, the percentage, what is done of what there is, and how fast.
func TestProgressCountsThingsWhereThereAreNoBytes(t *testing.T) {
	p, buf := bar(80)
	p.noun = "messages"
	p.Start(48213, "Indexing mail")
	p.Add(12400)

	got := frames(buf)[1]
	for _, want := range []string{"Indexing mail", " 26%", "12400 / 48213 messages"} {
		if !strings.Contains(got, want) {
			t.Errorf("want %q in %q", want, got)
		}
	}
	if strings.Contains(got, "B") {
		t.Errorf("a count of messages should carry no byte unit: %q", got)
	}

	if rate := p.speed(8); rate != "8.0/s" {
		t.Errorf("speed = %q, want things per second", rate)
	}
}

// Work that happens in stages draws one line each, and each line names what it
// is counting.
//
// A mailbox is read for its messages and then downloaded body by body, and the
// second stage is where the hours go: a line that stayed on the first stage's
// label and its noun would report the wrong work at the wrong scale.
func TestProgressDrawsALinePerStage(t *testing.T) {
	p, buf := bar(80)
	p.noun = "messages"
	p.Start(10887, "Indexing mail")
	p.Add(10887)
	p.Done()
	progress.Counting(p, "bodies")
	p.Start(10887, "Indexing mail bodies")
	progress.Resume(p, 2830)
	p.Done()

	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("want a line per stage, got %d: %q", len(lines), buf.String())
	}
	if walked := lines[0]; !strings.Contains(walked, "Indexing mail") ||
		!strings.Contains(walked, "100%") || !strings.Contains(walked, "10887 / 10887 messages") {
		t.Errorf("the first stage closed as %q", walked)
	}
	if fetched := lines[1]; !strings.Contains(fetched, "Indexing mail bodies") ||
		!strings.Contains(fetched, " 26%") || !strings.Contains(fetched, "2830 / 10887 bodies") {
		t.Errorf("the second stage drew %q", fetched)
	}
}

// Work whose size is not known until it ends closes at 100%: a walk that has
// finished is a walk that covered all of it, whatever the number turned out to
// be.
func TestProgressWithNoTotalClosesFull(t *testing.T) {
	p, buf := bar(80)
	p.noun = "items"
	p.Start(0, "Indexing drive")
	p.Add(8120)
	p.Done()

	drawn := frames(buf)
	if running := drawn[1]; !strings.Contains(running, "  0%") || !strings.Contains(running, "8120 items") {
		t.Errorf("while running, want the count and no claim about the share done: %q", running)
	}
	if last := drawn[len(drawn)-1]; !strings.Contains(last, "100%") {
		t.Errorf("a finished walk should close at 100%%: %q", last)
	}
}

// A shorter line has to erase the longer one it replaces, or the tail of the old
// frame stays on screen.
func TestProgressErasesTheRestOfTheLine(t *testing.T) {
	p, buf := bar(80)
	p.Start(1000, "Uploading")
	if !strings.Contains(buf.String(), clearToEOL) {
		t.Errorf("a redraw must clear to end of line: %q", buf.String())
	}
}

// Encryption adds per-block overhead, so the byte counter can overrun the source
// size. The bar reports the size the user asked about rather than going past
// 100%.
func TestProgressClampsOverrun(t *testing.T) {
	p, buf := bar(80)
	p.Start(100, "Uploading")
	p.Add(140)

	last := frames(buf)[1]
	if !strings.Contains(last, "100%  100 B / 100 B") {
		t.Errorf("overrun not clamped: %q", last)
	}
	if strings.Contains(last, GlyphBarPending) {
		t.Errorf("a complete bar should have no pending segment: %q", last)
	}
	if strings.Count(last, GlyphBarFilled) != barWidth {
		t.Errorf("a complete bar should be entirely filled: %q", last)
	}
}

// An unknown total still has to draw without dividing by zero, and says how much
// has arrived rather than pretending to know how much is coming.
func TestProgressUnknownTotal(t *testing.T) {
	p, buf := bar(80)
	p.Start(0, "Uploading")
	p.Add(4096)

	last := frames(buf)[1]
	if !strings.Contains(last, "  0%") || !strings.Contains(last, "4.0 KB") {
		t.Errorf("unknown total should report 0%% and the bytes so far: %q", last)
	}
	if strings.Contains(last, " / ") {
		t.Errorf("unknown total must not claim a size: %q", last)
	}
}

// The line adapts to the terminal rather than wrapping, giving up the estimate
// first and the label last.
func TestProgressFitsTheTerminal(t *testing.T) {
	for _, width := range []int{120, 80, 60, 40, 30, 20, 12} {
		p, buf := bar(width)
		p.Start(2_400_000_000, "Uploading a-rather-long-file-name.tar.gz")
		p.Add(500_000_000)
		for i, f := range frames(buf) {
			if n := Cells(stripANSI(f)); n >= width {
				t.Errorf("width %d, frame %d: line is %d cells: %q", width, i, n, f)
			}
		}
	}
}

// A rate needs enough history to be a measurement rather than an extrapolation
// from two readings a microsecond apart.
func TestProgressWithholdsARateUntilItMeansSomething(t *testing.T) {
	p, buf := bar(120)
	p.Start(1000, "Uploading")
	p.Add(100)
	if strings.Contains(buf.String(), "/s") {
		t.Errorf("a rate claimed too early: %q", buf.String())
	}

	p.samples = []sample{{time.Now().Add(-2 * time.Second), 0}, {time.Now(), 200}}
	if r := p.rate(); r < 90 || r > 110 {
		t.Errorf("rate over a real window = %v, want about 100", r)
	}
}

// Work that arrives in bursts is reported at the rate it is being done, not at
// the rate of whichever burst it is in.
//
// Indexing spends its time in bursts: requests in flight, then a page written
// to disk with nothing moving. Averaged over a window that fits inside one of
// them, the same steady work reads as 34 a second and then as 20 - a number
// nobody can act on, and an estimate that swings with it.
func TestProgressRateSurvivesBurstyWork(t *testing.T) {
	p, _ := bar(120)
	p.noun = "messages"
	p.total = 100000
	now := time.Now()

	// Twenty-five a second, arriving ten at a time with a pause after each page.
	done := int64(0)
	at := now.Add(-rateWindow)
	for at.Before(now) {
		for range 10 {
			at = at.Add(40 * time.Millisecond)
			done += 10
			p.samples = append(p.samples, sample{at, done})
		}
		at = at.Add(3600 * time.Millisecond)
		p.samples = append(p.samples, sample{at, done})
	}
	p.current = done

	rate := p.rate()
	if rate < 20 || rate > 30 {
		t.Errorf("rate = %.1f, want the work's own 25 a second rather than a burst's", rate)
	}

	// And the burst the bar happens to be drawn in does not run away with it.
	for range 10 {
		at = at.Add(40 * time.Millisecond)
		done += 10
		p.samples = append(p.samples, sample{at, done})
	}
	if burst := p.rate(); burst > rate*1.3 {
		t.Errorf("a burst moved the rate from %.1f to %.1f", rate, burst)
	}
}

// A build that carries on shows how much of the whole is covered, and counts
// none of it as work done just now.
func TestProgressResumesWithoutCountingThePast(t *testing.T) {
	p, buf := bar(80)
	p.noun = "messages"
	p.Start(48213, "Indexing mail")
	p.Resume(12400)

	resumed := frames(buf)[1]
	if !strings.Contains(resumed, " 26%") || !strings.Contains(resumed, "12400 / 48213 messages") {
		t.Errorf("a resumed build should open where it left off: %q", resumed)
	}
	if strings.Contains(resumed, "/s") {
		t.Errorf("what was done before the run began is not a rate: %q", resumed)
	}
	if r := p.rate(); r != 0 {
		t.Errorf("rate = %v straight after resuming, want none claimed yet", r)
	}

	// From there it measures what this run does, not what a previous one did.
	p.samples = []sample{{time.Now().Add(-10 * time.Second), 12400}, {time.Now(), 12600}}
	if r := p.rate(); r < 15 || r > 25 {
		t.Errorf("rate = %.1f, want the 20 a second this run managed", r)
	}
}

// A count of things is reported to a decimal only while the decimal says
// something; above ten a second it is a digit that changes every redraw.
func TestProgressRoundsARateThatIsFastEnough(t *testing.T) {
	p, _ := bar(120)
	p.noun = "messages"
	for _, tc := range []struct {
		rate float64
		want string
	}{{8.24, "8.2/s"}, {9.96, "10.0/s"}, {26.7, "27/s"}, {120.4, "120/s"}} {
		if got := p.speed(tc.rate); got != tc.want {
			t.Errorf("speed(%v) = %q, want %q", tc.rate, got, tc.want)
		}
	}
}

func TestProgressDoneIsIdempotent(t *testing.T) {
	p, buf := bar(80)
	p.Start(10, "x")
	p.Done()
	before := buf.Len()
	p.Done()
	p.Add(5)
	if buf.Len() != before {
		t.Errorf("writing after Done: %q", buf.String()[before:])
	}
}
