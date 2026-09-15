package kit

import (
	"math"
	"time"

	"github.com/spf13/cobra"
)

// Declarative day-range flags.
//
// A listing over a range of days judges two things nobody needs a session to
// judge: that each date is a date, and that the range runs forwards. Declaring the
// pair once is what has Run refuse both of those before the first request, and
// keeps every listing that takes a range taking the same one - a mail listing and
// a calendar listing are the same question about two collections.
//
// The pair is --after and --before, which is what narrows a selection everywhere
// in this CLI. --start and --end describe the thing being written - an event's
// own beginning, an auto-reply's window - and a name that meant both would be a
// name meaning two things: `events list --start` and `events create --start` ask
// about different days.

// dayLayout is the only form a day is written in, given or printed.
const dayLayout = "2006-01-02"

// AfterUsage and BeforeUsage are what the pair says for itself, wherever it is
// registered.
const (
	AfterUsage  = "First day to include (YYYY-MM-DD)"
	BeforeUsage = "Last day to include (YYYY-MM-DD)"
)

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
	cmd.Flags().StringVar(&d.first, "after", "", AfterUsage)
	cmd.Flags().StringVar(&d.last, "before", "", BeforeUsage)
	registerCheck(cmd, "after", nil, d)
}

// Set reports whether either end was named, which is what tells a narrowed
// listing from a whole one.
func (d *DayRange) Set() bool { return d.first != "" || d.last != "" }

// Days are the two days as given, with the zero time for an end left out. It is
// what a listing reads when it has no range of its own to fall back on: mail is
// bounded by the folder rather than by a window.
func (d *DayRange) Days() (first, last time.Time) {
	first, _ = parseDay(d.first)
	last, _ = parseDay(d.last)
	return first, last
}

// Or returns the range asked for, over a listing whose own range is the given
// days. Run has already validated the flags by the time a body can call it.
//
// An end nobody named never contradicts one they did. Given one end alone, the
// other sits the default span away from it, so a day past the end of the default
// range answers with the days around it - the window it stands in - rather than
// with a range that closes before it opens and an empty answer that reads as
// "nothing there".
func (d *DayRange) Or(first, last time.Time) (time.Time, time.Time) {
	span := spanDays(first, last)
	asked, askedLast := d.Days()
	switch {
	case !asked.IsZero() && !askedLast.IsZero():
		return asked, askedLast
	case !asked.IsZero():
		return asked, asked.AddDate(0, 0, span)
	case !askedLast.IsZero():
		return askedLast.AddDate(0, 0, -span), askedLast
	}
	return first, last
}

// spanDays is how many whole days a range covers, counted as days rather than as
// hours so that the clocks changing inside it does not shorten it.
func spanDays(first, last time.Time) int {
	return int(math.Round(last.Sub(first).Hours() / 24))
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
