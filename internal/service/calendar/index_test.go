package calendar

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/roman-16/proton-cli/internal/ical"
)

// What the index holds has to be the event the account holds: a record goes in
// as a decrypted event and has to come back out as one, because everything a
// search answers is read off what comes back.

func indexedStored(t *testing.T, summary, location, description string) stored {
	t.Helper()
	start := atVienna(t, 6, 3, 9)
	return stored{
		raw: rawEvent{
			ID: "event-1", CalendarID: "cal1", UID: "uid-1",
			StartTime: start.Unix(), EndTime: start.Add(30 * time.Minute).Unix(),
			StartTimezone: "Europe/Vienna", EndTimezone: "Europe/Vienna",
			Notifications: []rawNotification{{Type: 0, Trigger: "-PT15M"}},
		},
		model: ical.VEvent{
			UID:         "uid-1",
			Summary:     summary,
			Location:    location,
			Description: description,
			Organizer:   "jane@example.com",
			Start:       ical.Timed(start, "Europe/Vienna"),
			End:         ical.Timed(start.Add(30*time.Minute), "Europe/Vienna"),
			Attendees:   []ical.Attendee{{Email: "me@proton.me"}},
		},
	}
}

// An event written to the index and read back is the event it was.
func TestAnIndexedEventReadsBackAsItself(t *testing.T) {
	want := indexedStored(t, "Dentist", "Mariahilf", "bring the referral")
	rec, readable, err := record(want)
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	if !readable {
		t.Fatal("an event that decrypted was recorded as unreadable")
	}

	got := storedFrom(t, rec.Data)
	if got.readErr != nil {
		t.Fatalf("the record did not read back: %v", got.readErr)
	}
	switch {
	case got.model.Summary != want.model.Summary:
		t.Errorf("title = %q", got.model.Summary)
	case got.model.Location != want.model.Location:
		t.Errorf("location = %q", got.model.Location)
	case got.model.Description != want.model.Description:
		t.Errorf("description = %q", got.model.Description)
	case got.model.Organizer != want.model.Organizer:
		t.Errorf("organizer = %q", got.model.Organizer)
	case len(got.model.Attendees) != 1:
		t.Errorf("attendees = %v", got.model.Attendees)
	case got.raw.ID != want.raw.ID || got.raw.CalendarID != want.raw.CalendarID:
		t.Errorf("the event is addressed as %s/%s", got.raw.CalendarID, got.raw.ID)
	}
	if got.raw.triggers()[0] != want.raw.triggers()[0] {
		t.Errorf("reminders = %v, want %v", got.raw.triggers(), want.raw.triggers())
	}

	row := expand([]stored{got}, daysAtVienna(t, 6, 1, 6, 30))
	if len(row) != 1 || row[0].Title != "Dentist" {
		t.Fatalf("the indexed event lists as %+v", row)
	}
}

// An event this account cannot open is indexed by the times Proton keeps beside
// it, so it is still on the right day and still says nothing it does not know.
func TestAnEventThatWouldNotOpenIsIndexedByItsFrame(t *testing.T) {
	unreadable := unreadableStored(atVienna(t, 6, 3, 9))
	rec, readable, err := record(unreadable)
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	if readable {
		t.Error("an event that would not decrypt was recorded as readable")
	}
	got := storedFrom(t, rec.Data)
	if got.readErr == nil {
		t.Error("an event with no content read back as though it had some")
	}
	rows := expand([]stored{got}, daysAtVienna(t, 6, 1, 6, 30))
	if len(rows) != 1 {
		t.Fatalf("an unreadable event should still be listed: %+v", rows)
	}
	if rows[0].Title != "" {
		t.Errorf("title = %q, want nothing claimed about it", rows[0].Title)
	}
}

// A series is stored once and searched as the series: a keyword matches its
// text, and what the answer holds is every occurrence in the window.
func TestASearchOverTheIndexExpandsASeries(t *testing.T) {
	series := seriesStored(t, "FREQ=WEEKLY;COUNT=4")
	rec, _, err := record(series)
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	rows := expand([]stored{storedFrom(t, rec.Data)}, daysAtVienna(t, 4, 1, 4, 30))
	if len(rows) != 4 {
		t.Fatalf("got %d rows, want one per occurrence", len(rows))
	}
}

// The window a keyword search covers when nobody names one reaches back before
// any account existed and forward as far as a repeating event is listed.
//
// The far end is a choice rather than a limit: a weekly series generates fifty
// rows a year, so covering a lifetime of them would answer a question about a
// dentist with thousands of standups.
func TestTheSearchWindowCoversWhateverIsThere(t *testing.T) {
	w := SearchWindow()
	longAgo := ical.Timed(time.Date(1999, time.March, 2, 10, 0, 0, 0, time.UTC), "UTC")
	soon := ical.Timed(time.Now().AddDate(2, 0, 0), "UTC")
	farOff := ical.Timed(time.Now().AddDate(yearsExpanded+2, 0, 0), "UTC")
	if !w.Covers(longAgo, longAgo) {
		t.Error("an event from before the account existed is outside the search window")
	}
	if !w.Covers(soon, soon) {
		t.Error("an occurrence two years out is outside the search window")
	}
	if w.Covers(farOff, farOff) {
		t.Errorf("the search window reaches %d years out", yearsExpanded+2)
	}
}

func storedFrom(t *testing.T, data []byte) stored {
	t.Helper()
	var in indexed
	if err := json.Unmarshal(data, &in); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return in.event()
}
