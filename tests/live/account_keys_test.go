package live

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
)

// The keys an account holds, and what can be done with them.
//
// What reactivating does cannot be tested against these accounts: only a reset
// locks a key, resetting one would lock that account's Drive for good, and
// nothing puts the keys back afterwards. So what is live here is the one answer
// that needs the account - whether anything is locked - and what the command
// refuses from the command line alone is in tests/offline. The rest is pinned
// against the key material itself, in internal/account/keys/reactivation_test.go
// and imports_test.go, which is also where a copy of a locked key coming back is.
//
// Everything that changes a key does it on the primary account's own address and
// puts it back: a key made or imported here is deleted again, and the key the
// address wrote with before is made primary again.

// An account whose keys are all in use has nothing to reactivate, and hears so
// after the keys are read rather than being asked for a password first.
func TestAccountKeysReactivateOnAnAccountWithNothingLocked(t *testing.T) {
	_, stderr, code := run(t, "account", "keys", "reactivate",
		"--previous-password-file", passwordFile(t, "not the password"),
		"--password-file", passwordFile(t, "not the password either"))
	if code != 1 {
		t.Errorf("reactivating with nothing locked exited %d, want 1", code)
	}
	assertContains(t, stderr, "No keys are locked.")
}

// heldKey is one row of the key listing.
type heldKey struct {
	id, kind, address, fingerprint, status, usedFor string
	primary                                         bool
}

func accountKeys(t *testing.T) []heldKey {
	t.Helper()
	var out []heldKey
	for _, row := range runJSONArray(t, "account", "keys", "list") {
		m, _ := row.(map[string]interface{})
		str := func(k string) string { s, _ := m[k].(string); return s }
		primary, _ := m["primary"].(bool)
		out = append(out, heldKey{
			id: str("id"), kind: str("kind"), address: str("address"), fingerprint: str("fingerprint"),
			status: str("status"), usedFor: str("used_for"), primary: primary,
		})
	}
	return out
}

// primaryKeyOf is the key an address writes with.
func primaryKeyOf(t *testing.T, email string) heldKey {
	t.Helper()
	for _, k := range accountKeys(t) {
		if k.address == email && k.primary && len(k.fingerprint) == 40 {
			return k
		}
	}
	t.Fatalf("%s has no primary key in the listing", email)
	return heldKey{}
}

func keyByID(t *testing.T, id string) (heldKey, bool) {
	t.Helper()
	for _, k := range accountKeys(t) {
		if k.id == id {
			return k, true
		}
	}
	return heldKey{}, false
}

func assertKeyStatus(t *testing.T, id, want string) {
	t.Helper()
	k, ok := keyByID(t, id)
	if !ok {
		t.Fatalf("key %s is not in the listing", id)
	}
	if k.status != want {
		t.Errorf("key %s is %s, want %s", id, k.status, want)
	}
}

// The listing holds the address's keys and the account's own, each with what it
// is used for.
func TestAccountKeysListNamesTheAddressAndTheAccountKeys(t *testing.T) {
	primary := primaryKeyOf(t, selfEmail())
	if primary.status != "primary" || primary.usedFor != "encryption, decryption, signing, verification" {
		t.Errorf("the address's primary key reads as %s, used for %q", primary.status, primary.usedFor)
	}
	var account bool
	for _, k := range accountKeys(t) {
		account = account || (k.kind == "account" && k.primary)
	}
	if !account {
		t.Error("the listing holds no primary account key")
	}
}

// A key is named by its fingerprint as readily as by its ID.
func TestAccountKeysGetByFingerprint(t *testing.T) {
	primary := primaryKeyOf(t, selfEmail())
	stdout := runOK(t, "account", "keys", "get", primary.fingerprint)
	assertField(t, stdout, "Status:", "primary")
	assertField(t, stdout, "Address:", selfEmail())
}

// The public half comes out as a key anything reading OpenPGP opens, and is the
// key the listing names.
func TestAccountKeysExportWritesThePublicKey(t *testing.T) {
	primary := primaryKeyOf(t, selfEmail())
	dest := filepath.Join(t.TempDir(), "public.asc")
	runOK(t, "account", "keys", "export", "--dest", dest, primary.id)
	key := readKeyFile(t, dest)
	if key.IsPrivate() {
		t.Error("the public export holds a private key")
	}
	if key.GetFingerprint() != primary.fingerprint {
		t.Errorf("exported %s, want %s", key.GetFingerprint(), primary.fingerprint)
	}
}

// The private key comes out locked with the passphrase given, and only that
// passphrase opens it.
func TestAccountKeysExportPrivateLocksTheFile(t *testing.T) {
	primary := primaryKeyOf(t, selfEmail())
	dest := filepath.Join(t.TempDir(), "private.asc")
	runOK(t, "account", "keys", "export", "--private", "--dest", dest,
		"--passphrase-file", passwordFile(t, "the backup passphrase"), primary.id)
	key := readKeyFile(t, dest)
	if locked, err := key.IsLocked(); err != nil || !locked {
		t.Fatalf("the private export is not locked: %v", err)
	}
	if _, err := key.Unlock([]byte("the backup passphrase")); err != nil {
		t.Errorf("the passphrase does not open the export: %v", err)
	}
	if key.GetFingerprint() != primary.fingerprint {
		t.Errorf("exported %s, want %s", key.GetFingerprint(), primary.fingerprint)
	}
}

func readKeyFile(t *testing.T, path string) *pgp.Key {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the export: %v", err)
	}
	key, err := pgp.NewKeyFromArmored(string(data))
	if err != nil {
		t.Fatalf("the export is not an armoured key: %v", err)
	}
	return key
}

// The primary key is what the address writes with, so it is neither marked nor
// deleted, and the account's own keys are not changed here at all.
func TestAccountKeysRefuseToMarkOrDeleteThePrimary(t *testing.T) {
	primary := primaryKeyOf(t, selfEmail())
	_, stderr, code := run(t, "account", "keys", "update", "--compromised", primary.id)
	if code != 1 || !strings.Contains(stderr, "cannot be marked compromised") {
		t.Errorf("marking the primary compromised exited %d: %s", code, stderr)
	}
	_, stderr, code = run(t, "account", "keys", "delete", "--yes", primary.id)
	if code != 1 || !strings.Contains(stderr, "cannot be deleted") {
		t.Errorf("deleting the primary exited %d: %s", code, stderr)
	}
	for _, k := range accountKeys(t) {
		if k.kind != "account" {
			continue
		}
		_, stderr, code = run(t, "account", "keys", "update", "--obsolete", k.id)
		if code != 1 || !strings.Contains(stderr, "is an account key") {
			t.Errorf("marking an account key exited %d: %s", code, stderr)
		}
		break
	}
}

// A key made here becomes the primary; the one it replaced is made primary
// again, the new one is marked and cleared, and it is deleted.
func TestAccountKeysCreateMarkAndDelete(t *testing.T) {
	before := primaryKeyOf(t, selfEmail())
	stdout := runOK(t, "account", "keys", "create", selfEmail())
	made := assertBareID(t, stdout, "keys create")
	restore(t, before.id, made)

	assertKeyStatus(t, made, "primary")
	assertKeyStatus(t, before.id, "active")

	runOK(t, "account", "keys", "update", "--primary", before.id)
	assertKeyStatus(t, before.id, "primary")
	assertKeyStatus(t, made, "active")

	runOK(t, "account", "keys", "update", "--compromised", made)
	assertKeyStatus(t, made, "compromised")
	runOK(t, "account", "keys", "update", "--compromised=false", made)
	assertKeyStatus(t, made, "obsolete")
	runOK(t, "account", "keys", "update", "--obsolete=false", made)
	assertKeyStatus(t, made, "active")

	runOK(t, "account", "keys", "delete", "--yes", made)
	if _, ok := keyByID(t, made); ok {
		t.Error("the deleted key is still in the listing")
	}
}

// A key brought in from a file joins the address as one that reads, and goes
// again.
func TestAccountKeysImportAndDelete(t *testing.T) {
	key, err := pgp.GenerateKey("proton-cli test", selfEmail(), "x25519", 0)
	if err != nil {
		t.Fatalf("generate a key: %v", err)
	}
	locked, err := key.Lock([]byte("the file's passphrase"))
	if err != nil {
		t.Fatalf("lock the key: %v", err)
	}
	armored, err := locked.Armor()
	if err != nil {
		t.Fatalf("armor the key: %v", err)
	}
	file := filepath.Join(t.TempDir(), "imported.asc")
	if err := os.WriteFile(file, []byte(armored), 0o600); err != nil {
		t.Fatal(err)
	}

	stdout := runOK(t, "account", "keys", "import",
		"--passphrase-file", passwordFile(t, "the file's passphrase"), selfEmail(), file)
	imported := assertBareID(t, stdout, "keys import")
	cleanupRun(t, "Delete the imported key: proton account keys delete --yes "+imported,
		"account", "keys", "delete", "--yes", imported)

	k, ok := keyByID(t, imported)
	if !ok || k.fingerprint != key.GetFingerprint() || k.status != "active" {
		t.Errorf("the imported key reads as %+v", k)
	}
	runOK(t, "account", "keys", "delete", "--yes", imported)
}

// restore puts the address back as the test found it, whatever the test got to:
// the key it wrote with is primary, and the key the test made is gone.
func restore(t *testing.T, primary, made string) {
	t.Helper()
	cleanup(t, "Restore the address's keys: proton account keys update --primary "+primary+
		", then proton account keys delete --yes "+made, func() error {
		if k, ok := keyByID(t, primary); ok && !k.primary {
			if _, stderr, code := run(t, "account", "keys", "update", "--primary", primary); code != 0 {
				return fmt.Errorf("make %s primary again: %s", primary, stderr)
			}
		}
		if _, ok := keyByID(t, made); ok {
			if _, stderr, code := run(t, "account", "keys", "delete", "--yes", made); code != 0 {
				return fmt.Errorf("delete %s: %s", made, stderr)
			}
		}
		return nil
	})
}
