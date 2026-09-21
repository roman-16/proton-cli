package live

import (
	"strings"
	"testing"

	"github.com/roman-16/proton-cli/tests/account"
)

// Access tokens, which a plan gates: a way into Pass for a program, reading the
// vaults it was handed and nothing else.
//
// A token's key is sealed to the account's user key for Proton to hold and
// handed out in the clear once, beside the token. Everything a run makes it
// makes on vaults of its own and deletes again, by the name that carries the
// suite's prefix, which is what tests/paid keeps a delete pointed at.

// aVaultTheRunMade makes a vault on the paid account with one item in it, and
// registers its removal.
func aVaultTheRunMade(t *testing.T, purpose string) (shareID, name string) {
	t.Helper()
	name = testID() + "-" + purpose
	out, stderr, code := runPaid(t, "--yes", "pass", "vaults", "create", "--name", name)
	if code != 0 {
		t.Fatalf("could not make a vault: %s", truncateOutput(stderr))
	}
	shareID = strings.TrimSpace(out)
	cleanupRunPaid(t, "Delete vault: proton pass vaults delete "+shareID,
		"pass", "vaults", "delete", shareID)
	return shareID, name
}

// The whole life of a token: made with a vault to read, listed and shown,
// pointed at another vault, and deleted.
func TestPassAccessTokenRoundTrip(t *testing.T) {
	first, firstName := aVaultTheRunMade(t, "token-vault")
	second, secondName := aVaultTheRunMade(t, "other-vault")
	name := testID() + "-token"

	// A day rather than an hour: a token within an hour of expiring reads as
	// expiring, so one made to live an hour would never have read as active.
	stdout, stderr := runOKStderrPaid(t, "pass", "settings", "access-tokens", "create",
		"--name", name, "--expires", "1d", "--vault", first)
	cleanupRunPaid(t, "Delete access token: proton pass settings access-tokens delete "+name,
		"pass", "settings", "access-tokens", "delete", name)

	// The token is the answer and is shown once: the key that opens the vaults
	// follows the token Proton minted, and the warning stays out of what a
	// redirect captures.
	assertField(t, stdout, "Name:", name)
	assertField(t, stdout, "Vaults:", firstName)
	var secret string
	for _, line := range strings.Split(stdout, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "Token:") {
			secret = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "Token:"))
		}
	}
	if !strings.Contains(secret, "::") || strings.HasPrefix(secret, "::") || strings.HasSuffix(secret, "::") {
		t.Errorf("the token %q does not carry its key after the '::'", secret)
	}
	assertContains(t, stderr, "shown once")

	var token map[string]interface{}
	for _, row := range runJSONArrayPaid(t, "pass", "settings", "access-tokens", "list") {
		m, _ := row.(map[string]interface{})
		if m["name"] == name {
			token = m
		}
		// The listing never carries a token.
		if s, _ := m["secret"].(string); s != "" {
			t.Errorf("a token listing carries a token: %q", s)
		}
	}
	if token == nil {
		t.Fatal("the token this test made is not in the listing")
	}
	if token["status"] != "active" || token["agent"] != false {
		t.Errorf("a new token reads %v", token)
	}

	shown := runJSONPaid(t, "pass", "settings", "access-tokens", "get", name)
	if shown["id"] != token["id"] {
		t.Errorf("get answered for %v, want %v", shown["id"], token["id"])
	}
	vaults, _ := shown["vaults"].([]interface{})
	if len(vaults) != 1 {
		t.Fatalf("the token reads %d vaults, want the one it was handed: %v", len(vaults), vaults)
	}
	if granted, _ := vaults[0].(map[string]interface{}); granted["share_id"] != first || granted["vault"] != firstName {
		t.Errorf("the token reads %v, want %s", granted, firstName)
	}

	// The vaults are named whole: the one not named is taken back.
	runOKPaid(t, "pass", "settings", "access-tokens", "update", "--vault", second, name)
	shown = runJSONPaid(t, "pass", "settings", "access-tokens", "get", name)
	vaults, _ = shown["vaults"].([]interface{})
	if len(vaults) != 1 {
		t.Fatalf("after the change the token reads %d vaults, want 1: %v", len(vaults), vaults)
	}
	if granted, _ := vaults[0].(map[string]interface{}); granted["vault"] != secondName {
		t.Errorf("after the change the token reads %v, want %s", granted, secondName)
	}
	assertField(t, runOKPaid(t, "pass", "settings", "access-tokens", "get", name), "Vaults:", secondName)

	// A token nobody has used has done nothing, and the log says so.
	actions := runJSONArrayPaid(t, "pass", "settings", "access-tokens", "activity", "list", name)
	if len(actions) != 0 {
		t.Errorf("a token nobody has used has %d actions on record", len(actions))
	}

	runOKPaid(t, "pass", "settings", "access-tokens", "delete", name)
	if _, _, code := runPaid(t, "--yes", "pass", "settings", "access-tokens", "get", name); code != 3 {
		t.Errorf("a deleted token answers with exit %d, want 3", code)
	}
}

// A token for an agent is marked as one, and its record of actions opens even
// while it is empty.
func TestPassAccessTokenForAnAgent(t *testing.T) {
	vault, _ := aVaultTheRunMade(t, "agent-vault")
	name := testID() + "-agent"

	made := runJSONPaid(t, "pass", "settings", "access-tokens", "create",
		"--name", name, "--expires", "1d", "--vault", vault, "--agent")
	cleanupRunPaid(t, "Delete access token: proton pass settings access-tokens delete "+name,
		"pass", "settings", "access-tokens", "delete", name)
	if made["agent"] != true || made["status"] != "active" {
		t.Errorf("an agent token reads %v", made)
	}
	if secret, _ := made["secret"].(string); !strings.Contains(secret, "::") {
		t.Errorf("the token %q does not carry its key", secret)
	}
	stdout := runOKPaid(t, "pass", "settings", "access-tokens", "list")
	assertContains(t, stdout, name)

	if _, stderr, code := runPaid(t, "pass", "settings", "access-tokens", "activity", "list", name); code != 0 {
		t.Errorf("reading an agent's record exits %d: %s", code, truncateOutput(stderr))
	}
}

// An account with no plan has no tokens, and is told what is missing before a
// token is made.
func TestPassAccessTokensNeedAPlan(t *testing.T) {
	pinned(t, account.Primary, "vault", "Personal")
	_, stderr, code := run(t, "--yes", "pass", "settings", "access-tokens", "create",
		"--name", testID()+"-token", "--expires", "1d", "--vault", "Personal")
	if code != 1 {
		t.Errorf("a free account making a token exits %d, want 1", code)
	}
	if !strings.Contains(stderr, "need a paid Pass plan") {
		t.Errorf("the refusal does not say what is missing: %s", truncateOutput(stderr))
	}
}
