package ical

import (
	"strings"
	"testing"
	"time"
)

// A stored event arrives as several cards; the model is their union.
const storedCards = "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\n" +
	"UID:uid-1\r\n" +
	"DTSTAMP:20260101T000000Z\r\n" +
	"DTSTART;TZID=Europe/Vienna:20260416T090000\r\n" +
	"DTEND;TZID=Europe/Vienna:20260416T091500\r\n" +
	"RRULE:FREQ=WEEKLY;COUNT=10\r\n" +
	"EXDATE;TZID=Europe/Vienna:20260430T090000\r\n" +
	"EXDATE;TZID=Europe/Vienna:20260423T090000\r\n" +
	"ORGANIZER;CN=me@proton.me:mailto:me@proton.me\r\n" +
	"SEQUENCE:3\r\n" +
	"END:VEVENT\r\nEND:VCALENDAR\r\n" +
	"BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\n" +
	"UID:uid-1\r\n" +
	"SUMMARY:Standup\r\n" +
	"LOCATION:Meet\r\n" +
	"DESCRIPTION:first line\\nsecond line\\, with a comma\r\n" +
	"END:VEVENT\r\nEND:VCALENDAR"

func TestParseMergesEveryCardIntoOneEvent(t *testing.T) {
	v, err := Parse(storedCards)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if v.UID != "uid-1" || v.Summary != "Standup" || v.Location != "Meet" {
		t.Errorf("Parse = %+v", v)
	}
	if v.Description != "first line\nsecond line, with a comma" {
		t.Errorf("description did not survive escaping: %q", v.Description)
	}
	if v.RRule != "FREQ=WEEKLY;COUNT=10" || v.Sequence != 3 || v.Organizer != "me@proton.me" {
		t.Errorf("Parse = %+v", v)
	}
	if v.Start.TZID != "Europe/Vienna" || v.Start.AllDay {
		t.Errorf("start lost its anchor: %+v", v.Start)
	}
	if want := time.Date(2026, 4, 16, 9, 0, 0, 0, v.Start.Location()); !v.Start.Time.Equal(want) {
		t.Errorf("start = %v, want %v", v.Start.Time, want)
	}
	if v.Duration() != 15*time.Minute {
		t.Errorf("duration = %v", v.Duration())
	}
}

func TestParseSortsAndDeduplicatesExclusions(t *testing.T) {
	v, err := Parse(storedCards + "\r\nEXDATE;TZID=Europe/Vienna:20260423T090000")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(v.ExDates) != 2 {
		t.Fatalf("got %d exclusions, want 2 after deduplication", len(v.ExDates))
	}
	if !v.ExDates[0].Time.Before(v.ExDates[1].Time) {
		t.Errorf("exclusions are not sorted: %v", v.ExDates)
	}
}

func TestParseRefusesContentWithNoUID(t *testing.T) {
	// A card that decrypted into something without an identity is not an event,
	// and writing it back would sign nonsense over a real one.
	if _, err := Parse("BEGIN:VEVENT\r\nSUMMARY:x\r\nEND:VEVENT"); err == nil {
		t.Fatal("Parse accepted content with no UID")
	}
}

func TestParseReadsAllDayAndUTCForms(t *testing.T) {
	v, err := Parse("UID:u\r\nDTSTART;VALUE=DATE:20260416\r\nDTEND;VALUE=DATE:20260417")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !v.Start.AllDay || v.Start.String() != "2026-04-16" {
		t.Errorf("all-day start = %+v", v.Start)
	}

	v, err = Parse("UID:u\r\nDTSTART:20260416T070000Z")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if v.Start.TZID != "" || !v.Start.Time.Equal(time.Date(2026, 4, 16, 7, 0, 0, 0, time.UTC)) {
		t.Errorf("UTC start = %+v", v.Start)
	}
}

// Clients write an all-day event's end three ways: the exclusive next day, no end
// at all, and an end equal to the start. All three mean the same days, so an event
// measures the same however it arrived.
func TestSpanReadsEveryShapeOfAllDayEnd(t *testing.T) {
	day := time.Date(2026, 8, 14, 0, 0, 0, 0, time.UTC)
	for name, end := range map[string]DateTime{
		"an exclusive end": Day(day.AddDate(0, 0, 1)),
		"no end":           {},
		"an end at start":  Day(day),
	} {
		v := VEvent{UID: "u", Start: Day(day), End: end}
		_, got := v.Span()
		if got.String() != "2026-08-15" {
			t.Errorf("an all-day event with %s ends %s, want the midnight after its day", name, got.String())
		}
		if v.Duration() != 24*time.Hour {
			t.Errorf("an all-day event with %s lasts %v, want a day", name, v.Duration())
		}
	}
}

func TestSpanOfAThreeDayAllDayEventKeepsItsLength(t *testing.T) {
	day := time.Date(2026, 8, 14, 0, 0, 0, 0, time.UTC)
	v := VEvent{UID: "u", Start: Day(day), End: Day(day.AddDate(0, 0, 3))}
	if _, end := v.Span(); end.String() != "2026-08-17" {
		t.Errorf("end = %s, want the exclusive end it was written with", end.String())
	}
	if v.Duration() != 72*time.Hour {
		t.Errorf("duration = %v, want three days", v.Duration())
	}
}

// An event written with no end is an instant, and stays one: only a whole day has a
// length nobody wrote down.
func TestSpanOfATimedEventWithoutAnEndIsAnInstant(t *testing.T) {
	loc := vienna(t)
	at := time.Date(2026, 8, 14, 9, 0, 0, 0, loc)
	v := VEvent{UID: "u", Start: Timed(at, "Europe/Vienna")}
	start, end := v.Span()
	if !end.Equal(start) {
		t.Errorf("end = %+v, want the start", end)
	}
	if v.Duration() != 0 {
		t.Errorf("duration = %v, want none", v.Duration())
	}
}

// Reading is not rewriting: an event that arrived without an end is stored again
// without one, because normalising what another client wrote would change what the
// event says.
func TestSpanLeavesTheStoredValuesAlone(t *testing.T) {
	v, err := Parse("UID:u\r\nDTSTART;VALUE=DATE:20260814")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if _, end := v.Span(); end.IsZero() {
		t.Fatal("Span reported no end")
	}
	if !v.End.IsZero() {
		t.Errorf("reading the event gave it a stored end of %+v", v.End)
	}
	if got := v.SharedSigned(); strings.Contains(got, "DTEND") {
		t.Errorf("an event that arrived without an end was stored with one:\n%s", got)
	}
}

// The property split is Proton's: recurrence lives in the signed card, the words
// people read live in the encrypted one. An event rebuilt without the recurrence
// properties is an event silently turned back into a one-off.
func TestSharedSignedCarriesEveryRecurrenceProperty(t *testing.T) {
	v, err := Parse(storedCards)
	if err != nil {
		t.Fatal(err)
	}
	card := v.SharedSigned()
	for _, want := range []string{
		"UID:uid-1",
		"DTSTART;TZID=Europe/Vienna:20260416T090000",
		"DTEND;TZID=Europe/Vienna:20260416T091500",
		"RRULE:FREQ=WEEKLY;COUNT=10",
		"EXDATE;TZID=Europe/Vienna:20260423T090000",
		"EXDATE;TZID=Europe/Vienna:20260430T090000",
		"ORGANIZER;CN=me@proton.me:mailto:me@proton.me",
		"SEQUENCE:3",
	} {
		if !strings.Contains(card, want) {
			t.Errorf("signed card is missing %q:\n%s", want, card)
		}
	}
	for _, unwanted := range []string{"SUMMARY", "LOCATION", "DESCRIPTION"} {
		if strings.Contains(card, unwanted) {
			t.Errorf("signed card leaks %s, which belongs in the encrypted card", unwanted)
		}
	}
}

func TestSharedEncryptedCarriesTheWordsAndTheIdentity(t *testing.T) {
	v, err := Parse(storedCards)
	if err != nil {
		t.Fatal(err)
	}
	card := v.SharedEncrypted()
	// Proton lists uid and dtstamp in both parts, so a card without them is not
	// the shape its own clients write.
	for _, want := range []string{"UID:uid-1", "DTSTAMP:", "SUMMARY:Standup", "LOCATION:Meet"} {
		if !strings.Contains(card, want) {
			t.Errorf("encrypted card is missing %q:\n%s", want, card)
		}
	}
	if strings.Contains(card, "RRULE") {
		t.Error("encrypted card carries the recurrence rule, which belongs in the signed card")
	}
	if !strings.Contains(card, `DESCRIPTION:first line\nsecond line\, with a comma`) {
		t.Errorf("description was not escaped on the way out:\n%s", card)
	}
}

func TestRoundTripPreservesEverythingTheModelHolds(t *testing.T) {
	v, err := Parse(storedCards)
	if err != nil {
		t.Fatal(err)
	}
	back, err := Parse(v.SharedSigned() + "\r\n" + v.SharedEncrypted())
	if err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	if back.RRule != v.RRule || len(back.ExDates) != len(v.ExDates) || back.Sequence != v.Sequence {
		t.Errorf("round trip lost recurrence: %+v", back)
	}
	if !back.Start.Equal(v.Start) || back.Start.TZID != v.Start.TZID {
		t.Errorf("round trip moved the start: %+v", back.Start)
	}
	if back.Summary != v.Summary || back.Description != v.Description || back.Location != v.Location {
		t.Errorf("round trip lost the content: %+v", back)
	}
}

func TestAttendeesCardCarriesTokensAndIsEmptyWithoutAttendees(t *testing.T) {
	v := VEvent{UID: "u"}
	if v.AttendeesEncrypted() != "" {
		t.Error("an event with no attendees produced an attendees card")
	}
	v.Attendees = []Attendee{{Email: "alice@proton.me", Token: "tok-a"}}
	card := v.AttendeesEncrypted()
	if !strings.Contains(card, "X-PM-TOKEN=tok-a") || !strings.Contains(card, "mailto:alice@proton.me") {
		t.Errorf("attendees card = %s", card)
	}
	back, err := Parse(card)
	if err != nil {
		t.Fatal(err)
	}
	if len(back.Attendees) != 1 || back.Attendees[0].Token != "tok-a" {
		t.Errorf("attendees did not round trip: %+v", back.Attendees)
	}
}

func TestEventUIDDoesNotRepeat(t *testing.T) {
	// Two events created in the same moment must not share an identity, which a
	// nanosecond clock cannot promise.
	seen := map[string]bool{}
	for range 100 {
		uid := EventUID()
		if seen[uid] {
			t.Fatal("EventUID repeated itself")
		}
		seen[uid] = true
		if !strings.HasSuffix(uid, "@proton-cli") {
			t.Errorf("EventUID = %q, want the proton suffix", uid)
		}
	}
}

// A calendar file is not a message about an event: it carries no METHOD, so a
// client that opens it files the events rather than acting on them.
func TestCalendarFileCarriesNoMethod(t *testing.T) {
	doc := Calendar([]VEvent{{UID: "a", Summary: "One"}, {UID: "b", Summary: "Two"}})
	if strings.Contains(doc, "METHOD") {
		t.Errorf("a calendar file must carry no METHOD:\n%s", doc)
	}
	for _, want := range []string{"BEGIN:VCALENDAR", "UID:a", "UID:b", "END:VCALENDAR"} {
		if !strings.Contains(doc, want) {
			t.Errorf("calendar file is missing %q:\n%s", want, doc)
		}
	}
	if got := strings.Count(doc, "BEGIN:VEVENT"); got != 2 {
		t.Errorf("two events rendered as %d components", got)
	}
	if got := strings.Count(doc, "BEGIN:VCALENDAR"); got != 1 {
		t.Errorf("the events should share one calendar, got %d", got)
	}
}

// An exported reminder has to survive the trip: every other client reads VALARM,
// and Proton's two kinds map onto its two actions.
func TestAlarmsRenderAsValarmComponents(t *testing.T) {
	doc := Calendar([]VEvent{{
		UID: "a", Summary: "Standup",
		Alarms: []Alarm{
			{Action: "DISPLAY", Trigger: "-PT15M"},
			{Action: "EMAIL", Trigger: "-P1D"},
		},
	}})
	for _, want := range []string{
		"BEGIN:VALARM", "ACTION:DISPLAY", "TRIGGER:-PT15M",
		"ACTION:EMAIL", "TRIGGER:-P1D", "END:VALARM",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("missing %q:\n%s", want, doc)
		}
	}
	// An emailed reminder has to say something, and clients require both.
	if !strings.Contains(doc, "SUMMARY:Reminder") || !strings.Contains(doc, "DESCRIPTION:Reminder") {
		t.Errorf("an EMAIL alarm needs a summary and a description:\n%s", doc)
	}
	if got := strings.Count(doc, "BEGIN:VALARM"); got != 2 {
		t.Errorf("two reminders rendered as %d alarms", got)
	}
}

// An event's colour travels in the file and never in a card: it is Proton's own
// cleartext field, and writing it into the content would encrypt something the
// API reads for itself.
func TestColourTravelsInTheFileAndNotInTheCards(t *testing.T) {
	v := VEvent{UID: "a", Summary: "Deadline", Color: "#EC3E7C"}
	for _, card := range []string{v.SharedSigned(), v.SharedEncrypted()} {
		if strings.Contains(card, "COLOR") {
			t.Errorf("a card carries the colour:\n%s", card)
		}
	}

	file := Calendar([]VEvent{v})
	if !strings.Contains(file, "COLOR:#EC3E7C") {
		t.Errorf("the file does not carry the colour:\n%s", file)
	}
	back, err := ParseCalendar(file)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(back) != 1 || back[0].Color != "#EC3E7C" {
		t.Errorf("the colour came back as %+v", back)
	}

	if got := Calendar([]VEvent{{UID: "b", Summary: "Plain"}}); strings.Contains(got, "COLOR") {
		t.Errorf("an event with no colour wrote one:\n%s", got)
	}
}

// An event's own status round-trips, so exporting a cancelled event and reading
// it back does not quietly revive it.
func TestStatusSurvivesTheRoundTrip(t *testing.T) {
	v := VEvent{UID: "a", Summary: "Off", Status: "CANCELLED"}
	back, err := Parse(v.SharedSigned() + "\r\n" + v.SharedEncrypted())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if back.Status != "CANCELLED" {
		t.Errorf("status came back as %q, want CANCELLED", back.Status)
	}
}

// A file's events are separate events, and a VALARM inside one is not an event
// property. Reading a component's lines in flat would let a reminder's own
// SUMMARY overwrite the event's, which is the trap this exists to avoid.
func TestParseCalendarKeepsComponentsApart(t *testing.T) {
	file := strings.Join([]string{
		"BEGIN:VCALENDAR",
		"VERSION:2.0",
		"PRODID:-//Example//EN",
		"BEGIN:VTIMEZONE",
		"TZID:Europe/Vienna",
		"END:VTIMEZONE",
		"BEGIN:VEVENT",
		"UID:one@example.com",
		"DTSTART:20260416T090000Z",
		"SUMMARY:Standup",
		"BEGIN:VALARM",
		"ACTION:DISPLAY",
		"TRIGGER:-PT15M",
		"SUMMARY:Reminder wording that is not the event's",
		"DESCRIPTION:Nor is this",
		"END:VALARM",
		"END:VEVENT",
		"BEGIN:VEVENT",
		"UID:two@example.com",
		"DTSTART:20260417T090000Z",
		"SUMMARY:Retro",
		"END:VEVENT",
		"END:VCALENDAR",
	}, "\r\n")

	events, err := ParseCalendar(file)
	if err != nil {
		t.Fatalf("ParseCalendar: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("read %d events, want 2", len(events))
	}
	if events[0].Summary != "Standup" {
		t.Errorf("the alarm's wording leaked into the event: summary is %q", events[0].Summary)
	}
	if events[0].Description != "" {
		t.Errorf("the alarm's description leaked into the event: %q", events[0].Description)
	}
	if len(events[0].Alarms) != 1 || events[0].Alarms[0].Trigger != "-PT15M" {
		t.Errorf("alarms = %v, want one at -PT15M", events[0].Alarms)
	}
	if events[1].Summary != "Retro" {
		t.Errorf("second event = %q, want Retro", events[1].Summary)
	}
	// The VTIMEZONE's TZID must not have been read as an event.
	for _, e := range events {
		if e.UID == "" {
			t.Error("a component outside VEVENT was read as an event")
		}
	}
}

// What this writes, it reads back.
func TestCalendarRoundTripsThroughTheParser(t *testing.T) {
	want := []VEvent{
		{
			UID: "a@example.com", Summary: "Weekly", RRule: "FREQ=WEEKLY;COUNT=5",
			Start:  DateTime{Time: time.Date(2026, 4, 16, 9, 0, 0, 0, time.UTC)},
			Alarms: []Alarm{{Action: "EMAIL", Trigger: "-P1D"}},
		},
		{
			UID: "b@example.com", Summary: "One off", Status: "CANCELLED",
			Start: DateTime{Time: time.Date(2026, 4, 17, 9, 0, 0, 0, time.UTC)},
		},
	}
	got, err := ParseCalendar(Calendar(want))
	if err != nil {
		t.Fatalf("ParseCalendar: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("round-tripped %d events, want 2", len(got))
	}
	if got[0].RRule != want[0].RRule {
		t.Errorf("rule came back as %q, want %q", got[0].RRule, want[0].RRule)
	}
	if len(got[0].Alarms) != 1 || got[0].Alarms[0].Action != "EMAIL" {
		t.Errorf("alarms came back as %v", got[0].Alarms)
	}
	if got[1].Status != "CANCELLED" {
		t.Errorf("status came back as %q", got[1].Status)
	}
}

// A file another tool wrote may carry properties this one has never heard of.
// Skipping them beats refusing the events around them.
func TestParseCalendarIgnoresPropertiesItDoesNotKnow(t *testing.T) {
	file := strings.Join([]string{
		"BEGIN:VCALENDAR",
		"BEGIN:VEVENT",
		"UID:x@example.com",
		"DTSTART:20260416T090000Z",
		"SUMMARY:Kept",
		"X-SOMEONE-ELSES-EXTENSION:whatever",
		"END:VEVENT",
		"END:VCALENDAR",
	}, "\r\n")
	events, err := ParseCalendar(file)
	if err != nil {
		t.Fatalf("ParseCalendar: %v", err)
	}
	if len(events) != 1 || events[0].Summary != "Kept" {
		t.Errorf("an unknown property lost the event: %v", events)
	}
}
