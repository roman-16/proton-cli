package live

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/roman-16/proton-cli/tests/account"
)

// Reminders: the other side of an event.
//
// `events list` answers what is on those days; this answers what will interrupt
// you. A brand-new event's alarm is not served the moment the event is made -
// the server materialises it shortly after - so these wait for it rather than
// asserting on the first read.

// A reminder is the other side of an event: `reminders list` answers what will
// interrupt you on those days, where the event itself answers what is on them.
//
// A brand-new event's alarm is not served on the alarms endpoint the moment the
// event is created - the server materialises it shortly after - so the test
// waits for it rather than asserting on the first read.
func TestCalendarRemindersListReportsDue(t *testing.T) {
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
	w, err := watchAs(account.Primary, "calendar", "reminders", "watch", "--calendar", calID)
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
