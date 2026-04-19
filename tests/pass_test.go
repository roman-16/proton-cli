package tests

import (
	"fmt"
	"strings"
	"testing"
)

// ── vaults ──

func TestPassVaultsList(t *testing.T) {
	skipIfNoCredentials(t)
	stdout := runOK(t, "pass", "vaults", "list")
	assertContains(t, stdout, "SHARE_ID")
}

func TestPassVaultsCRUD(t *testing.T) {
	skipIfNoCredentials(t)
	name := testID() + "-vault"
	stdout := runOK(t, "pass", "vaults", "create", "--name", name)
	shareID := strings.TrimSpace(stdout)
	if !looksLikeID(shareID) {
		t.Fatalf("expected bare share ID on stdout, got %q", stdout)
	}
	cleanupRun(t, fmt.Sprintf("Delete vault: proton-cli pass vaults delete -- %s", shareID),
		"pass", "vaults", "delete", "--", shareID)

	list := runOK(t, "pass", "vaults", "list")
	assertContains(t, list, name)
}

// ── items: login ──

func TestPassItemsCRUDLogin(t *testing.T) {
	skipIfNoCredentials(t)
	name := testID() + "-login"
	url := "https://" + name + ".example.invalid/"

	stdout := runOK(t, "pass", "items", "create",
		"--type", "login",
		"--name", name,
		"--username", "tester",
		"--password", "s3cret!",
		"--url", url)
	itemID := strings.TrimSpace(stdout)
	if !looksLikeID(itemID) {
		t.Fatalf("expected bare item ID on stdout, got %q", stdout)
	}
	cleanupRun(t, fmt.Sprintf("Delete item: proton-cli pass items delete %s", name),
		"pass", "items", "delete", name)

	// Get by URL REF
	got := runOK(t, "pass", "items", "get", name+".example.invalid")
	assertField(t, got, "Name:", name)
	assertField(t, got, "Username:", "tester")
	assertField(t, got, "Password:", "s3cret!")

	// Edit password
	runOK(t, "pass", "items", "edit", name, "--password", "new-pass-v2")
	got2 := runOK(t, "pass", "items", "get", name)
	assertField(t, got2, "Password:", "new-pass-v2")
}

// ── items: note ──

func TestPassItemsCreateNote(t *testing.T) {
	skipIfNoCredentials(t)
	name := testID() + "-note"
	stdout := runOK(t, "pass", "items", "create",
		"--type", "note",
		"--name", name,
		"--note", "secret note content")
	id := strings.TrimSpace(stdout)
	if !looksLikeID(id) {
		t.Fatalf("expected bare ID on stdout, got %q", stdout)
	}
	cleanupRun(t, fmt.Sprintf("Delete note: proton-cli pass items delete %s", name),
		"pass", "items", "delete", name)

	got := runOK(t, "pass", "items", "get", name)
	assertField(t, got, "Type:", "note")
	assertField(t, got, "Note:", "secret note content")
}

// ── items: card (checks PIN rendering) ──

func TestPassItemsCreateCardShowsPIN(t *testing.T) {
	skipIfNoCredentials(t)
	name := testID() + "-card"
	stdout := runOK(t, "pass", "items", "create",
		"--type", "card",
		"--name", name,
		"--holder", "Test Holder",
		"--number", "4111111111111111",
		"--expiry", "2029-01",
		"--cvv", "123",
		"--pin", "7890")
	id := strings.TrimSpace(stdout)
	if !looksLikeID(id) {
		t.Fatalf("expected bare ID on stdout, got %q", stdout)
	}
	cleanupRun(t, fmt.Sprintf("Delete card: proton-cli pass items delete %s", name),
		"pass", "items", "delete", name)

	got := runOK(t, "pass", "items", "get", name)
	assertField(t, got, "Holder:", "Test Holder")
	assertField(t, got, "Number:", "4111111111111111")
	assertField(t, got, "Expiry:", "2029-01")
	assertField(t, got, "CVV:", "123")
	assertField(t, got, "PIN:", "7890")
}

// ── items: trash / restore / delete ──

func TestPassItemsTrashRestoreDelete(t *testing.T) {
	skipIfNoCredentials(t)
	name := testID() + "-trash"
	stdout := runOK(t, "pass", "items", "create",
		"--type", "login", "--name", name,
		"--username", "u", "--password", "p")
	itemID := strings.TrimSpace(stdout)

	// Need share ID for restore (trashed items don't appear in search)
	vaults := runJSONArray(t, "pass", "vaults", "list")
	shareID := vaults[0].(map[string]interface{})["share_id"].(string)

	// Register a best-effort cleanup (permanent delete by IDs)
	cleanupRun(t, fmt.Sprintf("Delete item: proton-cli pass items delete -- %s %s", shareID, itemID),
		"pass", "items", "delete", "--", shareID, itemID)

	runOK(t, "pass", "items", "trash", name)
	runOK(t, "pass", "items", "restore", "--", shareID, itemID)

	// It should be searchable again
	got := runOK(t, "pass", "items", "get", name)
	assertField(t, got, "Name:", name)
}

// ── items list with vault filter ──

func TestPassItemsListVaultFilter(t *testing.T) {
	skipIfNoCredentials(t)
	vaults := runJSONArray(t, "pass", "vaults", "list")
	if len(vaults) == 0 {
		t.Skip("no vaults")
	}
	firstName := vaults[0].(map[string]interface{})["name"].(string)
	runOK(t, "pass", "items", "list", "--vault", firstName)
}

// ── alias options (read-only) ──

func TestPassAliasOptions(t *testing.T) {
	skipIfNoCredentials(t)
	stdout := runOK(t, "pass", "alias", "options")
	assertContains(t, stdout, "Suffixes")
	assertContains(t, stdout, "Mailboxes")
}

// ── batch filters (all dry-run) ──

func TestPassBatchTrashDryRunByType(t *testing.T) {
	skipIfNoCredentials(t)
	_, stderr, code := run(t, "--dry-run", "pass", "items", "trash", "--type", "note")
	if code != 0 {
		t.Fatalf("dry-run should succeed, got exit %d: %s", code, stderr)
	}
	assertContains(t, stderr, "dry-run")
}

func TestPassBatchTrashDryRunOlderThanYear(t *testing.T) {
	skipIfNoCredentials(t)
	_, stderr, code := run(t, "--dry-run", "pass", "items", "trash",
		"--older-than", "1y", "--type", "login")
	if code != 0 {
		t.Fatalf("dry-run should succeed, got exit %d: %s", code, stderr)
	}
	// Either a "would trash" line or nothing to trash; at minimum doesn't crash
	_ = stderr
}

func TestPassBatchTrashDurationUnitMonths(t *testing.T) {
	skipIfNoCredentials(t)
	// "6mo" must parse without error.
	_, _, code := run(t, "--dry-run", "pass", "items", "trash",
		"--older-than", "6mo", "--type", "login")
	if code != 0 {
		t.Errorf("--older-than 6mo should parse, got exit %d", code)
	}
}

func TestPassBatchTrashRequiresInput(t *testing.T) {
	skipIfNoCredentials(t)
	_, stderr, code := run(t, "pass", "items", "trash")
	if code == 0 {
		t.Error("expected error when no REF and no filter given")
	}
	assertContains(t, stderr, "no items selected")
}
