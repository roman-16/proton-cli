package drive

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
)

// This file holds the node-key crypto for Drive: passphrase unlocking, name
// (de/re-)encryption, hash-key derivation and new-node key generation. Kept
// separate from the API surface in drive.go / items.go / trash.go.

func unlockNode(l *Link, parentKR, addrKR *pgp.KeyRing) (*pgp.KeyRing, error) {
	enc, err := pgp.NewPGPMessageFromArmored(l.NodePassphrase)
	if err != nil {
		return nil, err
	}
	dec, err := parentKR.Decrypt(enc, nil, pgp.GetUnixTime())
	if err != nil {
		return nil, fmt.Errorf("decrypt node passphrase: %w", err)
	}
	if l.NodePassphraseSignature != "" && addrKR != nil {
		if sig, err := pgp.NewPGPSignatureFromArmored(l.NodePassphraseSignature); err == nil {
			_ = addrKR.VerifyDetached(dec, sig, pgp.GetUnixTime())
		}
	}
	locked, err := pgp.NewKeyFromArmored(l.NodeKey)
	if err != nil {
		return nil, err
	}
	unlocked, err := locked.Unlock(dec.GetBinary())
	if err != nil {
		return nil, fmt.Errorf("unlock node key: %w", err)
	}
	return pgp.NewKeyRing(unlocked)
}

func decryptName(encName string, parentKR *pgp.KeyRing) (string, error) {
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

func encryptName(name string, parentKR, addrKR *pgp.KeyRing) (string, error) {
	pub, err := parentKR.GetKey(0)
	if err != nil {
		return "", err
	}
	pubKR, err := pgp.NewKeyRing(pub)
	if err != nil {
		return "", err
	}
	enc, err := pubKR.Encrypt(pgp.NewPlainMessageFromString(name), addrKR)
	if err != nil {
		return "", err
	}
	return enc.GetArmored()
}

func reEncryptName(encryptedName, plainName string, oldKR, newKR, addrKR *pgp.KeyRing) (string, error) {
	msg, err := pgp.NewPGPMessageFromArmored(encryptedName)
	if err != nil {
		return "", err
	}
	split, err := msg.SplitMessage()
	if err != nil {
		return "", err
	}
	sk, err := oldKR.DecryptSessionKey(split.GetBinaryKeyPacket())
	if err != nil {
		return "", err
	}
	newKP, err := newKR.EncryptSessionKey(sk)
	if err != nil {
		return "", err
	}
	dataPacket, err := sk.EncryptAndSign(pgp.NewPlainMessageFromString(plainName), addrKR)
	if err != nil {
		return "", err
	}
	return pgp.NewPGPSplitMessage(newKP, dataPacket).GetPGPMessage().GetArmored()
}

func reEncryptNodePassphrase(l *Link, oldKR, newKR, addrKR *pgp.KeyRing) (string, string, error) {
	enc, err := pgp.NewPGPMessageFromArmored(l.NodePassphrase)
	if err != nil {
		return "", "", err
	}
	split, err := enc.SplitMessage()
	if err != nil {
		return "", "", err
	}
	sk, err := oldKR.DecryptSessionKey(split.GetBinaryKeyPacket())
	if err != nil {
		return "", "", err
	}
	dec, err := oldKR.Decrypt(enc, nil, pgp.GetUnixTime())
	if err != nil {
		return "", "", err
	}
	newKP, err := newKR.EncryptSessionKey(sk)
	if err != nil {
		return "", "", err
	}
	dataPacket, err := sk.Encrypt(dec)
	if err != nil {
		return "", "", err
	}
	newPass, err := pgp.NewPGPSplitMessage(newKP, dataPacket).GetPGPMessage().GetArmored()
	if err != nil {
		return "", "", err
	}
	sig, err := addrKR.SignDetached(dec)
	if err != nil {
		return "", "", err
	}
	newSig, err := sig.GetArmored()
	if err != nil {
		return "", "", err
	}
	return newPass, newSig, nil
}

func hashKeyOf(l *Link, nodeKR *pgp.KeyRing) ([]byte, error) {
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

func lookupHash(name string, hashKey []byte) (string, error) {
	mac := hmac.New(sha256.New, hashKey)
	mac.Write([]byte(name))
	return hex.EncodeToString(mac.Sum(nil)), nil
}

func genNodeKeys(parentKR, addrKR *pgp.KeyRing) (nodeKey, passphrase, passSig string, priv *pgp.Key, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", "", nil, err
	}
	phrase := base64.StdEncoding.EncodeToString(raw)
	key, err := pgp.GenerateKey("Drive key", "", "x25519", 0)
	if err != nil {
		return "", "", "", nil, err
	}
	locked, err := key.Lock([]byte(phrase))
	if err != nil {
		return "", "", "", nil, err
	}
	armKey, err := locked.Armor()
	if err != nil {
		return "", "", "", nil, err
	}
	msg := pgp.NewPlainMessageFromString(phrase)
	enc, err := parentKR.Encrypt(msg, nil)
	if err != nil {
		return "", "", "", nil, err
	}
	armPass, err := enc.GetArmored()
	if err != nil {
		return "", "", "", nil, err
	}
	sig, err := addrKR.SignDetached(msg)
	if err != nil {
		return "", "", "", nil, err
	}
	armSig, err := sig.GetArmored()
	if err != nil {
		return "", "", "", nil, err
	}
	return armKey, armPass, armSig, key, nil
}

// genShareKeys differs from genNodeKeys: it encrypts the passphrase to a
// combined key ring (node key first, then address key) and returns the
// passphrase session key, which the link and invite flows wrap for recipients.
func genShareKeys(nodeKR, addrKR *pgp.KeyRing) (nodeKey, passphrase, passSig string, priv *pgp.Key, sessionKey *pgp.SessionKey, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", "", nil, nil, err
	}
	phrase := base64.StdEncoding.EncodeToString(raw)
	key, err := pgp.GenerateKey("Drive key", "", "x25519", 0)
	if err != nil {
		return "", "", "", nil, nil, err
	}
	locked, err := key.Lock([]byte(phrase))
	if err != nil {
		return "", "", "", nil, nil, err
	}
	armKey, err := locked.Armor()
	if err != nil {
		return "", "", "", nil, nil, err
	}
	combined, err := combinedKeyRing(nodeKR, addrKR)
	if err != nil {
		return "", "", "", nil, nil, err
	}
	msg := pgp.NewPlainMessageFromString(phrase)
	enc, err := combined.Encrypt(msg, nil)
	if err != nil {
		return "", "", "", nil, nil, err
	}
	armPass, err := enc.GetArmored()
	if err != nil {
		return "", "", "", nil, nil, err
	}
	split, err := enc.SplitMessage()
	if err != nil {
		return "", "", "", nil, nil, err
	}
	sk, err := nodeKR.DecryptSessionKey(split.GetBinaryKeyPacket())
	if err != nil {
		return "", "", "", nil, nil, err
	}
	sig, err := addrKR.SignDetached(msg)
	if err != nil {
		return "", "", "", nil, nil, err
	}
	armSig, err := sig.GetArmored()
	if err != nil {
		return "", "", "", nil, nil, err
	}
	return armKey, armPass, armSig, key, sk, nil
}

// combinedKeyRing holds the node key followed by the address key; the order is
// significant (node key must come first).
func combinedKeyRing(nodeKR, addrKR *pgp.KeyRing) (*pgp.KeyRing, error) {
	nk, err := nodeKR.GetKey(0)
	if err != nil {
		return nil, err
	}
	combined, err := pgp.NewKeyRing(nk)
	if err != nil {
		return nil, err
	}
	ak, err := addrKR.GetKey(0)
	if err != nil {
		return nil, err
	}
	if err := combined.AddKey(ak); err != nil {
		return nil, err
	}
	return combined, nil
}

func reEncryptSessionKeyTo(armored string, oldKR, newKR *pgp.KeyRing) (string, error) {
	msg, err := pgp.NewPGPMessageFromArmored(armored)
	if err != nil {
		return "", err
	}
	split, err := msg.SplitMessage()
	if err != nil {
		return "", err
	}
	sk, err := oldKR.DecryptSessionKey(split.GetBinaryKeyPacket())
	if err != nil {
		return "", err
	}
	kp, err := newKR.EncryptSessionKey(sk)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(kp), nil
}

type xAttr struct {
	Common struct {
		ModificationTime string
		Size             int64
		Digests          struct{ SHA1 string }
	}
}

func decryptXAttr(armored string, nodeKR *pgp.KeyRing) (*xAttr, error) {
	msg, err := pgp.NewPGPMessageFromArmored(armored)
	if err != nil {
		return nil, err
	}
	dec, err := nodeKR.Decrypt(msg, nil, pgp.GetUnixTime())
	if err != nil {
		return nil, err
	}
	var x xAttr
	if err := json.Unmarshal(dec.GetBinary(), &x); err != nil {
		return nil, err
	}
	return &x, nil
}

func genNodeHashKey(nodeKR, signingKR *pgp.KeyRing) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	s := base64.StdEncoding.EncodeToString(raw)
	enc, err := nodeKR.Encrypt(pgp.NewPlainMessageFromString(s), signingKR)
	if err != nil {
		return "", err
	}
	return enc.GetArmored()
}
