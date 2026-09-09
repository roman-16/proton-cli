package mail

import (
	"encoding/base64"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/ProtonMail/gopenpgp/v2/helper"
	"github.com/roman-16/proton-cli/internal/proton"
)

// eoAddress builds an encrypted-for-outside sub-package: the body session key
// and attachment keys are wrapped with the password, and a random token plus an
// SRP verifier let the recipient authenticate to Proton's EO viewer.
func eoAddress(sessionKey *pgp.SessionKey, password, hint string, atts []*draftAttachment, modulus proton.Modulus) (map[string]any, error) {
	bodyKP, err := pgp.EncryptSessionKeyWithPassword(sessionKey, []byte(password))
	if err != nil {
		return nil, err
	}
	tokenSK, err := pgp.GenerateSessionKey()
	if err != nil {
		return nil, err
	}
	token := base64.StdEncoding.EncodeToString(tokenSK.Key)
	encToken, err := helper.EncryptMessageWithPassword([]byte(password), token)
	if err != nil {
		return nil, err
	}
	auth, err := eoAuth(password, modulus)
	if err != nil {
		return nil, err
	}
	addr := map[string]any{
		"Type":          pkgEO,
		"BodyKeyPacket": base64.StdEncoding.EncodeToString(bodyKP),
		"Token":         token,
		"EncToken":      encToken,
		"Auth":          auth,
		"Signature":     0,
	}
	if hint != "" {
		addr["PasswordHint"] = hint
	}
	akp, err := attachmentPasswordKeyPackets(atts, password)
	if err != nil {
		return nil, err
	}
	if akp != nil {
		addr["AttachmentKeyPackets"] = akp
	}
	return addr, nil
}

// eoAuth builds the SRP verifier the EO recipient uses to prove knowledge of the
// password, reusing the shared modulus with a fresh per-recipient salt.
func eoAuth(password string, modulus proton.Modulus) (map[string]any, error) {
	v, err := modulus.Verifier([]byte(password))
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"Version":   v.Version,
		"ModulusID": v.ModulusID,
		"Salt":      v.Salt,
		"Verifier":  v.Value,
	}, nil
}
