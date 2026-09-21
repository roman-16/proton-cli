package live

import (
	"fmt"
	"strings"
	"testing"

	"github.com/roman-16/proton-cli/tests/account"
)

// Two-password mode, entered and left.
//
// The whole of it runs on the primary account, which starts and ends with one
// password: the secondary is the account whose two-password mode the rest of
// the suite depends on, and a run that left its second password changed would
// take that coverage away. So the primary enters the mode, changes the second
// password inside it, and leaves again - which is every write this collection
// has, in the one window where a failure is recoverable from this file.
//
// The second passwords are written here rather than generated, for the reason
// the passwords in account_password_test.go are: a run that stops in the middle
// leaves the account's keys locked with one of them, and reading it out of here
// is what makes that recoverable by hand.
const (
	firstSecondPassword  = "proton-cli-round-trip-second-password"
	secondSecondPassword = "proton-cli-round-trip-second-password-again"
)

func TestAccountSecondPasswordRoundTrip(t *testing.T) {
	first := passwordFile(t, firstSecondPassword)
	second := passwordFile(t, secondSecondPassword)

	if mode := runJSON(t, "account", "settings", "second-password", "get")["enabled"]; mode != false {
		t.Fatalf("the primary account is already in two-password mode (enabled = %v), "+
			"which the rest of the suite does not expect", mode)
	}

	left := false
	cleanup(t, "the primary account is in two-password mode, with one of the second passwords "+
		"in tests/live/account_second_password_test.go. Leave it with: proton --profile primary "+
		"account settings second-password disable",
		func() error {
			if left {
				return nil
			}
			_, stderr, code := run(t, "account", "settings", "second-password", "disable")
			if code != 0 {
				return fmt.Errorf("exit %d: %s", code, strings.TrimSpace(stderr))
			}
			return nil
		})

	_, stderr := runOKStderr(t, "account", "settings", "second-password", "enable",
		"--new-password-file", first)
	assertContains(t, stderr, "two-password mode")
	if mode := runJSON(t, "account", "settings", "get")["two_password_mode"]; mode != "on" {
		t.Errorf("two_password_mode = %v after enabling, want on", mode)
	}
	// The session that made the change goes on working: the key password sealed
	// into it was renewed with the keys.
	assertContains(t, runOK(t, "mail", "messages", "list", "--limit", "1"), "ID")

	// Changing it inside the mode is the same request with a different secret.
	_, stderr = runOKStderr(t, "account", "settings", "second-password", "set",
		"--new-password-file", second)
	assertContains(t, stderr, "Updated your second password")
	assertContains(t, runOK(t, "mail", "messages", "list", "--limit", "1"), "ID")

	_, stderr = runOKStderr(t, "account", "settings", "second-password", "disable")
	assertContains(t, stderr, "two-password mode")
	left = true
	if mode := runJSON(t, "account", "settings", "second-password", "get")["enabled"]; mode != false {
		t.Errorf("enabled = %v after leaving two-password mode, want false", mode)
	}
	// The account password opens the keys again, which is the whole of what
	// leaving the mode means.
	assertContains(t, runOK(t, "mail", "messages", "list", "--limit", "1"), "ID")
}

// Each write is refused by the state it cannot act on, before it asks for
// anything: the suite has nothing on standard input, so a prompt would fail
// rather than hang.
func TestAccountSecondPasswordRefusesTheStateItCannotActOn(t *testing.T) {
	// The primary keeps one password, so there is no second one to change or
	// take away.
	for _, verb := range []string{"set", "disable"} {
		_, stderr, code := run(t, "account", "settings", "second-password", verb)
		if code != 1 {
			t.Errorf("%s on a one-password account: exit %d, want 1\nstderr: %s",
				verb, code, truncateOutput(stderr))
		}
		assertContains(t, stderr, "does not use two-password mode")
	}

	// The secondary already keeps two.
	_, stderr, code := runSecondary(t, "account", "settings", "second-password", "enable")
	if code != 1 {
		t.Errorf("enabling on an account already in the mode: exit %d, want 1\nstderr: %s",
			code, truncateOutput(stderr))
	}
	assertContains(t, stderr, "already uses two-password mode")
}

// A second password equal to the one that signs in is refused, and the refusal
// says what the person probably wanted instead.
func TestAccountSecondPasswordRefusesTheLoginPassword(t *testing.T) {
	file := accounts[account.Secondary].passwordFile
	_, stderr, code := runSecondary(t, "account", "settings", "second-password", "set",
		"--password-file", file, "--new-password-file", file)
	if code != 1 {
		t.Errorf("exit %d, want 1\nstderr: %s", code, truncateOutput(stderr))
	}
	assertContains(t, stderr, "the password you sign in with")
	assertContains(t, stderr, "second-password disable")
}

func TestAccountSecondPasswordDryRunsAskForNothing(t *testing.T) {
	_, stderr := runOKStderr(t, "account", "settings", "second-password", "enable", "--dry-run")
	assertContains(t, stderr, "Dry run")
	_, stderr = runOKStderrSecondary(t, "account", "settings", "second-password", "disable", "--dry-run")
	assertContains(t, stderr, "Dry run")
	if strings.Contains(stderr, "password:") {
		t.Errorf("the preview asked for a password: %s", truncateOutput(stderr))
	}
}
