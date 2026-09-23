package live

import "testing"

// Emergency access: the people who may get into the account if its owner cannot.
//
// It is a paid feature, so the round trip runs on the paid account, handing the
// second account a sealed copy of the keys and taking it back, so the account
// ends as it began. The contact is referred to by its email throughout, which is
// the handle a listing prints and every verb resolves.

func TestAccountEmergencyAccess(t *testing.T) {
	contact := accountEmail(t, runJSONSecondary)

	_, stderr := runOKStderrPaid(t, "account", "settings", "emergency-access", "add", "--wait", "3d", contact)
	cleanupRunPaid(t, "Remove the emergency contact: proton --profile paid account settings emergency-access remove "+contact,
		"account", "settings", "emergency-access", "remove", contact)
	assertContains(t, stderr, "Added")
	assertContains(t, stderr, contact)

	// It shows up among the people this account granted, with the wait it was
	// given and in the outgoing direction.
	got := runJSONPaid(t, "account", "settings", "emergency-access", "get", contact)
	if got["contact"] != contact {
		t.Errorf("contact = %v, want %q", got["contact"], contact)
	}
	if got["direction"] != "outgoing" {
		t.Errorf("direction = %v, want outgoing", got["direction"])
	}

	list := runJSONArrayPaid(t, "account", "settings", "emergency-access", "list")
	if !containsContact(list, contact) {
		t.Errorf("emergency-access list does not name %q", contact)
	}

	// Changing the wait re-seals the keys under a fresh token; the record stays.
	_, stderr = runOKStderrPaid(t, "account", "settings", "emergency-access", "update", "--wait", "14d", contact)
	assertContains(t, stderr, "wait is now")

	_, stderr = runOKStderrPaid(t, "account", "settings", "emergency-access", "remove", contact)
	assertContains(t, stderr, "Removed")
	after := runJSONArrayPaid(t, "account", "settings", "emergency-access", "list")
	if containsContact(after, contact) {
		t.Errorf("emergency-access list still names %q after removing it", contact)
	}
}

// accountEmail is the email of the account a runner acts as, which is what one
// account names another by when it makes it a trusted contact.
func accountEmail(t *testing.T, get func(*testing.T, ...string) map[string]interface{}) string {
	t.Helper()
	email, _ := get(t, "account", "get")["email"].(string)
	if email == "" {
		t.Fatal("the account has no email to make a trusted contact of")
	}
	return email
}

// containsContact reports whether a delegated-access listing names a contact.
func containsContact(rows []interface{}, contact string) bool {
	for _, r := range rows {
		row, ok := r.(map[string]interface{})
		if ok && row["contact"] == contact {
			return true
		}
	}
	return false
}
