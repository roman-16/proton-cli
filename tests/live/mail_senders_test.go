package live

import (
	"fmt"
	"strings"
	"testing"
)

// Standing decisions about senders.

// Proton's three lists are one record with a destination on it, so one listing
// answers "what have I decided about whom".
func TestMailSendersBlockAllowAndForget(t *testing.T) {
	addr := testID() + "@example.com"

	runOK(t, "mail", "settings", "senders", "block", addr)
	cleanupRun(t, fmt.Sprintf("Forget sender: proton mail settings senders forget %s", addr),
		"mail", "settings", "senders", "forget", addr)
	assertSenderGoes(t, addr, "blocked")

	// Deciding again replaces the earlier decision rather than colliding.
	runOK(t, "mail", "settings", "senders", "spam", addr)
	assertSenderGoes(t, addr, "spam")
	runOK(t, "mail", "settings", "senders", "allow", addr)
	assertSenderGoes(t, addr, "inbox")

	runOK(t, "mail", "settings", "senders", "forget", addr)
	if senderRule(t, addr) != nil {
		t.Error("after forgetting, the sender should carry no standing decision")
	}
}

// A whole domain is a rule too, written with the @.
func TestMailSendersTakeAWholeDomain(t *testing.T) {
	domain := "@" + testID() + ".example.com"

	runOK(t, "mail", "settings", "senders", "block", domain)
	cleanupRun(t, fmt.Sprintf("Forget domain: proton mail settings senders forget %s", domain),
		"mail", "settings", "senders", "forget", domain)
	assertSenderGoes(t, domain, "blocked")
}

func senderRule(t *testing.T, target string) map[string]interface{} {
	t.Helper()
	for _, row := range runJSONArray(t, "mail", "settings", "senders", "list") {
		m, _ := row.(map[string]interface{})
		email, _ := m["email"].(string)
		domain, _ := m["domain"].(string)
		if strings.EqualFold(email, target) || (domain != "" && strings.EqualFold("@"+domain, target)) {
			return m
		}
	}
	return nil
}

func assertSenderGoes(t *testing.T, target, want string) {
	t.Helper()
	rule := senderRule(t, target)
	if rule == nil {
		t.Fatalf("no standing decision found for %s", target)
	}
	if got, _ := rule["goes"].(string); got != want {
		t.Errorf("%s goes to %q, want %q", target, got, want)
	}
}
