package offline

import "testing"

func TestAnEventIsConfirmedOrCancelled(t *testing.T) {
	refuses(t, 1, []string{"calendar", "events", "create", "--title", "Review", "--start", "2026-11-03T10:00", "--status", "tentative"},
		"--status accepts: confirmed, cancelled")
}
