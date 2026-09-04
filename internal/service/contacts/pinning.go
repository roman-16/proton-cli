package contacts

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	gopenpgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/crypto/pgp"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/proton"
	"github.com/roman-16/proton-cli/internal/skip"
	"github.com/roman-16/proton-cli/internal/vcard"
)

// ContactCrypto holds a contact's pinned-key encryption preferences for one
// email address, mirroring the x-pm-* vCard properties Proton stores. A nil
// Encrypt/Sign means the flag is unset in the contact.
type ContactCrypto struct {
	ArmoredKeys       []string `json:"armored_keys"`
	Encrypt           *bool    `json:"encrypt,omitempty"`
	Sign              *bool    `json:"sign,omitempty"`
	Scheme            string   `json:"scheme,omitempty"`
	SignatureVerified bool     `json:"signature_verified"`
}

// PinnedKeysFor returns the pinned public keys and encryption preferences a
// contact stores for email, or nil when the address has no contact or no
// pinned key. It is best-effort on decrypt/decode: a hiccup returns (nil, nil)
// so it never blocks sending, but a contact-lookup API error propagates.
func (s *Service) PinnedKeysFor(ctx context.Context, email string) (*ContactCrypto, error) {
	id, ok, err := s.contactIDByEmail(ctx, email)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	ct, err := s.Get(ctx, id)
	if err != nil {
		// A pinned key is somebody's decision about who they trust, and this is
		// answered on the way to encrypting a message. Reporting no pin for a
		// contact that has one sends the message under Proton's key instead,
		// which is a decision quietly reversed unless it is recorded.
		skip.Record(ctx, skip.KindContact, id, skip.Unreadable, err)
		return nil, nil
	}
	joined := strings.Join(ct.Cards, "\n")
	group := vcard.EmailGroup(joined, email)
	if group == "" {
		return nil, nil
	}
	armored := decodePinnedKeys(ctx, id, vcard.GroupValues(joined, group, "KEY"))
	if len(armored) == 0 {
		return nil, nil
	}
	cc := &ContactCrypto{
		ArmoredKeys:       armored,
		Scheme:            strings.ToLower(strings.TrimSpace(vcard.GroupValue(joined, group, "X-PM-SCHEME"))),
		SignatureVerified: ct.Signature == pgp.Verified,
	}
	if v := vcard.GroupValue(joined, group, "X-PM-ENCRYPT"); v != "" {
		b := parseVCardBool(v)
		cc.Encrypt = &b
	}
	if v := vcard.GroupValue(joined, group, "X-PM-SIGN"); v != "" {
		b := parseVCardBool(v)
		cc.Sign = &b
	}
	return cc, nil
}

// contactIDByEmail resolves an email to its contact ID via the contact-emails
// endpoint. Defaults==1 means the contact has no per-email configuration, so
// it is treated as a miss.
func (s *Service) contactIDByEmail(ctx context.Context, email string) (string, bool, error) {
	var r struct {
		ContactEmails []struct {
			ContactID string
			Defaults  int
		}
	}
	if err := s.C.Decode(ctx, proton.Request{
		Method: "GET", Path: "/contacts/v4/contacts/emails", Query: proton.Query("Email", email),
	}, &r); err != nil {
		return "", false, err
	}
	if len(r.ContactEmails) == 0 || r.ContactEmails[0].Defaults == 1 {
		return "", false, nil
	}
	return r.ContactEmails[0].ContactID, true, nil
}

// decodePinnedKeys turns "data:application/pgp-keys;base64,<b64>" vCard KEY
// values into armored public keys.
//
// A key that will not decode is counted, because a pinned key is a decision
// somebody made about who they trust: silently listing three of their four
// pinned keys tells them the fourth is gone when it is only unreadable.
func decodePinnedKeys(ctx context.Context, contactID string, values []string) []string {
	var out []string
	for _, v := range values {
		_, b64, ok := strings.Cut(v, ",")
		if !ok {
			skip.Record(ctx, skip.KindKey, contactID, skip.Malformed, nil)
			continue
		}
		bin, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
		if err != nil {
			skip.Record(ctx, skip.KindKey, contactID, skip.Malformed, err)
			continue
		}
		key, err := gopenpgp.NewKey(bin)
		if err != nil {
			skip.Record(ctx, skip.KindKey, contactID, skip.Malformed, err)
			continue
		}
		armored, err := key.GetArmoredPublicKey()
		if err != nil {
			skip.Record(ctx, skip.KindKey, contactID, skip.Malformed, err)
			continue
		}
		out = append(out, armored)
	}
	return out
}

func parseVCardBool(v string) bool {
	return strings.EqualFold(strings.TrimSpace(v), "true")
}

type rawCard struct {
	Type      int
	Data      string
	Signature string
}

func (s *Service) rawContactCards(ctx context.Context, id string) ([]rawCard, error) {
	var r struct {
		Contact struct{ Cards []rawCard }
	}
	if err := s.C.Decode(ctx, proton.Request{Method: "GET", Path: "/contacts/v4/contacts/" + id}, &r); err != nil {
		return nil, err
	}
	return r.Contact.Cards, nil
}

// editableSignedCard fetches a contact's raw cards, verifies and parses the
// signed card into an editable model, and returns the remaining (encrypted/
// clear) cards verbatim so callers can re-attach them unchanged on PUT.
func (s *Service) editableSignedCard(ctx context.Context, id string) (*vcard.Signed, []map[string]any, error) {
	u, err := s.keys(ctx)
	if err != nil {
		return nil, nil, err
	}
	cards, err := s.rawContactCards(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	var signedData string
	haveSigned := false
	var others []map[string]any
	for _, c := range cards {
		if c.Type == pgp.CardSigned && !haveSigned {
			msg := gopenpgp.NewPlainMessageFromString(c.Data)
			if v := pgp.VerifyDetachedStatus(u.UserKR, msg, c.Signature); v != pgp.Verified {
				return nil, nil, errs.Problemf("This contact's signed card could not be verified, so it will not be edited.").
					Hint("open the contact in a Proton app to re-sign it")
			}
			signedData = c.Data
			haveSigned = true
			continue
		}
		others = append(others, map[string]any{"Type": c.Type, "Data": c.Data, "Signature": c.Signature})
	}
	if !haveSigned {
		return nil, nil, fmt.Errorf("contact has no signed card to edit")
	}
	model := vcard.ParseSigned(signedData)
	if model.UID == "" {
		model.UID = vcard.UID()
	}
	return &model, others, nil
}

// putSignedCard re-signs the model and PUTs it alongside the preserved cards.
func (s *Service) putSignedCard(ctx context.Context, id string, model vcard.Signed, others []map[string]any) error {
	kr, err := s.writeKey(ctx)
	if err != nil {
		return err
	}
	signedCard, err := pgp.SignCard(vcard.BuildSigned(model), kr)
	if err != nil {
		return err
	}
	cards := make([]any, 0, len(others)+1)
	cards = append(cards, signedCard)
	for _, o := range others {
		cards = append(cards, o)
	}
	return s.C.Decode(ctx, proton.Request{Method: "PUT", Path: "/contacts/v4/contacts/" + id, Body: map[string]any{"Cards": cards}}, nil)
}

// PinKey pins armoredKey to the contact for email as the preferred key. Encrypt
// and sign default to true (matching the web client's "trust key" flow) unless
// overridden. The signed card is re-signed; all other cards are preserved.
func (s *Service) PinKey(ctx context.Context, id, email, armoredKey string, encrypt, sign *bool, scheme string) error {
	keyValue, err := encodePinnedKey(armoredKey)
	if err != nil {
		return err
	}
	model, others, err := s.editableSignedCard(ctx, id)
	if err != nil {
		return err
	}
	e := model.FindEmail(email)
	if e == nil {
		model.Emails = append(model.Emails, vcard.SignedEmail{Address: email})
		e = &model.Emails[len(model.Emails)-1]
	}
	e.KeyValues = prependUnique(e.KeyValues, keyValue)
	trueVal := true
	if encrypt != nil {
		e.Encrypt = encrypt
	} else {
		e.Encrypt = &trueVal
	}
	if sign != nil {
		e.Sign = sign
	} else {
		signVal := true
		e.Sign = &signVal
	}
	if scheme != "" {
		e.Scheme = scheme
	}
	return s.putSignedCard(ctx, id, *model, others)
}

// UnpinKey removes all pinned keys and crypto flags a contact stores for email.
func (s *Service) UnpinKey(ctx context.Context, id, email string) error {
	model, others, err := s.editableSignedCard(ctx, id)
	if err != nil {
		return err
	}
	e := model.FindEmail(email)
	if e == nil || len(e.KeyValues) == 0 {
		return &errs.NotFound{Kind: "pinned key", Ref: email}
	}
	e.KeyValues = nil
	e.Encrypt = nil
	e.Sign = nil
	e.Scheme = ""
	return s.putSignedCard(ctx, id, *model, others)
}

// encodePinnedKey converts an armored public key (or the public part of a
// private key) into a vCard KEY property value.
func encodePinnedKey(armored string) (string, error) {
	key, err := gopenpgp.NewKeyFromArmored(strings.TrimSpace(armored))
	if err != nil {
		return "", fmt.Errorf("invalid public key: %w", err)
	}
	bin, err := key.GetPublicKey()
	if err != nil {
		return "", fmt.Errorf("read public key: %w", err)
	}
	return "data:application/pgp-keys;base64," + base64.StdEncoding.EncodeToString(bin), nil
}

// prependUnique returns existing with v moved to the front (highest
// preference), dropping any duplicate of v.
func prependUnique(existing []string, v string) []string {
	out := make([]string, 0, len(existing)+1)
	out = append(out, v)
	for _, e := range existing {
		if e != v {
			out = append(out, e)
		}
	}
	return out
}
