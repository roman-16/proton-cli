package kit

import (
	"testing"
	"time"

	"github.com/spf13/cobra"
)

// asked builds a command carrying the flags a person typed.
func asked(t *testing.T, first, last string) *DayRange {
	t.Helper()
	var d DayRange
	cmd := &cobra.Command{Use: "list"}
	d.Register(cmd)
	if first != "" {
		if err := cmd.Flags().Set("after", first); err != nil {
			t.Fatal(err)
		}
	}
	if last != "" {
		if err := cmd.Flags().Set("before", last); err != nil {
			t.Fatal(err)
		}
	}
	return &d
}

// A range is judgeable from the command line alone, so it is judged there: a wrong
// one must not first cost a sign-in to discover.
func TestDayRangeRefusesWhatCannotBeARange(t *testing.T) {
	for _, tc := range []struct{ name, first, last, want string }{
		{"a date that is not one", "yesterday", "", "--after expects YYYY-MM-DD."},
		{"a date with a time on it", "2026-08-14T09:00", "", "--after expects YYYY-MM-DD."},
		{"an end that is not a date", "2026-08-14", "soon", "--before expects YYYY-MM-DD."},
		{"a range that runs backwards", "2026-08-20", "2026-08-14", "--before is earlier than --after."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := asked(t, tc.first, tc.last).validate()
			if err == nil {
				t.Fatalf("%s was accepted", tc.name)
			}
			if err.Error() != tc.want {
				t.Errorf("error = %q, want %q", err.Error(), tc.want)
			}
		})
	}
}

func TestDayRangeAcceptsARangeAndASingleDay(t *testing.T) {
	for _, tc := range [][2]string{
		{"", ""},
		{"2026-08-14", ""},
		{"", "2026-08-14"},
		{"2026-08-14", "2026-08-14"},
		{"2026-08-14", "2026-08-20"},
	} {
		if err := asked(t, tc[0], tc[1]).validate(); err != nil {
			t.Errorf("--after %q --before %q was refused: %v", tc[0], tc[1], err)
		}
	}
}

// The end left out sits the default span away from the one given, so a day past
// the default range lists the days around it rather than nothing at all.
func TestDayRangeSlidesTheEndLeftOut(t *testing.T) {
	day := func(y int, m time.Month, d int) time.Time {
		return time.Date(y, m, d, 0, 0, 0, 0, time.Local)
	}
	defaultFirst, defaultLast := day(2026, 1, 1), day(2026, 1, 30)

	for _, tc := range []struct {
		name, first, last string
		wantFirst         time.Time
		wantLast          time.Time
	}{
		{"neither", "", "", defaultFirst, defaultLast},
		{"only --after", "2027-08-14", "", day(2027, 8, 14), day(2027, 9, 12)},
		{"only --before", "", "2025-08-20", day(2025, 7, 22), day(2025, 8, 20)},
		{"both", "2026-08-14", "2026-08-20", day(2026, 8, 14), day(2026, 8, 20)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			first, last := asked(t, tc.first, tc.last).Or(defaultFirst, defaultLast)
			if !first.Equal(tc.wantFirst) || !last.Equal(tc.wantLast) {
				t.Errorf("range = (%s, %s), want (%s, %s)", first, last, tc.wantFirst, tc.wantLast)
			}
		})
	}
}

// A range that names one end is never a range that cannot hold anything, which is
// what a fixed default end makes of a day beyond it: an empty answer to a
// question the reader never asked.
func TestDayRangeNamingOneEndAlwaysHoldsIt(t *testing.T) {
	now := time.Now()
	defaultFirst := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	defaultLast := defaultFirst.AddDate(0, 0, 29)

	for _, tc := range []struct{ name, first, last string }{
		{"an --after past the default end", defaultLast.AddDate(0, 0, 200).Format(dayLayout), ""},
		{"a --before before the default start", "", defaultFirst.AddDate(0, 0, -200).Format(dayLayout)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			first, last := asked(t, tc.first, tc.last).Or(defaultFirst, defaultLast)
			if last.Before(first) {
				t.Errorf("range = (%s, %s), which closes before it opens", first, last)
			}
		})
	}
}

// The days are the reader's own. Read as UTC they would name a window two hours off
// the rows it comes back with, in Vienna by two and in Auckland by twelve.
func TestDayRangeReadsTheDaysInTheReadersZone(t *testing.T) {
	loc, err := time.LoadLocation("Pacific/Auckland")
	if err != nil {
		t.Skipf("Pacific/Auckland is not available: %v", err)
	}
	saved := time.Local
	time.Local = loc
	t.Cleanup(func() { time.Local = saved })

	first, _ := asked(t, "2026-08-14", "2026-08-14").Or(time.Time{}, time.Time{})
	want := time.Date(2026, 8, 14, 0, 0, 0, 0, loc)
	if !first.Equal(want) {
		t.Errorf("--after 2026-08-14 = %s, want %s", first, want)
	}
}
