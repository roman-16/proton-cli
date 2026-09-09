package drive

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	pgphelper "github.com/roman-16/proton-cli/internal/crypto/pgp"
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
		// Normalise to a text message: the passphrase is signed as text
		// (NewPlainMessageFromString) at creation, so verifying the binary form
		// would spuriously fail. The surfaced verdict lives on `info`; here we
		// only log non-verification at debug level (shared nodes are signed by a
		// key we may not hold, which detached verify can't tell from tampering).
		norm := pgp.NewPlainMessageFromString(string(dec.GetBinary()))
		if v := pgphelper.VerifyDetachedStatus(addrKR, norm, l.NodePassphraseSignature); v != pgphelper.Verified {
			slog.Debug("drive: node passphrase signature not verified", "link", l.LinkID, "signer", l.SignatureEmail, "result", string(v))
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
	// Folders carry the hash key in FolderProperties; photo albums carry it in
	// AlbumProperties.
	armored := ""
	if l.AlbumProperties != nil && l.AlbumProperties.NodeHashKey != "" {
		armored = l.AlbumProperties.NodeHashKey
	} else if l.FolderProperties != nil && l.FolderProperties.NodeHashKey != "" {
		armored = l.FolderProperties.NodeHashKey
	}
	if armored == "" {
		return nil, fmt.Errorf("link has no hash key")
	}
	msg, err := pgp.NewPGPMessageFromArmored(armored)
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

// xAttr is a file's own record of itself, sealed to its node key: what the bytes
// were before they were encrypted and cut into blocks.
//
// Proton cannot read it and never fills it in, so it is what one client tells
// the next - and what a revision has to carry for the endpoints a public link is
// served under to accept it.
type xAttr struct {
	Common struct {
		// ModificationTime is when the file was last changed where it came from, in
		// the form every Proton client writes it. A stream came from nowhere and has
		// none.
		ModificationTime string `json:",omitempty"`
		Size             int64
		BlockSizes       []int
		Digests          struct{ SHA1 string }
	}
}

// xAttrTime is the shape a modification time is written in: what JavaScript's
// toISOString produces, which is what every other Proton client reads.
const xAttrTime = "2006-01-02T15:04:05.000Z07:00"

// encryptXAttr seals a file's record of itself to its node key, signed by
// whoever wrote the file.
func encryptXAttr(x xAttr, nodeKR, signKR *pgp.KeyRing) (string, error) {
	raw, err := json.Marshal(x)
	if err != nil {
		return "", err
	}
	enc, err := nodeKR.Encrypt(pgp.NewPlainMessageFromString(string(raw)), signKR)
	if err != nil {
		return "", err
	}
	return enc.GetArmored()
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

// genNodeHashKey makes the key a folder's children are named under, returning
// it both ways: as itself, for hashing the names of children written in the
// same breath, and sealed to the folder, which is how Proton is told about it.
func genNodeHashKey(nodeKR, signingKR *pgp.KeyRing) ([]byte, string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, "", err
	}
	hashKey := base64.StdEncoding.EncodeToString(raw)
	enc, err := nodeKR.Encrypt(pgp.NewPlainMessageFromString(hashKey), signingKR)
	if err != nil {
		return nil, "", err
	}
	armored, err := enc.GetArmored()
	if err != nil {
		return nil, "", err
	}
	return []byte(hashKey), armored, nil
}
