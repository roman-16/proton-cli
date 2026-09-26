package mail

import (
	"context"
	"errors"
	"log/slog"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/proton"
	"github.com/roman-16/proton-cli/internal/vcard"
)

// Proton send-package types (PACKAGE_TYPE). A package's Type is the union of
// the types of the recipients it carries.
const (
	pkgInternal  = 1  // SEND_PM: E2EE to a Proton user
	pkgEO        = 2  // SEND_EO: encrypted-for-outside (password link)
	pkgClear     = 4  // SEND_CLEAR: cleartext, PGP/Inline-signed when asked
	pkgPGPInline = 8  // SEND_PGP_INLINE: PGP/Inline to an external key
	pkgPGPMIME   = 16 // SEND_PGP_MIME: PGP/MIME to an external key
	pkgClearMIME = 32 // SEND_CLEAR_MIME: cleartext, PGP/MIME-signed
)

const (
	mimeTypeMultipart = "multipart/mixed"

	// keyFlagEmailNoEncrypt (KEY_FLAG.FLAG_EMAIL_NO_ENCRYPT) marks a key that
	// cannot be used to encrypt mail (external address, e2ee-disabled, etc.).
	keyFlagEmailNoEncrypt = 4
	// apiKeySourceProton (API_KEY_SOURCE.PROTON) marks an internal Proton key;
	// any other source (WKD, KOO) is an external key.
	apiKeySourceProton = 0

	// signEnabled is MailSettings.Sign turned on.
	signEnabled = 1

	// defaultEOExpirationSeconds mirrors DEFAULT_EO_EXPIRATION_DAYS (28 days):
	// Proton always attaches an expiration to encrypted-for-outside messages.
	defaultEOExpirationSeconds = 28 * 24 * 60 * 60
)

// ContactSettings is what a recipient's contact says about mail sent to the
// address. Encrypt is about the pinned keys when there are any, and about the
// keys the address's provider publishes when there are none; a nil Encrypt
// means yes, and a nil Sign follows the account.
type ContactSettings struct {
	Keys              []string
	Encrypt           *bool
	Sign              *bool
	Scheme            string
	PlainText         bool
	SignatureVerified bool
	// Unknown says the contact exists and what it holds could not be read.
	Unknown bool
}

// Destination is what an address is before any contact says anything about it:
// a Proton mailbox, one whose provider publishes a key, or neither.
type Destination struct {
	Proton      bool
	ProviderKey bool
}

// SendDefaults are the account's own choices for mail to addresses outside
// Proton, which an address follows where its contact makes none.
type SendDefaults struct {
	Sign   bool
	Inline bool
}

// Preferences is how mail to one address goes out.
type Preferences struct {
	Encrypt   bool
	Sign      bool
	Inline    bool
	PlainText bool
}

// Resolve decides how mail to an address goes out from what it is, what its
// contact says and what the account says, by the rules Proton's own clients
// send by: mail to a Proton address is always encrypted and signed, encrypted
// mail is always signed, and signed mail outside Proton is plain text under
// PGP/Inline and in the format it was written in under PGP/MIME.
func Resolve(d Destination, c ContactSettings, def SendDefaults) Preferences {
	if d.Proton {
		return Preferences{Encrypt: true, Sign: true, PlainText: c.PlainText}
	}
	var p Preferences
	if len(c.Keys) > 0 || d.ProviderKey {
		p.Encrypt = c.Encrypt == nil || *c.Encrypt
	}
	p.Sign = def.Sign
	if c.Sign != nil {
		p.Sign = *c.Sign
	}
	p.Sign = p.Sign || p.Encrypt
	switch c.Scheme {
	case vcard.SchemeInline:
		p.Inline = true
	case vcard.SchemeMIME:
	default:
		p.Inline = def.Inline
	}
	p.PlainText = c.PlainText
	if p.Sign {
		p.PlainText = p.Inline
	}
	return p
}

type apiPublicKey struct {
	PublicKey string
	Flags     int
	Source    int
}

type keysAllResponse struct {
	Address    struct{ Keys []apiPublicKey }
	Unverified struct{ Keys []apiPublicKey }
	ProtonMX   bool
}

// plannedRecipient is one recipient's part of a send: the sub-package type,
// whether a cleartext copy is signed, which of the message's bodies it gets,
// and the key its copy is encrypted to.
type plannedRecipient struct {
	email      string
	kind       int
	signature  bool
	mimeType   string
	armoredKey string
}

func mailCapable(flags int) bool { return flags&keyFlagEmailNoEncrypt == 0 }

// destinationFrom reads a /core/v4/keys/all response the way the web client's
// getPublicKeys does - a mail-capable Proton key marks a Proton mailbox, and a
// mail-capable key from anywhere else is one the provider publishes - and
// returns the key a send encrypts to when nothing is pinned.
func destinationFrom(resp keysAllResponse) (Destination, string) {
	for _, k := range resp.Address.Keys {
		if mailCapable(k.Flags) {
			return Destination{Proton: true}, k.PublicKey
		}
	}
	for _, k := range resp.Unverified.Keys {
		if k.Source == apiKeySourceProton && mailCapable(k.Flags) {
			return Destination{Proton: true}, k.PublicKey
		}
	}
	for _, k := range resp.Unverified.Keys {
		if k.Source != apiKeySourceProton && mailCapable(k.Flags) {
			return Destination{ProviderKey: true}, k.PublicKey
		}
	}
	return Destination{}, ""
}

func (s *Service) keysFor(ctx context.Context, email string) (keysAllResponse, error) {
	var resp keysAllResponse
	err := s.C.Decode(ctx, proton.Request{
		Method: "GET", Path: "/core/v4/keys/all",
		Query: proton.Query("Email", email, "InternalOnly", "0"),
	}, &resp)
	return resp, err
}

// Proton's answers for an address it holds no keys for, which its own
// email-settings editor reads as an address outside Proton with none.
var noKeysCodes = map[int]bool{33101: true, 33102: true, 33103: true}

// Destination says what an address is.
func (s *Service) Destination(ctx context.Context, email string) (Destination, error) {
	resp, err := s.keysFor(ctx, email)
	var apiErr *proton.APIError
	if errors.As(err, &apiErr) && noKeysCodes[apiErr.Code] {
		// Recorded and not counted: the answer is the destination itself, an
		// address with no key, and the code is what says which of the ways
		// Proton has of saying so it was.
		slog.DebugContext(ctx, "mail: no keys for an address", "signer", email, "code", apiErr.Code)
		return Destination{}, nil
	}
	if err != nil {
		return Destination{}, err
	}
	d, _ := destinationFrom(resp)
	return d, nil
}

// SendDefaults reads the account's sign and pgp-scheme settings.
func (s *Service) SendDefaults(ctx context.Context) (SendDefaults, error) {
	set, err := s.settings(ctx)
	if err != nil {
		return SendDefaults{}, err
	}
	return SendDefaults{Sign: set.Sign == signEnabled, Inline: set.PGPScheme == pkgPGPInline}, nil
}

func (s *Service) planRecipient(ctx context.Context, email, eoPassword string, contact *ContactSettings, defaults SendDefaults, composer string) (plannedRecipient, error) {
	// A pin that cannot be seen is not the same as no pin. The person pinned a
	// key so that mail to this address goes to it and nothing else; sending under
	// whatever Proton hands back because their contact would not open is that
	// decision reversed without a word. Proton's own composer does send here;
	// this one stops, since a sent message cannot be taken back and a stopped
	// one can be sent again. Judged before the keys request, which a stopped
	// send has no use for.
	if contact != nil && contact.Unknown {
		return plannedRecipient{}, errs.Problemf(
			"The contact for %s could not be read, so whether it pins a key is unknown. Nothing was sent.", email).
			Hint("proton contacts get " + email + " says what is wrong with it")
	}
	resp, err := s.keysFor(ctx, email)
	if err != nil {
		return plannedRecipient{}, err
	}
	dest, key := destinationFrom(resp)
	var cs ContactSettings
	if contact != nil {
		cs = *contact
	}
	pref := Resolve(dest, cs, defaults)
	if len(cs.Keys) > 0 && pref.Encrypt {
		if key, err = pinnedSendKey(ctx, email, key, cs); err != nil {
			return plannedRecipient{}, err
		}
	}
	format := composer
	if pref.PlainText {
		format = mimeTypePlain
	}
	p := plannedRecipient{email: email}
	switch {
	case dest.Proton:
		p.kind, p.mimeType, p.armoredKey = pkgInternal, format, key
	case pref.Encrypt && pref.Inline:
		p.kind, p.mimeType, p.armoredKey = pkgPGPInline, mimeTypePlain, key
	case pref.Encrypt:
		p.kind, p.mimeType, p.armoredKey = pkgPGPMIME, mimeTypeMultipart, key
	case eoPassword != "":
		p.kind, p.mimeType = pkgEO, composer
	case pref.Sign && pref.Inline:
		p.kind, p.signature, p.mimeType = pkgClear, true, mimeTypePlain
	case pref.Sign:
		p.kind, p.signature, p.mimeType = pkgClearMIME, true, mimeTypeMultipart
	default:
		p.kind, p.mimeType = pkgClear, format
	}
	return p, nil
}

// planRecipients plans every recipient of a message once, reporting whether any
// of them is sent a password-protected copy.
func (s *Service) planRecipients(ctx context.Context, c Content, del Delivery) (plans []plannedRecipient, hasEO bool, err error) {
	defaults, err := s.SendDefaults(ctx)
	if err != nil {
		return nil, false, err
	}
	for _, email := range c.RecipientAddresses() {
		p, err := s.planRecipient(ctx, email, del.EOPassword, del.Contacts[email], defaults, c.mimeType())
		if err != nil {
			return nil, false, err
		}
		plans = append(plans, p)
		hasEO = hasEO || p.kind == pkgEO
	}
	return plans, hasEO, nil
}

// validForSending mirrors the web client's getIsValidForSending: a key must be
// encryption-capable and neither expired nor revoked.
func validForSending(key *pgp.Key) bool {
	return key.CanEncrypt() && !key.IsExpired() && !key.IsRevoked()
}

// pinnedSendKey picks the key mail to a recipient whose contact pins keys is
// encrypted to, mirroring extractEncryptionPreferences: when the address has a
// key of its own - a Proton key, or one its provider publishes - that key must
// be among the pinned ones, and the pinned copy is used; otherwise the first
// pinned key that can encrypt is.
func pinnedSendKey(ctx context.Context, email, apiArmored string, cs ContactSettings) (string, error) {
	// The three refusals below are about keys this account chose to pin, so each
	// one is something the person sending can put right and none of them is a
	// fault in this CLI. Saying so is what keeps them off the exit code that
	// means "report this".
	if !cs.SignatureVerified {
		return "", errs.Problemf(
			"The contact signature for %s could not be verified, so its pinned key is not trusted.", email).
			Hint("open the contact in a Proton app to re-sign it, or unpin the key")
	}
	type pinnedKey struct{ armored, fingerprint string }
	var valid []pinnedKey
	// Recorded and not counted. Nothing is hidden: when no pinned key survives,
	// the refusal below says so and the message is not sent - so the log is here
	// to say which of the pinned keys was the problem, not to warn about a
	// listing that came up short.
	for _, a := range cs.Keys {
		key, err := pgp.NewKeyFromArmored(a)
		if err != nil {
			slog.DebugContext(ctx, "mail: a pinned key is not readable armour",
				"signer", email, "error", err)
			continue
		}
		if !validForSending(key) {
			slog.DebugContext(ctx, "mail: a pinned key cannot encrypt",
				"signer", email, "reason", "expired, revoked or not an encryption key")
			continue
		}
		valid = append(valid, pinnedKey{armored: a, fingerprint: key.GetFingerprint()})
	}
	if len(valid) == 0 {
		return "", errs.Problemf(
			"No pinned key for %s can encrypt: they are expired, revoked, or not encryption keys.", email).
			Hint("proton contacts keys unpin " + email)
	}
	if apiArmored == "" {
		return valid[0].armored, nil
	}
	primaryFingerprint := ""
	// Recorded and not counted. The refusal below is on the screen and the
	// message is not sent, so nothing is hidden - this is here because a primary
	// key that could not be read and one that genuinely differs from the pinned
	// key reach that refusal looking identical.
	if k, err := pgp.NewKeyFromArmored(apiArmored); err != nil {
		slog.DebugContext(ctx, "mail: a recipient's primary key is not readable armour",
			"signer", email, "error", err)
	} else {
		primaryFingerprint = k.GetFingerprint()
	}
	for _, v := range valid {
		if v.fingerprint == primaryFingerprint {
			return v.armored, nil
		}
	}
	return "", errs.Problemf(
		"The pinned key(s) for %s do not match the recipient's current primary key.", email).
		Hint("update the pinned key before sending",
			"proton contacts keys pin "+email+" --key FILE")
}
