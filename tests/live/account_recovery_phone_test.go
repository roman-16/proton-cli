package live

import (
	"fmt"
	"strings"
	"testing"
	"unicode"
)

// The recovery number.
//
// Setting and removing it round-trips here; verifying it does not, and cannot:
// Proton texts a code to a real phone, and the number a run may set is one
// nobody receives anything at. What the run does check is that the code cannot
// be asked for out of order - an unverified number that nothing has been sent
// to is refused, and so is a number that is not one.
//
// The number is from the range set aside for fiction, so it reaches nobody.
const testRecoveryPhone = "+1 202 555 0143"

// samePhone compares two numbers as numbers: Proton hands one back written its
// own way, which is not always the way it was typed.
func samePhone(a, b string) bool {
	digits := func(s string) string {
		var out strings.Builder
		for _, r := range s {
			if unicode.IsDigit(r) {
				out.WriteRune(r)
			}
		}
		return out.String()
	}
	return digits(a) == digits(b)
}

func TestAccountRecoveryPhoneRoundTrip(t *testing.T) {
	before := runJSON(t, "account", "settings", "recovery-phone", "get")
	original, _ := before["number"].(string)

	cleanup(t, "put the primary account's recovery phone back: "+
		"proton --profile primary account settings recovery-phone set "+orNone(original),
		func() error {
			// Writing the number that is already there is refused, so the
			// restore asks what is there first.
			now, _ := runJSON(t, "account", "settings", "recovery-phone", "get")["number"].(string)
			if now == original {
				return nil
			}
			_, stderr, code := run(t, "account", "settings", "recovery-phone", "set", orNone(original))
			if code != 0 {
				return fmt.Errorf("exit %d: %s", code, strings.TrimSpace(stderr))
			}
			return nil
		})

	_, stderr := runOKStderr(t, "account", "settings", "recovery-phone", "set", testRecoveryPhone)
	assertContains(t, stderr, "Set recovery phone to")
	after := runJSON(t, "account", "settings", "recovery-phone", "get")
	if number, _ := after["number"].(string); !samePhone(number, testRecoveryPhone) {
		t.Errorf("number = %v, want the one just written", after["number"])
	}
	if verified, _ := after["verified"].(bool); verified {
		t.Error("a newly written number is not verified yet")
	}

	// Whether Proton may reset the password through it is a second question,
	// and the number stays whichever way it is answered. Which way to turn it
	// first is read rather than assumed.
	now, _ := after["allow_recovery"].(bool)
	for _, want := range []bool{!now, now} {
		_, stderr = runOKStderr(t, "account", "settings", "recovery-phone", verb(want))
		assertContains(t, stderr, "recovery by phone")
		state := runJSON(t, "account", "settings", "recovery-phone", "get")
		if on, _ := state["allow_recovery"].(bool); on != want {
			t.Errorf("allow_recovery = %v after %s, want %v", on, verb(want), want)
		}
		if number, _ := state["number"].(string); !samePhone(number, testRecoveryPhone) {
			t.Errorf("number = %v after %s, want it kept", state["number"], verb(want))
		}
	}

	// Writing the number that is already there is refused rather than sent, in
	// the CLI's own words.
	_, stderr, code := run(t, "account", "settings", "recovery-phone", "set", testRecoveryPhone)
	if code != 1 {
		t.Errorf("writing the number again: exit %d, want 1\nstderr: %s", code, truncateOutput(stderr))
	}
	assertContains(t, stderr, "already your recovery phone")

	_, stderr = runOKStderr(t, "account", "settings", "recovery-phone", "set", "none")
	assertContains(t, stderr, "Set recovery phone to none")
	if got := runJSON(t, "account", "settings", "recovery-phone", "get")["number"]; got != "" {
		t.Errorf("number = %v after removing it, want empty", got)
	}
}

func TestAccountRecoveryPhoneRefusesWhatItCannotAct(t *testing.T) {
	for _, arg := range []string{"0660 1234567", "+43 66", "not-a-number"} {
		_, stderr, code := run(t, "account", "settings", "recovery-phone", "set", arg)
		if code != 1 {
			t.Errorf("%q: exit %d, want 1\nstderr: %s", arg, code, truncateOutput(stderr))
		}
		assertContains(t, stderr, "not a phone number")
	}

	if number, _ := runJSON(t, "account", "settings", "recovery-phone", "get")["number"].(string); number != "" {
		t.Skip("the account has a recovery number, so there is nothing here to refuse for want of one")
	}
	for _, args := range [][]string{
		{"verify"}, {"verify", "--code", "482913"}, {"enable"}, {"set", "none"},
	} {
		_, stderr, code := run(t, append([]string{"account", "settings", "recovery-phone"}, args...)...)
		if code != 1 {
			t.Errorf("%v with no number: exit %d, want 1\nstderr: %s", args, code, truncateOutput(stderr))
		}
		assertContains(t, stderr, "no recovery phone")
	}
}
