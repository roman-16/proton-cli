package keys

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/proton"
)

// Re-locking the account's keys.
//
// What is checked here is the pair of things a live run cannot see: that the
// keys handed to Proton are the keys the account already had, and that they
// open with the passphrase the new secret and the new salt derive - which is
// what every other client will try. A round trip against Proton proves the
// request was accepted; only this proves it was the right one.

func TestRelockHandsBackTheSameKeysUnderTheNewSecret(t *testing.T) {
	u, key := unlockedAccount(t, "the old password")
	var sent map[string]any
	c := recording(t, &sent)

	if err := u.Relock(context.Background(), c, "the new password", true); err != nil {
		t.Fatalf("Relock: %v", err)
	}

	salt, _ := sent["KeySalt"].(string)
	raw, err := base64.StdEncoding.DecodeString(salt)
	if err != nil || len(raw) != keySaltBytes {
		t.Fatalf("KeySalt = %q, want %d base64 bytes (%v)", salt, keySaltBytes, err)
	}
	keys, _ := sent["UserKeys"].([]any)
	if len(keys) != 1 {
		t.Fatalf("sent %d user keys, want the one the account has", len(keys))
	}
	record, _ := keys[0].(map[string]any)
	if id, _ := record["ID"].(string); id != "user-key" {
		t.Errorf("ID = %v, want the record the key belongs to", record["ID"])
	}

	// The key opens with what the new secret and the new salt derive, and it is
	// the key that was there.
	passphrase, err := stretch("the new password", salt)
	if err != nil {
		t.Fatalf("stretch: %v", err)
	}
	armored, _ := record["PrivateKey"].(string)
	locked, err := pgp.NewKeyFromArmored(armored)
	if err != nil {
		t.Fatalf("read the key sent to Proton: %v", err)
	}
	opened, err := locked.Unlock([]byte(passphrase))
	if err != nil {
		t.Fatalf("the key does not open with the new key password: %v", err)
	}
	if opened.GetFingerprint() != key.GetFingerprint() {
		t.Error("the key sent to Proton is not the key the account had")
	}
	// The secret itself goes nowhere near the request.
	if body, _ := json.Marshal(sent); strings.Contains(string(body), "the new password") {
		t.Error("the request carries the password itself")
	}
}

// The verifier is what says whether the secret signs in as well, which is the
// whole of the difference between the account's two password modes.
func TestRelockSendsAVerifierOnlyForTheSecretThatSignsIn(t *testing.T) {
	for _, tc := range []struct {
		name      string
		signsIn   bool
		wantsAuth bool
	}{
		{name: "one password", signsIn: true, wantsAuth: true},
		{name: "two passwords", signsIn: false, wantsAuth: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u, _ := unlockedAccount(t, "the old password")
			var sent map[string]any
			c := recording(t, &sent)
			if err := u.Relock(context.Background(), c, "the new password", tc.signsIn); err != nil {
				t.Fatalf("Relock: %v", err)
			}
			auth, _ := sent["Auth"].(map[string]any)
			if tc.wantsAuth != (auth != nil) {
				t.Fatalf("Auth present = %v, want %v", auth != nil, tc.wantsAuth)
			}
			if !tc.wantsAuth {
				return
			}
			for _, field := range []string{"ModulusID", "Salt", "Verifier", "Version"} {
				if auth[field] == nil || auth[field] == "" {
					t.Errorf("Auth.%s is empty", field)
				}
			}
		})
	}
}

// An account whose address keys predate the move under the user key is refused
// before anything is sent: a change made here would leave them locked with the
// old secret and nothing on screen would say so.
func TestRelockRefusesAddressKeysAPasswordStillLocks(t *testing.T) {
	u, _ := unlockedAccount(t, "the old password")
	for i := range u.Addresses {
		for j := range u.Addresses[i].Keys {
			u.Addresses[i].Keys[j].Token = ""
			u.Addresses[i].Keys[j].Signature = ""
		}
	}
	var sent map[string]any
	c := recording(t, &sent)

	err := u.Relock(context.Background(), c, "the new password", true)
	if err == nil {
		t.Fatal("a legacy hierarchy was re-locked; it should be refused")
	}
	if !strings.Contains(err.Error(), "before Proton moved them") {
		t.Errorf("refusal reads %q", err)
	}
	if sent != nil {
		t.Error("the refusal sent a request")
	}
}

// unlockedAccount is a hierarchy as Relock finds it: one open user key, its
// record, and an address whose key hangs off it.
func unlockedAccount(t *testing.T, secret string) (*Unlocked, *pgp.Key) {
	t.Helper()
	key := generated(t, "user")
	ring, err := pgp.NewKeyRing(key)
	if err != nil {
		t.Fatalf("ring: %v", err)
	}
	return &Unlocked{
		UserKR: ring,
		UserKeys: []Key{{
			ID: "user-key", PrivateKey: locked(t, key, secret), Primary: 1, Active: 1,
		}},
		Addresses: []Address{{
			ID: "address", Email: "me@proton.me",
			Keys: []Key{{ID: "address-key", Token: "token", Signature: "signature", Active: 1}},
		}},
		private: true,
		keyPass: []byte(secret),
	}, key
}

// recording answers the requests a re-lock makes and keeps the one that
// matters.
func recording(t *testing.T, sent *map[string]any) *proton.Client {
	t.Helper()
	// A modulus Proton signed, because go-srp checks the signature before it
	// will derive anything from one. It is the fixture from go-srp's own tests.
	modulus, err := os.ReadFile("testdata/modulus.asc")
	if err != nil {
		t.Fatalf("modulus fixture: %v", err)
	}
	return client(t, func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{"Code": 1000}
		switch r.URL.Path {
		case "/core/v4/auth/modulus":
			body["Modulus"], body["ModulusID"] = string(modulus), "modulus-id"
		case "/core/v4/keys/private", "/core/v4/settings/mnemonic":
			if err := json.NewDecoder(r.Body).Decode(sent); err != nil {
				t.Errorf("decode the request to %s: %v", r.URL.Path, err)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(body)
	})
}
