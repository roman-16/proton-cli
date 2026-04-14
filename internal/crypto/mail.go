package crypto

import (
	"fmt"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
)

// DecryptMessageBody decrypts a PGP-encrypted mail message body.
func DecryptMessageBody(armoredBody string, addrKR *pgp.KeyRing) (string, error) {
	msg, err := pgp.NewPGPMessageFromArmored(armoredBody)
	if err != nil {
		return "", fmt.Errorf("failed to parse message: %w", err)
	}

	dec, err := addrKR.Decrypt(msg, nil, pgp.GetUnixTime())
	if err != nil {
		return "", fmt.Errorf("failed to decrypt message: %w", err)
	}

	return dec.GetString(), nil
}

// EncryptMessageBody encrypts a mail message body for the given recipients.
func EncryptMessageBody(body string, senderKR *pgp.KeyRing, recipientKRs ...*pgp.KeyRing) (string, error) {
	msg := pgp.NewPlainMessageFromString(body)

	// Build a combined key ring for all recipients + sender
	allKeys, err := pgp.NewKeyRing(nil)
	if err != nil {
		return "", err
	}

	for _, kr := range append(recipientKRs, senderKR) {
		for _, key := range kr.GetKeys() {
			if err := allKeys.AddKey(key); err != nil {
				continue
			}
		}
	}

	encrypted, err := allKeys.Encrypt(msg, senderKR)
	if err != nil {
		return "", fmt.Errorf("failed to encrypt message: %w", err)
	}

	armored, err := encrypted.GetArmored()
	if err != nil {
		return "", err
	}

	return armored, nil
}
