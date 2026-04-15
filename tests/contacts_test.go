package tests

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestContactsList(t *testing.T) {
	skipIfNoCredentials(t)
	stdout := runOK(t, "contacts", "list")
	assertContains(t, stdout, "ID")
	assertContains(t, stdout, "NAME")
	assertContains(t, stdout, "EMAIL")
}

func TestContactsListJSON(t *testing.T) {
	skipIfNoCredentials(t)
	arr := runJSONArray(t, "contacts", "list")
	_ = arr
}

// createTestContact creates a contact and registers cleanup. Returns contact ID.
func createTestContact(t *testing.T, name, email string) string {
	t.Helper()
	stdout := runOK(t, "contacts", "create", "--name", name, "--email", email)

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("failed to parse create output: %v", err)
	}
	responses := result["Responses"].([]interface{})
	resp := responses[0].(map[string]interface{})
	contactID := jsonField(resp["Response"].(map[string]interface{}), "Contact", "ID")
	if contactID == "" {
		t.Fatal("no Contact.ID in create response")
	}

	cleanupRun(t, fmt.Sprintf("Delete contact: proton-cli contacts delete %s", contactID),
		"contacts", "delete", contactID)

	return contactID
}

func TestContactsCreateGetDelete(t *testing.T) {
	skipIfNoCredentials(t)
	name := testID() + "-contact"
	email := testID() + "@example.com"

	contactID := createTestContact(t, name, email)

	// Get by ID
	getOut := runOK(t, "contacts", "get", contactID)
	assertContains(t, getOut, name)
	assertContains(t, getOut, email)

	// Get by name
	getOut2 := runOK(t, "contacts", "get", name)
	assertContains(t, getOut2, email)

	// Get by email
	getOut3 := runOK(t, "contacts", "get", email)
	assertContains(t, getOut3, name)
}

func TestContactsGetByName(t *testing.T) {
	skipIfNoCredentials(t)
	name := testID() + "-getname"
	email := testID() + "@example.com"

	contactID := createTestContact(t, name, email)
	_ = contactID

	out := runOK(t, "contacts", "get", name)
	assertContains(t, out, "Name:")
	assertContains(t, out, name)
}

func TestContactsUpdate(t *testing.T) {
	skipIfNoCredentials(t)
	name := testID() + "-update"
	email := testID() + "@example.com"

	contactID := createTestContact(t, name, email)

	// Update name
	newName := name + "-v2"
	runOK(t, "contacts", "update", contactID, "--name", newName)

	// Verify
	out := runOK(t, "contacts", "get", contactID)
	assertContains(t, out, newName)
}

func TestContactsUpdatePhone(t *testing.T) {
	skipIfNoCredentials(t)
	name := testID() + "-phone"
	email := testID() + "@example.com"

	contactID := createTestContact(t, name, email)

	// Add phone
	runOK(t, "contacts", "update", contactID, "--phone", "+43999888777")

	// Verify
	out := runOK(t, "contacts", "get", contactID, "--json")
	assertContains(t, out, "+43999888777")
}

func TestContactsDeleteByName(t *testing.T) {
	skipIfNoCredentials(t)
	name := testID() + "-delname"
	email := testID() + "@example.com"

	// Create (no cleanup — we're testing delete)
	runOK(t, "contacts", "create", "--name", name, "--email", email)

	// Delete by name
	runOK(t, "contacts", "delete", name)

	// Verify gone
	_, _, code := run(t, "contacts", "get", name)
	if code == 0 {
		t.Error("expected get to fail after delete")
	}
}
