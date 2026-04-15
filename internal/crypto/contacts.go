package crypto

import (
	"fmt"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
)

// SignContactCard creates a signed (Type 2) contact card.
func SignContactCard(vcard string, addrKR *pgp.KeyRing) (map[string]interface{}, error) {
	msg := pgp.NewPlainMessageFromString(vcard)
	sig, err := addrKR.SignDetached(msg)
	if err != nil {
		return nil, fmt.Errorf("failed to sign contact card: %w", err)
	}
	sigArmored, err := sig.GetArmored()
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"Type":      2,
		"Data":      vcard,
		"Signature": sigArmored,
	}, nil
}

// DecryptContactCards decrypts contact card data.
// Contact cards use the same card types as calendar events:
// 0 = clear, 1 = encrypted, 2 = signed, 3 = encrypted+signed
func DecryptContactCards(cards []map[string]interface{}, addrKR *pgp.KeyRing) ([]string, error) {
	var results []string

	for _, card := range cards {
		cardType, _ := card["Type"].(float64)
		data, _ := card["Data"].(string)
		signature, _ := card["Signature"].(string)

		switch int(cardType) {
		case 0: // Clear text
			results = append(results, data)

		case 1: // Encrypted
			msg, err := pgp.NewPGPMessageFromArmored(data)
			if err != nil {
				return nil, fmt.Errorf("failed to parse encrypted contact card: %w", err)
			}
			dec, err := addrKR.Decrypt(msg, nil, pgp.GetUnixTime())
			if err != nil {
				return nil, fmt.Errorf("failed to decrypt contact card: %w", err)
			}
			results = append(results, dec.GetString())

		case 2: // Signed
			if signature != "" {
				sig, err := pgp.NewPGPSignatureFromArmored(signature)
				if err == nil {
					_ = addrKR.VerifyDetached(pgp.NewPlainMessageFromString(data), sig, pgp.GetUnixTime()) // Non-fatal
				}
			}
			results = append(results, data)

		case 3: // Encrypted and signed
			msg, err := pgp.NewPGPMessageFromArmored(data)
			if err != nil {
				return nil, fmt.Errorf("failed to parse encrypted contact card: %w", err)
			}
			dec, err := addrKR.Decrypt(msg, nil, pgp.GetUnixTime())
			if err != nil {
				return nil, fmt.Errorf("failed to decrypt contact card: %w", err)
			}
			results = append(results, dec.GetString())

		default:
			results = append(results, data)
		}
	}

	return results, nil
}

// EncryptContactCard encrypts and signs vCard data for a contact.
// Encryption and signature are separate (detached), matching the web client.
func EncryptContactCard(vcard string, addrKR *pgp.KeyRing) (map[string]interface{}, error) {
	msg := pgp.NewPlainMessageFromString(vcard)

	// Encrypt with address public key (no embedded signature)
	encrypted, err := addrKR.Encrypt(msg, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt contact card: %w", err)
	}

	armored, err := encrypted.GetArmored()
	if err != nil {
		return nil, err
	}

	// Sign separately (detached)
	sig, err := addrKR.SignDetached(msg)
	if err != nil {
		return nil, err
	}

	sigArmored, err := sig.GetArmored()
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"Type":      3,
		"Data":      armored,
		"Signature": sigArmored,
	}, nil
}
