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

// Whichever end was left out keeps the default, so naming one day does not silently
// widen or narrow the other.
func TestDayRangeFallsBackForTheEndLeftOut(t *testing.T) {
	fallbackFirst := time.Date(2026, 1, 1, 0, 0, 0, 0, time.Local)
	fallbackLast := time.Date(2026, 1, 30, 0, 0, 0, 0, time.Local)

	first, last := asked(t, "", "").Or(fallbackFirst, fallbackLast)
	if !first.Equal(fallbackFirst) || !last.Equal(fallbackLast) {
		t.Errorf("with neither flag = (%s, %s), want the default", first, last)
	}

	first, last = asked(t, "2026-08-14", "").Or(fallbackFirst, fallbackLast)
	if !first.Equal(time.Date(2026, 8, 14, 0, 0, 0, 0, time.Local)) || !last.Equal(fallbackLast) {
		t.Errorf("with only --after = (%s, %s)", first, last)
	}

	first, last = asked(t, "", "2026-08-20").Or(fallbackFirst, fallbackLast)
	if !first.Equal(fallbackFirst) || !last.Equal(time.Date(2026, 8, 20, 0, 0, 0, 0, time.Local)) {
		t.Errorf("with only --before = (%s, %s)", first, last)
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
