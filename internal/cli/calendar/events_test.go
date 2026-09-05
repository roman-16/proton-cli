package calendar

import (
	"strings"
	"testing"
	"time"

	"github.com/roman-16/proton-cli/internal/cli/kit"
	"github.com/roman-16/proton-cli/internal/ical"
	calsvc "github.com/roman-16/proton-cli/internal/service/calendar"
	"github.com/spf13/cobra"
)

// A reference names an event and, for a recurring one, which occurrence. The
// occurrence half is only read as one when it is shaped like a time, so a title or
// an address containing an "@" is still a whole reference.
func TestSplitOccurrence(t *testing.T) {
	for _, tc := range []struct{ in, base, occ string }{
		{"cal/ev", "cal/ev", ""},
		{"cal/ev@2026-04-16T09:00", "cal/ev", "2026-04-16T09:00"},
		{"cal/ev@2026-04-16", "cal/ev", "2026-04-16"},
		{"cal/ev@2026-04-16T09:00:30", "cal/ev", "2026-04-16T09:00:30"},
		{"cal/ev@2026-04-16T09:00+02:00", "cal/ev", "2026-04-16T09:00+02:00"},
		{"4f2a1b9c@2026-04-16T09:00", "4f2a1b9c", "2026-04-16T09:00"},
		// A handle that merely contains an "@".
		{"standup@10", "standup@10", ""},
		{"jane@example.test", "jane@example.test", ""},
		{"Lunch @ Anna's", "Lunch @ Anna's", ""},
		{"cal/ev@notatime", "cal/ev@notatime", ""},
	} {
		base, occ := splitOccurrence(tc.in)
		if base != tc.base || occ != tc.occ {
			t.Errorf("splitOccurrence(%q) = %q, %q; want %q, %q", tc.in, base, occ, tc.base, tc.occ)
		}
	}
}

func TestEventRefIsWhatAListPrintsAndACommandReadsBack(t *testing.T) {
	one := calsvc.Event{CalendarID: "cal", ID: "ev"}
	if got := eventRef(one); got != "cal/ev" {
		t.Errorf("eventRef = %q", got)
	}
	occurrence := calsvc.Event{CalendarID: "cal", ID: "ev", Occurrence: "2026-04-16T09:00"}
	if got := eventRef(occurrence); got != "cal/ev@2026-04-16T09:00" {
		t.Errorf("eventRef = %q", got)
	}
	base, occ := splitOccurrence(eventRef(occurrence))
	if base != "cal/ev" || occ != "2026-04-16T09:00" {
		t.Errorf("a reference did not survive a round trip: %q, %q", base, occ)
	}
}

func TestOccurrenceLabelSaysWhereAnInstanceSits(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   calsvc.Event
		want string
	}{
		{"an instance of a bounded series", calsvc.Event{Number: 3, Count: count(10)}, "3 of 10"},
		{"an instance of an unbounded series", calsvc.Event{Number: 3}, "3 of a recurring series"},
		{"the series itself", calsvc.Event{RRule: "FREQ=WEEKLY;COUNT=12", Count: count(12)},
			"the whole series, 12 occurrences"},
		{"a series with no end", calsvc.Event{RRule: "FREQ=WEEKLY"}, "the whole series, which has no end"},
		{"a one-off event", calsvc.Event{}, ""},
	} {
		if got := occurrenceLabel(tc.in); got != tc.want {
			t.Errorf("%s: occurrenceLabel = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestSeriesLabelOnlyNamesASeriesFromOneOfItsOccurrences(t *testing.T) {
	if got := seriesLabel(calsvc.Event{CalendarID: "cal", ID: "ev"}); got != "" {
		t.Errorf("seriesLabel on a one-off = %q, want empty", got)
	}
	got := seriesLabel(calsvc.Event{CalendarID: "cal", ID: "ev", Occurrence: "2026-04-16T09:00"})
	if got != "cal/ev" {
		t.Errorf("seriesLabel = %q", got)
	}
}

// The clause is what a preview, a confirmation and a result all describe the
// scope with, so it has to say the same thing the reference and the flag asked
// for - including the case the reference says nothing about, where one word
// reaches every meeting a series will ever hold.
func TestTheReachClauseDescribesWhatTheReferenceAndTheFlagAskedFor(t *testing.T) {
	series := func(total *int) *reach { return &reach{series: true, total: total} }
	for _, tc := range []struct {
		name       string
		in         *reach
		occurrence string
		onwards    bool
		want       string
	}{
		{"a one-off event", &reach{total: count(1)}, "", false, ""},
		{"a single occurrence", series(count(10)), "2026-04-16T09:00", false, "on 2026-04-16T09:00"},
		{"an occurrence and its successors", series(count(10)), "2026-04-16T09:00", true,
			"from 2026-04-16T09:00 onwards"},
		{"a whole series", series(count(500)), "", false, "and all 500 occurrences of it"},
		{"a series with no end", series(nil), "", false, "and every occurrence of it, a series with no end"},
	} {
		if got := tc.in.clause(tc.occurrence, tc.onwards); got != tc.want {
			t.Errorf("%s: clause = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// A result says where a change put an event, in the zone it is anchored to,
// because the zone is the one part of a written time the command line does not
// state and nothing else would reveal in time.
//
// Every change that touches the times says so, including the one that names no
// time at all: an event losing its time of day has moved as surely as one given
// a new date, and it is the case where a silent result reads as nothing having
// happened.
func TestAMovedEventSaysWhenAndWhere(t *testing.T) {
	vienna, err := time.LoadLocation("Europe/Vienna")
	if err != nil {
		t.Skipf("Europe/Vienna is not available: %v", err)
	}
	meeting := &reach{rows: []calsvc.Event{{
		Start: time.Date(2026, 4, 16, 9, 0, 0, 0, vienna), Zone: "Europe/Vienna",
	}}}
	wholeDay := &reach{allDay: true, rows: []calsvc.Event{{
		Start: time.Date(2026, 4, 16, 0, 0, 0, 0, vienna), AllDay: true,
	}}}
	moment := ical.Timed(time.Date(2026, 4, 16, 10, 0, 0, 0, vienna), "Europe/Vienna")
	day := ical.Day(time.Date(2026, 4, 20, 0, 0, 0, 0, vienna))
	allDay, timed := true, false

	for _, tc := range []struct {
		name  string
		in    *reach
		patch calsvc.EventPatch
		want  string
	}{
		{"a change that leaves the times alone", meeting, calsvc.EventPatch{}, ""},
		{"a meeting given another time", meeting, calsvc.EventPatch{Start: &moment},
			"now 2026-04-16 10:00 Europe/Vienna"},
		{"a meeting moved to another day", meeting, calsvc.EventPatch{Start: &day},
			"now 2026-04-20 09:00 Europe/Vienna"},
		{"a meeting losing its time of day", meeting, calsvc.EventPatch{AllDay: &allDay},
			"now 2026-04-16 (all day)"},
		{"a whole day moved to another", wholeDay, calsvc.EventPatch{Start: &day},
			"now 2026-04-20 (all day)"},
		{"a whole day given a time of day", wholeDay,
			calsvc.EventPatch{Start: &moment, AllDay: &timed},
			"now 2026-04-16 10:00 Europe/Vienna"},
	} {
		if got := tc.in.moved(tc.patch); got != tc.want {
			t.Errorf("%s: moved = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// The length is on screen exactly when the command line did not state it, which
// is the only time it is news.
func TestOnlyALengthNobodyStatedIsSaidOutLoud(t *testing.T) {
	if got := lengthClause(30*time.Minute, true); got != "30m long" {
		t.Errorf("a length taken from the calendar reads %q", got)
	}
	if got := lengthClause(30*time.Minute, false); got != "" {
		t.Errorf("a length the command line stated reads %q, want nothing", got)
	}
}

func count(n int) *int { return &n }

// Whether an event has a time of day is settled by what was written, so every way
// of writing something that cannot be true at once is refused before a single
// request - and a person who wrote it is told which flag says the other thing.
func TestATimeOfDayIsJudgedBeforeAnythingIsAskedOfProton(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"a day with no time of day and nothing saying so",
			[]string{"--start", "2026-07-01"}, "names a day but not a time of day"},
		{"a day and --all-day agreeing", []string{"--start", "2026-07-01", "--all-day"}, ""},
		{"a time of day", []string{"--start", "2026-07-01T09:00"}, ""},
		{"a time of day and --all-day",
			[]string{"--start", "2026-07-01T09:00", "--all-day"}, "contradict each other"},
		{"a whole day measured in hours",
			[]string{"--start", "2026-07-01", "--all-day", "--duration", "90m"},
			"measured in days"},
		{"a whole day measured in days",
			[]string{"--start", "2026-07-01", "--all-day", "--duration", "3d"}, ""},
		{"a day running to a time of day",
			[]string{"--start", "2026-04-20", "--all-day", "--end", "2026-04-20T17:00"},
			"disagree on whether the event has a time of day"},
		{"a time of day running to a day",
			[]string{"--start", "2026-04-20T09:00", "--end", "2026-04-20"},
			"disagree on whether the event has a time of day"},
		{"a day running through another",
			[]string{"--start", "2026-04-20", "--all-day", "--end", "2026-04-22"}, ""},
		{"a length said twice",
			[]string{"--start", "2026-04-20T09:00", "--end", "2026-04-20T10:00", "--duration", "1h"},
			"both say when it ends"},
		{"a start nobody can read", []string{"--start", "whenever"}, "--start"},
	} {
		assertCheck(t, true, tc.name, tc.args, tc.want)
	}

	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"a time of day asked for and not named",
			[]string{"--all-day=false"}, "--start has to say which"},
		{"a time of day asked for and a day given",
			[]string{"--all-day=false", "--start", "2026-09-19"}, "--start has to say which"},
		{"a time of day asked for and named",
			[]string{"--all-day=false", "--start", "2026-09-19T13:00"}, ""},
		{"a time of day given without saying so",
			[]string{"--start", "2026-09-19T13:00"}, ""},
		{"a move to another day", []string{"--start", "2026-09-19"}, ""},
		{"a time of day taken away", []string{"--all-day"}, ""},
		{"a length with no beginning to hang off",
			[]string{"--duration", "1h"}, "needs --start"},
		{"a day measured in hours",
			[]string{"--start", "2026-09-19", "--duration", "90m"}, "measured in days"},
	} {
		assertCheck(t, false, tc.name, tc.args, tc.want)
	}
}

func assertCheck(t *testing.T, needsStart bool, name string, args []string, want string) {
	t.Helper()
	var d details
	c := &cobra.Command{Use: "events"}
	d.register(c, "Set")
	c.Flags().BoolVar(&d.allDay, "all-day", false, "")
	if err := c.ParseFlags(args); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	err := d.check(needsStart)(&kit.Invocation{Cmd: c})
	switch {
	case want == "" && err != nil:
		t.Errorf("%s was refused: %v", name, err)
	case want != "" && err == nil:
		t.Errorf("%s was accepted, want a refusal saying %q", name, want)
	case want != "" && err != nil && !strings.Contains(err.Error(), want):
		t.Errorf("%s was refused with %q, want it to say %q", name, err, want)
	}
}

// A whole-day row has no time of day, and it lasts a day rather than the hours a day
// happens to have. It is also printed on the date it carries: the service hands out
// times already read in the reader's zone, so a column that converted them again
// would move a whole-day event to the day before in every zone behind UTC.
func TestEventColumnsRenderAnAllDayRow(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("America/New_York: %v", err)
	}
	saved := time.Local
	time.Local = loc
	t.Cleanup(func() { time.Local = saved })

	day := calsvc.Event{
		CalendarID: "cal", ID: "ev", AllDay: true,
		Start: time.Date(2026, 4, 16, 0, 0, 0, 0, loc),
		End:   time.Date(2026, 4, 17, 0, 0, 0, 0, loc),
	}
	want := map[string]string{"DATE": "2026-04-16", "TIME": "all day", "DURATION": "1d"}
	for _, c := range eventColumns() {
		if w, ok := want[c.Header]; ok && c.Cell(day) != w {
			t.Errorf("%s on an all-day row = %q, want %q", c.Header, c.Cell(day), w)
		}
	}
}
