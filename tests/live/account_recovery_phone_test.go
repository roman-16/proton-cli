package live

import (
	"testing"
)

// The recovery number, refused. No run writes one: Proton refuses a recovery
// number changed on every run, argued beside PUT /core/v4/settings/phone in
// internal/cli/coverage_test.go. Nor can one be verified: Proton texts the code
// to a real phone, and there is none here to read it on.

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
