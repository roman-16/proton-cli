package offline

import (
	"strings"
	"testing"
)

func bookingPage(args ...string) []string {
	return append([]string{"--zone", "Europe/Vienna", "calendar", "booking-pages"}, args...)
}

func TestABookingPageNeedsATitle(t *testing.T) {
	refuses(t, 1, bookingPage("create"), "A booking page needs a title.")
	refuses(t, 1, bookingPage("create", "--title", strings.Repeat("a", 256)), "at most 255 characters")
	refuses(t, 1, bookingPage("update", "5bH2mQxK", "--title", ""), "A booking page needs a title.")
}

func TestWhenABookingPageIsAvailableIsJudgedFromTheCommandLine(t *testing.T) {
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"--available", "mon=9-17"}, "--available takes DAY=START-END"},
		{[]string{"--available", "mon=17:00-09:00"}, "ends before it starts"},
		{[]string{"--available", "mo=09:00-17:00"}, "is not a weekday"},
		{[]string{"--available", "mon=09:00-17:00", "--available", "2026-10-05=09:00-12:00"},
			"repeats every week or runs on set dates, not both"},
		{[]string{"--available", "mon=09:00-12:00", "--available", "mon=11:00-14:00"}, "overlap"},
		{[]string{"--available", "2020-01-06=09:00-12:00", "--duration", "30m"}, "has already begun"},
		{[]string{"--available", "mon=09:00-09:30", "--duration", "1h"}, "is shorter than one 1h appointment"},
		{[]string{"--available", "mon-sun=08:00-20:00", "--duration", "15m"}, "That is 336 appointments"},
	} {
		refuses(t, 1, bookingPage(append([]string{"create", "--title", "Office hours"}, c.args...)...), c.want)
	}
}

func TestABookingPageOffersAppointmentsOfFiveLengths(t *testing.T) {
	refuses(t, 1, bookingPage("create", "--title", "Office hours", "--duration", "45m"),
		"--duration accepts: 15m, 30m, 1h, 1h30m, 2h")
	refuses(t, 1, bookingPage("update", "5bH2mQxK", "--duration", "3d"), "--duration accepts:")
}

func TestABookingPagesNoticeIsOneOfFour(t *testing.T) {
	refuses(t, 1, bookingPage("create", "--title", "Office hours", "--notice", "soon"),
		"--notice accepts:", "none", "2h", "48h", "next-day")
}

func TestChangingABookingPageNeedsOneAnswer(t *testing.T) {
	refuses(t, 1, bookingPage("update", "5bH2mQxK"), "Nothing to change.")
	refuses(t, 1, bookingPage("update", "5bH2mQxK", "--location", "Room 3", "--meet"), "contradict")
	refuses(t, 1, bookingPage("update", "5bH2mQxK", "--blocked-by", "Work", "--no-blocked-by"), "contradict")
	refuses(t, 1, bookingPage("update", "5bH2mQxK", "--location", " "), "--location needs a place.")
}
