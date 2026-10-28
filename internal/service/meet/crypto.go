package meet

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"

	srp "github.com/ProtonMail/go-srp"
	"github.com/ProtonMail/gopenpgp/v3/constants"
	pgp "github.com/ProtonMail/gopenpgp/v3/crypto"
	pgphelper "github.com/roman-16/proton-cli/internal/crypto/pgp"
)

// The values Proton's Meet web app seals a meeting with, in
// packages/meet/utils/cryptoUtils.ts.
const (
	passwordLength      = 12
	passwordAlphabet    = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	saltLength          = 16
	sessionKeyAlgorithm = constants.AES256
	titleKeyInfo        = "aeskey.link.meet.proton"
	titleData           = "metadata.meet.proton"
	passwordContext     = "pw.link.meet.proton"
	titleIVLength       = 12
)

type secret struct {
	password   string
	salt       []byte
	sessionKey *pgp.SessionKey
}

func newSecret() (secret, error) {
	password := make([]byte, passwordLength)
	for i := range password {
		n, err := randIndex(len(passwordAlphabet))
		if err != nil {
			return secret{}, fmt.Errorf("draw the meeting's password: %w", err)
		}
		password[i] = passwordAlphabet[n]
	}
	salt := make([]byte, saltLength)
	if _, err := rand.Read(salt); err != nil {
		return secret{}, fmt.Errorf("draw the meeting's salt: %w", err)
	}
	sk, err := pgp.GenerateSessionKeyAlgo(sessionKeyAlgorithm)
	if err != nil {
		return secret{}, fmt.Errorf("make the meeting's session key: %w", err)
	}
	return secret{password: string(password), salt: salt, sessionKey: sk}, nil
}

func randIndex(n int) (int, error) {
	limit := 256 - 256%n
	var b [1]byte
	for {
		if _, err := rand.Read(b[:]); err != nil {
			return 0, err
		}
		if int(b[0]) < limit {
			return int(b[0]) % n, nil
		}
	}
}

func (s secret) lockedSessionKey() ([]byte, error) {
	kp, err := keyPassword(s.password, s.salt)
	if err != nil {
		return nil, err
	}
	locked, err := pgphelper.EncryptSessionKeyWithPassword(kp, s.sessionKey)
	if err != nil {
		return nil, fmt.Errorf("lock the meeting's session key: %w", err)
	}
	return locked, nil
}

// keyPassword is computeKeyPassword from Proton's SRP library: the bcrypt hash
// without its 29-character prefix of version, cost and salt.
func keyPassword(password string, salt []byte) ([]byte, error) {
	if password == "" || len(salt) != saltLength {
		return nil, errors.New("hash the meeting's password: a password and a 16-byte salt are required")
	}
	hashed, err := srp.MailboxPassword([]byte(password), salt)
	if err != nil {
		return nil, fmt.Errorf("hash the meeting's password: %w", err)
	}
	return hashed[len(hashed)-31:], nil
}

func unlockSessionKey(locked []byte, password string, salt []byte) (*pgp.SessionKey, error) {
	kp, err := keyPassword(password, salt)
	if err != nil {
		return nil, err
	}
	sk, err := pgphelper.DecryptSessionKeyWithPassword(kp, locked)
	if err != nil {
		return nil, fmt.Errorf("unlock the meeting's session key: %w", err)
	}
	return sk, nil
}

func titleCipher(sk *pgp.SessionKey) (cipher.AEAD, error) {
	if sk == nil || sk.Algo != sessionKeyAlgorithm {
		return nil, errors.New("the meeting's session key is not an AES-256 key")
	}
	key, err := hkdf.Key(sha256.New, sk.Key, make([]byte, 32), titleKeyInfo, 32)
	if err != nil {
		return nil, fmt.Errorf("derive the meeting's title key: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func sealTitle(sk *pgp.SessionKey, title string) (string, error) {
	aead, err := titleCipher(sk)
	if err != nil {
		return "", err
	}
	iv := make([]byte, titleIVLength)
	if _, err := rand.Read(iv); err != nil {
		return "", fmt.Errorf("seal the meeting's title: %w", err)
	}
	return base64.StdEncoding.EncodeToString(aead.Seal(iv, iv, []byte(title), []byte(titleData))), nil
}

func openTitle(sk *pgp.SessionKey, sealed string) (string, error) {
	aead, err := titleCipher(sk)
	if err != nil {
		return "", err
	}
	raw, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil {
		return "", fmt.Errorf("read the meeting's title: %w", err)
	}
	if len(raw) < titleIVLength {
		return "", errors.New("read the meeting's title: it is too short to hold one")
	}
	title, err := aead.Open(nil, raw[:titleIVLength], raw[titleIVLength:], []byte(titleData))
	if err != nil {
		return "", fmt.Errorf("open the meeting's title: %w", err)
	}
	return string(title), nil
}

func sealPassword(userKR *pgp.KeyRing, password string) (string, error) {
	enc, err := pgphelper.PGP.Encryption().Recipients(userKR).SigningKeys(userKR).
		SigningContext(pgp.NewSigningContext(passwordContext, true)).Utf8().New()
	if err != nil {
		return "", fmt.Errorf("seal the meeting's password: %w", err)
	}
	msg, err := enc.Encrypt([]byte(password))
	if err != nil {
		return "", fmt.Errorf("seal the meeting's password: %w", err)
	}
	return msg.Armor()
}

func openPassword(userKR *pgp.KeyRing, armored string) (string, error) {
	dec, err := pgphelper.PGP.Decryption().DecryptionKeys(userKR).VerificationKeys(userKR).
		VerificationContext(pgp.NewVerificationContext(passwordContext, true, 0)).Utf8().New()
	if err != nil {
		return "", fmt.Errorf("open the meeting's password: %w", err)
	}
	res, err := dec.Decrypt([]byte(armored), pgp.Armor)
	if err != nil {
		return "", fmt.Errorf("open the meeting's password: %w", err)
	}
	if err := pgphelper.SignatureError(res, userKR); err != nil {
		return "", fmt.Errorf("verify the meeting's password: %w", err)
	}
	return res.String(), nil
}
