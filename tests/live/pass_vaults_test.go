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

func TestPassVaultRename(t *testing.T) {
	name := testID() + "-vault"
	sid := createVault(t, name)
	cleanupRun(t, fmt.Sprintf("Delete vault: proton pass vaults delete %s", sid),
		"pass", "vaults", "delete", "--", sid)

	newName := name + "-renamed"
	runOK(t, "pass", "vaults", "update", "--name", newName, sid)
	assertContains(t, runOK(t, "pass", "vaults", "list"), newName)
}
