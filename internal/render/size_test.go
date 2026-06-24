package render

import (
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
