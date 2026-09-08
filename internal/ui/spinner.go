package ui

import (
	"fmt"
	"io"
	"sync"
	"time"
)

// The sign of life.
//
// A command that has printed nothing tells the person watching it nothing
// either, and the two things they might conclude - it is working, it is stuck -
// look identical. Nearly every command here opens with a request over the
// network, which routinely outlasts the moment that doubt sets in, so the run
// says it is alive before it can say anything else.
//
// It says only that. There is no message, because at that point the run does
// not yet know anything worth reporting, and a frame carrying a claim about
// what it is doing would be a claim nobody checked. What it does say is when to
// stop looking at it: the frame is taken back by the first byte the run writes,
// so it is never on the screen beside an answer, a question or a transfer bar.

// patience is how long a run may say nothing before it says this.
const patience = 100 * time.Millisecond

// spinner is one run's sign of life, shared by every clone of its UI.
type spinner struct {
	w     io.Writer
	style Style
	done  chan struct{}

	mu    sync.Mutex
	frame int
	drawn bool
	over  bool
}

func newSpinner(w io.Writer, style Style) *spinner {
	return &spinner{w: w, style: style, done: make(chan struct{})}
}

// start begins drawing and returns the way to stop, which is also called by the
// stream wrappers the moment anything real is written.
func (s *spinner) start() func() {
	go s.animate()
	return s.stop
}

func (s *spinner) animate() {
	wait := time.NewTimer(patience)
	defer wait.Stop()
	select {
	case <-s.done:
		return
	case <-wait.C:
	}
	tick := time.NewTicker(redrawEvery)
	defer tick.Stop()
	for {
		s.draw()
		select {
		case <-s.done:
			return
		case <-tick.C:
		}
	}
}

func (s *spinner) draw() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.over {
		return
	}
	frames := []rune(GlyphWorking)
	_, _ = fmt.Fprint(s.w, "\r"+clearToEOL+s.style.Paint(Accent, string(frames[s.frame%len(frames)])))
	s.frame++
	s.drawn = true
}

// stop erases whatever is on the line and gives up the screen for good. It is
// called once per run in the ordinary case and once per stream wrapper in the
// racing one, so it holds the lock the drawing takes and refuses to say
// anything after the first caller.
func (s *spinner) stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.over {
		return
	}
	s.over = true
	close(s.done)
	if s.drawn {
		_, _ = fmt.Fprint(s.w, "\r"+clearToEOL)
		s.drawn = false
	}
}

// yielding is a stream that takes the sign of life back before anything real
// reaches it.
//
// Both streams are wrapped, because a frame drawn on the commentary stream is
// on the same screen as the answer: a table written to stdout has to clear it
// just as a warning written to stderr does.
type yielding struct {
	to io.Writer
	sp *spinner
}

func (y yielding) Write(p []byte) (int, error) {
	y.sp.stop()
	return y.to.Write(p)
}
