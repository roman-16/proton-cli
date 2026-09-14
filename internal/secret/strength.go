package secret

import "unicode"

// Whether a password is weak is this CLI's own reading, stated here and in the
// help of the command that reports it, so a number on the screen is one anybody
// can arrive at again.
//
// It is deliberately a rule and not a score. A score invites the question "how
// close to the line is this one", which no listing can answer usefully, and a
// rule can be written in one sentence that a person can hold: a password is weak
// when it is shorter than twelve characters, or shorter than sixteen and drawn
// from fewer than three of the four kinds.
//
// The second clause is what lets a passphrase through. Twenty lowercase letters
// of four ordinary words is a fine secret and a terrible score on anything that
// counts character classes, so length past sixteen settles it on its own.
const (
	// tooShort is the length below which nothing is strong enough, whatever it
	// is made of.
	tooShort = 12
	// longEnough is the length past which length alone settles it.
	longEnough = 16
	// kindsWanted is how many of the four character kinds a password between the
	// two lengths has to draw from.
	kindsWanted = 3
)

// Weak reports whether a password is one to replace.
func Weak(password string) bool {
	runes := []rune(password)
	switch {
	case len(runes) < tooShort:
		return true
	case len(runes) >= longEnough:
		return false
	default:
		return kinds(runes) < kindsWanted
	}
}

// kinds is how many of lower, upper, digit and everything else appear.
func kinds(runes []rune) int {
	var hasLower, hasUpper, hasDigit, hasOther bool
	for _, r := range runes {
		switch {
		case unicode.IsLower(r):
			hasLower = true
		case unicode.IsUpper(r):
			hasUpper = true
		case unicode.IsDigit(r):
			hasDigit = true
		default:
			hasOther = true
		}
	}
	n := 0
	for _, present := range []bool{hasLower, hasUpper, hasDigit, hasOther} {
		if present {
			n++
		}
	}
	return n
}
