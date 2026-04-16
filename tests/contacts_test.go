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
	assertContains(t, stdout, "PHONE")
}

func TestContactsListJSON(t *testing.T) {
	skipIfNoCredentials(t)
	arr := runJSONArray(t, "contacts", "list")
	if len(arr) == 0 {
		t.Skip("no contacts found")
	}
	c := arr[0].(map[string]interface{})
	if c["ID"] == nil || c["ID"] == "" {
		t.Error("contact missing ID")
	}
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

	// Get by ID — verify exact fields
	getOut := runOK(t, "contacts", "get", contactID)
	assertField(t, getOut, "ID:", contactID)
	assertField(t, getOut, "Name:", name)
	assertField(t, getOut, "Email:", email)

	// Get by name — same email
	getOut2 := runOK(t, "contacts", "get", name)
	assertField(t, getOut2, "Email:", email)
	assertField(t, getOut2, "Name:", name)

	// Get by email — same name
	getOut3 := runOK(t, "contacts", "get", email)
	assertField(t, getOut3, "Name:", name)
	assertField(t, getOut3, "Email:", email)
}

func TestContactsGetByName(t *testing.T) {
	skipIfNoCredentials(t)
	name := testID() + "-getname"
	email := testID() + "@example.com"

	createTestContact(t, name, email)

	out := runOK(t, "contacts", "get", name)
	assertField(t, out, "Name:", name)
	assertField(t, out, "Email:", email)
}

func TestContactsUpdate(t *testing.T) {
	skipIfNoCredentials(t)
	name := testID() + "-update"
	email := testID() + "@example.com"

	contactID := createTestContact(t, name, email)

	// Update name
	newName := name + "-v2"
	runOK(t, "contacts", "update", contactID, "--name", newName)

	// Verify new name, email preserved
	out := runOK(t, "contacts", "get", contactID)
	assertField(t, out, "Name:", newName)
	assertField(t, out, "Email:", email)
	assertNotContains(t, out, name+"\n") // old name gone (but newName contains old name as prefix, so match on line boundary)
}

func TestContactsUpdatePhone(t *testing.T) {
	skipIfNoCredentials(t)
	name := testID() + "-phone"
	email := testID() + "@example.com"

	contactID := createTestContact(t, name, email)

	// Add phone
	runOK(t, "contacts", "update", contactID, "--phone", "+43999888777")

	// Verify phone added, other fields intact
	out := runOK(t, "contacts", "get", contactID)
	assertField(t, out, "Name:", name)
	assertField(t, out, "Email:", email)
	assertField(t, out, "Phone:", "+43999888777")
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
