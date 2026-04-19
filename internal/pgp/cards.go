// Package pgp wraps gopenpgp primitives Proton uses for Calendar events,
// Contacts, Drive links and Pass key blobs. Nothing here talks to the API.
package pgp

import (
	"encoding/base64"
	"fmt"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
)

// Card types used by Proton for its VEVENT / VCard blobs.
const (
	CardClear           = 0
	CardEncrypted       = 1
	CardSigned          = 2
	CardEncryptedSigned = 3
)

// Card is a Proton-style signed/encrypted blob as returned by the API.
type Card struct {
	Type      int    `json:"Type"`
	Data      string `json:"Data"`
	Signature string `json:"Signature,omitempty"`
}

// DecryptCards decrypts a list of mixed-type cards. decryptionKR decrypts
// types 1/3, verificationKR is used to (best-effort) verify type 2 signatures.
// If keyPacket is set, it is used as a prefix for types 1/3 whose Data field
// contains only the data packet (Proton calendar shared events).
func DecryptCards(cards []Card, decryptionKR, verificationKR *pgp.KeyRing, keyPacket []byte) ([]string, error) {
	out := make([]string, 0, len(cards))
	for _, c := range cards {
		switch c.Type {
		case CardClear:
			out = append(out, c.Data)
		case CardSigned:
			if verificationKR != nil && c.Signature != "" {
				if sig, err := pgp.NewPGPSignatureFromArmored(c.Signature); err == nil {
					_ = verificationKR.VerifyDetached(pgp.NewPlainMessageFromString(c.Data), sig, pgp.GetUnixTime())
				}
			}
			out = append(out, c.Data)
		case CardEncrypted, CardEncryptedSigned:
			plain, err := decryptCardData(c.Data, keyPacket, decryptionKR)
			if err != nil {
				return nil, fmt.Errorf("decrypt card (type %d): %w", c.Type, err)
			}
			out = append(out, plain)
		default:
			out = append(out, c.Data)
		}
	}
	return out, nil
}

// DecryptCardsRaw accepts the map form returned from json.Unmarshal.
func DecryptCardsRaw(cards []map[string]any, decryptionKR, verificationKR *pgp.KeyRing, keyPacket []byte) ([]string, error) {
	typed := make([]Card, 0, len(cards))
	for _, m := range cards {
		c := Card{}
		if v, ok := m["Type"].(float64); ok {
			c.Type = int(v)
		}
		if v, ok := m["Data"].(string); ok {
			c.Data = v
		}
		if v, ok := m["Signature"].(string); ok {
			c.Signature = v
		}
		typed = append(typed, c)
	}
	return DecryptCards(typed, decryptionKR, verificationKR, keyPacket)
}

func decryptCardData(data string, keyPacket []byte, kr *pgp.KeyRing) (string, error) {
	if keyPacket != nil {
		raw, err := base64.StdEncoding.DecodeString(data)
		if err != nil {
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
		dec, err := kr.Decrypt(split.GetPGPMessage(), nil, pgp.GetUnixTime())
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

// SignCard produces a card of type CardSigned.
func SignCard(data string, signingKR *pgp.KeyRing) (*Card, error) {
	sig, err := signingKR.SignDetached(pgp.NewPlainMessageFromString(data))
	if err != nil {
		return nil, fmt.Errorf("sign card: %w", err)
	}
	armored, err := sig.GetArmored()
	if err != nil {
		return nil, err
	}
	return &Card{Type: CardSigned, Data: data, Signature: armored}, nil
}

// EncryptAndSignCard produces a card of type CardEncryptedSigned where the
// encrypted payload is armored (no separate key packet). Used by Contacts.
func EncryptAndSignCard(data string, encryptionKR, signingKR *pgp.KeyRing) (*Card, error) {
	msg := pgp.NewPlainMessageFromString(data)
	enc, err := encryptionKR.Encrypt(msg, nil)
	if err != nil {
		return nil, fmt.Errorf("encrypt card: %w", err)
	}
	armored, err := enc.GetArmored()
	if err != nil {
		return nil, err
	}
	sig, err := signingKR.SignDetached(msg)
	if err != nil {
		return nil, err
	}
	sigArmored, err := sig.GetArmored()
	if err != nil {
		return nil, err
	}
	return &Card{Type: CardEncryptedSigned, Data: armored, Signature: sigArmored}, nil
}

// EncryptAndSignCardSplit produces (signedCard, encryptedCard, sharedKeyPacket)
// for Proton calendar events. When existingKeyPacket is non-empty, the
// existing session key is reused (update flow) and the returned keyPacket is
// empty.
func EncryptAndSignCardSplit(signedData, encryptedData string, encryptionKR, signingKR *pgp.KeyRing, existingKeyPacketB64 string) (signed, encrypted *Card, keyPacketB64 string, err error) {
	signed, err = SignCard(signedData, signingKR)
	if err != nil {
		return nil, nil, "", err
	}

	encMsg := pgp.NewPlainMessageFromString(encryptedData)
	var dataPacket []byte
	if existingKeyPacketB64 != "" {
		kpBytes, err := base64.StdEncoding.DecodeString(existingKeyPacketB64)
		if err != nil {
			return nil, nil, "", fmt.Errorf("decode existing key packet: %w", err)
		}
		sk, err := encryptionKR.DecryptSessionKey(kpBytes)
		if err != nil {
			return nil, nil, "", fmt.Errorf("decrypt existing session key: %w", err)
		}
		dataPacket, err = sk.Encrypt(encMsg)
		if err != nil {
			return nil, nil, "", err
		}
	} else {
		enc, err := encryptionKR.Encrypt(encMsg, signingKR)
		if err != nil {
			return nil, nil, "", err
		}
		split, err := enc.SplitMessage()
		if err != nil {
			return nil, nil, "", err
		}
		keyPacketB64 = base64.StdEncoding.EncodeToString(split.GetBinaryKeyPacket())
		dataPacket = split.GetBinaryDataPacket()
	}

	sig, err := signingKR.SignDetached(encMsg)
	if err != nil {
		return nil, nil, "", err
	}
	sigArmored, err := sig.GetArmored()
	if err != nil {
		return nil, nil, "", err
	}
	encrypted = &Card{Type: CardEncryptedSigned, Data: base64.StdEncoding.EncodeToString(dataPacket), Signature: sigArmored}
	return signed, encrypted, keyPacketB64, nil
}
