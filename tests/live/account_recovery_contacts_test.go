package live

import "testing"

// Data-recovery contacts: the people who may help the account back in.
//
// The round trip runs on the primary account and hands the second account a
// sealed copy of the keys, then takes it back, so the account ends as it began.

func TestAccountRecoveryContacts(t *testing.T) {
	contact := accountEmail(t, runJSONSecondary)

	_, stderr := runOKStderr(t, "account", "settings", "recovery-contacts", "add", contact)
	cleanupRun(t, "Remove the recovery contact: proton account settings recovery-contacts remove "+contact,
		"account", "settings", "recovery-contacts", "remove", contact)
	assertContains(t, stderr, "Added")
	assertContains(t, stderr, contact)

	got := runJSON(t, "account", "settings", "recovery-contacts", "get", contact)
	if got["contact"] != contact {
		t.Errorf("contact = %v, want %q", got["contact"], contact)
	}
	if got["direction"] != "outgoing" {
		t.Errorf("direction = %v, want outgoing", got["direction"])
	}

	list := runJSONArray(t, "account", "settings", "recovery-contacts", "list")
	if !containsContact(list, contact) {
		t.Errorf("recovery-contacts list does not name %q", contact)
	}

	// The second account sees this one among the accounts it can help recover.
	incoming := runJSONArraySecondary(t, "account", "settings", "recovery-contacts", "list", "--incoming")
	if me := accountEmail(t, runJSON); !containsContact(incoming, me) {
		t.Errorf("the second account's incoming list does not name %q", me)
	}

	_, stderr = runOKStderr(t, "account", "settings", "recovery-contacts", "remove", contact)
	assertContains(t, stderr, "Removed")
	after := runJSONArray(t, "account", "settings", "recovery-contacts", "list")
	if containsContact(after, contact) {
		t.Errorf("recovery-contacts list still names %q after removing it", contact)
	}
}
