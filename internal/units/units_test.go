package units

import (
	"strings"
	"testing"
	"time"
)

func TestTime(t *testing.T) {
	if got := Time(0); got != "-" {
		t.Errorf("Time(0) = %q, want %q", got, "-")
	}
	ts := int64(1_700_000_000)
	want := time.Unix(ts, 0).Local().Format("2006-01-02 15:04")
	if got := Time(ts); got != want {
		t.Errorf("Time(%d) = %q, want %q", ts, got, want)
	}
}

func TestParseSize(t *testing.T) {
	tests := []struct {
		in   string
		want int64
	}{
		{"1024", 1024},
		{"1KB", 1 << 10},
		{"1K", 1 << 10},
		{"5MB", 5 << 20},
		{"2GB", 2 << 30},
		{"1TB", 1 << 40},
		{"100B", 100},
	}
	for _, tc := range tests {
		got, err := ParseSize(tc.in)
		if err != nil {
			t.Errorf("ParseSize(%q) error: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ParseSize(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
	if _, err := ParseSize(""); err == nil {
		t.Error("ParseSize(\"\") should error")
	}
	if _, err := ParseSize("notasize"); err == nil {
		t.Error("ParseSize(\"notasize\") should error")
	}
}

func TestParseDuration(t *testing.T) {
	day := 24 * time.Hour
	tests := []struct {
		in   string
		want time.Duration
	}{
		{"30d", 30 * day},
		{"2w", 14 * day},
		{"6mo", 180 * day},
		{"1y", 365 * day},
		{"1h30m", 90 * time.Minute},
	}
	for _, tc := range tests {
		got, err := ParseDuration(tc.in)
		if err != nil {
			t.Errorf("ParseDuration(%q) error: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ParseDuration(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
	if _, err := ParseDuration(""); err == nil {
		t.Error("ParseDuration(\"\") should error")
	}
	if _, err := ParseDuration("5x"); err == nil {
		t.Error("ParseDuration(\"5x\") should error")
	}
}

func TestSizeRoundTripsRoughly(t *testing.T) {
	if Size(1<<30) != "1.0 GB" {
		t.Errorf("Size(1GB) = %q", Size(1<<30))
	}
	if Size(500) != "500 B" {
		t.Errorf("Size(500) = %q", Size(500))
	}
}

func TestDuration(t *testing.T) {
	cases := map[time.Duration]string{
		45 * time.Second: "45s",
		30 * time.Minute: "30m",
		2 * time.Hour:    "2h",
		90 * time.Minute: "1h30m",
		24 * time.Hour:   "1d",
		72 * time.Hour:   "3d",
		25 * time.Hour:   "25h",
		36 * time.Hour:   "36h",
	}
	for d, want := range cases {
		if got := Duration(d); got != want {
			t.Errorf("Duration(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestAWeekdayIsReadByItsShortOrItsFullName(t *testing.T) {
	for in, want := range map[string]time.Weekday{
		"mon": time.Monday, "Monday": time.Monday, " SUN ": time.Sunday, "saturday": time.Saturday,
	} {
		got, err := ParseWeekday(in)
		if err != nil || got != want {
			t.Errorf("ParseWeekday(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	if _, err := ParseWeekday("mo"); err == nil || !strings.Contains(err.Error(), "sun, mon, tue") {
		t.Errorf("ParseWeekday(mo) = %v, want the weekdays named", err)
	}
}

func TestAWeekdayIsWrittenTheWayItIsRead(t *testing.T) {
	for d := time.Sunday; d <= time.Saturday; d++ {
		back, err := ParseWeekday(Weekday(d))
		if err != nil || back != d {
			t.Errorf("%v is written %q, which reads back as %v, %v", d, Weekday(d), back, err)
		}
	}
	if got := Weekdays(); len(got) != 7 || got[0] != "sun" || got[1] != "mon" {
		t.Errorf("Weekdays() = %v", got)
	}
}
