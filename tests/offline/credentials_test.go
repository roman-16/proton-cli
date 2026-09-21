package offline

import (
	"strings"
	"testing"
)

// Changing a credential asks for the current password, so everything about the
// new one that the command line settles is settled before a password is asked
// for: how long it is, whether it is the one it replaces, and whether an
// address or a number is one at all. A run that got as far as Proton before
// saying any of this would have asked for a secret on the way.

// A password Proton would refuse is refused here, from the file it arrived in.
func TestANewPasswordIsJudgedBeforeTheNetwork(t *testing.T) {
	short := secretFile(t, "sh0rt")
	for _, args := range [][]string{
		{"account", "settings", "password", "set"},
		{"account", "settings", "second-password", "enable"},
		{"account", "settings", "second-password", "set"},
	} {
		refuses(t, 1, append(args, "--new-password-file", short),
			"at least eight characters")
	}
}

// Both flags pointed at one file is the mistake the second flag invites, and
// the two values settle it between them.
func TestAPasswordSetToTheOneItReplacesIsRefused(t *testing.T) {
	same := secretFile(t, "the-same-password")
	refuses(t, 1, []string{"account", "settings", "password", "set",
		"--password-file", same, "--new-password-file", same},
		"the one you already have")
	refuses(t, 1, []string{"account", "settings", "second-password", "enable",
		"--password-file", same, "--new-password-file", same},
		"the password you sign in with")
}

// An empty file is somebody meaning to put a secret there, not an empty secret.
func TestAnEmptyNewPasswordFileIsRefused(t *testing.T) {
	refuses(t, 1, []string{"account", "settings", "password", "set",
		"--new-password-file", secretFile(t, "")}, "is empty")
}

// A recovery address and a recovery number are judged by their shape, and the
// word that removes one is not mistaken for either.
func TestARecoveryAddressIsJudgedBeforeTheNetwork(t *testing.T) {
	for _, arg := range []string{"not-an-address", "jane roe@example.com", "Jane <jane@example.com>"} {
		refuses(t, 1, []string{"account", "settings", "recovery-email", "set", arg},
			"not an email address")
	}
}

func TestARecoveryNumberIsJudgedBeforeTheNetwork(t *testing.T) {
	for _, arg := range []string{"0660 1234567", "+43 1", "+43 660 12345678901234", "phone"} {
		refuses(t, 1, []string{"account", "settings", "recovery-phone", "set", arg},
			"not a phone number")
	}
}

// Turning a second factor on with nobody to ask and no code is answered by the
// two commands that do it instead, before anything is minted.
func TestTurningOnTwoFactorWithoutACodeSaysHowToScriptIt(t *testing.T) {
	refuses(t, 2, []string{"account", "settings", "two-factor", "enable"},
		"two-factor generate", "--totp")
}

// The commands that change a credential all take the password as a path, so
// none of them can be handed one on the command line.
func TestNoCredentialCommandTakesAPasswordAsAFlagValue(t *testing.T) {
	for _, args := range [][]string{
		{"account", "settings", "password", "set", "--password", "hunter2"},
		{"account", "settings", "password", "set", "--new-password", "hunter2"},
		{"account", "settings", "second-password", "enable", "--second-password", "hunter2"},
	} {
		_, stderr, code := run(t, args...)
		if code != 1 {
			t.Errorf("%v: exit %d, want 1\nstderr: %s", args, code, truncate(stderr))
		}
		if !strings.Contains(stderr, "Unknown flag") {
			t.Errorf("%v: stderr does not refuse the flag\nstderr: %s", args, truncate(stderr))
		}
	}
}
