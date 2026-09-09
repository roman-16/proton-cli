package live

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The extra password Pass can be protected with.
//
// The two free accounts sit either side of it: the secondary one has an extra
// password, which is how this suite covers unlocking Pass at all, and the primary
// has none. So the reading is checked against both, each refusal is checked
// against the account whose state it cannot act on, and the round trip is run on
// the primary, which is the account that starts and ends without one.

// The first assertion is the one that earns the test: it fails if the secondary
// account is ever left without an extra password, because the coverage of
// unlocking Pass would otherwise be gone with nothing saying so.
func TestPassExtraPasswordProtectsTheSecondaryAccount(t *testing.T) {
	extra := runJSONSecondary(t, "pass", "settings", "extra-password", "get")
	if on, _ := extra["enabled"].(bool); !on {
		t.Fatal("the secondary account has no extra password, so nothing here covers unlocking Pass with one")
	}

	scopes := runJSONSecondary(t, "api", "GET", "/core/v4/auth/scopes")
	held, _ := scopes["Scopes"].([]interface{})
	unlocked := false
	for _, s := range held {
		if name, _ := s.(string); name == "pass" {
			unlocked = true
		}
	}
	if !unlocked {
		t.Errorf("the session holds %v, and none of it is the Pass scope the extra password buys", held)
	}

	// And what the scope is for.
	assertContains(t, runOKSecondary(t, "pass", "vaults", "list"), "ID")
}

// The primary account has none, and says so rather than asking for one.
func TestPassExtraPasswordGetReportsEachAccount(t *testing.T) {
	if on, _ := runJSON(t, "pass", "settings", "extra-password", "get")["enabled"].(bool); on {
		t.Error("the primary account has an extra password, which the rest of the suite does not expect")
	}
	assertField(t, runOK(t, "pass", "settings", "extra-password", "get"), "Status:", "off")
}

// Each write is refused by the state it cannot act on, and refused before it asks
// for anything: the suite runs with nothing on standard input, so a prompt here
// would fail rather than hang.
func TestPassExtraPasswordRefusesTheStateItCannotActOn(t *testing.T) {
	_, stderr, code := runSecondary(t, "pass", "settings", "extra-password", "enable")
	if code != 1 {
		t.Errorf("enabling one that is already on: exit %d, want 1\nstderr: %s", code, truncateOutput(stderr))
	}
	assertContains(t, stderr, "already has an extra password")

	_, stderr, code = run(t, "pass", "settings", "extra-password", "disable")
	if code != 1 {
		t.Errorf("disabling one that is off: exit %d, want 1\nstderr: %s", code, truncateOutput(stderr))
	}
	assertContains(t, stderr, "no extra password")
}

// The round trip, which is the only way a run sees either write.
//
// Removing an extra password proves it first, so this is also what covers the SRP
// exchange against Proton rather than against a stub - the sign-in the suite
// performs answers the secondary account's once and never again.
//
// The password is written here rather than generated, because the failure worth
// designing for is this test stopping between the two halves: the account is then
// protected with a password, and the only thing that can take it off is knowing
// which one. Reading it out of this file is what makes that recoverable by hand.
func TestPassExtraPasswordRoundTrip(t *testing.T) {
	const password = "round-trip-extra-password"
	file := filepath.Join(t.TempDir(), "extra")
	if err := os.WriteFile(file, []byte(password), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	// Whatever happens in between, the account goes back to having none: every
	// other test that reaches Pass as this account assumes it, and so does
	// TestPassExtraPasswordGetReportsEachAccount.
	t.Cleanup(func() {
		if on, _ := runJSON(t, "pass", "settings", "extra-password", "get")["enabled"].(bool); !on {
			return
		}
		runOK(t, "pass", "settings", "extra-password", "disable", "--extra-password-file", file)
	})

	_, stderr := runOKStderr(t, "pass", "settings", "extra-password", "enable",
		"--extra-password-file", file)
	assertContains(t, stderr, "Enabled")
	if on, _ := runJSON(t, "pass", "settings", "extra-password", "get")["enabled"].(bool); !on {
		t.Error("the extra password was set, and `get` says the account has none")
	}
	// The session that set it goes on reaching Pass, which is what keeps an
	// unattended run from stopping dead in the middle of this test.
	assertContains(t, runOK(t, "pass", "vaults", "list"), "ID")

	// Removing it proves it first, which is the exchange no wrong answer is spent
	// on here: Proton counts those and ends the session after a few, and only a
	// person can start another. internal/proton/extrapassword_test.go covers being
	// refused, against go-srp's own server.
	_, stderr = runOKStderr(t, "pass", "settings", "extra-password", "disable",
		"--extra-password-file", file)
	assertContains(t, stderr, "Disabled")
	if on, _ := runJSON(t, "pass", "settings", "extra-password", "get")["enabled"].(bool); on {
		t.Error("the extra password was removed, and `get` says the account still has one")
	}
	// Removing it does not end the session that removed it.
	assertContains(t, runOK(t, "account", "get"), "Email")
}

// A preview asks for nothing, which is what makes it safe to suggest to somebody
// who has not decided yet.
func TestPassExtraPasswordDryRunsAskForNothing(t *testing.T) {
	_, stderr := runOKStderr(t, "pass", "settings", "extra-password", "enable", "--dry-run")
	assertContains(t, stderr, "Dry run")
	if strings.Contains(stderr, "Extra password:") {
		t.Errorf("the preview asked for a password: %s", truncateOutput(stderr))
	}

	_, stderr = runOKStderrSecondary(t, "pass", "settings", "extra-password", "disable", "--dry-run")
	assertContains(t, stderr, "Dry run")
	if strings.Contains(stderr, "Extra password:") {
		t.Errorf("the preview asked for a password: %s", truncateOutput(stderr))
	}
}
