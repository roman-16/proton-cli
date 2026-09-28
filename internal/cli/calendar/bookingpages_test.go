package calendar

import (
	"slices"
	"strings"
	"testing"

	calsvc "github.com/roman-16/proton-cli/internal/service/calendar"
)

func TestAvailableTakesADayOrARunOfThemAndTheHours(t *testing.T) {
	got, err := parseAvailable([]string{"mon-wed=09:00-12:00", "fri,sun=13:00-24:00", "Tuesday=14:00-17:00"})
	if err != nil {
		t.Fatal(err)
	}
	want := []calsvc.Window{
		{Day: "mon", Start: "09:00", End: "12:00"},
		{Day: "tue", Start: "09:00", End: "12:00"},
		{Day: "tue", Start: "14:00", End: "17:00"},
		{Day: "wed", Start: "09:00", End: "12:00"},
		{Day: "fri", Start: "13:00", End: "24:00"},
		{Day: "sun", Start: "13:00", End: "24:00"},
	}
	if !slices.Equal(got, want) {
		t.Errorf("windows = %v\nwant      %v", got, want)
	}
}

func TestAvailableRunsOfDaysWrapAroundTheWeekend(t *testing.T) {
	got, err := parseAvailable([]string{"sat-mon=10:00-11:00"})
	if err != nil {
		t.Fatal(err)
	}
	var days []string
	for _, w := range got {
		days = append(days, w.Day)
	}
	if !slices.Equal(days, []string{"mon", "sat", "sun"}) {
		t.Errorf("days = %v", days)
	}
}

func TestAvailableTakesDatesAndMidnight(t *testing.T) {
	got, err := parseAvailable([]string{"2026-10-06=9:00-00:00", "2026-10-05,2026-10-07=08:30-10:00"})
	if err != nil {
		t.Fatal(err)
	}
	want := []calsvc.Window{
		{Day: "2026-10-05", Start: "08:30", End: "10:00"},
		{Day: "2026-10-06", Start: "09:00", End: "24:00"},
		{Day: "2026-10-07", Start: "08:30", End: "10:00"},
	}
	if !slices.Equal(got, want) {
		t.Errorf("windows = %v", got)
	}
}

func TestAvailableRefusesWhatNoPageCouldOffer(t *testing.T) {
	for _, c := range []struct {
		tokens []string
		want   string
	}{
		{[]string{"mon=9-17"}, "--available takes DAY=START-END"},
		{[]string{"mon 09:00-17:00"}, "--available takes DAY=START-END"},
		{[]string{"mon=24:00-24:00"}, "--available takes DAY=START-END"},
		{[]string{"mon=17:00-09:00"}, "ends before it starts"},
		{[]string{"mo=09:00-17:00"}, `"mo" is not a weekday`},
		{[]string{"mon=09:00-17:00", "2026-10-05=09:00-12:00"}, "repeats every week or runs on set dates"},
		{[]string{"mon,2026-10-05=09:00-12:00"}, "repeats every week or runs on set dates"},
		{[]string{"mon=09:00-12:00", "mon-tue=11:00-14:00"}, "mon=09:00-12:00 and mon=11:00-14:00 overlap"},
	} {
		_, err := parseAvailable(c.tokens)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("parseAvailable(%v) = %v, want %q", c.tokens, err, c.want)
		}
	}
	if _, err := parseAvailable([]string{"mon=09:00-12:00", "mon=12:00-14:00"}); err != nil {
		t.Errorf("windows that meet were refused: %v", err)
	}
}
