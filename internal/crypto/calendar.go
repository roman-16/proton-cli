package crypto

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/client"
)

// CalendarKeys holds an unlocked calendar key ring.
type CalendarKeys struct {
	CalendarKR *pgp.KeyRing
	AddrKR     *pgp.KeyRing
}

type calendarKey struct {
	ID           string
	CalendarID   string
	PrivateKey   string
	PassphraseID string
	Flags        int
}

type calendarPassphrase struct {
	ID                string
	Flags             int
	MemberPassphrases []memberPassphrase
}

type memberPassphrase struct {
	MemberID   string
	Passphrase string
	Signature  string
}

type calendarMember struct {
	ID         string
	CalendarID string
	Email      string
	AddressID  string
}

// UnlockCalendar fetches calendar keys and unlocks them using the address key ring.
func UnlockCalendar(ctx context.Context, c *client.Client, calendarID string, kr *KeyRings) (*CalendarKeys, error) {
	// Get calendar members to find the matching address
	members, err := getCalendarMembers(ctx, c, calendarID)
	if err != nil {
		return nil, err
	}

	var addrKR *pgp.KeyRing
	var memberID string
	for _, m := range members {
		if akr, ok := kr.AddrKRs[m.AddressID]; ok {
			addrKR = akr
			memberID = m.ID
			break
		}
	}
	if addrKR == nil {
		return nil, fmt.Errorf("no matching address key for calendar %s", calendarID)
	}

	// Get calendar passphrase
	passphrase, err := getCalendarPassphrase(ctx, c, calendarID)
	if err != nil {
		return nil, err
	}

	// Find the member passphrase for our member
	var calPassphrase []byte
	for _, mp := range passphrase.MemberPassphrases {
		if mp.MemberID == memberID {
			calPassphrase, err = decryptMemberPassphrase(mp, addrKR)
			if err != nil {
				return nil, fmt.Errorf("failed to decrypt calendar passphrase: %w", err)
			}
			break
		}
	}
	if calPassphrase == nil {
		return nil, fmt.Errorf("no passphrase found for member %s", memberID)
	}

	// Get calendar keys and unlock
	keys, err := getCalendarKeys(ctx, c, calendarID)
	if err != nil {
		return nil, err
	}

	calKR, err := pgp.NewKeyRing(nil)
	if err != nil {
		return nil, err
	}

	for _, key := range keys {
		lockedKey, err := pgp.NewKeyFromArmored(key.PrivateKey)
		if err != nil {
			continue
		}
		unlockedKey, err := lockedKey.Unlock(calPassphrase)
		if err != nil {
			continue
		}
		if err := calKR.AddKey(unlockedKey); err != nil {
			continue
		}
	}

	if calKR.CountEntities() == 0 {
		return nil, fmt.Errorf("failed to unlock any calendar keys")
	}

	return &CalendarKeys{CalendarKR: calKR, AddrKR: addrKR}, nil
}

// DecryptEventCards decrypts the SharedEvents and CalendarEvents from an event.
func DecryptEventCards(cards []map[string]interface{}, calKeys *CalendarKeys, sharedKeyPacket string) ([]string, error) {
	var results []string

	var kp []byte
	if sharedKeyPacket != "" {
		var err error
		kp, err = base64.StdEncoding.DecodeString(sharedKeyPacket)
		if err != nil {
			return nil, err
		}
	}

	for _, card := range cards {
		cardType, _ := card["Type"].(float64)
		data, _ := card["Data"].(string)
		signature, _ := card["Signature"].(string)

		switch int(cardType) {
		case 0: // Clear text
			results = append(results, data)

		case 1: // Encrypted only
			dec, err := decryptCard(data, kp, calKeys.CalendarKR)
			if err != nil {
				return nil, fmt.Errorf("failed to decrypt card: %w", err)
			}
			results = append(results, dec)

		case 2: // Signed only
			if err := verifyCard(data, signature, calKeys.AddrKR); err != nil {
				// Verification failure is non-fatal, still return data
			}
			results = append(results, data)

		case 3: // Encrypted and signed
			dec, err := decryptCard(data, kp, calKeys.CalendarKR)
			if err != nil {
				return nil, fmt.Errorf("failed to decrypt card: %w", err)
			}
			results = append(results, dec)

		default:
			results = append(results, data)
		}
	}

	return results, nil
}

// EncryptEventCards creates the signed + encrypted event cards for a calendar event.
// Returns: signedCard (Type 2), encryptedCard (Type 3), sharedKeyPacket
// If existingKeyPacket is provided, reuses the existing session key (for updates).
func EncryptEventCards(signedVevent, encryptedVevent string, calKeys *CalendarKeys, existingKeyPacket string) (map[string]interface{}, map[string]interface{}, string, error) {
	// Type 2: Signed cleartext (times, UID, sequence)
	signedMsg := pgp.NewPlainMessageFromString(signedVevent)
	sig, err := calKeys.AddrKR.SignDetached(signedMsg)
	if err != nil {
		return nil, nil, "", fmt.Errorf("failed to sign event card: %w", err)
	}
	sigArmored, err := sig.GetArmored()
	if err != nil {
		return nil, nil, "", err
	}

	signedCard := map[string]interface{}{
		"Type":      2,
		"Data":      signedVevent,
		"Signature": sigArmored,
	}

	// Type 3: Encrypted and signed (title, location, description)
	encMsg := pgp.NewPlainMessageFromString(encryptedVevent)

	var keyPacket string
	var dataPacket string

	if existingKeyPacket != "" {
		// Reuse existing session key (for updates)
		kpBytes, err := base64.StdEncoding.DecodeString(existingKeyPacket)
		if err != nil {
			return nil, nil, "", fmt.Errorf("failed to decode existing key packet: %w", err)
		}
		sk, err := calKeys.CalendarKR.DecryptSessionKey(kpBytes)
		if err != nil {
			return nil, nil, "", fmt.Errorf("failed to decrypt existing session key: %w", err)
		}
		encData, err := sk.Encrypt(encMsg)
		if err != nil {
			return nil, nil, "", fmt.Errorf("failed to encrypt with session key: %w", err)
		}
		keyPacket = ""
		dataPacket = base64.StdEncoding.EncodeToString(encData)
	} else {
		// New event: encrypt with calendar key ring (generates new session key)
		encrypted, err := calKeys.CalendarKR.Encrypt(encMsg, calKeys.AddrKR)
		if err != nil {
			return nil, nil, "", fmt.Errorf("failed to encrypt event card: %w", err)
		}
		split, err := encrypted.SplitMessage()
		if err != nil {
			return nil, nil, "", fmt.Errorf("failed to split message: %w", err)
		}
		keyPacket = base64.StdEncoding.EncodeToString(split.GetBinaryKeyPacket())
		dataPacket = base64.StdEncoding.EncodeToString(split.GetBinaryDataPacket())
	}

	encSig, err := calKeys.AddrKR.SignDetached(encMsg)
	if err != nil {
		return nil, nil, "", err
	}
	encSigArmored, err := encSig.GetArmored()
	if err != nil {
		return nil, nil, "", err
	}

	encryptedCard := map[string]interface{}{
		"Type":      3,
		"Data":      dataPacket,
		"Signature": encSigArmored,
	}

	return signedCard, encryptedCard, keyPacket, nil
}

func decryptCard(data string, keyPacket []byte, kr *pgp.KeyRing) (string, error) {
	if keyPacket != nil {
		raw, err := base64.StdEncoding.DecodeString(data)
		if err != nil {
			// Try as armored
			msg, err := pgp.NewPGPMessageFromArmored(data)
			if err != nil {
				return "", err
			}
			dec, err := kr.Decrypt(msg, nil, pgp.GetUnixTime())
			if err != nil {
				return "", err
			}
			return dec.GetString(), nil
		}
		split := pgp.NewPGPSplitMessage(keyPacket, raw)
		msg := split.GetPGPMessage()
		dec, err := kr.Decrypt(msg, nil, pgp.GetUnixTime())
		if err != nil {
			return "", err
		}
		return dec.GetString(), nil
	}

	msg, err := pgp.NewPGPMessageFromArmored(data)
	if err != nil {
		return "", err
	}
	dec, err := kr.Decrypt(msg, nil, pgp.GetUnixTime())
	if err != nil {
		return "", err
	}
	return dec.GetString(), nil
}

func verifyCard(data, signatureArmored string, kr *pgp.KeyRing) error {
	sig, err := pgp.NewPGPSignatureFromArmored(signatureArmored)
	if err != nil {
		return err
	}
	return kr.VerifyDetached(pgp.NewPlainMessageFromString(data), sig, pgp.GetUnixTime())
}

func decryptMemberPassphrase(mp memberPassphrase, addrKR *pgp.KeyRing) ([]byte, error) {
	msg, err := pgp.NewPGPMessageFromArmored(mp.Passphrase)
	if err != nil {
		return nil, err
	}

	sig, err := pgp.NewPGPSignatureFromArmored(mp.Signature)
	if err != nil {
		return nil, err
	}

	dec, err := addrKR.Decrypt(msg, nil, pgp.GetUnixTime())
	if err != nil {
		return nil, err
	}

	if err := addrKR.VerifyDetached(dec, sig, pgp.GetUnixTime()); err != nil {
		return nil, err
	}

	return dec.GetBinary(), nil
}

func getCalendarMembers(ctx context.Context, c *client.Client, calendarID string) ([]calendarMember, error) {
	body, _, err := c.Do(ctx, "GET", "/calendar/v1/"+calendarID+"/members", nil, "", "", "")
	if err != nil {
		return nil, err
	}
	var res struct {
		Members []calendarMember
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, err
	}
	return res.Members, nil
}

func getCalendarPassphrase(ctx context.Context, c *client.Client, calendarID string) (*calendarPassphrase, error) {
	body, _, err := c.Do(ctx, "GET", "/calendar/v1/"+calendarID+"/passphrase", nil, "", "", "")
	if err != nil {
		return nil, err
	}
	var res struct {
		Passphrase calendarPassphrase
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, err
	}
	return &res.Passphrase, nil
}

func getCalendarKeys(ctx context.Context, c *client.Client, calendarID string) ([]calendarKey, error) {
	body, _, err := c.Do(ctx, "GET", "/calendar/v1/"+calendarID+"/keys", nil, "", "", "")
	if err != nil {
		return nil, err
	}
	var res struct {
		Keys []calendarKey
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, err
	}
	return res.Keys, nil
}
