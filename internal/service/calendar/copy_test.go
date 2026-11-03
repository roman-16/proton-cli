package calendar

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/roman-16/proton-cli/internal/export"
	"github.com/roman-16/proton-cli/internal/ical"
	"github.com/roman-16/proton-cli/internal/localfile"
)

func copyOf(t *testing.T) (*export.Copy, *export.Ledger) {
	t.Helper()
	c, err := export.Open(filepath.Join(t.TempDir(), "copy"), "user-1", "you@proton.me")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	l, err := c.Ledger(export.AppCalendar)
	if err != nil {
		t.Fatal(err)
	}
	return c, l
}

func event(uid, summary string) ical.VEvent {
	start := time.Date(2027, 9, 6, 9, 0, 0, 0, time.UTC)
	return ical.VEvent{UID: uid, Summary: summary, DTStamp: start, Start: ical.Timed(start, "UTC"), End: ical.Timed(start.Add(time.Hour), "UTC")}
}

func TestACalendarIsWrittenAgainOnlyWhenAnEventMoved(t *testing.T) {
	c, l := copyOf(t)
	cal := Calendar{ID: "cal-1", Name: "Work", Kind: KindPersonal}
	events := []ical.VEvent{event("a", "Standup"), event("b", "Review")}

	f, owed, err := copyPart{}.compare(c, l, cal, events)
	if err != nil {
		t.Fatal(err)
	}
	if !owed || f.changed != 2 {
		t.Fatalf("a first run owes %v with %d changed, want the file and both events", owed, f.changed)
	}
	if err := (copyPart{}).write(c, l, f); err != nil {
		t.Fatal(err)
	}
	if !localfile.Exists(c.Path("calendar/Work.ics")) {
		t.Fatal("the file is not named after the calendar")
	}

	if _, owed, err := (copyPart{}).compare(c, l, cal, events); err != nil || owed {
		t.Errorf("an unchanged calendar is owed again: %v, %v", owed, err)
	}

	moved := []ical.VEvent{event("a", "Standup, moved")}
	f, owed, err = copyPart{}.compare(c, l, cal, moved)
	if err != nil {
		t.Fatal(err)
	}
	if !owed || f.changed != 1 {
		t.Errorf("one changed event owes %v with %d changed, want 1", owed, f.changed)
	}
}

func TestAnEventDeletedInProtonStaysInTheCopy(t *testing.T) {
	c, l := copyOf(t)
	cal := Calendar{ID: "cal-1", Name: "Work", Kind: KindPersonal}
	f, _, err := copyPart{}.compare(c, l, cal, []ical.VEvent{event("a", "Standup"), event("b", "Dentist")})
	if err != nil {
		t.Fatal(err)
	}
	if err := (copyPart{}).write(c, l, f); err != nil {
		t.Fatal(err)
	}
	f, owed, err := copyPart{}.compare(c, l, cal, []ical.VEvent{event("a", "Standup")})
	if err != nil {
		t.Fatal(err)
	}
	if owed || f.changed != 0 {
		t.Errorf("a deletion in Proton owes %v with %d changed, want nothing", owed, f.changed)
	}
	if len(f.events) != 2 || !strings.Contains(f.text, "Dentist") {
		t.Errorf("the deleted event left the copy:\n%s", f.text)
	}
}

func TestACalendarFileReadsBackToItself(t *testing.T) {
	start := time.Date(2027, 9, 6, 9, 0, 0, 0, time.UTC)
	occurrence := ical.Timed(start.AddDate(0, 0, 7), "Europe/Vienna")
	rich := event("r", "Review; with, punctuation\nand a second line")
	rich.Start, rich.End = ical.Timed(start, "Europe/Vienna"), ical.Timed(start.Add(time.Hour), "Europe/Vienna")
	rich.RRule = "FREQ=WEEKLY;COUNT=10"
	rich.ExDates = []ical.DateTime{ical.Timed(start.AddDate(0, 0, 14), "Europe/Vienna")}
	rich.Organizer = "me@proton.me"
	rich.Attendees = []ical.Attendee{
		{Email: "jane@example.com", Token: "tok-j", PartStat: "ACCEPTED", Role: "REQ-PARTICIPANT", RSVP: true},
		{Email: "bob@example.com", Role: "OPT-PARTICIPANT", RSVP: true},
	}
	rich.Location, rich.Description, rich.Status, rich.Color = "Room 3", "Bring the numbers", "CONFIRMED", "#8080FF"
	rich.Sequence = 2
	rich.Alarms = []ical.Alarm{{Action: "DISPLAY", Trigger: "-PT15M"}, {Action: "EMAIL", Trigger: "-P1D"}}
	rich = rich.WithConference(ical.Conference{ID: "id-1", URL: "https://meet.proton.me/join/id-abc#pwd-secret", Provider: ical.ProviderMeet, Password: "secret"})
	override := event("r", "Review, moved")
	override.RecurrenceID = &occurrence
	allDay := event("d", "Holiday")
	allDay.Start, allDay.End = ical.Day(start), ical.Day(start.AddDate(0, 0, 1))

	text := ical.Calendar([]ical.VEvent{rich, override, allDay})
	back, err := ical.ParseCalendar(text)
	if err != nil {
		t.Fatal(err)
	}
	if again := ical.Calendar(back); again != text {
		t.Errorf("the file does not read back to itself:\n%s\n---\n%s", text, again)
	}
}

func TestACalendarFileThatWillNotReadBackIsLeftAlone(t *testing.T) {
	c, l := copyOf(t)
	cal := Calendar{ID: "cal-1", Name: "Work", Kind: KindPersonal}
	f, _, err := copyPart{}.compare(c, l, cal, []ical.VEvent{event("a", "Standup")})
	if err != nil {
		t.Fatal(err)
	}
	if err := (copyPart{}).write(c, l, f); err != nil {
		t.Fatal(err)
	}
	broken := "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nDTSTART:not-a-time\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
	if err := os.WriteFile(c.Path("calendar/Work.ics"), []byte(broken), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := (copyPart{}).compare(c, l, cal, []ical.VEvent{event("a", "Standup")}); !errors.Is(err, errUnreadableCopy) {
		t.Errorf("compare = %v, want errUnreadableCopy", err)
	}
}

func TestARenamedCalendarTakesItsFileAlong(t *testing.T) {
	c, l := copyOf(t)
	events := []ical.VEvent{event("a", "Standup")}
	f, _, err := copyPart{}.compare(c, l, Calendar{ID: "cal-1", Name: "Work"}, events)
	if err != nil {
		t.Fatal(err)
	}
	if err := (copyPart{}).write(c, l, f); err != nil {
		t.Fatal(err)
	}
	if _, owed, err := (copyPart{}).compare(c, l, Calendar{ID: "cal-1", Name: "Office"}, events); err != nil || owed {
		t.Errorf("a renamed calendar is written again: %v, %v", owed, err)
	}
	if localfile.Exists(c.Path("calendar/Work.ics")) || !localfile.Exists(c.Path("calendar/Office.ics")) {
		t.Error("the file did not follow the calendar's new name")
	}
}
