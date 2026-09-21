package live

import (
	"fmt"
	"strings"
	"testing"

	"github.com/roman-16/proton-cli/tests/account"
)

// Changing a password.
//
// Both halves are here because the two accounts do different things with the
// same command: the primary keeps one secret, so the change re-locks its keys
// and writes a verifier; the secondary keeps two, so the same command writes a
// verifier and touches no key.
//
// Each is a round trip, and the second leg is the assertion: it signs the new
// password over SRP, which nothing else could do if the first leg had not taken
// effect. A test that read the answer back would be reading the CLI's own idea
// of what it had just sent.
//
// The passwords are written here rather than generated, because the failure
// worth designing for is the test stopping between the legs: the account is
// then on a password only this file knows, and reading it out of here is what
// makes that recoverable by hand.
const (
	primaryTestPassword   = "proton-cli-round-trip-password"
	secondaryTestPassword = "proton-cli-round-trip-login-password"
)

func TestAccountPasswordRoundTrip(t *testing.T) {
	newFile := passwordFile(t, primaryTestPassword)
	original := accounts[account.Primary].passwordFile

	restored := false
	cleanup(t, "the primary account is on the password in tests/live/account_password_test.go. "+
		"Put it back with: proton --profile primary account settings password set "+
		"--password-file <that password in a file> --new-password-file <the original in a file>",
		func() error {
			if restored {
				return nil
			}
			return putPasswordBack(t, account.Primary, newFile, original)
		})

	_, stderr := runOKStderr(t, "account", "settings", "password", "set",
		"--new-password-file", newFile)
	assertContains(t, stderr, "Updated your password")

	// The session that changed it goes on working, with no password asked for:
	// the key password sealed into it was renewed by the change.
	assertContains(t, runOK(t, "account", "get"), "Email")
	assertContains(t, runOK(t, "mail", "messages", "list", "--limit", "1"), "ID")

	// And the new password is the account's, which only Proton can confirm: the
	// second leg proves it over SRP before Proton will take the change.
	_, stderr = runOKStderr(t, "account", "settings", "password", "set",
		"--password-file", newFile, "--new-password-file", original)
	assertContains(t, stderr, "Updated your password")
	restored = true
}

// In two-password mode the same command changes the password that signs in and
// says which one it changed. The keys stay where they are, which is what the
// rest of the run depends on: this account's keys are opened by its second
// password, and nothing here touches that.
func TestAccountPasswordChangesTheLoginPasswordInTwoPasswordMode(t *testing.T) {
	newFile := passwordFile(t, secondaryTestPassword)
	original := accounts[account.Secondary].passwordFile

	restored := false
	cleanup(t, "the secondary account is on the login password in "+
		"tests/live/account_password_test.go. Put it back with: proton --profile secondary "+
		"account settings password set --password-file <that password in a file> "+
		"--new-password-file <the original in a file>",
		func() error {
			if restored {
				return nil
			}
			return putPasswordBack(t, account.Secondary, newFile, original)
		})

	_, stderr := runOKStderrSecondary(t, "account", "settings", "password", "set",
		"--new-password-file", newFile)
	assertContains(t, stderr, "Updated your login password")

	// The keys were not part of it, so what they open still opens.
	assertContains(t, runOKSecondary(t, "mail", "messages", "list", "--limit", "1"), "ID")
	if mode := runJSONSecondary(t, "account", "settings", "get")["two_password_mode"]; mode != "on" {
		t.Errorf("two_password_mode = %v after a login-password change, want on", mode)
	}

	_, stderr = runOKStderrSecondary(t, "account", "settings", "password", "set",
		"--password-file", newFile, "--new-password-file", original)
	assertContains(t, stderr, "Updated your login password")
	restored = true
}

// A password equal to the one it replaces is refused, which is what pointing
// both files at one secret amounts to. It is judged from the two files, so it
// costs no request.
func TestAccountPasswordRefusesTheOneItReplaces(t *testing.T) {
	file := accounts[account.Primary].passwordFile
	_, stderr, code := run(t, "account", "settings", "password", "set",
		"--password-file", file, "--new-password-file", file)
	if code != 1 {
		t.Errorf("exit %d, want 1\nstderr: %s", code, truncateOutput(stderr))
	}
	assertContains(t, stderr, "the one you already have")
}

// A preview asks for nothing and changes nothing.
func TestAccountPasswordDryRunAsksForNothing(t *testing.T) {
	_, stderr := runOKStderr(t, "account", "settings", "password", "set", "--dry-run")
	assertContains(t, stderr, "Dry run")
	for _, prompt := range []string{"Current password:", "New password:"} {
		if strings.Contains(stderr, prompt) {
			t.Errorf("the preview asked for a password: %s", truncateOutput(stderr))
		}
	}
}

// putPasswordBack is the restore a cleanup runs when the round trip did not
// reach its second leg.
func putPasswordBack(t *testing.T, profile, from, to string) error {
	t.Helper()
	_, stderr, code := runProfile(t, profile, "account", "settings", "password", "set",
		"--password-file", from, "--new-password-file", to)
	if code == 0 {
		return nil
	}
	return fmt.Errorf("exit %d: %s", code, strings.TrimSpace(stderr))
}
