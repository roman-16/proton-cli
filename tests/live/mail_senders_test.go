package live

import (
	"fmt"
	"strings"
	"testing"
)

// Standing decisions about senders, and forwarding.
//
// Proton's three lists are one record with a destination on it, so one listing
// answers "what have I decided about whom".

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

// Both directions come back as one collection, which is the only part of
// forwarding these accounts can reach: setting one up is a paid feature, and
// accepting one is not built. See `untested` in internal/cli/coverage_test.go.
func TestMailForwardingListsBothDirections(t *testing.T) {
	rows := runJSONArray(t, "mail", "settings", "forwarding", "list")
	for _, row := range rows {
		m, _ := row.(map[string]interface{})
		switch m["direction"] {
		case "incoming", "outgoing":
		default:
			t.Errorf("a forwarding came back going neither way: %v", m["direction"])
		}
	}
}

// Forwarding to an address outside Proton is refused before anything is
// derived: Proton emails such an address a link its owner must follow, which no
// command can answer.
func TestMailForwardingRefusesAnAddressOutsideProton(t *testing.T) {
	_, stderr, code := run(t, "mail", "settings", "forwarding", "create", selfEmail(), "nobody@example.com")
	if code == 0 {
		t.Fatal("an address outside Proton was accepted")
	}
	if !strings.Contains(stderr, "not a Proton address") &&
		!strings.Contains(stderr, "address does not exist") {
		t.Errorf("the refusal does not name the address as the problem: %s", truncateOutput(stderr))
	}
}
