package ical

import (
	"testing"
	"time"
)

// newYork is a zone behind UTC, which is where reading an all-day date as an
// instant puts a whole-day event on the wrong day.
func newYork(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("America/New_York: %v", err)
	}
	return loc
}

func date(loc *time.Location, y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, loc)
}

func timed(loc *time.Location, y int, m time.Month, d, h, min int) DateTime {
	return Timed(time.Date(y, m, d, h, min, 0, 0, loc), loc.String())
}

// One day asked for is one day answered. An all-day event begins at the midnight
// the window ends on, so a range that included its last instant would report the
// day after as well - and an all-day row carries no time of day to give it away.
func TestWindowExcludesTheDayAfterTheLast(t *testing.T) {
	loc := vienna(t)
	w := Days(date(loc, 2026, time.August, 14), date(loc, 2026, time.August, 14))

	day := Day(time.Date(2026, time.August, 14, 0, 0, 0, 0, time.UTC))
	if !w.Covers(day, day.At(day.Time.AddDate(0, 0, 1))) {
		t.Error("the day asked for is not covered")
	}
	next := Day(time.Date(2026, time.August, 15, 0, 0, 0, 0, time.UTC))
	if w.Covers(next, next.At(next.Time.AddDate(0, 0, 1))) {
		t.Error("the day after the last one is covered")
	}
	if !w.Ended(next) {
		t.Error("an occurrence on the day after the last one did not end the window")
	}
}

// An all-day date names a day, not an instant. Read as an instant it lands on the
// previous day in every zone behind UTC, which would report the wrong day and hide
// the right one.
func TestWindowPlacesAnAllDayEventByItsDateBehindUTC(t *testing.T) {
	loc := newYork(t)
	w := Days(date(loc, 2026, time.August, 14), date(loc, 2026, time.August, 14))

	for _, tc := range []struct {
		day  int
		want bool
	}{{13, false}, {14, true}, {15, false}} {
		start := Day(time.Date(2026, time.August, tc.day, 0, 0, 0, 0, time.UTC))
		end := start.At(start.Time.AddDate(0, 0, 1))
		if got := w.Covers(start, end); got != tc.want {
			t.Errorf("the all-day event on the %dth: covered=%v, want %v", tc.day, got, tc.want)
		}
	}
}

// The days are the reader's own, so an event in the first or last hour of one is on
// it. Read as UTC days, a window in Vienna misses the small hours of its first day
// and reaches into the day after its last.
func TestWindowIsReadInItsOwnZone(t *testing.T) {
	loc := vienna(t)
	w := Days(date(loc, 2026, time.August, 14), date(loc, 2026, time.August, 14))

	if !w.Covers(timed(loc, 2026, time.August, 14, 0, 30), timed(loc, 2026, time.August, 14, 1, 0)) {
		t.Error("00:30 on the day asked for is not covered")
	}
	if w.Covers(timed(loc, 2026, time.August, 15, 1, 0), timed(loc, 2026, time.August, 15, 2, 0)) {
		t.Error("01:00 on the day after the last one is covered")
	}
}

// Overlap, not containment: what a caller wants from a query about one day is
// everything that is on it, including what began before it.
func TestWindowCoversAnEventThatSpansIt(t *testing.T) {
	loc := vienna(t)
	w := Days(date(loc, 2026, time.August, 14), date(loc, 2026, time.August, 14))
	if !w.Covers(timed(loc, 2026, time.August, 12, 9, 0), timed(loc, 2026, time.August, 16, 9, 0)) {
		t.Error("an event spanning the window is not covered")
	}
}

// An event that ends as the window opens is not on any of its days. Proton's own
// client makes the same exception, so that yesterday's 23:00 to 00:00 does not
// appear at the top of today.
func TestWindowExcludesAnEventEndingAsItOpens(t *testing.T) {
	loc := vienna(t)
	w := Days(date(loc, 2026, time.August, 14), date(loc, 2026, time.August, 14))
	if w.Covers(timed(loc, 2026, time.August, 13, 23, 0), timed(loc, 2026, time.August, 14, 0, 0)) {
		t.Error("an event ending as the window opens is covered")
	}
}

// An event with no length is the instant it names.
func TestWindowCoversAnEventWithNoLength(t *testing.T) {
	loc := vienna(t)
	w := Days(date(loc, 2026, time.August, 14), date(loc, 2026, time.August, 14))
	at := timed(loc, 2026, time.August, 14, 9, 0)
	if !w.Covers(at, at) {
		t.Error("an event with no length inside the window is not covered")
	}
	outside := timed(loc, 2026, time.August, 15, 9, 0)
	if w.Covers(outside, outside) {
		t.Error("an event with no length outside the window is covered")
	}
}

// Proton stores an all-day event's end exclusively, and clients differ on whether
// they write one at all. Every shape names the same day, so every shape is placed
// on it.
func TestWindowReadsEveryShapeOfAllDayEnd(t *testing.T) {
	loc := vienna(t)
	the14th := Days(date(loc, 2026, time.August, 14), date(loc, 2026, time.August, 14))
	the15th := Days(date(loc, 2026, time.August, 15), date(loc, 2026, time.August, 15))
	start := Day(time.Date(2026, time.August, 14, 0, 0, 0, 0, time.UTC))

	for name, end := range map[string]DateTime{
		"no end":        {},
		"end at start":  start,
		"exclusive end": start.At(start.Time.AddDate(0, 0, 1)),
	} {
		if !the14th.Covers(start, end) {
			t.Errorf("an all-day event written with %s is not on its own day", name)
		}
		if the15th.Covers(start, end) {
			t.Errorf("an all-day event written with %s reaches into the next day", name)
		}
	}
}

func TestDaysFromCountsTheDaysItIsGiven(t *testing.T) {
	loc := vienna(t)
	first := date(loc, 2026, time.August, 14)
	w := DaysFrom(first, 30)
	from, until := w.Bounds()
	if !from.Equal(first) {
		t.Errorf("the window opens at %s, want %s", from, first)
	}
	if want := first.AddDate(0, 0, 30); !until.Equal(want) {
		t.Errorf("30 days from %s ends at %s, want %s", first, until, want)
	}
}

// The window is a range of days, so the time of day it is built from is not part of
// it: an afternoon and the midnight before it name the same day.
func TestDaysIgnoresTheTimeOfDayItIsBuiltFrom(t *testing.T) {
	loc := vienna(t)
	whole := Days(date(loc, 2026, time.August, 14), date(loc, 2026, time.August, 16))
	from, until := Days(
		time.Date(2026, time.August, 14, 16, 30, 0, 0, loc),
		time.Date(2026, time.August, 16, 9, 15, 0, 0, loc),
	).Bounds()
	wantFrom, wantUntil := whole.Bounds()
	if !from.Equal(wantFrom) || !until.Equal(wantUntil) {
		t.Errorf("window = [%s, %s), want [%s, %s)", from, until, wantFrom, wantUntil)
	}
}
