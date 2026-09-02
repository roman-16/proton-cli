package kit

import (
	"strings"
	"time"

	"github.com/roman-16/proton-cli/internal/units"
)

// --expires asks one question - how long before this stops working - and Never
// is one of the answers to it.
//
// It is a word rather than a flag of its own because the CLI already prints it:
// a public link that does not expire reports "Expires: never", so the value that
// sets it is the value that was shown. A flag saying the same thing again would
// be a second way to say it, and the two would have to be kept from being given
// at once.
//
// A flag whose value cannot carry the word needs one - a password read from a
// file has no way of saying "none", which is what --clear-link-password is for.

// Never is what --expires takes for something that should not stop working.
const Never = "never"

// Expires reads what --expires was given. Never is zero, which is how every
// caller says "no expiry" to Proton.
//
// A duration of nothing is refused rather than read as never: "0s" is what an
// arithmetic mistake in a script produces, and treating it as a request to
// remove the expiry would be this CLI guessing.
func Expires(value string) (time.Duration, error) {
	if strings.EqualFold(strings.TrimSpace(value), Never) {
		return 0, nil
	}
	d, err := units.ParseDuration(value)
	if err != nil {
		return 0, Fail("--expires: %v", err).Hint("--expires never for one that does not expire.")
	}
	if d <= 0 {
		return 0, Fail("--expires must be longer than nothing.").
			Hint("--expires never for one that does not expire.")
	}
	return d, nil
}
