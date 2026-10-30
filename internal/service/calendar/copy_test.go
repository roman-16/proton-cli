package calendar

import (
	"path/filepath"
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
	if !owed || f.changed != 2 {
		t.Errorf("one changed event and one removed owe %v with %d changed, want 2", owed, f.changed)
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
