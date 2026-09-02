package tests

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// ── calendars ──

func TestCalendarCalendarsList(t *testing.T) {
	t.Parallel()
	stdout := runOK(t, "calendar", "settings", "calendars", "list")
	assertContains(t, stdout, "NAME")
	assertContains(t, stdout, "COLOR")
}

func TestCalendarCalendarsListColorPopulated(t *testing.T) {
	t.Parallel()
	cals := runJSONArray(t, "calendar", "settings", "calendars", "list")
	if len(cals) == 0 {
		t.Skip("no calendars on account")
	}
	// Color lives on Members[0] in the API, service should surface it.
	gotAny := false
	for _, c := range cals {
		color, _ := c.(map[string]interface{})["color"].(string)
		if strings.HasPrefix(color, "#") {
			gotAny = true
			break
		}
	}
	if !gotAny {
		t.Error("expected at least one calendar with a populated #hex color")
	}
}

func TestCalendarCalendarsCreateAndDelete(t *testing.T) {
	t.Parallel()
	lease(t, calendarSlot)
	name := testID() + "-cal"
	stdout := runOK(t, "calendar", "settings", "calendars", "create", "--name", name, "--color", "#8080FF")
	id := strings.TrimSpace(stdout)
	if !looksLikeID(id) {
		t.Fatalf("expected bare ID on stdout, got %q", stdout)
	}
	cleanupRun(t, fmt.Sprintf("Delete calendar: proton calendar settings calendars delete -- %s", id),
		"calendar", "settings", "calendars", "delete", "--", id)

	assertContains(t, runOK(t, "calendar", "settings", "calendars", "list"), name)

	// Deleting is what exercises the scope-elevation path, so it is asserted here
	// rather than left to cleanup, whose failure never fails a test.
	runOK(t, "calendar", "settings", "calendars", "delete", "--", id)
	assertNotContains(t, runOK(t, "calendar", "settings", "calendars", "list"), name)
}

// ── events ──

func TestCalendarEventsList(t *testing.T) {
	t.Parallel()
	stdout := runOK(t, "calendar", "events", "list", "--calendar", "Default")
	_ = stdout // may be empty; only assert the command runs
}

func TestCalendarEventsCRUDByIDs(t *testing.T) {
	t.Parallel()
	title := testID() + "-event"
	start := time.Now().Add(48 * time.Hour).Format("2006-01-02T15:04")

	idOut := runOK(t, "calendar", "events", "create",
		"--calendar", "Default",
		"--title", title,
		"--start", start,
		"--duration", "1h")
	// An event lives in a calendar, so creating one answers with both halves as
	// the single reference every event verb takes.
	eventID := strings.TrimSpace(idOut)
	if !looksLikePairRef(eventID) {
		t.Fatalf("expected CALENDAR_ID/EVENT_ID on stdout, got %q", idOut)
	}
	cleanupRun(t, fmt.Sprintf("Delete event: proton calendar events delete -- %s", eventID),
		"calendar", "events", "delete", "--", eventID)

	// Get by IDs
	got := runOK(t, "calendar", "events", "get", "--", eventID)
	assertContains(t, got, title)
	// Signature: an event we just created is signed with our own address key.
	assertField(t, got, "Signature:", "verified")

	// Update title + location
	runOK(t, "calendar", "events", "update", "--title", title+"-updated", "--location", "Vienna",
		"--", eventID)
	got2 := runOK(t, "calendar", "events", "get", "--", eventID)
	assertContains(t, got2, title+"-updated")
	assertContains(t, got2, "Vienna")
}

func TestCalendarEventsGetByTitleRef(t *testing.T) {
	t.Parallel()
	title := testID() + "-ref"
	start := time.Now().Add(48 * time.Hour).Format("2006-01-02T15:04")
	idOut := runOK(t, "calendar", "events", "create",
		"--calendar", "Default",
		"--title", title,
		"--start", start,
		"--duration", "30m")
	eventID := strings.TrimSpace(idOut)
	cleanupRun(t, fmt.Sprintf("Delete event: proton calendar events delete -- %s", eventID),
		"calendar", "events", "delete", "--", eventID)

	// REF = title substring
	stdout := runOK(t, "calendar", "events", "get", title)
	assertContains(t, stdout, title)
}

func TestCalendarEventsDeleteByTitleRef(t *testing.T) {
	t.Parallel()
	title := testID() + "-refdel"
	start := time.Now().Add(48 * time.Hour).Format("2006-01-02T15:04")
	runOK(t, "calendar", "events", "create",
		"--calendar", "Default",
		"--title", title,
		"--start", start,
		"--duration", "15m")

	runOK(t, "calendar", "events", "delete", title)

	_, _, code := run(t, "calendar", "events", "get", title)
	if code != 3 {
		t.Errorf("expected exit 3 after delete, got %d", code)
	}
}

func TestCalendarEventsNotFound(t *testing.T) {
	t.Parallel()
	_, _, code := run(t, "calendar", "events", "get", "no-such-event-"+testID())
	if code != 3 {
		t.Errorf("expected exit 3 for unknown event, got %d", code)
	}
}

// eventPath is the raw API path for an event. The CLI hands an event's two
// halves back as one reference, but the endpoint wants them apart.
// eventIfStillThere reads one event of a shared calendar, and answers nil when it
// is not there any more: the listing this walks is of a calendar other tests are
// making and deleting events in, so one going missing mid-walk is ordinary.
func eventIfStillThere(t *testing.T, calendarID, eventID string) map[string]interface{} {
	t.Helper()
	stdout, _, code := run(t, "--output", "json", "api", "GET", "/calendar/v1/"+calendarID+"/events/"+eventID)
	if code != 0 {
		return nil
	}
	var out map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("api GET of event %s returned unreadable JSON: %v", eventID, err)
	}
	return out
}

func eventPath(t *testing.T, ref string) string {
	t.Helper()
	cal, ev, ok := strings.Cut(ref, "/")
	if !ok {
		t.Fatalf("expected CALENDAR_ID/EVENT_ID, got %q", ref)
	}
	return "/calendar/v1/" + cal + "/events/" + ev
}

// firstCalendarID is which calendar a test writes to when it does not care. It is
// looked up once: the account's calendars do not change under the suite, so asking
// again for every test that needs one is a listing paid for six times over.
var defaultCalendarID = sync.OnceValues(func() (string, error) {
	stdout, stderr, code, err := runArgs(nil, "--output", "json", "calendar", "settings", "calendars", "list")
	if err != nil || code != 0 {
		return "", fmt.Errorf("list calendars (exit %d): %v %s", code, err, strings.TrimSpace(stderr))
	}
	var env struct {
		Calendars []struct{ ID string } `json:"calendars"`
	}
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		return "", err
	}
	if len(env.Calendars) == 0 {
		return "", nil
	}
	return env.Calendars[0].ID, nil
})

func firstCalendarID(t *testing.T) string {
	t.Helper()
	id, err := defaultCalendarID()
	if err != nil {
		t.Fatalf("could not read the account's calendars: %v", err)
	}
	if id == "" {
		t.Skip("no calendars on this account")
	}
	return id
}

func TestCalendarEventRecurrenceAndDescription(t *testing.T) {
	t.Parallel()
	calID := firstCalendarID(t)

	title := testID() + "-evt"
	eventID := strings.TrimSpace(runOK(t, "calendar", "events", "create",
		"--calendar", calID, "--title", title,
		"--description", "quarterly sync", "--start", "2026-08-16T14:00", "--duration", "1h",
		"--rrule", "FREQ=WEEKLY;COUNT=5", "--remind", "15m"))
	cleanupRun(t, fmt.Sprintf("Delete event: proton calendar events delete %s", eventID),
		"calendar", "events", "delete", eventID)

	got := runOK(t, "calendar", "events", "get", eventID)
	assertContains(t, got, "quarterly sync")
	assertContains(t, got, "FREQ=WEEKLY")
}

func TestCalendarEventReminderNotification(t *testing.T) {
	t.Parallel()
	calID := firstCalendarID(t)

	title := testID() + "-remind"
	eventID := strings.TrimSpace(runOK(t, "calendar", "events", "create",
		"--calendar", calID, "--title", title,
		"--start", "2026-09-01T09:00", "--duration", "30m", "--remind", "15m"))
	cleanupRun(t, fmt.Sprintf("Delete event: proton calendar events delete %s", eventID),
		"calendar", "events", "delete", eventID)

	data := runJSON(t, "api", "GET", eventPath(t, eventID))
	ev, _ := data["Event"].(map[string]interface{})
	notifs, _ := ev["Notifications"].([]interface{})
	found := false
	for _, n := range notifs {
		if m, ok := n.(map[string]interface{}); ok {
			if trig, _ := m["Trigger"].(string); trig == "-PT15M" {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("expected a -PT15M notification on the event, got: %v", notifs)
	}
}

func TestCalendarEventWithProtonAttendee(t *testing.T) {
	t.Parallel()
	calID := firstCalendarID(t)

	title := testID() + "-attendee"
	eventID := strings.TrimSpace(runOK(t, "calendar", "events", "create",
		"--calendar", calID, "--title", title,
		"--start", "2026-08-17T10:00", "--duration", "30m",
		"--attendee", selfEmail()))
	cleanupRun(t, fmt.Sprintf("Delete event: proton calendar events delete %s", eventID),
		"calendar", "events", "delete", eventID)

	if !looksLikeID(eventID) {
		t.Errorf("expected an event ID on stdout, got %q", eventID)
	}
	// The server accepted the encrypted attendee parts; the event must read back.
	runOK(t, "calendar", "events", "get", eventID)
}

func TestCalendarCalendarsRename(t *testing.T) {
	t.Parallel()
	lease(t, calendarSlot)
	name := testID() + "-cal"
	calID := strings.TrimSpace(runOK(t, "calendar", "settings", "calendars", "create", "--name", name, "--color", "#8080FF"))
	cleanupRun(t, fmt.Sprintf("Delete calendar: proton calendar settings calendars delete %s", calID),
		"calendar", "settings", "calendars", "delete", calID)

	newName := name + "-renamed"
	runOK(t, "calendar", "settings", "calendars", "update", "--name", newName, "--color", "#DB60D6", calID)
	assertContains(t, runOK(t, "calendar", "settings", "calendars", "list"), newName)
}

// TestCalendarCreateUsable proves a freshly created calendar is provisioned
// with keys (setupCalendar) by creating an event in it.
func TestCalendarCreateUsable(t *testing.T) {
	t.Parallel()
	lease(t, calendarSlot)
	name := testID() + "-usable"
	calID := strings.TrimSpace(runOK(t, "calendar", "settings", "calendars", "create", "--name", name, "--color", "#8080FF"))
	cleanupRun(t, fmt.Sprintf("Delete calendar: proton calendar settings calendars delete %s", calID),
		"calendar", "settings", "calendars", "delete", calID)

	eventID := strings.TrimSpace(runOK(t, "calendar", "events", "create",
		"--calendar", calID, "--title", name+"-evt",
		"--start", "2026-08-20T10:00", "--duration", "1h"))
	if !looksLikeID(eventID) {
		t.Errorf("expected event ID on stdout, got %q", eventID)
	}
	assertContains(t, runOK(t, "calendar", "events", "get", eventID), name+"-evt")
}

// ── events respond (RSVP) ──
//
// A full accept/decline round-trip needs a *second* Proton account to invite
// this one (the harness can't act as the organizer), so it's manual - same as
// `drive invitations` accept/reject. These cover the branches reachable with
// a single account: flag validation, dry-run, and the organizer rejection.

func TestCalendarEventsRespondDryRun(t *testing.T) {
	t.Parallel()
	calID := firstCalendarID(t)
	title := testID() + "-rsvp-dry"
	start := time.Now().Add(48 * time.Hour).Format("2006-01-02T15:04")
	eventID := strings.TrimSpace(runOK(t, "calendar", "events", "create",
		"--calendar", calID, "--title", title, "--start", start, "--duration", "30m",
		"--attendee", selfEmail()))
	cleanupRun(t, fmt.Sprintf("Delete event: proton calendar events delete %s", eventID),
		"calendar", "events", "delete", eventID)

	_, stderr := runOKStderr(t, "--dry-run", "calendar", "events", "respond",
		"--answer", "accept", eventID)
	assertContains(t, stderr, "Dry run")
	// The event still reads back (no mutation happened).
	assertContains(t, runOK(t, "calendar", "events", "get", eventID), title)
}

func TestCalendarEventsRespondRejectsOrganizer(t *testing.T) {
	t.Parallel()
	calID := firstCalendarID(t)
	title := testID() + "-rsvp-org"
	start := time.Now().Add(48 * time.Hour).Format("2006-01-02T15:04")
	// We create the event, so we are its organizer; RSVP must be refused.
	eventID := strings.TrimSpace(runOK(t, "calendar", "events", "create",
		"--calendar", calID, "--title", title, "--start", start, "--duration", "30m",
		"--attendee", selfEmail()))
	cleanupRun(t, fmt.Sprintf("Delete event: proton calendar events delete %s", eventID),
		"calendar", "events", "delete", eventID)

	_, stderr, code := run(t, "calendar", "events", "respond", "--answer", "accept", eventID)
	if code != 1 {
		t.Errorf("expected exit 1 responding to your own event, got %d (stderr: %s)", code, stderr)
	}
	assertContains(t, stderr, "organizer")
}

// ── events respond: two-account RSVP round-trip ──
//
// Needs the second account (the `secondary` profile): it organizes
// an event and invites the primary, the primary RSVPs, then we verify the
// primary's partstat flipped and the organizer got the METHOD:REPLY email.

func eventField(ev map[string]interface{}, key string) interface{} {
	e, _ := ev["Event"].(map[string]interface{})
	if e == nil {
		return nil
	}
	return e[key]
}

func firstAttendeeStatus(ev map[string]interface{}) (int, bool) {
	e, _ := ev["Event"].(map[string]interface{})
	if e == nil {
		return 0, false
	}
	ai, _ := e["AttendeesInfo"].(map[string]interface{})
	if ai == nil {
		return 0, false
	}
	atts, _ := ai["Attendees"].([]interface{})
	if len(atts) == 0 {
		return 0, false
	}
	a, _ := atts[0].(map[string]interface{})
	s, ok := a["Status"].(float64)
	return int(s), ok
}

func TestCalendarEventsRespondRoundTrip(t *testing.T) {
	t.Parallel()
	secondaryCals := runJSONArraySecondary(t, "calendar", "settings", "calendars", "list")
	if len(secondaryCals) == 0 {
		t.Skip("the second account has no calendars")
	}
	secondaryCal := secondaryCals[0].(map[string]interface{})["id"].(string)

	title := testID() + "-rsvp-rt"
	start := time.Now().Add(72 * time.Hour).Format("2006-01-02T15:04")
	secondaryEventID := strings.TrimSpace(runOKSecondary(t, "calendar", "events", "create",
		"--calendar", secondaryCal, "--title", title, "--start", start, "--duration", "30m",
		"--attendee", selfEmail()))
	cleanupRunSecondary(t, fmt.Sprintf("Delete the secondary account's event: proton --profile secondary calendar events delete %s", secondaryEventID),
		"calendar", "events", "delete", secondaryEventID)

	uid, _ := eventField(runJSONSecondary(t, "api", "GET", eventPath(t, secondaryEventID)), "UID").(string)
	if uid == "" {
		t.Fatal("could not read the event UID")
	}

	// The Proton-to-Proton invite lands on the primary's calendar as a shared
	// event (IsOrganizer=0). Find our copy by matching the UID.
	primaryCal := firstCalendarID(t)
	var primaryEventID string
	waitFor(45*time.Second, 3*time.Second, func() bool {
		evs := runJSONArray(t, "calendar", "events", "list", "--calendar", primaryCal,
			"--start", time.Now().Format("2006-01-02"),
			"--end", time.Now().Add(120*time.Hour).Format("2006-01-02"))
		for _, e := range evs {
			id := e.(map[string]interface{})["id"].(string)
			ev := eventIfStillThere(t, primaryCal, id)
			if ev == nil {
				continue
			}
			if u, _ := eventField(ev, "UID").(string); u == uid {
				if org, _ := eventField(ev, "IsOrganizer").(float64); int(org) == 0 {
					primaryEventID = id
					return true
				}
			}
		}
		return false
	})
	if primaryEventID == "" {
		t.Fatal("invitation did not appear on the primary's calendar")
	}
	// `events list` reports the two halves separately, so this one has to be
	// joined into the single reference every event verb takes.
	primaryRef := primaryCal + "/" + primaryEventID
	cleanupRun(t, fmt.Sprintf("Delete primary event copy: proton calendar events delete %s", primaryRef),
		"calendar", "events", "delete", primaryRef)

	runOK(t, "calendar", "events", "respond", "--answer", "accept", primaryRef)

	// The primary's own attendee record now shows ACCEPTED (ATTENDEE_STATUS_API 3).
	got := runJSON(t, "api", "GET", "/calendar/v1/"+primaryCal+"/events/"+primaryEventID)
	if status, ok := firstAttendeeStatus(got); !ok || status != 3 {
		t.Errorf("primary attendee Status = %d (ok=%v), want 3 (accepted)", status, ok)
	}

	// The organizer receives the METHOD:REPLY email naming the title.
	var replyID string
	waitFor(45*time.Second, 3*time.Second, func() bool {
		replyID = secondaryMailContaining(t, selfEmail(), title)
		return replyID != ""
	})
	if replyID == "" {
		t.Error("the organizer did not receive the RSVP reply email")
	} else {
		cleanupRunSecondary(t, "Delete reply mail (secondary): proton --profile secondary mail messages delete "+replyID,
			"mail", "messages", "delete", replyID)
	}
}

// ── recurrence ──
//
// A recurring event is stored once and happens many times, so almost everything
// about it is only observable through a window: whether the occurrences show up,
// whether changing one leaves the others alone, and whether a deleted one stays
// deleted. These tests work the way a person does - list a range, act on a row,
// list again - because that is the only place the difference is visible.

// seriesAnchor is a fixed far-future date, so a run cannot collide with real
// events on the account or with a previous run's leftovers.
const seriesAnchor = "2027-03-01"

// occurrencesOf lists a window and returns the rows whose title matches.
func occurrencesOf(t *testing.T, title, from, to string) []map[string]interface{} {
	t.Helper()
	rows := runJSONArray(t, "calendar", "events", "list",
		"--calendar", "Default", "--start", from, "--end", to)
	var out []map[string]interface{}
	for _, r := range rows {
		row, ok := r.(map[string]interface{})
		if !ok {
			continue
		}
		if got, _ := row["title"].(string); strings.Contains(got, title) {
			out = append(out, row)
		}
	}
	return out
}

// occurrenceRefs is the reference each matching row is addressed by, which is what
// a person would copy out of the table.
func occurrenceRefs(t *testing.T, title, from, to string) []string {
	t.Helper()
	var out []string
	for _, row := range occurrencesOf(t, title, from, to) {
		cal, _ := row["calendar_id"].(string)
		id, _ := row["id"].(string)
		ref := cal + "/" + id
		if occ, _ := row["occurrence"].(string); occ != "" {
			ref += "@" + occ
		}
		out = append(out, ref)
	}
	return out
}

func createSeries(t *testing.T, title, start, rule string, extra ...string) string {
	t.Helper()
	args := append([]string{
		"calendar", "events", "create",
		"--calendar", "Default", "--title", title,
		"--start", start, "--duration", "15m", "--rrule", rule,
	}, extra...)
	ref := strings.TrimSpace(runOK(t, args...))
	if !looksLikePairRef(ref) {
		t.Fatalf("expected CALENDAR_ID/EVENT_ID on stdout, got %q", ref)
	}
	cleanupRun(t, fmt.Sprintf("Delete series: proton calendar events delete -- %s", ref),
		"calendar", "events", "delete", "--", ref)
	return ref
}

// A weekly event is one stored record. Asking about a week three weeks out has to
// answer with that week's occurrence, which is only true if the rule is expanded
// and if the record is asked for in the window it reaches into rather than the one
// it starts in.
func TestCalendarRecurringSeriesAppearsOnEveryOccurrence(t *testing.T) {
	t.Parallel()
	title := testID() + "-weekly"
	createSeries(t, title, seriesAnchor+"T09:00", "FREQ=WEEKLY;COUNT=5")

	all := occurrencesOf(t, title, seriesAnchor, "2027-04-05")
	if len(all) != 5 {
		t.Fatalf("a five-occurrence series listed %d rows", len(all))
	}
	for i, row := range all {
		occ, _ := row["occurrence"].(string)
		if occ == "" {
			t.Errorf("row %d has no occurrence, so it cannot be addressed", i)
		}
		if n, _ := row["occurrence_number"].(float64); int(n) != i+1 {
			t.Errorf("row %d is numbered %v", i, row["occurrence_number"])
		}
	}

	// A window that contains only the fourth occurrence answers with it alone,
	// even though the record itself is dated three weeks earlier.
	later := occurrencesOf(t, title, "2027-03-22", "2027-03-22")
	if len(later) != 1 {
		t.Fatalf("a window three weeks out listed %d rows, want 1", len(later))
	}
	if occ, _ := later[0]["occurrence"].(string); occ != "2027-03-22T09:00" {
		t.Errorf("the occurrence in that window is %q", occ)
	}
}

// An all-day event is asked for through a different window than a timed one, so
// asking for only one window makes every all-day event invisible.
func TestCalendarAllDayEventAppearsInAList(t *testing.T) {
	t.Parallel()
	title := testID() + "-allday"
	ref := strings.TrimSpace(runOK(t, "calendar", "events", "create",
		"--calendar", "Default", "--title", title,
		"--start", "2027-05-04", "--all-day"))
	cleanupRun(t, fmt.Sprintf("Delete all-day event: proton calendar events delete -- %s", ref),
		"calendar", "events", "delete", "--", ref)

	rows := occurrencesOf(t, title, "2027-05-04", "2027-05-04")
	if len(rows) != 1 {
		t.Fatalf("an all-day event listed %d rows in its own day, want 1", len(rows))
	}
	if allDay, _ := rows[0]["all_day"].(bool); !allDay {
		t.Error("the row does not report itself as all-day")
	}
	assertContains(t, runOK(t, "calendar", "events", "list",
		"--calendar", "Default", "--start", "2027-05-04", "--end", "2027-05-04"), "all day")
}

// A whole-day event runs for a day, whichever way the end was stored: Proton keeps
// an all-day range non-inclusively and clients differ on whether they write the end
// at all, so a consumer that measured the row would otherwise get 0 or 24 hours for
// the same event.
func TestCalendarAllDayEventLastsAWholeDay(t *testing.T) {
	t.Parallel()
	title := testID() + "-allday-span"
	ref := strings.TrimSpace(runOK(t, "calendar", "events", "create",
		"--calendar", "Default", "--title", title,
		"--start", "2027-05-11", "--all-day"))
	cleanupRun(t, fmt.Sprintf("Delete all-day event: proton calendar events delete -- %s", ref),
		"calendar", "events", "delete", "--", ref)

	rows := occurrencesOf(t, title, "2027-05-11", "2027-05-11")
	if len(rows) != 1 {
		t.Fatalf("an all-day event listed %d rows in its own day, want 1", len(rows))
	}
	start, _ := rows[0]["start"].(string)
	end, _ := rows[0]["end"].(string)
	from, err := time.Parse(time.RFC3339, start)
	if err != nil {
		t.Fatalf("start %q is not a timestamp: %v", start, err)
	}
	until, err := time.Parse(time.RFC3339, end)
	if err != nil {
		t.Fatalf("end %q is not a timestamp: %v", end, err)
	}
	if from.Format("2006-01-02") != "2027-05-11" {
		t.Errorf("the row starts %s, want the day the event names", start)
	}
	if until.Format("2006-01-02") != "2027-05-12" {
		t.Errorf("the row ends %s, want the midnight after its day", end)
	}
	assertField(t, runOK(t, "calendar", "events", "get", "--", ref), "Duration:", "1d")
}

// The day after the last one named is not reported, by either route into a listing:
// an all-day event begins at the instant that day begins, and an all-day row carries
// no time of day to give it away. Recurring and one-off events are filtered by the
// same rule, so both are checked here.
func TestCalendarListStopsAtTheLastDayNamed(t *testing.T) {
	t.Parallel()
	series := testID() + "-leak-series"
	seriesRef := strings.TrimSpace(runOK(t, "calendar", "events", "create",
		"--calendar", "Default", "--title", series,
		"--start", "2027-05-18", "--all-day", "--rrule", "FREQ=DAILY;COUNT=5"))
	cleanupRun(t, fmt.Sprintf("Delete all-day series: proton calendar events delete -- %s", seriesRef),
		"calendar", "events", "delete", "--", seriesRef)

	oneOff := testID() + "-leak-oneoff"
	oneOffRef := strings.TrimSpace(runOK(t, "calendar", "events", "create",
		"--calendar", "Default", "--title", oneOff,
		"--start", "2027-05-19", "--all-day"))
	cleanupRun(t, fmt.Sprintf("Delete all-day event: proton calendar events delete -- %s", oneOffRef),
		"calendar", "events", "delete", "--", oneOffRef)

	rows := occurrencesOf(t, series, "2027-05-18", "2027-05-18")
	if len(rows) != 1 {
		t.Errorf("a one-day window listed %d occurrences of a daily all-day series, want 1", len(rows))
	}
	if rows := occurrencesOf(t, oneOff, "2027-05-18", "2027-05-18"); len(rows) != 0 {
		t.Errorf("a one-day window listed an all-day event belonging to the next day: %+v", rows)
	}
}

// The last day named is a whole day, not the instant it begins.
func TestCalendarListIncludesTheLastDayNamed(t *testing.T) {
	t.Parallel()
	title := testID() + "-lastday"
	ref := strings.TrimSpace(runOK(t, "calendar", "events", "create",
		"--calendar", "Default", "--title", title,
		"--start", "2027-06-10T23:30", "--duration", "15m"))
	cleanupRun(t, fmt.Sprintf("Delete event: proton calendar events delete -- %s", ref),
		"calendar", "events", "delete", "--", ref)

	if rows := occurrencesOf(t, title, "2027-06-10", "2027-06-10"); len(rows) != 1 {
		t.Errorf("an event late on the last day named listed %d rows, want 1", len(rows))
	}
}

// Proton reads an absent reminder list as "use the calendar's defaults", so
// sending one on every write silently resets whatever the event had. 42 minutes is
// a value no default uses.
func TestCalendarUpdatingAnEventKeepsItsReminders(t *testing.T) {
	t.Parallel()
	title := testID() + "-remind"
	ref := strings.TrimSpace(runOK(t, "calendar", "events", "create",
		"--calendar", "Default", "--title", title,
		"--start", "2027-07-01T09:00", "--duration", "30m", "--remind", "42m"))
	cleanupRun(t, fmt.Sprintf("Delete event: proton calendar events delete -- %s", ref),
		"calendar", "events", "delete", "--", ref)

	if got := triggersOf(t, ref); !contains(got, "-PT42M") {
		t.Fatalf("the event was created with reminders %v, want -PT42M", got)
	}
	runOK(t, "calendar", "events", "update", "--title", title+"-renamed", "--", ref)
	if got := triggersOf(t, ref); !contains(got, "-PT42M") {
		t.Errorf("renaming the event left it with reminders %v, want -PT42M kept", got)
	}

	runOK(t, "calendar", "events", "update", "--remind", "5m", "--", ref)
	if got := triggersOf(t, ref); !contains(got, "-PT5M") || contains(got, "-PT42M") {
		t.Errorf("--remind left the event with %v, want only -PT5M", got)
	}
	runOK(t, "calendar", "events", "update", "--no-remind", "--", ref)
	if got := triggersOf(t, ref); len(got) != 0 {
		t.Errorf("--no-remind left the event with %v", got)
	}
}

// triggersOf reads an event's reminder triggers straight from the API, so the
// assertion does not depend on how the CLI renders them.
func triggersOf(t *testing.T, ref string) []string {
	t.Helper()
	data := runJSON(t, "api", "GET", eventPath(t, ref))
	ev, _ := data["Event"].(map[string]interface{})
	notifs, _ := ev["Notifications"].([]interface{})
	var out []string
	for _, n := range notifs {
		if m, ok := n.(map[string]interface{}); ok {
			if trig, _ := m["Trigger"].(string); trig != "" {
				out = append(out, trig)
			}
		}
	}
	return out
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

// Editing one occurrence writes a second record that replaces it. The rest of the
// series has to be untouched, and the edited occurrence keeps the reference it had
// before it was edited.
func TestCalendarOccurrenceUpdateLeavesTheSeriesAlone(t *testing.T) {
	t.Parallel()
	title := testID() + "-single"
	createSeries(t, title, seriesAnchor+"T09:00", "FREQ=WEEKLY;COUNT=4")

	refs := occurrenceRefs(t, title, seriesAnchor, "2027-03-29")
	if len(refs) != 4 {
		t.Fatalf("the series listed %d occurrences, want 4", len(refs))
	}
	second := refs[1]
	runOK(t, "calendar", "events", "update", "--title", title+"-moved",
		"--start", "2027-03-08T14:00", "--duration", "45m", "--", second)

	after := occurrencesOf(t, title, seriesAnchor, "2027-03-29")
	if len(after) != 4 {
		t.Fatalf("editing one occurrence left %d rows, want the series intact", len(after))
	}
	var edited map[string]interface{}
	for _, row := range after {
		if got, _ := row["title"].(string); strings.Contains(got, "-moved") {
			edited = row
		}
	}
	if edited == nil {
		t.Fatal("the edited occurrence is not in the window")
	}
	if occ, _ := edited["occurrence"].(string); occ != "2027-03-08T09:00" {
		t.Errorf("the edited occurrence is named %q, want its original start", occ)
	}
	if stored, _ := edited["stored_id"].(string); stored == "" {
		t.Error("the edited occurrence does not report the record backing it")
	}
	// Re-addressing it by the same reference finds the edit rather than the rule's
	// version of that day.
	assertContains(t, runOK(t, "calendar", "events", "get", "--", second), title+"-moved")
}

// Cancelling one occurrence has to leave the rule alone, and has to stick: an
// exclusion written in the wrong frame does not cancel anything.
func TestCalendarOccurrenceDeleteLeavesTheSeriesAloneAndSticks(t *testing.T) {
	t.Parallel()
	title := testID() + "-cancel"
	createSeries(t, title, seriesAnchor+"T09:00", "FREQ=WEEKLY;COUNT=4")

	refs := occurrenceRefs(t, title, seriesAnchor, "2027-03-29")
	if len(refs) != 4 {
		t.Fatalf("the series listed %d occurrences, want 4", len(refs))
	}
	runOK(t, "calendar", "events", "delete", "--yes", "--", refs[2])

	after := occurrenceRefs(t, title, seriesAnchor, "2027-03-29")
	if len(after) != 3 {
		t.Fatalf("cancelling one occurrence left %d, want 3", len(after))
	}
	if contains(after, refs[2]) {
		t.Error("the cancelled occurrence is still listed")
	}

	// Deleting it again changes nothing rather than accumulating exclusions.
	runOK(t, "calendar", "events", "delete", "--yes", "--", refs[2])
	if again := occurrenceRefs(t, title, seriesAnchor, "2027-03-29"); len(again) != 3 {
		t.Errorf("cancelling the same occurrence twice left %d occurrences, want 3", len(again))
	}
}

// Renaming a series must not resurrect the occurrences somebody cancelled, which
// is what happens when an update rebuilds the event instead of merging into it.
func TestCalendarSeriesUpdateKeepsItsExclusions(t *testing.T) {
	t.Parallel()
	title := testID() + "-exdate"
	ref := createSeries(t, title, seriesAnchor+"T09:00", "FREQ=WEEKLY;COUNT=4")

	refs := occurrenceRefs(t, title, seriesAnchor, "2027-03-29")
	if len(refs) != 4 {
		t.Fatalf("the series listed %d occurrences, want 4", len(refs))
	}
	runOK(t, "calendar", "events", "delete", "--yes", "--", refs[1])
	runOK(t, "calendar", "events", "update", "--title", title+"-renamed", "--", ref)

	after := occurrencesOf(t, title, seriesAnchor, "2027-03-29")
	if len(after) != 3 {
		t.Errorf("renaming the series left %d occurrences, want the cancelled one still gone", len(after))
	}
	for _, row := range after {
		if got, _ := row["title"].(string); !strings.Contains(got, "-renamed") {
			t.Errorf("an occurrence still reads %q after the rename", got)
		}
	}
}

// Ending a series at one occurrence keeps everything before it and removes
// everything from it on.
func TestCalendarOnwardsEndsTheSeriesAtAnOccurrence(t *testing.T) {
	t.Parallel()
	title := testID() + "-onwards"
	createSeries(t, title, seriesAnchor+"T09:00", "FREQ=WEEKLY;COUNT=5")

	refs := occurrenceRefs(t, title, seriesAnchor, "2027-04-05")
	if len(refs) != 5 {
		t.Fatalf("the series listed %d occurrences, want 5", len(refs))
	}
	runOK(t, "calendar", "events", "delete", "--onwards", "--yes", "--", refs[2])

	after := occurrenceRefs(t, title, seriesAnchor, "2027-04-05")
	if len(after) != 2 {
		t.Fatalf("ending the series at the third occurrence left %d, want 2", len(after))
	}
	for _, r := range after[:2] {
		if !contains(refs[:2], r) {
			t.Errorf("%q is not one of the occurrences before the split", r)
		}
	}
}

// Changing one occurrence and every later one keeps the earlier ones as they were
// and starts a second series from the split.
func TestCalendarOnwardsUpdateSplitsTheSeries(t *testing.T) {
	t.Parallel()
	title := testID() + "-split"
	createSeries(t, title, seriesAnchor+"T09:00", "FREQ=WEEKLY;COUNT=5")

	refs := occurrenceRefs(t, title, seriesAnchor, "2027-04-05")
	if len(refs) != 5 {
		t.Fatalf("the series listed %d occurrences, want 5", len(refs))
	}
	runOK(t, "calendar", "events", "update", "--onwards",
		"--title", title+"-later", "--start", "2027-03-15T11:00", "--duration", "30m", "--", refs[2])

	after := occurrencesOf(t, title, seriesAnchor, "2027-04-05")
	if len(after) != 5 {
		t.Fatalf("splitting the series left %d occurrences, want 5", len(after))
	}
	early, late := 0, 0
	for _, row := range after {
		got, _ := row["title"].(string)
		if strings.Contains(got, "-later") {
			late++
			continue
		}
		early++
	}
	if early != 2 || late != 3 {
		t.Errorf("the split left %d occurrences before and %d after, want 2 and 3", early, late)
	}
	// The remainder has its own identity, so the two halves are separate series.
	var uids []string
	for _, row := range after {
		if uid, _ := row["uid"].(string); uid != "" && !contains(uids, uid) {
			uids = append(uids, uid)
		}
	}
	if len(uids) != 2 {
		t.Errorf("the split produced %d identities, want 2", len(uids))
	}

	// Any occurrence created by the split has to be deletable on its own.
	for _, row := range after {
		if got, _ := row["title"].(string); strings.Contains(got, "-later") {
			cal, _ := row["calendar_id"].(string)
			id, _ := row["id"].(string)
			cleanupRun(t, fmt.Sprintf("Delete split remainder: proton calendar events delete -- %s/%s", cal, id),
				"calendar", "events", "delete", "--yes", "--", cal+"/"+id)
			break
		}
	}
}

// Ending a series at its own first occurrence would leave nothing, so it is
// refused rather than quietly removing everything.
func TestCalendarOnwardsRefusesTheFirstOccurrence(t *testing.T) {
	t.Parallel()
	title := testID() + "-firstocc"
	createSeries(t, title, seriesAnchor+"T09:00", "FREQ=WEEKLY;COUNT=3")

	refs := occurrenceRefs(t, title, seriesAnchor, "2027-03-22")
	if len(refs) == 0 {
		t.Fatal("the series listed no occurrences")
	}
	_, stderr, code := run(t, "calendar", "events", "delete", "--onwards", "--yes", "--", refs[0])
	if code == 0 {
		t.Error("ending a series at its first occurrence was accepted")
	}
	assertContains(t, stderr, "first occurrence")
}

// Deleting the series removes every occurrence, and says how many before it does.
func TestCalendarDeletingASeriesRemovesEveryOccurrence(t *testing.T) {
	t.Parallel()
	title := testID() + "-whole"
	ref := createSeries(t, title, seriesAnchor+"T09:00", "FREQ=WEEKLY;COUNT=4")

	_, stderr := runOKStderr(t, "calendar", "events", "delete", "--dry-run", "--", ref)
	assertContains(t, stderr, "all 4 occurrences of it")

	runOK(t, "calendar", "events", "delete", "--yes", "--", ref)
	if after := occurrenceRefs(t, title, seriesAnchor, "2027-03-29"); len(after) != 0 {
		t.Errorf("deleting the series left %d occurrences", len(after))
	}
}

// A series anchored to a zone keeps its wall-clock time when the clocks change.
// Stored as a plain UTC instant it slides by an hour, which is the whole reason an
// event carries a zone. European summer time ends on 31 October 2027.
func TestCalendarRecurringSeriesKeepsItsWallClockAcrossADaylightSavingChange(t *testing.T) {
	t.Parallel()
	title := testID() + "-dst"
	createSeries(t, title, "2027-10-28T09:00", "FREQ=DAILY;COUNT=6", "--zone", "Europe/Vienna")

	rows := occurrencesOf(t, title, "2027-10-28", "2027-11-02")
	if len(rows) != 6 {
		t.Fatalf("the series listed %d occurrences, want 6", len(rows))
	}
	for _, row := range rows {
		occ, _ := row["occurrence"].(string)
		if !strings.HasSuffix(occ, "T09:00") {
			t.Errorf("an occurrence reads %q, want every one at 09:00", occ)
		}
		if zone, _ := row["zone"].(string); zone != "Europe/Vienna" {
			t.Errorf("an occurrence is anchored to %q", zone)
		}
	}
}

// Changing the rule is a change to the event like any other.
func TestCalendarSeriesRuleCanBeChanged(t *testing.T) {
	t.Parallel()
	title := testID() + "-rerule"
	ref := createSeries(t, title, seriesAnchor+"T09:00", "FREQ=WEEKLY;COUNT=3")

	runOK(t, "calendar", "events", "update", "--rrule", "FREQ=DAILY;COUNT=4", "--", ref)
	got := runOK(t, "calendar", "events", "get", "--", ref)
	assertContains(t, got, "FREQ=DAILY")

	if rows := occurrencesOf(t, title, seriesAnchor, "2027-03-08"); len(rows) != 4 {
		t.Errorf("the re-ruled series listed %d occurrences, want 4", len(rows))
	}
}

// An answer applies to a whole invitation, so a reference naming one occurrence is
// refused rather than half-honoured.
func TestCalendarRespondRefusesAnOccurrenceReference(t *testing.T) {
	t.Parallel()
	title := testID() + "-rsvpocc"
	createSeries(t, title, seriesAnchor+"T09:00", "FREQ=WEEKLY;COUNT=2")

	refs := occurrenceRefs(t, title, seriesAnchor, "2027-03-08")
	if len(refs) == 0 {
		t.Fatal("the series listed no occurrences")
	}
	_, stderr, code := run(t, "calendar", "events", "respond", "--answer", "accept", "--", refs[0])
	if code == 0 {
		t.Error("responding to one occurrence was accepted")
	}
	assertContains(t, stderr, "whole series")
}

// Proton's composer takes an end time, so this one does too. It has to land on
// the same event a duration would have made, or the two spellings mean different
// things.
func TestCalendarEventEndIsTheSameAsADuration(t *testing.T) {
	t.Parallel()
	title := testID() + "-end"
	ref := strings.TrimSpace(runOK(t, "calendar", "events", "create",
		"--calendar", "Default", "--title", title,
		"--start", "2027-08-03T09:00", "--end", "2027-08-03T10:30"))
	cleanupRun(t, fmt.Sprintf("Delete event: proton calendar events delete -- %s", ref),
		"calendar", "events", "delete", "--", ref)

	got := runJSON(t, "calendar", "events", "get", "--", ref)
	start, _ := got["start"].(string)
	end, _ := got["end"].(string)
	if !strings.Contains(start, "2027-08-03T09:00") || !strings.Contains(end, "2027-08-03T10:30") {
		t.Errorf("--end gave start %q end %q, want 09:00 to 10:30", start, end)
	}
}

// A reminder says how it is delivered, and what a listing prints has to be what
// --remind would accept: an event you can read is an event you can recreate.
func TestCalendarEventEmailReminderRoundTrips(t *testing.T) {
	t.Parallel()
	title := testID() + "-remind-email"
	ref := strings.TrimSpace(runOK(t, "calendar", "events", "create",
		"--calendar", "Default", "--title", title,
		"--start", "2027-08-10T09:00", "--duration", "30m",
		"--remind", "1d:email", "--remind", "17m"))
	cleanupRun(t, fmt.Sprintf("Delete event: proton calendar events delete -- %s", ref),
		"calendar", "events", "delete", "--", ref)

	// Proton stores the delivery as the notification's Type: 0 emails, 1 is the
	// device. Read from the API so the assertion does not rest on our own words.
	data := runJSON(t, "api", "GET", eventPath(t, ref))
	ev, _ := data["Event"].(map[string]interface{})
	notifs, _ := ev["Notifications"].([]interface{})
	kinds := map[string]float64{}
	for _, n := range notifs {
		if m, ok := n.(map[string]interface{}); ok {
			trig, _ := m["Trigger"].(string)
			kind, _ := m["Type"].(float64)
			kinds[trig] = kind
		}
	}
	if kinds["-P1D"] != 0 {
		t.Errorf("1d:email stored as Type %v, want 0 (email)", kinds["-P1D"])
	}
	if kinds["-PT17M"] != 1 {
		t.Errorf("a bare 17m stored as Type %v, want 1 (device)", kinds["-PT17M"])
	}

	// And it comes back spelled the way it was written.
	got := runJSON(t, "calendar", "events", "get", "--", ref)
	reminders, _ := got["reminders"].([]interface{})
	var printed []string
	for _, r := range reminders {
		if s, ok := r.(string); ok {
			printed = append(printed, s)
		}
	}
	if !contains(printed, "1d:email") || !contains(printed, "17m") {
		t.Errorf("reminders printed as %v, want 1d:email and 17m", printed)
	}
}

// An export has to be a file another client can open: one VCALENDAR, no METHOD,
// and a series written once with its rule rather than expanded.
func TestCalendarEventsExportWritesAnICSFile(t *testing.T) {
	t.Parallel()
	title := testID() + "-export"
	ref := strings.TrimSpace(runOK(t, "calendar", "events", "create",
		"--calendar", "Default", "--title", title,
		"--start", "2027-09-06T09:00", "--duration", "15m",
		"--rrule", "FREQ=WEEKLY;COUNT=5", "--remind", "10m"))
	cleanupRun(t, fmt.Sprintf("Delete event: proton calendar events delete -- %s", ref),
		"calendar", "events", "delete", "--", ref)

	out := runOK(t, "calendar", "events", "export",
		"--start", "2027-09-01", "--end", "2027-09-30", "--dest", "-")

	for _, want := range []string{"BEGIN:VCALENDAR", "END:VCALENDAR", "BEGIN:VEVENT", title} {
		if !strings.Contains(out, want) {
			t.Errorf("the export is missing %q", want)
		}
	}
	if strings.Contains(out, "METHOD") {
		t.Error("a calendar file must carry no METHOD; that is what makes it a file and not an invitation")
	}
	if !strings.Contains(out, "RRULE:FREQ=WEEKLY") {
		t.Error("a series should be exported once with its rule, not expanded")
	}
	if got := strings.Count(out, title); got != 1 {
		t.Errorf("the series appears %d times; expanding it would make five unrelated events", got)
	}
	if !strings.Contains(out, "BEGIN:VALARM") || !strings.Contains(out, "TRIGGER:-PT10M") {
		t.Error("reminders should travel as VALARM components")
	}
}

// An event's own status is not an attendee's answer, so cancelling one keeps it
// and its history rather than removing it.
func TestCalendarEventStatusIsStoredAndRead(t *testing.T) {
	t.Parallel()
	title := testID() + "-status"
	ref := strings.TrimSpace(runOK(t, "calendar", "events", "create",
		"--calendar", "Default", "--title", title,
		"--start", "2027-09-20T09:00", "--duration", "30m", "--status", "tentative"))
	cleanupRun(t, fmt.Sprintf("Delete event: proton calendar events delete -- %s", ref),
		"calendar", "events", "delete", "--", ref)

	if got := runJSON(t, "calendar", "events", "get", "--", ref)["status"]; got != "tentative" {
		t.Errorf("status came back as %v, want tentative", got)
	}

	runOK(t, "calendar", "events", "update", "--status", "cancelled", "--", ref)
	if got := runJSON(t, "calendar", "events", "get", "--", ref)["status"]; got != "cancelled" {
		t.Errorf("after cancelling, status is %v, want cancelled", got)
	}
}

// An event with no status is confirmed - that is what every client reads an
// absent STATUS as, so the CLI says so rather than leaving it blank.
func TestCalendarEventWithoutAStatusIsConfirmed(t *testing.T) {
	t.Parallel()
	title := testID() + "-nostatus"
	ref := strings.TrimSpace(runOK(t, "calendar", "events", "create",
		"--calendar", "Default", "--title", title,
		"--start", "2027-09-27T09:00", "--duration", "30m"))
	cleanupRun(t, fmt.Sprintf("Delete event: proton calendar events delete -- %s", ref),
		"calendar", "events", "delete", "--", ref)

	if got := runJSON(t, "calendar", "events", "get", "--", ref)["status"]; got != "confirmed" {
		t.Errorf("an event that never said is %v, want confirmed", got)
	}
}

// What a calendar gives a new event is stored per calendar, which is where
// Proton keeps it: a work calendar can open half-hour meetings with a reminder
// while a personal one does not.
func TestCalendarDefaultsAreReadAndWritten(t *testing.T) {
	t.Parallel()
	lease(t, calendarDefaults)

	before := runJSON(t, "calendar", "settings", "calendars", "get", "Default")
	defaults, _ := before["defaults"].(map[string]interface{})
	if defaults == nil {
		t.Fatal("calendars get should report the calendar's defaults")
	}
	wasDuration, _ := defaults["default_duration_minutes"].(float64)
	if wasDuration == 0 {
		t.Fatal("a calendar always has a default event duration")
	}
	wasBusy, _ := defaults["shows_as_busy"].(bool)

	// Put back exactly what was there, whichever assertion below fails first.
	cleanup(t, fmt.Sprintf("Restore calendar defaults: proton calendar settings calendars update Default --default-duration %dm --busy %s",
		int(wasDuration), onOff(wasBusy)),
		func() error {
			_, _, code := run(t, "calendar", "settings", "calendars", "update",
				"--default-duration", fmt.Sprintf("%dm", int(wasDuration)),
				"--busy", onOff(wasBusy), "Default")
			if code != 0 {
				return fmt.Errorf("restore exit %d", code)
			}
			return nil
		})

	target := 45
	if int(wasDuration) == target {
		target = 30
	}
	runOK(t, "calendar", "settings", "calendars", "update",
		"--default-duration", fmt.Sprintf("%dm", target),
		"--busy", onOff(!wasBusy), "Default")

	after, _ := runJSON(t, "calendar", "settings", "calendars", "get", "Default")["defaults"].(map[string]interface{})
	if got, _ := after["default_duration_minutes"].(float64); int(got) != target {
		t.Errorf("default duration is %v, want %d", got, target)
	}
	if got, _ := after["shows_as_busy"].(bool); got != !wasBusy {
		t.Errorf("shows-as-busy is %v, want %v", got, !wasBusy)
	}
}

// editEventNamed adds a line to the VEVENT whose SUMMARY is title, leaving every
// other event in the file alone.
func editEventNamed(t *testing.T, ics, title, line string) string {
	t.Helper()
	const begin = "BEGIN:VEVENT"
	parts := strings.Split(ics, begin)
	for i := 1; i < len(parts); i++ {
		if strings.Contains(parts[i], title) {
			parts[i] = "\r\n" + line + parts[i]
			return strings.Join(parts, begin)
		}
	}
	t.Fatalf("the export holds no event called %s", title)
	return ""
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

// Export and import are each other's inverse, so a file written here reads back
// as the same events - which is the only thing that makes an export a backup.
func TestCalendarEventsImportRoundTripsAnExport(t *testing.T) {
	t.Parallel()
	title := testID() + "-roundtrip"
	day := "2027-10-04"
	runOK(t, "calendar", "events", "create",
		"--calendar", "Default", "--title", title,
		"--start", day+"T09:00", "--duration", "45m", "--remind", "20m")
	// Overwriting by UID replaces the event, and the replacement has an ID of its
	// own, so cleanup goes looking by name rather than holding the first one.
	cleanup(t, "Delete event: proton calendar events list, then delete the one called "+title, func() error {
		for _, ref := range eventsTitled(t, day, title) {
			if _, stderr, code, err := runArgs(nil, "--yes", "calendar", "events", "delete", "--", ref); err != nil {
				return err
			} else if code != 0 {
				return fmt.Errorf("exit %d: %s", code, stderr)
			}
		}
		return nil
	})

	file := filepath.Join(t.TempDir(), "export.ics")
	runOK(t, "calendar", "events", "export",
		"--start", "2027-10-01", "--end", "2027-10-31", "--dest", file)

	// And back in. An event carries the UID of the event it is, so reading the
	// file back changes that event rather than making a second one - which is the
	// difference between a backup and a way to fill a calendar with duplicates.
	written, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read the export: %v", err)
	}
	// The file may hold other events from the same window, so the location goes
	// on the one this test made rather than on whichever came first.
	edited := editEventNamed(t, string(written), title, "LOCATION:Room 12")
	if err := os.WriteFile(file, []byte(edited), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	runOK(t, "calendar", "events", "import", "--calendar", "Default", file)

	copies := eventsTitled(t, day, title)
	if len(copies) != 1 {
		t.Fatalf("re-importing an export made %d events out of one", len(copies))
	}
	assertContains(t, runOK(t, "calendar", "events", "get", "--", copies[0]), "Room 12")
}

// eventsTitled returns the references of every event on one day with that title.
func eventsTitled(t *testing.T, day, title string) []string {
	t.Helper()
	var refs []string
	for _, row := range runJSONArray(t, "calendar", "events", "list", "--start", day, "--end", day) {
		m, _ := row.(map[string]interface{})
		if s, _ := m["title"].(string); s != title {
			continue
		}
		id, _ := m["calendar_id"].(string)
		eid, _ := m["id"].(string)
		refs = append(refs, id+"/"+eid)
	}
	return refs
}

// A file that holds nothing usable is refused before anything is written, and
// the refusal says which file.
func TestCalendarEventsImportRefusesAFileWithNoEvents(t *testing.T) {
	t.Parallel()
	empty := filepath.Join(t.TempDir(), "empty.ics")
	if err := os.WriteFile(empty, []byte("BEGIN:VCALENDAR\r\nEND:VCALENDAR\r\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, stderr, code := run(t, "calendar", "events", "import", empty)
	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	if !strings.Contains(stderr, "holds no events") {
		t.Errorf("stderr should say the file holds no events, got: %s", stderr)
	}
}

// A reminder is the other side of an event: `reminders list` answers what will
// interrupt you on those days, where the event itself answers what is on them.
//
// A brand-new event's alarm is not served on the alarms endpoint the moment the
// event is created - the server materialises it shortly after - so the test
// waits for it rather than asserting on the first read.
func TestCalendarRemindersListReportsDue(t *testing.T) {
	t.Parallel()
	calID := firstCalendarID(t)

	today := time.Now().Format("2006-01-02")
	start := time.Now().Add(20 * time.Minute).Format("2006-01-02T15:04")
	title := testID() + "-remindo"
	location := title + "-room"
	eventID := strings.TrimSpace(runOK(t, "calendar", "events", "create",
		"--calendar", calID, "--title", title, "--location", location,
		"--start", start, "--duration", "15m", "--remind", "15m"))
	cleanupRun(t, fmt.Sprintf("Delete event: proton calendar events delete %s", eventID),
		"calendar", "events", "delete", eventID)

	// The alarm appears only once the server has materialised it; poll until it
	// does, then assert it reads a reminder. The wording lives in the JSON `says`
	// field, not in the text columns, so it is asserted there.
	waitFor(60*time.Second, 2*time.Second, func() bool {
		return strings.Contains(runOK(t, "calendar", "reminders", "list", "--start", today, "--end", today), title)
	})
	got := runJSON(t, "calendar", "reminders", "list", "--start", today, "--end", today)
	for _, raw := range got["reminders"].([]interface{}) {
		row := raw.(map[string]interface{})
		if row["title"] != title {
			continue
		}
		if row["remind"] != "15m" {
			t.Errorf("remind = %v, want 15m", row["remind"])
		}
		// A reminder reports the event it warns about, whole, so what the event
		// says about itself is on the row rather than a lookup away.
		if row["location"] != location {
			t.Errorf("location = %v, want %q", row["location"], location)
		}
		says, _ := row["says"].(string)
		if !strings.Contains(says, "starts") && !strings.Contains(says, "started") {
			t.Errorf("says = %q, want a starts/started wording", says)
		}
		return
	}
	t.Fatalf("created event %q never appeared in reminders list", title)
}

// `watch` sleeps until the moment and prints it, so an event about to remind
// produces a line at its firing second rather than at the next poll.
func TestCalendarRemindersWatchRaisesOnTime(t *testing.T) {
	t.Parallel()
	calID := firstCalendarID(t)

	// The reminder has to still be ahead of us when the watch comes up, and the
	// arithmetic is what guarantees it rather than luck. A start carries minutes,
	// so four minutes out lands between three and four minutes away once the
	// seconds are dropped, and a minute's warning puts the reminder at least two
	// minutes off - more than the minute the alarm is given to materialise and the
	// ten seconds the watch is given to come up.
	today := time.Now().Format("2006-01-02")
	start := time.Now().Add(4 * time.Minute).Format("2006-01-02T15:04")
	title := testID() + "-remindw"
	eventID := strings.TrimSpace(runOK(t, "calendar", "events", "create",
		"--calendar", calID, "--title", title,
		"--start", start, "--duration", "10m", "--remind", "1m"))
	cleanupRun(t, fmt.Sprintf("Delete event: proton calendar events delete %s", eventID),
		"calendar", "events", "delete", eventID)

	// Start the watch only once the alarm exists, so its first read finds it
	// rather than racing the server materialising it. A minute is long enough that
	// running out of it is Proton being slow rather than this being early, and
	// saying so beats letting the watch look for something that is not there yet.
	if !waitFor(60*time.Second, 2*time.Second, func() bool {
		return strings.Contains(runOK(t, "calendar", "reminders", "list", "--start", today, "--end", today), title)
	}) {
		t.Fatalf("the alarm for %q never materialised, so there was nothing to watch for", title)
	}
	w, err := watchAs(primary, "calendar", "reminders", "watch", "--calendar", calID)
	if err != nil {
		t.Fatalf("start reminders watch: %v", err)
	}
	defer w.stop(t)
	w.waitReady(t, 10*time.Second)

	// Long enough for the far end, measured from the near end: the reminder can be
	// a full three minutes out from when the event was made, and the alarm usually
	// materialises in a second or two, so almost all of that is spent here.
	line := w.waitForLine(t, 200*time.Second, func(l string) bool {
		return strings.Contains(l, title)
	})
	// The wording is Proton's - "starts at" for one ahead, "started at" for one
	// already begun - so accept either.
	if !strings.Contains(line, "starts") && !strings.Contains(line, "started") {
		t.Fatalf("reminder line %q does not read like a reminder", line)
	}
}
