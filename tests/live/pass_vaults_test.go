package live

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// Vaults.
//
// The free plan allows two and the fixture holds one, so every test that makes
// one takes the same spare slot - and Proton goes on counting a vault for a few
// seconds after it is deleted, which is what createVault waits out.

func TestPassVaultsList(t *testing.T) {
	stdout := runOK(t, "pass", "vaults", "list")
	assertContains(t, stdout, "ID")
}

// createVault makes a vault and does not return until Proton will answer about
// it, waiting out both of the delays around one.
//
// The free plan allows two and the fixture holds one, so every test that makes a
// vault takes the same spare slot. The lease hands it over the moment the delete
// returns, but the quota it is counted against catches up a few seconds later,
// and until it does the answer is that you cannot have another.
//
// The second delay is at the other end: Pass answers a write before every reader
// of it agrees the thing is there, so a name resolved moments after the vault
// was made can be resolved against a listing that has not got it yet. Waiting
// here rather than in each test is what keeps that race out of all of them.
func createVault(t *testing.T, name string) string {
	t.Helper()
	var ref string
	waitFor(30*time.Second, 2*time.Second, func() bool {
		stdout, stderr, code := run(t, "pass", "vaults", "create", "--name", name)
		if code == 0 {
			ref = strings.TrimSpace(stdout)
			return true
		}
		if !strings.Contains(stderr, "cannot access more vaults") {
			t.Fatalf("creating a vault failed (exit %d): %s", code, stderr)
		}
		return false
	})
	if ref == "" {
		t.Fatal("the spare vault slot never came back")
	}
	if !waitFor(30*time.Second, time.Second, func() bool {
		return strings.Contains(runOK(t, "pass", "vaults", "list"), name)
	}) {
		t.Fatalf("pass never listed the vault it had just made: %s", name)
	}
	return ref
}

func TestPassVaultsCRUD(t *testing.T) {
	name := testID() + "-vault"
	shareID := assertBareID(t, createVault(t, name), "vaults create")
	cleanupRun(t, fmt.Sprintf("Delete vault: proton pass vaults delete -- %s", shareID),
		"pass", "vaults", "delete", "--", shareID)

	list := runOK(t, "pass", "vaults", "list")
	assertContains(t, list, name)
}

// A vault is renamed by the name it has, which is how anybody reaches one.
func TestPassVaultRename(t *testing.T) {
	name := testID() + "-vault"
	sid := createVault(t, name)
	cleanupRun(t, fmt.Sprintf("Delete vault: proton pass vaults delete %s", sid),
		"pass", "vaults", "delete", "--", sid)

	newName := name + "-renamed"
	_, stderr := runOKStderr(t, "pass", "vaults", "update", "--name", newName, name)
	assertContains(t, stderr, `Updated vault "`+newName+`"`)
	assertContains(t, runOK(t, "pass", "vaults", "list"), newName)

	_, stderr = runOKStderr(t, "pass", "vaults", "update", "--icon", "star", newName)
	assertContains(t, stderr, `Updated vault "`+newName+`"`)
}

// Hiding a vault takes it out of every listing that does not name it, and out
// of what a name is looked for in; naming the vault, or an item's ID, still
// reaches it.
func TestPassVaultsHideAndUnhide(t *testing.T) {
	name := testID() + "-hidden"
	sid := createVault(t, name)
	cleanupRun(t, fmt.Sprintf("Delete vault: proton pass vaults delete -- %s", sid),
		"pass", "vaults", "delete", "--", sid)
	item := testID() + "-in-hidden"
	ref := createItem(t, "--type", "note", "--name", item, "--vault", sid)

	_, stderr := runOKStderr(t, "pass", "vaults", "hide", "--", sid)
	assertContains(t, stderr, `Hid vault "`+name+`"`)
	cleanupRun(t, fmt.Sprintf("Unhide vault: proton pass vaults unhide -- %s", sid),
		"pass", "vaults", "unhide", "--", sid)

	if !waitFor(30*time.Second, time.Second, func() bool { return vaultHidden(t, sid) }) {
		t.Fatalf("vaults list never showed %s as hidden", name)
	}
	assertField(t, runOK(t, "pass", "vaults", "get", "--", sid), "Hidden:", "yes")
	if itemListed(t, item, "pass", "items", "list", "--limit", "0") {
		t.Error("items list shows an item from a hidden vault")
	}
	if !itemListed(t, item, "pass", "items", "list", "--vault", sid) {
		t.Error("naming the hidden vault did not list what is in it")
	}
	_, stderr, code := run(t, "pass", "items", "get", item)
	if code != 3 {
		t.Errorf("a name found an item in a hidden vault (exit %d): %s", code, truncateOutput(stderr))
	}
	assertContains(t, stderr, "hidden vaults are not searched")
	assertField(t, runOK(t, "pass", "items", "get", "--", ref), "Name:", item)

	_, stderr = runOKStderr(t, "pass", "vaults", "unhide", "--", sid)
	assertContains(t, stderr, `Unhid vault "`+name+`"`)
	if !waitFor(30*time.Second, time.Second, func() bool {
		return itemListed(t, item, "pass", "items", "list", "--limit", "0")
	}) {
		t.Error("items list never showed the item again once its vault was unhidden")
	}
}

func vaultHidden(t *testing.T, shareID string) bool {
	t.Helper()
	for _, row := range runJSONArray(t, "pass", "vaults", "list") {
		v, _ := row.(map[string]interface{})
		if v["share_id"] == shareID {
			hidden, _ := v["hidden"].(bool)
			return hidden
		}
	}
	t.Fatalf("vaults list does not show %s at all", shareID)
	return false
}

// itemListed reports whether a listing holds an item of that name.
func itemListed(t *testing.T, name string, args ...string) bool {
	t.Helper()
	for _, row := range runJSONArray(t, args...) {
		m, _ := row.(map[string]interface{})
		if m["name"] == name {
			return true
		}
	}
	return false
}
