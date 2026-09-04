package live

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// Addresses: the identity mail goes out under, and the signature that goes with
// it.

// primaryAddressID returns the account's first address ID.
func primaryAddressID(t *testing.T) string {
	t.Helper()
	raw := runOK(t, "--full-ids", "mail", "settings", "addresses", "list", "--output", "json")
	var env struct {
		Addresses []struct {
			ID    string `json:"id"`
			Email string `json:"email"`
		} `json:"addresses"`
	}
	if err := json.Unmarshal([]byte(raw), &env); err != nil || len(env.Addresses) == 0 {
		t.Fatalf("could not read the address list: %v\n%s", err, truncateOutput(raw))
	}
	for _, a := range env.Addresses {
		if a.Email == selfEmail() {
			return a.ID
		}
	}
	return env.Addresses[0].ID
}

func addressSignature(t *testing.T, addrID string) string {
	t.Helper()
	raw := runOK(t, "mail", "settings", "addresses", "get", "--output", "json", "--", addrID)
	var a struct {
		Signature string `json:"signature"`
	}
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		t.Fatalf("could not read the address: %v", err)
	}
	return a.Signature
}

func TestMailSettingsAddressesList(t *testing.T) {
	stdout := runOK(t, "mail", "settings", "addresses", "list")
	assertContains(t, stdout, "EMAIL")
	assertContains(t, stdout, selfEmail())
}

func TestMailSettingsAddressesGetByEmail(t *testing.T) {
	stdout := runOK(t, "mail", "settings", "addresses", "get", selfEmail())
	assertField(t, stdout, "Email:", selfEmail())
	assertContains(t, stdout, "Can Send:")
}

func TestMailSettingsAddressesUpdateSignature(t *testing.T) {
	addrID := primaryAddressID(t)
	original := addressSignature(t, addrID)
	restoreSignature(t, addrID, original)

	marker := testID() + "-signature"
	runOK(t, "mail", "settings", "addresses", "update", "--signature", marker, "--", addrID)
	assertContains(t, runOK(t, "mail", "settings", "addresses", "get", "--", addrID), marker)

	// Plain text is stored as HTML, so newlines survive as line breaks.
	runOK(t, "mail", "settings", "addresses", "update", "--signature", "line one\nline two", "--", addrID)
	if got := addressSignature(t, addrID); got != "line one<br>line two" {
		t.Errorf("stored signature = %q, want the newline turned into a line break", got)
	}

	runOK(t, "mail", "settings", "addresses", "update", "--clear-signature", "--", addrID)
	assertContains(t, runOK(t, "mail", "settings", "addresses", "get", "--", addrID), "(none)")
}

func restoreSignature(t *testing.T, addrID, original string) {
	t.Helper()
	cleanup(t, "Restore the address signature", func() error {
		args := []string{"mail", "settings", "addresses", "update", "--html", "--signature", original, "--", addrID}
		if strings.TrimSpace(original) == "" {
			args = []string{"mail", "settings", "addresses", "update", "--clear-signature", "--", addrID}
		}
		if _, stderr, code := run(t, args...); code != 0 {
			return fmt.Errorf("exit %d: %s", code, strings.TrimSpace(stderr))
		}
		return nil
	})
}
