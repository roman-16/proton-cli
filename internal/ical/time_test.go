package ical

import (
	"strings"
	"testing"
	"time"
)

func TestDateTimeRendersItsOwnAnchor(t *testing.T) {
	loc := vienna(t)
	at := time.Date(2026, 4, 16, 9, 0, 0, 0, loc)

	zoned := Timed(at, "Europe/Vienna").line("DTSTART")
	if got := zoned.String(); got != "DTSTART;TZID=Europe/Vienna:20260416T090000" {
		t.Errorf("zoned = %q", got)
	}

	utc := Timed(at, "").line("DTSTART")
	if got := utc.String(); got != "DTSTART:20260416T070000Z" {
		t.Errorf("UTC = %q", got)
	}

	allDay := Day(at).line("DTSTART")
	if got := allDay.String(); got != "DTSTART;VALUE=DATE:20260416" {
		t.Errorf("all-day = %q", got)
	}
}

// The reference form is what a list prints beside its local columns and what a
// person types back off that row, so it is the local reading rather than the
// value's own anchor. Here the two agree because the reading is declared to be
// from Vienna, which is where the series is anchored.
func TestStringIsTheLocalReadingOfTheInstant(t *testing.T) {
	loc := vienna(t)
	readingIn(t, loc)
	at := time.Date(2026, 4, 16, 9, 0, 0, 0, loc)
	if got := Timed(at, "Europe/Vienna").String(); got != "2026-04-16T09:00" {
		t.Errorf("String = %q", got)
	}
	if got := Day(at).String(); got != "2026-04-16" {
		t.Errorf("all-day String = %q", got)
	}
}

// A date-time names one instant, whoever reads it. An all-day date names a day, and
// a day begins when the reader's day begins: read as an instant it slips to the day
// before in every zone behind UTC.
func TestInReadsAnAllDayDateAsTheReadersOwnDay(t *testing.T) {
	newYork, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("America/New_York: %v", err)
	}
	day := Day(time.Date(2026, 8, 14, 0, 0, 0, 0, time.UTC))
	if got := day.In(newYork); got.Format("2006-01-02 15:04") != "2026-08-14 00:00" {
		t.Errorf("the day read in New York = %s", got)
	}

	loc := vienna(t)
	at := time.Date(2026, 8, 14, 9, 0, 0, 0, loc)
	timed := Timed(at, "Europe/Vienna")
	if got := timed.In(newYork); !got.Equal(at) {
		t.Errorf("reading a date-time in another zone moved it: %s", got)
	}
	if got := timed.In(newYork).Format("15:04"); got != "03:00" {
		t.Errorf("09:00 in Vienna reads as %s in New York, want 03:00", got)
	}
}

func TestUntilValueIsTheLastSecondOfThePreviousDay(t *testing.T) {
	loc := vienna(t)
	// 26 October 2026 is after the clocks go back, so the previous day ends at
	// 22:59:59 UTC rather than 21:59:59.
	occ := Timed(time.Date(2026, 10, 26, 9, 0, 0, 0, loc), "Europe/Vienna")
	if got := UntilValue(occ); got != "20261025T225959Z" {
		t.Errorf("UntilValue = %q", got)
	}
}

func TestUntilValueOfAnAllDaySeriesIsAFloatingDate(t *testing.T) {
	occ := Day(time.Date(2026, 4, 16, 0, 0, 0, 0, time.UTC))
	if got := UntilValue(occ); got != "20260415" {
		t.Errorf("UntilValue = %q", got)
	}
}

func TestLocationFallsBackToUTCForAZoneThisHostDoesNotKnow(t *testing.T) {
	// An event anchored to a zone this build has never heard of is still readable;
	// refusing it outright would hide a real event over a name.
	d := DateTime{Time: time.Now(), TZID: "Mars/Olympus_Mons"}
	if d.Location() != time.UTC {
		t.Errorf("Location = %v, want UTC", d.Location())
	}
}

func TestParseTimeReadsBareValuesInTheGivenZone(t *testing.T) {
	loc := vienna(t)
	got, err := ParseTime("2026-04-16T09:00", loc)
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, 4, 16, 9, 0, 0, 0, loc); !got.Time.Equal(want) {
		t.Errorf("ParseTime = %v, want %v", got.Time, want)
	}
	if _, err := ParseTime("not a time", loc); err == nil {
		t.Error("ParseTime accepted a value that is not a time")
	}
}

// A day and a time of day are different things to have written, and the
// difference is the whole difference between an all-day event and one that
// begins at midnight. A reader that only kept the instant could not tell them
// apart, since both name the same one.
func TestParseTimeKeepsWhetherATimeOfDayWasWritten(t *testing.T) {
	loc := vienna(t)
	for _, tc := range []struct {
		in     string
		allDay bool
	}{
		{"2026-04-16", true},
		{"2026-04-16T00:00", false},
		{"2026-04-16T09:00", false},
		{"2026-04-16 09:00", false},
		{"2026-04-16T09:00:00+02:00", false},
	} {
		got, err := ParseTime(tc.in, loc)
		if err != nil {
			t.Fatalf("%s: %v", tc.in, err)
		}
		if got.AllDay != tc.allDay {
			t.Errorf("%s reads as all-day=%v, want %v", tc.in, got.AllDay, tc.allDay)
		}
	}
	// A day names the day it was written as, in whatever zone reads it back.
	day, err := ParseTime("2026-04-16", loc)
	if err != nil {
		t.Fatal(err)
	}
	if got := day.In(time.UTC).Format("2006-01-02"); got != "2026-04-16" {
		t.Errorf("a day read in UTC is %s", got)
	}
}

// Which zone a written time is anchored to is the caller's to say: the same
// reading means a different instant in each, and the frame an event is stored
// against is the account's, not the parser's.
func TestAnchoredGivesAZoneToWhatHasATimeOfDay(t *testing.T) {
	loc := vienna(t)
	timed, err := ParseTime("2026-04-16T09:00", loc)
	if err != nil {
		t.Fatal(err)
	}
	if got := timed.Anchored("Europe/Vienna"); got.TZID != "Europe/Vienna" {
		t.Errorf("Anchored left the value written against %q", got.TZID)
	}
	day, err := ParseTime("2026-04-16", loc)
	if err != nil {
		t.Fatal(err)
	}
	if got := day.Anchored("Europe/Vienna"); got.TZID != "" {
		t.Errorf("a day was anchored to %q, and a day has no time of day to anchor", got.TZID)
	}
}

// A reference is printed beside local columns and typed back by a person reading
// them, so it is rendered and read local whatever the series is anchored to.
// Matching is by the instant, so the reference still names the right occurrence.
func TestOccurrenceReferencesRoundTripAcrossAnchors(t *testing.T) {
	loc := vienna(t)
	at := time.Date(2026, 8, 31, 10, 0, 0, 0, loc)

	utcAnchored := VEvent{UID: "u", Start: Timed(at, ""), End: Timed(at.Add(time.Hour), "")}
	zoneAnchored := VEvent{UID: "u", Start: Timed(at, "Europe/Vienna"), End: Timed(at.Add(time.Hour), "Europe/Vienna")}

	for _, v := range []VEvent{utcAnchored, zoneAnchored} {
		printed := v.Start.String()
		if printed != at.Local().Format(refTimeLayout) {
			t.Errorf("a reference reads %q, want the local reading %q", printed, at.Local().Format(refTimeLayout))
		}
		back, err := v.ParseOccurrence(printed)
		if err != nil {
			t.Fatalf("ParseOccurrence(%q): %v", printed, err)
		}
		if !back.Equal(v.Start) {
			t.Errorf("a reference did not name the instant it was printed from: %v vs %v", back.Time, v.Start.Time)
		}
	}
}

// The clocks going forward leave an hour that no wall clock ever reads. Asking
// for a time inside it used to yield the hour after, so an event written for
// 02:30 was stored at 03:30 and nothing said so.
func TestATimeInsideASkippedHourIsRefused(t *testing.T) {
	loc := vienna(t)
	_, err := ParseTime("2026-03-29T02:30", loc)
	if err == nil {
		t.Fatal("02:30 does not exist that day, and was accepted")
	}
	for _, want := range []string{"does not exist", "2026-03-29", "Europe/Vienna", "03:30"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

// The clocks going back read the same hour twice, and only an offset can say
// which of the two was meant - so the message offers both, spelled the way the
// answer has to be written.
func TestATimeInsideARepeatedHourIsRefused(t *testing.T) {
	loc := vienna(t)
	_, err := ParseTime("2026-10-25T02:30", loc)
	if err == nil {
		t.Fatal("02:30 happens twice that day, and was accepted")
	}
	for _, want := range []string{"happens twice", "2026-10-25T02:30:00+02:00", "2026-10-25T02:30:00+01:00"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

// An offset names one instant outright, which is what makes it the answer to
// both refusals above rather than a second way of saying the same thing.
func TestAnOffsetSettlesAnAmbiguousReading(t *testing.T) {
	loc := vienna(t)
	for _, tc := range []struct{ in, want string }{
		{"2026-10-25T02:30:00+02:00", "2026-10-25T00:30:00Z"},
		{"2026-10-25T02:30:00+01:00", "2026-10-25T01:30:00Z"},
		{"2026-03-29T02:30:00+01:00", "2026-03-29T01:30:00Z"},
	} {
		got, err := ParseTime(tc.in, loc)
		if err != nil {
			t.Fatalf("%s: %v", tc.in, err)
		}
		if utc := got.Time.UTC().Format(time.RFC3339); utc != tc.want {
			t.Errorf("%s = %s, want %s", tc.in, utc, tc.want)
		}
	}
}

// Every ordinary reading still parses, in each form somebody writes one.
func TestAnUnambiguousReadingIsAccepted(t *testing.T) {
	loc := vienna(t)
	for _, in := range []string{
		"2026-04-16T14:00", "2026-04-16T14:00:30", "2026-04-16 14:00", "2026-04-16",
		"2026-04-16T14:00:00+02:00",
	} {
		if _, err := ParseTime(in, loc); err != nil {
			t.Errorf("%s: %v", in, err)
		}
	}
}

// A reference this CLI printed has to keep resolving. The occurrence behind a
// repeated 02:30 is settled by the series that generated it, which knows which
// instant it meant, so refusing to read the string back would leave that
// occurrence with no way to address it.
func TestAPrintedReferenceIsReadBackWithoutJudgement(t *testing.T) {
	loc := vienna(t)
	for _, in := range []string{"2026-10-25T02:30", "2026-03-29T02:30"} {
		if _, err := ParseWallTime(in, loc); err != nil {
			t.Errorf("%s: %v", in, err)
		}
	}
}

// Where the clocks move by less than an hour, an overlap is shorter than an
// hour too. Looking exactly an hour away for the twin would step straight over
// Lord Howe Island's.
func TestAHalfHourOverlapIsFound(t *testing.T) {
	loc, err := time.LoadLocation("Australia/Lord_Howe")
	if err != nil {
		t.Fatalf("Australia/Lord_Howe: %v", err)
	}
	if _, err := ParseTime("2026-04-05T01:45", loc); err == nil {
		t.Error("01:45 happens twice that day on Lord Howe Island, and was accepted")
	}
}
