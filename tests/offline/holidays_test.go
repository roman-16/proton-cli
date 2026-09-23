package offline

import "testing"

// A holidays calendar is named by its country and takes the name Proton gives
// it, so asking for one with a name of your own, or from an address, says two
// things at once - which the flags alone settle.
func TestAHolidaysCalendarIsNamedByItsCountryAlone(t *testing.T) {
	refuses(t, 1, []string{"calendar", "settings", "calendars", "create",
		"--holidays", "Austria", "--name", "Feiertage"}, "--holidays and --name contradict each other.")
	refuses(t, 1, []string{"calendar", "settings", "calendars", "create",
		"--holidays", "Austria", "--url", "https://example.com/team.ics"}, "--holidays and --url contradict each other.")
	refuses(t, 1, []string{"calendar", "settings", "calendars", "create",
		"--name", "Work", "--language", "de"}, "--language picks a holidays calendar")
}

// The default calendar is replaced by making another one the default, never
// unset, and saying so needs nobody signed in.
func TestADefaultCalendarIsReplacedNotUnset(t *testing.T) {
	refuses(t, 1, []string{"calendar", "settings", "calendars", "update",
		"--default=false", "Work"}, "A default calendar is replaced, not unset")
}
