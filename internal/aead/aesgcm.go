// Package aead wraps AES-256-GCM for Proton Pass items, vault keys and similar
// symmetric blobs. Everything is pure crypto — no I/O, no API calls.
package aead

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
)

// Proton Pass AAD tags used for AES-GCM domain separation.
const (
	TagItemContent  = "itemcontent"
	TagItemKey      = "itemkey"
	TagVaultContent = "vaultcontent"
)

const (
	KeyLen = 32
	IVLen  = 12
)

// Decrypt expects [12-byte IV | ciphertext+tag].
func Decrypt(key, data, aad []byte) ([]byte, error) {
	if len(key) != KeyLen {
		return nil, fmt.Errorf("aead: invalid key length %d", len(key))
	}
	if len(data) < IVLen {
		return nil, fmt.Errorf("aead: ciphertext too short")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return gcm.Open(nil, data[:IVLen], data[IVLen:], aad)
}

// Encrypt returns [12-byte IV | ciphertext+tag].
func Encrypt(key, plaintext, aad []byte) ([]byte, error) {
	if len(key) != KeyLen {
		return nil, fmt.Errorf("aead: invalid key length %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	iv := make([]byte, IVLen)
	if _, err := rand.Read(iv); err != nil {
		return nil, err
	}
	ct := gcm.Seal(nil, iv, plaintext, aad)
	out := make([]byte, IVLen+len(ct))
	copy(out, iv)
	copy(out[IVLen:], ct)
	return out, nil
}

// NewKey returns a random 32-byte AES key.
func NewKey() ([]byte, error) {
	k := make([]byte, KeyLen)
	if _, err := rand.Read(k); err != nil {
		return nil, err
	}
	return k, nil
}
