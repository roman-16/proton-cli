package keys

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/errs"
)

// Bringing keys in from a file is judged before anything is sent, so what a
// file can be is pinned here - armoured or not, several keys, locked or not -
// and so is what becomes of each key once it meets the address.

func noPassphrase(t *testing.T) func() (string, error) {
	return func() (string, error) {
		t.Error("asked for a passphrase a file whose keys are not locked does not need")
		return "", errors.New("not asked")
	}
}

func passphrase(p string) func() (string, error) {
	return func() (string, error) { return p, nil }
}

func TestReadKeysReadsEveryArmouredBlock(t *testing.T) {
	one, two := generated(t, "one"), generated(t, "two")
	file := armoredOf(t, one) + "\n" + armoredOf(t, two)
	offered, err := ReadKeys([]byte(file), "keys.asc", noPassphrase(t))
	if err != nil {
		t.Fatalf("ReadKeys: %v", err)
	}
	if len(offered) != 2 || offered[0].Key.GetFingerprint() != one.GetFingerprint() ||
		offered[1].Key.GetFingerprint() != two.GetFingerprint() {
		t.Errorf("read %d keys, want both in order", len(offered))
	}
}

func TestReadKeysReadsBarePackets(t *testing.T) {
	key := generated(t, "bare")
	packets, err := key.Serialize()
	if err != nil {
		t.Fatalf("Serialize: %v", err)
	}
	offered, err := ReadKeys(packets, "key.gpg", noPassphrase(t))
	if err != nil || len(offered) != 1 {
		t.Fatalf("ReadKeys = %d keys, %v", len(offered), err)
	}
}

func TestReadKeysOpensALockedKeyWithThePassphrase(t *testing.T) {
	key := generated(t, "locked")
	file := locked(t, key, "correct horse")
	offered, err := ReadKeys([]byte(file), "key.asc", passphrase("correct horse"))
	if err != nil {
		t.Fatalf("ReadKeys: %v", err)
	}
	if unlocked, err := offered[0].Key.IsUnlocked(); err != nil || !unlocked {
		t.Error("the key came back still locked")
	}

	_, err = ReadKeys([]byte(file), "key.asc", passphrase("wrong"))
	var refused *PassphraseRefused
	if !errors.As(err, &refused) || !strings.Contains(err.Error(), "key.asc did not open") {
		t.Errorf("a wrong passphrase reads as %v", err)
	}
}

func TestReadKeysRefusesWhatIsNotAnAddressKey(t *testing.T) {
	public, err := generated(t, "public").GetArmoredPublicKey()
	if err != nil {
		t.Fatalf("GetArmoredPublicKey: %v", err)
	}
	for name, tc := range map[string]struct {
		file string
		want string
	}{
		"a public key alone": {public, "holds no private key"},
		"not a key at all":   {"hello", "holds no key this build can read"},
		"a post-quantum key": {publishedKey(t, "postquantum-private.asc"), "post-quantum"},
		"a key that signs only": {
			signOnlyKey(t, "signer", "pass"), "cannot encrypt",
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ReadKeys([]byte(tc.file), "file", passphrase("pass"))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("ReadKeys = %v, want %q", err, tc.want)
			}
			var coder errs.ExitCoder
			if !errors.As(err, &coder) {
				t.Errorf("the refusal is not phrased for a person: %v", err)
			}
		})
	}
}

func TestArrivalsSortWhatAFileBrought(t *testing.T) {
	m := newManaged(t)
	old := generated(t, "old")
	m.u.Addresses[0].Keys = append(m.u.Addresses[0].Keys, Key{
		ID: "locked-key", PrivateKey: locked(t, old, "a password from before"), Flags: mailKeyFlags,
	})
	fresh := generated(t, "fresh")

	arrivals, err := m.u.Arrivals("address", []Offered{
		{Key: fresh, Source: "fresh.asc"}, {Key: old, Source: "old.asc"}, {Key: fresh, Source: "again.asc"},
	})
	if err != nil {
		t.Fatalf("Arrivals: %v", err)
	}
	if len(arrivals) != 2 {
		t.Fatalf("%d arrivals, want the fresh key once and the old one", len(arrivals))
	}
	if arrivals[0].Returning != nil || arrivals[1].Returning == nil || arrivals[1].Returning.ID != "locked-key" {
		t.Errorf("arrivals read %+v", arrivals)
	}

	if _, err := m.u.Arrivals("address", []Offered{{Key: m.second, Source: "second.asc"}}); err == nil ||
		!strings.Contains(err.Error(), "already on me@proton.me") {
		t.Errorf("a key the address uses was offered again: %v", err)
	}
}

// A key that is new to the address joins as one that reads, under a passphrase
// of its own.
func TestImportAddsAKeyThatReads(t *testing.T) {
	m := newManaged(t)
	key := generated(t, "imported")
	if _, err := m.u.Import(context.Background(), m.api, "address", Arrival{Key: key, Source: "key.asc"}); err != nil {
		t.Fatalf("Import: %v", err)
	}
	body := m.api.sent(t, "POST", "/core/v4/keys/address")
	if body["Primary"] != 0 || body["Token"] == m.token.sealed {
		t.Errorf("imported as primary %v under the address's shared token: %v", body["Primary"], body["Token"] == m.token.sealed)
	}
	entries, data, signature := listOf(t, body)
	if len(entries) != 3 || entries[2].Fingerprint != key.GetFingerprint() || entries[2].Primary != 0 {
		t.Fatalf("the list reads %+v, want the key named last", entries)
	}
	assertKeyListSignature(t, m.primary, data, signature)
}

// An address with no key in use takes an imported key as its primary, with a
// list naming it alone.
func TestImportGivesAnEmptyAddressItsPrimary(t *testing.T) {
	m := newManaged(t)
	m.u.Addresses[0].Keys, m.u.Addresses[0].SignedKeyList = nil, nil
	key := generated(t, "first")
	if _, err := m.u.Import(context.Background(), m.api, "address", Arrival{Key: key, Source: "key.asc"}); err != nil {
		t.Fatalf("Import: %v", err)
	}
	body := m.api.sent(t, "POST", "/core/v4/keys/address")
	entries, data, signature := listOf(t, body)
	if body["Primary"] != 1 || len(entries) != 1 || entries[0].Primary != 1 {
		t.Errorf("published as primary %v with the list %+v", body["Primary"], entries)
	}
	assertKeyListSignature(t, key, data, signature)
}

// A copy of a key the address holds locked comes back as that key, with the
// user IDs its record has and named as one that reads.
func TestImportBringsBackALockedKeyAsItself(t *testing.T) {
	m := newManaged(t)
	old := generated(t, "old")
	m.u.Addresses[0].Keys = append(m.u.Addresses[0].Keys, Key{
		ID: "locked-key", PrivateKey: locked(t, old, "a password from before"), Flags: mailKeyFlags,
	})
	// The same key material under other user IDs, as an export from somewhere
	// else may carry it.
	copied, err := old.Copy()
	if err != nil {
		t.Fatalf("Copy: %v", err)
	}
	copied.GetEntity().Identities = generated(t, "elsewhere").GetEntity().Identities

	arrival := Arrival{Key: copied, Source: "backup.asc", Returning: &m.u.Addresses[0].Keys[2]}
	id, err := m.u.Import(context.Background(), m.api, "address", arrival)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if id != "locked-key" {
		t.Errorf("came back as %q, want the key's own ID", id)
	}
	body := m.api.sent(t, "PUT", "/core/v4/keys/address/locked-key")
	token, err := decryptToken(body["Token"].(string), body["Signature"].(string), m.u.UserKR)
	if err != nil {
		t.Fatalf("the token does not open with the user key: %v", err)
	}
	returned, err := pgp.NewKeyFromArmored(body["PrivateKey"].(string))
	if err != nil {
		t.Fatalf("the key is not readable armour: %v", err)
	}
	if _, err := returned.Unlock(token); err != nil {
		t.Errorf("the key does not open with the token sent beside it: %v", err)
	}
	if got := returned.GetEntity().PrimaryIdentity().UserId.Email; got != "old@example.invalid" {
		t.Errorf("the key came back addressed to %q, want its record's own", got)
	}
	entries, _, _ := listOf(t, body)
	last := entries[len(entries)-1]
	if last.Fingerprint != old.GetFingerprint() || last.Primary != 0 || last.Flags != keyNotCompromised {
		t.Errorf("the list names it as %+v, want a key that reads and is sealed to nothing new", last)
	}
	if h := m.held(t, "locked-key"); h.Locked() {
		t.Error("afterwards the account believes the key is still locked")
	}
}

// A record whose only user ID is the placeholder some early keys carry comes
// back addressed to the address.
func TestImportAddressesAKeyWhoseRecordHasOnlyAPlaceholder(t *testing.T) {
	m := newManaged(t)
	entity, err := openpgp.NewEntity("UserID", "", "", &packet.Config{Algorithm: packet.PubKeyAlgoEdDSA})
	if err != nil {
		t.Fatalf("NewEntity: %v", err)
	}
	placeholder, err := pgp.NewKeyFromEntity(entity)
	if err != nil {
		t.Fatalf("NewKeyFromEntity: %v", err)
	}
	record := Key{ID: "legacy-key", PrivateKey: armoredOf(t, placeholder), Flags: mailKeyFlags}

	restored, err := m.u.withUserIDsOf(record, placeholder, "me@proton.me")
	if err != nil {
		t.Fatalf("withUserIDsOf: %v", err)
	}
	if got := restored.GetEntity().PrimaryIdentity().UserId.Email; got != "me@proton.me" {
		t.Errorf("the key came back addressed to %q, want the address", got)
	}
}
