package live

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// Sharing a vault out of Pass, which a plan gates.
//
// What travels is every rotation of the key that opens it, encrypted to the
// recipient's and signed with yours - so the assertion that matters is the last
// one, that the other account can actually read what is inside.
//
// Sharing a single item is not here: Proton allows an item invitation only to
// somebody on a paid plan, and the account this shares with is a free one. See
// `untested` in internal/cli/coverage_test.go.

// Sharing a vault hands somebody every rotation of the key that opens it. This
// is the whole round trip, and the assertion that matters is the last one: an
// item in the vault reads on the other account, which is only true if the keys
// it was given actually open what is in there.
func TestPassVaultSharingRoundTrip(t *testing.T) {
	name := testID() + "-shared-vault"
	out, stderr, code := runPaid(t, "--yes", "pass", "vaults", "create", "--name", name)
	if code != 0 {
		t.Fatalf("could not make a vault to share: %s", truncateOutput(stderr))
	}
	vault := strings.TrimSpace(out)
	cleanupRunPaid(t, "Delete vault: proton pass vaults delete "+vault,
		"pass", "vaults", "delete", vault)

	// Something in it, so the other side has more to open than an empty vault.
	secret := testID() + "-in-shared-vault"
	runOKPaid(t, "pass", "items", "create", "--vault", vault, "--name", secret,
		"--username", "jane", "--secret-file", secretFile(t, "password", "hunter2"))

	runOKPaid(t, "pass", "vaults", "share", "add", vault, secondaryEmail())

	// The other side sees the offer, and can read the vault's name before taking
	// it - that much travels with the invitation.
	var token string
	waitFor(60*time.Second, 3*time.Second, func() bool {
		for _, row := range runJSONArraySecondary(t, "pass", "invitations", "list") {
			m, _ := row.(map[string]interface{})
			if v, _ := m["vault"].(string); v != name {
				continue
			}
			token, _ = m["id"].(string)
			return token != ""
		}
		return false
	})
	if token == "" {
		t.Fatal("the offer never reached the second account, or its vault could not be named")
	}
	runOKSecondary(t, "pass", "invitations", "accept", "--", token)

	// The item reads on the other account, which is the proof the keys opened.
	var found bool
	waitFor(60*time.Second, 3*time.Second, func() bool {
		for _, row := range runJSONArraySecondary(t, "pass", "items", "list") {
			m, _ := row.(map[string]interface{})
			if n, _ := m["name"].(string); n == secret {
				found = true
				return true
			}
		}
		return false
	})
	if !found {
		t.Error("the shared vault's item did not read on the account that took it")
	}

	// Somebody who accepted is a member, which is a different thing from an
	// invitation and the only one whose access can be changed in place.
	shared := runJSONPaid(t, "pass", "vaults", "share", "get", vault)
	if !strings.Contains(fmt.Sprintf("%v", shared["members"]), secondaryEmail()) {
		t.Fatalf("the second account accepted the vault and is not a member: %v", shared["members"])
	}
	if !strings.Contains(fmt.Sprintf("%v", shared["members"]), "owner") {
		t.Errorf("the owner is not among the members: %v", shared["members"])
	}

	// A backup is of what you own. Somebody else's vault is theirs to back up,
	// and an archive that leaves one out says how many.
	_, note := runOKStderrSecondary(t, "pass", "export", "--dest", "-")
	if !strings.Contains(note, "shared with you") {
		t.Errorf("the second account's export says nothing about the vault it does not own: %s", truncateOutput(note))
	}

	runOKPaid(t, "pass", "vaults", "share", "update", vault, secondaryEmail(), "--access", "manager")
	if !strings.Contains(fmt.Sprintf("%v", runJSONPaid(t, "pass", "vaults", "share", "get", vault)["members"]), "manager") {
		t.Error("the member's access did not change")
	}

	// Handing it over, and taking it back. Only an owner may transfer, so the
	// return trip is the other account's to make - and if it fails, the vault is
	// theirs and the clean-up says so loudly rather than leaving it unsaid.
	//
	// The address to hand it back to is the one Proton reports, which is the
	// account's primary Proton address and not necessarily the address it signs
	// in as.
	owner := memberEmail(t, shared["members"], true)
	if owner == "" {
		t.Fatalf("the vault has no owner among its members: %v", shared["members"])
	}
	runOKPaid(t, "pass", "vaults", "transfer", vault, secondaryEmail())
	cleanupRunSecondary(t, "Give the vault back: proton pass vaults transfer "+name+" "+owner,
		"pass", "vaults", "transfer", name, owner)
	if was := fmt.Sprint(runJSONPaid(t, "pass", "vaults", "get", vault)["owner"]); was != "false" {
		t.Errorf("after handing the vault over, owner = %s", was)
	}
	runOKSecondary(t, "pass", "vaults", "transfer", name, owner)
	if was := fmt.Sprint(runJSONPaid(t, "pass", "vaults", "get", vault)["owner"]); was != "true" {
		t.Errorf("after taking the vault back, owner = %s", was)
	}

	runOKPaid(t, "pass", "vaults", "share", "remove", vault, secondaryEmail())
	if left := runJSONPaid(t, "pass", "vaults", "share", "get", vault); strings.Contains(
		fmt.Sprintf("%v", left["members"]), secondaryEmail()) {
		t.Errorf("the member is still there after being removed: %v", left["members"])
	}
}

// An offer can be withdrawn before it is taken.
func TestPassVaultShareWithdrawn(t *testing.T) {
	name := testID() + "-withdrawn-vault"
	out, stderr, code := runPaid(t, "--yes", "pass", "vaults", "create", "--name", name)
	if code != 0 {
		t.Fatalf("could not make a vault: %s", truncateOutput(stderr))
	}
	vault := strings.TrimSpace(out)
	cleanupRunPaid(t, "Delete vault: proton pass vaults delete "+vault,
		"pass", "vaults", "delete", vault)

	runOKPaid(t, "pass", "vaults", "share", "add", vault, secondaryEmail())

	shared := runJSONPaid(t, "pass", "vaults", "share", "get", vault)
	if !strings.Contains(fmt.Sprintf("%v", shared["invited"]), secondaryEmail()) {
		t.Fatalf("the offer is not on the vault it was made for: %v", shared["invited"])
	}
	runOKPaid(t, "pass", "vaults", "share", "remove", vault, secondaryEmail())
	left := runJSONPaid(t, "pass", "vaults", "share", "get", vault)
	if strings.Contains(fmt.Sprintf("%v", left["invited"]), secondaryEmail()) {
		t.Errorf("the offer is still there after being withdrawn: %v", left["invited"])
	}
}

// An offer can be turned down, which opens nothing.
func TestPassVaultInviteDeclined(t *testing.T) {
	name := testID() + "-declined-vault"
	out, stderr, code := runPaid(t, "--yes", "pass", "vaults", "create", "--name", name)
	if code != 0 {
		t.Fatalf("could not make a vault: %s", truncateOutput(stderr))
	}
	vault := strings.TrimSpace(out)
	cleanupRunPaid(t, "Delete vault: proton pass vaults delete "+vault,
		"pass", "vaults", "delete", vault)

	runOKPaid(t, "pass", "vaults", "share", "add", vault, secondaryEmail())

	var token string
	waitFor(60*time.Second, 3*time.Second, func() bool {
		for _, row := range runJSONArraySecondary(t, "pass", "invitations", "list") {
			m, _ := row.(map[string]interface{})
			if v, _ := m["vault"].(string); v != name {
				continue
			}
			token, _ = m["id"].(string)
			return token != ""
		}
		return false
	})
	if token == "" {
		t.Fatal("the second account never saw the offer")
	}
	runOKSecondary(t, "pass", "invitations", "decline", "--", token)

	for _, row := range runJSONArraySecondary(t, "pass", "vaults", "list") {
		m, _ := row.(map[string]interface{})
		if n, _ := m["name"].(string); n == name {
			t.Error("a declined vault turned up on the account anyway")
		}
	}
}

// memberEmail is the address Proton reports for the owner of a share, or for
// somebody who is not.
//
// It is read out of the listing rather than assumed, because an account is a
// member under its primary Proton address whatever address it signs in as.
func memberEmail(t *testing.T, members any, owner bool) string {
	t.Helper()
	rows, _ := members.([]any)
	for _, row := range rows {
		m, _ := row.(map[string]any)
		if is, _ := m["owner"].(bool); is == owner {
			email, _ := m["email"].(string)
			return email
		}
	}
	return ""
}
