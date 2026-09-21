package live

import (
	"strings"
	"testing"
)

// SMTP tokens, which let a device send from a custom domain address.
//
// A plan gates them and a custom domain address carries them, so everything
// that makes one is the paid account's - and what the run makes is the token,
// carrying the suite's prefix and deleted again by it.

// The whole life of a token: made for the fixture address, listed, shown, and
// deleted. The token itself is on the screen once, at the moment it is made.
func TestMailSettingsSMTPTokenRoundTrip(t *testing.T) {
	address := paidCustomAddress(t)
	name := testID() + "-token"

	stdout, stderr := runOKStderrPaid(t, "mail", "settings", "smtp-tokens", "create",
		"--name", name, address)
	cleanupRunPaid(t, "Delete smtp token: proton mail settings smtp-tokens delete "+name,
		"mail", "settings", "smtp-tokens", "delete", name)

	// What a device is set up with: the token as its password, the address as
	// its username, and where to connect.
	assertField(t, stdout, "Address:", address)
	assertField(t, stdout, "Server:", "smtp.protonmail.ch")
	assertField(t, stdout, "Port:", "587")
	assertField(t, stdout, "Name:", name)
	var secret string
	for _, line := range strings.Split(stdout, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "Token:") {
			secret = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "Token:"))
		}
	}
	if secret == "" {
		t.Fatalf("the token is not in what create answered with: %s", truncateOutput(stdout))
	}
	assertContains(t, stderr, "shown once")

	var token map[string]interface{}
	for _, row := range runJSONArrayPaid(t, "mail", "settings", "smtp-tokens", "list") {
		m, _ := row.(map[string]interface{})
		if m["name"] == name {
			token = m
		}
		// A listing never carries a token.
		if s, _ := m["token"].(string); s != "" {
			t.Errorf("a token listing carries a token: %q", s)
		}
	}
	if token == nil {
		t.Fatal("the token this test made is not in the listing")
	}
	if token["address"] != address {
		t.Errorf("the token sends from %v, want %s", token["address"], address)
	}
	if token["last_used"] != nil {
		t.Errorf("a token nothing has sent with was last used at %v", token["last_used"])
	}

	shown := runJSONPaid(t, "mail", "settings", "smtp-tokens", "get", name)
	if shown["id"] != token["id"] {
		t.Errorf("get answered for %v, want %v", shown["id"], token["id"])
	}
	if s, _ := shown["token"].(string); s != "" {
		t.Errorf("a token that was shown once came back: %q", s)
	}
	if shown["server"] != "smtp.protonmail.ch" || shown["port"] != float64(587) {
		t.Errorf("get says to connect to %v:%v", shown["server"], shown["port"])
	}

	runOKPaid(t, "mail", "settings", "smtp-tokens", "delete", name)
	if _, _, code := runPaid(t, "--yes", "mail", "settings", "smtp-tokens", "get", name); code != 3 {
		t.Errorf("a deleted token answers with exit %d, want 3", code)
	}
}

// A token sends from a custom domain address, so an address on a Proton domain
// is turned away before anything is asked of Proton.
func TestMailSettingsSMTPTokensNeedACustomDomainAddress(t *testing.T) {
	_, stderr, code := run(t, "--yes", "mail", "settings", "smtp-tokens", "create",
		"--name", testID()+"-token", selfEmail())
	if code != 1 {
		t.Errorf("making a token for a Proton address exits %d, want 1", code)
	}
	if !strings.Contains(stderr, "custom domain address") {
		t.Errorf("the refusal does not say what kind of address is needed: %s", truncateOutput(stderr))
	}
}

// An account with no plan has no custom domain, so it can hold no tokens.
// Whichever way Proton answers its listing - with nothing, or by refusing -
// what reaches the screen is an answer rather than a server error.
func TestMailSettingsSMTPTokensOnAFreeAccount(t *testing.T) {
	stdout, stderr, code := run(t, "mail", "settings", "smtp-tokens", "list")
	switch code {
	case 0:
		if strings.TrimSpace(stdout) != "" {
			t.Errorf("an account with no custom domain holds tokens: %s", truncateOutput(stdout))
		}
	case 1:
		if !strings.Contains(stderr, "need a paid Mail plan") {
			t.Errorf("the refusal does not say what is missing: %s", truncateOutput(stderr))
		}
	default:
		t.Errorf("a free account's listing exits %d: %s", code, truncateOutput(stderr))
	}
	if strings.Contains(stderr, "HTTP 500") {
		t.Errorf("Proton's own server error reached the screen: %s", truncateOutput(stderr))
	}
}
