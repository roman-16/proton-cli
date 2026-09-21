package live

import (
	"fmt"
	"strings"
	"testing"
)

// The recovery address.
//
// The round trip runs on the primary account and puts back whatever address was
// there. One thing it cannot put back is the verification: Proton marks a
// re-written address unverified, so the account ends the run with its recovery
// email unverified and a fresh verification mail sent to it. Opening the link
// in that mail is what restores it.

func TestAccountRecoveryEmailRoundTrip(t *testing.T) {
	before := runJSON(t, "account", "settings", "recovery-email", "get")
	original, _ := before["address"].(string)
	allowed, _ := before["allow_recovery"].(bool)
	address := externalRecipient(t)

	cleanup(t, "put the primary account's recovery email back: "+
		"proton --profile primary account settings recovery-email set "+orNone(original),
		func() error { return putRecoveryEmailBack(t, original, allowed) })

	_, stderr := runOKStderr(t, "account", "settings", "recovery-email", "set", address)
	assertContains(t, stderr, "Set recovery email to "+address)
	// A freshly written address is Proton's to verify, and until it is the
	// answer says so rather than leaving it to be discovered at a reset.
	after := runJSON(t, "account", "settings", "recovery-email", "get")
	if after["address"] != address {
		t.Errorf("address = %v, want %q", after["address"], address)
	}
	if verified, _ := after["verified"].(bool); verified {
		t.Error("a newly written address is not verified yet")
	}

	// Whether Proton may reset the password through it is a second question,
	// and the address stays whichever way it is answered. Which way to turn it
	// first is read rather than assumed: Proton decides what a freshly written
	// address is allowed to do, and each verb refuses the state it is already
	// in.
	now, _ := after["allow_recovery"].(bool)
	for _, want := range []bool{!now, now} {
		_, stderr = runOKStderr(t, "account", "settings", "recovery-email", verb(want))
		assertContains(t, stderr, "recovery by email")
		state := runJSON(t, "account", "settings", "recovery-email", "get")
		if on, _ := state["allow_recovery"].(bool); on != want {
			t.Errorf("allow_recovery = %v after %s, want %v", on, verb(want), want)
		}
		if state["address"] != address {
			t.Errorf("address = %v after %s, want it kept", state["address"], verb(want))
		}
	}

	// One verification mail per run, which is what the send costs.
	_, stderr = runOKStderr(t, "account", "settings", "recovery-email", "verify")
	assertContains(t, stderr, "Sent 1 verification email to "+address)

	// And the word that takes the address away.
	_, stderr = runOKStderr(t, "account", "settings", "recovery-email", "set", "none")
	assertContains(t, stderr, "Set recovery email to none")
	if got := runJSON(t, "account", "settings", "recovery-email", "get")["address"]; got != "" {
		t.Errorf("address = %v after removing it, want empty", got)
	}
}

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

// putRecoveryEmailBack restores the address and the choice that was made about
// it, in that order: allowing recovery needs an address to allow it through.
func putRecoveryEmailBack(t *testing.T, address string, allowed bool) error {
	t.Helper()
	now := runJSON(t, "account", "settings", "recovery-email", "get")
	if current, _ := now["address"].(string); current != address {
		if _, stderr, code := run(t, "account", "settings", "recovery-email", "set",
			orNone(address)); code != 0 {
			return fmt.Errorf("exit %d: %s", code, strings.TrimSpace(stderr))
		}
	}
	if address == "" {
		return nil
	}
	if on, _ := runJSON(t, "account", "settings", "recovery-email",
		"get")["allow_recovery"].(bool); on == allowed {
		return nil
	}
	if _, stderr, code := run(t, "account", "settings", "recovery-email", verb(allowed)); code != 0 {
		return fmt.Errorf("put recovery by email back: exit %d: %s", code, strings.TrimSpace(stderr))
	}
	return nil
}

// orNone is the argument that writes a value, and the word for writing none.
func orNone(value string) string {
	if value == "" {
		return "none"
	}
	return value
}

// verb is the command that puts a toggle into the state wanted.
func verb(on bool) string {
	if on {
		return "enable"
	}
	return "disable"
}
