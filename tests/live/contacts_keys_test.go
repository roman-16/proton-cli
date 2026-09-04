package live

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gopenpgp "github.com/ProtonMail/gopenpgp/v2/crypto"
)

// Pinned keys: telling Proton which key belongs to somebody, whatever it would
// otherwise have found.
//
// A pin is looked up by address across the whole address book, so what a pin
// decides is whether a send goes through at all - which is why the sends are
// here with the pinning rather than with the rest of the compose tests.

// writeGeneratedPubKey generates a throwaway key pair and writes its armored
// public key to a temp .asc file, returning the path.
func writeGeneratedPubKey(t *testing.T) string {
	t.Helper()
	key, err := gopenpgp.GenerateKey("pin-test", "pin@example.invalid", "x25519", 0)
	if err != nil {
		t.Fatal(err)
	}
	pub, err := key.GetArmoredPublicKey()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "key.asc")
	if err := os.WriteFile(path, []byte(pub), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// signedCardData returns the cleartext Data of a contact's signed (Type-2)
// card, fetched raw via the API. Pinned KEY properties live here.
func signedCardData(t *testing.T, contactID string) string {
	t.Helper()
	data := runJSON(t, "api", "GET", "/contacts/v4/contacts/"+contactID)
	contact, _ := data["Contact"].(map[string]interface{})
	cards, _ := contact["Cards"].([]interface{})
	for _, c := range cards {
		m, _ := c.(map[string]interface{})
		if tp, _ := m["Type"].(float64); int(tp) == 2 {
			s, _ := m["Data"].(string)
			return s
		}
	}
	return ""
}

func TestContactsPinUnpinKey(t *testing.T) {
	email := "pin-" + testID() + "@example.invalid"
	id := strings.TrimSpace(runOK(t, "contacts", "create", "--name", testID()+"-pin", "--email", email))
	cleanupRun(t, fmt.Sprintf("Delete contact: proton contacts delete %s", id),
		"contacts", "delete", "--", id)

	runOK(t, "contacts", "keys", "pin", "--key", writeGeneratedPubKey(t), "--email", email, id)
	if !strings.Contains(signedCardData(t, id), "KEY;") {
		t.Error("expected a pinned KEY property in the signed card after pin-key")
	}

	runOK(t, "contacts", "keys", "unpin", "--email", email, id)
	if strings.Contains(signedCardData(t, id), "KEY;") {
		t.Error("KEY property should be gone after unpin-key")
	}
}

func TestContactsUpdatePreservesPinnedKey(t *testing.T) {
	email := "pin-" + testID() + "@example.invalid"
	id := strings.TrimSpace(runOK(t, "contacts", "create", "--name", testID()+"-pin", "--email", email))
	cleanupRun(t, fmt.Sprintf("Delete contact: proton contacts delete %s", id),
		"contacts", "delete", "--", id)

	runOK(t, "contacts", "keys", "pin", "--key", writeGeneratedPubKey(t), "--email", email, id)
	if !strings.Contains(signedCardData(t, id), "KEY;") {
		t.Fatal("setup: pinned key missing after pin-key")
	}

	// An unrelated field update must not drop the pinned key.
	runOK(t, "contacts", "update", "--job-title", "Boss", id)
	if !strings.Contains(signedCardData(t, id), "KEY;") {
		t.Error("contacts update dropped the pinned key")
	}
}

// TestContactsMatchingPinStillDelivers pins the second account's real public key on a
// contact and sends to it: a matching pin must not break E2EE delivery, and
// the second account must still decrypt the body with a verified signature.
func TestContactsMatchingPinStillDelivers(t *testing.T) {
	data := runJSON(t, "api", "GET", "/core/v4/keys/all", "--query", "Email="+secondaryEmail(), "--query", "InternalOnly=0")
	addr, _ := data["Address"].(map[string]interface{})
	ks, _ := addr["Keys"].([]interface{})
	if len(ks) == 0 {
		t.Fatal("the second account publishes no address key, so nothing can be encrypted to it")
	}
	pub, _ := ks[0].(map[string]interface{})["PublicKey"].(string)
	keyPath := filepath.Join(t.TempDir(), "secondary.asc")
	if err := os.WriteFile(keyPath, []byte(pub), 0o644); err != nil {
		t.Fatal(err)
	}

	id := strings.TrimSpace(runOK(t, "contacts", "create", "--name", testID()+"-altpin", "--email", secondaryEmail()))
	cleanupRun(t, fmt.Sprintf("Delete contact: proton contacts delete %s", id),
		"contacts", "delete", "--", id)
	runOK(t, "contacts", "keys", "pin", "--key", keyPath, "--email", secondaryEmail(), id)

	subject := testID() + "-pinned-send"
	body := "pinned-key body for " + subject
	runOK(t, "mail", "messages", "send", "--to", secondaryEmail(), "--subject", subject, "--body", body)
	if sentID := findMessage(t, "sent", subject); sentID != "" {
		cleanupRun(t, "Delete sent mail: proton mail messages delete "+sentID,
			"mail", "messages", "delete", sentID)
	}

	var recvID string
	waitFor(45*time.Second, 3*time.Second, func() bool {
		recvID = secondaryMailContaining(t, selfEmail(), body)
		return recvID != ""
	})
	if recvID == "" {
		t.Fatal("the second account did not receive the pinned-key mail")
	}
	cleanupRunSecondary(t, "Delete received mail (secondary): proton --profile secondary mail messages delete "+recvID,
		"mail", "messages", "delete", recvID)

	read := runOKSecondary(t, "mail", "messages", "get", recvID)
	assertContains(t, read, body)
	assertField(t, read, "Signature:", "verified")
}

// TestContactsPinnedMismatchRefusesTheSend pins a wrong key on a contact for a Proton
// recipient: the send must refuse (the recipient's primary key isn't among the
// pinned keys) and must not leak the draft it created. The second assertion is
// a regression guard for the send-abort cleanup, which used the wrong HTTP
// method and silently leaked drafts on any aborted send.
func TestContactsPinnedMismatchRefusesTheSend(t *testing.T) {
	id := strings.TrimSpace(runOK(t, "contacts", "create", "--name", testID()+"-mismatch", "--email", secondaryEmail()))
	cleanupRun(t, fmt.Sprintf("Delete contact: proton contacts delete %s", id),
		"contacts", "delete", "--", id)
	// A freshly generated key is a valid PGP key but not the second account's.
	runOK(t, "contacts", "keys", "pin", "--key", writeGeneratedPubKey(t), "--email", secondaryEmail(), id)

	subject := testID() + "-mismatch"
	_, stderr, code := run(t, "mail", "messages", "send", "--to", secondaryEmail(), "--subject", subject, "--body", "nope")
	if code != 1 {
		t.Errorf("expected exit 1 on a pinned-key mismatch, got %d (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stderr, "do not match") {
		t.Errorf("expected a primary-not-pinned message, got: %s", stderr)
	}

	// The aborted send must not leave its draft behind.
	if leaked := messageIDInFolder("drafts", subject); leaked != "" {
		cleanupRun(t, "Delete leaked draft: proton mail messages delete "+leaked,
			"mail", "messages", "delete", leaked)
		t.Errorf("aborted send leaked a draft into Drafts: %s", leaked)
	}
}
