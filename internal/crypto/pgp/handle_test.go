package pgp

import (
	"bytes"
	"errors"
	"testing"

	"github.com/ProtonMail/gopenpgp/v3/constants"
	gopenpgp "github.com/ProtonMail/gopenpgp/v3/crypto"
)

const multiline = "first line\nsecond line\n"

// Text travels with its line endings made canonical and comes back with them
// restored, which is what another client writes and expects.
func TestTextComesBackAsItWasWritten(t *testing.T) {
	kr := genKeyRing(t)
	msg, err := EncryptText(kr, nil, multiline)
	if err != nil {
		t.Fatalf("EncryptText: %v", err)
	}
	raw, err := Decrypt(kr, nil, msg.Bytes(), gopenpgp.Bytes)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if !bytes.Contains(raw.Bytes(), []byte("first line\r\nsecond line\r\n")) {
		t.Errorf("the message holds %q, want the line endings canonical", raw.Bytes())
	}
	text, err := DecryptText(kr, nil, msg.Bytes(), gopenpgp.Bytes)
	if err != nil {
		t.Fatalf("DecryptText: %v", err)
	}
	if text.String() != multiline {
		t.Errorf("DecryptText = %q, want %q", text.String(), multiline)
	}
}

// Bytes are carried exactly, carriage returns and all.
func TestBinaryIsCarriedExactly(t *testing.T) {
	kr := genKeyRing(t)
	data := []byte("a\r\nb\nc\x00")
	msg, err := EncryptBinary(kr, nil, data)
	if err != nil {
		t.Fatalf("EncryptBinary: %v", err)
	}
	out, err := Decrypt(kr, nil, msg.Bytes(), gopenpgp.Bytes)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if !bytes.Equal(out.Bytes(), data) {
		t.Errorf("Decrypt = %q, want %q", out.Bytes(), data)
	}
}

// A text signature holds whichever line endings the text is handed over with,
// because both sides make them canonical first.
func TestTextSignatureHoldsAcrossLineEndings(t *testing.T) {
	kr := genKeyRing(t)
	sig, err := SignText(kr, multiline, gopenpgp.Bytes)
	if err != nil {
		t.Fatalf("SignText: %v", err)
	}
	if err := VerifyText(kr, "first line\r\nsecond line\r\n", sig, gopenpgp.Bytes); err != nil {
		t.Errorf("the signature does not hold over the canonical form: %v", err)
	}
	if err := VerifyText(kr, "first line\nsecond line!\n", sig, gopenpgp.Bytes); Classify(err) != Invalid {
		t.Errorf("a changed text verified as %s, want invalid", Classify(err))
	}
}

// The data packet written under a session key opens with that key alone, and
// the key packet opens the session key for the ring it was sealed to.
func TestSessionKeyRoundTrip(t *testing.T) {
	kr := genKeyRing(t)
	sk, err := PGP.GenerateSessionKey()
	if err != nil {
		t.Fatalf("GenerateSessionKey: %v", err)
	}
	dataPacket, err := EncryptTextWithSessionKey(sk, nil, multiline)
	if err != nil {
		t.Fatalf("EncryptTextWithSessionKey: %v", err)
	}
	keyPacket, err := EncryptSessionKey(kr, sk)
	if err != nil {
		t.Fatalf("EncryptSessionKey: %v", err)
	}
	opened, err := DecryptSessionKey(kr, keyPacket)
	if err != nil {
		t.Fatalf("DecryptSessionKey: %v", err)
	}
	text, err := DecryptTextWithSessionKey(opened, dataPacket)
	if err != nil {
		t.Fatalf("DecryptTextWithSessionKey: %v", err)
	}
	if text != multiline {
		t.Errorf("the data packet opens to %q, want %q", text, multiline)
	}
	whole, err := DecryptText(kr, nil, gopenpgp.NewPGPSplitMessage(keyPacket, dataPacket).Bytes(), gopenpgp.Bytes)
	if err != nil {
		t.Fatalf("the joined packets do not open: %v", err)
	}
	if whole.String() != multiline {
		t.Errorf("the joined packets open to %q, want %q", whole.String(), multiline)
	}
}

func TestPasswordRoundTrip(t *testing.T) {
	password := []byte("correct horse")
	armored, err := EncryptTextWithPassword(password, multiline)
	if err != nil {
		t.Fatalf("EncryptTextWithPassword: %v", err)
	}
	text, err := DecryptTextWithPassword(password, armored)
	if err != nil {
		t.Fatalf("DecryptTextWithPassword: %v", err)
	}
	if text != multiline {
		t.Errorf("DecryptTextWithPassword = %q, want %q", text, multiline)
	}
	if _, err := DecryptTextWithPassword([]byte("wrong"), armored); err == nil {
		t.Error("a wrong password opened the message")
	}
}

// A message signed by the key that decrypts it is not thereby verified as
// coming from somebody else: the verdict is against the verifier alone.
func TestSignatureErrorJudgesAgainstTheVerifierAlone(t *testing.T) {
	me := genKeyRing(t)
	sender := genKeyRing(t)
	signedByMe, err := EncryptText(me, me, multiline)
	if err != nil {
		t.Fatalf("EncryptText: %v", err)
	}
	signedBySender, err := EncryptText(me, sender, multiline)
	if err != nil {
		t.Fatalf("EncryptText: %v", err)
	}

	for _, tc := range []struct {
		name     string
		message  *gopenpgp.PGPMessage
		verifier *gopenpgp.KeyRing
		want     VerifyResult
	}{
		{"signed by the verifier", signedBySender, sender, Verified},
		{"signed by the decrypting key, judged against the sender", signedByMe, sender, Unverified},
		{"no verifier", signedBySender, nil, Unverified},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := DecryptText(me, tc.verifier, tc.message.Bytes(), gopenpgp.Bytes)
			if err != nil {
				t.Fatalf("DecryptText: %v", err)
			}
			verdict := SignatureError(res, tc.verifier)
			if got := Classify(verdict); got != tc.want {
				t.Errorf("verdict = %s, want %s", got, tc.want)
			}
			var sigErr gopenpgp.SignatureVerificationError
			if tc.want == Unverified && (!errors.As(verdict, &sigErr) || sigErr.Status != constants.SIGNATURE_NO_VERIFIER) {
				t.Errorf("verdict = %v, want no matching verifier", verdict)
			}
		})
	}
}
