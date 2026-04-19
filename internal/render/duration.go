package render

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

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
