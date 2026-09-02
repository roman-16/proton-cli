package ical

import (
	"errors"
	"testing"
	"time"
)

func vienna(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Vienna")
	if err != nil {
		t.Fatalf("Europe/Vienna: %v", err)
	}
	return loc
}

// readingIn fixes the zone a test reads references in.
func readingIn(t *testing.T, loc *time.Location) {
	t.Helper()
	saved := time.Local
	time.Local = loc
	t.Cleanup(func() { time.Local = saved })
}

// weekly builds a zone-anchored series at 09:00 lasting 15 minutes, read from
// that same zone.
//
// The reading matters as much as the series. A reference is the local reading of
// an instant, so a test that writes one as a literal is writing it as read from
// somewhere; left to the machine, that somewhere is whoever runs the test.
func weekly(t *testing.T, rule string, day int) VEvent {
	t.Helper()
	loc := vienna(t)
	readingIn(t, loc)
	start := time.Date(2026, 10, day, 9, 0, 0, 0, loc)
	return VEvent{
		UID:     "uid-1",
		Summary: "Standup",
		Start:   Timed(start, "Europe/Vienna"),
		End:     Timed(start.Add(15*time.Minute), "Europe/Vienna"),
		RRule:   rule,
	}
}

// A series anchored to a zone keeps its wall-clock time across a daylight-saving
// change. Stored as a UTC instant it would slide by an hour, which is the whole
// reason an event carries a zone rather than only a moment.
func TestOccurrencesKeepTheWallClockAcrossADaylightSavingChange(t *testing.T) {
	loc := vienna(t)
	v := weekly(t, "FREQ=WEEKLY;COUNT=4", 12)
	occs, err := v.Occurrences(Days(date(loc, 2026, time.October, 1), date(loc, 2026, time.November, 30)))
	if err != nil {
		t.Fatal(err)
	}
	if len(occs) != 4 {
		t.Fatalf("got %d occurrences, want 4", len(occs))
	}
	for i, o := range occs {
		if got := o.Start.Wall().Format("15:04"); got != "09:00" {
			t.Errorf("occurrence %d starts at %s, want 09:00", i+1, got)
		}
		if o.Number != i+1 {
			t.Errorf("occurrence %d is numbered %d", i+1, o.Number)
		}
		if o.End.Time.Sub(o.Start.Time) != 15*time.Minute {
			t.Errorf("occurrence %d lasts %v", i+1, o.End.Time.Sub(o.Start.Time))
		}
	}
	// The clocks go back on 25 October 2026, so the fourth occurrence is an hour
	// further from the first in absolute terms than three weeks.
	if _, offsetFirst := occs[0].Start.Time.Zone(); true {
		_, offsetLast := occs[3].Start.Time.Zone()
		if offsetFirst == offsetLast {
			t.Skip("this zone does not change offset in the tested window")
		}
	}
}

func TestOccurrencesIncludeAnEventThatReachesIntoTheWindow(t *testing.T) {
	loc := vienna(t)
	start := time.Date(2026, 4, 16, 9, 0, 0, 0, loc)
	v := VEvent{
		UID: "u", Summary: "Conference",
		Start: Timed(start, "Europe/Vienna"),
		End:   Timed(start.Add(72*time.Hour), "Europe/Vienna"),
	}
	// The window opens after it started and closes before it ends.
	occs, err := v.Occurrences(Days(date(loc, 2026, time.April, 17), date(loc, 2026, time.April, 17)))
	if err != nil {
		t.Fatal(err)
	}
	if len(occs) != 1 {
		t.Fatalf("an event spanning the window was not reported: %+v", occs)
	}
}

func TestOccurrencesSkipExclusionsButNotTheirNumbers(t *testing.T) {
	// COUNT counts what the rule generates, so an excluded date still takes its
	// place: truncating a counted rule depends on that.
	loc := vienna(t)
	v := weekly(t, "FREQ=WEEKLY;COUNT=4", 12)
	v.ExDates = []DateTime{Timed(time.Date(2026, 10, 19, 9, 0, 0, 0, loc), "Europe/Vienna")}
	occs, err := v.Occurrences(Days(date(loc, 2026, time.October, 1), date(loc, 2026, time.November, 30)))
	if err != nil {
		t.Fatal(err)
	}
	if len(occs) != 3 {
		t.Fatalf("got %d occurrences, want 3 with one excluded", len(occs))
	}
	if occs[1].Number != 3 {
		t.Errorf("the occurrence after the excluded one is numbered %d, want 3", occs[1].Number)
	}
}

func TestOccurrencesReadsOrdinalBydayRules(t *testing.T) {
	loc := vienna(t)
	v := weekly(t, "FREQ=MONTHLY;BYDAY=2MO;COUNT=3", 12)
	occs, err := v.Occurrences(Days(date(loc, 2026, time.January, 1), date(loc, 2027, time.June, 1)))
	if err != nil {
		t.Fatal(err)
	}
	if len(occs) != 3 {
		t.Fatalf("got %d occurrences, want 3", len(occs))
	}
	for _, o := range occs {
		w := o.Start.Wall()
		if w.Weekday() != time.Monday || w.Day() > 14 || w.Day() < 8 {
			t.Errorf("%s is not the second Monday of its month", w.Format("2006-01-02"))
		}
	}
}

func TestOccurrencesOfAnAllDaySeries(t *testing.T) {
	loc := vienna(t)
	day := time.Date(2026, 4, 16, 0, 0, 0, 0, time.UTC)
	v := VEvent{
		UID: "u", Start: Day(day), End: Day(day.AddDate(0, 0, 1)),
		RRule: "FREQ=DAILY;COUNT=3",
	}
	occs, err := v.Occurrences(Days(date(loc, 2026, time.April, 16), date(loc, 2026, time.April, 26)))
	if err != nil {
		t.Fatal(err)
	}
	if len(occs) != 3 {
		t.Fatalf("got %d occurrences, want 3", len(occs))
	}
	if !occs[0].Start.AllDay || occs[0].Start.String() != "2026-04-16" {
		t.Errorf("first occurrence = %+v", occs[0].Start)
	}
	if occs[2].Start.String() != "2026-04-18" {
		t.Errorf("third occurrence = %s", occs[2].Start.String())
	}
}

// A day asked for is a day answered, even where the next occurrence begins at the
// instant the day ends. Only an all-day occurrence can fall exactly on that
// boundary, and it carries no time of day to give itself away, so a range that
// included it would quietly report two days of events for a one-day question.
func TestOccurrencesOfAnAllDaySeriesStopAtTheLastDay(t *testing.T) {
	loc := vienna(t)
	start := time.Date(2026, 8, 14, 0, 0, 0, 0, time.UTC)
	v := VEvent{UID: "u", Start: Day(start), RRule: "FREQ=DAILY;COUNT=5"}

	occ, err := v.Occurrences(Days(date(loc, 2026, time.August, 14), date(loc, 2026, time.August, 14)))
	if err != nil {
		t.Fatal(err)
	}
	if len(occ) != 1 {
		t.Fatalf("a one-day window reported %d occurrences, want 1", len(occ))
	}
	if got := occ[0].Start.String(); got != "2026-08-14" {
		t.Errorf("the occurrence reported is %s, want the day asked for", got)
	}
	// An all-day event's end is the midnight after its last day, whether or not the
	// series was written with one.
	if got := occ[0].End.String(); got != "2026-08-15" {
		t.Errorf("the occurrence ends %s, want the midnight after its day", got)
	}

	two, err := v.Occurrences(Days(date(loc, 2026, time.August, 14), date(loc, 2026, time.August, 15)))
	if err != nil {
		t.Fatal(err)
	}
	if len(two) != 2 {
		t.Errorf("a two-day window reported %d occurrences, want 2", len(two))
	}
}

func TestOccurrenceAtFindsAnInstanceAndItsNumber(t *testing.T) {
	v := weekly(t, "FREQ=WEEKLY;COUNT=8", 12)
	at, err := v.ParseOccurrence("2026-10-26T09:00")
	if err != nil {
		t.Fatal(err)
	}
	occ, ok, err := v.OccurrenceAt(at)
	if err != nil || !ok {
		t.Fatalf("OccurrenceAt: ok=%v err=%v", ok, err)
	}
	if occ.Number != 3 {
		t.Errorf("occurrence number = %d, want 3", occ.Number)
	}

	missing, err := v.ParseOccurrence("2026-10-27T09:00")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := v.OccurrenceAt(missing); ok {
		t.Error("OccurrenceAt found an instance the rule does not generate")
	}
}

// A reference is printed in the frame it is read in, so it is parsed in that
// frame too, and the anchor it comes back carrying is the series'. Matching is by
// instant, so the same reference names the same occurrence wherever it is typed.
//
// This is what the web client does: it draws every event in the viewer's own
// zone, links to one occurrence by an absolute timestamp, and applies the
// series' zone only when it writes the RECURRENCE-ID that names the instance.
func TestParseOccurrenceReadsTheReferenceInTheFrameItWasPrintedIn(t *testing.T) {
	loc := vienna(t)
	v := weekly(t, "FREQ=WEEKLY", 12)
	at, err := v.ParseOccurrence("2026-10-19T09:00")
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, 10, 19, 9, 0, 0, 0, loc); !at.Time.Equal(want) {
		t.Errorf("ParseOccurrence = %v, want %v", at.Time, want)
	}
	if at.TZID != "Europe/Vienna" {
		t.Errorf("ParseOccurrence lost the anchor: %+v", at)
	}
}

func TestExcludeOccurrenceWritesTheExclusionInTheSeriesZone(t *testing.T) {
	// A bare UTC exclusion does not cancel an instance of a zone-anchored series,
	// so the "deleted" occurrence would keep coming back.
	v := weekly(t, "FREQ=WEEKLY;COUNT=4", 12)
	at, _ := v.ParseOccurrence("2026-10-19T09:00")
	excluded, changed := v.ExcludeOccurrence(at)
	if !changed {
		t.Fatal("ExcludeOccurrence reported no change")
	}
	if len(excluded.ExDates) != 1 || excluded.ExDates[0].TZID != "Europe/Vienna" {
		t.Fatalf("exclusion = %+v", excluded.ExDates)
	}
	if excluded.RRule != v.RRule {
		t.Errorf("excluding one occurrence changed the rule: %q", excluded.RRule)
	}
	if _, changed := excluded.ExcludeOccurrence(at); changed {
		t.Error("excluding the same occurrence twice added a second exclusion")
	}
}

func TestExcludeOccurrenceKeepsExclusionsSorted(t *testing.T) {
	v := weekly(t, "FREQ=WEEKLY;COUNT=4", 12)
	late, _ := v.ParseOccurrence("2026-11-02T09:00")
	early, _ := v.ParseOccurrence("2026-10-19T09:00")
	out, _ := v.ExcludeOccurrence(late)
	out, _ = out.ExcludeOccurrence(early)
	if len(out.ExDates) != 2 || !out.ExDates[0].Time.Before(out.ExDates[1].Time) {
		t.Errorf("exclusions are not sorted: %+v", out.ExDates)
	}
}

func TestTruncateBeforeShortensACountedRule(t *testing.T) {
	v := weekly(t, "FREQ=WEEKLY;COUNT=8", 12)
	at, _ := v.ParseOccurrence("2026-10-26T09:00")
	out, ok := v.TruncateBefore(at, 3)
	if !ok {
		t.Fatal("TruncateBefore refused a mid-series split")
	}
	if out.RRule != "FREQ=WEEKLY;COUNT=2" {
		t.Errorf("rule = %q, want COUNT=2", out.RRule)
	}
}

func TestTruncateBeforeRefusesTheFirstOccurrence(t *testing.T) {
	// Nothing would be left, so the caller has to remove the event instead of
	// storing an empty series.
	v := weekly(t, "FREQ=WEEKLY;COUNT=8", 12)
	at, _ := v.ParseOccurrence("2026-10-12T09:00")
	if _, ok := v.TruncateBefore(at, 1); ok {
		t.Error("TruncateBefore accepted the first occurrence")
	}
}

func TestTruncateBeforeReplacesAnExistingUntil(t *testing.T) {
	v := weekly(t, "FREQ=WEEKLY;UNTIL=20261231T225959Z", 12)
	at, _ := v.ParseOccurrence("2026-10-26T09:00")
	out, ok := v.TruncateBefore(at, 3)
	if !ok {
		t.Fatal("TruncateBefore refused")
	}
	// The last second of the day before, in the series' own zone, expressed as the
	// UTC instant the RFC requires.
	if out.RRule != "FREQ=WEEKLY;UNTIL=20261025T225959Z" {
		t.Errorf("rule = %q", out.RRule)
	}
}

func TestTruncateBeforeDropsACountWhenItSetsAnUntil(t *testing.T) {
	v := weekly(t, "FREQ=WEEKLY;INTERVAL=2", 12)
	at, _ := v.ParseOccurrence("2026-10-26T09:00")
	out, _ := v.TruncateBefore(at, 2)
	if RuleValue(out.RRule, "INTERVAL") != "2" {
		t.Errorf("truncating lost part of the rule: %q", out.RRule)
	}
	if RuleValue(out.RRule, "COUNT") != "" {
		t.Errorf("rule kept a count alongside an until: %q", out.RRule)
	}
}

func TestAsOverrideNamesTheInstanceAndCarriesNoRule(t *testing.T) {
	series := weekly(t, "FREQ=WEEKLY;COUNT=8", 12)
	series.ExDates = []DateTime{series.Start}
	at, _ := series.ParseOccurrence("2026-10-26T09:00")

	edited := series
	edited.Summary = "Standup (long)"
	out := edited.AsOverride(series, at)

	if out.UID != series.UID {
		t.Errorf("an override must keep the series identity, got %q", out.UID)
	}
	if out.RecurrenceID == nil || !out.RecurrenceID.Equal(at) {
		t.Errorf("recurrence id = %+v", out.RecurrenceID)
	}
	if out.RecurrenceID.TZID != "Europe/Vienna" {
		t.Errorf("recurrence id lost the series anchor: %+v", out.RecurrenceID)
	}
	if out.RRule != "" || out.ExDates != nil {
		t.Errorf("an override carries recurrence of its own: %+v", out)
	}
}

func TestAsFutureSeriesDerivesAUIDAndReducesTheCount(t *testing.T) {
	series := weekly(t, "FREQ=WEEKLY;COUNT=8", 12)
	at, _ := series.ParseOccurrence("2026-10-26T09:00")
	out := series.AsFutureSeries(series, at, 3)
	if out.UID == series.UID {
		t.Error("the remainder of a split must have its own identity")
	}
	if out.UID != "uid-1_R20261026T090000" {
		t.Errorf("UID = %q", out.UID)
	}
	if out.RRule != "FREQ=WEEKLY;COUNT=6" {
		t.Errorf("rule = %q, want the remaining count", out.RRule)
	}
	if out.Sequence != 0 {
		t.Errorf("a new series starts at sequence 0, got %d", out.Sequence)
	}
}

func TestFutureUIDDoesNotStackOffsets(t *testing.T) {
	at := Timed(time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC), "")
	once := FutureUID("uid-1@proton-cli", at)
	twice := FutureUID(once, at)
	if once != twice {
		t.Errorf("splitting twice stacked offsets: %q then %q", once, twice)
	}
	if once != "uid-1_R20261026T090000@proton-cli" {
		t.Errorf("FutureUID = %q", once)
	}
}

func TestNextSequenceRisesOnlyForABreakingChange(t *testing.T) {
	old := weekly(t, "FREQ=WEEKLY;COUNT=8", 12)
	old.Sequence = 4

	renamed := old
	renamed.Summary = "New name"
	if got := NextSequence(old, renamed); got != 4 {
		t.Errorf("renaming bumped the sequence to %d; attendees do not need telling", got)
	}

	moved := old
	moved.Start = Timed(old.Start.Time.Add(time.Hour), old.Start.TZID)
	if got := NextSequence(old, moved); got != 5 {
		t.Errorf("moving the event left the sequence at %d", got)
	}

	shortened := old
	shortened.RRule = "FREQ=WEEKLY;COUNT=3"
	if got := NextSequence(old, shortened); got != 4 {
		t.Errorf("shortening the tail bumped the sequence to %d", got)
	}

	repatterned := old
	repatterned.RRule = "FREQ=DAILY;COUNT=8"
	if got := NextSequence(old, repatterned); got != 5 {
		t.Errorf("changing the pattern left the sequence at %d", got)
	}
}

func TestOccurrencesOfANonRecurringEventIsItself(t *testing.T) {
	loc := vienna(t)
	start := time.Date(2026, 4, 16, 9, 0, 0, 0, loc)
	v := VEvent{UID: "u", Start: Timed(start, "Europe/Vienna"), End: Timed(start.Add(time.Hour), "Europe/Vienna")}
	occs, err := v.Occurrences(Days(date(loc, 2026, time.April, 15), date(loc, 2026, time.April, 17)))
	if err != nil {
		t.Fatal(err)
	}
	if len(occs) != 1 || occs[0].Number != 1 {
		t.Errorf("Occurrences = %+v", occs)
	}
}

// A count is either the whole truth or not a count. A rule that ends has one; a
// rule that does not has none, and reporting a cap in its place is how "every
// weekday forever" came to be printed as a thousand occurrences.
func TestCountOccurrencesCountsWhatEnds(t *testing.T) {
	for _, tc := range []struct {
		name string
		rule string
		want *int
	}{
		{"a rule saying how many times", "FREQ=DAILY;COUNT=12", ptr(12)},
		{"a rule saying when it stops", "FREQ=DAILY;UNTIL=20261231T225959Z", ptr(81)},
		{"a rule saying neither", "FREQ=DAILY", nil},
	} {
		v := weekly(t, tc.rule, 12)
		got, err := v.CountOccurrences()
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		switch {
		case tc.want == nil && got != nil:
			t.Errorf("%s: counted %d, want no count at all", tc.name, *got)
		case tc.want != nil && got == nil:
			t.Errorf("%s: no count, want %d", tc.name, *tc.want)
		case tc.want != nil && *got != *tc.want:
			t.Errorf("%s: counted %d, want %d", tc.name, *got, *tc.want)
		}
	}
}

// COUNT counts the instances a rule generates, cancelled ones included, so a
// series somebody has cancelled an instance out of cannot be answered from the
// rule text and has to be walked.
func TestCountOccurrencesLeavesOutTheCancelledOnes(t *testing.T) {
	v := weekly(t, "FREQ=WEEKLY;COUNT=10", 12)
	v.ExDates = []DateTime{v.Start.At(v.Start.Wall().AddDate(0, 0, 7))}
	got, err := v.CountOccurrences()
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || *got != 9 {
		t.Errorf("CountOccurrences = %v, want 9", got)
	}
}

// A walk that runs out of room says so. Returning as though the rule had ended
// is what let three separate counts be printed as exact.
func TestAWalkThatRunsOutOfRoomSaysSo(t *testing.T) {
	v := weekly(t, "FREQ=MINUTELY", 12)
	if err := v.Walk(func(Occurrence) bool { return true }); !errors.Is(err, ErrTooManyOccurrences) {
		t.Errorf("Walk err = %v, want ErrTooManyOccurrences", err)
	}
}

func ptr(n int) *int { return &n }

// A reference keeps naming the same instance after it is cancelled. Without that,
// cancelling one twice complains that it is not there instead of doing nothing.
func TestOccurrenceAtStillFindsACancelledInstance(t *testing.T) {
	v := weekly(t, "FREQ=WEEKLY;COUNT=4", 12)
	at, err := v.ParseOccurrence("2026-10-19T09:00")
	if err != nil {
		t.Fatal(err)
	}
	cancelled, changed := v.ExcludeOccurrence(at)
	if !changed {
		t.Fatal("ExcludeOccurrence reported no change")
	}

	occ, ok, err := cancelled.OccurrenceAt(at)
	if err != nil || !ok {
		t.Fatalf("OccurrenceAt on a cancelled instance: ok=%v err=%v", ok, err)
	}
	if occ.Number != 2 {
		t.Errorf("the cancelled instance is numbered %d, want 2", occ.Number)
	}
	// Cancelling it again is a no-op rather than a second exclusion.
	if _, changed := cancelled.ExcludeOccurrence(at); changed {
		t.Error("cancelling the same instance twice added a second exclusion")
	}
	// A listing still hides it.
	loc := vienna(t)
	occs, err := cancelled.Occurrences(Days(date(loc, 2026, time.October, 1), date(loc, 2026, time.November, 30)))
	if err != nil {
		t.Fatal(err)
	}
	if len(occs) != 3 {
		t.Errorf("a window listed %d occurrences, want the cancelled one hidden", len(occs))
	}
}
