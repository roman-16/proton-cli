package mail

import (
	"context"
	"encoding/base64"
	"fmt"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
)

// buildBodyPackages encrypts the body once under a shared session key and
// returns up to three packages (internal, encrypted-for-outside, cleartext) that
// reference it, keyed per recipient scheme.
func (s *Service) buildBodyPackages(c Content, del Delivery, atts []*draftAttachment, plans []plannedRecipient, eoModulus, eoModulusID string) ([]map[string]any, error) {
	sessionKey, err := pgp.GenerateSessionKey()
	if err != nil {
		return nil, err
	}
	encBody, err := sessionKey.EncryptAndSign(pgp.NewPlainMessageFromString(c.Body), c.From.KR)
	if err != nil {
		return nil, err
	}
	bodyB64 := base64.StdEncoding.EncodeToString(encBody)
	mimeType := c.mimeType()

	internalAddrs := map[string]any{}
	eoAddrs := map[string]any{}
	clearAddrs := map[string]any{}

	for _, p := range plans {
		switch p.scheme {
		case schemeInternal:
			recKR, err := keyRingFromArmored(p.armoredKey, p.email)
			if err != nil {
				return nil, err
			}
			recKP, err := recKR.EncryptSessionKey(sessionKey)
			if err != nil {
				return nil, err
			}
			addr := map[string]any{
				"Type":          pkgInternal,
				"BodyKeyPacket": base64.StdEncoding.EncodeToString(recKP),
				"Signature":     0,
			}
			akp, err := attachmentKeyPackets(recKR, atts)
			if err != nil {
				return nil, err
			}
			if akp != nil {
				addr["AttachmentKeyPackets"] = akp
			}
			internalAddrs[p.email] = addr
		case schemeEO:
			addr, err := eoAddress(sessionKey, del.EOPassword, del.EOPasswordHint, atts, eoModulus, eoModulusID)
			if err != nil {
				return nil, err
			}
			eoAddrs[p.email] = addr
		case schemeClear:
			clearAddrs[p.email] = map[string]any{"Type": pkgClear, "Signature": 0}
		}
	}

	var packages []map[string]any
	if len(internalAddrs) > 0 {
		bodyKP, err := c.From.KR.EncryptSessionKey(sessionKey)
		if err != nil {
			return nil, err
		}
		packages = append(packages, map[string]any{
			"Addresses":     internalAddrs,
			"MIMEType":      mimeType,
			"Type":          pkgInternal,
			"Body":          bodyB64,
			"BodyKeyPacket": base64.StdEncoding.EncodeToString(bodyKP),
		})
	}
	if len(eoAddrs) > 0 {
		packages = append(packages, map[string]any{
			"Addresses": eoAddrs,
			"MIMEType":  mimeType,
			"Type":      pkgEO,
			"Body":      bodyB64,
		})
	}
	if len(clearAddrs) > 0 {
		clearPkg := map[string]any{
			"Addresses": clearAddrs,
			"MIMEType":  mimeType,
			"Type":      pkgClear,
			"Body":      bodyB64,
			"BodyKey":   map[string]any{"Key": base64.StdEncoding.EncodeToString(sessionKey.Key), "Algorithm": sessionKey.Algo},
		}
		if ak := attachmentCleartextKeys(atts); ak != nil {
			clearPkg["AttachmentKeys"] = ak
		}
		packages = append(packages, clearPkg)
	}
	return packages, nil
}

// buildInlinePackage builds the SEND_PGP_INLINE package: a plaintext body
// encrypted under a fresh session key, with the session key and each attachment
// key wrapped to every inline recipient's pinned PGP key. Inline is
// plaintext-only, so an HTML body is flattened. Returns ok=false when no
// recipient uses PGP-Inline.
func (s *Service) buildInlinePackage(c Content, atts []*draftAttachment, plans []plannedRecipient) (map[string]any, bool, error) {
	body := c.plainBody()
	addrs := map[string]any{}
	var sessionKey *pgp.SessionKey
	var bodyB64 string
	for _, p := range plans {
		if p.scheme != schemeExternalInline {
			continue
		}
		if sessionKey == nil {
			sk, err := pgp.GenerateSessionKey()
			if err != nil {
				return nil, false, err
			}
			enc, err := sk.EncryptAndSign(pgp.NewPlainMessageFromString(body), c.From.KR)
			if err != nil {
				return nil, false, err
			}
			sessionKey = sk
			bodyB64 = base64.StdEncoding.EncodeToString(enc)
		}
		recKR, err := keyRingFromArmored(p.armoredKey, p.email)
		if err != nil {
			return nil, false, err
		}
		recKP, err := recKR.EncryptSessionKey(sessionKey)
		if err != nil {
			return nil, false, err
		}
		addr := map[string]any{
			"Type":          pkgPGPInline,
			"BodyKeyPacket": base64.StdEncoding.EncodeToString(recKP),
			"Signature":     0,
		}
		akp, err := attachmentKeyPackets(recKR, atts)
		if err != nil {
			return nil, false, err
		}
		if akp != nil {
			addr["AttachmentKeyPackets"] = akp
		}
		addrs[p.email] = addr
	}
	if len(addrs) == 0 {
		return nil, false, nil
	}
	bodyKP, err := c.From.KR.EncryptSessionKey(sessionKey)
	if err != nil {
		return nil, false, err
	}
	return map[string]any{
		"Addresses":     addrs,
		"MIMEType":      mimeTypePlain,
		"Type":          pkgPGPInline,
		"Body":          bodyB64,
		"BodyKeyPacket": base64.StdEncoding.EncodeToString(bodyKP),
	}, true, nil
}

// buildPGPMIMEPackage builds the multipart MIME body (with attachments embedded
// verbatim rather than referenced), encrypts it under a fresh session key, and
// wraps that key to each external-PGP recipient's key. Returns ok=false when no
// recipient uses PGP/MIME.
func (s *Service) buildPGPMIMEPackage(ctx context.Context, c Content, atts []*draftAttachment, plans []plannedRecipient) (map[string]any, bool, error) {
	addrs := map[string]any{}
	var sessionKey *pgp.SessionKey
	var bodyB64 string

	for _, p := range plans {
		if p.scheme != schemeExternalPGP {
			continue
		}
		if sessionKey == nil {
			parts, err := s.mimeParts(ctx, atts)
			if err != nil {
				return nil, false, err
			}
			mimeStr, err := buildMIMEMessage(c.Body, c.mimeType(), parts)
			if err != nil {
				return nil, false, err
			}
			sessionKey, err = pgp.GenerateSessionKey()
			if err != nil {
				return nil, false, err
			}
			enc, err := sessionKey.EncryptAndSign(pgp.NewPlainMessageFromString(mimeStr), c.From.KR)
			if err != nil {
				return nil, false, err
			}
			bodyB64 = base64.StdEncoding.EncodeToString(enc)
		}
		recKR, err := keyRingFromArmored(p.armoredKey, p.email)
		if err != nil {
			return nil, false, err
		}
		kp, err := recKR.EncryptSessionKey(sessionKey)
		if err != nil {
			return nil, false, err
		}
		addrs[p.email] = map[string]any{
			"Type":          pkgPGPMIME,
			"BodyKeyPacket": base64.StdEncoding.EncodeToString(kp),
		}
	}
	if len(addrs) == 0 {
		return nil, false, nil
	}
	return map[string]any{
		"Addresses": addrs,
		"MIMEType":  "multipart/mixed",
		"Type":      pkgPGPMIME,
		"Body":      bodyB64,
	}, true, nil
}

// mimeParts materialises a draft's attachments as MIME parts, downloading and
// decrypting any whose bytes are not already in hand.
func (s *Service) mimeParts(ctx context.Context, atts []*draftAttachment) ([]mimePart, error) {
	out := make([]mimePart, 0, len(atts))
	for _, a := range atts {
		data, err := s.attachmentBytes(ctx, a)
		if err != nil {
			return nil, err
		}
		out = append(out, mimePart{
			Filename: a.Name, MIMEType: a.MIMEType, Data: data, ContentID: a.ContentID,
		})
	}
	return out, nil
}

func keyRingFromArmored(armored, email string) (*pgp.KeyRing, error) {
	key, err := pgp.NewKeyFromArmored(armored)
	if err != nil {
		return nil, fmt.Errorf("parse recipient key for %s: %w", email, err)
	}
	return pgp.NewKeyRing(key)
}
