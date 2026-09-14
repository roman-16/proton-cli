package live

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// Passkeys on a login.
//
// One is minted by a site and a browser between them, so no command makes one
// and the suite cannot ask Proton for one either. What it can do is put a login
// carrying one into the account the way a person moving from another device
// does - through the document Proton Pass itself writes - and then read, remove
// and restore it.

// passkeyDocument is a Proton Pass export holding a single login with one
// passkey on it, written the way the app writes one.
func passkeyDocument(t *testing.T, name string) string {
	t.Helper()
	content := map[string]any{
		"itemEmail":    "",
		"itemUsername": "tester",
		"password":     "",
		"urls":         []string{"https://example.com/"},
		"totpUri":      "",
		"passkeys": []map[string]any{{
			"keyId":           "7f3a1c9dpasskeytestkey",
			"content":         "cGFzc2tleS1jb250ZW50",
			"domain":          "example.com",
			"rpId":            "example.com",
			"rpName":          "Example",
			"userName":        "tester",
			"userDisplayName": "Tester",
			"userId":          "dXNlci1pZA==",
			"createTime":      1762074840,
			"note":            "",
			"credentialId":    "Y3JlZGVudGlhbC1pZA==",
			"userHandle":      "dXNlci1oYW5kbGU=",
			"creationData": map[string]any{
				"osName":     "macOS",
				"osVersion":  "15.1",
				"deviceName": "Apple",
				"appVersion": "web-pass@1.31.0",
			},
		}},
	}
	document := map[string]any{
		"encrypted": false,
		"userId":    "",
		"version":   "1.31.0",
		"vaults": map[string]any{
			"exported": map[string]any{
				"name":        name,
				"description": "",
				"display":     map[string]any{"color": 0, "icon": 0},
				"items": []map[string]any{{
					"itemId":               "",
					"shareId":              "",
					"state":                1,
					"aliasEmail":           nil,
					"contentFormatVersion": 7,
					"createTime":           1762074840,
					"modifyTime":           1762074840,
					"files":                []string{},
					"data": map[string]any{
						"metadata":    map[string]any{"name": name, "note": "", "itemUuid": ""},
						"extraFields": []any{},
						"type":        "login",
						"content":     content,
					},
				}},
			},
		},
	}
	body, err := json.Marshal(document)
	if err != nil {
		t.Fatalf("render the document: %v", err)
	}
	path := filepath.Join(t.TempDir(), "passkeys.json")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatalf("write the document: %v", err)
	}
	return path
}

// A login carrying a passkey reads it back whole, lists it, gives it up, and
// gets it back out of its own history.
func TestPassPasskeysReadRemovedAndRestored(t *testing.T) {
	name := testID() + "-passkey"
	into := importVault(t)
	runOK(t, "pass", "import", passkeyDocument(t, name), "--vault", into)

	row := itemInVault(t, into, name)
	ref := fmt.Sprint(row["share_id"], "/", row["item_id"])

	// The item says what it carries, which is the whole gap this closes: a login
	// signed into by passkey holds no password, so without this it reads empty.
	shown := runOK(t, "pass", "items", "get", "--", ref)
	assertField(t, shown, "Passkey:", "tester (example.com)")

	held := runJSON(t, "pass", "items", "get", "--", ref)
	keys, _ := held["passkeys"].([]interface{})
	if len(keys) != 1 {
		t.Fatalf("the item carries %d passkeys, want 1: %v", len(keys), held["passkeys"])
	}
	key, _ := keys[0].(map[string]interface{})
	for field, want := range map[string]string{
		"domain":       "example.com",
		"rp_name":      "Example",
		"username":     "tester",
		"display_name": "Tester",
		"device":       "macOS 15.1",
		"app_version":  "web-pass@1.31.0",
	} {
		if got, _ := key[field].(string); got != want {
			t.Errorf("%s = %q, want %q", field, got, want)
		}
	}
	// The material that signs is not something a record hands out; the export is
	// what carries it.
	for _, secret := range []string{"content", "credential_id", "user_handle", "user_id"} {
		if _, leaked := key[secret]; leaked {
			t.Errorf("the record carries %s, which no command prints", secret)
		}
	}

	rows := runJSONArray(t, "pass", "items", "passkeys", "list", "--", ref)
	if len(rows) != 1 {
		t.Fatalf("the listing shows %d passkeys, want 1", len(rows))
	}

	// A dry run says what it would take and leaves the login as it was.
	_, stderr := runOKStderr(t, "--dry-run", "pass", "items", "passkeys", "remove", ref, "tester")
	assertContains(t, stderr, `would remove passkey "tester"`)
	if still := runJSONArray(t, "pass", "items", "passkeys", "list", "--", ref); len(still) != 1 {
		t.Fatalf("a dry run took the passkey off: %d left", len(still))
	}

	// Named by the username the listing shows, rather than by its ID.
	_, stderr = runOKStderr(t, "pass", "items", "passkeys", "remove", ref, "tester")
	assertContains(t, stderr, "Removed passkey")
	if gone := runJSONArray(t, "pass", "items", "passkeys", "list", "--", ref); len(gone) != 0 {
		t.Fatalf("the passkey is still on the login: %v", gone)
	}
	assertNotContains(t, runOK(t, "pass", "items", "get", "--", ref), "Passkey:")

	// Taking one off is a version of the item, so the history still holds it.
	revisions := runJSONArray(t, "pass", "items", "revisions", "list", "--", ref)
	if len(revisions) < 2 {
		t.Fatalf("removing a passkey wrote no new version: %d in the history", len(revisions))
	}
	oldest, _ := revisions[len(revisions)-1].(map[string]interface{})
	first, _ := oldest["revision"].(float64)
	runOK(t, "--yes", "pass", "items", "revisions", "restore", ref, strconv.Itoa(int(first)))

	back := runJSONArray(t, "pass", "items", "passkeys", "list", "--", ref)
	if len(back) != 1 {
		t.Fatalf("restoring the earlier version brought back %d passkeys, want 1", len(back))
	}
}

// Only a login carries passkeys, and the refusal says so rather than reporting
// an empty listing for a note.
func TestPassPasskeysRefuseWhatIsNotALogin(t *testing.T) {
	name := testID() + "-not-a-login"
	ref := createItem(t, "--type", "note", "--name", name, "--note", "nothing to sign in to")
	cleanupRun(t, fmt.Sprintf("Delete item: proton pass items delete %s", ref),
		"pass", "items", "delete", "--", ref)

	_, stderr, code := run(t, "pass", "items", "passkeys", "list", "--", ref)
	if code == 0 {
		t.Error("a note was listed for passkeys")
	}
	assertContains(t, stderr, "not a login")
}
