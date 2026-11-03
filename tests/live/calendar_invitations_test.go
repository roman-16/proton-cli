package live

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/roman-16/proton-cli/tests/account"
)

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
	assertContains(t, runOK(t, "calendar", "events", "get", eventID), title)
}

func TestCalendarEventsRespondRejectsOrganizer(t *testing.T) {
	calID := firstCalendarID(t)
	title := testID() + "-rsvp-org"
	start := time.Now().Add(48 * time.Hour).Format("2006-01-02T15:04")
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

func answerOf(ev map[string]interface{}, email string) string {
	attendees, _ := ev["attendees"].([]interface{})
	for _, a := range attendees {
		m, _ := a.(map[string]interface{})
		if e, _ := m["email"].(string); strings.EqualFold(e, email) {
			answer, _ := m["answer"].(string)
			return answer
		}
	}
	return ""
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

	primaryCal := firstCalendarID(t)
	var primaryEventID string
	waitFor(45*time.Second, 3*time.Second, func() bool {
		evs := runJSONArray(t, "calendar", "events", "list", "--calendar", primaryCal,
			"--after", time.Now().Format("2006-01-02"),
			"--before", time.Now().Add(120*time.Hour).Format("2006-01-02"))
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
	primaryRef := primaryCal + "/" + primaryEventID
	cleanupRun(t, fmt.Sprintf("Delete primary event copy: proton calendar events delete %s", primaryRef),
		"calendar", "events", "delete", primaryRef)

	runOK(t, "calendar", "events", "respond", "--answer", "accept", primaryRef)

	if got := answerOf(runJSON(t, "calendar", "events", "get", primaryRef), selfEmail()); got != "accepted" {
		t.Errorf("the invitee's own copy says %q, want accepted", got)
	}
	if got := answerOf(runJSONSecondary(t, "calendar", "events", "get", secondaryEventID), selfEmail()); got != "accepted" {
		t.Errorf("the organizer's copy says %q, want accepted", got)
	}

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

	for _, row := range runJSONArraySecondary(t, "calendar", "settings", "calendars", "list") {
		m, _ := row.(map[string]interface{})
		if n, _ := m["name"].(string); n == name {
			t.Error("a declined calendar turned up on the account anyway")
		}
	}
}

func TestCalendarAnEmailedAnswerIsRecordedWhenRead(t *testing.T) {
	title := testID() + "-emailed-answer"
	start := time.Now().Add(96 * time.Hour).Truncate(time.Minute)
	eventID := strings.TrimSpace(runOK(t, "calendar", "events", "create",
		"--calendar", firstCalendarID(t), "--title", title, "--start", start.Format("2006-01-02T15:04"),
		"--duration", "30m", "--attendee", secondaryEmail(), "--no-meet"))
	cleanupRun(t, "Delete event: proton calendar events delete "+eventID, "calendar", "events", "delete", eventID)
	uid, _ := eventField(runJSON(t, "api", "GET", eventPath(t, eventID)), "UID").(string)
	if uid == "" {
		t.Fatal("could not read the event UID")
	}

	reply := filepath.Join(t.TempDir(), "invite.ics")
	ics := strings.Join([]string{
		"BEGIN:VCALENDAR", "VERSION:2.0", "PRODID:-//proton-cli live test//EN", "METHOD:REPLY",
		"BEGIN:VEVENT", "UID:" + uid, "DTSTAMP:" + time.Now().UTC().Format("20060102T150405Z"),
		"DTSTART:" + start.UTC().Format("20060102T150405Z"), "SEQUENCE:0",
		"ORGANIZER:mailto:" + selfEmail(), "ATTENDEE;PARTSTAT=TENTATIVE:mailto:" + secondaryEmail(),
		"END:VEVENT", "END:VCALENDAR", "",
	}, "\r\n")
	if err := os.WriteFile(reply, []byte(ics), 0o600); err != nil {
		t.Fatal(err)
	}
	subject := title + " answer"
	runOKSecondary(t, "mail", "messages", "send", "--to", selfEmail(), "--subject", subject,
		"--body", "Tentatively.", "--attach", reply)
	if sentID := findMessageAs(t, account.Secondary, "sent", subject); sentID != "" {
		cleanupRunSecondary(t, "Delete sent mail (secondary): proton --profile secondary mail messages delete "+sentID,
			"mail", "messages", "delete", "--", sentID)
	}
	inboxID := findMessage(t, "inbox", subject)
	if inboxID == "" {
		t.Fatal("the answer never reached the organizer's inbox")
	}
	cleanupRun(t, "Delete inbox mail: proton mail messages delete "+inboxID, "mail", "messages", "delete", "--", inboxID)

	_, stderr := runOKStderr(t, "--dry-run", "mail", "messages", "get", inboxID)
	assertContains(t, stderr, "Dry run")
	if got := answerOf(runJSON(t, "calendar", "events", "get", eventID), secondaryEmail()); got != "pending" {
		t.Errorf("a dry run recorded %q", got)
	}

	_, stderr = runOKStderr(t, "mail", "messages", "get", inboxID)
	assertContains(t, stderr, "tentative")
	if got := answerOf(runJSON(t, "calendar", "events", "get", eventID), secondaryEmail()); got != "tentative" {
		t.Errorf("after reading the answer the event says %q, want tentative", got)
	}

	if _, stderr = runOKStderr(t, "mail", "messages", "get", inboxID); strings.Contains(stderr, "Updated") {
		t.Errorf("reading the answer again recorded it again: %s", stderr)
	}
}
