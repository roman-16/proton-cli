package contacts

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"strings"

	gopenpgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/crypto/pgp"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/proton"
	"github.com/roman-16/proton-cli/internal/skip"
	"github.com/roman-16/proton-cli/internal/vcard"
)

// EmailPreferences is how a contact wants mail to one of its addresses sent. A
// nil Sign follows the account's sign setting. Encrypt is about the pinned keys
// when the address has any, and about the keys its provider publishes when it
// has none; nil means yes whenever there is a key.
type EmailPreferences struct {
	Encrypt   *bool
	Sign      *bool
	Scheme    string
	PlainText bool
}

// EmailSettings is everything a contact holds for one of its addresses.
type EmailSettings struct {
	Address string
	Kind    string
	// Keys are the pinned public keys, armoured, in preference order.
	Keys []string
	EmailPreferences
	SignatureVerified bool
	// Unknown says what the address holds could not be established: the
	// contact would not open, or a key it pins would not decode. It is a fact
	// for the caller to weigh, not a refusal - a listing shows what it could
	// read, and a send decides for itself what it does with a pin it cannot see.
	Unknown bool
}

// EmailSettingsFor returns what a contact holds for email, nil when no contact
// has settings for it, or Unknown when there is a contact and it would not open.
//
// A pinned key is somebody's decision about who they trust, and this is
// answered on the way to encrypting a message. Reporting no pin for a contact
// whose card will not open would send the message under Proton's key instead,
// which is that decision quietly reversed - so a contact that will not open is
// reported as exactly that, and the caller decides.
func (s *Service) EmailSettingsFor(ctx context.Context, email string) (*EmailSettings, error) {
	id, ok, err := s.contactIDByEmail(ctx, email)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	ct, err := s.Get(ctx, id)
	if err != nil {
		// Recorded and not counted. The caller says on the screen what it did
		// about a pin it could not see; this line says which contact and why.
		slog.DebugContext(ctx, "contacts: a contact did not open",
			"kind", string(skip.KindContact), "reason", string(skip.Unreadable), "ref", id, "error", err.Error())
		return &EmailSettings{Address: email, Unknown: true}, nil
	}
	return SettingsOf(ctx, ct, email), nil
}

// ContactEmailSettings returns the settings of every address a contact holds,
// in the order the contact lists them.
func ContactEmailSettings(ctx context.Context, ct *Contact) []EmailSettings {
	out := make([]EmailSettings, 0, len(ct.Emails))
	for _, raw := range ct.Emails {
		typed := vcard.ParseTyped("EMAIL", raw)
		es := SettingsOf(ctx, ct, typed.Value)
		if es == nil {
			es = &EmailSettings{Address: typed.Value, SignatureVerified: ct.Signature == pgp.Verified}
		}
		es.Kind = typed.Kind
		out = append(out, *es)
	}
	return out
}

// SettingsOf reads what a contact already in hand holds for one of its
// addresses, or nil when its signed card does not hold the address.
//
// Only the signed card is read. Settings in the clear or the encrypted card
// were vouched for by nobody, and Proton's own clients do not read them either.
func SettingsOf(ctx context.Context, ct *Contact, email string) *EmailSettings {
	signed := vcard.ParseSigned(ct.signed)
	e := signed.FindEmail(email)
	if e == nil {
		return nil
	}
	keys := decodePinnedKeys(ctx, ct.ID, e.KeyValues)
	es := &EmailSettings{
		Address: e.Address,
		Kind:    e.Kind,
		Keys:    keys,
		EmailPreferences: EmailPreferences{
			Encrypt:   e.EncryptUntrusted,
			Sign:      e.Sign,
			Scheme:    e.Scheme,
			PlainText: e.MIMEType == vcard.MIMETypePlain,
		},
		SignatureVerified: ct.Signature == pgp.Verified,
		Unknown:           len(keys) < len(e.KeyValues),
	}
	if len(e.KeyValues) > 0 {
		es.Encrypt = e.Encrypt
	}
	return es
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

// editSignedCard rewrites a contact's signed card through edit, re-signing it
// and sending every other card back as it came.
//
// The signed card's verdict comes back rather than deciding anything here. A
// card that does not verify is far more often one signed by a key this account
// has since retired than one somebody altered - detached verification cannot
// tell the two apart - and Proton's own client saves over either. So the write
// goes ahead and the verdict is reported, which is the one thing a refusal could
// not do: leave the person knowing.
func (s *Service) editSignedCard(ctx context.Context, id string, edit func(*vcard.Signed) error) (pgp.VerifyResult, error) {
	u, err := s.keys(ctx)
	if err != nil {
		return "", err
	}
	cards, err := s.rawContactCards(ctx, id)
	if err != nil {
		return "", err
	}
	var signedData string
	verdict := pgp.Unsigned
	haveSigned := false
	var others []any
	for _, c := range cards {
		if c.Type == pgp.CardSigned && !haveSigned {
			verdict = pgp.VerifyDetachedStatus(u.UserKR, gopenpgp.NewPlainMessageFromString(c.Data), c.Signature)
			signedData = c.Data
			haveSigned = true
			continue
		}
		others = append(others, map[string]any{"Type": c.Type, "Data": c.Data, "Signature": c.Signature})
	}
	if !haveSigned {
		return "", fmt.Errorf("contact has no signed card to edit")
	}
	model := vcard.ParseSigned(signedData)
	if model.UID == "" {
		model.UID = vcard.UID()
	}
	if err := edit(&model); err != nil {
		return "", err
	}
	kr, err := s.writeKey(ctx)
	if err != nil {
		return "", err
	}
	signedCard, err := pgp.SignCard(vcard.BuildSigned(model), kr)
	if err != nil {
		return "", err
	}
	return verdict, s.C.Decode(ctx, contactWrite(id, append([]any{signedCard}, others...)), nil)
}

// PinKey pins armoredKey to the contact's address as its preferred key, and
// encrypts mail to the address from then on.
//
// It returns the verdict on the card it rewrote, for the caller to say.
func (s *Service) PinKey(ctx context.Context, id, email, armoredKey string) (pgp.VerifyResult, error) {
	keyValue, err := encodePinnedKey(armoredKey)
	if err != nil {
		return "", err
	}
	return s.editSignedCard(ctx, id, func(model *vcard.Signed) error {
		e := model.FindEmail(email)
		if e == nil {
			model.Emails = append(model.Emails, vcard.SignedEmail{Address: email})
			e = &model.Emails[len(model.Emails)-1]
		}
		on := true
		e.KeyValues = prependUnique(e.KeyValues, keyValue)
		e.Encrypt, e.EncryptUntrusted = &on, nil
		return nil
	})
}

// UnpinKey removes the keys a contact pins for email, and the choice of whether
// to encrypt to them, returning the verdict on the card it rewrote.
func (s *Service) UnpinKey(ctx context.Context, id, email string) (pgp.VerifyResult, error) {
	return s.editSignedCard(ctx, id, func(model *vcard.Signed) error {
		e := model.FindEmail(email)
		if e == nil || len(e.KeyValues) == 0 {
			return &errs.NotFound{Kind: "pinned key", Ref: email}
		}
		e.KeyValues, e.Encrypt = nil, nil
		return nil
	})
}

// SetEmailPreferences stores how mail to one of a contact's addresses is sent,
// returning the verdict on the card it rewrote.
func (s *Service) SetEmailPreferences(ctx context.Context, id, email string, p EmailPreferences) (pgp.VerifyResult, error) {
	return s.editSignedCard(ctx, id, func(model *vcard.Signed) error {
		e := model.FindEmail(email)
		if e == nil {
			model.Emails = append(model.Emails, vcard.SignedEmail{Address: email})
			e = &model.Emails[len(model.Emails)-1]
		}
		e.Encrypt, e.EncryptUntrusted = nil, nil
		if len(e.KeyValues) > 0 {
			e.Encrypt = p.Encrypt
		} else {
			e.EncryptUntrusted = p.Encrypt
		}
		e.Sign, e.Scheme, e.MIMEType = p.Sign, p.Scheme, ""
		if p.PlainText {
			e.MIMEType = vcard.MIMETypePlain
		}
		return nil
	})
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
