// Package aead wraps AES-256-GCM for Proton Pass items, vault keys and similar
// symmetric blobs. Everything is pure crypto - no I/O, no API calls.
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
	// TagLinkKey seals the key that opens a secure link. It is the key that ends
	// up in the URL, so it is sealed under a tag of its own.
	TagLinkKey = "linkkey"
	// TagFileKey seals the key of one attached file under the key of the item it
	// hangs on.
	TagFileKey = "filekey"
	// TagFileData covers a whole file written at encryption version 1: its
	// metadata and every chunk of its contents share the one tag.
	TagFileData = "filedata"
	// TagFileMetadata is a version-2 file's name and type.
	TagFileMetadata = "v2;filemetadata.item.pass.proton"
)

// TagFileChunk is the tag over one chunk of a version-2 file.
//
// Where version 1 tags every chunk alike, version 2 writes the chunk's place in
// the file into the tag, so a chunk cannot be opened as though it were another
// one or as though the file were a different length.
func TagFileChunk(index, total int) string {
	return fmt.Sprintf("v2;%d;%d;filedata.item.pass.proton", index, total)
}

const (
	KeyLen = 32
	IVLen  = 12
	// TagLen is the authentication tag GCM puts at the end.
	TagLen = 16
	// Overhead is how much longer a sealed blob is than what is in it, which is
	// what tells the size of a file from the size of the pieces it is stored as.
	Overhead = IVLen + TagLen
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

func NewKey() ([]byte, error) {
	k := make([]byte, KeyLen)
	if _, err := rand.Read(k); err != nil {
		return nil, err
	}
	return k, nil
}
