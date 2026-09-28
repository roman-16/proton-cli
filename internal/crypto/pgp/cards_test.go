package pgp

import (
	"testing"

	gopenpgp "github.com/ProtonMail/gopenpgp/v3/crypto"
)

func genKeyRing(t *testing.T) *gopenpgp.KeyRing {
	t.Helper()
	key, err := GenerateKey("test", "test@example.invalid")
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	kr, err := gopenpgp.NewKeyRing(key)
	if err != nil {
		t.Fatalf("NewKeyRing: %v", err)
	}
	return kr
}

// TestDecryptCardsVerdicts exercises the per-card verdict logic with real PGP
// keys (no API), covering every VerifyResult path end-to-end.
func TestDecryptCardsVerdicts(t *testing.T) {
	signer := genKeyRing(t)
	other := genKeyRing(t)

	signed, err := SignCard("FN:John", signer)
	if err != nil {
		t.Fatalf("SignCard: %v", err)
	}
	encSigned, err := EncryptAndSignCard("TEL:+123", signer, signer)
	if err != nil {
		t.Fatalf("EncryptAndSignCard: %v", err)
	}
	clear := Card{Type: CardClear, Data: "X-NOTE:hello"}

	tests := []struct {
		name     string
		card     Card
		verifier *gopenpgp.KeyRing
		wantData string
		want     VerifyResult
	}{
		{"signed, correct verifier", *signed, signer, "FN:John", Verified},
		// Detached verify cannot tell "wrong key" from "bad signature", so a key
		// ring lacking the signer reports Unverified, not Invalid.
		{"signed, unrelated verifier", *signed, other, "FN:John", Unverified},
		{"signed, no verifier", *signed, nil, "FN:John", Unverified},
		{"encrypted+signed, correct verifier", *encSigned, signer, "TEL:+123", Verified},
		{"clear card", clear, signer, "X-NOTE:hello", Unsigned},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data, verdicts, err := DecryptCards([]Card{tc.card}, signer, tc.verifier, nil)
			if err != nil {
				t.Fatalf("DecryptCards: %v", err)
			}
			if data[0] != tc.wantData {
				t.Errorf("data = %q, want %q", data[0], tc.wantData)
			}
			if verdicts[0] != tc.want {
				t.Errorf("verdict = %q, want %q", verdicts[0], tc.want)
			}
		})
	}

	t.Run("tampered data is not verified", func(t *testing.T) {
		// On the detached path a bad signature is indistinguishable from an
		// absent signer key, so tampering surfaces as Unverified (never Invalid).
		tampered := *signed
		tampered.Data = "FN:Mallory" // signature no longer matches the data
		_, verdicts, err := DecryptCards([]Card{tampered}, signer, signer, nil)
		if err != nil {
			t.Fatalf("DecryptCards: %v", err)
		}
		if verdicts[0] != Unverified {
			t.Errorf("verdict = %q, want unverified", verdicts[0])
		}
	})

	t.Run("aggregate of two valid cards is verified", func(t *testing.T) {
		_, verdicts, err := DecryptCards([]Card{*signed, *encSigned}, signer, signer, nil)
		if err != nil {
			t.Fatalf("DecryptCards: %v", err)
		}
		if got := Aggregate(verdicts...); got != Verified {
			t.Errorf("aggregate = %q, want verified", got)
		}
	})

	t.Run("aggregate with one unverifiable card is unverified", func(t *testing.T) {
		tampered := *signed
		tampered.Data = "FN:Mallory"
		_, verdicts, err := DecryptCards([]Card{tampered, *encSigned}, signer, signer, nil)
		if err != nil {
			t.Fatalf("DecryptCards: %v", err)
		}
		if got := Aggregate(verdicts...); got != Unverified {
			t.Errorf("aggregate = %q, want unverified", got)
		}
	})
}
