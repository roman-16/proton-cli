package fork

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/roman-16/proton-cli/internal/crypto/aead"
)

// A payload sealed by Proton's own code, so the format is checked against the
// clients that write it rather than against this implementation's idea of it.
// Produced by WebCrypto the way getForkEncryptedBlob does at payload version 1:
// AES-GCM under a sixteen-byte nonce, no additional data, nonce ahead of the
// ciphertext, base64.
const (
	protonKey         = "AwoRGB8mLTQ7QklQV15lbHN6gYiPlp2kq7K5wMfO1dw="
	protonPayload     = "BRAbJjE8R1JdaHN+iZSfqt6mHGnTF0wv2CvRVoV1gK6gun/i10M6vrpRzTr3Lf0tGy69IAQBHmxTMfmx0d+3K3P+7U1Q1hZiBfSi5ScbwC/74RKzgrXiJXjYaHhvcpxIg28="
	protonKeyPassword = "20ssOkPNuXPd1Q0ROdPTLZ5NpUuCcjO"
)

func TestOpensAPayloadProtonSealed(t *testing.T) {
	key, err := base64.StdEncoding.DecodeString(protonKey)
	if err != nil {
		t.Fatalf("decode key: %v", err)
	}
	got, err := Code{UserCode: "8FJ3K2QP", Key: key, ClientID: "Other"}.Open(protonPayload)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if got != protonKeyPassword {
		t.Errorf("key password = %q, want %q", got, protonKeyPassword)
	}
}

func TestSealsWhatItCanOpen(t *testing.T) {
	c, err := New("8FJ3K2QP", "Other")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	sealed, err := c.Seal(protonKeyPassword)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if strings.Contains(sealed, protonKeyPassword) {
		t.Error("the sealed payload carries the key password in the clear")
	}
	got, err := c.Open(sealed)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if got != protonKeyPassword {
		t.Errorf("key password = %q, want %q", got, protonKeyPassword)
	}
}

// A payload is sealed to the code that was shown, so a code that was not shown
// opens nothing.
func TestAnotherCodeOpensNothing(t *testing.T) {
	mine, err := New("8FJ3K2QP", "Other")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	sealed, err := mine.Seal(protonKeyPassword)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	theirs, err := New("8FJ3K2QP", "Other")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := theirs.Open(sealed); err == nil {
		t.Error("a payload sealed to one code opened under another")
	}
}

func TestACodeSurvivesBeingWrittenDown(t *testing.T) {
	c, err := New("8FJ3K2QP", "Other")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	back, err := Parse(c.String())
	if err != nil {
		t.Fatalf("Parse(%q): %v", c.String(), err)
	}
	if back.UserCode != c.UserCode || back.ClientID != c.ClientID {
		t.Errorf("read back %+v, want %+v", back, c)
	}
	if len(back.Key) != aead.KeyLen || string(back.Key) != string(c.Key) {
		t.Error("the key did not survive the round trip")
	}
}

// Whitespace is what a code picks up on its way through a terminal and a
// clipboard, and it is the one thing about a mistyped code that is not the
// person's mistake.
func TestACodeIsReadWithoutItsSurroundingSpace(t *testing.T) {
	c, err := New("8FJ3K2QP", "Other")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := Parse("  " + c.String() + "\n"); err != nil {
		t.Errorf("Parse with space around it: %v", err)
	}
}

// A code Proton's apps write with no key signs a device in without unlocking it,
// which is a shape to read rather than a code to refuse.
func TestACodeMayCarryNoKey(t *testing.T) {
	c, err := Parse("0:8FJ3K2QP::web-mail")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(c.Key) != 0 {
		t.Errorf("key = %v, want none", c.Key)
	}
	sealed, err := c.Seal(protonKeyPassword)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if sealed != "" {
		t.Errorf("sealed %q, want nothing to seal", sealed)
	}
}

func TestRefusesWhatIsNotACode(t *testing.T) {
	good, err := New("8FJ3K2QP", "Other")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	key := base64.StdEncoding.EncodeToString(good.Key)
	for _, c := range []struct{ name, code string }{
		{"empty", ""},
		{"too few parts", "0:8FJ3K2QP:" + key},
		{"too many parts", "0:8FJ3K2QP:" + key + ":web-mail:extra"},
		{"unknown version", "9:8FJ3K2QP:" + key + ":web-mail"},
		{"version is not a number", "v0:8FJ3K2QP:" + key + ":web-mail"},
		{"no user code", "0::" + key + ":web-mail"},
		{"no client", "0:8FJ3K2QP:" + key + ":"},
		{"key is not base64", "0:8FJ3K2QP:not base64!:web-mail"},
		{"key is the wrong length", "0:8FJ3K2QP:" + base64.StdEncoding.EncodeToString([]byte("short")) + ":web-mail"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if _, err := Parse(c.code); err == nil {
				t.Errorf("Parse(%q) was accepted", c.code)
			}
		})
	}
}
