package search

import (
	"context"

	"github.com/roman-16/proton-cli/internal/progress"
)

// Part is one app's share of the index: the thing that knows what to fetch, how
// to decrypt it and what about it is worth keeping.
//
// The store knows none of that, and the commands know none of it either - which
// is what lets one set of verbs cover apps whose contents have nothing in common.
type Part interface {
	// App is which app this indexes.
	App() App
	// Noun is what the app's things are called, plural, as a listing says it.
	Noun() string
	// Count is how many there are to index, for a preview that promises a
	// number before any of the work is done.
	Count(ctx context.Context) (int, error)
	// Open reads the app's index, for a run that is about to work on it.
	Open(ctx context.Context) (Session, error)
}

// Session is one app's index, open.
//
// Opening it decrypts the log, which is the expensive part of every command that
// touches one: a search that caught up and then read what it had caught up with
// would pay for the whole file twice. So a run opens it once and does whatever
// it came to do to the same open copy - which is also what lets a watch poll a
// feed without reading its own index again every half-minute.
type Session interface {
	// Build indexes what is not indexed yet, and reports through the sink as it
	// goes. What that means is the app's own: a mailbox is walked for its
	// envelopes and then again for the bodies they are owed, a tree is walked
	// once.
	Build(ctx context.Context, sink progress.Sink) (Result, error)
	// Sync applies what has changed since the index was last brought up to date.
	Sync(ctx context.Context) (Result, error)
	// Status is what the index holds as this run has left it.
	Status() Status
}

// Result is what one run of a part did.
type Result struct {
	// Indexed is how many things the index took something in about that it did
	// not have; Removed is how many the account no longer has.
	Indexed, Removed int
	// Unreadable is how many of them went in without their contents, because
	// the contents would not open. They are counted here rather than read off
	// the index, so a run reports what it could not open rather than what every
	// run before it could not open either.
	Unreadable int
	// Refreshed says Proton could not describe what has happened, so the index
	// is owed a reading of the account itself. Nothing about it is lost - what
	// is in it is what it held - but what it is missing can only be found by
	// looking, which is a build rather than a catch-up.
	Refreshed bool
}

// Total adds up what a run over several parts did.
func Total(rs ...Result) Result {
	var out Result
	for _, r := range rs {
		out.Indexed += r.Indexed
		out.Removed += r.Removed
		out.Unreadable += r.Unreadable
		out.Refreshed = out.Refreshed || r.Refreshed
	}
	return out
}

// Did reports whether anything happened, which is what decides between saying
// what was done and saying there was nothing to do.
func (r Result) Did() bool { return r.Indexed > 0 || r.Removed > 0 }
