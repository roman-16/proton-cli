package calendar

import (
	"testing"
	"time"

	calsvc "github.com/roman-16/proton-cli/internal/service/calendar"
)

// A busy time reads like an event row: the day, the time of day or that there is
// none, and how long, with a whole day counted in days.
func TestBusyTimeColumnsReadLikeAnEventRow(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Vienna")
	if err != nil {
		t.Fatalf("Europe/Vienna: %v", err)
	}
	for _, tc := range []struct {
		name string
		row  calsvc.BusyTime
		want map[string]string
	}{
		{
			name: "a meeting",
			row: calsvc.BusyTime{Email: "jane.roe@example.com",
				Start: time.Date(2026, 5, 4, 14, 0, 0, 0, loc), End: time.Date(2026, 5, 4, 15, 30, 0, 0, loc)},
			want: map[string]string{"EMAIL": "jane.roe@example.com", "DATE": "2026-05-04", "TIME": "14:00", "DURATION": "1h30m"},
		},
		{
			name: "a whole day the clocks change on",
			row: calsvc.BusyTime{Email: "jane.roe@example.com", AllDay: true,
				Start: time.Date(2026, 3, 29, 0, 0, 0, 0, loc), End: time.Date(2026, 3, 30, 0, 0, 0, 0, loc)},
			want: map[string]string{"DATE": "2026-03-29", "TIME": "all day", "DURATION": "1d"},
		},
	} {
		for _, c := range busyTimeColumns() {
			if w, ok := tc.want[c.Header]; ok && c.Cell(tc.row) != w {
				t.Errorf("%s: %s = %q, want %q", tc.name, c.Header, c.Cell(tc.row), w)
			}
		}
	}
}
