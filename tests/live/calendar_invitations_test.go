package live

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// Answering an invitation, and the invitations this account has been sent.
//
// The full round trip needs two Proton users: the secondary organizes an event
// and invites the primary, the primary answers, and the organizer's own copy is
// where the answer shows up.

func TestCalendarEventsRespondDryRun(t *testing.T) {
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
	secondaryCals := runJSONArraySecondary(t, "calendar", "settings", "calendars", "list")
	if len(secondaryCals) == 0 {
		t.Fatal("the second account lists no calendar to organize an invitation in")
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

// An answer applies to a whole invitation, so a reference naming one occurrence is
// refused rather than half-honoured.
func TestCalendarRespondRefusesAnOccurrenceReference(t *testing.T) {
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

// An invitation can be turned down, which takes it out of the way without
// opening anything.
func TestCalendarInvitationDeclined(t *testing.T) {
	name := testID() + "-declined"
	out, stderr, code := runPaid(t, "--yes", "calendar", "settings", "calendars", "create", "--name", name)
	if code != 0 {
		t.Fatalf("could not make a calendar: %s", truncateOutput(stderr))
	}
	ref := strings.TrimSpace(out)
	cleanupRunPaid(t, "Delete the calendar: proton calendar settings calendars delete "+ref,
		"calendar", "settings", "calendars", "delete", ref)

	runOKPaid(t, "calendar", "settings", "calendars", "share", "add", ref, secondaryEmail())

	var invitationID string
	waitFor(60*time.Second, 3*time.Second, func() bool {
		for _, row := range runJSONArraySecondary(t, "calendar", "invitations", "list") {
			m, _ := row.(map[string]interface{})
			if n, _ := m["name"].(string); n != name {
				continue
			}
			invitationID, _ = m["id"].(string)
			return invitationID != ""
		}
		return false
	})
	if invitationID == "" {
		t.Fatal("the second account never saw the invitation")
	}
	runOKSecondary(t, "calendar", "invitations", "decline", "--", invitationID)

	// Turning it down does not put the calendar on that account.
	for _, row := range runJSONArraySecondary(t, "calendar", "settings", "calendars", "list") {
		m, _ := row.(map[string]interface{})
		if n, _ := m["name"].(string); n == name {
			t.Error("a declined calendar turned up on the account anyway")
		}
	}
}
