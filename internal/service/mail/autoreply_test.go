package mail

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestAutoReplyFixedEncodesAbsoluteTimesInItsZone(t *testing.T) {
	ar := AutoReply{
		Enabled: true, Repeat: "fixed", Zone: "Europe/Vienna",
		Start: "2026-07-01T09:00", End: "2026-07-14T18:00",
		Message: "away",
	}
	got, err := ar.encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	loc, _ := time.LoadLocation("Europe/Vienna")
	wantStart := time.Date(2026, 7, 1, 9, 0, 0, 0, loc).Unix()
	wantEnd := time.Date(2026, 7, 14, 18, 0, 0, 0, loc).Unix()
	if got.StartTime != wantStart || got.EndTime != wantEnd {
		t.Errorf("times = %d/%d, want %d/%d", got.StartTime, got.EndTime, wantStart, wantEnd)
	}
	if got.Repeat != repeatFixed {
		t.Errorf("repeat = %d, want %d", got.Repeat, repeatFixed)
	}
	// Proton hardcodes the subject and the CLI does not expose it.
	if got.Subject != "Auto" {
		t.Errorf("subject = %q, want Auto", got.Subject)
	}
	if !got.IsEnabled {
		t.Error("the enabled flag was not carried onto the wire object")
	}
}

func TestAutoReplyDailyEncodesSecondsIntoTheDay(t *testing.T) {
	ar := AutoReply{
		Repeat: "daily", Zone: "UTC", Start: "09:30", End: "17:00",
		Days: []string{"mon", "tue,wed", "friday"},
	}
	got, err := ar.encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if got.StartTime != 9*3600+30*60 {
		t.Errorf("start = %d, want %d", got.StartTime, 9*3600+30*60)
	}
	if got.EndTime != 17*3600 {
		t.Errorf("end = %d, want %d", got.EndTime, 17*3600)
	}
	// Sunday is 0, so Monday is 1. Both comma-joined and long names parse.
	if want := []int{1, 2, 3, 5}; !reflect.DeepEqual(got.DaysSelected, want) {
		t.Errorf("days = %v, want %v", got.DaysSelected, want)
	}
}

func TestAutoReplyWeeklyPacksTheWeekdayWithSundayAtZero(t *testing.T) {
	ar := AutoReply{Repeat: "weekly", Zone: "UTC", Start: "sun:00:00", End: "sat:23:00"}
	got, err := ar.encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if got.StartTime != 0 {
		t.Errorf("Sunday midnight = %d, want 0", got.StartTime)
	}
	if want := int64(6*daySeconds + 23*3600); got.EndTime != want {
		t.Errorf("Saturday 23:00 = %d, want %d", got.EndTime, want)
	}
}

func TestAutoReplyMonthlyIsOneBasedOnTheWire(t *testing.T) {
	ar := AutoReply{Repeat: "monthly", Zone: "UTC", Start: "1:09:00", End: "15:17:30"}
	got, err := ar.encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	// The first of the month is offset zero.
	if want := int64(9 * 3600); got.StartTime != want {
		t.Errorf("1st at 09:00 = %d, want %d", got.StartTime, want)
	}
	if want := int64(14*daySeconds + 17*3600 + 30*60); got.EndTime != want {
		t.Errorf("15th at 17:30 = %d, want %d", got.EndTime, want)
	}
}

func TestAutoReplyPermanentHasNoBounds(t *testing.T) {
	got, err := AutoReply{Repeat: "permanent", Zone: "UTC"}.encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if got.StartTime != 0 || got.EndTime != 0 {
		t.Errorf("permanent should carry no times, got %d/%d", got.StartTime, got.EndTime)
	}
}

func TestAutoReplyRejectsMismatchedFlags(t *testing.T) {
	tests := []struct {
		name string
		ar   AutoReply
		want string
	}{
		{"days outside daily", AutoReply{Repeat: "weekly", Zone: "UTC", Start: "mon:09:00", End: "fri:17:00",
			Days: []string{"mon"}}, "--days applies to --repeat daily"},
		{"permanent with times", AutoReply{Repeat: "permanent", Zone: "UTC", Start: "09:00"},
			"takes no --start"},
		{"daily without days", AutoReply{Repeat: "daily", Zone: "UTC", Start: "09:00", End: "17:00"},
			"needs --days"},
		{"missing bounds", AutoReply{Repeat: "fixed", Zone: "UTC"}, "needs --start and --end"},
		{"end before start", AutoReply{Repeat: "fixed", Zone: "UTC",
			Start: "2026-07-14T18:00", End: "2026-07-01T09:00"}, "--end must be after --start"},
		{"unknown repeat", AutoReply{Repeat: "hourly", Zone: "UTC"}, "unknown repeat"},
		{"unknown zone", AutoReply{Repeat: "permanent", Zone: "Mars/Olympus"}, "unknown time zone"},
		{"bad weekday", AutoReply{Repeat: "weekly", Zone: "UTC", Start: "funday:09:00", End: "fri:17:00"},
			"is not a weekday"},
		{"bad day of month", AutoReply{Repeat: "monthly", Zone: "UTC", Start: "32:09:00", End: "15:17:00"},
			"day of the month"},
		{"bad clock", AutoReply{Repeat: "daily", Zone: "UTC", Start: "9am", End: "17:00", Days: []string{"mon"}},
			"is not a time of day"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.ar.encode()
			if err == nil {
				t.Fatalf("expected an error mentioning %q", tt.want)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %q, want it to mention %q", err, tt.want)
			}
		})
	}
}

// What `get` prints must be what `set` accepts, so the two speak one grammar.
func TestAutoReplyDecodeIsTheInverseOfEncode(t *testing.T) {
	tests := []AutoReply{
		{Repeat: "fixed", Zone: "Europe/Vienna", Start: "2026-07-01T09:00", End: "2026-07-14T18:00"},
		{Repeat: "daily", Zone: "UTC", Start: "09:30", End: "17:00", Days: []string{"mon", "wed", "fri"}},
		{Repeat: "weekly", Zone: "UTC", Start: "mon:09:00", End: "fri:17:00"},
		{Repeat: "monthly", Zone: "UTC", Start: "1:09:00", End: "15:17:30"},
		{Repeat: "permanent", Zone: "UTC"},
	}
	for _, want := range tests {
		t.Run(want.Repeat, func(t *testing.T) {
			wire, err := want.encode()
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			got := wire.decode()
			if got.Repeat != want.Repeat || got.Start != want.Start || got.End != want.End {
				t.Errorf("round trip = %+v, want repeat=%s start=%s end=%s",
					got, want.Repeat, want.Start, want.End)
			}
			if len(want.Days) > 0 && !reflect.DeepEqual(got.Days, want.Days) {
				t.Errorf("days = %v, want %v", got.Days, want.Days)
			}
		})
	}
}

func TestAutoReplyScheduleSummary(t *testing.T) {
	tests := []struct {
		ar   AutoReply
		want string
	}{
		{AutoReply{Repeat: "permanent"}, "permanent"},
		{AutoReply{Repeat: "daily", Start: "09:00", End: "17:00", Days: []string{"mon", "tue"}},
			"daily 09:00-17:00 on mon,tue"},
		{AutoReply{Repeat: "fixed", Start: "2026-07-01T09:00", End: "2026-07-14T18:00"},
			"fixed 2026-07-01T09:00 to 2026-07-14T18:00"},
	}
	for _, tt := range tests {
		if got := tt.ar.ScheduleSummary(); got != tt.want {
			t.Errorf("ScheduleSummary() = %q, want %q", got, tt.want)
		}
	}
}

func TestDecodeAutoReplyReadsTheSettingsObject(t *testing.T) {
	raw := []byte(`{"IsEnabled":true,"Repeat":4,"Message":"away","Subject":"Auto","Zone":"UTC","DaysSelected":[]}`)
	got, err := DecodeAutoReply(raw)
	if err != nil {
		t.Fatalf("DecodeAutoReply: %v", err)
	}
	if !got.Enabled || got.Repeat != "permanent" || got.Message != "away" {
		t.Errorf("decoded = %+v", got)
	}
}

// A schedule is stored against a named zone, and an hour written with no zone
// means nothing. Guessing one would put the auto-reply on at the wrong time and
// say nothing about it, so an unnamed zone is refused here and named by whoever
// resolved it.
func TestAutoReplyNeedsTheZoneItIsReadIn(t *testing.T) {
	if _, err := (AutoReply{Repeat: "permanent"}).encode(); err == nil {
		t.Error("a schedule with no zone was encoded")
	}
	got, err := AutoReply{Repeat: "permanent", Zone: "Europe/Vienna"}.encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if got.Zone != "Europe/Vienna" {
		t.Errorf("Zone = %q, want the one it was given", got.Zone)
	}
}
