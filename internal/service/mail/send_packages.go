package mail

import (
	"context"
	"encoding/base64"
	"fmt"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/proton"
)

// packageFormats are the bodies a message can be sent as, one package each.
var packageFormats = []string{mimeTypeHTML, mimeTypePlain, mimeTypeMultipart}

// buildPackages lays a message out the way Proton's own clients do: one
// package per body the recipients need - the HTML, the plain text, or the
// whole MIME message with its attachments inside - each encrypted once under a
// session key of its own, with every recipient attached to the one they get.
func (s *Service) buildPackages(ctx context.Context, c Content, del Delivery, atts []*draftAttachment, plans []plannedRecipient, eoModulus proton.Modulus) ([]map[string]any, error) {
	var packages []map[string]any
	for _, format := range packageFormats {
		var members []plannedRecipient
		for _, p := range plans {
			if p.mimeType == format {
				members = append(members, p)
			}
		}
		if len(members) == 0 {
			continue
		}
		body, err := s.packageBody(ctx, c, atts, format)
		if err != nil {
			return nil, err
		}
		sessionKey, err := pgp.GenerateSessionKey()
		if err != nil {
			return nil, err
		}
		encBody, err := sessionKey.EncryptAndSign(pgp.NewPlainMessageFromString(body), c.From.Keys.Write)
		if err != nil {
			return nil, err
		}
		addresses := map[string]any{}
		kind := 0
		for _, p := range members {
			sub, err := subPackage(p, sessionKey, del, atts, eoModulus)
			if err != nil {
				return nil, err
			}
			addresses[p.email] = sub
			kind |= p.kind
		}
		pkg := map[string]any{
			"Addresses": addresses,
			"MIMEType":  format,
			"Type":      kind,
			"Body":      base64.StdEncoding.EncodeToString(encBody),
		}
		if kind&(pkgClear|pkgClearMIME) != 0 {
			pkg["BodyKey"] = map[string]any{
				"Key": base64.StdEncoding.EncodeToString(sessionKey.Key), "Algorithm": sessionKey.Algo,
			}
		}
		if kind&pkgClear != 0 && format != mimeTypeMultipart {
			if ak := attachmentCleartextKeys(atts); ak != nil {
				pkg["AttachmentKeys"] = ak
			}
		}
		packages = append(packages, pkg)
	}
	return packages, nil
}

// packageBody is the message as one format carries it. The MIME message holds
// the attachments itself, so its recipients are handed no attachment keys.
func (s *Service) packageBody(ctx context.Context, c Content, atts []*draftAttachment, format string) (string, error) {
	switch format {
	case mimeTypeHTML:
		return c.Body, nil
	case mimeTypePlain:
		return c.plainBody(), nil
	}
	parts, err := s.mimeParts(ctx, atts)
	if err != nil {
		return "", err
	}
	return buildMIMEMessage(c.Body, c.mimeType(), parts)
}

// subPackage is one recipient's entry in a package: the body's session key
// wrapped to their key, to a password, or handed over for Proton to send in the
// clear - signed when their settings ask for it.
func subPackage(p plannedRecipient, sessionKey *pgp.SessionKey, del Delivery, atts []*draftAttachment, eoModulus proton.Modulus) (map[string]any, error) {
	switch p.kind {
	case pkgEO:
		return eoAddress(sessionKey, del.EOPassword, del.EOPasswordHint, atts, eoModulus)
	case pkgClear, pkgClearMIME:
		return map[string]any{"Type": p.kind, "Signature": signatureBit(p.signature)}, nil
	}
	recKR, err := keyRingFromArmored(p.armoredKey, p.email)
	if err != nil {
		return nil, err
	}
	kp, err := recKR.EncryptSessionKey(sessionKey)
	if err != nil {
		return nil, err
	}
	sub := map[string]any{"Type": p.kind, "BodyKeyPacket": base64.StdEncoding.EncodeToString(kp)}
	if p.kind == pkgPGPMIME {
		return sub, nil
	}
	sub["Signature"] = 0
	akp, err := attachmentKeyPackets(recKR, atts)
	if err != nil {
		return nil, err
	}
	if akp != nil {
		sub["AttachmentKeyPackets"] = akp
	}
	return sub, nil
}

func signatureBit(signed bool) int {
	if signed {
		return 1
	}
	return 0
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
