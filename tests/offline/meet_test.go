package offline

import "testing"

func TestAutoAddMeetLinksIsOnOrOff(t *testing.T) {
	refuses(t, 1, []string{"calendar", "settings", "set", "auto-add-meet-links", "zoom"}, "auto-add-meet-links accepts: off, on")
}

func TestAMeetingIsJudgedBeforeTheNetwork(t *testing.T) {
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"create", "--start", "2026-11-04T09:00"}, "A meeting needs a title and a start."},
		{[]string{"create", "--title", "Sync"}, "A meeting needs a title and a start."},
		{[]string{"create", "--title", "Sync", "--start", "2026-11-04"}, "--start names a day but not a time of day."},
		{[]string{"create", "--title", "Sync", "--start", "2026-11-04T09:00", "--duration", "0m"}, "--duration has to be longer than nothing."},
		{[]string{"create", "--title", " ", "--start", "2026-11-04T09:00"}, "--title needs a title."},
		{[]string{"update", "865JZ8JM"}, "Nothing to change."},
	} {
		refuses(t, 1, append([]string{"meet", "meetings"}, c.args...), c.want)
	}
}

func TestARoomIsJudgedBeforeTheNetwork(t *testing.T) {
	refuses(t, 1, []string{"meet", "rooms", "create"}, "A room needs a title.")
	refuses(t, 1, []string{"meet", "rooms", "update", "personal"}, "Nothing to change.")
	refuses(t, 1, []string{"meet", "rooms", "rotate", "Team room"}, "Only your personal room gets a new link.")
}
