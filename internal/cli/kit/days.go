package kit

import (
	"time"

	"github.com/spf13/cobra"
)

// Declarative day-range flags.
//
// A listing over a range of days judges two things nobody needs a session to
// judge: that each date is a date, and that the range runs forwards. Declaring the
// pair once is what has Run refuse both of those before the first request, and
// keeps every listing that takes a range taking the same one.
//
// The pair is --after and --before, which is what narrows a selection everywhere
// in this CLI. --start and --end describe the thing being written - an event's
// own beginning, an auto-reply's window - and a name that meant both would be a
// name meaning two things: `events list --start` and `events create --start` ask
// about different days.

// dayLayout is the only form a day is written in, given or printed.
const dayLayout = "2006-01-02"

// DayRange is the pair of flags naming the first and last whole day of a range.
//
// Whole days, read in the reader's own zone, because a date is what a command line
// can say and the zone is the one the rows come back dated in. Both named days
// are included: a range whose ends mean different things is a range somebody
// gets wrong once a year.
type DayRange struct {
	first, last string
}

// Register binds --after and --before to cmd.
func (d *DayRange) Register(cmd *cobra.Command) {
	cmd.Flags().StringVar(&d.first, "after", "", "First day to include (YYYY-MM-DD)")
	cmd.Flags().StringVar(&d.last, "before", "", "Last day to include (YYYY-MM-DD)")
	registerCheck(cmd, "after", nil, d)
}

// Or returns the range asked for, falling back to the given days for whichever end
// was left out. Run has already validated the flags by the time a body can call it.
func (d *DayRange) Or(first, last time.Time) (time.Time, time.Time) {
	if t, err := parseDay(d.first); err == nil && !t.IsZero() {
		first = t
	}
	if t, err := parseDay(d.last); err == nil && !t.IsZero() {
		last = t
	}
	return first, last
}

func (d *DayRange) validate() error {
	first, err := parseDay(d.first)
	if err != nil {
		return Fail("--after expects YYYY-MM-DD.")
	}
	last, err := parseDay(d.last)
	if err != nil {
		return Fail("--before expects YYYY-MM-DD.")
	}
	if !first.IsZero() && !last.IsZero() && last.Before(first) {
		return Fail("--before is earlier than --after.")
	}
	return nil
}

// parseDay reads a day in the zone the listing is read in. An absent day is the
// zero time, which is what lets a caller supply its own default.
func parseDay(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	return time.ParseInLocation(dayLayout, s, time.Local)
}
