package live

import (
	"strings"
	"testing"
)

// The security keys the account signs in with.
//
// The ceremony itself is the one thing here no run may reach: registering a key
// means a person touching one, and a run on a machine with a key plugged in
// would either wait for a hand or enrol somebody's key on a test account for
// good. So what is checked is everything around it - the listing, the preview,
// and the refusals - and internal/fido holds the ceremony against a key made of
// software.

// The listing, in both formats: the ID a key is addressed by and the name it
// was given, read off the settings record Proton writes the credential into.
func TestAccountSecurityKeysList(t *testing.T) {
	stdout, stderr := runOKStderr(t, "account", "settings", "security-keys", "list")
	// No test account has a key, and either answer is the command working.
	answer := stdout + stderr
	if !strings.Contains(answer, "No security keys.") && !strings.Contains(answer, "NAME") {
		t.Errorf("the listing said neither that there are none nor what there is:\n%s", answer)
	}

	keys := runJSONArray(t, "account", "settings", "security-keys", "list")
	for _, key := range keys {
		row, _ := key.(map[string]interface{})
		for _, field := range []string{"id", "name"} {
			if _, ok := row[field]; !ok {
				t.Errorf("missing %q in %v", field, keysOf(row))
			}
		}
	}
}

// What `two-factor get` says and what the collection lists are the same keys,
// so a key that appears in one appears in the other.
func TestAccountSecurityKeysAgreeWithTheSecondFactor(t *testing.T) {
	named, _ := runJSON(t, "account", "settings", "two-factor", "get")["security_keys"].([]interface{})
	listed := runJSONArray(t, "account", "settings", "security-keys", "list")
	if len(named) != len(listed) {
		t.Errorf("two-factor get names %d keys and the listing has %d", len(named), len(listed))
	}
}

// The preview of a registration, which is as far as a run may go: it sends
// nothing, asks for no password, and waits for nobody.
func TestAccountSecurityKeysCreateDryRun(t *testing.T) {
	_, stderr := runOKStderr(t, "--dry-run", "account", "settings", "security-keys",
		"create", "--name", testID()+"-key")
	assertContains(t, stderr, "Dry run")
	assertContains(t, stderr, "security key")
}

// A reference that matches no key is exit 3, before anything is changed.
func TestAccountSecurityKeysRefuseAKeyThatIsNotThere(t *testing.T) {
	for _, args := range [][]string{
		{"account", "settings", "security-keys", "update", "no-such-key", "--name", "whatever"},
		{"account", "settings", "security-keys", "delete", "--yes", "no-such-key"},
	} {
		_, stderr, code := run(t, args...)
		if code != 3 {
			t.Errorf("%v: exit %d, want 3\nstderr: %s", args, code, truncateOutput(stderr))
		}
		assertContains(t, stderr, "no-such-key")
	}
}
