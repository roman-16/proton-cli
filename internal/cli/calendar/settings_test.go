package calendar

import (
	"testing"

	"github.com/roman-16/proton-cli/internal/cli/kit"
	calsvc "github.com/roman-16/proton-cli/internal/service/calendar"
)

// What `update` accepts depends on what the calendar is: a holidays calendar
// takes its colour, its all-day reminders and whether it makes you busy, and a
// default duration or the default calendar are for a personal calendar of your
// own.
func TestUpdateTakesWhatTheCalendarsKindHas(t *testing.T) {
	holidays := calsvc.Calendar{Name: "Holidays in Austria", Kind: calsvc.KindHolidays}
	shared := calsvc.Calendar{Name: "Team", Kind: calsvc.KindShared, Owner: "jane@proton.me", Access: "editor"}
	subscribed := calsvc.Calendar{Name: "Timetable", Kind: calsvc.KindSubscribed}
	switchedOff := calsvc.Calendar{Name: "Old", Kind: calsvc.KindPersonal}
	const onlyWhatItHas = "A holidays calendar takes only --color, --remind-all-day, --no-remind and --busy."
	for _, tc := range []struct {
		name string
		cal  calsvc.Calendar
		args []string
		want string
	}{
		{"a holidays calendar renamed", holidays, []string{"--name", "Feiertage"}, onlyWhatItHas},
		{"a holidays calendar given a duration", holidays, []string{"--default-duration", "30m"}, onlyWhatItHas},
		{"a holidays calendar given a timed reminder", holidays, []string{"--remind", "1d"}, onlyWhatItHas},
		{"a holidays calendar made the default", holidays, []string{"--default"}, onlyWhatItHas},
		{"a holidays calendar recoloured", holidays, []string{"--color", "pacific"}, ""},
		{"a holidays calendar given an all-day reminder", holidays, []string{"--remind-all-day", "1d"}, ""},
		{"a holidays calendar left free", holidays, []string{"--busy", "off"}, ""},
		{"a shared calendar given a duration", shared, []string{"--default-duration", "30m"},
			"--default-duration applies only to a personal calendar of your own."},
		{"a subscribed calendar made the default", subscribed, []string{"--default"},
			"--default applies only to a personal calendar of your own."},
		{"a shared calendar renamed", shared, []string{"--name", "Jane's team"}, ""},
		{"a subscribed calendar given reminders", subscribed, []string{"--remind", "15m"}, ""},
		{"a switched-off calendar made the default", switchedOff, []string{"--default"},
			"Old is disabled, so it cannot be your default calendar."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := calendarsUpdateCmd()
			if err := cmd.ParseFlags(tc.args); err != nil {
				t.Fatalf("parse: %v", err)
			}
			err := fitsKind(&kit.Invocation{Cmd: cmd}, tc.cal)
			switch {
			case tc.want == "" && err != nil:
				t.Errorf("was refused: %v", err)
			case tc.want != "" && (err == nil || err.Error() != tc.want):
				t.Errorf("answered %v, want %q", err, tc.want)
			}
		})
	}
}

// A name in a hint is written so the command line it sits in can be pasted back.
func TestAHintWritesANameAsOneArgument(t *testing.T) {
	for in, want := range map[string]string{
		"Team":                "Team",
		"Holidays in Austria": "'Holidays in Austria'",
		"Jane's":              `'Jane'\''s'`,
		"a;b":                 "'a;b'",
		"":                    "''",
	} {
		if got := shellArg(in); got != want {
			t.Errorf("shellArg(%q) = %s, want %s", in, got, want)
		}
	}
}
