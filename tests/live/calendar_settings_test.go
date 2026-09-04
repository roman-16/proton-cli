package live

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// Calendars themselves: making one, what a new event inherits from it, sharing
// it, and filling one from an address Proton fetches.
//
// Deleting a calendar is what exercises the scope-elevation path: Proton guards
// the endpoint behind a re-authentication the client performs when asked.

func TestCalendarCalendarsList(t *testing.T) {
	stdout := runOK(t, "calendar", "settings", "calendars", "list")
	assertContains(t, stdout, "NAME")
	assertContains(t, stdout, "COLOR")
}

func TestCalendarCalendarsListColorPopulated(t *testing.T) {
	cals := runJSONArray(t, "calendar", "settings", "calendars", "list")
	if len(cals) == 0 {
		t.Fatal("an account always has a calendar, and this one lists none")
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
	name := testID() + "-cal"
	id := assertBareID(t, runOK(t, "calendar", "settings", "calendars", "create",
		"--name", name, "--color", "#8080FF"), "calendars create")
	cleanupRun(t, fmt.Sprintf("Delete calendar: proton calendar settings calendars delete -- %s", id),
		"calendar", "settings", "calendars", "delete", "--", id)

	assertContains(t, runOK(t, "calendar", "settings", "calendars", "list"), name)

	// Deleting is what exercises the scope-elevation path, so it is asserted here
	// rather than left to cleanup, whose failure never fails a test.
	runOK(t, "calendar", "settings", "calendars", "delete", "--", id)
	assertNotContains(t, runOK(t, "calendar", "settings", "calendars", "list"), name)
}

func TestCalendarCalendarsRename(t *testing.T) {
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

// What a calendar gives a new event is stored per calendar, which is where
// Proton keeps it: a work calendar can open half-hour meetings with a reminder
// while a personal one does not.
func TestCalendarDefaultsAreReadAndWritten(t *testing.T) {
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

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

// A subscribed calendar is filled from an address Proton fetches rather than
// from events made here, which is a paid feature.
//
// It makes its own calendar and deletes it again, so the account comes back as
// it was - and the canary checks that rather than taking this test's word.
func TestCalendarSubscriptionIsFilledFromAnAddress(t *testing.T) {
	// An address Proton can actually fetch, so what is being tested is
	// subscribing rather than the address being unreachable. Checked by hand
	// against the validate endpoint, which answers 0 for it.
	const feed = "https://www.officeholidays.com/ics/austria"

	name := testID() + "-subscribed"
	ref := strings.TrimSpace(runOKPaid(t, "calendar", "settings", "calendars", "create",
		"--name", name, "--url", feed))
	cleanupRunPaid(t, "Delete the subscribed calendar: proton calendar settings calendars delete "+ref,
		"calendar", "settings", "calendars", "delete", ref)

	shown := runJSONPaid(t, "calendar", "settings", "calendars", "get", ref)
	if kind, _ := shown["kind"].(string); kind != "subscribed" {
		t.Errorf("the calendar came back as %q, want subscribed", kind)
	}
}

// An address Proton cannot read is refused before the calendar is made, so
// nobody is left with an empty calendar that never fills.
func TestCalendarSubscriptionRefusesAnAddressItCannotRead(t *testing.T) {
	name := testID() + "-bad-feed"
	_, stderr, code := runPaid(t, "--yes", "calendar", "settings", "calendars", "create",
		"--name", name, "--url", "https://example.com/not-a-calendar.ics")
	if code == 0 {
		t.Fatal("an address holding no calendar was accepted")
	}
	// The refusal has to be about the address, which is why the assertion is on
	// the wording rather than only on the exit code.
	assertContains(t, stderr, "nothing at that address")
	// Proton's own account of it is more specific than any wording here, so it
	// is passed through rather than replaced.
	assertContains(t, stderr, "404")
	// Nothing was made, so there is nothing to clean up - which the canary will
	// confirm at the end of the run.
}

// Sharing a calendar hands somebody the key that opens it, encrypted to theirs
// and signed with yours. This is the whole round trip: the paid account makes a
// calendar, gives it to the second free account, and takes it back.
//
// The calendar is this test's own and is deleted at the end, so nothing of the
// account's own is shared even for a moment.
func TestCalendarSharingRoundTrip(t *testing.T) {
	name := testID() + "-shared"
	out, stderr, code := runPaid(t, "--yes", "calendar", "settings", "calendars", "create", "--name", name)
	if code != 0 {
		t.Fatalf("could not make a calendar to share: %s", truncateOutput(stderr))
	}
	ref := strings.TrimSpace(out)
	cleanupRunPaid(t, "Delete the shared calendar: proton calendar settings calendars delete "+ref,
		"calendar", "settings", "calendars", "delete", ref)

	runOKPaid(t, "calendar", "settings", "calendars", "share", "add", ref, secondaryEmail())
	// Deleting the calendar takes the invitation with it, so the clean-up above
	// covers this even if the test stops before withdrawing it by hand.

	// Until it is answered the invitation is pending, and the other account sees
	// nothing.
	var invited bool
	for _, row := range runJSONArrayPaid(t, "calendar", "settings", "calendars", "share", "list", ref) {
		m, _ := row.(map[string]interface{})
		if email, _ := m["email"].(string); !strings.EqualFold(email, secondaryEmail()) {
			continue
		}
		invited = true
		if status, _ := m["status"].(string); status != "pending" {
			t.Errorf("a fresh invitation is %q, want pending", status)
		}
		if access, _ := m["access"].(string); access != "viewer" {
			t.Errorf("access is %q, want viewer", access)
		}
	}
	if !invited {
		t.Fatal("the second account is not listed on the calendar it was given")
	}

	// The other side takes it, which is the half that proves the key it was
	// handed actually opens the calendar.
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
		t.Fatal("the invitation never reached the second account")
	}
	runOKSecondary(t, "calendar", "invitations", "accept", "--", invitationID)

	// Once accepted it is a calendar like any other on that account, which is
	// only true if the passphrase it was given opened.
	var got bool
	waitFor(60*time.Second, 3*time.Second, func() bool {
		for _, row := range runJSONArraySecondary(t, "calendar", "settings", "calendars", "list") {
			m, _ := row.(map[string]interface{})
			if n, _ := m["name"].(string); n == name {
				got = true
				return true
			}
		}
		return false
	})
	if !got {
		t.Error("the calendar did not appear on the account that accepted it")
	}

	// And it is listed as a member rather than an invitation now, so ending it
	// goes through the other endpoint.
	var active bool
	for _, row := range runJSONArrayPaid(t, "calendar", "settings", "calendars", "share", "list", ref) {
		m, _ := row.(map[string]interface{})
		if email, _ := m["email"].(string); !strings.EqualFold(email, secondaryEmail()) {
			continue
		}
		if status, _ := m["status"].(string); status == "active" {
			active = true
		}
	}
	if !active {
		t.Error("after accepting, the second account is not an active member")
	}

	runOKPaid(t, "calendar", "settings", "calendars", "share", "remove", ref, secondaryEmail())
	for _, row := range runJSONArrayPaid(t, "calendar", "settings", "calendars", "share", "list", ref) {
		m, _ := row.(map[string]interface{})
		if email, _ := m["email"].(string); strings.EqualFold(email, secondaryEmail()) {
			t.Error("the second account still has the calendar after being removed")
		}
	}
}

// An offer can be taken back before it is answered, which is a different
// endpoint from ending a membership somebody is already using.
func TestCalendarSharingWithdrawnBeforeAnswer(t *testing.T) {
	name := testID() + "-withdrawn"
	out, stderr, code := runPaid(t, "--yes", "calendar", "settings", "calendars", "create", "--name", name)
	if code != 0 {
		t.Fatalf("could not make a calendar: %s", truncateOutput(stderr))
	}
	ref := strings.TrimSpace(out)
	cleanupRunPaid(t, "Delete the calendar: proton calendar settings calendars delete "+ref,
		"calendar", "settings", "calendars", "delete", ref)

	runOKPaid(t, "calendar", "settings", "calendars", "share", "add", ref, secondaryEmail())

	// Nobody has answered, so this withdraws the invitation rather than ending a
	// membership.
	runOKPaid(t, "calendar", "settings", "calendars", "share", "remove", ref, secondaryEmail())
	for _, row := range runJSONArrayPaid(t, "calendar", "settings", "calendars", "share", "list", ref) {
		m, _ := row.(map[string]interface{})
		if email, _ := m["email"].(string); strings.EqualFold(email, secondaryEmail()) {
			t.Error("the invitation is still listed after being withdrawn")
		}
	}
}

// A calendar can only be given to another Proton account: what is handed over is
// encrypted to the recipient's key, and an address Proton holds no keys for has
// none to encrypt to.
func TestCalendarSharingNeedsAProtonAddress(t *testing.T) {
	name := testID() + "-outside"
	out, stderr, code := runPaid(t, "--yes", "calendar", "settings", "calendars", "create", "--name", name)
	if code != 0 {
		t.Fatalf("could not make a calendar: %s", truncateOutput(stderr))
	}
	ref := strings.TrimSpace(out)
	cleanupRunPaid(t, "Delete the calendar: proton calendar settings calendars delete "+ref,
		"calendar", "settings", "calendars", "delete", ref)

	_, stderr, code = runPaid(t, "--yes", "calendar", "settings", "calendars", "share",
		"add", ref, "nobody@example.com")
	if code == 0 {
		t.Fatal("an address outside Proton was accepted")
	}
	// Proton answers the key lookup first for an address it does not hold, so
	// the refusal is its own sentence rather than this tool's. Either way it
	// says the address is the problem, which is what somebody needs to read.
	if !strings.Contains(stderr, "not a Proton address") &&
		!strings.Contains(stderr, "address does not exist") {
		t.Errorf("the refusal does not name the address as the problem: %s", truncateOutput(stderr))
	}
}

func TestCalendarSettings(t *testing.T) {
	stdout := runOK(t, "calendar", "settings", "get")
	assertContains(t, stdout, "Primary Time Zone")
}

func TestCalendarSettingsSetListsKeys(t *testing.T) {
	stdout := runOK(t, "calendar", "settings", "list")
	assertContains(t, stdout, "primary-timezone")
	assertContains(t, stdout, "week-numbers")
}
