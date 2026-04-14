package crypto

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/client"
)

// DriveKeys holds an unlocked share and provides node key decryption.
type DriveKeys struct {
	ShareKR   *pgp.KeyRing
	AddrKR    *pgp.KeyRing
	AddrEmail string
	AddrID    string
}

type share struct {
	ShareID             string
	AddressID           string
	Key                 string
	Passphrase          string
	PassphraseSignature string
}

type link struct {
	LinkID                  string
	ParentLinkID            string
	Type                    int
	Name                    string
	NodeKey                 string
	NodePassphrase          string
	NodePassphraseSignature string
	SignatureEmail          string
	FolderProperties        *folderProperties
}

type folderProperties struct {
	NodeHashKey string
}

// UnlockShare fetches a share and unlocks its key ring using the address key ring.
func UnlockShare(ctx context.Context, c *client.Client, shareID string, kr *KeyRings) (*DriveKeys, error) {
	s, err := getShare(ctx, c, shareID)
	if err != nil {
		return nil, err
	}

	addrKR, ok := kr.AddrKRs[s.AddressID]
	if !ok {
		return nil, fmt.Errorf("no key ring for address %s", s.AddressID)
	}

	// Find the share's address email
	var addrEmail string
	for _, addr := range kr.Addresses {
		if addr.ID == s.AddressID {
			addrEmail = addr.Email
			break
		}
	}

	// Decrypt share passphrase
	enc, err := pgp.NewPGPMessageFromArmored(s.Passphrase)
	if err != nil {
		return nil, err
	}

	dec, err := addrKR.Decrypt(enc, nil, pgp.GetUnixTime())
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt share passphrase: %w", err)
	}

	sig, err := pgp.NewPGPSignatureFromArmored(s.PassphraseSignature)
	if err != nil {
		return nil, err
	}

	if err := addrKR.VerifyDetached(dec, sig, pgp.GetUnixTime()); err != nil {
		return nil, fmt.Errorf("share passphrase signature invalid: %w", err)
	}

	// Unlock share key
	lockedKey, err := pgp.NewKeyFromArmored(s.Key)
	if err != nil {
		return nil, err
	}

	unlockedKey, err := lockedKey.Unlock(dec.GetBinary())
	if err != nil {
		return nil, fmt.Errorf("failed to unlock share key: %w", err)
	}

	shareKR, err := pgp.NewKeyRing(unlockedKey)
	if err != nil {
		return nil, err
	}

	return &DriveKeys{ShareKR: shareKR, AddrKR: addrKR, AddrEmail: addrEmail, AddrID: s.AddressID}, nil
}

// UnlockNode decrypts a link's node key using the parent key ring.
func UnlockNode(l *link, parentKR *pgp.KeyRing, addrKR *pgp.KeyRing) (*pgp.KeyRing, error) {
	enc, err := pgp.NewPGPMessageFromArmored(l.NodePassphrase)
	if err != nil {
		return nil, err
	}

	dec, err := parentKR.Decrypt(enc, nil, pgp.GetUnixTime())
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt node passphrase: %w", err)
	}

	if l.NodePassphraseSignature != "" {
		sig, err := pgp.NewPGPSignatureFromArmored(l.NodePassphraseSignature)
		if err == nil {
			addrKR.VerifyDetached(dec, sig, pgp.GetUnixTime()) // Non-fatal
		}
	}

	lockedKey, err := pgp.NewKeyFromArmored(l.NodeKey)
	if err != nil {
		return nil, err
	}

	unlockedKey, err := lockedKey.Unlock(dec.GetBinary())
	if err != nil {
		return nil, fmt.Errorf("failed to unlock node key: %w", err)
	}

	return pgp.NewKeyRing(unlockedKey)
}

// DecryptName decrypts a link's encrypted name using the parent node key ring.
func DecryptName(encName string, parentKR *pgp.KeyRing) (string, error) {
	msg, err := pgp.NewPGPMessageFromArmored(encName)
	if err != nil {
		return "", err
	}

	dec, err := parentKR.Decrypt(msg, nil, pgp.GetUnixTime())
	if err != nil {
		return "", err
	}

	return dec.GetString(), nil
}

// DecryptFolderChildren fetches children of a folder and decrypts their names.
func DecryptFolderChildren(ctx context.Context, c *client.Client, shareID, linkID string, parentKR *pgp.KeyRing, addrKR *pgp.KeyRing) ([]map[string]interface{}, error) {
	body, _, err := c.Do(ctx, "GET", fmt.Sprintf("/drive/shares/%s/folders/%s/children", shareID, linkID),
		map[string]string{"Page": "0", "PageSize": "150"}, "", "", "")
	if err != nil {
		return nil, err
	}

	var res struct {
		Links []json.RawMessage
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, err
	}

	var results []map[string]interface{}
	for _, raw := range res.Links {
		var l link
		if err := json.Unmarshal(raw, &l); err != nil {
			continue
		}

		name, err := DecryptName(l.Name, parentKR)
		if err != nil {
			name = "(decryption failed)"
		}

		var entry map[string]interface{}
		json.Unmarshal(raw, &entry)
		entry["DecryptedName"] = name
		results = append(results, entry)
	}

	return results, nil
}

func getShare(ctx context.Context, c *client.Client, shareID string) (*share, error) {
	body, _, err := c.Do(ctx, "GET", "/drive/shares/"+shareID, nil, "", "", "")
	if err != nil {
		return nil, err
	}
	// Response is flat (fields at root level, not nested under "Share")
	var s share
	if err := json.Unmarshal(body, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// GetLink fetches a single link.
func GetLink(ctx context.Context, c *client.Client, shareID, linkID string) (*link, error) {
	body, _, err := c.Do(ctx, "GET", fmt.Sprintf("/drive/shares/%s/links/%s", shareID, linkID), nil, "", "", "")
	if err != nil {
		return nil, err
	}
	var res struct {
		Link link
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, err
	}
	return &res.Link, nil
}

// EncryptName encrypts a file/folder name with the parent's public key.
func EncryptName(name string, parentKR *pgp.KeyRing, addrKR *pgp.KeyRing) (string, error) {
	pubKey, err := parentKR.GetKey(0)
	if err != nil {
		return "", err
	}
	pubKR, err := pgp.NewKeyRing(pubKey)
	if err != nil {
		return "", err
	}

	msg := pgp.NewPlainMessageFromString(name)
	encrypted, err := pubKR.Encrypt(msg, addrKR)
	if err != nil {
		return "", err
	}
	armored, err := encrypted.GetArmored()
	if err != nil {
		return "", err
	}
	return armored, nil
}

// GenerateLookupHash generates an HMAC-SHA256 hash for name collision detection.
func GenerateLookupHash(name string, hashKey []byte) (string, error) {
	mac := hmac.New(sha256.New, hashKey)
	mac.Write([]byte(name))
	return hex.EncodeToString(mac.Sum(nil)), nil
}

// GetHashKey decrypts a folder's NodeHashKey.
func GetHashKey(l *link, nodeKR *pgp.KeyRing) ([]byte, error) {
	if l.FolderProperties == nil || l.FolderProperties.NodeHashKey == "" {
		return nil, fmt.Errorf("link has no hash key")
	}

	msg, err := pgp.NewPGPMessageFromArmored(l.FolderProperties.NodeHashKey)
	if err != nil {
		return nil, err
	}

	dec, err := nodeKR.Decrypt(msg, nodeKR, pgp.GetUnixTime())
	if err != nil {
		return nil, err
	}

	return dec.GetBinary(), nil
}

// GenerateNodeKeys generates a new PGP key pair for a node (file or folder).
// Matches WebClients: encryptPassphrase(parentKey, addressKey) + generateDriveKey(passphrase)
func GenerateNodeKeys(parentKR *pgp.KeyRing, addrKR *pgp.KeyRing) (nodeKey string, nodePassphrase string, nodePassphraseSignature string, privateKey *pgp.Key, err error) {
	// Generate random passphrase
	rawPassphrase := make([]byte, 32)
	if _, err := rand.Read(rawPassphrase); err != nil {
		return "", "", "", nil, err
	}
	passphrase := base64.StdEncoding.EncodeToString(rawPassphrase)

	// Generate new key (matches generateDriveKey)
	key, err := pgp.GenerateKey("Drive key", "", "x25519", 0)
	if err != nil {
		return "", "", "", nil, err
	}

	// Lock key with passphrase
	lockedKey, err := key.Lock([]byte(passphrase))
	if err != nil {
		return "", "", "", nil, err
	}
	armoredKey, err := lockedKey.Armor()
	if err != nil {
		return "", "", "", nil, err
	}

	// Encrypt passphrase with parent key + sign with address key (detached)
	// This matches encryptPassphrase(parentKey, addressKey, rawPassphrase)
	passphraseMsg := pgp.NewPlainMessageFromString(passphrase)

	// Encrypt with parent key
	encPassphrase, err := parentKR.Encrypt(passphraseMsg, nil)
	if err != nil {
		return "", "", "", nil, err
	}
	armoredPassphrase, err := encPassphrase.GetArmored()
	if err != nil {
		return "", "", "", nil, err
	}

	// Sign with address key (detached signature)
	sig, err := addrKR.SignDetached(passphraseMsg)
	if err != nil {
		return "", "", "", nil, err
	}
	armoredSig, err := sig.GetArmored()
	if err != nil {
		return "", "", "", nil, err
	}

	return armoredKey, armoredPassphrase, armoredSig, key, nil
}

// GenerateNodeHashKey generates and encrypts a hash key for a folder node.
// Matches WebClients: generateNodeHashKey(privateKey, privateKey)
// The hash key is encrypted AND signed with the node's own key (not address key).
func GenerateNodeHashKey(nodeKR *pgp.KeyRing, signingKR *pgp.KeyRing) (string, error) {
	// Generate random hash key
	rawKey := make([]byte, 32)
	if _, err := rand.Read(rawKey); err != nil {
		return "", err
	}
	hashKeyStr := base64.StdEncoding.EncodeToString(rawKey)

	msg := pgp.NewPlainMessageFromString(hashKeyStr)
	encrypted, err := nodeKR.Encrypt(msg, signingKR)
	if err != nil {
		return "", err
	}
	armored, err := encrypted.GetArmored()
	if err != nil {
		return "", err
	}
	return armored, nil
}

// EncryptBlock encrypts a file block with the session key.
// Returns: encrypted block data, armored encrypted signature.
func EncryptBlock(data []byte, sessionKey *pgp.SessionKey, nodeKR *pgp.KeyRing, addrKR *pgp.KeyRing) ([]byte, string, error) {
	msg := pgp.NewPlainMessage(data)

	// Encrypt block with session key
	encrypted, err := sessionKey.Encrypt(msg)
	if err != nil {
		return nil, "", err
	}

	// Sign block (detached) with address key if available
	if addrKR != nil {
		sig, err := addrKR.SignDetached(msg)
		if err != nil {
			return nil, "", err
		}

		// Encrypt signature with node key (produces armored PGP message)
		sigMsg := pgp.NewPlainMessage(sig.GetBinary())
		encSig, err := nodeKR.Encrypt(sigMsg, nil)
		if err != nil {
			return nil, "", err
		}
		armoredEncSig, err := encSig.GetArmored()
		if err != nil {
			return nil, "", err
		}

		return encrypted, armoredEncSig, nil
	}

	return encrypted, "", nil
}

// DecryptBlock decrypts a file block.
func DecryptBlock(encData []byte, sessionKey *pgp.SessionKey) ([]byte, error) {
	dec, err := sessionKey.Decrypt(encData)
	if err != nil {
		return nil, err
	}
	return dec.GetBinary(), nil
}

// GetFileSessionKey decrypts a file's content session key.
func GetFileSessionKey(contentKeyPacket string, nodeKR *pgp.KeyRing) (*pgp.SessionKey, error) {
	kp, err := base64.StdEncoding.DecodeString(contentKeyPacket)
	if err != nil {
		return nil, err
	}
	return nodeKR.DecryptSessionKey(kp)
}

// GenerateFileKeys generates session key and content key packet for a new file.
func GenerateFileKeys(nodeKR *pgp.KeyRing, addrKR *pgp.KeyRing) (sessionKey *pgp.SessionKey, contentKeyPacket string, contentKeyPacketSignature string, err error) {
	// Generate session key
	sk, err := pgp.GenerateSessionKey()
	if err != nil {
		return nil, "", "", err
	}

	// Encrypt session key with node key
	kp, err := nodeKR.EncryptSessionKey(sk)
	if err != nil {
		return nil, "", "", err
	}
	contentKeyPacket = base64.StdEncoding.EncodeToString(kp)

	// Sign the session key
	keyMsg := pgp.NewPlainMessage(sk.Key)
	sig, err := nodeKR.SignDetached(keyMsg)
	if err != nil {
		return nil, "", "", err
	}
	armoredSig, err := sig.GetArmored()
	if err != nil {
		return nil, "", "", err
	}

	return sk, contentKeyPacket, armoredSig, nil
}
