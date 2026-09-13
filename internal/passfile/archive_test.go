package passfile

import (
	"errors"
	"strings"
	"testing"
)

func TestAnEntryWithinTheBudgetIsReadWhole(t *testing.T) {
	body, err := readCapped(strings.NewReader("0123456789"), 10)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "0123456789" {
		t.Errorf("read %q", body)
	}
}

// Stopping at the budget would hand back a file short by an unknown amount, and
// every reader downstream would then report a truncated export as one written by
// another program.
func TestAnEntryOverTheBudgetIsRefused(t *testing.T) {
	body, err := readCapped(strings.NewReader("0123456789"), 9)
	if !errors.Is(err, errTooLarge) {
		t.Errorf("read %q, %v", body, err)
	}
}
