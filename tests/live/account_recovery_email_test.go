package live

import (
	"strings"
	"testing"
)

// The recovery address, read and refused. No run writes one: Proton refuses a
// recovery address changed on every run, argued beside PUT
// /core/v4/settings/email in internal/cli/coverage_test.go.

// What cannot be acted on is refused before the network where the command line
// says so, and after one read where the account does.
func TestAccountRecoveryEmailRefusesWhatItCannotAct(t *testing.T) {
	_, stderr, code := run(t, "account", "settings", "recovery-email", "set", "not-an-address")
	if code != 1 {
		t.Errorf("a malformed address: exit %d, want 1\nstderr: %s", code, truncateOutput(stderr))
	}
	assertContains(t, stderr, "not an email address")

	if address, _ := runJSON(t, "account", "settings", "recovery-email", "get")["address"].(string); address != "" {
		t.Skip("the account has a recovery address, so there is nothing here to refuse for want of one")
	}
	_, stderr, code = run(t, "account", "settings", "recovery-email", "verify")
	if code != 1 {
		t.Errorf("verifying an address that is not there: exit %d, want 1\nstderr: %s",
			code, truncateOutput(stderr))
	}
	assertContains(t, stderr, "no recovery email")
}

func TestAccountRecoveryEmailDryRunAsksForNothing(t *testing.T) {
	_, stderr := runOKStderr(t, "account", "settings", "recovery-email", "set",
		externalRecipient(t), "--dry-run")
	assertContains(t, stderr, "Dry run")
	if strings.Contains(stderr, "Password:") {
		t.Errorf("the preview asked for a password: %s", truncateOutput(stderr))
	}
}
