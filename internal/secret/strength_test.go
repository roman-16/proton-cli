package secret

import (
	"strings"
	"testing"
)

func TestWeakJudgesByLengthThenByKinds(t *testing.T) {
	cases := []struct {
		password string
		weak     bool
		why      string
	}{
		{"", true, "nothing at all is the weakest there is"},
		{"Passw0rd!", true, "nine characters is short whatever it is made of"},
		{"Tr0ub4dor&3", true, "eleven characters, all four kinds, still short"},
		{"hunterseven12", true, "thirteen characters from two kinds"},
		{"hunter2hunter2", true, "fourteen characters from two kinds"},
		{"HUNTERHUNTER12", true, "fourteen characters of capitals and digits"},
		{"Hunter2hunter2", false, "fourteen characters from three kinds"},
		{"HunterSeven2!", false, "thirteen characters from four kinds"},
		{"huntersevenX2", false, "thirteen characters from three kinds"},
		{"Hunter2hunter2!", false, "fifteen characters from four kinds"},
		{"aardvarkbadger12", false, "sixteen characters settles it on length"},
		{"correcthorsebatterystaple", false, "a passphrase is long, not varied"},
	}
	for _, c := range cases {
		if got := Weak(c.password); got != c.weak {
			t.Errorf("Weak(%q) = %v, want %v: %s", c.password, got, c.weak, c.why)
		}
	}
}

func TestWeakCountsCharactersRatherThanBytes(t *testing.T) {
	// Twelve letters and twenty-four bytes. Counting bytes would call it long
	// enough to skip the kinds check, which is the wrong answer twice over.
	twelveGreekLetters := strings.Repeat("α", 12)
	if !Weak(twelveGreekLetters) {
		t.Errorf("Weak(%q) = false; twelve letters of one kind is weak", twelveGreekLetters)
	}
}

// What `pass generate` hands out unasked is never something the listing beside
// it then calls weak. Asked for less - one word, no digits, no capitals - it
// will be, and rightly: the flags are there to make a smaller secret on purpose.
func TestTheGeneratorsDefaultsAreNeverWeak(t *testing.T) {
	defaults := []Options{
		{Length: DefaultLength, Digits: true, Symbols: true, Upper: true},
		{Words: DefaultWords, Separator: DefaultSeparator, Digits: true, Symbols: true, Upper: true},
	}
	for _, o := range defaults {
		for range 200 {
			pw, err := Make(o)
			if err != nil {
				t.Fatalf("Make(%+v): %v", o, err)
			}
			if Weak(pw) {
				t.Fatalf("Make(%+v) produced %q, which this CLI calls weak", o, pw)
			}
		}
	}
}
