package crypto

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/client"
	pb "github.com/roman-16/proton-cli/internal/proto"
	"google.golang.org/protobuf/proto"
)

const (
	aesKeyLen = 32
	aesIVLen  = 12

	tagItemContent  = "itemcontent"
	tagItemKey      = "itemkey"
	tagVaultContent = "vaultcontent"
)

// Content type aliases for use in cmd package.
type (
	ContentLogin      = pb.Content_Login
	ContentNote       = pb.Content_Note
	ContentAlias      = pb.Content_Alias
	ContentCreditCard = pb.Content_CreditCard
	ContentIdentity   = pb.Content_Identity
	ContentSshKey     = pb.Content_SshKey
	ContentWifi       = pb.Content_Wifi
	ContentCustom     = pb.Content_Custom
)

// PassShareKey holds a decrypted vault share key.
type PassShareKey struct {
	Raw      []byte
	Rotation int
}

// PassItem holds a decrypted Pass item.
type PassItem struct {
	ItemID   string
	Revision int
	State    int
	ShareID  string

	Metadata *pb.Metadata
	Content  *pb.Content
	Item     *pb.Item
}

// PassVault holds a decrypted vault.
type PassVault struct {
	ShareID   string
	VaultID   string
	Owner     bool
	Shared    bool
	Members   int
	AddressID string

	Vault *pb.Vault
}

// GetPassShares fetches all Pass shares (vaults).
func GetPassShares(ctx context.Context, c *client.Client) ([]json.RawMessage, error) {
	body, _, err := c.Do(ctx, "GET", "/pass/v1/share", nil, "", "", "")
	if err != nil {
		return nil, err
	}
	var res struct {
		Shares []json.RawMessage
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, err
	}
	return res.Shares, nil
}

// GetPassShareKeys fetches keys for a share.
func GetPassShareKeys(ctx context.Context, c *client.Client, shareID string) ([]json.RawMessage, error) {
	body, _, err := c.Do(ctx, "GET", "/pass/v1/share/"+shareID+"/key",
		map[string]string{"Page": "0"}, "", "", "")
	if err != nil {
		return nil, err
	}
	var res struct {
		ShareKeys struct {
			Keys []json.RawMessage
		}
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, err
	}
	return res.ShareKeys.Keys, nil
}

// OpenShareKey decrypts a share key using the user's PGP key ring.
func OpenShareKey(encryptedKey string, userKR *pgp.KeyRing) ([]byte, error) {
	keyBytes, err := base64.StdEncoding.DecodeString(encryptedKey)
	if err != nil {
		return nil, fmt.Errorf("failed to decode share key: %w", err)
	}

	msg := pgp.NewPGPMessage(keyBytes)
	dec, err := userKR.Decrypt(msg, userKR, pgp.GetUnixTime())
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt share key: %w", err)
	}

	return dec.GetBinary(), nil
}

// DecryptVaultContent decrypts vault protobuf content using a share key.
func DecryptVaultContent(encryptedContent string, shareKey []byte) (*pb.Vault, error) {
	data, err := base64.StdEncoding.DecodeString(encryptedContent)
	if err != nil {
		return nil, fmt.Errorf("failed to decode vault content: %w", err)
	}

	plaintext, err := aesGCMDecrypt(shareKey, data, []byte(tagVaultContent))
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt vault content: %w", err)
	}

	var vault pb.Vault
	if err := proto.Unmarshal(plaintext, &vault); err != nil {
		return nil, fmt.Errorf("failed to parse vault protobuf: %w", err)
	}

	return &vault, nil
}

// DecryptItemContent decrypts an item's content using the item key.
func DecryptItemContent(encryptedContent string, itemKey []byte) (*pb.Item, error) {
	data, err := base64.StdEncoding.DecodeString(encryptedContent)
	if err != nil {
		return nil, fmt.Errorf("failed to decode item content: %w", err)
	}

	plaintext, err := aesGCMDecrypt(itemKey, data, []byte(tagItemContent))
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt item content: %w", err)
	}

	var item pb.Item
	if err := proto.Unmarshal(plaintext, &item); err != nil {
		return nil, fmt.Errorf("failed to parse item protobuf: %w", err)
	}

	return &item, nil
}

// DecryptItemKey decrypts an item key using the vault share key.
func DecryptItemKey(encryptedItemKey string, shareKey []byte) ([]byte, error) {
	data, err := base64.StdEncoding.DecodeString(encryptedItemKey)
	if err != nil {
		return nil, fmt.Errorf("failed to decode item key: %w", err)
	}

	return aesGCMDecrypt(shareKey, data, []byte(tagItemKey))
}

// EncryptVaultContent encrypts vault protobuf content with a share key.
func EncryptVaultContent(vault *pb.Vault, shareKey []byte) (string, error) {
	plaintext, err := proto.Marshal(vault)
	if err != nil {
		return "", fmt.Errorf("failed to marshal vault protobuf: %w", err)
	}

	encrypted, err := aesGCMEncrypt(shareKey, plaintext, []byte(tagVaultContent))
	if err != nil {
		return "", fmt.Errorf("failed to encrypt vault content: %w", err)
	}

	return base64.StdEncoding.EncodeToString(encrypted), nil
}

// EncryptItemContent encrypts item protobuf content with an item key.
func EncryptItemContent(item *pb.Item, itemKey []byte) (string, error) {
	plaintext, err := proto.Marshal(item)
	if err != nil {
		return "", fmt.Errorf("failed to marshal item protobuf: %w", err)
	}

	encrypted, err := aesGCMEncrypt(itemKey, plaintext, []byte(tagItemContent))
	if err != nil {
		return "", fmt.Errorf("failed to encrypt item content: %w", err)
	}

	return base64.StdEncoding.EncodeToString(encrypted), nil
}

// EncryptItemKey encrypts an item key with the vault share key.
func EncryptItemKey(itemKey, shareKey []byte) (string, error) {
	encrypted, err := aesGCMEncrypt(shareKey, itemKey, []byte(tagItemKey))
	if err != nil {
		return "", fmt.Errorf("failed to encrypt item key: %w", err)
	}
	return base64.StdEncoding.EncodeToString(encrypted), nil
}

// GenerateItemKey generates a random 32-byte AES key.
func GenerateItemKey() ([]byte, error) {
	key := make([]byte, aesKeyLen)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	return key, nil
}

// CreateVaultKeys generates a new vault key and encrypts it with the user's PGP key.
func CreateVaultKeys(userKR *pgp.KeyRing) (encryptedVaultKey string, rawKey []byte, err error) {
	rawKey = make([]byte, aesKeyLen)
	if _, err := rand.Read(rawKey); err != nil {
		return "", nil, err
	}

	msg := pgp.NewPlainMessage(rawKey)
	encrypted, err := userKR.Encrypt(msg, userKR)
	if err != nil {
		return "", nil, fmt.Errorf("failed to encrypt vault key: %w", err)
	}

	encBytes := encrypted.GetBinary()
	return base64.StdEncoding.EncodeToString(encBytes), rawKey, nil
}

// aesGCMDecrypt decrypts data using AES-256-GCM.
// Format: [12-byte IV | ciphertext+tag]
// additionalData is used as AAD (authenticated additional data).
func aesGCMDecrypt(key, data, additionalData []byte) ([]byte, error) {
	if len(key) != aesKeyLen {
		return nil, fmt.Errorf("invalid key length: %d (expected %d)", len(key), aesKeyLen)
	}
	if len(data) < aesIVLen {
		return nil, fmt.Errorf("ciphertext too short")
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	iv := data[:aesIVLen]
	ciphertext := data[aesIVLen:]

	return gcm.Open(nil, iv, ciphertext, additionalData)
}

// aesGCMEncrypt encrypts data using AES-256-GCM.
// Returns: [12-byte IV | ciphertext+tag]
func aesGCMEncrypt(key, plaintext, additionalData []byte) ([]byte, error) {
	if len(key) != aesKeyLen {
		return nil, fmt.Errorf("invalid key length: %d (expected %d)", len(key), aesKeyLen)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	iv := make([]byte, aesIVLen)
	if _, err := rand.Read(iv); err != nil {
		return nil, err
	}

	ciphertext := gcm.Seal(nil, iv, plaintext, additionalData)
	result := make([]byte, aesIVLen+len(ciphertext))
	copy(result, iv)
	copy(result[aesIVLen:], ciphertext)

	return result, nil
}
