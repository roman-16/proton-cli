package live

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// Email settings: how mail to each of a contact's addresses is sent, stored
// where Proton's own apps read it, and followed by what this CLI sends.

// emailRow returns the `contacts emails list` row for one address.
func emailRow(t *testing.T, ref, email string) map[string]interface{} {
	t.Helper()
	for _, row := range runJSONArray(t, "contacts", "emails", "list", "--", ref) {
		m, _ := row.(map[string]interface{})
		if m["email"] == email {
			return m
		}
	}
	t.Fatalf("contacts emails list %s has no row for %s", ref, email)
	return nil
}

// The settings land in the signed card, and survive an edit of something else
// and a key being trusted and untrusted.
func TestContactsEmailsUpdate(t *testing.T) {
	email := "settings-" + testID() + "@example.invalid"
	id := strings.TrimSpace(runOK(t, "contacts", "create", "--name", testID()+"-settings", "--email", email))
	cleanupRun(t, fmt.Sprintf("Delete contact: proton contacts delete %s", id),
		"contacts", "delete", "--", id)

	runOK(t, "contacts", "emails", "update", "--sign", "on", "--scheme", "pgp-inline", "--", email)

	row := emailRow(t, id, email)
	for field, want := range map[string]string{"sign": "on", "scheme": "pgp-inline", "format": "plain-text"} {
		if row[field] != want {
			t.Errorf("%s = %v, want %s", field, row[field], want)
		}
	}
	card := signedCardData(t, id)
	for _, want := range []string{"X-PM-SIGN:true", "X-PM-SCHEME:pgp-inline", "X-PM-MIMETYPE:text/plain"} {
		if !strings.Contains(card, want) {
			t.Errorf("the signed card lacks %s:\n%s", want, card)
		}
	}

	runOK(t, "contacts", "update", "--job-title", "Boss", "--", id)
	runOK(t, "contacts", "keys", "trust", "--key", writeGeneratedPubKey(t), email)
	runOK(t, "contacts", "keys", "untrust", email)

	card = signedCardData(t, id)
	for _, want := range []string{"X-PM-SIGN:true", "X-PM-SCHEME:pgp-inline", "X-PM-MIMETYPE:text/plain"} {
		if !strings.Contains(card, want) {
			t.Errorf("an edit, a trust and an untrust dropped %s:\n%s", want, card)
		}
	}
}

// A Proton address is always encrypted and signed, so only its format is
// anybody's to choose.
func TestContactsEmailsRefuseToSignAProtonAddress(t *testing.T) {
	id := strings.TrimSpace(runOK(t, "contacts", "create", "--name", testID()+"-proton", "--email", secondaryEmail()))
	cleanupRun(t, fmt.Sprintf("Delete contact: proton contacts delete %s", id),
		"contacts", "delete", "--", id)

	_, stderr, code := run(t, "contacts", "emails", "update", "--sign", "off", "--", id)
	if code != 1 || !strings.Contains(stderr, "is a Proton address") {
		t.Errorf("exit %d, stderr %q; want the refusal for a Proton address", code, stderr)
	}
}

// An address set to plain text is sent plain text, even when the message was
// written in HTML.
func TestContactsEmailsPlainTextReachesTheRecipient(t *testing.T) {
	id := strings.TrimSpace(runOK(t, "contacts", "create", "--name", testID()+"-plain", "--email", secondaryEmail()))
	cleanupRun(t, fmt.Sprintf("Delete contact: proton contacts delete %s", id),
		"contacts", "delete", "--", id)
	runOK(t, "contacts", "emails", "update", "--email-format", "plain-text", "--", id)

	subject := testID() + "-plain-send"
	needle := "plain body for " + subject
	runOK(t, "mail", "messages", "send", "--to", secondaryEmail(), "--subject", subject,
		"--body", "<p><b>"+needle+"</b></p>", "--html")
	if sentID := findMessage(t, "sent", subject); sentID != "" {
		cleanupRun(t, "Delete sent mail: proton mail messages delete "+sentID,
			"mail", "messages", "delete", sentID)
	}

	var recvID string
	waitFor(45*time.Second, 3*time.Second, func() bool {
		recvID = secondaryMailContaining(t, selfEmail(), needle)
		return recvID != ""
	})
	if recvID == "" {
		t.Fatal("the second account did not receive the plain-text mail")
	}
	cleanupRunSecondary(t, "Delete received mail (secondary): proton --profile secondary mail messages delete "+recvID,
		"mail", "messages", "delete", recvID)

	got := runJSONSecondary(t, "mail", "messages", "get", "--", recvID)
	if got["mime_type"] != "text/plain" {
		t.Errorf("the message arrived as %v, want text/plain", got["mime_type"])
	}
	if body, _ := got["body"].(string); strings.Contains(body, "<b>") {
		t.Errorf("the body kept its markup: %q", body)
	}
}

// Mail to an address outside Proton that asks to be signed goes out as a
// PGP/MIME-signed message, which Proton has to accept.
func TestContactsEmailsSignedMailGoesOutside(t *testing.T) {
	outsider := externalRecipient(t)
	id := strings.TrimSpace(runOK(t, "contacts", "create", "--name", testID()+"-signed", "--email", outsider))
	cleanupRun(t, fmt.Sprintf("Delete contact: proton contacts delete %s", id),
		"contacts", "delete", "--", id)
	runOK(t, "contacts", "emails", "update", "--sign", "on", "--scheme", "pgp-mime", "--", id)

	subject := testID() + "-signed-send"
	runOK(t, "mail", "messages", "send", "--to", outsider, "--subject", subject, "--body", "signed body for "+subject)
	sentID := findMessage(t, "sent", subject)
	if sentID == "" {
		t.Fatal("the signed message is not in Sent")
	}
	cleanupRun(t, "Delete sent mail: proton mail messages delete "+sentID,
		"mail", "messages", "delete", sentID)
}
