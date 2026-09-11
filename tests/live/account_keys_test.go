package live

import "testing"

// The keys a password reset locks.
//
// What reactivating does cannot be tested against these accounts: only a reset
// locks a key, resetting one would lock that account's Drive for good, and
// nothing puts the keys back afterwards. So what is live here is the one answer
// that needs the account - whether anything is locked - and what the command
// refuses from the command line alone is in tests/offline. The rest is pinned
// against the key material itself, in internal/account/keys/reactivation_test.go.

// An account whose keys are all in use has nothing to reactivate, and hears so
// after the keys are read rather than being asked for a password first.
func TestAccountKeysReactivateOnAnAccountWithNothingLocked(t *testing.T) {
	_, stderr, code := run(t, "account", "keys", "reactivate",
		"--previous-password-file", passwordFile(t, "not the password"),
		"--password-file", passwordFile(t, "not the password either"))
	if code != 1 {
		t.Errorf("reactivating with nothing locked exited %d, want 1", code)
	}
	assertContains(t, stderr, "No keys are locked.")
}
