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
	// Build indexes what is not indexed yet, from wherever the last run stopped,
	// and reports through the sink as it goes.
	Build(ctx context.Context, sink progress.Sink) (Result, error)
	// Sync applies what has changed since the index was last brought up to date.
	Sync(ctx context.Context) (Result, error)
	// Status is what this app's index holds, or ErrNotIndexed.
	Status() (Status, error)
}

// Result is what one run of a part did.
type Result struct {
	// Indexed is how many things went in or were rewritten; Removed is how many
	// the account no longer has.
	Indexed, Removed int
	// Unreadable is how many of them went in without their contents, because
	// the contents would not open. They are counted here rather than read off
	// the index, so a run reports what it could not open rather than what every
	// run before it could not open either.
	Unreadable int
}

// Total adds up what a run over several parts did.
func Total(rs ...Result) Result {
	var out Result
	for _, r := range rs {
		out.Indexed += r.Indexed
		out.Removed += r.Removed
		out.Unreadable += r.Unreadable
	}
	return out
}

// Did reports whether anything happened, which is what decides between saying
// what was done and saying there was nothing to do.
func (r Result) Did() bool { return r.Indexed > 0 || r.Removed > 0 }
