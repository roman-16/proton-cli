package tests

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

func TestCalendarList(t *testing.T) {
	skipIfNoCredentials(t)
	stdout := runOK(t, "calendar", "list")
	assertContains(t, stdout, "ID")
	assertContains(t, stdout, "NAME")
}

func TestCalendarListJSON(t *testing.T) {
	skipIfNoCredentials(t)
	data := runJSON(t, "calendar", "list")
	if _, ok := data["Calendars"]; !ok {
		t.Error("expected Calendars in JSON output")
	}
}

func TestCalendarListEvents(t *testing.T) {
	skipIfNoCredentials(t)
	stdout := runOK(t, "calendar", "list-events")
	assertContains(t, stdout, "DATE")
	assertContains(t, stdout, "TITLE")
}

func TestCalendarListEventsDateRange(t *testing.T) {
	skipIfNoCredentials(t)
	// Create an event on a known date
	title := testID() + "-range"
	start := time.Now().Add(48 * time.Hour).Format("2006-01-02") + "T10:00"

	runOK(t, "calendar", "create-event", "--title", title, "--start", start, "--duration", "1h")

	// Cleanup: delete by title
	cleanupRun(t, fmt.Sprintf("Delete event: proton-cli calendar delete-event %q", title),
		"calendar", "delete-event", title)

	// List with date range that includes it
	startDate := time.Now().Format("2006-01-02")
	endDate := time.Now().Add(72 * time.Hour).Format("2006-01-02")
	stdout := runOK(t, "calendar", "list-events", "--start", startDate, "--end", endDate)
	assertContains(t, stdout, title)
}

func TestCalendarListEventsJSON(t *testing.T) {
	skipIfNoCredentials(t)
	data := runJSON(t, "calendar", "list-events")
	if _, ok := data["Events"]; !ok {
		t.Error("expected Events in JSON output")
	}
}

func TestCalendarCreateDeleteEvent(t *testing.T) {
	skipIfNoCredentials(t)
	title := testID() + "-event"

	// Create
	start := time.Now().Add(24 * time.Hour).Format("2006-01-02") + "T14:00"
	runOK(t, "calendar", "create-event", "--title", title, "--start", start, "--duration", "1h")

	cleanupRun(t, fmt.Sprintf("Delete event: proton-cli calendar delete-event %q", title),
		"calendar", "delete-event", title)

	// Verify in list
	stdout := runOK(t, "calendar", "list-events")
	assertContains(t, stdout, title)
}

func TestCalendarCreateEventWithLocation(t *testing.T) {
	skipIfNoCredentials(t)
	title := testID() + "-loc"
	start := time.Now().Add(24 * time.Hour).Format("2006-01-02") + "T15:00"

	runOK(t, "calendar", "create-event", "--title", title, "--location", "Vienna", "--start", start, "--duration", "2h")

	cleanupRun(t, fmt.Sprintf("Delete event: proton-cli calendar delete-event %q", title),
		"calendar", "delete-event", title)

	// Get by title and verify location
	stdout := runOK(t, "calendar", "get-event", title)
	assertContains(t, stdout, title)
	assertContains(t, stdout, "Vienna")
}

func TestCalendarGetEventByTitle(t *testing.T) {
	skipIfNoCredentials(t)
	title := testID() + "-gettitle"
	start := time.Now().Add(24 * time.Hour).Format("2006-01-02") + "T16:00"

	runOK(t, "calendar", "create-event", "--title", title, "--start", start, "--duration", "30m")

	cleanupRun(t, fmt.Sprintf("Delete event: proton-cli calendar delete-event %q", title),
		"calendar", "delete-event", title)

	stdout := runOK(t, "calendar", "get-event", title)
	assertContains(t, stdout, "Event:")
	assertContains(t, stdout, title)
	assertContains(t, stdout, "Duration:")
}

func TestCalendarGetEventByID(t *testing.T) {
	skipIfNoCredentials(t)
	title := testID() + "-getid"
	start := time.Now().Add(24 * time.Hour).Format("2006-01-02") + "T17:00"

	runOK(t, "calendar", "create-event", "--title", title, "--start", start, "--duration", "1h")

	cleanupRun(t, fmt.Sprintf("Delete event: proton-cli calendar delete-event %q", title),
		"calendar", "delete-event", title)

	// Get IDs from list-events --json
	data := runJSON(t, "calendar", "list-events")
	events := data["Events"].([]interface{})
	var calID, evtID string
	for _, e := range events {
		ev := e.(map[string]interface{})
		// Match by decrypted title
		if decrypted, ok := ev["DecryptedSharedEvents"].([]interface{}); ok {
			for _, d := range decrypted {
				if ds, ok := d.(string); ok {
					if contains(ds, title) {
						calID = ev["CalendarID"].(string)
						evtID = ev["ID"].(string)
						break
					}
				}
			}
		}
		if evtID != "" {
			break
		}
	}
	if evtID == "" {
		t.Fatal("could not find created event in list-events --json")
	}

	// Get by IDs
	stdout := runOK(t, "calendar", "get-event", calID, evtID)
	assertContains(t, stdout, title)
}

func TestCalendarDeleteEventByTitle(t *testing.T) {
	skipIfNoCredentials(t)
	title := testID() + "-deltitle"
	start := time.Now().Add(24 * time.Hour).Format("2006-01-02") + "T18:00"

	runOK(t, "calendar", "create-event", "--title", title, "--start", start, "--duration", "1h")

	// Delete by title
	runOK(t, "calendar", "delete-event", title)

	// Verify gone
	stdout := runOK(t, "calendar", "list-events")
	assertNotContains(t, stdout, title)
}

func TestCalendarCreateDeleteCalendar(t *testing.T) {
	skipIfNoCredentials(t)
	name := testID() + "-cal"

	stdout := runOK(t, "calendar", "create", "--name", name, "--color", "#8080FF")

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("failed to parse create output: %v", err)
	}
	calID := jsonField(result, "Calendar", "ID")
	if calID == "" {
		t.Fatal("no Calendar.ID in create response")
	}

	cleanupRun(t, fmt.Sprintf("Delete calendar: proton-cli calendar delete %s", calID),
		"calendar", "delete", calID)

	// Verify in list
	listOut := runOK(t, "calendar", "list")
	assertContains(t, listOut, calID)
}

// contains checks if a string contains a substring (used for iCal content).
func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
