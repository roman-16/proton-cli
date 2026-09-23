package live

import (
	"sort"
	"strings"
	"testing"

	"github.com/roman-16/proton-cli/tests/fixture"
)

// The public holidays calendars Proton offers, and adding one. Adding joins the
// calendar Proton keeps rather than making a copy, so what proves the add worked
// is reading the calendar's events: that is only possible if the membership it
// made opens.

func TestCalendarHolidaysListsWhatProtonOffers(t *testing.T) {
	rows := runJSONArray(t, "calendar", "settings", "holidays", "list")
	if len(rows) == 0 {
		t.Fatal("Proton offers no holidays calendars")
	}
	for _, row := range rows {
		m, _ := row.(map[string]interface{})
		for _, key := range []string{"id", "country", "country_code", "language", "language_code"} {
			if s, _ := m[key].(string); s == "" {
				t.Errorf("a holidays calendar has no %s: %v", key, m)
			}
		}
		if _, ok := m["added"].(bool); !ok {
			t.Errorf("a holidays calendar does not say whether it is added: %v", m)
		}
	}
	assertContains(t, runOK(t, "calendar", "settings", "holidays", "list"), "COUNTRY")
}

// A holidays calendar is added, read, changed the ways one can be, refused the
// ways it cannot, and deleted without a password.
//
// The country is the first the directory offers in a single language, the same
// on every run. Found already added, it is a leftover of an interrupted run on
// this test account, and it is deleted first so that leftovers cannot pile up
// against the free plan's limit of three calendars.
func TestCalendarHolidaysAreAddedReadAndDeleted(t *testing.T) {
	country := singleLanguageCountry(t)
	if country.added {
		runOK(t, "calendar", "settings", "calendars", "delete", "--", country.id)
	}

	id := assertBareID(t, runOK(t, "calendar", "settings", "calendars", "create",
		"--holidays", country.code), "calendars create --holidays")
	cleanupRun(t, "Delete the holidays calendar: proton calendar settings calendars delete -- "+id,
		"calendar", "settings", "calendars", "delete", "--", id)
	if id != country.id {
		t.Errorf("added as %s, want the directory's own ID %s", id, country.id)
	}

	shown := runJSON(t, "calendar", "settings", "calendars", "get", id)
	if kind, _ := shown["kind"].(string); kind != "holidays" {
		t.Errorf("the calendar came back as %q, want holidays", kind)
	}
	started, _ := shown["defaults"].(map[string]interface{})
	if busy, _ := started["shows_as_busy"].(bool); busy {
		t.Error("a new holidays calendar makes you look busy, where Proton's clients start one free")
	}
	if reminders, _ := started["default_all_day_reminders"].([]interface{}); len(reminders) != 0 {
		t.Errorf("a new holidays calendar starts with reminders %v, want none", reminders)
	}
	events := runJSONArray(t, "calendar", "events", "list", "--calendar="+id,
		"--after", fixture.Today(), "--before", fixture.InDays(365))
	if len(events) == 0 {
		t.Error("a year of public holidays holds no events, so the membership the add made does not open")
	}
	if !holidaysAdded(t, id) {
		t.Error("the directory does not list the calendar as added")
	}

	runOK(t, "calendar", "settings", "calendars", "update", "--remind-all-day", "1d", id)
	defaults, _ := runJSON(t, "calendar", "settings", "calendars", "get", id)["defaults"].(map[string]interface{})
	reminders, _ := defaults["default_all_day_reminders"].([]interface{})
	if len(reminders) != 1 || reminders[0] != "1d" {
		t.Errorf("all-day reminders are %v, want [1d]", reminders)
	}

	_, stderr, code := run(t, "calendar", "settings", "calendars", "update", "--remind", "1d", id)
	if code != 1 || !strings.Contains(stderr, "A holidays calendar takes only") {
		t.Errorf("a timed reminder on a holidays calendar: exit %d, %s", code, truncateOutput(stderr))
	}
	_, stderr, code = run(t, "calendar", "events", "create", "--calendar="+id,
		"--title", testID()+"-picnic", "--start", fixture.InDays(3), "--all-day")
	if code != 1 || !strings.Contains(stderr, "is read-only: its events are Proton's") {
		t.Errorf("an event made in a holidays calendar: exit %d, %s", code, truncateOutput(stderr))
	}
	_, stderr, code = run(t, "calendar", "settings", "calendars", "create", "--holidays", country.code)
	if code != 4 || !strings.Contains(stderr, "You already have the holidays calendar") {
		t.Errorf("adding the same holidays twice: exit %d, %s", code, truncateOutput(stderr))
	}

	runOK(t, "calendar", "settings", "calendars", "delete", "--", id)
	assertNotContains(t, runOK(t, "calendar", "settings", "calendars", "list", "--full-ids"), id)
	if holidaysAdded(t, id) {
		t.Error("the directory still lists the calendar as added after it was deleted")
	}
}

type offeredHolidays struct {
	id, code string
	added    bool
}

// singleLanguageCountry is the first country, by code, whose holidays Proton
// offers in one language only, so --holidays names it without --language.
func singleLanguageCountry(t *testing.T) offeredHolidays {
	t.Helper()
	byCode := map[string][]offeredHolidays{}
	for _, row := range runJSONArray(t, "calendar", "settings", "holidays", "list") {
		m, _ := row.(map[string]interface{})
		id, _ := m["id"].(string)
		code, _ := m["country_code"].(string)
		added, _ := m["added"].(bool)
		byCode[code] = append(byCode[code], offeredHolidays{id: id, code: code, added: added})
	}
	codes := make([]string, 0, len(byCode))
	for code, rows := range byCode {
		if len(rows) == 1 {
			codes = append(codes, code)
		}
	}
	if len(codes) == 0 {
		t.Fatal("Proton offers no country's holidays in a single language")
	}
	sort.Strings(codes)
	return byCode[codes[0]][0]
}

func holidaysAdded(t *testing.T, id string) bool {
	t.Helper()
	for _, row := range runJSONArray(t, "calendar", "settings", "holidays", "list") {
		m, _ := row.(map[string]interface{})
		if m["id"] == id {
			added, _ := m["added"].(bool)
			return added
		}
	}
	t.Fatalf("the directory no longer offers %s", id)
	return false
}
