// Package progress defines the contract between long-running domain work and
// whatever reports it. Services take a Sink so they never depend on the
// presentation layer; the ui package supplies the implementation that draws a
// bar, and Nop covers every other caller.
package progress

// Sink receives byte-transfer progress for one operation.
//
// Start is called once before any Add, Done once after the last one. An
// implementation must tolerate a zero total (unknown size) and a running count
// that exceeds total, which happens whenever encryption adds per-block
// overhead to the source size.
type Sink interface {
	Start(total int64, label string)
	Add(n int64)
	Done()
}

// Resumer is a sink that can be told what was done before this run started.
type Resumer interface{ Resume(done int64) }

// Resume says that this much of the work was already done when the run began.
//
// It is not the same as reporting it: a build that carries on from twelve
// thousand of forty-eight has covered a quarter of the whole, and none of that
// quarter happened just now. Counted as work, it would be a burst of twelve
// thousand in an instant, and every rate and estimate for the next half-minute
// would be drawn from it.
func Resume(s Sink, done int64) {
	if r, ok := s.(Resumer); ok {
		r.Resume(done)
	}
}

// Nop discards everything. It is the zero-value Sink, so a nil-safe caller can
// use Of to avoid branching.
type Nop struct{}

func (Nop) Start(int64, string) {}
func (Nop) Add(int64)           {}
func (Nop) Done()               {}
func (Nop) Resume(int64)        {}

// Of returns s, or a Nop when s is nil, so callers never nil-check.
func Of(s Sink) Sink {
	if s == nil {
		return Nop{}
	}
	return s
}
