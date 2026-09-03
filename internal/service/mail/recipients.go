package mail

import (
	"context"
	"log/slog"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/proton"
)

// Proton send-package types (PACKAGE_TYPE).
const (
	pkgInternal  = 1  // SEND_PM: E2EE to a Proton user
	pkgEO        = 2  // SEND_EO: encrypted-for-outside (password link)
	pkgClear     = 4  // SEND_CLEAR: cleartext (TLS only)
	pkgPGPInline = 8  // SEND_PGP_INLINE: PGP-Inline (plaintext body) to an external key
	pkgPGPMIME   = 16 // SEND_PGP_MIME: encrypted to an external recipient's PGP key
)

const (
	// keyFlagEmailNoEncrypt (KEY_FLAG.FLAG_EMAIL_NO_ENCRYPT) marks a key that
	// cannot be used to encrypt mail (external address, e2ee-disabled, etc.).
	keyFlagEmailNoEncrypt = 4
	// apiKeySourceProton (API_KEY_SOURCE.PROTON) marks an internal Proton key;
	// any other source (WKD, KOO) is an external key.
	apiKeySourceProton = 0

	// defaultEOExpirationSeconds mirrors DEFAULT_EO_EXPIRATION_DAYS (28 days):
	// Proton always attaches an expiration to encrypted-for-outside messages.
	defaultEOExpirationSeconds = 28 * 24 * 60 * 60
	// srpAuthVersion is Proton's current SRP verifier version.
	srpAuthVersion = 4
)

// sendScheme is how a single recipient's copy is packaged.
type sendScheme int

const (
	schemeInternal       sendScheme = iota // Proton user -> E2EE
	schemeExternalPGP                      // external user with a usable PGP key -> PGP/MIME
	schemeExternalInline                   // external user with a pinned key preferring PGP-Inline
	schemeEO                               // external user + EO password -> password link
	schemeClear                            // external user, no key, no password -> cleartext
)

// externalScheme maps a pinned contact's x-pm-scheme to the send scheme for an
// external recipient: PGP-Inline when requested, otherwise PGP/MIME.
func externalScheme(pinScheme string) sendScheme {
	if pinScheme == "pgp-inline" {
		return schemeExternalInline
	}
	return schemeExternalPGP
}

// PinnedRecipient is a recipient's contact-pinned encryption preferences,
// resolved from Contacts. Presence of a pinned key defaults encryption ON
// (unless Encrypt is explicitly false), matching the Proton web client.
type PinnedRecipient struct {
	ArmoredKeys       []string
	Encrypt           *bool
	Sign              *bool
	Scheme            string
	SignatureVerified bool
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

type plannedRecipient struct {
	email      string
	scheme     sendScheme
	armoredKey string // recipient public key for internal + external-PGP schemes
}

func mailCapable(flags int) bool { return flags&keyFlagEmailNoEncrypt == 0 }

// classifyRecipient picks the send scheme (and recipient key, if any) from a
// /core/v4/keys/all response, mirroring the web client's getPublicKeys logic:
// mail-capable internal address keys mark a Proton user; otherwise a mail-capable
// external key (WKD/KOO) enables PGP/MIME; otherwise EO (with a password) or
// cleartext.
func classifyRecipient(resp keysAllResponse, eoPassword string) (sendScheme, string) {
	for _, k := range resp.Address.Keys {
		if mailCapable(k.Flags) {
			return schemeInternal, k.PublicKey
		}
	}
	for _, k := range resp.Unverified.Keys {
		if k.Source == apiKeySourceProton && mailCapable(k.Flags) {
			return schemeInternal, k.PublicKey
		}
	}
	for _, k := range resp.Unverified.Keys {
		if k.Source != apiKeySourceProton && mailCapable(k.Flags) {
			return schemeExternalPGP, k.PublicKey
		}
	}
	if eoPassword != "" {
		return schemeEO, ""
	}
	return schemeClear, ""
}

func (s *Service) planRecipient(ctx context.Context, email, eoPassword string, pin *PinnedRecipient) (plannedRecipient, error) {
	var resp keysAllResponse
	if err := s.C.Decode(ctx, proton.Request{
		Method: "GET", Path: "/core/v4/keys/all",
		Query: proton.Query("Email", email, "InternalOnly", "0"),
	}, &resp); err != nil {
		return plannedRecipient{}, err
	}
	scheme, armored := classifyRecipient(resp, eoPassword)
	if pin != nil && len(pin.ArmoredKeys) > 0 && pinEncrypts(pin) {
		return planPinnedRecipient(ctx, email, scheme, armored, pin)
	}
	return plannedRecipient{email: email, scheme: scheme, armoredKey: armored}, nil
}

// planRecipients classifies every recipient of a message once, reporting whether
// the shared body package and an encrypted-for-outside package are needed.
func (s *Service) planRecipients(ctx context.Context, c Content, del Delivery) (plans []plannedRecipient, needBody, hasEO bool, err error) {
	for _, email := range c.RecipientAddresses() {
		p, err := s.planRecipient(ctx, email, del.EOPassword, del.PinnedKeys[email])
		if err != nil {
			return nil, false, false, err
		}
		plans = append(plans, p)
		// PGP/MIME and PGP-Inline recipients each get their own body package;
		// everything else shares the single internal/EO/clear body.
		if p.scheme != schemeExternalPGP && p.scheme != schemeExternalInline {
			needBody = true
		}
		if p.scheme == schemeEO {
			hasEO = true
		}
	}
	return plans, needBody, hasEO, nil
}

// pinEncrypts reports whether the pinned config asks us to encrypt. Presence of
// a pinned key defaults encryption ON (matching the web client); an explicit
// x-pm-encrypt:false opts out.
func pinEncrypts(pin *PinnedRecipient) bool {
	return pin.Encrypt == nil || *pin.Encrypt
}

// validForSending mirrors the web client's getIsValidForSending: a key must be
// encryption-capable and neither expired nor revoked.
func validForSending(key *pgp.Key) bool {
	return key.CanEncrypt() && !key.IsExpired() && !key.IsRevoked()
}

// planPinnedRecipient resolves a recipient's send scheme when their contact
// pins a key, mirroring extractEncryptionPreferences:
//   - internal / external-WKD: the recipient's primary API key must itself be
//     pinned (same fingerprint); we then send to the pinned copy. A mismatch is
//     the web client's PRIMARY_NOT_PINNED error.
//   - external without a server/WKD key: encrypt (PGP/MIME) to the first valid
//     pinned key.
func planPinnedRecipient(ctx context.Context, email string, base sendScheme, apiArmored string, pin *PinnedRecipient) (plannedRecipient, error) {
	// The three refusals below are about keys this account chose to pin, so each
	// one is something the person sending can put right and none of them is a
	// fault in this CLI. Saying so is what keeps them off the exit code that
	// means "report this".
	if !pin.SignatureVerified {
		return plannedRecipient{}, errs.Problemf(
			"The contact signature for %s could not be verified, so its pinned key is not trusted.", email).
			Hint("open the contact in a Proton app to re-sign it, or unpin the key")
	}
	type pinnedKey struct{ armored, fingerprint string }
	var valid []pinnedKey
	// Recorded and not counted. Nothing is hidden: when no pinned key survives,
	// the refusal below says so and the message is not sent - so the log is here
	// to say which of the pinned keys was the problem, not to warn about a
	// listing that came up short.
	for _, a := range pin.ArmoredKeys {
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
		return plannedRecipient{}, errs.Problemf(
			"No pinned key for %s can encrypt: they are expired, revoked, or not encryption keys.", email).
			Hint("proton contacts keys unpin --email " + email)
	}
	switch base {
	case schemeInternal, schemeExternalPGP:
		primaryFingerprint := ""
		if apiArmored != "" {
			if k, err := pgp.NewKeyFromArmored(apiArmored); err == nil {
				primaryFingerprint = k.GetFingerprint()
			}
		}
		sendScheme := base
		if base == schemeExternalPGP {
			// A WKD external recipient may prefer PGP-Inline over PGP/MIME.
			sendScheme = externalScheme(pin.Scheme)
		}
		for _, v := range valid {
			if v.fingerprint == primaryFingerprint {
				return plannedRecipient{email: email, scheme: sendScheme, armoredKey: v.armored}, nil
			}
		}
		return plannedRecipient{}, errs.Problemf(
			"The pinned key(s) for %s do not match the recipient's current primary key.", email).
			Hint("update the pinned key before sending",
				"proton contacts keys pin --email "+email+" --key FILE")
	default:
		// External recipient with no server/WKD key: encrypt to the pinned key.
		return plannedRecipient{email: email, scheme: externalScheme(pin.Scheme), armoredKey: valid[0].armored}, nil
	}
}
