package tests

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// ── pass vaults list ──

func TestPassVaultsList(t *testing.T) {
	skipIfNoCredentials(t)
	stdout := runOK(t, "pass", "vaults", "list")
	assertContains(t, stdout, "SHARE_ID")
	assertContains(t, stdout, "NAME")
	assertContains(t, stdout, "OWNER")
}

func TestPassVaultsListJSON(t *testing.T) {
	skipIfNoCredentials(t)
	arr := runJSONArray(t, "pass", "vaults", "list")
	if len(arr) == 0 {
		t.Skip("no vaults found")
	}
	v := arr[0].(map[string]interface{})
	if v["ShareID"] == nil || v["ShareID"] == "" {
		t.Error("vault missing ShareID")
	}
	if v["Vault"] == nil {
		t.Error("vault missing decrypted Vault content")
	}
}

// ── pass list ──

func TestPassList(t *testing.T) {
	skipIfNoCredentials(t)
	stdout := runOK(t, "pass", "list")
	assertContains(t, stdout, "VAULT")
	assertContains(t, stdout, "TYPE")
	assertContains(t, stdout, "NAME")
	assertContains(t, stdout, "USERNAME")
}

func TestPassListJSON(t *testing.T) {
	skipIfNoCredentials(t)
	arr := runJSONArray(t, "pass", "list")
	if len(arr) == 0 {
		t.Skip("no items found")
	}
	item := arr[0].(map[string]interface{})
	if item["ShareID"] == nil || item["ShareID"] == "" {
		t.Error("item missing ShareID")
	}
	if item["ItemID"] == nil || item["ItemID"] == "" {
		t.Error("item missing ItemID")
	}
	if item["Type"] == nil || item["Type"] == "" {
		t.Error("item missing Type")
	}
}

// ── helpers ──

func getFirstVaultShareID(t *testing.T) string {
	t.Helper()
	arr := runJSONArray(t, "pass", "vaults", "list")
	if len(arr) == 0 {
		t.Fatal("no vaults found")
	}
	v := arr[0].(map[string]interface{})
	return v["ShareID"].(string)
}

func createPassItem(t *testing.T, name string) (string, string) {
	t.Helper()
	shareID := getFirstVaultShareID(t)

	stdout := runOK(t, "pass", "create",
		"--type", "login",
		"--name", name,
		"--username", "testuser",
		"--password", "testpass123",
		"--url", "https://example.com")

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("failed to parse create output: %v", err)
	}
	itemMap, ok := result["Item"].(map[string]interface{})
	if !ok {
		t.Fatal("no Item in create response")
	}
	itemID, ok := itemMap["ItemID"].(string)
	if !ok || itemID == "" {
		t.Fatal("no ItemID in create response")
	}

	cleanupRun(t, fmt.Sprintf("Delete pass item: proton-cli pass delete %s %s", shareID, itemID),
		"pass", "delete", shareID, itemID)

	return shareID, itemID
}

// ── pass create + get + delete ──

func TestPassCreateGetDelete(t *testing.T) {
	skipIfNoCredentials(t)
	name := testID() + "-login"
	shareID, itemID := createPassItem(t, name)

	// Get by IDs
	getOut := runOK(t, "pass", "get", shareID, itemID)
	assertField(t, getOut, "Name:", name)
	assertField(t, getOut, "Username:", "testuser")
	assertField(t, getOut, "Password:", "testpass123")
	assertField(t, getOut, "URL:", "https://example.com")
	assertField(t, getOut, "Type:", "login")
	assertField(t, getOut, "ID:", itemID)
	assertField(t, getOut, "Share:", shareID)

	// Get by name search
	getOut2 := runOK(t, "pass", "get", name)
	assertField(t, getOut2, "Name:", name)
	assertField(t, getOut2, "Username:", "testuser")

	// JSON mode
	jsonOut := runOK(t, "pass", "get", shareID, itemID, "--json")
	var jsonResult map[string]interface{}
	if err := json.Unmarshal([]byte(jsonOut), &jsonResult); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if jsonResult["ShareID"] != shareID {
		t.Errorf("JSON ShareID: got %v, want %s", jsonResult["ShareID"], shareID)
	}
	if jsonResult["ItemID"] != itemID {
		t.Errorf("JSON ItemID: got %v, want %s", jsonResult["ItemID"], itemID)
	}
}

func TestPassCreateNote(t *testing.T) {
	skipIfNoCredentials(t)
	name := testID() + "-note"
	shareID := getFirstVaultShareID(t)

	stdout := runOK(t, "pass", "create",
		"--type", "note",
		"--name", name,
		"--note", "This is a test note")

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("failed to parse create output: %v", err)
	}
	itemID := result["Item"].(map[string]interface{})["ItemID"].(string)

	cleanupRun(t, fmt.Sprintf("Delete pass item: proton-cli pass delete %s %s", shareID, itemID),
		"pass", "delete", shareID, itemID)

	getOut := runOK(t, "pass", "get", shareID, itemID)
	assertField(t, getOut, "Name:", name)
	assertField(t, getOut, "Note:", "This is a test note")
	assertField(t, getOut, "Type:", "note")
}

// ── pass edit ──

func TestPassEdit(t *testing.T) {
	skipIfNoCredentials(t)
	name := testID() + "-edit"
	shareID, itemID := createPassItem(t, name)

	newName := name + "-v2"
	runOK(t, "pass", "edit", shareID, itemID,
		"--username", "newuser",
		"--password", "newpass456",
		"--name", newName)

	getOut := runOK(t, "pass", "get", shareID, itemID)
	assertField(t, getOut, "Name:", newName)
	assertField(t, getOut, "Username:", "newuser")
	assertField(t, getOut, "Password:", "newpass456")
	assertField(t, getOut, "URL:", "https://example.com") // preserved
	assertField(t, getOut, "Type:", "login")

	// Old values gone
	assertNotContains(t, getOut, "testuser")
	assertNotContains(t, getOut, "testpass123")
}

// ── pass trash + restore ──

func TestPassTrashRestore(t *testing.T) {
	skipIfNoCredentials(t)
	name := testID() + "-trash"
	shareID, itemID := createPassItem(t, name)

	// Trash
	runOK(t, "pass", "trash", shareID, itemID)

	// Not in active list
	listOut := runOK(t, "pass", "list")
	assertNotContains(t, listOut, name)

	// Restore
	runOK(t, "pass", "restore", shareID, itemID)

	// Back in list
	listOut2 := runOK(t, "pass", "list")
	assertContains(t, listOut2, name)

	// Content intact
	getOut := runOK(t, "pass", "get", shareID, itemID)
	assertField(t, getOut, "Name:", name)
	assertField(t, getOut, "Username:", "testuser")
	assertField(t, getOut, "Password:", "testpass123")
}

// ── pass alias ──

func TestPassAliasOptions(t *testing.T) {
	skipIfNoCredentials(t)
	stdout := runOK(t, "pass", "alias", "options")
	assertContains(t, stdout, "Suffixes:")
	assertContains(t, stdout, "Mailboxes:")

	jsonOut := runOK(t, "pass", "alias", "options", "--json")
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(jsonOut), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	suffixes, ok := result["Suffixes"].([]interface{})
	if !ok || len(suffixes) == 0 {
		t.Error("expected at least one suffix")
	}
	mailboxes, ok := result["Mailboxes"].([]interface{})
	if !ok || len(mailboxes) == 0 {
		t.Error("expected at least one mailbox")
	}
}

func TestPassAliasCreate(t *testing.T) {
	skipIfNoCredentials(t)
	prefix := testID() + "-alias"

	stdout := runOK(t, "pass", "alias", "create", "--prefix", prefix, "--name", prefix)

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("failed to parse create output: %v", err)
	}
	itemMap := result["Item"].(map[string]interface{})
	itemID := itemMap["ItemID"].(string)
	aliasEmail, _ := itemMap["AliasEmail"].(string)
	shareID := getFirstVaultShareID(t)

	cleanupRun(t, fmt.Sprintf("Delete alias: proton-cli pass delete %s %s", shareID, itemID),
		"pass", "delete", shareID, itemID)

	if !strings.Contains(aliasEmail, prefix) {
		t.Errorf("alias email %q should contain prefix %q", aliasEmail, prefix)
	}

	listOut := runOK(t, "pass", "list")
	assertContains(t, listOut, prefix)
	assertContains(t, listOut, "alias")
}

func TestPassCreateLoginWithAlias(t *testing.T) {
	skipIfNoCredentials(t)
	name := testID() + "-loginalias"
	prefix := testID() + "-la"

	stdout := runOK(t, "pass", "create",
		"--type", "login",
		"--name", name,
		"--username", "testuser",
		"--password", "testpass",
		"--url", "https://example.com",
		"--alias-prefix", prefix)

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("failed to parse create output: %v", err)
	}

	shareID := getFirstVaultShareID(t)

	bundle, ok := result["Bundle"].(map[string]interface{})
	if !ok {
		t.Fatal("expected Bundle in response")
	}

	loginItem, ok := bundle["Item"].(map[string]interface{})
	if !ok {
		t.Fatal("expected Item in Bundle")
	}
	loginID := loginItem["ItemID"].(string)
	cleanupRun(t, fmt.Sprintf("Delete login: proton-cli pass delete %s %s", shareID, loginID),
		"pass", "delete", shareID, loginID)

	aliasItem, ok := bundle["Alias"].(map[string]interface{})
	if !ok {
		t.Fatal("expected Alias in Bundle")
	}
	aliasID := aliasItem["ItemID"].(string)
	aliasEmail, _ := aliasItem["AliasEmail"].(string)
	cleanupRun(t, fmt.Sprintf("Delete alias: proton-cli pass delete %s %s", shareID, aliasID),
		"pass", "delete", shareID, aliasID)

	if !strings.Contains(aliasEmail, prefix) {
		t.Errorf("alias email %q should contain prefix %q", aliasEmail, prefix)
	}

	// Verify login has all fields including alias email
	getOut := runOK(t, "pass", "get", shareID, loginID)
	assertField(t, getOut, "Name:", name)
	assertField(t, getOut, "Username:", "testuser")
	assertField(t, getOut, "Password:", "testpass")
	assertField(t, getOut, "URL:", "https://example.com")
	assertContains(t, getOut, "Email:    "+prefix) // alias email starts with prefix

	listOut := runOK(t, "pass", "list")
	assertContains(t, listOut, name)
}

// ── pass vaults create + delete ──

func TestPassVaultsCreateDelete(t *testing.T) {
	skipIfNoCredentials(t)
	name := testID() + "-vault"

	stdout := runOK(t, "pass", "vaults", "create", "--name", name)

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("failed to parse create output: %v", err)
	}
	shareMap, ok := result["Share"].(map[string]interface{})
	if !ok {
		t.Fatal("no Share in create response")
	}
	shareID, ok := shareMap["ShareID"].(string)
	if !ok || shareID == "" {
		t.Fatal("no ShareID in create response")
	}

	cleanupRun(t, fmt.Sprintf("Delete vault: proton-cli pass vaults delete %s", shareID),
		"pass", "vaults", "delete", shareID)

	listOut := runOK(t, "pass", "vaults", "list")
	assertContains(t, listOut, name)
	assertContains(t, listOut, shareID)
}
