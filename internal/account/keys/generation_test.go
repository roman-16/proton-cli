package keys

import (
	"crypto"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
)

// What a key this build writes looks like from outside.
//
// Every choice pinned here is one the standard calls free and Proton does not:
// its own clients settle each of them one way, and a key settling them any other
// way is refused where nobody can see it happen. So the shape is read back off a
// generated key rather than trusted to a configuration nobody checks.

// The clock a key is dated by is Proton's, not this machine's. A key dated in
// the future is one Proton refuses outright, so a fast clock here would write
// keys nobody accepts.
func TestGenerationDatesAKeyByProtonsClock(t *testing.T) {
	served := time.Now().Add(-36 * time.Hour).Truncate(time.Second)
	u := &Unlocked{Now: func() time.Time { return served }}

	key := generatedUnder(t, u)
	if got := key.GetEntity().PrimaryKey.CreationTime; !got.Equal(served) {
		t.Errorf("the key is dated %s, want Proton's clock at %s", got, served)
	}
	identity := key.GetEntity().PrimaryIdentity()
	if got := identity.SelfSignature.CreationTime; !got.Equal(served) {
		t.Errorf("the key's signature is dated %s, want %s", got, served)
	}
}

// An account whose clock nothing has said anything about still writes keys, by
// this machine's - which is what a key made before the first response is dated
// by.
func TestGenerationFallsBackToThisMachinesClock(t *testing.T) {
	before := time.Now().Add(-time.Minute)

	key := generatedUnder(t, &Unlocked{})
	if got := key.GetEntity().PrimaryKey.CreationTime; got.Before(before) {
		t.Errorf("the key is dated %s, want the time of the run", got)
	}
}

// What the key is, what a signature over it is hashed with, and what it says it
// prefers.
func TestGenerationWritesWhatProtonsClientsWrite(t *testing.T) {
	key := generatedUnder(t, &Unlocked{})
	entity := key.GetEntity()
	identity := entity.PrimaryIdentity()

	if got := entity.PrimaryKey.PubKeyAlgo; got != packet.PubKeyAlgoEdDSA {
		t.Errorf("the key is algorithm %d, want EdDSA", got)
	}
	if got := entity.PrimaryKey.Version; got != 4 {
		t.Errorf("the key is version %d, want 4", got)
	}
	if identity.SelfSignature.Hash != crypto.SHA512 {
		t.Errorf("the key's signature is hashed with %v, want SHA-512", identity.SelfSignature.Hash)
	}
	if got := identity.SelfSignature.PreferredHash; !slices.Equal(got, []uint8{10, 8}) {
		t.Errorf("the key prefers hashes %v, want SHA-512 then SHA-256", got)
	}
	if got := identity.SelfSignature.PreferredSymmetric; !slices.Equal(got, []uint8{9, 7}) {
		t.Errorf("the key prefers ciphers %v, want AES-256 then AES-128", got)
	}
	if len(entity.Subkeys) != 1 {
		t.Fatalf("the key carries %d subkeys, want one to encrypt with", len(entity.Subkeys))
	}
	if got := entity.Subkeys[0].PublicKey.PubKeyAlgo; got != packet.PubKeyAlgoECDH {
		t.Errorf("the subkey is algorithm %d, want ECDH, which a forwarding is derived from", got)
	}
	if entity.Subkeys[0].Sig.Hash != crypto.SHA512 {
		t.Errorf("the subkey binding is hashed with %v, want SHA-512", entity.Subkeys[0].Sig.Hash)
	}
}

// A key goes to Proton as nothing but the key: the armour carries no headers,
// because what Proton's clients send carries none.
func TestLockAndArmorWritesNoArmorHeaders(t *testing.T) {
	key := generatedUnder(t, &Unlocked{})

	armored, err := LockAndArmor(key, []byte("the passphrase"))
	if err != nil {
		t.Fatalf("LockAndArmor: %v", err)
	}
	for _, header := range []string{"Comment:", "Version:"} {
		if strings.Contains(armored, header) {
			t.Errorf("the armour carries a %s header, which Proton's clients do not write", header)
		}
	}
	read, err := pgp.NewKeyFromArmored(armored)
	if err != nil {
		t.Fatalf("the locked key is not readable armour: %v", err)
	}
	if _, err := read.Unlock([]byte("the passphrase")); err != nil {
		t.Errorf("the locked key does not open with the passphrase it was locked under: %v", err)
	}
}

func generatedUnder(t *testing.T, u *Unlocked) *pgp.Key {
	t.Helper()
	entity, err := openpgp.NewEntity("me@proton.me", "", "me@proton.me", u.Generation())
	if err != nil {
		t.Fatalf("generate a key: %v", err)
	}
	key, err := pgp.NewKeyFromEntity(entity)
	if err != nil {
		t.Fatalf("read the generated key: %v", err)
	}
	return key
}
