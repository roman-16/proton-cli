package kit

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/roman-16/proton-cli/internal/errs"
)

// --expires asks how long, and never is one of the answers: zero, which is what
// every caller sends Proton for no expiry.
func TestExpiresReadsADurationOrNever(t *testing.T) {
	for _, tt := range []struct {
		value string
		want  time.Duration
	}{
		{"7d", 7 * 24 * time.Hour},
		{"90m", 90 * time.Minute},
		{"never", 0},
		{"Never", 0},
		{"  never  ", 0},
	} {
		got, err := Expires(tt.value)
		if err != nil {
			t.Errorf("Expires(%q) failed: %v", tt.value, err)
			continue
		}
		if got != tt.want {
			t.Errorf("Expires(%q) = %v, want %v", tt.value, got, tt.want)
		}
	}
}

// A duration of nothing is an arithmetic mistake, not a way of saying never, so
// it is refused - and the refusal says which word says never.
func TestExpiresRefusesNothingAndNonsense(t *testing.T) {
	for _, value := range []string{"0s", "0", "-1h", "soon", ""} {
		_, err := Expires(value)
		if err == nil {
			t.Errorf("Expires(%q) was accepted", value)
			continue
		}
		var hinter errs.Hinter
		if !errors.As(err, &hinter) {
			t.Errorf("Expires(%q) refused without a remedy: %v", value, err)
			continue
		}
		if !strings.Contains(strings.Join(hinter.Hints(), " "), "--expires never") {
			t.Errorf("Expires(%q) refused without naming --expires never: %v", value, hinter.Hints())
		}
	}
}
