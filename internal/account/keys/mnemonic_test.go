package keys

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/crypto/bip39"
)

// Setting a recovery phrase.
//
// The phrase is only worth anything if the words open the copy of the keys that
// went with them, and nothing about a successful request says they do: Proton
// stores what it is handed. So this walks the whole way back - the words to the
// bytes, the bytes and the salt to a passphrase, the passphrase to the key -
// and checks the key that comes out is the account's.

func TestANewRecoveryPhraseOpensTheKeysItWasSentWith(t *testing.T) {
	u, key := unlockedAccount(t, "the password")
	var sent map[string]any
	c := recording(t, &sent)

	phrase, err := u.NewRecoveryPhrase(context.Background(), c)
	if err != nil {
		t.Fatalf("NewRecoveryPhrase: %v", err)
	}
	if words := strings.Fields(phrase); len(words) != 12 {
		t.Fatalf("a recovery phrase is twelve words, got %d", len(words))
	}

	entropy, err := bip39.Entropy(phrase)
	if err != nil {
		t.Fatalf("the phrase does not read back: %v", err)
	}
	salt, _ := sent["MnemonicSalt"].(string)
	passphrase, err := stretch(base64.StdEncoding.EncodeToString(entropy), salt)
	if err != nil {
		t.Fatalf("stretch: %v", err)
	}
	keys, _ := sent["MnemonicUserKeys"].([]any)
	if len(keys) != 1 {
		t.Fatalf("sent %d keys, want the one the account has", len(keys))
	}
	record, _ := keys[0].(map[string]any)
	armored, _ := record["PrivateKey"].(string)
	shut, err := pgp.NewKeyFromArmored(armored)
	if err != nil {
		t.Fatalf("read the key sent to Proton: %v", err)
	}
	opened, err := shut.Unlock([]byte(passphrase))
	if err != nil {
		t.Fatalf("the phrase does not open the key it was sent with: %v", err)
	}
	if opened.GetFingerprint() != key.GetFingerprint() {
		t.Error("the phrase opens a key that is not the account's")
	}
	// And the verifier that lets the phrase be proved at a sign-in nobody has
	// the password for.
	if auth, _ := sent["MnemonicAuth"].(map[string]any); auth["Verifier"] == nil {
		t.Errorf("MnemonicAuth = %v, want a verifier for the phrase", sent["MnemonicAuth"])
	}
}

// An account whose keys its organization holds has no phrase of its own to
// hand out, and one whose address keys predate the move under the user key
// would get a phrase that opens the account and not its mail.
func TestARecoveryPhraseIsRefusedWhereItWouldNotOpenEverything(t *testing.T) {
	for _, tc := range []struct {
		name   string
		break_ func(*Unlocked)
		want   string
	}{{
		name:   "keys the organization holds",
		break_: func(u *Unlocked) { u.private = false },
		want:   "belong to its organization",
	}, {
		name: "address keys a password still locks",
		break_: func(u *Unlocked) {
			u.Addresses[0].Keys[0].Token, u.Addresses[0].Keys[0].Signature = "", ""
		},
		want: "before Proton moved them",
	}} {
		t.Run(tc.name, func(t *testing.T) {
			u, _ := unlockedAccount(t, "the password")
			tc.break_(u)
			var sent map[string]any
			c := recording(t, &sent)
			_, err := u.NewRecoveryPhrase(context.Background(), c)
			if err == nil {
				t.Fatal("a phrase was set; it should be refused")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("refusal reads %q", err)
			}
			if sent != nil {
				t.Error("the refusal sent a request")
			}
		})
	}
}
