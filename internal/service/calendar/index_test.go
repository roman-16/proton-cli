package calendar

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/account/keys"
	"github.com/roman-16/proton-cli/internal/ical"
	"github.com/roman-16/proton-cli/internal/progress"
	"github.com/roman-16/proton-cli/internal/proton"
	"github.com/roman-16/proton-cli/internal/search"
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

// A calendar that is in the account and will not open for it, and the Proton
// that answers for it.
//
// It is the state a calendar shared from another account, or one whose key a
// password reset left shut, is in: Proton hands over the events and nothing
// here can read a word of them.
func sealedCalendar(t *testing.T, events string) (*Service, *routeDoer) {
	t.Helper()
	d := &routeDoer{handler: func(r proton.Request) ([]byte, error) {
		switch {
		case r.Path == "/calendar/v1":
			return []byte(`{"Calendars":[{"ID":"cal1","Type":0,"Members":[{"Name":"Personal"}]}]}`), nil
		case strings.HasSuffix(r.Path, "/modelevents/latest"):
			return []byte(`{"CalendarModelEventID":"cursor-0"}`), nil
		case strings.HasSuffix(r.Path, "/events"):
			if events == "" {
				return nil, fmt.Errorf("the calendar would not be read")
			}
			return []byte(events), nil
		case strings.HasSuffix(r.Path, "/bootstrap"):
			return nil, fmt.Errorf("the calendar's keys would not open")
		}
		return []byte(`{}`), nil
	}}
	s := New(d, testKeys(&keys.Unlocked{Addresses: []keys.Address{{ID: "a1", Email: "me@proton.me"}}}))
	kr := indexKeyRing(t)
	s.SetIndex(search.New(t.TempDir(), func() string { return "user-1" },
		func(context.Context) (search.Keys, error) {
			return search.Keys{Seal: kr, Open: kr}, nil
		}))
	return s, d
}

// feed puts one page of the calendar's history in front of the next sync.
func feed(d *routeDoer, page string) {
	inner := d.handler
	d.handler = func(r proton.Request) ([]byte, error) {
		if strings.HasSuffix(r.Path, "/modelevents/cursor-0") {
			return []byte(page), nil
		}
		return inner(r)
	}
}

// indexKeyRing is the account key an index is sealed to.
func indexKeyRing(t *testing.T) *pgp.KeyRing {
	t.Helper()
	key, err := pgp.GenerateKey("test", "test@example.invalid", "x25519", 0)
	if err != nil {
		t.Fatalf("generate a key: %v", err)
	}
	kr, err := pgp.NewKeyRing(key)
	if err != nil {
		t.Fatalf("key ring: %v", err)
	}
	return kr
}

// A calendar whose key will not open is indexed by the times Proton keeps in the
// clear beside each event, so a search covers the days it holds and says nothing
// about what is on them.
func TestACalendarThatWillNotOpenIsIndexedByItsFrames(t *testing.T) {
	s, _ := sealedCalendar(t, `{"Events":[
		{"ID":"ev1","CalendarID":"cal1","StartTime":1700000000,"EndTime":1700003600},
		{"ID":"ev2","CalendarID":"cal1","StartTime":1700090000,"EndTime":1700093600}
	]}`)
	x, err := s.openIndex(t.Context())
	if err != nil {
		t.Fatalf("open the index: %v", err)
	}
	got, err := x.Build(t.Context(), progress.Nop{})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if got.Indexed != 2 || got.Unreadable != 2 {
		t.Errorf("build = %+v, want both events indexed and both unreadable", got)
	}
	st := x.Status()
	if !st.Complete || st.Unreadable != 2 {
		t.Errorf("status = %+v, want a complete index that says what it could not read", st)
	}

	// What is says it could not read is what it holds, so reading the calendar
	// again says the same thing rather than twice as much.
	x.log.State.Stale = true
	if _, err := x.Build(t.Context(), progress.Nop{}); err != nil {
		t.Fatalf("build again: %v", err)
	}
	if st := x.Status(); st.Unreadable != 2 {
		t.Errorf("status = %+v after reading the calendar again, want the same 2 events unread", st)
	}
}

// A calendar whose key will not open still follows its history: what changed
// goes in by the frame Proton keeps in the clear, the way reading the calendar
// whole puts it in, and the catch-up moves past it.
func TestACalendarThatWillNotOpenFollowsItsHistory(t *testing.T) {
	s, d := sealedCalendar(t, `{"Events":[{"ID":"ev1","CalendarID":"cal1","StartTime":1700000000,"EndTime":1700003600}]}`)
	x, err := s.openIndex(t.Context())
	if err != nil {
		t.Fatalf("open the index: %v", err)
	}
	if _, err := x.Build(t.Context(), progress.Nop{}); err != nil {
		t.Fatalf("build: %v", err)
	}
	feed(d, `{"CalendarModelEventID":"cursor-1","More":0,"CalendarEvents":[
		{"ID":"ev2","Action":1,"Event":{"ID":"ev2","CalendarID":"cal1","StartTime":1700090000,"EndTime":1700093600}}
	]}`)

	got, err := x.Sync(t.Context())
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if got.Indexed != 1 || got.Unreadable != 1 {
		t.Errorf("sync = %+v, want the event that changed indexed by its frame", got)
	}
	if cursor := x.log.State.Cursors["cal1"]; cursor != "cursor-1" {
		t.Errorf("cursor = %q, want the applied page moved past", cursor)
	}
	rec, held := x.log.Get("ev2")
	if !held {
		t.Fatal("the event the feed carried is not in the index")
	}
	in := storedFrom(t, rec.Data)
	if in.readErr == nil {
		t.Error("an event of a calendar that will not open went in as though it had been read")
	}
	if in.raw.StartTime != 1700090000 {
		t.Errorf("the event is indexed at %d, want the day Proton keeps beside it", in.raw.StartTime)
	}
	if st := x.Status(); st.Unreadable != 2 {
		t.Errorf("status = %+v, want both the built and the caught-up event counted as unread", st)
	}
}

// A calendar that could not be read leaves the build unfinished, and keeps no
// cursor: a catch-up from a moment nothing was ever read at would report a
// calendar as current that has never been indexed.
func TestACalendarThatWouldNotBeReadLeavesTheBuildUnfinished(t *testing.T) {
	s, _ := sealedCalendar(t, "")
	x, err := s.openIndex(t.Context())
	if err != nil {
		t.Fatalf("open the index: %v", err)
	}
	if _, err := x.Build(t.Context(), progress.Nop{}); err != nil {
		t.Fatalf("build: %v", err)
	}
	if st := x.Status(); st.Complete {
		t.Errorf("status = %+v, want a build that says it did not finish", st)
	}
	if cursor, kept := x.log.State.Cursors["cal1"]; kept {
		t.Errorf("the calendar kept the cursor %q it was never read at", cursor)
	}
}

// What a calendar no longer has leaves the index, and so does everything of a
// calendar the account no longer has.
func TestWhatACalendarNoLongerHasLeavesTheIndex(t *testing.T) {
	s, _ := sealedCalendar(t, `{"Events":[{"ID":"ev1","CalendarID":"cal1","StartTime":1700000000,"EndTime":1700003600}]}`)
	x, err := s.openIndex(t.Context())
	if err != nil {
		t.Fatalf("open the index: %v", err)
	}
	// Two events of a calendar that is gone, and one of a calendar that is not.
	for _, in := range []indexed{
		{ID: "old1", CalendarID: "cal-gone"},
		{ID: "old2", CalendarID: "cal-gone"},
		{ID: "ev-stale", CalendarID: "cal1"},
	} {
		data, err := json.Marshal(in)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if err := x.log.Append(search.Record{ID: in.ID, Data: data}); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	x.log.State.Cursors["cal-gone"] = "cursor-gone"

	if _, err := x.Build(t.Context(), progress.Nop{}); err != nil {
		t.Fatalf("build: %v", err)
	}
	held := map[string]bool{}
	for _, rec := range x.log.Records() {
		held[rec.ID] = true
	}
	if held["old1"] || held["old2"] {
		t.Error("events of a calendar the account no longer has are still in the index")
	}
	if _, kept := x.log.State.Cursors["cal-gone"]; kept {
		t.Error("a calendar the account no longer has kept its place in the feed")
	}
	if held["ev-stale"] {
		t.Error("an event the calendar no longer holds is still in the index")
	}
	if !held["ev1"] {
		t.Error("the event the calendar does hold is not in the index")
	}
}

// A page of a calendar's history that could not be written down is asked for
// again rather than skipped: the cursor stays where the index last caught up,
// in the open session and on disk alike, and the next catch-up applies it.
func TestAPageACalendarCouldNotWriteDownIsAskedForAgain(t *testing.T) {
	s, d := sealedCalendar(t, `{"Events":[{"ID":"ev1","CalendarID":"cal1","StartTime":1700000000,"EndTime":1700003600}]}`)
	x, err := s.openIndex(t.Context())
	if err != nil {
		t.Fatalf("open the index: %v", err)
	}
	if _, err := x.Build(t.Context(), progress.Nop{}); err != nil {
		t.Fatalf("build: %v", err)
	}
	feed(d, `{"CalendarModelEventID":"cursor-1","More":0,"CalendarEvents":[
		{"ID":"ev2","Action":1,"Event":{"ID":"ev2","CalendarID":"cal1","StartTime":1700090000,"EndTime":1700093600}}
	]}`)
	restore := unwritable(t, filepath.Join(s.index.Dir(), "calendar.bin"))

	for range 2 {
		if _, err := x.Sync(t.Context()); err != nil {
			t.Fatalf("sync: %v", err)
		}
		if cursor := x.log.State.Cursors["cal1"]; cursor != "cursor-0" {
			t.Fatalf("cursor = %q after a page that was never written down, want it left at cursor-0", cursor)
		}
	}
	asked := 0
	for _, r := range d.reqs {
		if strings.HasSuffix(r.Path, "/modelevents/cursor-0") {
			asked++
		}
	}
	if asked != 2 {
		t.Errorf("the page was asked for %d times over two catch-ups, want both to ask for it", asked)
	}

	restore()
	reopened, err := s.openIndex(t.Context())
	if err != nil {
		t.Fatalf("reopen the index: %v", err)
	}
	if cursor := reopened.log.State.Cursors["cal1"]; cursor != "cursor-0" {
		t.Errorf("cursor = %q on disk, want the page the index has yet to apply", cursor)
	}
	if _, err := reopened.Sync(t.Context()); err != nil {
		t.Fatalf("sync once the index could be written: %v", err)
	}
	if !reopened.log.Has("ev2") {
		t.Error("the page was never applied")
	}
	if cursor := reopened.log.State.Cursors["cal1"]; cursor != "cursor-1" {
		t.Errorf("cursor = %q, want the page applied and moved past", cursor)
	}
}

// unwritable puts something no write can get past in the way of the index's
// log, and gives back what puts the log back as it was.
func unwritable(t *testing.T, path string) func() {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the log: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("take the log out of the way: %v", err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatalf("put a directory where the log was: %v", err)
	}
	return func() {
		t.Helper()
		if err := os.Remove(path); err != nil {
			t.Fatalf("take the directory away: %v", err)
		}
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatalf("put the log back: %v", err)
		}
	}
}
