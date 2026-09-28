// Package units converts between machine values and human-friendly forms -
// byte sizes, durations, and timestamps - in both directions. It is a pure
// leaf package (no I/O) shared by the render and cli layers.
package units

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Size formats a byte count as a human-readable string (e.g. "1.5 GB").
func Size(bytes int64) string {
	switch {
	case bytes >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(1<<30))
	case bytes >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(1<<20))
	case bytes >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

// ParseSize parses a human byte size ("100KB", "5MB", "2GB", "1024"). K/M/G/T
// are base 1024. It is the inverse of Size.
func ParseSize(s string) (int64, error) {
	s = strings.TrimSpace(strings.ToUpper(s))
	if s == "" {
		return 0, fmt.Errorf("empty size")
	}
	mult := int64(1)
	switch {
	case strings.HasSuffix(s, "TB"), strings.HasSuffix(s, "T"):
		mult = 1 << 40
		s = strings.TrimSuffix(strings.TrimSuffix(s, "TB"), "T")
	case strings.HasSuffix(s, "GB"), strings.HasSuffix(s, "G"):
		mult = 1 << 30
		s = strings.TrimSuffix(strings.TrimSuffix(s, "GB"), "G")
	case strings.HasSuffix(s, "MB"), strings.HasSuffix(s, "M"):
		mult = 1 << 20
		s = strings.TrimSuffix(strings.TrimSuffix(s, "MB"), "M")
	case strings.HasSuffix(s, "KB"), strings.HasSuffix(s, "K"):
		mult = 1 << 10
		s = strings.TrimSuffix(strings.TrimSuffix(s, "KB"), "K")
	case strings.HasSuffix(s, "B"):
		s = strings.TrimSuffix(s, "B")
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0, fmt.Errorf("invalid size %q: %w", s, err)
	}
	return int64(f * float64(mult)), nil
}

// Time formats a Unix timestamp in the local zone, or "-" for the zero value.
func Time(unix int64) string {
	if unix == 0 {
		return "-"
	}
	return time.Unix(unix, 0).Local().Format("2006-01-02 15:04")
}

// Duration formats a duration compactly (e.g. "45s", "30m", "2h", "1h30m", "3d").
//
// Whole days read as days, because a span that long is one somebody counted in
// days: a day-long event is "1d", not "24h".
func Duration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	const day = 24 * time.Hour
	if d >= day && d%day == 0 {
		return fmt.Sprintf("%dd", int(d/day))
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	switch {
	case h == 0:
		return fmt.Sprintf("%dm", m)
	case m == 0:
		return fmt.Sprintf("%dh", h)
	default:
		return fmt.Sprintf("%dh%dm", h, m)
	}
}

var weekdayNames = []string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}

func Weekday(d time.Weekday) string { return weekdayNames[d] }

func Weekdays() []string { return slices.Clone(weekdayNames) }

func ParseWeekday(s string) (time.Weekday, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	for i, name := range weekdayNames {
		if s == name || s == strings.ToLower(time.Weekday(i).String()) {
			return time.Weekday(i), nil
		}
	}
	return 0, fmt.Errorf("%q is not a weekday (%s)", s, strings.Join(weekdayNames, ", "))
}

// ParseDuration accepts Go's time.ParseDuration formats plus trailing unit
// suffixes for longer spans:
//
//	d = day (24h)
//	w = week (7d)
//	mo = month (30d)
//	y = year (365d)
//
// Examples: "30d", "2w", "6mo", "1y", "1h30m".
func ParseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty duration")
	}
	day := 24 * time.Hour
	suffixes := []struct {
		suffix string
		unit   time.Duration
	}{
		{"mo", 30 * day},
		{"y", 365 * day},
		{"w", 7 * day},
		{"d", day},
	}
	for _, sx := range suffixes {
		if strings.HasSuffix(s, sx.suffix) {
			nStr := strings.TrimSuffix(s, sx.suffix)
			n, err := strconv.Atoi(nStr)
			if err != nil {
				return 0, fmt.Errorf("invalid duration %q: %w", s, err)
			}
			return time.Duration(n) * sx.unit, nil
		}
	}
	return time.ParseDuration(s)
}
