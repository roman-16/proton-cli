package ui

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/roman-16/proton-cli/internal/progress"
	"github.com/roman-16/proton-cli/internal/units"
)

// A transfer raises exactly one question - how long? - so the line answers it.
//
// Everything on it is optional except the label: the parts are dropped in order
// of how little they are missed as the terminal narrows, so the line never wraps
// and never has to be read twice.
const (
	// barWidth is the drawn length of the bar on a comfortable terminal.
	barWidth = 30
	// barFloor is the shortest a bar still says anything at.
	barFloor = 8
	// rateWindow is how much history the speed is averaged over.
	//
	// Long enough to cover the way work actually arrives. A transfer streams and
	// would read steadily over any window, but indexing arrives in bursts - ten
	// requests in flight, then a page written to disk with nothing moving - and a
	// window that fits inside one burst measures the burst rather than the work,
	// which is how a bar comes to say 34/s and 20/s a second apart. A stall still
	// shows: with nothing arriving the average falls away across the window.
	rateWindow = 30 * time.Second
	// minRateWindow is how much has to have happened before a speed is worth
	// claiming. Two readings a millisecond apart support an extrapolation to
	// hundreds of megabytes a second, which is a number, not information.
	minRateWindow = time.Second
	// redrawEvery throttles the line so a fast transfer spends its time
	// transferring rather than drawing.
	redrawEvery = 80 * time.Millisecond
	// defaultWidth is what a terminal is assumed to be when it will not say.
	defaultWidth = 80
	// separator is the gap between the parts of the line, the same two spaces a
	// table puts between its columns.
	separator = "  "
)

// Progress draws a transfer bar on a terminal and nothing anywhere else. It
// implements progress.Sink so services report through the interface and never
// import this package.
//
// The bar is built from the same box-drawing glyphs as a table rule, so a
// transfer looks like part of the same interface rather than a borrowed
// ASCII widget.
type Progress struct {
	w     io.Writer
	style Style
	width func() int
	// interval throttles redraws. Zero draws every time, which is what a test
	// wants and what makes the frames deterministic.
	interval time.Duration
	active   bool
	// noun is what is being counted, for work measured in things rather than in
	// bytes. Empty is bytes, which is what a transfer moves.
	noun string
	// finished says Done has drawn the closing frame, which is what lets work
	// whose size was never known end at 100% rather than at nought.
	finished bool

	// prefix numbers this transfer within a batch, e.g. "[3/27] ".
	prefix  string
	total   int64
	label   string
	current int64

	started  time.Time
	lastDraw time.Time
	// samples is a short trail of (time, bytes) readings, enough to average a
	// rate over the recent past rather than over the whole transfer - a stalled
	// connection should show as slowing down, not as a good average.
	samples []sample
}

type sample struct {
	at time.Time
	n  int64
}

// NewProgress returns a sink drawing on the UI's stderr, or a no-op when --quiet
// was given, the answer is for a program, or stderr is not a terminal that can
// take back what it has drawn. Callers can therefore hand the result straight to
// a service without checking.
//
// A line that is redrawn is a line that has to be erased, and erasing is an
// escape sequence like any other - so a terminal that renders none gets the
// result and no bar, rather than one frame per update with the erase printed in
// front of each.
func NewProgress(u *UI) progress.Sink { return newProgress(u, "") }

// NewCounter is the same bar over work measured in things rather than in bytes:
// messages indexed, files walked. The noun is what the count is of, so the line
// reads "12400 / 48213 messages" where a transfer would read "36.2 MB / 1.9 GB".
func NewCounter(u *UI, noun string) progress.Sink { return newProgress(u, noun) }

func newProgress(u *UI, noun string) progress.Sink {
	if !u.animates() {
		return progress.Nop{}
	}
	return &Progress{w: u.drawing, style: u.errStyle, active: true, interval: redrawEvery, noun: noun, width: func() int {
		if cols := u.err.columns(); cols > 0 {
			return cols
		}
		return defaultWidth
	}}
}

// columns is the terminal width to lay the line out in, falling back to the
// width a terminal is assumed to have when nobody has said otherwise.
func (p *Progress) columns() int {
	if p.width == nil {
		return defaultWidth
	}
	return p.width()
}

// Batch numbers the transfers that follow, so a recursive upload says where it
// is rather than drawing five hundred identical bars. It is a no-op on a sink
// that draws nothing.
func Batch(s progress.Sink, index, total int) progress.Sink {
	p, ok := s.(*Progress)
	if !ok || total <= 1 {
		return s
	}
	p.prefix = fmt.Sprintf("[%d/%d] ", index, total)
	return p
}

// Start opens a line for a stretch of work. Work that happens in stages draws
// one line each, so the stage that finished stays on the screen above the one
// that is running.
func (p *Progress) Start(total int64, label string) {
	p.total, p.label, p.current = total, label, 0
	p.active, p.finished = true, false
	p.started = time.Now()
	p.lastDraw = time.Time{}
	p.samples = p.samples[:0]
	p.draw(true)
}

// Counting says what the count on the line is of.
func (p *Progress) Counting(noun string) { p.noun = noun }

func (p *Progress) Add(n int64) {
	p.current += n
	p.draw(false)
}

// Resume places the bar at what was done before this run began, without any of
// it counting as progress made now.
func (p *Progress) Resume(done int64) {
	p.current = done
	p.started = time.Now()
	p.samples = p.samples[:0]
	p.draw(true)
}

// Done closes the line so whatever prints next starts fresh.
func (p *Progress) Done() {
	if !p.active {
		return
	}
	p.finished = true
	p.draw(true)
	_, _ = fmt.Fprintln(p.w)
	p.active = false
}

// clearToEOL erases whatever the previous, possibly longer, line left behind. A
// bare carriage return overwrites only as far as the new line reaches, so a
// shrinking line would leave the tail of the old one on screen.
const clearToEOL = "\x1b[K"

func (p *Progress) draw(force bool) {
	if !p.active {
		return
	}
	now := time.Now()
	if !force && now.Sub(p.lastDraw) < p.interval {
		return
	}
	p.lastDraw = now

	// Encryption adds per-block overhead, so the byte counter can run past the
	// source size. Report the size the user asked about.
	done := p.current
	if p.total > 0 && done > p.total {
		done = p.total
	}
	p.observe(now, done)

	// Work whose size was never known draws an empty bar while it runs: a
	// fraction of an unknown is not a thing to claim. It closes at full, because
	// finishing is the one moment the fraction is known.
	ratio := 0.0
	switch {
	case p.total > 0:
		ratio = float64(done) / float64(p.total)
	case p.finished:
		ratio = 1
	}
	_, _ = fmt.Fprint(p.w, "\r"+clearToEOL+p.line(ratio, done, now))
}

// observe records a reading and forgets the ones that have aged out.
func (p *Progress) observe(now time.Time, done int64) {
	p.samples = append(p.samples, sample{now, done})
	cut := now.Add(-rateWindow)
	drop := 0
	for drop < len(p.samples)-1 && p.samples[drop].at.Before(cut) {
		drop++
	}
	p.samples = p.samples[drop:]
}

// rate is the recent speed in bytes per second, or 0 when there is not yet
// enough history to claim one.
func (p *Progress) rate() float64 {
	if len(p.samples) < 2 {
		return 0
	}
	first, last := p.samples[0], p.samples[len(p.samples)-1]
	elapsed := last.at.Sub(first.at)
	if elapsed < minRateWindow || last.n <= first.n {
		return 0
	}
	return float64(last.n-first.n) / elapsed.Seconds()
}

// line composes the widest form that fits.
//
// The parts are given up in the order they are least missed: the estimate first
// (it is the least certain), then the speed, then the byte counter, then the bar
// itself narrows. What survives to the very end is the label and the percentage,
// because a transfer nobody can name is worse than one nobody can time.
func (p *Progress) line(ratio float64, done int64, now time.Time) string {
	head := p.prefix + p.label
	pct := fmt.Sprintf("%3.0f%%", ratio*100)

	var bytes, speed, eta string
	if p.total > 0 {
		bytes = p.amount(done) + " / " + p.amount(p.total)
	} else {
		bytes = p.amount(done)
	}
	if p.noun != "" {
		bytes += " " + p.noun
	}
	if r := p.rate(); r > 0 {
		speed = p.speed(r)
		if p.total > 0 && done < p.total {
			left := time.Duration(float64(p.total-done)/r) * time.Second
			eta = units.Duration(left) + " left"
		}
	}
	// A transfer that finished says how long it took instead of how long is left.
	if p.total > 0 && done >= p.total {
		eta = ""
		if d := now.Sub(p.started); d >= time.Second {
			eta = "in " + units.Duration(d)
		}
	}

	budget := p.columns() - 1 // one cell spare, so a full line never wraps
	for _, attempt := range [][]string{
		{head, "", pct, bytes, speed, eta},
		{head, "", pct, bytes, speed},
		{head, "", pct, bytes},
		{head, "", pct},
	} {
		// The bar is the empty slot, so making room for it means allowing for the
		// separator it brings with it as well as for the bar itself.
		fixed := Cells(strings.Join(withoutEmpty(attempt), separator)) + len(separator)
		if bar := budget - fixed; bar >= barFloor {
			attempt[1] = p.bar(min(bar, barWidth), ratio)
			return strings.Join(withoutEmpty(attempt), separator)
		}
	}
	return truncateCells(head+separator+pct, budget)
}

// amount renders how much has been done, in whatever the work is measured in.
func (p *Progress) amount(n int64) string {
	if p.noun == "" {
		return units.Size(n)
	}
	return strconv.FormatInt(n, 10)
}

// speed says how fast it is going.
//
// Bytes carry their own unit. A count of things is per second, to one decimal
// while that decimal distinguishes anything - eight a second from nine - and
// whole above ten, where it is a digit that changes on every redraw and tells
// the reader nothing they can use.
func (p *Progress) speed(r float64) string {
	switch {
	case p.noun == "":
		return units.Size(int64(r)) + "/s"
	case r < 10:
		return fmt.Sprintf("%.1f/s", r)
	}
	return fmt.Sprintf("%.0f/s", r)
}

func (p *Progress) bar(width int, ratio float64) string {
	filled := int(ratio * float64(width))
	if filled > width {
		filled = width
	}
	return p.style.Paint(Accent, strings.Repeat(GlyphBarFilled, filled)) +
		p.style.Paint(Muted, strings.Repeat(GlyphBarPending, width-filled))
}

func withoutEmpty(parts []string) []string {
	out := parts[:0:0]
	for _, s := range parts {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}
