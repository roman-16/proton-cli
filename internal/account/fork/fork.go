// Package fork carries a sign-in from a device that holds one to a device that
// does not: the code a person moves between them, and the key password the code
// seals.
//
// The format is Proton's own (packages/account/signInWithAnotherDevice/ in
// WebClients): a colon-separated code carrying a one-time key, and a payload
// sealed under that key with AES-256-GCM at Proton's payload version 1, whose
// nonce is sixteen bytes rather than the usual twelve.
package fork

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/roman-16/proton-cli/internal/crypto/aead"
	"github.com/roman-16/proton-cli/internal/errs"
)

// version is the shape of the code, and the only one Proton's clients write.
const version = 0

// nonceLen is how long the nonce of a fork payload is. Proton's session blobs
// predate the standard twelve bytes, and the format is the other device's to
// choose, not ours.
const nonceLen = 16

// Code is what a person moves from the device being signed in to the device that
// approves it.
//
// The key is the point of it. It is made by the device that is waiting, travels
// no further than the screen it is shown on, and is what the approving device
// seals the key password to - so what passes through Proton is readable by the
// one machine the person was looking at.
type Code struct {
	UserCode string
	Key      []byte
	ClientID string
}

// New opens a code for a fork, with a key nothing else has held.
//
// clientID is how the new session names itself, which is what Proton lists it
// under afterwards.
func New(userCode, clientID string) (Code, error) {
	key, err := aead.NewKey()
	if err != nil {
		return Code{}, fmt.Errorf("fork key: %w", err)
	}
	return Code{UserCode: userCode, Key: key, ClientID: clientID}, nil
}

// Parse reads a code off the other device's screen.
//
// A code with no key is one Proton's own apps write when they expect to hand
// over no password; what it signs in is a session whose keys stay shut.
func Parse(s string) (Code, error) {
	refuse := errs.Problemf("That is not a sign-in code.").
		Hint("run `proton account login --qr` on the other device and copy the line it prints")

	parts := strings.Split(strings.TrimSpace(s), ":")
	if len(parts) != 4 {
		return Code{}, refuse
	}
	if v, err := strconv.Atoi(parts[0]); err != nil || v != version {
		return Code{}, refuse
	}
	if parts[1] == "" || parts[3] == "" {
		return Code{}, refuse
	}
	var key []byte
	if parts[2] != "" {
		decoded, err := base64.StdEncoding.DecodeString(parts[2])
		if err != nil || len(decoded) != aead.KeyLen {
			return Code{}, refuse
		}
		key = decoded
	}
	return Code{UserCode: parts[1], Key: key, ClientID: parts[3]}, nil
}

// String is the code as the other device receives it.
func (c Code) String() string {
	key := ""
	if len(c.Key) > 0 {
		key = base64.StdEncoding.EncodeToString(c.Key)
	}
	return strings.Join([]string{strconv.Itoa(version), c.UserCode, key, c.ClientID}, ":")
}

// payload is what the approving device seals: the passphrase the account's keys
// are locked with, as Proton's clients write it.
type payload struct {
	Type        string `json:"type"`
	KeyPassword string `json:"keyPassword"`
}

// Seal encrypts the key password for whoever holds this code. A code with no key
// seals nothing, which is what signs a device in without unlocking it.
func (c Code) Seal(keyPassword string) (string, error) {
	if len(c.Key) == 0 {
		return "", nil
	}
	plain, err := json.Marshal(payload{Type: "default", KeyPassword: keyPassword})
	if err != nil {
		return "", err
	}
	gcm, err := sealer(c.Key)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, nonceLen)
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("fork nonce: %w", err)
	}
	return base64.StdEncoding.EncodeToString(gcm.Seal(nonce, nonce, plain, nil)), nil
}

// Open recovers the key password from what the approving device sealed. An empty
// payload carries none, and the session it belongs to stays locked.
func (c Code) Open(sealed string) (string, error) {
	if sealed == "" {
		return "", nil
	}
	raw, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil {
		return "", fmt.Errorf("decode fork payload: %w", err)
	}
	gcm, err := sealer(c.Key)
	if err != nil {
		return "", err
	}
	if len(raw) < nonceLen {
		return "", fmt.Errorf("fork payload is too short to hold a nonce")
	}
	plain, err := gcm.Open(nil, raw[:nonceLen], raw[nonceLen:], nil)
	if err != nil {
		return "", fmt.Errorf("open fork payload: %w", err)
	}
	var p payload
	if err := json.Unmarshal(plain, &p); err != nil {
		return "", fmt.Errorf("read fork payload: %w", err)
	}
	return p.KeyPassword, nil
}

func sealer(key []byte) (cipher.AEAD, error) {
	if len(key) != aead.KeyLen {
		return nil, fmt.Errorf("fork key is %d bytes, want %d", len(key), aead.KeyLen)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCMWithNonceSize(block, nonceLen)
}
