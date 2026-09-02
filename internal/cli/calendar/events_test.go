package calendar

import (
	"testing"
	"time"

	calsvc "github.com/roman-16/proton-cli/internal/service/calendar"
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
func TestAMovedEventSaysWhenAndWhere(t *testing.T) {
	at := time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC)
	r := &reach{}
	if got := r.moved(calsvc.EventPatch{}); got != "" {
		t.Errorf("an event that did not move reads %q", got)
	}
	zone := "Europe/Vienna"
	got := r.moved(calsvc.EventPatch{Start: &at, Zone: &zone})
	if want := "now 2026-04-16 10:00 Europe/Vienna"; got != want {
		t.Errorf("moved = %q, want %q", got, want)
	}
	allDay := true
	got = r.moved(calsvc.EventPatch{Start: &at, Zone: &zone, AllDay: &allDay})
	if want := "now 2026-04-16 (all day)"; got != want {
		t.Errorf("an all-day event reads %q, want %q", got, want)
	}
}

func count(n int) *int { return &n }

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
